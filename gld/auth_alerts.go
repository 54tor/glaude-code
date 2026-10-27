package main

import (
	"net/http"
	"strconv"
)

// Bannissements d'adresses survenus depuis `since` (secondes Unix) : l'interface les interroge et ouvre une alerte dans chaque
// session connectée (l'adresse n'est donnée qu'aux administrateurs). `now` est l'horloge du serveur, à renvoyer comme `since` au
// prochain appel. Les jumelages, eux, sont propres au compte : une session volée qui en crée une nouvelle via /pair-code le
// révèle ici, pour révocation depuis /auth/sessions (device_id non journalisé : on ne pointe pas la session, on alerte).
func (a *authAPI) alerts(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, true)
	if p == nil {
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	bans, err := authEventRows(a.s, `SELECT at, ip FROM auth_events WHERE kind='lock' AND outcome='ban' AND at > ? ORDER BY at LIMIT 20`, since)
	if err != nil {
		authInternal(w, err)
		return
	}
	if !p.User.Admin {
		for _, b := range bans {
			b["ip"] = "" // l'alerte vaut pour tous les comptes, l'adresse seulement pour les administrateurs
		}
	}
	pairings, err := authEventRows(a.s, `SELECT at, ip FROM auth_events WHERE user_id = ? AND kind='login' AND outcome='pair' AND at > ? ORDER BY at LIMIT 20`, p.User.ID, since)
	if err != nil {
		authInternal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "now": authNow().Unix(), "bans": bans, "pairings": pairings})
}

// authEventRows : lignes {at, ip} d'une requête auth_events, factorisé pour alerts (bannissements et jumelages).
func authEventRows(s *authStore, query string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var at int64
		var ip string
		if err := rows.Scan(&at, &ip); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"at": at, "ip": ip})
	}
	return out, rows.Err()
}
