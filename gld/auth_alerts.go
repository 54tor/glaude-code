package main

import (
	"net/http"
	"strconv"
)

// Bannissements d'adresses survenus depuis `since` (secondes Unix) : l'interface les interroge et ouvre une alerte dans chaque
// session connectée (l'adresse n'est donnée qu'aux administrateurs). `now` est l'horloge du serveur, à renvoyer comme `since` au
// prochain appel. Les jumelages, eux, sont propres au compte : une session volée qui en crée une nouvelle via POST /v1/pairing/codes le
// révèle ici, pour révocation depuis /auth/sessions (device_id non journalisé : on ne pointe pas la session, on alerte).
func (a *authAPI) alerts(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, true)
	if p == nil {
		return
	}
	// after (rowid de la dernière ligne vue) plutôt que since (secondes) : deux évènements dans la même seconde, de part et d'autre
	// d'un relevé, ne se perdent pas. after=-1 : premier appel, seul le curseur courant est rendu.
	q := r.URL.Query()
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	after, err := strconv.ParseInt(q.Get("after"), 10, 64)
	if err != nil {
		after = 0
	}
	var cur int64 // lu avant les requêtes, qui s'y arrêtent : une ligne écrite entre-temps viendra au relevé suivant
	if err := a.s.db.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM auth_events`).Scan(&cur); err != nil {
		authInternal(w, err)
		return
	}
	if after < 0 {
		writeJSON(w, 200, map[string]any{"ok": true, "now": authNow().Unix(), "cursor": cur, "bans": []any{}, "pairings": []any{}})
		return
	}
	bans, bCur, err := authEventRows(a.s, cur, `SELECT rowid, at, ip FROM auth_events WHERE kind='lock' AND outcome='ban' AND at > ? AND rowid > ? AND rowid <= ? ORDER BY rowid LIMIT 20`, since, after, cur)
	if err != nil {
		authInternal(w, err)
		return
	}
	if !p.User.Admin {
		for _, b := range bans {
			b["ip"] = "" // l'alerte vaut pour tous les comptes, l'adresse seulement pour les administrateurs
		}
	}
	pairings, pCur, err := authEventRows(a.s, cur, `SELECT rowid, at, ip FROM auth_events WHERE user_id = ? AND kind='login' AND outcome='pair' AND at > ? AND rowid > ? AND rowid <= ? ORDER BY rowid LIMIT 20`, p.User.ID, since, after, cur)
	if err != nil {
		authInternal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "now": authNow().Unix(), "cursor": min(bCur, pCur), "bans": bans, "pairings": pairings})
}

// authEventRows : lignes {at, ip} d'une requête auth_events (rowid, at, ip), factorisé pour alerts. cursor : jusqu'où le prochain
// relevé peut reprendre (cur, ou la dernière ligne d'une liste coupée par LIMIT : la suite viendra au relevé suivant).
func authEventRows(s *authStore, cur int64, query string, args ...any) (out []map[string]any, cursor int64, err error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out = []map[string]any{}
	var last int64
	for rows.Next() {
		var id, at int64
		var ip string
		if err := rows.Scan(&id, &at, &ip); err != nil {
			return nil, 0, err
		}
		last = id
		out = append(out, map[string]any{"at": at, "ip": ip})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 20 {
		return out, last, nil
	}
	return out, cur, nil
}
