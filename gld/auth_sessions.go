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
	s.db.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`, time.Now().Unix(), userID, ip, kind, outcome)
}

func (s *authStore) NewSession(userID int64, ip, ua string) (string, error) {
	raw := make([]byte, 32)
	rand.Read(raw)
	h := sha256.Sum256(raw)
	if len(ua) > 512 {
		ua = ua[:512]
	}
	now := authNow().Unix()
	_, err := s.db.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,ip,ua) VALUES(?,?,?,?,?,?,?)`,
		h[:], userID, now, now, now+int64(sessIdle/time.Second), ip, ua)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *authStore) Session(cookie string) (*Principal, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil || len(raw) != 32 {
		return nil, errNoSession
	}
	h := sha256.Sum256(raw)
	var uid, created, seen, exp int64
	var mfa sql.NullInt64
	err = s.db.QueryRow(`SELECT user_id,created_at,last_seen_at,expires_at,mfa_at FROM sessions WHERE id_hash=?`, h[:]).Scan(&uid, &created, &seen, &exp, &mfa)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSession
	}
	if err != nil {
		return nil, err
	}
	now := authNow().Unix()
	abs := created + int64(sessAbsolute/time.Second)
	if now >= exp || now >= abs || (!mfa.Valid && now >= created+int64(thMFAWindow/time.Second)) {
		s.db.Exec(`DELETE FROM sessions WHERE id_hash=?`, h[:])
		return nil, errNoSession
	}
	u, err := s.userByID(s.db, uid)
	if err = s.guard(err); err != nil || u.Disabled {
		return nil, errNoSession
	}
	if now-seen >= int64(sessTouch/time.Second) {
		s.db.Exec(`UPDATE sessions SET last_seen_at=?, expires_at=? WHERE id_hash=?`, now, min(now+int64(sessIdle/time.Second), abs), h[:])
	}
	return &Principal{User: u, SessionID: hex.EncodeToString(h[:]), MFA: mfa.Valid, CSRF: s.csrfFor(h[:]), Expires: time.Unix(abs, 0)}, nil
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

func (s *authStore) MarkMFA(sessionID string) error {
	h, err := hex.DecodeString(sessionID)
	if err != nil {
		return errNoSession
	}
	res, err := s.db.Exec(`UPDATE sessions SET mfa_at=? WHERE id_hash=?`, authNow().Unix(), h)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errNoSession
	}
	return nil
}

// RevokeSession porte ce nom car authStore.Revoke retire déjà des droits de home_acl.
func (s *authStore) RevokeSession(sessionID string) {
	if h, err := hex.DecodeString(sessionID); err == nil {
		s.db.Exec(`DELETE FROM sessions WHERE id_hash=?`, h)
	}
}

func (s *authStore) RevokeUser(userID int64) {
	s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
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
