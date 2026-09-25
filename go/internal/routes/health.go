package routes

import "net/http"

// registerHealth exposes the two unlimited introspection endpoints.
func registerHealth(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /api/open/health", func(w http.ResponseWriter, r *http.Request) {
		status := "disconnected"
		if d.Client.IsHealthy() {
			status = "connected"
		}
		writeJSON(w, map[string]any{"status": "ok", "redis": status})
	})

	mux.HandleFunc("GET /api/open/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, d.Buffer.Stats())
	})
}
