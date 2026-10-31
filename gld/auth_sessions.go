package main

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

const (
	sessCookie   = "gld_session"
	sessIdle     = 12 * time.Hour
	sessAbsolute = 30 * 24 * time.Hour
	thMFAWindow  = 10 * time.Minute
	sessTouch    = time.Minute
	csrfHeader   = "X-Gld-CSRF"
)

// Inconnue, expirée, révoquée, compte désactivé ou altéré : l'appelant traite tout comme « pas de session ».
var errNoSession = errors.New("no session")

var authNow = time.Now

type Principal struct {
	User      *User
	SessionID string
	MFA       bool
	CSRF      string
	Token     bool
	// Expires est l'échéance absolue, celle que porte le cookie ; l'inactivité est vérifiée côté serveur.
	Expires time.Time
}

func (store *authStore) eventIP(userID int64, kind, outcome, ip string) {
	store.db.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`, authNow().Unix(), userID, ip, kind, outcome)
}

const (
	eventsKeep       = 90 * 24 * time.Hour
	eventsPurgeEvery = 24 * time.Hour
	sessionValueLen  = 32
)

// PurgeEvents supprime en une seule instruction les événements plus anciens que olderThan.
func (store *authStore) PurgeEvents(olderThan time.Duration) (int64, error) {
	result, err := store.db.Exec(`DELETE FROM auth_events WHERE at < ?`, authNow().Add(-olderThan).Unix())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// PurgeSessions retire les sessions qu'aucun cookie ne peut plus rouvrir : échues, au-delà de l'échéance absolue,
// ou partielles au-delà de la fenêtre du second facteur.
func (store *authStore) PurgeSessions() (int64, error) {
	now := authNow().Unix()
	result, err := store.db.Exec(`DELETE FROM sessions WHERE expires_at <= ? OR created_at <= ? OR (mfa_at IS NULL AND created_at <= ?)`,
		now, now-int64(sessAbsolute/time.Second), now-int64(thMFAWindow/time.Second))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// sessHash : HMAC avec la clé d'intégrité, pour qu'une ligne insérée sans la clé ne corresponde à aucun cookie.
func (store *authStore) sessHash(rawValue []byte) []byte {
	mac := hmac.New(sha256.New, rowSigKey(store.key))
	mac.Write([]byte("gld-session\x00"))
	mac.Write(rawValue)
	return mac.Sum(nil)
}

func (store *authStore) newSessionValue() (rawValue, hash []byte) {
	rawValue = make([]byte, sessionValueLen)
	rand.Read(rawValue)
	return rawValue, store.sessHash(rawValue)
}

// expires_at et last_seen_at restent hors signature : l'échéance absolue dérive de created_at, signé.
func (store *authStore) sessSig(sessionHash []byte, userID, created int64, mfa sql.NullInt64) []byte {
	mfaText := ""
	if mfa.Valid {
		mfaText = strconv.FormatInt(mfa.Int64, 10)
	}
	return rowSig(store.key, "sessions", hex.EncodeToString(sessionHash), strconv.FormatInt(userID, 10), strconv.FormatInt(created, 10), mfaText)
}

func (store *authStore) sessOK(sessionHash []byte, userID, created int64, mfa sql.NullInt64, sig []byte) bool {
	return hmac.Equal(sig, store.sessSig(sessionHash, userID, created, mfa))
}

func (store *authStore) NewSession(userID int64, ip, userAgent string) (string, error) {
	rawValue, sessionHash := store.newSessionValue()
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	now := authNow().Unix()
	_, err := store.db.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,ip,ua,sig) VALUES(?,?,?,?,?,?,?,?)`,
		sessionHash, userID, now, now, now+int64(sessIdle/time.Second), ip, userAgent, store.sessSig(sessionHash, userID, now, sql.NullInt64{}))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(rawValue), nil
}

func (store *authStore) Session(cookie string) (*Principal, error) {
	return store.session(cookie, true)
}

// SessionPassive valide sans prolonger la fenêtre d'inactivité : un onglet qui sonde seul ne garde pas la session vivante.
func (store *authStore) SessionPassive(cookie string) (*Principal, error) {
	return store.session(cookie, false)
}

func (store *authStore) session(cookie string, touch bool) (*Principal, error) {
	rawValue, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil || len(rawValue) != sessionValueLen {
		return nil, errNoSession
	}
	sessionHash := store.sessHash(rawValue)
	var userID, created, lastSeen, expiresAt int64
	var mfa sql.NullInt64
	var sig []byte
	err = store.db.QueryRow(`SELECT user_id,created_at,last_seen_at,expires_at,mfa_at,sig FROM sessions WHERE id_hash=?`, sessionHash).Scan(&userID, &created, &lastSeen, &expiresAt, &mfa, &sig)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSession
	}
	if err != nil {
		return nil, err
	}
	if !store.sessOK(sessionHash, userID, created, mfa, sig) {
		store.guard(&tamperErr{userID, "sessions"})
		// Retirée après journalisation : un cookie rejoué ne remplit pas auth_events.
		store.db.Exec(`DELETE FROM sessions WHERE id_hash=? AND sig IS ?`, sessionHash, sig)
		return nil, errNoSession
	}
	now := authNow().Unix()
	absoluteExpiry := created + int64(sessAbsolute/time.Second)
	if now >= expiresAt || now >= absoluteExpiry || (!mfa.Valid && now >= created+int64(thMFAWindow/time.Second)) {
		if _, purgeErr := store.db.Exec(`DELETE FROM sessions WHERE id_hash=?`, sessionHash); purgeErr != nil {
			journal("auth: purge session: %v", purgeErr)
		}
		return nil, errNoSession
	}
	user, err := store.userByID(store.db, userID)
	if err = store.guard(err); err != nil || user.Disabled {
		return nil, errNoSession
	}
	if touch && now-lastSeen >= int64(sessTouch/time.Second) {
		if _, touchErr := store.db.Exec(`UPDATE sessions SET last_seen_at=?, expires_at=? WHERE id_hash=?`, now, min(now+int64(sessIdle/time.Second), absoluteExpiry), sessionHash); touchErr != nil {
			journal("auth: touch session: %v", touchErr)
		}
	}
	return &Principal{User: user, SessionID: hex.EncodeToString(sessionHash), MFA: mfa.Valid, CSRF: store.csrfFor(sessionHash), Expires: time.Unix(absoluteExpiry, 0)}, nil
}

func (store *authStore) csrfFor(idHash []byte) string {
	csrfKey, err := hkdf.Key(sha256.New, store.key, nil, "gld-auth-csrf", 32)
	if err != nil {
		panic(err)
	}
	mac := hmac.New(sha256.New, csrfKey)
	mac.Write(idHash)
	return hex.EncodeToString(mac.Sum(nil))
}

// PromoteMFA remplace la session partielle par une nouvelle valeur : un cookie capté avant
// le second facteur ne vaut rien après. created_at est conservé, donc l'échéance absolue aussi.
func (store *authStore) PromoteMFA(sessionID string) (cookie string, expiresAt time.Time, err error) {
	oldHash, err := hex.DecodeString(sessionID)
	if err != nil {
		return "", time.Time{}, errNoSession
	}
	rawValue, sessionHash := store.newSessionValue()
	now := authNow().Unix()
	err = store.inTx(func(transaction *sql.Tx) error {
		var userID, created int64
		var ip, userAgent string
		var sig []byte
		queryErr := transaction.QueryRow(`SELECT user_id,created_at,ip,ua,sig FROM sessions WHERE id_hash=? AND mfa_at IS NULL AND created_at > ?`,
			oldHash, now-int64(thMFAWindow/time.Second)).Scan(&userID, &created, &ip, &userAgent, &sig)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return errNoSession
		}
		if queryErr != nil {
			return queryErr
		}
		if !store.sessOK(oldHash, userID, created, sql.NullInt64{}, sig) {
			return &tamperErr{userID, "sessions"}
		}
		if _, deleteErr := transaction.Exec(`DELETE FROM sessions WHERE id_hash=?`, oldHash); deleteErr != nil {
			return deleteErr
		}
		absoluteExpiry := created + int64(sessAbsolute/time.Second)
		expiresAt = time.Unix(absoluteExpiry, 0)
		mfa := sql.NullInt64{Int64: now, Valid: true}
		_, queryErr = transaction.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,ip,ua,mfa_at,sig) VALUES(?,?,?,?,?,?,?,?,?)`,
			sessionHash, userID, created, now, min(now+int64(sessIdle/time.Second), absoluteExpiry), ip, userAgent, now, store.sessSig(sessionHash, userID, created, mfa))
		return queryErr
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return base64.RawURLEncoding.EncodeToString(rawValue), expiresAt, nil
}

// NewPairedSession : une session complète ouverte par un code de jumelage. Elle reprend le created_at de l'émettrice (même
// échéance absolue) et pointe vers l'émettrice d'origine : la supprimer supprime aussi ses sessions jumelées (trigger).
func (store *authStore) NewPairedSession(userID int64, parentID, ip, userAgent string) (cookie string, expiresAt time.Time, err error) {
	parentHash, err := hex.DecodeString(parentID)
	if err != nil {
		return "", time.Time{}, errNoSession
	}
	rawValue, sessionHash := store.newSessionValue()
	now := authNow().Unix()
	err = store.inTx(func(transaction *sql.Tx) error {
		var parentUserID, created int64
		var mfa sql.NullInt64
		var sig, rootHash []byte
		queryErr := transaction.QueryRow(`SELECT user_id,created_at,mfa_at,sig,parent_hash FROM sessions WHERE id_hash=? AND expires_at>?`, parentHash, now).
			Scan(&parentUserID, &created, &mfa, &sig, &rootHash)
		if errors.Is(queryErr, sql.ErrNoRows) || queryErr == nil && (parentUserID != userID || !mfa.Valid) {
			return errNoSession
		}
		if queryErr != nil {
			return queryErr
		}
		if !store.sessOK(parentHash, parentUserID, created, mfa, sig) {
			return &tamperErr{userID, "sessions"}
		}
		if rootHash == nil { // le trigger ne descend que d'un niveau : on rattache toujours à l'émettrice d'origine
			rootHash = parentHash
		}
		absoluteExpiry := created + int64(sessAbsolute/time.Second)
		expiresAt = time.Unix(absoluteExpiry, 0)
		mfaAt := sql.NullInt64{Int64: now, Valid: true}
		_, queryErr = transaction.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,ip,ua,mfa_at,parent_hash,sig) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			sessionHash, userID, created, now, min(now+int64(sessIdle/time.Second), absoluteExpiry), ip, userAgent, now, rootHash, store.sessSig(sessionHash, userID, created, mfaAt))
		return queryErr
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return base64.RawURLEncoding.EncodeToString(rawValue), expiresAt, nil
}

type sessRow struct {
	hash          []byte
	created, seen int64
	ip, ua        string
	mfa           bool
}

// userSessions : les sessions non expirées du compte ; une ligne à la signature invalide est écartée et journalisée.
func (store *authStore) userSessions(userID int64) ([]sessRow, error) {
	rows, err := store.db.Query(`SELECT id_hash,created_at,last_seen_at,ip,ua,mfa_at,sig FROM sessions WHERE user_id=? AND expires_at>? ORDER BY last_seen_at DESC`,
		userID, authNow().Unix())
	if err != nil {
		return nil, err
	}
	var sessions []sessRow
	sawTampered := false
	for rows.Next() {
		var session sessRow
		var mfa sql.NullInt64
		var sig []byte
		if scanErr := rows.Scan(&session.hash, &session.created, &session.seen, &session.ip, &session.ua, &mfa, &sig); scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		if !store.sessOK(session.hash, userID, session.created, mfa, sig) {
			sawTampered = true
			continue
		}
		session.mfa = mfa.Valid
		sessions = append(sessions, session)
	}
	err = rows.Err()
	rows.Close()
	if sawTampered {
		store.guard(&tamperErr{userID, "sessions"})
	}
	return sessions, err
}

// sessionOwner : le compte d'une session, signature vérifiée ; une ligne altérée vaut une session inconnue.
func (store *authStore) sessionOwner(sessionHash []byte) (int64, error) {
	var userID, created int64
	var mfa sql.NullInt64
	var sig []byte
	err := store.db.QueryRow(`SELECT user_id,created_at,mfa_at,sig FROM sessions WHERE id_hash=?`, sessionHash).Scan(&userID, &created, &mfa, &sig)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNoSession
	}
	if err != nil {
		return 0, err
	}
	if !store.sessOK(sessionHash, userID, created, mfa, sig) {
		store.guard(&tamperErr{userID, "sessions"})
		return 0, errNoSession
	}
	return userID, nil
}

// RevokeSession porte ce nom car authStore.Revoke retire déjà des droits de home_acl.
func (store *authStore) RevokeSession(sessionID string) error {
	sessionHash, err := hex.DecodeString(sessionID)
	if err != nil {
		return errNoSession
	}
	_, err = store.db.Exec(`DELETE FROM sessions WHERE id_hash=?`, sessionHash)
	return err
}

// RevokeOthers ferme toutes les sessions de l'utilisateur sauf celle dont l'identifiant est donné.
func (store *authStore) RevokeOthers(userID int64, keepID string) error {
	sessionHash, err := hex.DecodeString(keepID)
	if err != nil {
		return errNoSession
	}
	_, err = store.db.Exec(`DELETE FROM sessions WHERE user_id=? AND id_hash<>?`, userID, sessionHash)
	return err
}

func (store *authStore) RevokeUser(userID int64) error {
	_, err := store.db.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

func csrfOK(request *http.Request, principal *Principal) bool {
	switch request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	submittedToken := request.Header.Get(csrfHeader)
	return principal != nil && principal.CSRF != "" && subtle.ConstantTimeCompare([]byte(submittedToken), []byte(principal.CSRF)) == 1
}

// X-Forwarded-Proto n'est cru que du pair direct de confiance, comme X-Forwarded-For.
func requestHTTPS(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	if request.Header.Get("X-Forwarded-Proto") != "https" {
		return false
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	address, err := netip.ParseAddr(host)
	return err == nil && trustedProxy(address.Unmap().WithZone(""))
}

func setSessionCookie(responseWriter http.ResponseWriter, request *http.Request, value string, expiresAt time.Time) {
	http.SetCookie(responseWriter, &http.Cookie{Name: sessCookie, Value: value, Path: "/", Expires: expiresAt, HttpOnly: true, Secure: requestHTTPS(request), SameSite: http.SameSiteStrictMode})
}

func clearSessionCookie(responseWriter http.ResponseWriter) {
	http.SetCookie(responseWriter, &http.Cookie{Name: sessCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func clearHomeCookie(responseWriter http.ResponseWriter) {
	http.SetCookie(responseWriter, &http.Cookie{Name: homeCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
