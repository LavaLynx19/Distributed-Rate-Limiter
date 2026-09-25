package middleware

import (
	"net/http"

	"github.com/manan/distributed-rate-limiter/go/internal/redis"
	"github.com/manan/distributed-rate-limiter/go/internal/registry"
)

// LeakyBucket enforces a steady drain rate, smoothing bursty input. Like the
// token bucket it keys on the resolved identity with a global capacity.
func (l *Limiter) LeakyBucket() func(http.Handler) http.Handler {
	return l.strictHandler("LeakyBucket", fixedLimit(l.cfg.LeakyBucket.Capacity),
		func(r *http.Request, id registry.Identity) redis.Decision {
			return l.client.CheckLeakyBucket(r.Context(), id.ID)
		})
}
