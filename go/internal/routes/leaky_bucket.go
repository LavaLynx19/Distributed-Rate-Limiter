package routes

import "net/http"

func registerLeakyBucket(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /api/leaky-bucket/resource",
		d.Limiter.LeakyBucket()(message("Leaky bucket resource")))
}
