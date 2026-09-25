package middleware

import (
	"net/http"

	"github.com/manan/distributed-rate-limiter/go/internal/redis"
	"github.com/manan/distributed-rate-limiter/go/internal/registry"
)

// TokenBucket allows bursts up to capacity, then admits at the refill rate.
// It resolves the same identity as the sliding window but keeps a global
// capacity: buckets protect backend capacity, not a billing plan.
func (l *Limiter) TokenBucket() func(http.Handler) http.Handler {
	return l.strictHandler("TokenBucket", fixedLimit(l.cfg.TokenBucket.Capacity),
		func(r *http.Request, id registry.Identity) redis.Decision {
			return l.client.CheckTokenBucket(r.Context(), id.ID)
		})
}
