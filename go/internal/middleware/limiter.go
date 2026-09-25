package middleware

import (
	"context"
	"log"
	"net/http"

	"github.com/manan/distributed-rate-limiter/go/internal/buffer"
	"github.com/manan/distributed-rate-limiter/go/internal/config"
	"github.com/manan/distributed-rate-limiter/go/internal/redis"
	"github.com/manan/distributed-rate-limiter/go/internal/registry"
)

// resolver is the slice of the key registry the middleware needs.
type resolver interface {
	Resolve(ctx context.Context, key string) (registry.Identity, bool)
}

// Limiter carries the dependencies every middleware needs. Constructing it
// once and hanging the middlewares off it keeps the wiring explicit.
type Limiter struct {
	cfg      config.Config
	client   *redis.Client
	buf      *buffer.Buffer
	trust    ProxyTrust
	resolver resolver
}

func NewLimiter(cfg config.Config, client *redis.Client, buf *buffer.Buffer) (*Limiter, error) {
	trust, err := ParseProxyTrust(cfg.Server.TrustProxy)
	if err != nil {
		return nil, err
	}
	plans, err := registry.ParsePlans(cfg.Registry.PlanLimits)
	if err != nil {
		return nil, err
	}
	res := registry.NewResolver(client, plans, cfg.Registry.CacheTTL, cfg.Registry.CacheSize)
	return &Limiter{cfg: cfg, client: client, buf: buf, trust: trust, resolver: res}, nil
}

// identify resolves who a request is limited as. A registered key maps to its
// key or tenant identity and plan limit; anything else — no key, an unknown
// or malformed key, or a failed lookup — is limited by client IP at the
// anonymous limit, so rotating made-up keys gains nothing.
func (l *Limiter) identify(r *http.Request) registry.Identity {
	if key := PresentedKey(r); key != "" {
		if id, ok := l.resolver.Resolve(r.Context(), key); ok {
			return id
		}
	}
	return registry.Identity{ID: "ip:" + l.trust.ClientIP(r), Limit: l.cfg.RateLimiter.MaxLimit}
}

// checkFunc is one algorithm's synchronous verdict for an identity.
type checkFunc func(r *http.Request, id registry.Identity) redis.Decision

// strictHandler is the shape shared by all three synchronous algorithms: ask
// Redis, then allow, fail open, or reject. limitFor gives the limit reported
// in headers: the identity's plan limit for the sliding window, the global
// capacity for the buckets.
func (l *Limiter) strictHandler(label string, limitFor func(registry.Identity) int64, check checkFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := l.identify(r)
			result := check(r, id)

			if result.FailedOpen {
				log.Printf("[%s] Fail-open for: %s", label, id.ID)
				next.ServeHTTP(w, r)
				return
			}

			limit := limitFor(id)
			if result.Allowed {
				setRateLimitHeaders(w, limit, result.Remaining, result.ResetTTL)
				next.ServeHTTP(w, r)
				return
			}

			setBlockedHeaders(w, limit, result.ResetTTL)
			writeTooManyRequests(w, result.ResetTTL)
		})
	}
}

func identityLimit(id registry.Identity) int64 { return id.Limit }

func fixedLimit(limit int64) func(registry.Identity) int64 {
	return func(registry.Identity) int64 { return limit }
}
