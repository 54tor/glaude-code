package main

import (
	"encoding/json"
	"io"
	"net/http"
)

// routesTabs : sessions ouvertes sauvegardées.
func routesTabs(mux *http.ServeMux) {
	mux.HandleFunc("GET /tabs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"saves": loadTabs()})
	})
	mux.HandleFunc("POST /tabs-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		s, _, err := saveTabs(req.Name, false)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_tabs.sessionSSauvegardeeS", "A1", len(s.Sessions))})
	})
	mux.HandleFunc("POST /tabs-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ At string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		if err := removeTabs(req.At); err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_tabs.sauvegardeSupprimee")})
	})
	// Fusion de fichiers de mémoire par Claude : le prompt est construit ici, à partir des seules clés validées ;
	// launch l'ouvre dans une nouvelle session (gld-matin --prompt), sinon il est seulement renvoyé pour être copié.
}
