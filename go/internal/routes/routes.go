// Package routes wires the demo endpoints onto a ServeMux. Handlers are
// deliberately trivial — the interesting behaviour is in the middleware.
package routes

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/manan/distributed-rate-limiter/go/internal/buffer"
	"github.com/manan/distributed-rate-limiter/go/internal/middleware"
	"github.com/manan/distributed-rate-limiter/go/internal/redis"
)

type Deps struct {
	Limiter *middleware.Limiter
	Client  *redis.Client
	Buffer  *buffer.Buffer
}

func Register(mux *http.ServeMux, d Deps) {
	registerStrict(mux, d)
	registerLoose(mux, d)
	registerTokenBucket(mux, d)
	registerLeakyBucket(mux, d)
	registerHealth(mux, d)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// message is the standard demo-resource body, matching the Node routes.
func message(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"message":   text,
			"timestamp": time.Now().UnixMilli(),
		})
	}
}
