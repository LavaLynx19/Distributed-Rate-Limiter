package buffer

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/manan/distributed-rate-limiter/go/internal/config"
	"github.com/manan/distributed-rate-limiter/go/internal/redis"
)

// fakeRedis mimics the Lua script's writeback mode for one identifier: every
// batch is recorded, and the reply carries the cluster-wide remaining
// capacity. total can be set directly to simulate other instances' usage or
// segments expiring.
type fakeRedis struct {
	mu       sync.Mutex
	limit    int64
	total    int64
	batches  []int64
	calls    int
	failOpen bool
}

func (f *fakeRedis) WriteBack(_ context.Context, _ string, limit, batch int64) redis.Decision {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.limit = limit
	if f.failOpen {
		return redis.Decision{Allowed: true, Remaining: -1, FailedOpen: true}
	}
	if batch > 0 {
		f.batches = append(f.batches, batch)
	}
	f.total += batch
	if remaining := f.limit - f.total; remaining > 0 {
		return redis.Decision{Allowed: true, Remaining: remaining, ResetTTL: 60}
	}
	return redis.Decision{Allowed: false, Remaining: 0, ResetTTL: 60}
}

func (f *fakeRedis) set(total int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.total = total
}

func (f *fakeRedis) snapshot() (total int64, batches []int64, calls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total, append([]int64(nil), f.batches...), f.calls
}

func newTestBuffer(t *testing.T) (*Buffer, *fakeRedis) {
	t.Helper()
	cfg := config.Load()
	cfg.RateLimiter.MaxLimit = 100
	cfg.RateLimiter.WindowDuration = 300 * time.Second
	cfg.LooseMode.BatchThreshold = 50
	cfg.LooseMode.RefreshInterval = 5 * time.Second
	fake := &fakeRedis{limit: 100}
	return New(cfg, fake), fake
}

func admitN(b *Buffer, id string, n int) (admitted int) {
	for i := 0; i < n; i++ {
		if b.Admit(id, 100).Admitted {
			admitted++
		}
	}
	return admitted
}

func TestThresholdFlushRecordsBatch(t *testing.T) {
	b, fake := newTestBuffer(t)

	admitN(b, "user", 50)
	b.wg.Wait()

	if _, batches, _ := fake.snapshot(); len(batches) != 1 || batches[0] != 50 {
		t.Fatalf("batches = %v, want one batch of 50", batches)
	}
	e := b.entries["user"]
	if _, pending := e.counters.load(); pending != 0 {
		t.Errorf("pending = %d, want 0 after flush", pending)
	}
	if allowance, _ := e.counters.load(); allowance != 50 {
		t.Errorf("allowance = %d, want Redis's remaining 50", allowance)
	}
}

func TestLocalGuardAdmitsExactlyTheLimit(t *testing.T) {
	b, fake := newTestBuffer(t)

	admitted := admitN(b, "flooder", 150)
	b.StopFlushLoop()

	if admitted != 100 {
		t.Errorf("admitted = %d, want exactly the limit of 100", admitted)
	}
	// Rejected requests must not consume quota.
	if total, _, _ := fake.snapshot(); total != 100 {
		t.Errorf("recorded in Redis = %d, want 100", total)
	}
}

// The multi-instance bug: another gateway has already used most of the
// window. This instance must learn that at its first sync, admit at most one
// batch beyond the limit, and record every request it served.
func TestSyncLearnsOtherInstancesUsage(t *testing.T) {
	b, fake := newTestBuffer(t)
	fake.set(90) // another instance's usage

	admitted := admitN(b, "shared", 50) // first batch: cluster usage unknown
	b.wg.Wait()
	admitted += admitN(b, "shared", 50) // after sync: must all be rejected
	b.StopFlushLoop()

	if admitted != 50 {
		t.Errorf("admitted = %d, want 50 — one batch before the first sync, then none", admitted)
	}
	if total, _, _ := fake.snapshot(); total != 140 {
		t.Errorf("recorded in Redis = %d, want 140 — served requests must be counted even over the limit", total)
	}
}

func TestExhaustedIdentifierRefreshes(t *testing.T) {
	b, fake := newTestBuffer(t)

	admitN(b, "user", 100)
	b.wg.Wait()
	// A second batch can fill while the first flush is in flight; write it
	// so that only refreshes remain.
	b.flushAll()
	b.wg.Wait()
	if b.Admit("user", 100).Admitted {
		t.Fatal("expected the identifier to be exhausted")
	}
	_, _, callsBefore := fake.snapshot()

	// Not yet due: the refresh interval hasn't passed.
	b.flushAll()
	b.wg.Wait()
	if _, _, calls := fake.snapshot(); calls != callsBefore {
		t.Fatalf("refreshed early: %d calls, want %d", calls, callsBefore)
	}

	// Two segments expire in Redis; once the interval passes, a refresh
	// should notice and restore the capacity.
	fake.set(30)
	e := b.entries["user"]
	e.lastFlush.Store(time.Now().Add(-6 * time.Second).UnixNano())
	b.flushAll()
	b.wg.Wait()

	if allowance, _ := e.counters.load(); allowance != 70 {
		t.Fatalf("allowance = %d, want 70 after refresh", allowance)
	}
	if !b.Admit("user", 100).Admitted {
		t.Error("expected the identifier to be admitted again after capacity freed")
	}
	if _, batches, _ := fake.snapshot(); len(batches) != 2 {
		t.Errorf("batches = %v — a refresh must not record anything", batches)
	}
}

func TestFailOpenFlushKeepsBatch(t *testing.T) {
	b, fake := newTestBuffer(t)
	fake.failOpen = true

	admitN(b, "user", 50)
	b.wg.Wait()

	if _, pending := b.entries["user"].counters.load(); pending != 50 {
		t.Errorf("pending = %d, want 50 restored after a fail-open flush", pending)
	}
}

func TestUnsyncedWindowResetsAllowance(t *testing.T) {
	b, fake := newTestBuffer(t)
	fake.failOpen = true // Redis down: no sync ever lands

	if admitted := admitN(b, "user", 150); admitted != 100 {
		t.Fatalf("admitted = %d, want the local guard to hold at 100 even with Redis down", admitted)
	}

	e := b.entries["user"]
	e.lastSync.Store(time.Now().Add(-301 * time.Second).UnixNano())

	if !b.Admit("user", 100).Admitted {
		t.Error("after a full window without a sync the allowance should reset (fail-open)")
	}
}

func TestStopFlushLoopWritesRemainder(t *testing.T) {
	b, fake := newTestBuffer(t)

	admitN(b, "user", 30) // below the batch threshold
	b.StopFlushLoop()

	if _, batches, _ := fake.snapshot(); len(batches) != 1 || batches[0] != 30 {
		t.Fatalf("batches = %v, want the 30 pending counts flushed on stop", batches)
	}
}

func TestStats(t *testing.T) {
	b, _ := newTestBuffer(t)

	admitN(b, "user-a", 2)
	admitN(b, "user-b", 1)

	stats := b.Stats()
	if stats.Identifiers != 2 {
		t.Fatalf("Identifiers = %d, want 2", stats.Identifiers)
	}
	for _, e := range stats.Entries {
		if e.Identifier == "user-a" && (e.PendingCount != 2 || e.Allowance != 98 || e.Blocked) {
			t.Errorf("user-a stats = %+v, want pending 2, allowance 98, not blocked", e)
		}
	}
}

func TestConcurrentAdmitsNeverOvershoot(t *testing.T) {
	b, fake := newTestBuffer(t)

	const goroutines, perGoroutine = 20, 100
	var admitted atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if b.Admit("hot-key", 100).Admitted {
					admitted.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	b.StopFlushLoop()

	total, _, _ := fake.snapshot()
	_, pending := b.entries["hot-key"].counters.load()

	// Allowance and pending share one atomic word, so concurrent admits and
	// write-backs can neither overshoot nor undershoot.
	if got := admitted.Load(); got != 100 {
		t.Errorf("admitted = %d of %d attempts, want exactly 100", got, goroutines*perGoroutine)
	}
	// Every admitted request is accounted for in Redis or still pending.
	if total+pending != admitted.Load() {
		t.Errorf("recorded %d + pending %d != admitted %d — counts were lost", total, pending, admitted.Load())
	}
}

func TestRetryAfter(t *testing.T) {
	b, _ := newTestBuffer(t)

	// Exhausted locally before any BLOCKED reply: the next write-back, at most
	// one tick away, will know more, so ask the client back in 1s.
	admitN(b, "user", 100)
	if v := b.Admit("user", 100); v.Admitted || v.RetryAfter != 1 {
		t.Fatalf("before a BLOCKED reply: got %+v, want rejected with RetryAfter 1", v)
	}

	// Once a write-back comes back BLOCKED, report its precise wait.
	b.wg.Wait()
	b.flushAll() // writes the second batch; total 100 → BLOCKED, ResetTTL 60
	b.wg.Wait()
	v := b.Admit("user", 100)
	if v.Admitted || v.RetryAfter < 59 || v.RetryAfter > 60 {
		t.Errorf("after a BLOCKED reply: got %+v, want rejected with RetryAfter ~60", v)
	}
}

func TestPlanChangeShiftsAllowance(t *testing.T) {
	b, _ := newTestBuffer(t)

	admitN(b, "tenant:acme", 30) // allowance 100 -> 70
	if v := b.Admit("tenant:acme", 1000); !v.Admitted || v.Remaining != 969 {
		t.Errorf("after upgrade to 1000: got %+v, want admitted with 969 remaining (70 + 900 - 1)", v)
	}
	if v := b.Admit("tenant:acme", 100); !v.Admitted || v.Remaining != 68 {
		t.Errorf("after downgrade to 100: got %+v, want admitted with 68 remaining", v)
	}
}

func TestThresholdScalesWithSmallLimits(t *testing.T) {
	b, fake := newTestBuffer(t)

	for i := 0; i < 5; i++ { // limit 10: threshold min(50, 10/2) = 5
		b.Admit("small", 10)
	}
	b.wg.Wait()

	if _, batches, _ := fake.snapshot(); len(batches) != 1 || batches[0] != 5 {
		t.Errorf("batches = %v, want one early batch of 5 for a limit of 10", batches)
	}
}

func TestPackRoundTrip(t *testing.T) {
	for _, tc := range []struct{ allowance, pending int64 }{
		{0, 0}, {100, 0}, {-5, 3}, {math.MaxInt32, math.MaxUint32}, {math.MinInt32, 7},
	} {
		if a, p := unpack(pack(tc.allowance, tc.pending)); a != tc.allowance || p != tc.pending {
			t.Errorf("pack/unpack(%d, %d) = (%d, %d)", tc.allowance, tc.pending, a, p)
		}
	}
	if a, _ := unpack(pack(1<<40, 0)); a != math.MaxInt32 {
		t.Errorf("an oversized allowance should clamp to MaxInt32, got %d", a)
	}
}
