package registry

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"
)

// Lookup fetches a tenant record by key hash: nil fields mean unregistered,
// an error means the lookup itself failed (Redis down or over budget).
type Lookup interface {
	ResolveKey(ctx context.Context, keyHash string) ([]string, error)
}

type cacheEntry struct {
	identity   Identity
	registered bool
	expires    int64 // unix nanos
}

type call struct {
	done     chan struct{}
	identity Identity
	found    bool
	err      error
}

// Resolver turns presented keys into identities, caching results (including
// "unregistered") per gateway so loose mode stays network-free on hits.
type Resolver struct {
	lookup Lookup
	plans  Plans
	ttl    time.Duration
	size   int

	mu    sync.RWMutex
	cache map[string]cacheEntry
	order []string // insertion order, for oldest-first eviction

	flightMu sync.Mutex
	inflight map[string]*call
}

func NewResolver(lookup Lookup, plans Plans, ttl time.Duration, size int) *Resolver {
	return &Resolver{
		lookup:   lookup,
		plans:    plans,
		ttl:      ttl,
		size:     size,
		cache:    make(map[string]cacheEntry, size),
		inflight: make(map[string]*call),
	}
}

// Resolve returns the identity for a presented key. ok is false when the
// caller should fall back to IP identity: the key is malformed, unregistered,
// on an unknown plan, or couldn't be looked up.
func (r *Resolver) Resolve(ctx context.Context, key string) (Identity, bool) {
	if !ValidFormat(key) {
		return Identity{}, false
	}
	hash := Hash(key)
	now := time.Now().UnixNano()

	r.mu.RLock()
	entry, hit := r.cache[hash]
	r.mu.RUnlock()
	if hit && entry.expires > now {
		return entry.identity, entry.registered
	}

	identity, found, err := r.fetch(ctx, hash)
	if err != nil {
		// Not cached: the next request retries instead of being stuck on
		// IP identity for a whole TTL after a blip.
		return Identity{}, false
	}
	r.store(hash, cacheEntry{identity: identity, registered: found, expires: now + int64(r.ttl)})
	return identity, found
}

// fetch performs one lookup per key even when many requests miss at once.
func (r *Resolver) fetch(ctx context.Context, hash string) (Identity, bool, error) {
	r.flightMu.Lock()
	if c, ok := r.inflight[hash]; ok {
		r.flightMu.Unlock()
		<-c.done
		return c.identity, c.found, c.err
	}
	c := &call{done: make(chan struct{})}
	r.inflight[hash] = c
	r.flightMu.Unlock()

	c.identity, c.found, c.err = r.lookupIdentity(ctx, hash)

	r.flightMu.Lock()
	delete(r.inflight, hash)
	r.flightMu.Unlock()
	close(c.done)
	return c.identity, c.found, c.err
}

func (r *Resolver) lookupIdentity(ctx context.Context, hash string) (Identity, bool, error) {
	fields, err := r.lookup.ResolveKey(ctx, hash)
	if err != nil || len(fields) == 0 {
		return Identity{}, false, err
	}
	if len(fields) != 4 {
		log.Printf("[Registry] Unexpected record shape for key %s…: %v", hash[:8], fields)
		return Identity{}, false, nil
	}

	keys, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		keys = 1
	}
	tenant := Tenant{ID: fields[0], Plan: fields[1], Keys: keys, Mode: fields[3]}

	identity, ok := r.plans.identityFor(tenant, hash)
	if !ok {
		log.Printf("[Registry] Tenant %q has unknown plan %q; using anonymous limit", tenant.ID, tenant.Plan)
	}
	return identity, ok, nil
}

func (r *Resolver) store(hash string, entry cacheEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.cache[hash]; !exists {
		r.order = append(r.order, hash)
		for len(r.order) > r.size {
			delete(r.cache, r.order[0])
			r.order = r.order[1:]
		}
	}
	r.cache[hash] = entry
}
