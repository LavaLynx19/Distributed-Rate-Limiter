// Package buffer implements loose mode's local write-back cache: requests are
// decided in process memory and synced to Redis in batches, trading exact
// accuracy for throughput (ARCHITECTURE.md section 6).
package buffer

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/manan/distributed-rate-limiter/go/internal/config"
	"github.com/manan/distributed-rate-limiter/go/internal/redis"
)

// counters packs an identity's allowance and pending count into one word, so
// every admit and every write-back sees a consistent pair without a lock.
//
//	allowance (high 32 bits, signed): how many more requests this instance may
//	    admit before it hears from Redis again
//	pending   (low 32 bits): admitted requests not yet written to Redis
//
// With the pair updated atomically, a write-back can set the allowance to
// exactly "Redis's remaining minus what was admitted since the batch was
// taken" — no request is double-counted or missed, however admits interleave.
type counters struct{ word atomic.Uint64 }

func pack(allowance, pending int64) uint64 {
	return uint64(uint32(int32(clamp32(allowance))))<<32 | uint64(uint32(pending))
}

func unpack(word uint64) (allowance, pending int64) {
	return int64(int32(uint32(word >> 32))), int64(uint32(word))
}

// clamp32 bounds an allowance to the packed field. Limits beyond ~2.1 billion
// per window are treated as that ceiling.
func clamp32(v int64) int64 {
	return max(math.MinInt32, min(math.MaxInt32, v))
}

// update applies f to the pair until the compare-and-swap succeeds. f returns
// ok=false to leave the word unchanged.
func (c *counters) update(f func(allowance, pending int64) (int64, int64, bool)) (allowance, pending int64, changed bool) {
	for {
		old := c.word.Load()
		a, p := unpack(old)
		na, np, ok := f(a, p)
		if !ok {
			return a, p, false
		}
		if c.word.CompareAndSwap(old, pack(na, np)) {
			return na, np, true
		}
	}
}

func (c *counters) load() (allowance, pending int64) { return unpack(c.word.Load()) }

// entry holds one identifier's local state. Every field is atomic so the
// request path only ever takes a read lock on the map.
type entry struct {
	counters counters
	// limit is the identity's sliding-window limit (plans differ per tenant).
	limit atomic.Int64
	// lastSync is the last successful write-back (unix nanos). If none lands
	// for a whole window (Redis down), the allowance resets to the full limit.
	lastSync atomic.Int64
	// lastFlush is the last write-back attempt; it paces refreshes and
	// decides when an idle entry can be pruned.
	lastFlush atomic.Int64
	// retryAt is when the last BLOCKED write-back said capacity frees up
	// (unix nanos), or 0 if the last write-back wasn't BLOCKED.
	retryAt atomic.Int64
	// flushing serialises write-backs for this identifier so a burst cannot
	// spawn one goroutine per request.
	flushing atomic.Bool
}

// Verdict is the local decision for one request.
type Verdict struct {
	Admitted bool
	// Remaining is this instance's allowance left after an admit.
	Remaining int64
	// RetryAfter (seconds) is set on a rejection: the precise value from the
	// last BLOCKED write-back, or 1 if Redis hasn't said BLOCKED yet — the
	// next write-back, at most one tick away, will know.
	RetryAfter int64
}

// writeBacker is the slice of the Redis client the buffer needs. Narrowing it
// keeps the write-back path testable without a live Redis.
type writeBacker interface {
	WriteBack(ctx context.Context, identifier string, limit, batchCount int64) redis.Decision
}

type Buffer struct {
	cfg    config.Config
	client writeBacker

	mu      sync.RWMutex
	entries map[string]*entry

	stop func()
	wg   sync.WaitGroup
}

func New(cfg config.Config, client writeBacker) *Buffer {
	return &Buffer{
		cfg:     cfg,
		client:  client,
		entries: make(map[string]*entry),
	}
}

// Admit decides one request locally and never touches the network; a
// write-back may be triggered in the background. Rejected requests are not
// counted, so a blocked client cannot burn its own future quota.
func (b *Buffer) Admit(identifier string, limit int64) Verdict {
	e := b.lookup(identifier, limit)
	now := time.Now()
	b.resetIfUnsynced(e, now)

	allowance, pending, admitted := e.counters.update(func(a, p int64) (int64, int64, bool) {
		return a - 1, p + 1, a > 0
	})
	if !admitted {
		return reject(e, now)
	}

	if pending >= b.threshold(e.limit.Load()) {
		b.triggerFlush(identifier, e)
	}
	return Verdict{Admitted: true, Remaining: allowance}
}

func reject(e *entry, now time.Time) Verdict {
	retry := int64(1)
	if wait := e.retryAt.Load() - now.UnixNano(); wait > 0 {
		retry = (wait + int64(time.Second) - 1) / int64(time.Second) // round up
	}
	return Verdict{RetryAfter: retry}
}

// threshold is the pending count that triggers an immediate write-back. It
// shrinks for small limits so the overshoot bound stays proportional.
func (b *Buffer) threshold(limit int64) int64 {
	return max(1, min(b.cfg.LooseMode.BatchThreshold, limit/2))
}

func (b *Buffer) lookup(identifier string, limit int64) *entry {
	b.mu.RLock()
	e, ok := b.entries[identifier]
	b.mu.RUnlock()
	if ok {
		// A plan change reaches the buffer as a new limit: shift the
		// allowance by the difference rather than starting over.
		if e.limit.Load() != limit {
			if old := e.limit.Swap(limit); old != limit {
				e.counters.update(func(a, p int64) (int64, int64, bool) { return a + limit - old, p, true })
			}
		}
		return e
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	// Re-check: another goroutine may have created it while we upgraded.
	if e, ok := b.entries[identifier]; ok {
		return e
	}

	// Cluster usage is unknown until the first sync, so start optimistic;
	// the first write-back (at most one batch away) corrects it.
	now := time.Now().UnixNano()
	e = &entry{}
	e.limit.Store(limit)
	e.counters.word.Store(pack(limit, 0))
	e.lastSync.Store(now)
	e.lastFlush.Store(now)
	b.entries[identifier] = e
	return e
}

// resetIfUnsynced restores the full allowance when Redis hasn't answered for
// a whole window — fail-open, as with every other Redis outage. With Redis
// healthy this never fires for active identifiers, since each sync refreshes
// lastSync; an identifier idle for a window has no live segments left anyway.
func (b *Buffer) resetIfUnsynced(e *entry, now time.Time) {
	last := e.lastSync.Load()
	if now.UnixNano()-last <= int64(b.cfg.RateLimiter.WindowDuration) {
		return
	}
	if e.lastSync.CompareAndSwap(last, now.UnixNano()) {
		limit := e.limit.Load()
		e.counters.update(func(_, p int64) (int64, int64, bool) { return limit, p, true })
	}
}

// triggerFlush starts a write-back unless one is already in flight.
func (b *Buffer) triggerFlush(identifier string, e *entry) {
	if !e.flushing.CompareAndSwap(false, true) {
		return
	}

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer e.flushing.Store(false)
		b.flush(identifier, e)
	}()
}

// flush records the pending batch in Redis and resets the allowance to the
// cluster-wide remaining capacity. With nothing pending it is a refresh: an
// exhausted identifier asks whether capacity has freed up.
func (b *Buffer) flush(identifier string, e *entry) {
	// Take the batch; requests admitted from here on accumulate in pending
	// again and are exactly the ones Redis's answer won't include.
	var count int64
	_, _, taken := e.counters.update(func(a, p int64) (int64, int64, bool) {
		count = p
		return a, 0, p > 0 || a <= 0
	})
	if !taken {
		return
	}

	e.lastFlush.Store(time.Now().UnixNano())

	result := b.client.WriteBack(context.Background(), identifier, e.limit.Load(), count)
	if result.FailedOpen {
		// Redis never recorded this batch; keep it for the next attempt
		// instead of silently dropping requests that were already served.
		e.counters.update(func(a, p int64) (int64, int64, bool) { return a, p + count, true })
		return
	}

	e.counters.update(func(_, p int64) (int64, int64, bool) { return result.Remaining - p, p, true })

	now := time.Now()
	e.lastSync.Store(now.UnixNano())
	if result.Allowed {
		e.retryAt.Store(0)
	} else {
		e.retryAt.Store(now.Add(time.Duration(result.ResetTTL) * time.Second).UnixNano())
	}
}

// StartFlushLoop begins periodic write-back, refreshes and stale-entry pruning.
func (b *Buffer) StartFlushLoop(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	b.stop = cancel

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(b.cfg.LooseMode.FlushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.flushAll()
			}
		}
	}()
}

// StopFlushLoop halts the ticker, waits for in-flight write-backs, then writes
// whatever is still pending, so a graceful shutdown loses no counts. Call it
// only after the HTTP server has stopped handing requests to the buffer.
func (b *Buffer) StopFlushLoop() {
	if b.stop != nil {
		b.stop()
	}
	b.wg.Wait()

	b.flushAll()
	b.wg.Wait()
}

func (b *Buffer) flushAll() {
	now := time.Now().UnixNano()
	staleThreshold := int64(b.cfg.RateLimiter.WindowDuration)
	refreshInterval := int64(b.cfg.LooseMode.RefreshInterval)

	var stale []string

	b.mu.RLock()
	for identifier, e := range b.entries {
		allowance, pending := e.counters.load()
		sinceFlush := now - e.lastFlush.Load()
		switch {
		case pending > 0:
			b.triggerFlush(identifier, e)
		case allowance <= 0:
			if sinceFlush >= refreshInterval {
				b.triggerFlush(identifier, e)
			}
		case sinceFlush > staleThreshold:
			stale = append(stale, identifier)
		}
	}
	b.mu.RUnlock()

	if len(stale) == 0 {
		return
	}

	b.mu.Lock()
	for _, identifier := range stale {
		// Re-check under the write lock: traffic may have resumed for this
		// identifier between the two critical sections.
		if e, ok := b.entries[identifier]; ok {
			if _, pending := e.counters.load(); pending == 0 && now-e.lastFlush.Load() > staleThreshold {
				delete(b.entries, identifier)
			}
		}
	}
	b.mu.Unlock()
}

type EntryStats struct {
	Identifier   string `json:"identifier"`
	Limit        int64  `json:"limit"`
	PendingCount int64  `json:"pendingCount"`
	Allowance    int64  `json:"allowance"`
	Blocked      bool   `json:"blocked"`
}

type Stats struct {
	Identifiers int          `json:"identifiers"`
	Entries     []EntryStats `json:"entries"`
}

func (b *Buffer) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := Stats{
		Identifiers: len(b.entries),
		Entries:     make([]EntryStats, 0, len(b.entries)),
	}
	for identifier, e := range b.entries {
		allowance, pending := e.counters.load()
		stats.Entries = append(stats.Entries, EntryStats{
			Identifier:   identifier,
			Limit:        e.limit.Load(),
			PendingCount: pending,
			Allowance:    allowance,
			Blocked:      allowance <= 0,
		})
	}
	return stats
}
