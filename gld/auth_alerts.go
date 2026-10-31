package main

import (
	"net/http"
	"strconv"
)

// Bannissements d'adresses survenus depuis `since` (secondes Unix) : l'interface les interroge et ouvre une alerte dans chaque
// session connectée (l'adresse n'est donnée qu'aux administrateurs). `now` est l'horloge du serveur, à renvoyer comme `since` au
// prochain appel. Les jumelages, eux, sont propres au compte : une session volée qui en crée une nouvelle via POST /v1/pairing/codes le
// révèle ici, pour révocation depuis /v1/auth/sessions (device_id non journalisé : on ne pointe pas la session, on alerte).
func (authHandlers *authAPI) alerts(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, true)
	if principal == nil {
		return
	}
	// after (rowid de la dernière ligne vue) plutôt que since (secondes) : deux évènements dans la même seconde, de part et d'autre
	// d'un relevé, ne se perdent pas. after=-1 : premier appel, seul le curseur courant est rendu.
	query := request.URL.Query()
	since, _ := strconv.ParseInt(query.Get("since"), 10, 64)
	after, err := strconv.ParseInt(query.Get("after"), 10, 64)
	if err != nil {
		after = 0
	}
	var currentCursor int64 // lu avant les requêtes, qui s'y arrêtent : une ligne écrite entre-temps viendra au relevé suivant
	if cursorErr := authHandlers.s.db.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM auth_events`).Scan(&currentCursor); cursorErr != nil {
		authInternal(responseWriter, cursorErr)
		return
	}
	if after < 0 {
		writeJSON(responseWriter, http.StatusOK, map[string]any{"now": authNow().Unix(), "cursor": currentCursor, "bans": []any{}, "pairings": []any{}})
		return
	}
	bans, bansCursor, err := authEventRows(authHandlers.s, currentCursor, `SELECT rowid, at, ip FROM auth_events WHERE kind='lock' AND outcome='ban' AND at > ? AND rowid > ? AND rowid <= ? ORDER BY rowid LIMIT 20`, since, after, currentCursor)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	if !principal.User.Admin {
		for _, ban := range bans {
			ban["ip"] = "" // l'alerte vaut pour tous les comptes, l'adresse seulement pour les administrateurs
		}
	}
	pairings, pairingsCursor, err := authEventRows(authHandlers.s, currentCursor, `SELECT rowid, at, ip FROM auth_events WHERE user_id = ? AND kind='login' AND outcome='pair' AND at > ? AND rowid > ? AND rowid <= ? ORDER BY rowid LIMIT 20`, principal.User.ID, since, after, currentCursor)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"now": authNow().Unix(), "cursor": min(bansCursor, pairingsCursor), "bans": bans, "pairings": pairings})
}

// authEventRows : lignes {at, ip} d'une requête auth_events (rowid, at, ip), factorisé pour alerts. cursor : jusqu'où le prochain
// relevé peut reprendre (cur, ou la dernière ligne d'une liste coupée par LIMIT : la suite viendra au relevé suivant).
func authEventRows(store *authStore, currentCursor int64, query string, args ...any) (eventRows []map[string]any, cursor int64, err error) {
	rows, err := store.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	eventRows = []map[string]any{}
	var lastRowID int64
	for rows.Next() {
		var rowID, at int64
		var ip string
		if scanErr := rows.Scan(&rowID, &at, &ip); scanErr != nil {
			return nil, 0, scanErr
		}
		lastRowID = rowID
		eventRows = append(eventRows, map[string]any{"at": at, "ip": ip})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, 0, rowsErr
	}
	if len(eventRows) == 20 {
		return eventRows, lastRowID, nil
	}
	return eventRows, currentCursor, nil
}
