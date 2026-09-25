package routes

import "net/http"

func registerLoose(mux *http.ServeMux, d Deps) {
	loose := d.Limiter.Loose()
	mux.Handle("GET /api/loose/resource", loose(message("Loose-mode resource")))
	mux.Handle("GET /api/loose/burst", loose(message("Loose-mode burst endpoint")))
}
