package main

import "net/http"

// routesTabs : sessions ouvertes sauvegardées.
func routesTabs(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tabs", func(w http.ResponseWriter, r *http.Request) {
		saves, err := loadTabs()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"saves": saves})
	})
	mux.HandleFunc("PUT /v1/tabs", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		if !decodeBody(w, r, &req, 1<<12) {
			return
		}
		s, _, err := saveTabs(req.Name, false)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"n": len(s.Sessions), "message": L("srv.routes_tabs.sessionSSauvegardeeS", "A1", len(s.Sessions))})
	})
	mux.HandleFunc("DELETE /v1/tabs/{tab}", func(w http.ResponseWriter, r *http.Request) {
		if err := removeTabs(r.PathValue("tab")); err != nil {
			writeErr(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_tabs.sauvegardeSupprimee")})
	})
	// Fusion de fichiers de mémoire par Claude : le prompt est construit ici, à partir des seules clés validées ;
	// launch l'ouvre dans une nouvelle session (gld-matin --prompt), sinon il est seulement renvoyé pour être copié.
}
