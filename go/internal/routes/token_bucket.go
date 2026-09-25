package routes

import "net/http"

func registerTokenBucket(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /api/token-bucket/resource",
		d.Limiter.TokenBucket()(message("Token bucket resource")))
}
