package middleware

import (
	"net/http"
)

// Loose never touches Redis on the request path. It counts locally and lets
// the buffer sync in batches, so throughput is bounded by memory rather than
// network round trips.
//
// A request is rejected when this instance's allowance for the identifier is
// spent. The allowance is reset to the cluster-wide remaining capacity on
// every write-back, so it reflects other instances' usage as of the last
// sync; rejection itself never needs a network call (ARCHITECTURE.md
// section 6).
func (l *Limiter) Loose() func(http.Handler) http.Handler {
	segmentTTL := int64(l.cfg.RateLimiter.SegmentDuration.Seconds())

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := l.identify(r)
			v := l.buf.Admit(id.ID, id.Limit)

			if !v.Admitted {
				setBlockedHeaders(w, id.Limit, v.RetryAfter)
				writeTooManyRequests(w, v.RetryAfter)
				return
			}

			setRateLimitHeaders(w, id.Limit, v.Remaining, segmentTTL)
			next.ServeHTTP(w, r)
		})
	}
}
