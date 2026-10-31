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
	"strings"
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

func (factors factorState) none() bool { return !factors.TOTP && factors.WebAuthn == 0 }

// factors échoue sur toute ligne altérée : un facteur douteux ne doit jamais rouvrir l'enrôlement sans second facteur.
func (store *authStore) factors(userID int64) (factorState, error) {
	var factors factorState
	user, err := store.userByID(store.db, userID)
	if err != nil {
		return factors, store.guard(err)
	}
	totp, err := store.loadTOTP(store.db, userID)
	if err = store.guard(err); err != nil && !errors.Is(err, errNoTOTP) {
		return factors, err
	}
	factors.TOTP = err == nil && totp.confirmed.Valid
	creds, err := store.goodCreds(store.db, userID)
	if err != nil {
		return factors, store.guard(err)
	}
	factors.WebAuthn = len(creds)
	// Compte enrôlé sans aucun facteur : lignes supprimées, jamais une raison d'enrôler avec le seul mot de passe.
	if user.MFAEnrolled && factors.none() {
		return factors, store.guard(&tamperErr{userID, "mfa_factors"})
	}
	err = store.db.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, userID).Scan(&factors.Recovery)
	return factors, err
}

// mfaFails lit le compteur mfa: global du compte sans passer par Check, qui consommerait une place du plafond global.
func (throttler *throttle) mfaFails(user string) int64 {
	_, _, _, mfaKey := thKeys(user, "")
	return throttler.mfaCount(mfaKey)
}

// mfaOver : le second facteur est bloqué pour ce compte depuis cette adresse, ou pour ce compte partout.
func (throttler *throttle) mfaOver(user, ip string) bool {
	return throttler.mfaCount(mfaPairKey(user, ip)) >= thMFALimit || throttler.mfaFails(user) >= thMFAGlobal
}

func (throttler *throttle) mfaCount(key string) int64 {
	var fails, firstAt int64
	if throttler.db.QueryRow(`SELECT fails, first_at FROM throttle WHERE key=?`, key).Scan(&fails, &firstAt) != nil {
		return 0
	}
	if firstAt <= throttler.now().Add(-thWindow).Unix() {
		return 0
	}
	return fails
}

func routesAuth(mux *http.ServeMux, store *authStore, throttler *throttle) *authAPI {
	authHandlers := &authAPI{s: store, th: throttler, reauthAt: map[string]time.Time{}, busy: map[string]bool{}}
	mux.HandleFunc("POST /v1/auth/sessions", authHandlers.login)
	mux.HandleFunc("POST /v1/auth/mfa/totp", authHandlers.mfaTOTP)
	mux.HandleFunc("POST /v1/auth/mfa/recovery", authHandlers.mfaRecovery)
	mux.HandleFunc("POST /v1/auth/mfa/webauthn/challenges", authHandlers.mfaWebAuthnBegin)
	mux.HandleFunc("POST /v1/auth/mfa/webauthn/assertions", authHandlers.mfaWebAuthnFinish)
	mux.HandleFunc("DELETE /v1/auth/sessions/current", authHandlers.logout)
	mux.HandleFunc("GET /v1/auth/me", authHandlers.me)
	mux.HandleFunc("POST /v1/auth/totp/enrollments", authHandlers.totpBegin)
	mux.HandleFunc("PUT /v1/auth/totp/enrollments/current", authHandlers.totpConfirm)
	mux.HandleFunc("POST /v1/auth/webauthn/registrations", authHandlers.waRegisterBegin)
	mux.HandleFunc("PUT /v1/auth/webauthn/registrations/current", authHandlers.waRegisterFinish)
	mux.HandleFunc("POST /v1/auth/factors/{factor}/removals", authHandlers.factorRemove)
	mux.HandleFunc("PUT /v1/auth/recovery-codes", authHandlers.recoveryRegenerate)
	mux.HandleFunc("PUT /v1/auth/password", authHandlers.password)
	mux.HandleFunc("GET /v1/auth/alerts", authHandlers.alerts)
	mux.HandleFunc("GET /v1/auth/sessions", authHandlers.sessions)
	mux.HandleFunc("POST /v1/auth/sessions/{sid}/revocations", authHandlers.sessionRevoke)
	mux.HandleFunc("POST /v1/auth/sessions/revocations", authHandlers.sessionRevokeOthers)
	return authHandlers
}

// authFail est l'unique réponse d'échec : même statut, même corps, mêmes en-têtes quelle que soit la cause.
func authFail(responseWriter http.ResponseWriter, wait time.Duration) {
	responseWriter.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
	authMsg(responseWriter, http.StatusUnauthorized, L("srv.auth.invalide"))
}

func authInternal(responseWriter http.ResponseWriter, err error) {
	journal("auth: %v", err)
	authMsg(responseWriter, http.StatusInternalServerError, L("srv.auth.erreur"))
}

// authMsg : réponse d'erreur uniforme de l'API /v1, le code découlant du statut.
func authMsg(responseWriter http.ResponseWriter, status int, msg string) {
	writeErr(responseWriter, status, statusCode(status), msg)
}

// authUserErr ne montre que les erreurs du catalogue ; le reste part au journal.
func authUserErr(responseWriter http.ResponseWriter, code int, err error) {
	var catalogErr *msgErr
	if errors.As(err, &catalogErr) {
		authMsg(responseWriter, code, err.Error())
		return
	}
	authInternal(responseWriter, err)
}

func decodeAuth(responseWriter http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, authBodyMax)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
		return false
	}
	return true
}

// pwSane écarte avant tout calcul argon2 ce qu'aucun mot de passe valide ne peut être.
func pwSane(password string) bool {
	if password == "" || len(password) > passwordMaxBytes {
		return false
	}
	for byteIndex := 0; byteIndex < len(password); byteIndex++ {
		if password[byteIndex] == 0 {
			return false
		}
	}
	return true
}

func (authHandlers *authAPI) log(userID int64, name, ip, kind, outcome string) {
	var nullableUserID any
	if userID != 0 {
		nullableUserID = userID
	}
	authHandlers.s.db.Exec(`INSERT INTO auth_events(at,user_id,name_tried,ip,kind,outcome) VALUES(?,?,?,?,?,?)`, authNow().Unix(), nullableUserID, name, ip, kind, outcome)
}

func (authHandlers *authAPI) fail(responseWriter http.ResponseWriter, userID int64, name, ip, kind, outcome, stage string) {
	if err := authHandlers.th.Fail(name, ip, stage); err != nil {
		journal("auth: throttle: %v", err)
	}
	authHandlers.log(userID, name, ip, kind, outcome)
	wait, _ := authHandlers.th.Pending(name, ip)
	authFail(responseWriter, wait)
}

// throttled répond aussitôt, sans rien vérifier, quand une attente ou un verrou s'applique ;
// onBlocked passe avant la réponse, pour pouvoir encore effacer le cookie.
func (authHandlers *authAPI) throttled(responseWriter http.ResponseWriter, userID int64, name, ip, kind string, onBlocked func()) bool {
	wait, blocked := authHandlers.th.Check(name, ip)
	if !blocked && wait <= 0 {
		return false
	}
	authHandlers.rateLog(userID, name, ip, kind, "blocked")
	if blocked && onBlocked != nil {
		onBlocked()
	}
	authFail(responseWriter, wait)
	return true
}

func (authHandlers *authAPI) login(responseWriter http.ResponseWriter, request *http.Request) {
	// Exiger JSON interdit le formulaire HTML intersite, seule voie d'une connexion forcée sans pré-vol.
	if mediaType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type")); mediaType != "application/json" {
		authMsg(responseWriter, http.StatusUnsupportedMediaType, L("srv.http_util.requeteInvalide"))
		return
	}
	var requestBody struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	name, ip := normName(requestBody.Name), clientIP(request)
	// Un nom trop long pour un compte ne crée ni clé ni événement à son nom : tous partagent une seule clé.
	if utf8.RuneCountInString(name) > nameMaxRunes {
		name = oversizeName
	}
	release, claimed := authHandlers.claim("login:" + name)
	if !claimed {
		authHandlers.rateLog(0, name, ip, evLogin, "busy")
		authFail(responseWriter, 0)
		return
	}
	defer release()
	if authHandlers.throttled(responseWriter, 0, name, ip, evLogin, nil) {
		return
	}
	if !pwSane(requestBody.Password) || name == oversizeName {
		authHandlers.fail(responseWriter, 0, name, ip, evLogin, "invalid_password", "password")
		return
	}
	user, err := authHandlers.s.UserByName(name)
	if err != nil {
		dummyPassword()
		outcome := "unknown"
		if errors.Is(err, errTamper) {
			outcome = "tamper"
		} else if !errors.Is(err, errNoUser) {
			journal("auth: login: %v", err)
		}
		authHandlers.fail(responseWriter, 0, name, ip, evLogin, outcome, "password")
		return
	}
	if !verifyPassword(user.PwHash, requestBody.Password) {
		authHandlers.fail(responseWriter, user.ID, name, ip, evLogin, "bad_password", "password")
		return
	}
	if user.Disabled {
		authHandlers.fail(responseWriter, user.ID, name, ip, evLogin, "disabled", "password")
		return
	}
	factors, err := authHandlers.s.factors(user.ID)
	if err != nil {
		journal("auth: login factors: %v", err)
		authHandlers.fail(responseWriter, user.ID, name, ip, evLogin, "factors", "password")
		return
	}
	sessionValue, err := authHandlers.s.NewSession(user.ID, ip, request.UserAgent())
	var principal *Principal
	if err == nil {
		principal, err = authHandlers.s.Session(sessionValue)
	}
	if err != nil {
		journal("auth: login session: %v", err)
		authHandlers.fail(responseWriter, user.ID, name, ip, evLogin, "session_error", "password")
		return
	}
	setSessionCookie(responseWriter, request, sessionValue, authNow().Add(thMFAWindow))
	methods := []string{}
	if factors.TOTP {
		methods = append(methods, "totp")
	}
	if factors.WebAuthn > 0 {
		methods = append(methods, "webauthn")
	}
	if factors.Recovery > 0 {
		methods = append(methods, "recovery")
	}
	authHandlers.log(user.ID, name, ip, evLogin, "password_ok")
	writeJSON(responseWriter, http.StatusOK, map[string]any{"mfa": methods, "enroll": factors.none(), "csrf": principal.CSRF})
}

// need exige une session valide et, hors GET, le jeton CSRF ; full exige en plus le second facteur.
func (authHandlers *authAPI) need(responseWriter http.ResponseWriter, request *http.Request, full bool) *Principal {
	authHandlers.rlFlush()
	cookie, err := request.Cookie(sessCookie)
	var principal *Principal
	if err == nil {
		principal, err = authHandlers.s.Session(cookie.Value)
	}
	if err != nil {
		if !errors.Is(err, errNoSession) && !errors.Is(err, http.ErrNoCookie) {
			journal("auth: session: %v", err)
		}
		authHandlers.deniedLog(0, clientIP(request), "auth")
		authMsg(responseWriter, http.StatusUnauthorized, L("srv.auth.sessionRequise"))
		return nil
	}
	if !csrfOK(request, principal) {
		authHandlers.deniedLog(principal.User.ID, clientIP(request), "csrf")
		authMsg(responseWriter, http.StatusForbidden, L("srv.auth.csrf"))
		return nil
	}
	if full && !principal.MFA {
		authMsg(responseWriter, http.StatusForbidden, L("srv.auth.mfaRequis"))
		return nil
	}
	return principal
}

// deniedLog journalise un refus ; anonyme, il passe par la limite de rateLog.
func (authHandlers *authAPI) deniedLog(userID int64, ip, kind string) {
	authHandlers.rateLog(userID, "", ip, kind, "denied")
}

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
func (authHandlers *authAPI) rateLog(userID int64, name, ip, kind, outcome string) {
	if nameRunes := []rune(name); len(nameRunes) > nameMaxRunes {
		name = string(nameRunes[:nameMaxRunes])
	}
	now := authNow()
	shouldWrite := false
	authHandlers.mu.Lock()
	summary := authHandlers.rlRoll(now)
	window := &authHandlers.rl
	if userID != 0 {
		dedupKey := strconv.FormatInt(userID, 10) + "|" + kind + "|" + outcome
		if !window.uids[dedupKey] {
			window.uids[dedupKey], shouldWrite = true, true
		} else {
			window.uidDrop++
		}
	} else if sentCount, addressKnown := window.src[ip]; !addressKnown && len(window.src) >= rlCap {
		window.overflow++
	} else if sentCount < rateBudget && window.anon < rlGlobal {
		window.src[ip], window.anon, shouldWrite = sentCount+1, window.anon+1, true
	} else {
		window.src[ip] = sentCount
		window.anonDrop++
	}
	authHandlers.mu.Unlock()
	authHandlers.writeSuppressed(summary)
	if shouldWrite {
		authHandlers.log(userID, name, ip, kind, outcome)
	}
}

// rlRoll ferme la minute écoulée et renvoie son résumé (vide s'il n'y a rien eu à taire) ; appelée sous a.mu.
func (authHandlers *authAPI) rlRoll(now time.Time) string {
	window := &authHandlers.rl
	if window.src != nil && now.Sub(window.start) < time.Minute {
		return ""
	}
	summary := ""
	if window.anonDrop+window.overflow+window.uidDrop > 0 {
		summary = fmt.Sprintf("anon=%d overflow=%d uid=%d", window.anonDrop, window.overflow, window.uidDrop)
	}
	*window = rlWindow{start: now, src: map[string]int{}, uids: map[string]bool{}}
	return summary
}

// rlFlush ferme la minute écoulée sans événement à écrire ; appelée par chaque requête qui vérifie une session.
func (authHandlers *authAPI) rlFlush() {
	authHandlers.mu.Lock()
	summary := authHandlers.rlRoll(authNow())
	authHandlers.mu.Unlock()
	authHandlers.writeSuppressed(summary)
}

func (authHandlers *authAPI) writeSuppressed(summary string) {
	if summary != "" {
		authHandlers.s.db.Exec(`INSERT INTO auth_events(at,kind,outcome) VALUES(?,?,?)`, authNow().Unix(), "suppressed", summary)
	}
}

// claim réserve une clé le temps d'une vérification : une requête concurrente sur la même clé
// est refusée sans rien vérifier, pour que les compteurs voient chaque échec avant l'essai suivant.
func (authHandlers *authAPI) claim(key string) (release func(), claimed bool) {
	authHandlers.mu.Lock()
	defer authHandlers.mu.Unlock()
	if authHandlers.busy[key] {
		return nil, false
	}
	authHandlers.busy[key] = true
	return func() {
		authHandlers.mu.Lock()
		delete(authHandlers.busy, key)
		authHandlers.mu.Unlock()
	}, true
}

func codeKey(userID int64) string { return "code:" + strconv.FormatInt(userID, 10) }

func (authHandlers *authAPI) kill(responseWriter http.ResponseWriter, principal *Principal, ip string) {
	if err := authHandlers.s.RevokeSession(principal.SessionID); err != nil {
		journal("auth: revoke partial session: %v", err)
	}
	clearSessionCookie(responseWriter)
	authHandlers.log(principal.User.ID, principal.User.Name, ip, "mfa", "session_killed")
}

// second applique le même contrôle à toute méthode de second facteur ; p.User a déjà été relu
// et vérifié (signature, compte actif) par Session.
func (authHandlers *authAPI) second(responseWriter http.ResponseWriter, request *http.Request, principal *Principal, method string, verify func() bool) {
	name, ip, userID := principal.User.Name, clientIP(request), principal.User.ID
	kind := "mfa_" + method
	release, succeeded := authHandlers.claim(codeKey(userID))
	if !succeeded {
		authHandlers.rateLog(userID, name, ip, kind, "busy")
		authFail(responseWriter, 0)
		return
	}
	defer release()
	var onBlocked func()
	if !principal.MFA {
		onBlocked = func() { authHandlers.kill(responseWriter, principal, ip) }
	}
	if authHandlers.throttled(responseWriter, userID, name, ip, kind, onBlocked) {
		return
	}
	if !verify() {
		if err := authHandlers.th.Fail(name, ip, "mfa"); err != nil {
			journal("auth: throttle: %v", err)
		}
		authHandlers.log(userID, name, ip, kind, "failure")
		if !principal.MFA && authHandlers.th.mfaOver(name, ip) {
			authHandlers.kill(responseWriter, principal, ip)
		}
		wait, _ := authHandlers.th.Pending(name, ip)
		authFail(responseWriter, wait)
		return
	}
	authHandlers.log(userID, name, ip, kind, evSuccess)
	if principal.MFA {
		authHandlers.markReauth(principal.SessionID)
		writeJSON(responseWriter, http.StatusOK, map[string]any{"csrf": principal.CSRF})
		return
	}
	csrf, succeeded := authHandlers.promote(responseWriter, request, principal, ip)
	if !succeeded {
		authFail(responseWriter, 0)
		return
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"csrf": csrf})
}

// promote achève la connexion : compteurs remis à zéro, nouvelle valeur de cookie, événement login/success.
func (authHandlers *authAPI) promote(responseWriter http.ResponseWriter, request *http.Request, principal *Principal, ip string) (string, bool) {
	if err := authHandlers.th.Success(principal.User.Name, ip); err != nil {
		journal("auth: throttle success: %v", err)
		return "", false
	}
	sessionValue, expiresAt, err := authHandlers.s.PromoteMFA(principal.SessionID)
	var promotedPrincipal *Principal
	if err == nil {
		promotedPrincipal, err = authHandlers.s.Session(sessionValue)
	}
	if err != nil {
		if !errors.Is(err, errNoSession) {
			journal("auth: promote: %v", err)
		}
		return "", false
	}
	setSessionCookie(responseWriter, request, sessionValue, expiresAt)
	// Le compte précédent du navigateur est inconnu (session échue) : le home choisi n'est jamais légué.
	clearHomeCookie(responseWriter)
	authHandlers.s.eventIP(principal.User.ID, evLogin, evSuccess, ip)
	return promotedPrincipal.CSRF, true
}

func (authHandlers *authAPI) markReauth(sessionID string) {
	now := authNow()
	authHandlers.mu.Lock()
	defer authHandlers.mu.Unlock()
	for reauthSessionID, reauthedAt := range authHandlers.reauthAt {
		if now.Sub(reauthedAt) > reauthFor {
			delete(authHandlers.reauthAt, reauthSessionID)
		}
	}
	authHandlers.reauthAt[sessionID] = now
}

func (authHandlers *authAPI) reauthFresh(sessionID string) bool {
	authHandlers.mu.Lock()
	defer authHandlers.mu.Unlock()
	reauthedAt, found := authHandlers.reauthAt[sessionID]
	return found && authNow().Sub(reauthedAt) <= reauthFor
}

func (authHandlers *authAPI) forgetReauth(sessionID string) {
	authHandlers.mu.Lock()
	delete(authHandlers.reauthAt, sessionID)
	authHandlers.mu.Unlock()
}

type codeReq struct {
	Code string `json:"code"`
}

func (authHandlers *authAPI) mfaTOTP(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, false)
	var requestBody codeReq
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	authHandlers.second(responseWriter, request, principal, "totp", func() bool { return verifyTOTP(authHandlers.s, principal.User.ID, requestBody.Code, authNow()) })
}

func (authHandlers *authAPI) mfaRecovery(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, false)
	var requestBody codeReq
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	authHandlers.second(responseWriter, request, principal, "recovery", func() bool { return useRecovery(authHandlers.s, principal.User.ID, requestBody.Code) })
}

func (authHandlers *authAPI) mfaWebAuthnBegin(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, false)
	if principal == nil {
		return
	}
	opts, err := authHandlers.s.WebAuthnBeginLogin(principal.User.ID, principal.SessionID)
	if err != nil {
		authUserErr(responseWriter, http.StatusBadRequest, err)
		return
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"options": opts})
}

func (authHandlers *authAPI) mfaWebAuthnFinish(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, false)
	if principal == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(responseWriter, request.Body, waBodyMax))
	if err != nil {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
		return
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	authHandlers.second(responseWriter, request, principal, "webauthn", func() bool {
		return authHandlers.s.WebAuthnFinishLogin(principal.User.ID, principal.SessionID, request) == nil
	})
}

func (authHandlers *authAPI) logout(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, false)
	if principal == nil {
		return
	}
	if err := authHandlers.s.RevokeSession(principal.SessionID); err != nil {
		authInternal(responseWriter, err)
		return
	}
	authHandlers.forgetReauth(principal.SessionID)
	clearSessionCookie(responseWriter)
	clearHomeCookie(responseWriter)
	authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "logout", "ok")
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (authHandlers *authAPI) me(responseWriter http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(sessCookie)
	var principal *Principal
	if err == nil {
		principal, err = authHandlers.s.Session(cookie.Value)
	}
	if err != nil {
		authMsg(responseWriter, http.StatusUnauthorized, L("srv.auth.sessionRequise"))
		return
	}
	factors, err := authHandlers.s.factors(principal.User.ID)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	response := map[string]any{"user": map[string]any{"name": principal.User.Name, "admin": principal.User.Admin}, "mfa": principal.MFA, "csrf": principal.CSRF, "factors": factors}
	if !principal.MFA {
		writeJSON(responseWriter, http.StatusOK, response)
		return
	}
	creds, err := authHandlers.s.WebAuthnList(principal.User.ID)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	keys := make([]map[string]any, 0, len(creds))
	for _, credential := range creds {
		keys = append(keys, map[string]any{"id": credential.ID, "label": credential.Label, "created": credential.CreatedAt, "bad": credential.Bad})
	}
	response["factors"] = map[string]any{"totp": factors.TOTP, "webauthn": factors.WebAuthn, "recovery": factors.Recovery, "keys": keys}
	rights, homes, err := authHandlers.myHomes(principal.User.ID)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	response["rights"], response["homes"] = rights, homes
	writeJSON(responseWriter, http.StatusOK, response)
}

// myHomes ne liste que les homes où le compte a au moins un droit (ou une ligne altérée, montrée vide) :
// un compte sans droit n'apprend rien des homes existants.
func (authHandlers *authAPI) myHomes(userID int64) (map[string][]Right, []map[string]string, error) {
	rights := map[string][]Right{}
	homes := []map[string]string{}
	names := homeNames()
	homeIDs := []string{mainHomeID}
	for _, home := range loadHomes() {
		homeIDs = append(homeIDs, home.ID)
	}
	for _, homeID := range homeIDs {
		grantedRights, err := authHandlers.s.Rights(userID, homeID)
		held := []Right{}
		switch {
		case errors.Is(err, errTamper):
		case err != nil:
			return nil, nil, err
		default:
			for _, right := range allRights {
				if grantedRights[right] {
					held = append(held, right)
				}
			}
			if len(held) == 0 {
				continue
			}
		}
		rights[homeID] = held
		homes = append(homes, map[string]string{"id": homeID, "name": names[homeID]})
	}
	return rights, homes, nil
}

// reauthReq : mot de passe, plus un code (TOTP ou secours) ou, à défaut, une assertion WebAuthn
// réussie sur cette session depuis moins de cinq minutes (/v1/auth/mfa/webauthn/*).
type reauthReq struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (authHandlers *authAPI) reauth(responseWriter http.ResponseWriter, request *http.Request, principal *Principal, reauthBody reauthReq) bool {
	name, ip, userID := principal.User.Name, clientIP(request), principal.User.ID
	release, passed := authHandlers.claim(codeKey(userID))
	if !passed {
		authHandlers.rateLog(userID, name, ip, "reauth", "busy")
		authFail(responseWriter, 0)
		return false
	}
	defer release()
	if authHandlers.throttled(responseWriter, userID, name, ip, "reauth", nil) {
		return false
	}
	if !pwSane(reauthBody.Password) || !verifyPassword(principal.User.PwHash, reauthBody.Password) {
		authHandlers.fail(responseWriter, userID, name, ip, "reauth", "bad_password", "password")
		return false
	}
	passed = authHandlers.reauthFresh(principal.SessionID)
	if reauthBody.Code != "" {
		// Un code de secours coûte jusqu'à dix argon2id : claim garantit qu'un seul calcule à la fois.
		passed = verifyTOTP(authHandlers.s, userID, reauthBody.Code, authNow()) || useRecovery(authHandlers.s, userID, reauthBody.Code)
	}
	if !passed {
		authHandlers.fail(responseWriter, userID, name, ip, "reauth", "bad_factor", "mfa")
		return false
	}
	authHandlers.log(userID, name, ip, "reauth", evSuccess)
	return true
}

// enrolSession admet une session partielle seulement pour un compte sans aucun facteur :
// c'est l'enrôlement obligatoire de la première connexion.
func (authHandlers *authAPI) enrolSession(responseWriter http.ResponseWriter, request *http.Request) (*Principal, factorState, bool) {
	principal := authHandlers.need(responseWriter, request, false)
	if principal == nil {
		return nil, factorState{}, false
	}
	factors, err := authHandlers.s.factors(principal.User.ID)
	if err != nil {
		authInternal(responseWriter, err)
		return nil, factors, false
	}
	if !principal.MFA && !factors.none() {
		authMsg(responseWriter, http.StatusForbidden, L("srv.auth.mfaRequis"))
		return nil, factors, false
	}
	return principal, factors, true
}

// enrolBegin : la réauthentification ne s'applique qu'à une session complète ; confirm/finish
// n'aboutissent qu'avec le secret ou le défi émis par begin sur cette même session.
func (authHandlers *authAPI) enrolBegin(responseWriter http.ResponseWriter, request *http.Request) (*Principal, bool) {
	principal, _, canProceed := authHandlers.enrolSession(responseWriter, request)
	var reauthBody reauthReq
	if !canProceed || !decodeAuth(responseWriter, request, &reauthBody) {
		return nil, false
	}
	if principal.MFA && !authHandlers.reauth(responseWriter, request, principal, reauthBody) {
		return nil, false
	}
	return principal, true
}

// enrolCommit écrit le facteur, corps déjà lu. Sous la clé first:<uid> (qui ne sérialise que ce compte) :
// vérification « aucun facteur », écriture qui valide le code ou l'attestation, puis seulement en cas
// de succès les codes de secours (argon2) et la promotion. Un échec ne coûte aucun argon2.
func (authHandlers *authAPI) enrolCommit(responseWriter http.ResponseWriter, request *http.Request, principal *Principal, what string, write func() error) {
	if !principal.MFA {
		release, claimed := authHandlers.claim("first:" + strconv.FormatInt(principal.User.ID, 10))
		if !claimed {
			authHandlers.rateLog(principal.User.ID, principal.User.Name, clientIP(request), "enrol", what+"_busy")
			authMsg(responseWriter, http.StatusConflict, L("srv.auth.enrolementEnCours"))
			return
		}
		defer release()
	}
	factors, err := authHandlers.s.factors(principal.User.ID)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	if !principal.MFA && !factors.none() {
		authMsg(responseWriter, http.StatusForbidden, L("srv.auth.mfaRequis"))
		return
	}
	if writeErr := write(); writeErr != nil {
		authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "enrol", what+"_refused")
		authUserErr(responseWriter, http.StatusBadRequest, writeErr)
		return
	}
	ip := clientIP(request)
	authHandlers.log(principal.User.ID, principal.User.Name, ip, "enrol", what)
	response := map[string]any{"csrf": principal.CSRF}
	if factors.none() {
		codes, hashes, generateErr := genRecovery()
		if generateErr != nil {
			authInternal(responseWriter, generateErr)
			return
		}
		if storeErr := authHandlers.s.storeRecoveryCodes(principal.User.ID, hashes); storeErr != nil {
			authInternal(responseWriter, storeErr)
			return
		}
		response["recovery"] = codes
	}
	if !principal.MFA {
		csrf, promoted := authHandlers.promote(responseWriter, request, principal, ip)
		if !promoted {
			authFail(responseWriter, 0)
			return
		}
		response["csrf"] = csrf
	}
	writeJSON(responseWriter, http.StatusOK, response)
}

func (authHandlers *authAPI) totpBegin(responseWriter http.ResponseWriter, request *http.Request) {
	principal, canProceed := authHandlers.enrolBegin(responseWriter, request)
	if !canProceed {
		return
	}
	secret, uri, err := authHandlers.s.TOTPBegin(principal.User.ID, principal.User.Name)
	if err != nil {
		authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "enrol", "totp_begin_refused")
		authUserErr(responseWriter, http.StatusConflict, err)
		return
	}
	authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "enrol", "totp_begin")
	writeJSON(responseWriter, http.StatusOK, map[string]any{"secret": secret, "uri": uri})
}

func (authHandlers *authAPI) totpConfirm(responseWriter http.ResponseWriter, request *http.Request) {
	principal, _, canProceed := authHandlers.enrolSession(responseWriter, request)
	var requestBody codeReq
	if !canProceed || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	authHandlers.enrolCommit(responseWriter, request, principal, "totp", func() error { return authHandlers.s.TOTPConfirm(principal.User.ID, requestBody.Code) })
}

func (authHandlers *authAPI) waRegisterBegin(responseWriter http.ResponseWriter, request *http.Request) {
	principal, canProceed := authHandlers.enrolBegin(responseWriter, request)
	if !canProceed {
		return
	}
	opts, err := authHandlers.s.WebAuthnBeginRegister(principal.User.ID, principal.SessionID)
	if err != nil {
		authUserErr(responseWriter, http.StatusBadRequest, err)
		return
	}
	authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "enrol", "webauthn_begin")
	writeJSON(responseWriter, http.StatusOK, map[string]any{"options": opts})
}

// Le corps est la réponse brute du navigateur, lue par la bibliothèque : le libellé passe en paramètre d'URL.
func (authHandlers *authAPI) waRegisterFinish(responseWriter http.ResponseWriter, request *http.Request) {
	principal, _, canProceed := authHandlers.enrolSession(responseWriter, request)
	if !canProceed {
		return
	}
	// Lu en entier avant tout verrou : un client lent ne doit rien bloquer.
	body, err := io.ReadAll(http.MaxBytesReader(responseWriter, request.Body, waBodyMax))
	if err != nil {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.http_util.requeteInvalide"))
		return
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	authHandlers.enrolCommit(responseWriter, request, principal, "webauthn", func() error {
		return authHandlers.s.WebAuthnFinishRegister(principal.User.ID, principal.SessionID, request.URL.Query().Get("label"), request)
	})
}

// parseFactor lit le segment {factor} : « totp » ou « webauthn-<id> ».
func parseFactor(segment string) (kind string, id int64, valid bool) {
	if segment == "totp" {
		return segment, 0, true
	}
	idText, found := strings.CutPrefix(segment, "webauthn-")
	if !found {
		return "", 0, false
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	return "webauthn", id, err == nil
}

func (authHandlers *authAPI) factorRemove(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, true)
	var requestBody reauthReq
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	kind, factorID, valid := parseFactor(request.PathValue("factor"))
	if !valid {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.auth.facteurInconnu"))
		return
	}
	if !authHandlers.reauth(responseWriter, request, principal, requestBody) {
		return
	}
	var err error
	if kind == "totp" {
		err = authHandlers.s.TOTPRemove(principal.User.ID)
	} else {
		err = authHandlers.s.WebAuthnRemove(principal.User.ID, factorID)
	}
	ip := clientIP(request)
	switch {
	case err == nil:
		authHandlers.log(principal.User.ID, principal.User.Name, ip, "factor_remove", kind)
		responseWriter.WriteHeader(http.StatusNoContent)
	case errors.Is(err, errNoTOTP) || errors.Is(err, errWebAuthnFailed):
		authHandlers.log(principal.User.ID, principal.User.Name, ip, "factor_remove", "unknown")
		authMsg(responseWriter, http.StatusNotFound, L("srv.auth.facteurInconnu"))
	default:
		authHandlers.log(principal.User.ID, principal.User.Name, ip, "factor_remove", "refused")
		authUserErr(responseWriter, http.StatusConflict, err)
	}
}

// recoveryRegenerate remplace tout le jeu : les anciens codes cessent de valoir, les nouveaux ne sont montrés qu'ici.
func (authHandlers *authAPI) recoveryRegenerate(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, true)
	var requestBody reauthReq
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) || !authHandlers.reauth(responseWriter, request, principal, requestBody) {
		return
	}
	codes, err := authHandlers.s.NewRecoveryCodes(principal.User.ID)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "recovery", "regenerated")
	writeJSON(responseWriter, http.StatusOK, map[string]any{"recovery": codes})
}

func (authHandlers *authAPI) password(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, true)
	var requestBody struct {
		reauthReq
		New string `json:"new"`
	}
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	// Valider avant la réauthentification évite de consommer un code pour un mot de passe refusé.
	if err := validPassword(requestBody.New); err != nil {
		authMsg(responseWriter, http.StatusBadRequest, pwError(err).Error())
		return
	}
	if !authHandlers.reauth(responseWriter, request, principal, requestBody.reauthReq) {
		return
	}
	if err := authHandlers.s.SetPassword(principal.User.ID, requestBody.New); err != nil {
		authInternal(responseWriter, err)
		return
	}
	authHandlers.forgetReauth(principal.SessionID)
	clearSessionCookie(responseWriter)
	authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "password", "changed")
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (authHandlers *authAPI) sessions(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, true)
	if principal == nil {
		return
	}
	sessions, err := authHandlers.s.userSessions(principal.User.ID)
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	sessionRows := []map[string]any{}
	for _, session := range sessions {
		sessionHex := hex.EncodeToString(session.hash)
		sessionRows = append(sessionRows, map[string]any{"id": sessionHex, "created": session.created, "seen": session.seen, "ip": session.ip, "ua": session.ua, "mfa": session.mfa, "current": sessionHex == principal.SessionID})
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"sessions": sessionRows})
}

// sessionRevokeOthers : nettoyage à la demande des autres sessions ouvertes du compte, la courante reste.
func (authHandlers *authAPI) sessionRevokeOthers(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, true)
	var requestBody reauthReq
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	if !authHandlers.reauth(responseWriter, request, principal, requestBody) {
		return
	}
	if err := authHandlers.s.RevokeOthers(principal.User.ID, principal.SessionID); err != nil {
		authInternal(responseWriter, err)
		return
	}
	authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "session_revoke", "others")
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (authHandlers *authAPI) sessionRevoke(responseWriter http.ResponseWriter, request *http.Request) {
	principal := authHandlers.need(responseWriter, request, true)
	var requestBody reauthReq
	sessionID := request.PathValue("sid")
	if principal == nil || !decodeAuth(responseWriter, request, &requestBody) {
		return
	}
	sessionHash, err := hex.DecodeString(sessionID)
	if err != nil || len(sessionHash) != 32 {
		authMsg(responseWriter, http.StatusBadRequest, L("srv.auth.sessionInconnue"))
		return
	}
	if !authHandlers.reauth(responseWriter, request, principal, requestBody) {
		return
	}
	// user_id est signé : vérifier puis révoquer ne laisse pas de fenêtre.
	owner, err := authHandlers.s.sessionOwner(sessionHash)
	if errors.Is(err, errNoSession) || (err == nil && owner != principal.User.ID) {
		authMsg(responseWriter, http.StatusNotFound, L("srv.auth.sessionInconnue"))
		return
	}
	if err == nil {
		err = authHandlers.s.RevokeSession(sessionID)
	}
	if err != nil {
		authInternal(responseWriter, err)
		return
	}
	authHandlers.forgetReauth(sessionID)
	if sessionID == principal.SessionID {
		clearSessionCookie(responseWriter)
	}
	authHandlers.log(principal.User.ID, principal.User.Name, clientIP(request), "session_revoke", "ok")
	responseWriter.WriteHeader(http.StatusNoContent)
}
