package main

import (
	"net/http"
	"strconv"
)

// Bannissements d'adresses survenus depuis `since` (secondes Unix) : l'interface les interroge et ouvre une alerte dans chaque
// session connectée. `now` est l'horloge du serveur, à renvoyer comme `since` au prochain appel.
func (a *authAPI) alerts(w http.ResponseWriter, r *http.Request) {
	if a.need(w, r, true) == nil {
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	rows, err := a.s.db.Query(`SELECT at, ip FROM auth_events WHERE kind='lock' AND outcome='ban' AND at > ? ORDER BY at LIMIT 20`, since)
	if err != nil {
		authInternal(w, err)
		return
	}
	defer rows.Close()
	bans := []map[string]any{}
	for rows.Next() {
		var at int64
		var ip string
		if err := rows.Scan(&at, &ip); err != nil {
			authInternal(w, err)
			return
		}
		bans = append(bans, map[string]any{"at": at, "ip": ip})
	}
	if err := rows.Err(); err != nil {
		authInternal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "now": authNow().Unix(), "bans": bans})
}
