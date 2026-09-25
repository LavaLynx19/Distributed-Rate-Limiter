package middleware

import (
	"context"
	"testing"

	"github.com/manan/distributed-rate-limiter/go/internal/config"
	"github.com/manan/distributed-rate-limiter/go/internal/registry"
)

type fakeResolver map[string]registry.Identity

func (f fakeResolver) Resolve(_ context.Context, key string) (registry.Identity, bool) {
	id, ok := f[key]
	return id, ok
}

func TestIdentify(t *testing.T) {
	cfg := config.Load()
	cfg.RateLimiter.MaxLimit = 100
	l := &Limiter{
		cfg:      cfg,
		trust:    mustTrust(t, ""),
		resolver: fakeResolver{"registered": {ID: "tenant:acme", Limit: 5000}},
	}

	tests := []struct {
		name    string
		headers map[string]string
		want    registry.Identity
	}{
		{"registered key uses its tenant and plan limit", map[string]string{"X-API-Key": "registered"}, registry.Identity{ID: "tenant:acme", Limit: 5000}},
		{"registered bearer token too", map[string]string{"Authorization": "Bearer registered"}, registry.Identity{ID: "tenant:acme", Limit: 5000}},
		// The rotation bypass: a made-up key must not become its own identity.
		{"unregistered key falls back to IP", map[string]string{"X-API-Key": "made-up"}, registry.Identity{ID: "ip:203.0.113.9", Limit: 100}},
		{"no key uses IP", nil, registry.Identity{ID: "ip:203.0.113.9", Limit: 100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.identify(request("203.0.113.9:4000", tt.headers)); got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
