package registry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyFormat(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidFormat(key) {
		t.Fatalf("generated key %q fails its own format check", key)
	}
	other, _ := GenerateKey()
	if key == other {
		t.Error("two generated keys are identical")
	}

	for _, bad := range []string{"", "rlk_short", "xyz_" + strings.Repeat("a", 32), "rlk_" + strings.Repeat("a", 31) + "!", "rlk_" + strings.Repeat("a", 33)} {
		if ValidFormat(bad) {
			t.Errorf("ValidFormat(%q) = true, want false", bad)
		}
	}
	// FIPS 180-2 test vector: SHA-256("abc").
	if got := Hash("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("Hash(\"abc\") = %s, want the SHA-256 test vector", got)
	}
}

func TestParsePlans(t *testing.T) {
	plans, err := ParsePlans(" free=100 , paid=1000 ")
	if err != nil || plans["free"] != 100 || plans["paid"] != 1000 {
		t.Fatalf("got %v, %v", plans, err)
	}
	for _, bad := range []string{"", "free", "free=0", "free=-5", "free=abc"} {
		if _, err := ParsePlans(bad); err == nil {
			t.Errorf("ParsePlans(%q) accepted invalid input", bad)
		}
	}
}

func TestIdentityFor(t *testing.T) {
	plans := Plans{"free": 100, "paid": 1000}
	hash := Hash("rlk_" + strings.Repeat("a", 32))

	tests := []struct {
		name   string
		tenant Tenant
		want   Identity
		ok     bool
	}{
		{"isolated: per-key identity at the plan limit", Tenant{ID: "acme", Plan: "paid", Keys: 10, Mode: ModeIsolated}, Identity{ID: "key:" + hash[:16], Limit: 1000}, true},
		{"pooled: tenant identity at plan limit x slots", Tenant{ID: "acme", Plan: "paid", Keys: 10, Mode: ModePooled}, Identity{ID: "tenant:acme", Limit: 10000}, true},
		{"free pooled with one slot", Tenant{ID: "hobby", Plan: "free", Keys: 1, Mode: ModePooled}, Identity{ID: "tenant:hobby", Limit: 100}, true},
		{"zero slots treated as one", Tenant{ID: "odd", Plan: "free", Keys: 0, Mode: ModePooled}, Identity{ID: "tenant:odd", Limit: 100}, true},
		{"unknown plan is not honored", Tenant{ID: "x", Plan: "gold", Keys: 1, Mode: ModePooled}, Identity{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := plans.identityFor(tt.tenant, hash)
			if got != tt.want || ok != tt.ok {
				t.Errorf("got %+v %v, want %+v %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// fakeLookup serves registry records and counts round-trips.
type fakeLookup struct {
	mu      sync.Mutex
	records map[string][]string
	fail    bool
	delay   time.Duration
	calls   atomic.Int64
}

func (f *fakeLookup) ResolveKey(_ context.Context, hash string) ([]string, error) {
	f.calls.Add(1)
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("redis down")
	}
	return f.records[hash], nil
}

func newResolver(t *testing.T, ttl time.Duration, size int) (*Resolver, *fakeLookup, string) {
	t.Helper()
	key, _ := GenerateKey()
	lookup := &fakeLookup{records: map[string][]string{
		Hash(key): {"acme", "paid", "3", ModePooled},
	}}
	return NewResolver(lookup, Plans{"free": 100, "paid": 1000}, ttl, size), lookup, key
}

func TestResolverCachesHitsAndMisses(t *testing.T) {
	r, lookup, key := newResolver(t, time.Minute, 100)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if id, ok := r.Resolve(ctx, key); !ok || id != (Identity{ID: "tenant:acme", Limit: 3000}) {
			t.Fatalf("got %+v %v", id, ok)
		}
	}
	unknown, _ := GenerateKey()
	for i := 0; i < 3; i++ {
		if _, ok := r.Resolve(ctx, unknown); ok {
			t.Fatal("unregistered key resolved")
		}
	}
	if calls := lookup.calls.Load(); calls != 2 {
		t.Errorf("lookups = %d, want 2 (one per key; negative results cached too)", calls)
	}
}

func TestResolverSkipsLookupForMalformedKeys(t *testing.T) {
	r, lookup, _ := newResolver(t, time.Minute, 100)
	if _, ok := r.Resolve(context.Background(), "made-up-key"); ok {
		t.Fatal("malformed key resolved")
	}
	if calls := lookup.calls.Load(); calls != 0 {
		t.Errorf("lookups = %d, want 0 for a malformed key", calls)
	}
}

func TestResolverDoesNotCacheFailures(t *testing.T) {
	r, lookup, key := newResolver(t, time.Minute, 100)
	ctx := context.Background()

	lookup.fail = true
	if _, ok := r.Resolve(ctx, key); ok {
		t.Fatal("resolved during an outage")
	}
	lookup.fail = false
	if _, ok := r.Resolve(ctx, key); !ok {
		t.Error("a failed lookup was cached: the key stayed unresolved after recovery")
	}
}

func TestResolverExpiresEntries(t *testing.T) {
	r, lookup, key := newResolver(t, 20*time.Millisecond, 100)
	ctx := context.Background()

	r.Resolve(ctx, key)
	lookup.mu.Lock()
	delete(lookup.records, Hash(key)) // revoked
	lookup.mu.Unlock()

	if _, ok := r.Resolve(ctx, key); !ok {
		t.Fatal("expected the cached identity before the TTL elapses")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := r.Resolve(ctx, key); ok {
		t.Error("revoked key still honored after the TTL")
	}
}

func TestResolverBoundsCacheSize(t *testing.T) {
	r, _, _ := newResolver(t, time.Minute, 10)
	for i := 0; i < 50; i++ {
		k, _ := GenerateKey()
		r.Resolve(context.Background(), k)
	}
	if n := len(r.cache); n != 10 {
		t.Errorf("cache holds %d entries, want it capped at 10", n)
	}
}

func TestResolverDeduplicatesConcurrentMisses(t *testing.T) {
	r, lookup, key := newResolver(t, time.Minute, 100)
	lookup.delay = 20 * time.Millisecond

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := r.Resolve(context.Background(), key); !ok {
				t.Error("concurrent resolve failed")
			}
		}()
	}
	wg.Wait()

	if calls := lookup.calls.Load(); calls != 1 {
		t.Errorf("lookups = %d, want 1 for 50 concurrent misses on one key", calls)
	}
}
