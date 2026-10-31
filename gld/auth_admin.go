package main

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const (
	eventsDefault = 100
	eventsMax     = 500
)

type adminAPI struct {
	*authAPI
	// mu sérialise les désactivations : deux admins qui se désactivent l'un l'autre ne doivent pas laisser zéro admin actif.
	mu sync.Mutex
}

// adminReq : la réauthentification voyage à part, pour ne pas se confondre avec le mot de passe donné à un compte.
type adminReq struct {
	Reauth reauthReq `json:"reauth"`
}

func routesAdmin(mux *http.ServeMux, authHandlers *authAPI) {
	adminHandlers := &adminAPI{authAPI: authHandlers}
	mux.HandleFunc("GET /v1/admin/users", adminHandlers.users)
	mux.HandleFunc("POST /v1/admin/users", adminHandlers.userAdd)
	mux.HandleFunc("PATCH /v1/admin/users/{user}", adminHandlers.userDisable)
	mux.HandleFunc("POST /v1/admin/users/{user}/mfa-resets", adminHandlers.userResetMFA)
	mux.HandleFunc("PUT /v1/admin/users/{user}/password", adminHandlers.userPassword)
	mux.HandleFunc("PUT /v1/admin/acl", adminHandlers.acl)
	mux.HandleFunc("GET /v1/admin/events", adminHandlers.events)
	mux.HandleFunc("GET /v1/admin/homes", adminHandlers.homes)
	mux.HandleFunc("GET /v1/admin/sessions", adminHandlers.sessionsList)
	mux.HandleFunc("POST /v1/admin/sessions/revocations", adminHandlers.sessionsRevoke)
}

// userParam lit le segment {user} (identifiant numérique du compte) ; false après avoir répondu 400.
func userParam(responseWriter http.ResponseWriter, request *http.Request) (int64, bool) {
	userID, err := strconv.ParseInt(request.PathValue("user"), 10, 64)
	if err != nil || userID < 1 {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
		return 0, false
	}
	return userID, true
}

// admin : session complète, CSRF, compte admin vérifié. Le garde l'exige déjà ; le refaire ici couvre une base sans compte
// (garde non monté) et tout montage futur hors garde.
func (adminHandlers *adminAPI) admin(responseWriter http.ResponseWriter, request *http.Request) *Principal {
	principal := adminHandlers.need(responseWriter, request, true)
	if principal == nil {
		return nil
	}
	if !principal.User.Admin {
		adminHandlers.deniedLog(principal.User.ID, clientIP(request), "admin")
		authMsg(responseWriter, http.StatusForbidden, L("srv.auth.droitRefuse"))
		return nil
	}
	return principal
}

// fresh exige mot de passe plus code, ou mot de passe plus second facteur validé sur cette session depuis moins de
// reauthFor. Sans l'un ou l'autre, refus sans rien vérifier : aucun essai de mot de passe n'est consommé.
func (adminHandlers *adminAPI) fresh(responseWriter http.ResponseWriter, request *http.Request, principal *Principal, reauthBody reauthReq) bool {
	if reauthBody.Password == "" || (reauthBody.Code == "" && !adminHandlers.reauthFresh(principal.SessionID)) {
		authMsg(responseWriter, http.StatusForbidden, L("srv.admin.reauthRequise"))
		return false
	}
	return adminHandlers.reauth(responseWriter, request, principal, reauthBody)
}

func (adminHandlers *adminAPI) audit(principal *Principal, request *http.Request, kind, outcome string) {
	adminHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), kind, outcome)
}

func rightNames(grantedRights map[Right]bool) []string {
	names := []string{}
	for _, right := range allRights {
		if grantedRights[right] {
			names = append(names, string(right))
		}
	}
	return names
}

func homeNames() map[string]string {
	namesByID := map[string]string{mainHomeID: mainHomeID}
	for _, home := range loadHomes() {
		namesByID[home.ID] = home.Name
	}
	return namesByID
}

// homes : tous les homes, quels que soient les droits de l'admin, pour la matrice d'ACL ; noms et identifiants seulement.
func (adminHandlers *adminAPI) homes(responseWriter http.ResponseWriter, request *http.Request) {
	if adminHandlers.admin(responseWriter, request) == nil {
		return
	}
	homeList := []map[string]string{{"id": mainHomeID, "name": mainHomeID}}
	for _, home := range loadHomes() {
		homeList = append(homeList, map[string]string{"id": home.ID, "name": home.Name})
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"homes": homeList})
}

func (adminHandlers *adminAPI) users(responseWriter http.ResponseWriter, request *http.Request) {
	if adminHandlers.admin(responseWriter, request) == nil {
		return
	}
	users, tamperedByID, err := adminHandlers.s.Users()
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	names := homeNames()
	userRows := []map[string]any{}
	for _, user := range users {
		tampered := tamperedByID[user.ID]
		factors, rowErr := adminHandlers.s.factors(user.ID)
		if errors.Is(rowErr, errTamper) {
			tampered = true
		} else if rowErr != nil {
			authInternal(responseWriter, rowErr)
			return
		}
		var sessionCount int
		if countErr := adminHandlers.s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=? AND expires_at>?`, user.ID, authNow().Unix()).Scan(&sessionCount); countErr != nil {
			authInternal(responseWriter, countErr)
			return
		}
		acl, aclBad, rowErr := adminHandlers.userACL(user.ID, names)
		if rowErr != nil {
			authInternal(responseWriter, rowErr)
			return
		}
		userRows = append(userRows, map[string]any{"id": user.ID, "name": user.Name, "admin": user.Admin, "disabled": user.Disabled, "created": user.CreatedAt,
			"factors": factors, "sessions": sessionCount, "acl": acl, "tampered": tampered || aclBad})
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"users": userRows})
}

// userACL lit chaque ligne par Rights (signature vérifiée) ; une ligne altérée est montrée sans droits, marquée.
func (adminHandlers *adminAPI) userACL(userID int64, names map[string]string) ([]map[string]any, bool, error) {
	rows, err := adminHandlers.s.db.Query(`SELECT home_id FROM home_acl WHERE user_id=? ORDER BY home_id`, userID)
	if err != nil {
		return nil, false, err
	}
	var homeIDs []string
	for rows.Next() {
		var homeID string
		if scanErr := rows.Scan(&homeID); scanErr != nil {
			rows.Close()
			return nil, false, scanErr
		}
		homeIDs = append(homeIDs, homeID)
	}
	rows.Close()
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, false, rowsErr
	}
	aclRows, anyBad := []map[string]any{}, false
	for _, homeID := range homeIDs {
		grantedRights, rightsLoadErr := adminHandlers.s.Rights(userID, homeID)
		row := map[string]any{"home_id": homeID, "home_name": names[homeID]}
		switch {
		case errors.Is(rightsLoadErr, errTamper):
			row["rights"], row["tampered"], anyBad = []string{}, true, true
		case rightsLoadErr != nil:
			return nil, false, rightsLoadErr
		default:
			row["rights"] = rightNames(grantedRights)
		}
		aclRows = append(aclRows, row)
	}
	return aclRows, anyBad, nil
}

// userAdd : le mot de passe donné par l'admin est provisoire ; rien n'oblige encore à le changer (à la charge de l'utilisateur).
func (adminHandlers *adminAPI) userAdd(responseWriter http.ResponseWriter, request *http.Request) {
	principal := adminHandlers.admin(responseWriter, request)
	var requestBody struct {
		adminReq
		Name     string `json:"name"`
		Password string `json:"password"`
		Admin    bool   `json:"admin"`
	}
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	if _, err := cleanName(requestBody.Name); err != nil {
		authUserErr(responseWriter, http.StatusBadRequest, err)
		return
	}
	if err := validPassword(requestBody.Password); err != nil {
		authMsg(responseWriter, http.StatusBadRequest, pwError(err).Error())
		return
	}
	if !adminHandlers.fresh(responseWriter, request, principal, requestBody.Reauth) {
		return
	}
	userID, err := adminHandlers.s.AddUser(requestBody.Name, requestBody.Password, requestBody.Admin)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errNameTaken) {
			code = http.StatusConflict
		}
		authUserErr(responseWriter, code, pwError(err))
		return
	}
	adminHandlers.audit(principal, request, "admin_user", fmt.Sprintf("add id=%d name=%s admin=%t", userID, normName(requestBody.Name), requestBody.Admin))
	writeJSON(responseWriter, http.StatusOK, map[string]any{"id": userID})
}

// lastAdmin : target est le seul admin actif à signature valide. Ne journalise pas les lignes altérées : appelé à chaque
// désactivation, il remplirait auth_events ; GET /v1/admin/users les signale.
func (store *authStore) lastAdmin(targetID int64) (bool, error) {
	rows, err := store.db.Query(`SELECT `+userCols+` FROM users WHERE id<>? AND is_admin=1 AND disabled=0`, targetID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		user, scanErr := store.scanUser(rows)
		if scanErr == nil && user.Admin && !user.Disabled {
			return false, nil
		}
		if scanErr != nil && !errors.Is(scanErr, errTamper) {
			return false, scanErr
		}
	}
	return true, rows.Err()
}

// adminTarget : une cible admin, ou dont la ligne est altérée (statut douteux), exige une réauth fraîche, pour qu'un
// cookie volé ne suffise pas à écarter les autres admins.
func (adminHandlers *adminAPI) adminTarget(responseWriter http.ResponseWriter, userID int64) (isAdmin, canProceed bool) {
	user, err := adminHandlers.s.userByID(adminHandlers.s.db, userID)
	switch {
	case errors.Is(err, errTamper):
		return true, true
	case err != nil:
		adminHandlers.userErr(responseWriter, err)
		return false, false
	}
	return user.Admin, true
}

// userDisable : désactiver un compte ordinaire est une mesure d'urgence, sans réauthentification ; réactiver rend un accès
// et désactiver un admin écarte un pair : les deux l'exigent.
func (adminHandlers *adminAPI) userDisable(responseWriter http.ResponseWriter, request *http.Request) {
	principal := adminHandlers.admin(responseWriter, request)
	var requestBody struct {
		adminReq
		Disabled bool `json:"disabled"`
	}
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	userID, canProceed := userParam(responseWriter, request)
	if !canProceed {
		return
	}
	if userID == principal.User.ID {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.admin.soiMeme"))
		return
	}
	targetIsAdmin, canProceed := adminHandlers.adminTarget(responseWriter, userID)
	if !canProceed || ((!requestBody.Disabled || targetIsAdmin) && !adminHandlers.fresh(responseWriter, request, principal, requestBody.Reauth)) {
		return
	}
	adminHandlers.mu.Lock()
	defer adminHandlers.mu.Unlock()
	if requestBody.Disabled {
		isLastAdmin, err := adminHandlers.s.lastAdmin(userID)
		if err != nil {
			authInternal(responseWriter, err)
			return
		}
		if isLastAdmin {
			authMsg(responseWriter, http.StatusConflict, L("srv.admin.dernierAdmin"))
			return
		}
	}
	if err := adminHandlers.s.SetDisabled(userID, requestBody.Disabled); err != nil {
		adminHandlers.userErr(responseWriter, err)
		return
	}
	adminHandlers.audit(principal, request, "admin_user", fmt.Sprintf("disabled=%t id=%d", requestBody.Disabled, userID))
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (adminHandlers *adminAPI) userErr(responseWriter http.ResponseWriter, err error) {
	code := http.StatusConflict
	if errors.Is(err, errNoUser) {
		code = http.StatusNotFound
	}
	authUserErr(responseWriter, code, err)
}

// selfOut : une action qui révoque les sessions de l'admin lui-même efface aussi son cookie.
func (adminHandlers *adminAPI) selfOut(responseWriter http.ResponseWriter, principal *Principal, userID int64) {
	if userID == principal.User.ID {
		adminHandlers.forgetReauth(principal.SessionID)
		clearSessionCookie(responseWriter)
	}
}

func (adminHandlers *adminAPI) userResetMFA(responseWriter http.ResponseWriter, request *http.Request) {
	principal := adminHandlers.admin(responseWriter, request)
	var requestBody struct {
		adminReq
	}
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	userID, canProceed := userParam(responseWriter, request)
	if !canProceed || !adminHandlers.fresh(responseWriter, request, principal, requestBody.Reauth) {
		return
	}
	if err := adminHandlers.s.ResetMFA(userID); err != nil {
		adminHandlers.userErr(responseWriter, err)
		return
	}
	adminHandlers.selfOut(responseWriter, principal, userID)
	adminHandlers.audit(principal, request, "mfa_reset", fmt.Sprintf("id=%d", userID))
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (adminHandlers *adminAPI) userPassword(responseWriter http.ResponseWriter, request *http.Request) {
	principal := adminHandlers.admin(responseWriter, request)
	var requestBody struct {
		adminReq
		Password string `json:"password"`
	}
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	userID, canProceed := userParam(responseWriter, request)
	if !canProceed {
		return
	}
	if err := validPassword(requestBody.Password); err != nil {
		authMsg(responseWriter, http.StatusBadRequest, pwError(err).Error())
		return
	}
	if !adminHandlers.fresh(responseWriter, request, principal, requestBody.Reauth) {
		return
	}
	if err := adminHandlers.s.SetPassword(userID, requestBody.Password); err != nil {
		adminHandlers.userErr(responseWriter, err)
		return
	}
	adminHandlers.selfOut(responseWriter, principal, userID)
	adminHandlers.audit(principal, request, "admin_user", fmt.Sprintf("password id=%d", userID))
	responseWriter.WriteHeader(http.StatusNoContent)
}

func homeExists(homeID string) bool {
	return homeID == mainHomeID || slices.ContainsFunc(loadHomes(), func(home Home) bool { return home.ID == homeID })
}

func (adminHandlers *adminAPI) acl(responseWriter http.ResponseWriter, request *http.Request) {
	principal := adminHandlers.admin(responseWriter, request)
	var requestBody struct {
		adminReq
		UserID  int64    `json:"user_id"`
		HomeID  string   `json:"home_id"`
		Grant   []string `json:"grant"`
		Revoke  []string `json:"revoke"`
		Confirm bool     `json:"confirm"`
	}
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	grant, err := parseRights(requestBody.Grant)
	var revoke []Right
	if err == nil {
		revoke, err = parseRights(requestBody.Revoke)
	}
	if err != nil {
		authUserErr(responseWriter, http.StatusBadRequest, err)
		return
	}
	rejectBadRequest := func(key string, messageArgs ...any) {
		authMsg(responseWriter, http.StatusBadRequest, L(key, messageArgs...))
	}
	switch {
	case len(grant) == 0 && len(revoke) == 0:
		rejectBadRequest("srv.admin.droitsVides")
		return
	case slices.ContainsFunc(grant, func(grantedRight Right) bool { return slices.Contains(revoke, grantedRight) }):
		rejectBadRequest("srv.admin.droitsContradictoires")
		return
	case !homeExists(requestBody.HomeID):
		rejectBadRequest("srv.admin.homeInconnu", "A1", requestBody.HomeID)
		return
	}
	if slices.Contains(grant, RightExec) || slices.Contains(grant, RightConfigWrite) || slices.Contains(grant, RightFsWrite) {
		if !requestBody.Confirm {
			rejectBadRequest("srv.admin.confirmerExec")
			return
		}
		if !adminHandlers.fresh(responseWriter, request, principal, requestBody.Reauth) {
			return
		}
	} else if len(grant) > 0 && (slices.Contains(grant, RightSessionsRead) || slices.Contains(grant, RightFsRead) || requestBody.UserID == principal.User.ID) {
		// Lire les transcriptions d'autrui, ou s'accorder un droit : un cookie volé ne doit pas y suffire.
		if !adminHandlers.fresh(responseWriter, request, principal, requestBody.Reauth) {
			return
		}
	}
	if setRightsErr := adminHandlers.s.SetRights(requestBody.UserID, requestBody.HomeID, grant, revoke); setRightsErr != nil {
		adminHandlers.userErr(responseWriter, setRightsErr)
		return
	}
	updatedRights, err := adminHandlers.s.Rights(requestBody.UserID, requestBody.HomeID)
	if err != nil {
		adminHandlers.userErr(responseWriter, err)
		return
	}
	adminHandlers.audit(principal, request, "acl", fmt.Sprintf("user=%d home=%s grant=%s revoke=%s", requestBody.UserID, requestBody.HomeID, strings.Join(requestBody.Grant, ","), strings.Join(requestBody.Revoke, ",")))
	writeJSON(responseWriter, http.StatusOK, map[string]any{"rights": rightNames(updatedRights)})
}

// events : les plus récents d'abord ; before est l'identifiant (rowid) du dernier événement déjà vu, pour paginer sans
// perdre les événements d'une même seconde.
func (adminHandlers *adminAPI) events(responseWriter http.ResponseWriter, request *http.Request) {
	if adminHandlers.admin(responseWriter, request) == nil {
		return
	}
	query := request.URL.Query()
	limit := eventsDefault
	if limitParam := query.Get("limit"); limitParam != "" {
		parsedLimit, err := strconv.Atoi(limitParam)
		if err != nil || parsedLimit < 1 {
			authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
			return
		}
		limit = min(parsedLimit, eventsMax)
	}
	var before int64
	if beforeParam := query.Get("before"); beforeParam != "" {
		parsedBefore, err := strconv.ParseInt(beforeParam, 10, 64)
		if err != nil || parsedBefore < 1 {
			authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
			return
		}
		before = parsedBefore
	}
	kind := query.Get("kind")
	rows, err := adminHandlers.s.db.Query(`SELECT e.rowid,e.at,COALESCE(e.user_id,0),COALESCE(u.name,''),e.name_tried,e.ip,e.kind,e.outcome
		FROM auth_events e LEFT JOIN users u ON u.id=e.user_id
		WHERE (?='' OR e.kind=?) AND (?=0 OR e.rowid<?) ORDER BY e.rowid DESC LIMIT ?`, kind, kind, before, before, limit)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	defer rows.Close()
	eventRows := []map[string]any{}
	for rows.Next() {
		var eventID, at, userID int64
		var userName, nameTried, ip, eventKind, outcome string
		if scanErr := rows.Scan(&eventID, &at, &userID, &userName, &nameTried, &ip, &eventKind, &outcome); scanErr != nil {
			authInternal(responseWriter, scanErr)
			return
		}
		eventRows = append(eventRows, map[string]any{"id": eventID, "at": at, "user_id": userID, "user": userName, "name_tried": nameTried, "ip": ip, "kind": eventKind, "outcome": outcome})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		authInternal(responseWriter, rowsErr)
		return
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"events": eventRows})
}

func (adminHandlers *adminAPI) sessionsList(responseWriter http.ResponseWriter, request *http.Request) {
	if adminHandlers.admin(responseWriter, request) == nil {
		return
	}
	userID, err := strconv.ParseInt(request.URL.Query().Get("user_id"), 10, 64)
	if err != nil {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
		return
	}
	sessions, err := adminHandlers.s.userSessions(userID)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	sessionRows := []map[string]any{}
	for _, session := range sessions {
		// Un préfixe suffit à reconnaître une session ; l'identifiant entier permettrait de la révoquer ailleurs.
		sessionRows = append(sessionRows, map[string]any{"id": hex.EncodeToString(session.hash)[:8], "created": session.created, "seen": session.seen, "ip": session.ip, "ua": session.ua, "mfa": session.mfa})
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"sessions": sessionRows})
}

func (adminHandlers *adminAPI) sessionsRevoke(responseWriter http.ResponseWriter, request *http.Request) {
	principal := adminHandlers.admin(responseWriter, request)
	var requestBody struct {
		adminReq
	}
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	userID, err := strconv.ParseInt(request.URL.Query().Get("user_id"), 10, 64)
	if err != nil {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
		return
	}
	// Ses propres sessions : pas de pair à écarter.
	if userID != principal.User.ID {
		targetIsAdmin, canProceed := adminHandlers.adminTarget(responseWriter, userID)
		if !canProceed || (targetIsAdmin && !adminHandlers.fresh(responseWriter, request, principal, requestBody.Reauth)) {
			return
		}
	}
	if revokeErr := adminHandlers.s.RevokeUser(userID); revokeErr != nil {
		authInternal(responseWriter, revokeErr)
		return
	}
	adminHandlers.selfOut(responseWriter, principal, userID)
	adminHandlers.audit(principal, request, "session_revoke", fmt.Sprintf("user=%d all", userID))
	responseWriter.WriteHeader(http.StatusNoContent)
}

// DeleteHomesACL retire les droits des homes supprimés : un identifiant ne revient jamais, mais une ligne orpheline
// resterait visible et signée.
func (store *authStore) DeleteHomesACL(homeIDs []string, actorUserID int64, ip string) error {
	if len(homeIDs) == 0 {
		return nil
	}
	var removedRows int64
	err := store.inTx(func(transaction *sql.Tx) error {
		for _, homeID := range homeIDs {
			result, execErr := transaction.Exec(`DELETE FROM home_acl WHERE home_id=?`, homeID)
			if execErr != nil {
				return execErr
			}
			affectedRows, _ := result.RowsAffected()
			removedRows += affectedRows
		}
		return nil
	})
	if err == nil {
		store.eventIP(actorUserID, "acl", fmt.Sprintf("homes_removed=%s rows=%d", strings.Join(homeIDs, ","), removedRows), ip)
	}
	return err
}

func init() { onHomesRemoved = homesRemovedACL }

// homesRemovedACL : branché sur onHomesRemoved ; sans garde (mode ouvert, home enfant), aucune ligne à retirer.
func homesRemovedACL(request *http.Request, homeIDs []string) {
	gate := gateFrom(request)
	if gate == nil || gate.s == nil || gate.p == nil || gate.p.User == nil {
		return
	}
	if err := gate.s.DeleteHomesACL(homeIDs, gate.p.User.ID, clientIP(request)); err != nil {
		journal("auth: droits des homes retirés %v : %v", homeIDs, err)
	}
}
