package middleware

import (
	"net/http"

	"github.com/manan/distributed-rate-limiter/go/internal/redis"
	"github.com/manan/distributed-rate-limiter/go/internal/registry"
)

// Strict checks the sliding window synchronously on every request, at the
// identity's plan limit. Used for billing and monetization, where
// over-admission is not acceptable.
func (l *Limiter) Strict() func(http.Handler) http.Handler {
	return l.strictHandler("Strict", identityLimit,
		func(r *http.Request, id registry.Identity) redis.Decision {
			return l.client.CheckSlidingWindow(r.Context(), id.ID, id.Limit, 1)
		})
}
