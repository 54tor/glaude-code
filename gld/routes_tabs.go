package main

import "net/http"

// routesTabs : sessions ouvertes sauvegardées.
func routesTabs(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tabs", func(responseWriter http.ResponseWriter, request *http.Request) {
		saves, err := loadTabs()
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"saves": saves})
	})
	mux.HandleFunc("PUT /v1/tabs", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Name string }
		if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		save, _, err := saveTabs(body.Name, false)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"n": len(save.Sessions), "message": L("srv.routes_tabs.sessionSSauvegardeeS", "A1", len(save.Sessions))})
	})
	mux.HandleFunc("DELETE /v1/tabs/{tab}", func(responseWriter http.ResponseWriter, request *http.Request) {
		if err := removeTabs(request.PathValue("tab")); err != nil {
			writeErr(responseWriter, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_tabs.sauvegardeSupprimee")})
	})
	// Fusion de fichiers de mémoire par Claude : le prompt est construit ici, à partir des seules clés validées ;
	// launch l'ouvre dans une nouvelle session (gld-matin --prompt), sinon il est seulement renvoyé pour être copié.
}
