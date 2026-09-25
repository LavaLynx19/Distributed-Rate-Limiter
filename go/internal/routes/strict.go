package routes

import "net/http"

func registerStrict(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /api/strict/resource",
		d.Limiter.Strict()(message("Strict-mode resource")))
}
