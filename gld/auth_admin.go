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

func routesAdmin(mux *http.ServeMux, a *authAPI) {
	d := &adminAPI{authAPI: a}
	mux.HandleFunc("GET /v1/admin/users", d.users)
	mux.HandleFunc("POST /v1/admin/users", d.userAdd)
	mux.HandleFunc("PATCH /v1/admin/users/{user}", d.userDisable)
	mux.HandleFunc("DELETE /v1/admin/users/{user}/mfa", d.userResetMFA)
	mux.HandleFunc("PUT /v1/admin/users/{user}/password", d.userPassword)
	mux.HandleFunc("PUT /v1/admin/acl", d.acl)
	mux.HandleFunc("GET /v1/admin/events", d.events)
	mux.HandleFunc("GET /v1/admin/homes", d.homes)
	mux.HandleFunc("GET /v1/admin/sessions", d.sessionsList)
	mux.HandleFunc("DELETE /v1/admin/sessions", d.sessionsRevoke)
}

// userParam lit le segment {user} (identifiant numérique du compte) ; false après avoir répondu 400.
func userParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil || id < 1 {
		authMsg(w, 400, L("srv.http_util.requeteInvalide"))
		return 0, false
	}
	return id, true
}

// admin : session complète, CSRF, compte admin vérifié. Le garde l'exige déjà ; le refaire ici couvre une base sans compte
// (garde non monté) et tout montage futur hors garde.
func (d *adminAPI) admin(w http.ResponseWriter, r *http.Request) *Principal {
	p := d.need(w, r, true)
	if p == nil {
		return nil
	}
	if !p.User.Admin {
		d.deniedLog(p.User.ID, clientIP(r), "admin")
		authMsg(w, 403, L("srv.auth.droitRefuse"))
		return nil
	}
	return p
}

// fresh exige mot de passe plus code, ou mot de passe plus second facteur validé sur cette session depuis moins de
// reauthFor. Sans l'un ou l'autre, refus sans rien vérifier : aucun essai de mot de passe n'est consommé.
func (d *adminAPI) fresh(w http.ResponseWriter, r *http.Request, p *Principal, q reauthReq) bool {
	if q.Password == "" || (q.Code == "" && !d.reauthFresh(p.SessionID)) {
		authMsg(w, 403, L("srv.admin.reauthRequise"))
		return false
	}
	return d.reauth(w, r, p, q)
}

func (d *adminAPI) audit(p *Principal, r *http.Request, kind, outcome string) {
	d.log(p.User.ID, p.User.Name, clientIP(r), kind, outcome)
}

func rightNames(m map[Right]bool) []string {
	out := []string{}
	for _, r := range allRights {
		if m[r] {
			out = append(out, string(r))
		}
	}
	return out
}

func homeNames() map[string]string {
	n := map[string]string{mainHomeID: mainHomeID}
	for _, h := range loadHomes() {
		n[h.ID] = h.Name
	}
	return n
}

// homes : tous les homes, quels que soient les droits de l'admin, pour la matrice d'ACL ; noms et identifiants seulement.
func (d *adminAPI) homes(w http.ResponseWriter, r *http.Request) {
	if d.admin(w, r) == nil {
		return
	}
	out := []map[string]string{{"id": mainHomeID, "name": mainHomeID}}
	for _, h := range loadHomes() {
		out = append(out, map[string]string{"id": h.ID, "name": h.Name})
	}
	writeJSON(w, 200, map[string]any{"homes": out})
}

func (d *adminAPI) users(w http.ResponseWriter, r *http.Request) {
	if d.admin(w, r) == nil {
		return
	}
	users, bad, err := d.s.Users()
	if err != nil {
		authInternal(w, err)
		return
	}
	names := homeNames()
	out := []map[string]any{}
	for _, u := range users {
		tampered := bad[u.ID]
		f, err := d.s.factors(u.ID)
		if errors.Is(err, errTamper) {
			tampered = true
		} else if err != nil {
			authInternal(w, err)
			return
		}
		var n int
		if err := d.s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=? AND expires_at>?`, u.ID, authNow().Unix()).Scan(&n); err != nil {
			authInternal(w, err)
			return
		}
		acl, aclBad, err := d.userACL(u.ID, names)
		if err != nil {
			authInternal(w, err)
			return
		}
		out = append(out, map[string]any{"id": u.ID, "name": u.Name, "admin": u.Admin, "disabled": u.Disabled, "created": u.CreatedAt,
			"factors": f, "sessions": n, "acl": acl, "tampered": tampered || aclBad})
	}
	writeJSON(w, 200, map[string]any{"users": out})
}

// userACL lit chaque ligne par Rights (signature vérifiée) ; une ligne altérée est montrée sans droits, marquée.
func (d *adminAPI) userACL(uid int64, names map[string]string) ([]map[string]any, bool, error) {
	rows, err := d.s.db.Query(`SELECT home_id FROM home_acl WHERE user_id=? ORDER BY home_id`, uid)
	if err != nil {
		return nil, false, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	out, anyBad := []map[string]any{}, false
	for _, id := range ids {
		m, err := d.s.Rights(uid, id)
		row := map[string]any{"home_id": id, "home_name": names[id]}
		switch {
		case errors.Is(err, errTamper):
			row["rights"], row["tampered"], anyBad = []string{}, true, true
		case err != nil:
			return nil, false, err
		default:
			row["rights"] = rightNames(m)
		}
		out = append(out, row)
	}
	return out, anyBad, nil
}

// userAdd : le mot de passe donné par l'admin est provisoire ; rien n'oblige encore à le changer (à la charge de l'utilisateur).
func (d *adminAPI) userAdd(w http.ResponseWriter, r *http.Request) {
	p := d.admin(w, r)
	var req struct {
		adminReq
		Name     string `json:"name"`
		Password string `json:"password"`
		Admin    bool   `json:"admin"`
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	if _, err := cleanName(req.Name); err != nil {
		authUserErr(w, 400, err)
		return
	}
	if err := validPassword(req.Password); err != nil {
		authMsg(w, 400, pwError(err).Error())
		return
	}
	if !d.fresh(w, r, p, req.Reauth) {
		return
	}
	id, err := d.s.AddUser(req.Name, req.Password, req.Admin)
	if err != nil {
		code := 400
		if errors.Is(err, errNameTaken) {
			code = 409
		}
		authUserErr(w, code, pwError(err))
		return
	}
	d.audit(p, r, "admin_user", fmt.Sprintf("add id=%d name=%s admin=%t", id, normName(req.Name), req.Admin))
	writeJSON(w, 200, map[string]any{"id": id})
}

// lastAdmin : target est le seul admin actif à signature valide. Ne journalise pas les lignes altérées : appelé à chaque
// désactivation, il remplirait auth_events ; GET /v1/admin/users les signale.
func (s *authStore) lastAdmin(target int64) (bool, error) {
	rows, err := s.db.Query(`SELECT `+userCols+` FROM users WHERE id<>? AND is_admin=1 AND disabled=0`, target)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err == nil && u.Admin && !u.Disabled {
			return false, nil
		}
		if err != nil && !errors.Is(err, errTamper) {
			return false, err
		}
	}
	return true, rows.Err()
}

// adminTarget : une cible admin, ou dont la ligne est altérée (statut douteux), exige une réauth fraîche, pour qu'un
// cookie volé ne suffise pas à écarter les autres admins.
func (d *adminAPI) adminTarget(w http.ResponseWriter, id int64) (admin, ok bool) {
	u, err := d.s.userByID(d.s.db, id)
	switch {
	case errors.Is(err, errTamper):
		return true, true
	case err != nil:
		d.userErr(w, err)
		return false, false
	}
	return u.Admin, true
}

// userDisable : désactiver un compte ordinaire est une mesure d'urgence, sans réauthentification ; réactiver rend un accès
// et désactiver un admin écarte un pair : les deux l'exigent.
func (d *adminAPI) userDisable(w http.ResponseWriter, r *http.Request) {
	p := d.admin(w, r)
	var req struct {
		adminReq
		Disabled bool `json:"disabled"`
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	id, ok := userParam(w, r)
	if !ok {
		return
	}
	if id == p.User.ID {
		authMsg(w, 400, L("srv.admin.soiMeme"))
		return
	}
	admin, ok := d.adminTarget(w, id)
	if !ok || ((!req.Disabled || admin) && !d.fresh(w, r, p, req.Reauth)) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if req.Disabled {
		last, err := d.s.lastAdmin(id)
		if err != nil {
			authInternal(w, err)
			return
		}
		if last {
			authMsg(w, 409, L("srv.admin.dernierAdmin"))
			return
		}
	}
	if err := d.s.SetDisabled(id, req.Disabled); err != nil {
		d.userErr(w, err)
		return
	}
	d.audit(p, r, "admin_user", fmt.Sprintf("disabled=%t id=%d", req.Disabled, id))
	w.WriteHeader(http.StatusNoContent)
}

func (d *adminAPI) userErr(w http.ResponseWriter, err error) {
	code := 409
	if errors.Is(err, errNoUser) {
		code = 404
	}
	authUserErr(w, code, err)
}

// selfOut : une action qui révoque les sessions de l'admin lui-même efface aussi son cookie.
func (d *adminAPI) selfOut(w http.ResponseWriter, p *Principal, id int64) {
	if id == p.User.ID {
		d.forgetReauth(p.SessionID)
		clearSessionCookie(w)
	}
}

func (d *adminAPI) userResetMFA(w http.ResponseWriter, r *http.Request) {
	p := d.admin(w, r)
	var req struct {
		adminReq
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	id, ok := userParam(w, r)
	if !ok || !d.fresh(w, r, p, req.Reauth) {
		return
	}
	if err := d.s.ResetMFA(id); err != nil {
		d.userErr(w, err)
		return
	}
	d.selfOut(w, p, id)
	d.audit(p, r, "mfa_reset", fmt.Sprintf("id=%d", id))
	w.WriteHeader(http.StatusNoContent)
}

func (d *adminAPI) userPassword(w http.ResponseWriter, r *http.Request) {
	p := d.admin(w, r)
	var req struct {
		adminReq
		Password string `json:"password"`
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	id, ok := userParam(w, r)
	if !ok {
		return
	}
	if err := validPassword(req.Password); err != nil {
		authMsg(w, 400, pwError(err).Error())
		return
	}
	if !d.fresh(w, r, p, req.Reauth) {
		return
	}
	if err := d.s.SetPassword(id, req.Password); err != nil {
		d.userErr(w, err)
		return
	}
	d.selfOut(w, p, id)
	d.audit(p, r, "admin_user", fmt.Sprintf("password id=%d", id))
	w.WriteHeader(http.StatusNoContent)
}

func homeExists(id string) bool {
	return id == mainHomeID || slices.ContainsFunc(loadHomes(), func(h Home) bool { return h.ID == id })
}

func (d *adminAPI) acl(w http.ResponseWriter, r *http.Request) {
	p := d.admin(w, r)
	var req struct {
		adminReq
		UserID  int64    `json:"user_id"`
		HomeID  string   `json:"home_id"`
		Grant   []string `json:"grant"`
		Revoke  []string `json:"revoke"`
		Confirm bool     `json:"confirm"`
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	grant, err := parseRights(req.Grant)
	var revoke []Right
	if err == nil {
		revoke, err = parseRights(req.Revoke)
	}
	if err != nil {
		authUserErr(w, 400, err)
		return
	}
	bad := func(key string, kv ...any) {
		authMsg(w, 400, L(key, kv...))
	}
	switch {
	case len(grant) == 0 && len(revoke) == 0:
		bad("srv.admin.droitsVides")
		return
	case slices.ContainsFunc(grant, func(g Right) bool { return slices.Contains(revoke, g) }):
		bad("srv.admin.droitsContradictoires")
		return
	case !homeExists(req.HomeID):
		bad("srv.admin.homeInconnu", "A1", req.HomeID)
		return
	}
	if slices.Contains(grant, RightExec) || slices.Contains(grant, RightConfigWrite) || slices.Contains(grant, RightFsWrite) {
		if !req.Confirm {
			bad("srv.admin.confirmerExec")
			return
		}
		if !d.fresh(w, r, p, req.Reauth) {
			return
		}
	} else if len(grant) > 0 && (slices.Contains(grant, RightSessionsRead) || slices.Contains(grant, RightFsRead) || req.UserID == p.User.ID) {
		// Lire les transcriptions d'autrui, ou s'accorder un droit : un cookie volé ne doit pas y suffire.
		if !d.fresh(w, r, p, req.Reauth) {
			return
		}
	}
	if err := d.s.SetRights(req.UserID, req.HomeID, grant, revoke); err != nil {
		d.userErr(w, err)
		return
	}
	m, err := d.s.Rights(req.UserID, req.HomeID)
	if err != nil {
		d.userErr(w, err)
		return
	}
	d.audit(p, r, "acl", fmt.Sprintf("user=%d home=%s grant=%s revoke=%s", req.UserID, req.HomeID, strings.Join(req.Grant, ","), strings.Join(req.Revoke, ",")))
	writeJSON(w, 200, map[string]any{"rights": rightNames(m)})
}

// events : les plus récents d'abord ; before est l'identifiant (rowid) du dernier événement déjà vu, pour paginer sans
// perdre les événements d'une même seconde.
func (d *adminAPI) events(w http.ResponseWriter, r *http.Request) {
	if d.admin(w, r) == nil {
		return
	}
	q := r.URL.Query()
	limit := eventsDefault
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			authMsg(w, 400, L("srv.http_util.requeteInvalide"))
			return
		}
		limit = min(n, eventsMax)
	}
	var before int64
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			authMsg(w, 400, L("srv.http_util.requeteInvalide"))
			return
		}
		before = n
	}
	kind := q.Get("kind")
	rows, err := d.s.db.Query(`SELECT e.rowid,e.at,COALESCE(e.user_id,0),COALESCE(u.name,''),e.name_tried,e.ip,e.kind,e.outcome
		FROM auth_events e LEFT JOIN users u ON u.id=e.user_id
		WHERE (?='' OR e.kind=?) AND (?=0 OR e.rowid<?) ORDER BY e.rowid DESC LIMIT ?`, kind, kind, before, before, limit)
	if err != nil {
		authInternal(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, at, uid int64
		var user, tried, ip, k, outcome string
		if err := rows.Scan(&id, &at, &uid, &user, &tried, &ip, &k, &outcome); err != nil {
			authInternal(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "at": at, "user_id": uid, "user": user, "name_tried": tried, "ip": ip, "kind": k, "outcome": outcome})
	}
	if err := rows.Err(); err != nil {
		authInternal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"events": out})
}

func (d *adminAPI) sessionsList(w http.ResponseWriter, r *http.Request) {
	if d.admin(w, r) == nil {
		return
	}
	uid, err := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	if err != nil {
		authMsg(w, 400, L("srv.http_util.requeteInvalide"))
		return
	}
	list, err := d.s.userSessions(uid)
	if err != nil {
		authInternal(w, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		// Un préfixe suffit à reconnaître une session ; l'identifiant entier permettrait de la révoquer ailleurs.
		out = append(out, map[string]any{"id": hex.EncodeToString(x.hash)[:8], "created": x.created, "seen": x.seen, "ip": x.ip, "ua": x.ua, "mfa": x.mfa})
	}
	writeJSON(w, 200, map[string]any{"sessions": out})
}

func (d *adminAPI) sessionsRevoke(w http.ResponseWriter, r *http.Request) {
	p := d.admin(w, r)
	var req struct {
		adminReq
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	uid, err := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	if err != nil {
		authMsg(w, 400, L("srv.http_util.requeteInvalide"))
		return
	}
	// Ses propres sessions : pas de pair à écarter.
	if uid != p.User.ID {
		admin, ok := d.adminTarget(w, uid)
		if !ok || (admin && !d.fresh(w, r, p, req.Reauth)) {
			return
		}
	}
	if err := d.s.RevokeUser(uid); err != nil {
		authInternal(w, err)
		return
	}
	d.selfOut(w, p, uid)
	d.audit(p, r, "session_revoke", fmt.Sprintf("user=%d all", uid))
	w.WriteHeader(http.StatusNoContent)
}

// DeleteHomesACL retire les droits des homes supprimés : un identifiant ne revient jamais, mais une ligne orpheline
// resterait visible et signée.
func (s *authStore) DeleteHomesACL(ids []string, by int64, ip string) error {
	if len(ids) == 0 {
		return nil
	}
	var n int64
	err := s.inTx(func(tx *sql.Tx) error {
		for _, id := range ids {
			res, err := tx.Exec(`DELETE FROM home_acl WHERE home_id=?`, id)
			if err != nil {
				return err
			}
			k, _ := res.RowsAffected()
			n += k
		}
		return nil
	})
	if err == nil {
		s.eventIP(by, "acl", fmt.Sprintf("homes_removed=%s rows=%d", strings.Join(ids, ","), n), ip)
	}
	return err
}

func init() { onHomesRemoved = homesRemovedACL }

// homesRemovedACL : branché sur onHomesRemoved ; sans garde (mode ouvert, home enfant), aucune ligne à retirer.
func homesRemovedACL(r *http.Request, ids []string) {
	g := gateFrom(r)
	if g == nil || g.s == nil || g.p == nil || g.p.User == nil {
		return
	}
	if err := g.s.DeleteHomesACL(ids, g.p.User.ID, clientIP(r)); err != nil {
		journal("auth: droits des homes retirés %v : %v", ids, err)
	}
}
