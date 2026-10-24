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

func (s *authStore) eventIP(userID int64, kind, outcome, ip string) {
	s.db.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`, authNow().Unix(), userID, ip, kind, outcome)
}

const (
	eventsKeep       = 90 * 24 * time.Hour
	eventsPurgeEvery = 24 * time.Hour
)

// PurgeEvents supprime en une seule instruction les événements plus anciens que olderThan.
func (s *authStore) PurgeEvents(olderThan time.Duration) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM auth_events WHERE at < ?`, authNow().Add(-olderThan).Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeSessions retire les sessions qu'aucun cookie ne peut plus rouvrir : échues, au-delà de l'échéance absolue,
// ou partielles au-delà de la fenêtre du second facteur.
func (s *authStore) PurgeSessions() (int64, error) {
	now := authNow().Unix()
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ? OR created_at <= ? OR (mfa_at IS NULL AND created_at <= ?)`,
		now, now-int64(sessAbsolute/time.Second), now-int64(thMFAWindow/time.Second))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// sessHash : HMAC avec la clé d'intégrité, pour qu'une ligne insérée sans la clé ne corresponde à aucun cookie.
func (s *authStore) sessHash(raw []byte) []byte {
	m := hmac.New(sha256.New, rowSigKey(s.key))
	m.Write([]byte("gld-session\x00"))
	m.Write(raw)
	return m.Sum(nil)
}

func (s *authStore) newSessionValue() (raw, hash []byte) {
	raw = make([]byte, 32)
	rand.Read(raw)
	return raw, s.sessHash(raw)
}

// expires_at et last_seen_at restent hors signature : l'échéance absolue dérive de created_at, signé.
func (s *authStore) sessSig(h []byte, uid, created int64, mfa sql.NullInt64) []byte {
	m := ""
	if mfa.Valid {
		m = strconv.FormatInt(mfa.Int64, 10)
	}
	return rowSig(s.key, "sessions", hex.EncodeToString(h), strconv.FormatInt(uid, 10), strconv.FormatInt(created, 10), m)
}

func (s *authStore) sessOK(h []byte, uid, created int64, mfa sql.NullInt64, sig []byte) bool {
	return hmac.Equal(sig, s.sessSig(h, uid, created, mfa))
}

func (s *authStore) NewSession(userID int64, ip, ua string) (string, error) {
	raw, h := s.newSessionValue()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	now := authNow().Unix()
	_, err := s.db.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,ip,ua,sig) VALUES(?,?,?,?,?,?,?,?)`,
		h, userID, now, now, now+int64(sessIdle/time.Second), ip, ua, s.sessSig(h, userID, now, sql.NullInt64{}))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *authStore) Session(cookie string) (*Principal, error) { return s.session(cookie, true) }

// SessionPassive valide sans prolonger la fenêtre d'inactivité : un onglet qui sonde seul ne garde pas la session vivante.
func (s *authStore) SessionPassive(cookie string) (*Principal, error) {
	return s.session(cookie, false)
}

func (s *authStore) session(cookie string, touch bool) (*Principal, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil || len(raw) != 32 {
		return nil, errNoSession
	}
	h := s.sessHash(raw)
	var uid, created, seen, exp int64
	var mfa sql.NullInt64
	var sig []byte
	err = s.db.QueryRow(`SELECT user_id,created_at,last_seen_at,expires_at,mfa_at,sig FROM sessions WHERE id_hash=?`, h).Scan(&uid, &created, &seen, &exp, &mfa, &sig)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSession
	}
	if err != nil {
		return nil, err
	}
	if !s.sessOK(h, uid, created, mfa, sig) {
		s.guard(&tamperErr{uid, "sessions"})
		// Retirée après journalisation : un cookie rejoué ne remplit pas auth_events.
		s.db.Exec(`DELETE FROM sessions WHERE id_hash=? AND sig IS ?`, h, sig)
		return nil, errNoSession
	}
	now := authNow().Unix()
	abs := created + int64(sessAbsolute/time.Second)
	if now >= exp || now >= abs || (!mfa.Valid && now >= created+int64(thMFAWindow/time.Second)) {
		if _, err := s.db.Exec(`DELETE FROM sessions WHERE id_hash=?`, h); err != nil {
			journal("auth: purge session: %v", err)
		}
		return nil, errNoSession
	}
	u, err := s.userByID(s.db, uid)
	if err = s.guard(err); err != nil || u.Disabled {
		return nil, errNoSession
	}
	if touch && now-seen >= int64(sessTouch/time.Second) {
		if _, err := s.db.Exec(`UPDATE sessions SET last_seen_at=?, expires_at=? WHERE id_hash=?`, now, min(now+int64(sessIdle/time.Second), abs), h); err != nil {
			journal("auth: touch session: %v", err)
		}
	}
	return &Principal{User: u, SessionID: hex.EncodeToString(h), MFA: mfa.Valid, CSRF: s.csrfFor(h), Expires: time.Unix(abs, 0)}, nil
}

func (s *authStore) csrfFor(idHash []byte) string {
	k, err := hkdf.Key(sha256.New, s.key, nil, "gld-auth-csrf", 32)
	if err != nil {
		panic(err)
	}
	m := hmac.New(sha256.New, k)
	m.Write(idHash)
	return hex.EncodeToString(m.Sum(nil))
}

// PromoteMFA remplace la session partielle par une nouvelle valeur : un cookie capté avant
// le second facteur ne vaut rien après. created_at est conservé, donc l'échéance absolue aussi.
func (s *authStore) PromoteMFA(sessionID string) (cookie string, exp time.Time, err error) {
	old, err := hex.DecodeString(sessionID)
	if err != nil {
		return "", time.Time{}, errNoSession
	}
	raw, h := s.newSessionValue()
	now := authNow().Unix()
	err = s.inTx(func(tx *sql.Tx) error {
		var uid, created int64
		var ip, ua string
		var sig []byte
		err := tx.QueryRow(`SELECT user_id,created_at,ip,ua,sig FROM sessions WHERE id_hash=? AND mfa_at IS NULL AND created_at > ?`,
			old, now-int64(thMFAWindow/time.Second)).Scan(&uid, &created, &ip, &ua, &sig)
		if errors.Is(err, sql.ErrNoRows) {
			return errNoSession
		}
		if err != nil {
			return err
		}
		if !s.sessOK(old, uid, created, sql.NullInt64{}, sig) {
			return &tamperErr{uid, "sessions"}
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE id_hash=?`, old); err != nil {
			return err
		}
		abs := created + int64(sessAbsolute/time.Second)
		exp = time.Unix(abs, 0)
		mfa := sql.NullInt64{Int64: now, Valid: true}
		_, err = tx.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,ip,ua,mfa_at,sig) VALUES(?,?,?,?,?,?,?,?,?)`,
			h, uid, created, now, min(now+int64(sessIdle/time.Second), abs), ip, ua, now, s.sessSig(h, uid, created, mfa))
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return base64.RawURLEncoding.EncodeToString(raw), exp, nil
}

type sessRow struct {
	hash          []byte
	created, seen int64
	ip, ua        string
	mfa           bool
}

// userSessions : les sessions non expirées du compte ; une ligne à la signature invalide est écartée et journalisée.
func (s *authStore) userSessions(uid int64) ([]sessRow, error) {
	rows, err := s.db.Query(`SELECT id_hash,created_at,last_seen_at,ip,ua,mfa_at,sig FROM sessions WHERE user_id=? AND expires_at>? ORDER BY last_seen_at DESC`,
		uid, authNow().Unix())
	if err != nil {
		return nil, err
	}
	var out []sessRow
	bad := false
	for rows.Next() {
		var r sessRow
		var mfa sql.NullInt64
		var sig []byte
		if err := rows.Scan(&r.hash, &r.created, &r.seen, &r.ip, &r.ua, &mfa, &sig); err != nil {
			rows.Close()
			return nil, err
		}
		if !s.sessOK(r.hash, uid, r.created, mfa, sig) {
			bad = true
			continue
		}
		r.mfa = mfa.Valid
		out = append(out, r)
	}
	err = rows.Err()
	rows.Close()
	if bad {
		s.guard(&tamperErr{uid, "sessions"})
	}
	return out, err
}

// sessionOwner : le compte d'une session, signature vérifiée ; une ligne altérée vaut une session inconnue.
func (s *authStore) sessionOwner(h []byte) (int64, error) {
	var uid, created int64
	var mfa sql.NullInt64
	var sig []byte
	err := s.db.QueryRow(`SELECT user_id,created_at,mfa_at,sig FROM sessions WHERE id_hash=?`, h).Scan(&uid, &created, &mfa, &sig)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNoSession
	}
	if err != nil {
		return 0, err
	}
	if !s.sessOK(h, uid, created, mfa, sig) {
		s.guard(&tamperErr{uid, "sessions"})
		return 0, errNoSession
	}
	return uid, nil
}

// RevokeSession porte ce nom car authStore.Revoke retire déjà des droits de home_acl.
func (s *authStore) RevokeSession(sessionID string) error {
	h, err := hex.DecodeString(sessionID)
	if err != nil {
		return errNoSession
	}
	_, err = s.db.Exec(`DELETE FROM sessions WHERE id_hash=?`, h)
	return err
}

// RevokeOthers ferme toutes les sessions de l'utilisateur sauf celle dont l'identifiant est donné.
func (s *authStore) RevokeOthers(userID int64, keepID string) error {
	h, err := hex.DecodeString(keepID)
	if err != nil {
		return errNoSession
	}
	_, err = s.db.Exec(`DELETE FROM sessions WHERE user_id=? AND id_hash<>?`, userID, h)
	return err
}

func (s *authStore) RevokeUser(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

func csrfOK(r *http.Request, p *Principal) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	got := r.Header.Get(csrfHeader)
	return p != nil && p.CSRF != "" && subtle.ConstantTimeCompare([]byte(got), []byte(p.CSRF)) == 1
}

// X-Forwarded-Proto n'est cru que du pair direct de confiance, comme X-Forwarded-For.
func requestHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") != "https" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	return err == nil && trustedProxy(a.Unmap().WithZone(""))
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, v string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{Name: sessCookie, Value: v, Path: "/", Expires: exp, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteStrictMode})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func clearHomeCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: homeCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
