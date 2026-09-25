package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// setRateLimitHeaders advertises remaining quota on an allowed request.
// limit varies by algorithm: the window max for sliding window, capacity for
// the buckets.
func setRateLimitHeaders(w http.ResponseWriter, limit, remaining, resetTTL int64) {
	resetAt := time.Now().Unix() + resetTTL
	h := w.Header()
	h.Set("X-RateLimit-Limit", strconv.FormatInt(limit, 10))
	h.Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
}

func setBlockedHeaders(w http.ResponseWriter, limit, resetTTL int64) {
	resetAt := time.Now().Unix() + resetTTL
	h := w.Header()
	h.Set("X-RateLimit-Limit", strconv.FormatInt(limit, 10))
	h.Set("X-RateLimit-Remaining", "0")
	h.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
	h.Set("Retry-After", strconv.FormatInt(resetTTL, 10))
}

// writeTooManyRequests emits the 429 body. Headers must already be set —
// WriteHeader freezes them.
func writeTooManyRequests(w http.ResponseWriter, retryAfter int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":      "Too Many Requests",
		"retryAfter": retryAfter,
	})
}
