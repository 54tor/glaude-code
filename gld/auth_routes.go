package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	authBodyMax = 16 << 10
	waBodyMax   = 64 << 10
	reauthFor   = 5 * time.Minute
)

// Points d'accroche des tests : compter le travail argon2 et bloquer une vérification en cours.
var (
	verifyPassword = checkPassword
	dummyPassword  = dummyCheck
	useRecovery    = (*authStore).UseRecoveryCode
	genRecovery    = genRecoveryCodes
	verifyTOTP     = (*authStore).TOTPVerify
)

type authAPI struct {
	s  *authStore
	th *throttle

	mu       sync.Mutex
	reauthAt map[string]time.Time
	busy     map[string]bool
	rl       rlWindow
}

const (
	rlCap      = 1024
	rateBudget = 5
	rlGlobal   = 200
)

// oversizeName : clé commune des noms de plus de 64 caractères ; normName rogne les espaces, aucun nom tapé n'y aboutit.
const oversizeName = " oversize"

type factorState struct {
	TOTP     bool `json:"totp"`
	WebAuthn int  `json:"webauthn"`
	Recovery int  `json:"recovery"`
}

func (f factorState) none() bool { return !f.TOTP && f.WebAuthn == 0 }

// factors échoue sur toute ligne altérée : un facteur douteux ne doit jamais rouvrir l'enrôlement sans second facteur.
func (s *authStore) factors(uid int64) (factorState, error) {
	var f factorState
	u, err := s.userByID(s.db, uid)
	if err != nil {
		return f, s.guard(err)
	}
	t, err := s.loadTOTP(s.db, uid)
	if err = s.guard(err); err != nil && !errors.Is(err, errNoTOTP) {
		return f, err
	}
	f.TOTP = err == nil && t.confirmed.Valid
	creds, err := s.goodCreds(s.db, uid)
	if err != nil {
		return f, s.guard(err)
	}
	f.WebAuthn = len(creds)
	// Compte enrôlé sans aucun facteur : lignes supprimées, jamais une raison d'enrôler avec le seul mot de passe.
	if u.MFAEnrolled && f.none() {
		return f, s.guard(&tamperErr{uid, "mfa_factors"})
	}
	err = s.db.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, uid).Scan(&f.Recovery)
	return f, err
}

// mfaFails lit le compteur mfa: sans passer par Check, qui consommerait une place du plafond global.
func (t *throttle) mfaFails(user string) int64 {
	_, _, _, mk := thKeys(user, "")
	var fails, first int64
	if t.db.QueryRow(`SELECT fails, first_at FROM throttle WHERE key=?`, mk).Scan(&fails, &first) != nil {
		return 0
	}
	if first <= t.now().Add(-thWindow).Unix() {
		return 0
	}
	return fails
}

func routesAuth(mux *http.ServeMux, s *authStore, th *throttle) *authAPI {
	a := &authAPI{s: s, th: th, reauthAt: map[string]time.Time{}, busy: map[string]bool{}}
	mux.HandleFunc("POST /auth/login", a.login)
	mux.HandleFunc("POST /auth/mfa/totp", a.mfaTOTP)
	mux.HandleFunc("POST /auth/mfa/recovery", a.mfaRecovery)
	mux.HandleFunc("POST /auth/mfa/webauthn/begin", a.mfaWebAuthnBegin)
	mux.HandleFunc("POST /auth/mfa/webauthn/finish", a.mfaWebAuthnFinish)
	mux.HandleFunc("POST /auth/logout", a.logout)
	mux.HandleFunc("GET /auth/me", a.me)
	mux.HandleFunc("POST /auth/totp/begin", a.totpBegin)
	mux.HandleFunc("POST /auth/totp/confirm", a.totpConfirm)
	mux.HandleFunc("POST /auth/webauthn/register/begin", a.waRegisterBegin)
	mux.HandleFunc("POST /auth/webauthn/register/finish", a.waRegisterFinish)
	mux.HandleFunc("POST /auth/factor/remove", a.factorRemove)
	mux.HandleFunc("POST /auth/recovery/regenerate", a.recoveryRegenerate)
	mux.HandleFunc("POST /auth/password", a.password)
	mux.HandleFunc("GET /auth/sessions", a.sessions)
	mux.HandleFunc("POST /auth/sessions/revoke", a.sessionRevoke)
	return a
}

// authFail est l'unique réponse d'échec : même statut, même corps, mêmes en-têtes quelle que soit la cause.
func authFail(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
	writeJSON(w, 401, map[string]any{"ok": false, "out": L("srv.auth.invalide")})
}

func authInternal(w http.ResponseWriter, err error) {
	journal("auth: %v", err)
	writeJSON(w, 500, map[string]any{"ok": false, "out": L("srv.auth.erreur")})
}

// authUserErr ne montre que les erreurs du catalogue ; le reste part au journal.
func authUserErr(w http.ResponseWriter, code int, err error) {
	var m *msgErr
	if errors.As(err, &m) {
		writeJSON(w, code, map[string]any{"ok": false, "out": err.Error()})
		return
	}
	authInternal(w, err)
}

func decodeAuth(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, authBodyMax)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil || dec.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
		return false
	}
	return true
}

// pwSane écarte avant tout calcul argon2 ce qu'aucun mot de passe valide ne peut être.
func pwSane(pw string) bool {
	if pw == "" || len(pw) > 1024 {
		return false
	}
	for i := 0; i < len(pw); i++ {
		if pw[i] == 0 {
			return false
		}
	}
	return true
}

func (a *authAPI) log(uid int64, name, ip, kind, outcome string) {
	var u any
	if uid != 0 {
		u = uid
	}
	a.s.db.Exec(`INSERT INTO auth_events(at,user_id,name_tried,ip,kind,outcome) VALUES(?,?,?,?,?,?)`, authNow().Unix(), u, name, ip, kind, outcome)
}

func (a *authAPI) fail(w http.ResponseWriter, uid int64, name, ip, kind, outcome, stage string) {
	if err := a.th.Fail(name, ip, stage); err != nil {
		journal("auth: throttle: %v", err)
	}
	a.log(uid, name, ip, kind, outcome)
	wait, _ := a.th.Pending(name, ip)
	authFail(w, wait)
}

// throttled répond aussitôt, sans rien vérifier, quand une attente ou un verrou s'applique ;
// onBlocked passe avant la réponse, pour pouvoir encore effacer le cookie.
func (a *authAPI) throttled(w http.ResponseWriter, uid int64, name, ip, kind string, onBlocked func()) bool {
	wait, blocked := a.th.Check(name, ip)
	if !blocked && wait <= 0 {
		return false
	}
	a.rateLog(uid, name, ip, kind, "blocked")
	if blocked && onBlocked != nil {
		onBlocked()
	}
	authFail(w, wait)
	return true
}

func (a *authAPI) login(w http.ResponseWriter, r *http.Request) {
	// Exiger JSON interdit le formulaire HTML intersite, seule voie d'une connexion forcée sans pré-vol.
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeJSON(w, 415, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
		return
	}
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decodeAuth(w, r, &req) {
		return
	}
	name, ip := normName(req.Name), clientIP(r)
	// Un nom trop long pour un compte ne crée ni clé ni événement à son nom : tous partagent une seule clé.
	if utf8.RuneCountInString(name) > 64 {
		name = oversizeName
	}
	release, ok := a.claim("login:" + name)
	if !ok {
		a.rateLog(0, name, ip, evLogin, "busy")
		authFail(w, 0)
		return
	}
	defer release()
	if a.throttled(w, 0, name, ip, evLogin, nil) {
		return
	}
	if !pwSane(req.Password) || name == oversizeName {
		a.fail(w, 0, name, ip, evLogin, "invalid_password", "password")
		return
	}
	u, err := a.s.UserByName(name)
	if err != nil {
		dummyPassword()
		outcome := "unknown"
		if errors.Is(err, errTamper) {
			outcome = "tamper"
		} else if !errors.Is(err, errNoUser) {
			journal("auth: login: %v", err)
		}
		a.fail(w, 0, name, ip, evLogin, outcome, "password")
		return
	}
	if !verifyPassword(u.PwHash, req.Password) {
		a.fail(w, u.ID, name, ip, evLogin, "bad_password", "password")
		return
	}
	if u.Disabled {
		a.fail(w, u.ID, name, ip, evLogin, "disabled", "password")
		return
	}
	f, err := a.s.factors(u.ID)
	if err != nil {
		journal("auth: login factors: %v", err)
		a.fail(w, u.ID, name, ip, evLogin, "factors", "password")
		return
	}
	c, err := a.s.NewSession(u.ID, ip, r.UserAgent())
	var p *Principal
	if err == nil {
		p, err = a.s.Session(c)
	}
	if err != nil {
		journal("auth: login session: %v", err)
		a.fail(w, u.ID, name, ip, evLogin, "session_error", "password")
		return
	}
	setSessionCookie(w, r, c, authNow().Add(thMFAWindow))
	methods := []string{}
	if f.TOTP {
		methods = append(methods, "totp")
	}
	if f.WebAuthn > 0 {
		methods = append(methods, "webauthn")
	}
	if f.Recovery > 0 {
		methods = append(methods, "recovery")
	}
	a.log(u.ID, name, ip, evLogin, "password_ok")
	writeJSON(w, 200, map[string]any{"ok": true, "mfa": methods, "enroll": f.none(), "csrf": p.CSRF, "out": L("srv.auth.mfaRequis")})
}

// need exige une session valide et, hors GET, le jeton CSRF ; full exige en plus le second facteur.
func (a *authAPI) need(w http.ResponseWriter, r *http.Request, full bool) *Principal {
	a.rlFlush()
	c, err := r.Cookie(sessCookie)
	var p *Principal
	if err == nil {
		p, err = a.s.Session(c.Value)
	}
	if err != nil {
		if !errors.Is(err, errNoSession) && !errors.Is(err, http.ErrNoCookie) {
			journal("auth: session: %v", err)
		}
		a.deniedLog(0, clientIP(r), "auth")
		writeJSON(w, 401, map[string]any{"ok": false, "out": L("srv.auth.sessionRequise")})
		return nil
	}
	if !csrfOK(r, p) {
		a.deniedLog(p.User.ID, clientIP(r), "csrf")
		writeJSON(w, 403, map[string]any{"ok": false, "out": L("srv.auth.csrf")})
		return nil
	}
	if full && !p.MFA {
		writeJSON(w, 403, map[string]any{"ok": false, "out": L("srv.auth.mfaRequis")})
		return nil
	}
	return p
}

// deniedLog journalise un refus ; anonyme, il passe par la limite de rateLog.
func (a *authAPI) deniedLog(uid int64, ip, kind string) { a.rateLog(uid, "", ip, kind, "denied") }

// rlWindow : la minute en cours du limiteur d'événements, tenue sous a.mu ; rien n'y touche la base.
type rlWindow struct {
	start    time.Time
	src      map[string]int  // événements anonymes écrits par adresse (déjà réduite à son /64 par clientIP)
	anon     int             // événements anonymes écrits, toutes adresses
	uids     map[string]bool // (compte, type|issue) déjà écrits
	anonDrop int64
	overflow int64
	uidDrop  int64
}

// rateLog écrit un refus de connexion ou une contention. Anonyme : au plus rateBudget par adresse et rlGlobal en tout par
// minute ; table d'adresses pleine, l'événement va au compte commun « overflow » (aucune adresse n'est évincée). Avec un
// compte : une fois par minute par (compte, type|issue). Le nom tapé n'entre dans aucune clé. Les comptes perdus sont tenus
// en mémoire et écrits en un seul résumé « suppressed » par minute, après la minute, hors du verrou. Les événements
// critiques (altération, verrou, échecs et succès du second facteur, changements de droits ou de facteurs) ne passent pas ici.
func (a *authAPI) rateLog(uid int64, name, ip, kind, outcome string) {
	if r := []rune(name); len(r) > 64 {
		name = string(r[:64])
	}
	now := authNow()
	write := false
	a.mu.Lock()
	sum := a.rlRoll(now)
	w := &a.rl
	if uid != 0 {
		k := strconv.FormatInt(uid, 10) + "|" + kind + "|" + outcome
		if !w.uids[k] {
			w.uids[k], write = true, true
		} else {
			w.uidDrop++
		}
	} else if n, ok := w.src[ip]; !ok && len(w.src) >= rlCap {
		w.overflow++
	} else if n < rateBudget && w.anon < rlGlobal {
		w.src[ip], w.anon, write = n+1, w.anon+1, true
	} else {
		w.src[ip] = n
		w.anonDrop++
	}
	a.mu.Unlock()
	a.writeSuppressed(sum)
	if write {
		a.log(uid, name, ip, kind, outcome)
	}
}

// rlRoll ferme la minute écoulée et renvoie son résumé (vide s'il n'y a rien eu à taire) ; appelée sous a.mu.
func (a *authAPI) rlRoll(now time.Time) string {
	w := &a.rl
	if w.src != nil && now.Sub(w.start) < time.Minute {
		return ""
	}
	sum := ""
	if w.anonDrop+w.overflow+w.uidDrop > 0 {
		sum = fmt.Sprintf("anon=%d overflow=%d uid=%d", w.anonDrop, w.overflow, w.uidDrop)
	}
	*w = rlWindow{start: now, src: map[string]int{}, uids: map[string]bool{}}
	return sum
}

// rlFlush ferme la minute écoulée sans événement à écrire ; appelée par chaque requête qui vérifie une session.
func (a *authAPI) rlFlush() {
	a.mu.Lock()
	sum := a.rlRoll(authNow())
	a.mu.Unlock()
	a.writeSuppressed(sum)
}

func (a *authAPI) writeSuppressed(sum string) {
	if sum != "" {
		a.s.db.Exec(`INSERT INTO auth_events(at,kind,outcome) VALUES(?,?,?)`, authNow().Unix(), "suppressed", sum)
	}
}

// claim réserve une clé le temps d'une vérification : une requête concurrente sur la même clé
// est refusée sans rien vérifier, pour que les compteurs voient chaque échec avant l'essai suivant.
func (a *authAPI) claim(key string) (release func(), ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy[key] {
		return nil, false
	}
	a.busy[key] = true
	return func() {
		a.mu.Lock()
		delete(a.busy, key)
		a.mu.Unlock()
	}, true
}

func codeKey(uid int64) string { return "code:" + strconv.FormatInt(uid, 10) }

func (a *authAPI) kill(w http.ResponseWriter, p *Principal, ip string) {
	if err := a.s.RevokeSession(p.SessionID); err != nil {
		journal("auth: revoke partial session: %v", err)
	}
	clearSessionCookie(w)
	a.log(p.User.ID, p.User.Name, ip, "mfa", "session_killed")
}

// second applique le même contrôle à toute méthode de second facteur ; p.User a déjà été relu
// et vérifié (signature, compte actif) par Session.
func (a *authAPI) second(w http.ResponseWriter, r *http.Request, p *Principal, method string, verify func() bool) {
	name, ip, uid := p.User.Name, clientIP(r), p.User.ID
	kind := "mfa_" + method
	release, ok := a.claim(codeKey(uid))
	if !ok {
		a.rateLog(uid, name, ip, kind, "busy")
		authFail(w, 0)
		return
	}
	defer release()
	var onBlocked func()
	if !p.MFA {
		onBlocked = func() { a.kill(w, p, ip) }
	}
	if a.throttled(w, uid, name, ip, kind, onBlocked) {
		return
	}
	if !verify() {
		if err := a.th.Fail(name, ip, "mfa"); err != nil {
			journal("auth: throttle: %v", err)
		}
		a.log(uid, name, ip, kind, "failure")
		if !p.MFA && a.th.mfaFails(name) >= thMFALimit {
			a.kill(w, p, ip)
		}
		wait, _ := a.th.Pending(name, ip)
		authFail(w, wait)
		return
	}
	a.log(uid, name, ip, kind, evSuccess)
	if p.MFA {
		a.markReauth(p.SessionID)
		writeJSON(w, 200, map[string]any{"ok": true, "csrf": p.CSRF})
		return
	}
	csrf, ok := a.promote(w, r, p, ip)
	if !ok {
		authFail(w, 0)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "csrf": csrf})
}

// promote achève la connexion : compteurs remis à zéro, nouvelle valeur de cookie, événement login/success.
func (a *authAPI) promote(w http.ResponseWriter, r *http.Request, p *Principal, ip string) (string, bool) {
	if err := a.th.Success(p.User.Name, ip); err != nil {
		journal("auth: throttle success: %v", err)
		return "", false
	}
	c, exp, err := a.s.PromoteMFA(p.SessionID)
	var np *Principal
	if err == nil {
		np, err = a.s.Session(c)
	}
	if err != nil {
		if !errors.Is(err, errNoSession) {
			journal("auth: promote: %v", err)
		}
		return "", false
	}
	setSessionCookie(w, r, c, exp)
	// Le compte précédent du navigateur est inconnu (session échue) : le home choisi n'est jamais légué.
	clearHomeCookie(w)
	a.s.eventIP(p.User.ID, evLogin, evSuccess, ip)
	return np.CSRF, true
}

func (a *authAPI) markReauth(sid string) {
	now := authNow()
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, t := range a.reauthAt {
		if now.Sub(t) > reauthFor {
			delete(a.reauthAt, k)
		}
	}
	a.reauthAt[sid] = now
}

func (a *authAPI) reauthFresh(sid string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.reauthAt[sid]
	return ok && authNow().Sub(t) <= reauthFor
}

func (a *authAPI) forgetReauth(sid string) {
	a.mu.Lock()
	delete(a.reauthAt, sid)
	a.mu.Unlock()
}

type codeReq struct {
	Code string `json:"code"`
}

func (a *authAPI) mfaTOTP(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, false)
	var req codeReq
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	a.second(w, r, p, "totp", func() bool { return verifyTOTP(a.s, p.User.ID, req.Code, authNow()) })
}

func (a *authAPI) mfaRecovery(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, false)
	var req codeReq
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	a.second(w, r, p, "recovery", func() bool { return useRecovery(a.s, p.User.ID, req.Code) })
}

func (a *authAPI) mfaWebAuthnBegin(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, false)
	if p == nil {
		return
	}
	opts, err := a.s.WebAuthnBeginLogin(p.User.ID, p.SessionID)
	if err != nil {
		authUserErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "options": opts})
}

func (a *authAPI) mfaWebAuthnFinish(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, false)
	if p == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, waBodyMax))
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	a.second(w, r, p, "webauthn", func() bool { return a.s.WebAuthnFinishLogin(p.User.ID, p.SessionID, r) == nil })
}

func (a *authAPI) logout(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, false)
	if p == nil {
		return
	}
	if err := a.s.RevokeSession(p.SessionID); err != nil {
		authInternal(w, err)
		return
	}
	a.forgetReauth(p.SessionID)
	clearSessionCookie(w)
	clearHomeCookie(w)
	a.log(p.User.ID, p.User.Name, clientIP(r), "logout", "ok")
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *authAPI) me(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessCookie)
	var p *Principal
	if err == nil {
		p, err = a.s.Session(c.Value)
	}
	if err != nil {
		writeJSON(w, 401, map[string]any{"ok": false})
		return
	}
	f, err := a.s.factors(p.User.ID)
	if err != nil {
		authInternal(w, err)
		return
	}
	out := map[string]any{"ok": true, "user": map[string]any{"name": p.User.Name, "admin": p.User.Admin}, "mfa": p.MFA, "csrf": p.CSRF, "factors": f}
	if !p.MFA {
		out["out"] = L("srv.auth.mfaRequis")
		writeJSON(w, 200, out)
		return
	}
	creds, err := a.s.WebAuthnList(p.User.ID)
	if err != nil {
		authInternal(w, err)
		return
	}
	keys := make([]map[string]any, 0, len(creds))
	for _, k := range creds {
		keys = append(keys, map[string]any{"id": k.ID, "label": k.Label, "created": k.CreatedAt, "bad": k.Bad})
	}
	out["factors"] = map[string]any{"totp": f.TOTP, "webauthn": f.WebAuthn, "recovery": f.Recovery, "keys": keys}
	rights, homes, err := a.myHomes(p.User.ID)
	if err != nil {
		authInternal(w, err)
		return
	}
	out["rights"], out["homes"] = rights, homes
	writeJSON(w, 200, out)
}

// myHomes ne liste que les homes où le compte a au moins un droit (ou une ligne altérée, montrée vide) :
// un compte sans droit n'apprend rien des homes existants.
func (a *authAPI) myHomes(uid int64) (map[string][]Right, []map[string]string, error) {
	rights := map[string][]Right{}
	homes := []map[string]string{}
	names := homeNames()
	ids := []string{mainHomeID}
	for _, h := range loadHomes() {
		ids = append(ids, h.ID)
	}
	for _, id := range ids {
		m, err := a.s.Rights(uid, id)
		held := []Right{}
		switch {
		case errors.Is(err, errTamper):
		case err != nil:
			return nil, nil, err
		default:
			for _, r := range allRights {
				if m[r] {
					held = append(held, r)
				}
			}
			if len(held) == 0 {
				continue
			}
		}
		rights[id] = held
		homes = append(homes, map[string]string{"id": id, "name": names[id]})
	}
	return rights, homes, nil
}

// reauthReq : mot de passe, plus un code (TOTP ou secours) ou, à défaut, une assertion WebAuthn
// réussie sur cette session depuis moins de cinq minutes (/auth/mfa/webauthn/*).
type reauthReq struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (a *authAPI) reauth(w http.ResponseWriter, r *http.Request, p *Principal, q reauthReq) bool {
	name, ip, uid := p.User.Name, clientIP(r), p.User.ID
	release, ok := a.claim(codeKey(uid))
	if !ok {
		a.rateLog(uid, name, ip, "reauth", "busy")
		authFail(w, 0)
		return false
	}
	defer release()
	if a.throttled(w, uid, name, ip, "reauth", nil) {
		return false
	}
	if !pwSane(q.Password) || !verifyPassword(p.User.PwHash, q.Password) {
		a.fail(w, uid, name, ip, "reauth", "bad_password", "password")
		return false
	}
	ok = a.reauthFresh(p.SessionID)
	if q.Code != "" {
		// Un code de secours coûte jusqu'à dix argon2id : claim garantit qu'un seul calcule à la fois.
		ok = verifyTOTP(a.s, uid, q.Code, authNow()) || useRecovery(a.s, uid, q.Code)
	}
	if !ok {
		a.fail(w, uid, name, ip, "reauth", "bad_factor", "mfa")
		return false
	}
	a.log(uid, name, ip, "reauth", evSuccess)
	return true
}

// enrolSession admet une session partielle seulement pour un compte sans aucun facteur :
// c'est l'enrôlement obligatoire de la première connexion.
func (a *authAPI) enrolSession(w http.ResponseWriter, r *http.Request) (*Principal, factorState, bool) {
	p := a.need(w, r, false)
	if p == nil {
		return nil, factorState{}, false
	}
	f, err := a.s.factors(p.User.ID)
	if err != nil {
		authInternal(w, err)
		return nil, f, false
	}
	if !p.MFA && !f.none() {
		writeJSON(w, 403, map[string]any{"ok": false, "out": L("srv.auth.mfaRequis")})
		return nil, f, false
	}
	return p, f, true
}

// enrolBegin : la réauthentification ne s'applique qu'à une session complète ; confirm/finish
// n'aboutissent qu'avec le secret ou le défi émis par begin sur cette même session.
func (a *authAPI) enrolBegin(w http.ResponseWriter, r *http.Request) (*Principal, bool) {
	p, _, ok := a.enrolSession(w, r)
	var q reauthReq
	if !ok || !decodeAuth(w, r, &q) {
		return nil, false
	}
	if p.MFA && !a.reauth(w, r, p, q) {
		return nil, false
	}
	return p, true
}

// enrolCommit écrit le facteur, corps déjà lu. Sous la clé first:<uid> (qui ne sérialise que ce compte) :
// vérification « aucun facteur », écriture qui valide le code ou l'attestation, puis seulement en cas
// de succès les codes de secours (argon2) et la promotion. Un échec ne coûte aucun argon2.
func (a *authAPI) enrolCommit(w http.ResponseWriter, r *http.Request, p *Principal, what string, write func() error) {
	if !p.MFA {
		release, ok := a.claim("first:" + strconv.FormatInt(p.User.ID, 10))
		if !ok {
			a.rateLog(p.User.ID, p.User.Name, clientIP(r), "enrol", what+"_busy")
			writeJSON(w, 409, map[string]any{"ok": false, "out": L("srv.auth.enrolementEnCours")})
			return
		}
		defer release()
	}
	f, err := a.s.factors(p.User.ID)
	if err != nil {
		authInternal(w, err)
		return
	}
	if !p.MFA && !f.none() {
		writeJSON(w, 403, map[string]any{"ok": false, "out": L("srv.auth.mfaRequis")})
		return
	}
	if err := write(); err != nil {
		a.log(p.User.ID, p.User.Name, clientIP(r), "enrol", what+"_refused")
		authUserErr(w, 400, err)
		return
	}
	ip := clientIP(r)
	a.log(p.User.ID, p.User.Name, ip, "enrol", what)
	out := map[string]any{"ok": true, "csrf": p.CSRF}
	if f.none() {
		codes, hashes, err := genRecovery()
		if err != nil {
			authInternal(w, err)
			return
		}
		if err := a.s.storeRecoveryCodes(p.User.ID, hashes); err != nil {
			authInternal(w, err)
			return
		}
		out["recovery"] = codes
	}
	if !p.MFA {
		csrf, ok := a.promote(w, r, p, ip)
		if !ok {
			authFail(w, 0)
			return
		}
		out["csrf"] = csrf
	}
	writeJSON(w, 200, out)
}

func (a *authAPI) totpBegin(w http.ResponseWriter, r *http.Request) {
	p, ok := a.enrolBegin(w, r)
	if !ok {
		return
	}
	secret, uri, err := a.s.TOTPBegin(p.User.ID, p.User.Name)
	if err != nil {
		a.log(p.User.ID, p.User.Name, clientIP(r), "enrol", "totp_begin_refused")
		authUserErr(w, 409, err)
		return
	}
	a.log(p.User.ID, p.User.Name, clientIP(r), "enrol", "totp_begin")
	writeJSON(w, 200, map[string]any{"ok": true, "secret": secret, "uri": uri})
}

func (a *authAPI) totpConfirm(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.enrolSession(w, r)
	var req codeReq
	if !ok || !decodeAuth(w, r, &req) {
		return
	}
	a.enrolCommit(w, r, p, "totp", func() error { return a.s.TOTPConfirm(p.User.ID, req.Code) })
}

func (a *authAPI) waRegisterBegin(w http.ResponseWriter, r *http.Request) {
	p, ok := a.enrolBegin(w, r)
	if !ok {
		return
	}
	opts, err := a.s.WebAuthnBeginRegister(p.User.ID, p.SessionID)
	if err != nil {
		authUserErr(w, 400, err)
		return
	}
	a.log(p.User.ID, p.User.Name, clientIP(r), "enrol", "webauthn_begin")
	writeJSON(w, 200, map[string]any{"ok": true, "options": opts})
}

// Le corps est la réponse brute du navigateur, lue par la bibliothèque : le libellé passe en paramètre d'URL.
func (a *authAPI) waRegisterFinish(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.enrolSession(w, r)
	if !ok {
		return
	}
	// Lu en entier avant tout verrou : un client lent ne doit rien bloquer.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, waBodyMax))
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	a.enrolCommit(w, r, p, "webauthn", func() error {
		return a.s.WebAuthnFinishRegister(p.User.ID, p.SessionID, r.URL.Query().Get("label"), r)
	})
}

func (a *authAPI) factorRemove(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, true)
	var req struct {
		reauthReq
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	if req.Kind != "totp" && req.Kind != "webauthn" {
		writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.auth.facteurInconnu")})
		return
	}
	if !a.reauth(w, r, p, req.reauthReq) {
		return
	}
	var err error
	if req.Kind == "totp" {
		err = a.s.TOTPRemove(p.User.ID)
	} else {
		err = a.s.WebAuthnRemove(p.User.ID, req.ID)
	}
	ip := clientIP(r)
	switch {
	case err == nil:
		a.log(p.User.ID, p.User.Name, ip, "factor_remove", req.Kind)
		writeJSON(w, 200, map[string]any{"ok": true})
	case errors.Is(err, errNoTOTP) || errors.Is(err, errWebAuthnFailed):
		a.log(p.User.ID, p.User.Name, ip, "factor_remove", "unknown")
		writeJSON(w, 404, map[string]any{"ok": false, "out": L("srv.auth.facteurInconnu")})
	default:
		a.log(p.User.ID, p.User.Name, ip, "factor_remove", "refused")
		authUserErr(w, 409, err)
	}
}

// recoveryRegenerate remplace tout le jeu : les anciens codes cessent de valoir, les nouveaux ne sont montrés qu'ici.
func (a *authAPI) recoveryRegenerate(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, true)
	var req reauthReq
	if p == nil || !decodeAuth(w, r, &req) || !a.reauth(w, r, p, req) {
		return
	}
	codes, err := a.s.NewRecoveryCodes(p.User.ID)
	if err != nil {
		authInternal(w, err)
		return
	}
	a.log(p.User.ID, p.User.Name, clientIP(r), "recovery", "regenerated")
	writeJSON(w, 200, map[string]any{"ok": true, "recovery": codes})
}

func (a *authAPI) password(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, true)
	var req struct {
		reauthReq
		New string `json:"new"`
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	// Valider avant la réauthentification évite de consommer un code pour un mot de passe refusé.
	if err := validPassword(req.New); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "out": pwError(err).Error()})
		return
	}
	if !a.reauth(w, r, p, req.reauthReq) {
		return
	}
	if err := a.s.SetPassword(p.User.ID, req.New); err != nil {
		authInternal(w, err)
		return
	}
	a.forgetReauth(p.SessionID)
	clearSessionCookie(w)
	a.log(p.User.ID, p.User.Name, clientIP(r), "password", "changed")
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *authAPI) sessions(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, true)
	if p == nil {
		return
	}
	list, err := a.s.userSessions(p.User.ID)
	if err != nil {
		authInternal(w, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		id := hex.EncodeToString(x.hash)
		out = append(out, map[string]any{"id": id, "created": x.created, "seen": x.seen, "ip": x.ip, "ua": x.ua, "mfa": x.mfa, "current": id == p.SessionID})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "sessions": out})
}

func (a *authAPI) sessionRevoke(w http.ResponseWriter, r *http.Request) {
	p := a.need(w, r, true)
	var req struct {
		reauthReq
		ID string `json:"id"`
	}
	if p == nil || !decodeAuth(w, r, &req) {
		return
	}
	h, err := hex.DecodeString(req.ID)
	if err != nil || len(h) != 32 {
		writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.auth.sessionInconnue")})
		return
	}
	if !a.reauth(w, r, p, req.reauthReq) {
		return
	}
	// user_id est signé : vérifier puis révoquer ne laisse pas de fenêtre.
	owner, err := a.s.sessionOwner(h)
	if errors.Is(err, errNoSession) || (err == nil && owner != p.User.ID) {
		writeJSON(w, 404, map[string]any{"ok": false, "out": L("srv.auth.sessionInconnue")})
		return
	}
	if err == nil {
		err = a.s.RevokeSession(req.ID)
	}
	if err != nil {
		authInternal(w, err)
		return
	}
	a.forgetReauth(req.ID)
	if req.ID == p.SessionID {
		clearSessionCookie(w)
	}
	a.log(p.User.ID, p.User.Name, clientIP(r), "session_revoke", "ok")
	writeJSON(w, 200, map[string]any{"ok": true})
}
