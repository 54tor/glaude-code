package main

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	sqlite3 "modernc.org/sqlite/lib"
)

const waChallengeTTL = 5 * time.Minute

// Plafond du nombre de défis en attente : une rafale de Begin ne doit pas faire grossir la mémoire sans fin.
var waMaxChallenges = 1000

var (
	errWebAuthnNeedsHTTPS = terr("srv.auth.webauthnHTTPS")
	errWebAuthnBadURL     = terr("srv.auth.urlPubliqueInvalide")
	errWebAuthnFailed     = terr("srv.auth.cleRefusee")
	errNoChallenge        = terr("srv.auth.defiExpire")
	errLastFactor         = terr("srv.auth.dernierFacteur")
	errBadLabel           = terr("srv.auth.libelleInvalide")
)

// newWebAuthn lit GLD_PUBLIC_URL à chaque appel : l'origine doit suivre la configuration effective.
// Un RPID ne peut pas être une adresse IP et les navigateurs n'acceptent « localhost » que depuis localhost :
// sans variable, seul http://localhost:<port> est servi, et une IP dans GLD_PUBLIC_URL est refusée.
func newWebAuthn() (*webauthn.WebAuthn, error) {
	rpid, origins := "localhost", []string{"http://localhost:" + port}
	if raw := strings.TrimSpace(os.Getenv("GLD_PUBLIC_URL")); raw != "" {
		u, err := url.Parse(raw)
		h := ""
		if err == nil {
			h = strings.ToLower(u.Hostname())
		}
		if h == "" || net.ParseIP(h) != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errWebAuthnBadURL
		}
		if u.Scheme == "http" && h != "localhost" {
			return nil, errWebAuthnNeedsHTTPS
		}
		origin := u.Scheme + "://" + h
		if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
			origin += ":" + p
		}
		rpid, origins = h, []string{origin}
	}
	return webauthn.New(&webauthn.Config{RPDisplayName: "gld", RPID: rpid, RPOrigins: origins})
}

type waPending struct {
	uid  int64
	data webauthn.SessionData
	at   time.Time
}

// Défis en mémoire : un redémarrage les perd, ce qui oblige seulement à recommencer la cérémonie.
var waChallenges = struct {
	sync.Mutex
	m map[string]waPending
}{m: map[string]waPending{}}

func waKey(purpose, sessionID string) string { return purpose + "\x00" + sessionID }

func waPut(purpose, sessionID string, uid int64, d *webauthn.SessionData) {
	now := authNow()
	waChallenges.Lock()
	defer waChallenges.Unlock()
	k := waKey(purpose, sessionID)
	if _, ok := waChallenges.m[k]; !ok && len(waChallenges.m) >= waMaxChallenges {
		for k, p := range waChallenges.m {
			if now.Sub(p.at) > waChallengeTTL {
				delete(waChallenges.m, k)
			}
		}
		// ponytail: recherche linéaire du plus ancien, seulement quand le plafond est atteint.
		for len(waChallenges.m) >= waMaxChallenges {
			oldest, first := "", true
			for k, p := range waChallenges.m {
				if first || p.at.Before(waChallenges.m[oldest].at) {
					oldest, first = k, false
				}
			}
			delete(waChallenges.m, oldest)
		}
	}
	waChallenges.m[k] = waPending{uid, *d, now}
}

// waTake consomme le défi même s'il est refusé : un défi ne sert qu'une fois.
func waTake(purpose, sessionID string, uid int64) (webauthn.SessionData, error) {
	waChallenges.Lock()
	defer waChallenges.Unlock()
	k := waKey(purpose, sessionID)
	p, ok := waChallenges.m[k]
	delete(waChallenges.m, k)
	if !ok || sessionID == "" || p.uid != uid || authNow().Sub(p.at) > waChallengeTTL {
		return webauthn.SessionData{}, errNoChallenge
	}
	return p.data, nil
}

type credRow struct {
	id         int64
	uid        int64
	credID     []byte
	pub        []byte
	signCount  int64
	transports string
	label      string
	createdAt  int64
	lastUsed   sql.NullInt64
	flags      int64
	sig        []byte
	bad        bool
}

func (s *authStore) credSig(r *credRow) []byte {
	return rowSig(s.key, "webauthn_creds", strconv.FormatInt(r.uid, 10), string(r.credID), string(r.pub),
		strconv.FormatInt(r.signCount, 10), r.transports, r.label, strconv.FormatInt(r.createdAt, 10), strconv.FormatInt(r.flags, 10))
}

type rowsQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

// loadCreds marque les lignes altérées sans échouer : chaque appelant décide de leur sort.
func (s *authStore) loadCreds(q rowsQuerier, userID int64) ([]*credRow, error) {
	rows, err := q.Query(`SELECT id,cred_id,public_key,sign_count,transports,label,created_at,last_used_at,flags,sig FROM webauthn_creds WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*credRow
	for rows.Next() {
		r := &credRow{uid: userID}
		if err := rows.Scan(&r.id, &r.credID, &r.pub, &r.signCount, &r.transports, &r.label, &r.createdAt, &r.lastUsed, &r.flags, &r.sig); err != nil {
			return nil, err
		}
		r.bad = r.signCount < 0 || r.signCount > 0xFFFFFFFF || r.flags < 0 || r.flags > 0xFF || !rowSigOK(s.key, "webauthn_creds", strconv.FormatInt(userID, 10), r.sig,
			string(r.credID), string(r.pub), strconv.FormatInt(r.signCount, 10), r.transports, r.label, strconv.FormatInt(r.createdAt, 10), strconv.FormatInt(r.flags, 10))
		out = append(out, r)
	}
	return out, rows.Err()
}

// goodCreds refuse tout le jeu dès qu'une ligne est altérée : jamais de cérémonie sur une base douteuse.
func (s *authStore) goodCreds(q rowsQuerier, userID int64) ([]*credRow, error) {
	list, err := s.loadCreds(q, userID)
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		if r.bad {
			return nil, &tamperErr{userID, "webauthn_creds"}
		}
	}
	return list, nil
}

func (r *credRow) credential() webauthn.Credential {
	var tr []protocol.AuthenticatorTransport
	for _, t := range strings.Split(r.transports, ",") {
		if t != "" {
			tr = append(tr, protocol.AuthenticatorTransport(t))
		}
	}
	return webauthn.Credential{
		ID:            r.credID,
		PublicKey:     r.pub,
		Transport:     tr,
		Flags:         webauthn.NewCredentialFlags(protocol.AuthenticatorFlags(r.flags)),
		Authenticator: webauthn.Authenticator{SignCount: uint32(r.signCount)},
	}
}

type waUser struct {
	handle []byte
	name   string
	creds  []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.handle }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.name }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// waUserFor dérive un identifiant opaque et stable du compte, sans colonne dédiée ni fuite de l'id interne.
func (s *authStore) waUserFor(userID int64) (*waUser, []*credRow, error) {
	u, err := s.userByID(s.db, userID)
	if err != nil {
		return nil, nil, s.guard(err)
	}
	if u.Disabled {
		return nil, nil, errNoUser
	}
	list, err := s.goodCreds(s.db, userID)
	if err != nil {
		return nil, nil, s.guard(err)
	}
	wu := &waUser{handle: rowSig(s.key, "webauthn_user", strconv.FormatInt(userID, 10)), name: u.Name}
	for _, r := range list {
		wu.creds = append(wu.creds, r.credential())
	}
	return wu, list, nil
}

func cleanLabel(l string) (string, error) {
	l = strings.TrimSpace(l)
	if utf8.RuneCountInString(l) > 64 || !utf8.ValidString(l) || strings.IndexFunc(l, unicode.IsControl) >= 0 {
		return "", errBadLabel
	}
	return l, nil
}

func (s *authStore) WebAuthnBeginRegister(userID int64, sessionID string) (any, error) {
	if sessionID == "" {
		return nil, errNoChallenge
	}
	wa, err := newWebAuthn()
	if err != nil {
		return nil, err
	}
	wu, _, err := s.waUserFor(userID)
	if err != nil {
		return nil, err
	}
	// La vérification de l'utilisateur est exigée dès l'enrôlement : une clé incapable de la fournir serait refusée à la connexion.
	opts, sd, err := wa.BeginRegistration(wu,
		webauthn.WithExclusions(webauthn.Credentials(wu.creds).CredentialDescriptors()),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementDiscouraged,
			UserVerification: protocol.VerificationRequired,
		}))
	if err != nil {
		return nil, err
	}
	waPut("register", sessionID, userID, sd)
	return opts, nil
}

func (s *authStore) WebAuthnFinishRegister(userID int64, sessionID, label string, r *http.Request) error {
	sd, err := waTake("register", sessionID, userID)
	if err != nil {
		return err
	}
	if label, err = cleanLabel(label); err != nil {
		return err
	}
	wa, err := newWebAuthn()
	if err != nil {
		return err
	}
	wu, _, err := s.waUserFor(userID)
	if err != nil {
		return err
	}
	c, err := wa.FinishRegistration(wu, sd, r)
	if err != nil {
		journal("auth: webauthn register: %v", err)
		return errWebAuthnFailed
	}
	tr := make([]string, 0, len(c.Transport))
	for _, t := range c.Transport {
		tr = append(tr, string(t))
	}
	row := &credRow{uid: userID, credID: c.ID, pub: c.PublicKey, signCount: int64(c.Authenticator.SignCount),
		transports: strings.Join(tr, ","), label: label, createdAt: authNow().Unix(), flags: int64(c.Flags.ProtocolValue())}
	err = s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO webauthn_creds(user_id,cred_id,public_key,sign_count,transports,label,created_at,flags,sig) VALUES(?,?,?,?,?,?,?,?,?)`,
			userID, row.credID, row.pub, row.signCount, row.transports, row.label, row.createdAt, row.flags, s.credSig(row))
		var se interface{ Code() int }
		if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return errWebAuthnFailed
		}
		return err
	})
	if err != nil {
		return err
	}
	s.event(userID, "webauthn", "enrolled")
	return nil
}

func (s *authStore) WebAuthnBeginLogin(userID int64, sessionID string) (any, error) {
	if sessionID == "" {
		return nil, errNoChallenge
	}
	wa, err := newWebAuthn()
	if err != nil {
		return nil, err
	}
	wu, _, err := s.waUserFor(userID)
	if err != nil {
		return nil, err
	}
	if len(wu.creds) == 0 {
		return nil, errWebAuthnFailed
	}
	opts, sd, err := wa.BeginLogin(wu, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, err
	}
	waPut("login", sessionID, userID, sd)
	return opts, nil
}

// WebAuthnFinishLogin refuse un compteur qui recule (clé clonée probable) et avance le compteur
// par un UPDATE conditionnel sur l'ancienne signature : de deux assertions concurrentes, une seule gagne.
func (s *authStore) WebAuthnFinishLogin(userID int64, sessionID string, r *http.Request) error {
	sd, err := waTake("login", sessionID, userID)
	if err != nil {
		return err
	}
	wa, err := newWebAuthn()
	if err != nil {
		return err
	}
	wu, list, err := s.waUserFor(userID)
	if err != nil {
		return err
	}
	c, err := wa.FinishLogin(wu, sd, r)
	if err != nil {
		journal("auth: webauthn login: %v", err)
		return errWebAuthnFailed
	}
	if c.Authenticator.CloneWarning {
		s.event(userID, "webauthn", "clone")
		return errWebAuthnFailed
	}
	var old *credRow
	for _, row := range list {
		if string(row.credID) == string(c.ID) {
			old = row
		}
	}
	if old == nil {
		return errWebAuthnFailed
	}
	nr := *old
	nr.signCount, nr.flags = int64(c.Authenticator.SignCount), int64(c.Flags.ProtocolValue())
	err = s.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE webauthn_creds SET sign_count=?, flags=?, last_used_at=?, sig=? WHERE user_id=? AND cred_id=? AND sig=?`,
			nr.signCount, nr.flags, authNow().Unix(), s.credSig(&nr), userID, old.credID, old.sig)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return errWebAuthnFailed
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.event(userID, "webauthn", "used")
	return nil
}

type CredInfo struct {
	ID         int64
	Label      string
	CreatedAt  int64
	LastUsedAt int64
	// Bad signale une ligne altérée : inutilisable, mais l'utilisateur doit pouvoir la retirer.
	Bad bool
}

// Une ligne altérée n'est journalisée qu'une fois par processus depuis la liste : la page de profil la relit à chaque affichage.
var waTamperSeen = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

func waTamperFirstSeen(r *credRow) bool {
	k := strconv.FormatInt(r.id, 10) + "\x00" + string(r.sig)
	waTamperSeen.Lock()
	defer waTamperSeen.Unlock()
	if waTamperSeen.m[k] {
		return false
	}
	waTamperSeen.m[k] = true
	return true
}

func (s *authStore) WebAuthnList(userID int64) ([]CredInfo, error) {
	list, err := s.loadCreds(s.db, userID)
	if err != nil {
		return nil, err
	}
	out := make([]CredInfo, 0, len(list))
	for _, r := range list {
		out = append(out, CredInfo{ID: r.id, Label: r.label, CreatedAt: r.createdAt, LastUsedAt: r.lastUsed.Int64, Bad: r.bad})
		if r.bad && waTamperFirstSeen(r) {
			s.event(userID, "tamper", "webauthn_creds")
		}
	}
	return out, nil
}

// WebAuthnRemove ne compte que les facteurs utilisables : TOTP confirmé et clés à la signature valide.
func (s *authStore) WebAuthnRemove(userID, id int64) error {
	err := s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		list, err := s.loadCreds(tx, userID)
		if err != nil {
			return err
		}
		found, left := false, 0
		for _, r := range list {
			if r.id == id {
				found = true
			} else if !r.bad {
				left++
			}
		}
		if !found {
			return errWebAuthnFailed
		}
		t, err := s.loadTOTP(tx, userID)
		if err != nil && !errors.Is(err, errNoTOTP) {
			return err
		}
		if err == nil && t.confirmed.Valid {
			left++
		}
		if left == 0 {
			return errLastFactor
		}
		_, err = tx.Exec(`DELETE FROM webauthn_creds WHERE id=? AND user_id=?`, id, userID)
		return err
	})
	if err != nil {
		return err
	}
	s.event(userID, "webauthn", "removed")
	return nil
}
