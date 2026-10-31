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
	relyingPartyID, origins := "localhost", []string{"http://localhost:" + port}
	if publicURL := strings.TrimSpace(os.Getenv("GLD_PUBLIC_URL")); publicURL != "" {
		parsedURL, err := url.Parse(publicURL)
		host := ""
		if err == nil {
			host = strings.ToLower(parsedURL.Hostname())
		}
		if host == "" || net.ParseIP(host) != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return nil, errWebAuthnBadURL
		}
		if parsedURL.Scheme == "http" && host != "localhost" {
			return nil, errWebAuthnNeedsHTTPS
		}
		origin := parsedURL.Scheme + "://" + host
		if explicitPort := parsedURL.Port(); explicitPort != "" && !(parsedURL.Scheme == "https" && explicitPort == "443") && !(parsedURL.Scheme == "http" && explicitPort == "80") {
			origin += ":" + explicitPort
		}
		relyingPartyID, origins = host, []string{origin}
	}
	return webauthn.New(&webauthn.Config{RPDisplayName: "gld", RPID: relyingPartyID, RPOrigins: origins})
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

func waPut(purpose, sessionID string, userID int64, challenge *webauthn.SessionData) {
	now := authNow()
	waChallenges.Lock()
	defer waChallenges.Unlock()
	challengeKey := waKey(purpose, sessionID)
	if _, exists := waChallenges.m[challengeKey]; !exists && len(waChallenges.m) >= waMaxChallenges {
		for staleKey, pending := range waChallenges.m {
			if now.Sub(pending.at) > waChallengeTTL {
				delete(waChallenges.m, staleKey)
			}
		}
		// ponytail: recherche linéaire du plus ancien, seulement quand le plafond est atteint.
		for len(waChallenges.m) >= waMaxChallenges {
			oldest, isFirst := "", true
			for candidateKey, pending := range waChallenges.m {
				if isFirst || pending.at.Before(waChallenges.m[oldest].at) {
					oldest, isFirst = candidateKey, false
				}
			}
			delete(waChallenges.m, oldest)
		}
	}
	waChallenges.m[challengeKey] = waPending{userID, *challenge, now}
}

// waTake consomme le défi même s'il est refusé : un défi ne sert qu'une fois.
func waTake(purpose, sessionID string, userID int64) (webauthn.SessionData, error) {
	waChallenges.Lock()
	defer waChallenges.Unlock()
	challengeKey := waKey(purpose, sessionID)
	pending, found := waChallenges.m[challengeKey]
	delete(waChallenges.m, challengeKey)
	if !found || sessionID == "" || pending.uid != userID || authNow().Sub(pending.at) > waChallengeTTL {
		return webauthn.SessionData{}, errNoChallenge
	}
	return pending.data, nil
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

func (store *authStore) credSig(cred *credRow) []byte {
	return rowSig(store.key, "webauthn_creds", strconv.FormatInt(cred.uid, 10), string(cred.credID), string(cred.pub),
		strconv.FormatInt(cred.signCount, 10), cred.transports, cred.label, strconv.FormatInt(cred.createdAt, 10), strconv.FormatInt(cred.flags, 10))
}

type rowsQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

// loadCreds marque les lignes altérées sans échouer : chaque appelant décide de leur sort.
func (store *authStore) loadCreds(queryRunner rowsQuerier, userID int64) ([]*credRow, error) {
	rows, err := queryRunner.Query(`SELECT id,cred_id,public_key,sign_count,transports,label,created_at,last_used_at,flags,sig FROM webauthn_creds WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var creds []*credRow
	for rows.Next() {
		cred := &credRow{uid: userID}
		if scanErr := rows.Scan(&cred.id, &cred.credID, &cred.pub, &cred.signCount, &cred.transports, &cred.label, &cred.createdAt, &cred.lastUsed, &cred.flags, &cred.sig); scanErr != nil {
			return nil, scanErr
		}
		cred.bad = cred.signCount < 0 || cred.signCount > 0xFFFFFFFF || cred.flags < 0 || cred.flags > 0xFF || !rowSigOK(store.key, "webauthn_creds", strconv.FormatInt(userID, 10), cred.sig,
			string(cred.credID), string(cred.pub), strconv.FormatInt(cred.signCount, 10), cred.transports, cred.label, strconv.FormatInt(cred.createdAt, 10), strconv.FormatInt(cred.flags, 10))
		creds = append(creds, cred)
	}
	return creds, rows.Err()
}

// goodCreds refuse tout le jeu dès qu'une ligne est altérée : jamais de cérémonie sur une base douteuse.
func (store *authStore) goodCreds(queryRunner rowsQuerier, userID int64) ([]*credRow, error) {
	creds, err := store.loadCreds(queryRunner, userID)
	if err != nil {
		return nil, err
	}
	for _, cred := range creds {
		if cred.bad {
			return nil, &tamperErr{userID, "webauthn_creds"}
		}
	}
	return creds, nil
}

func (cred *credRow) credential() webauthn.Credential {
	var transports []protocol.AuthenticatorTransport
	for _, transport := range strings.Split(cred.transports, ",") {
		if transport != "" {
			transports = append(transports, protocol.AuthenticatorTransport(transport))
		}
	}
	return webauthn.Credential{
		ID:            cred.credID,
		PublicKey:     cred.pub,
		Transport:     transports,
		Flags:         webauthn.NewCredentialFlags(protocol.AuthenticatorFlags(cred.flags)),
		Authenticator: webauthn.Authenticator{SignCount: uint32(cred.signCount)},
	}
}

type waUser struct {
	handle []byte
	name   string
	creds  []webauthn.Credential
}

func (webAuthnUser *waUser) WebAuthnID() []byte                         { return webAuthnUser.handle }
func (webAuthnUser *waUser) WebAuthnName() string                       { return webAuthnUser.name }
func (webAuthnUser *waUser) WebAuthnDisplayName() string                { return webAuthnUser.name }
func (webAuthnUser *waUser) WebAuthnCredentials() []webauthn.Credential { return webAuthnUser.creds }

// waUserFor dérive un identifiant opaque et stable du compte, sans colonne dédiée ni fuite de l'id interne.
func (store *authStore) waUserFor(userID int64) (*waUser, []*credRow, error) {
	user, err := store.userByID(store.db, userID)
	if err != nil {
		return nil, nil, store.guard(err)
	}
	if user.Disabled {
		return nil, nil, errNoUser
	}
	creds, err := store.goodCreds(store.db, userID)
	if err != nil {
		return nil, nil, store.guard(err)
	}
	webAuthnUser := &waUser{handle: rowSig(store.key, "webauthn_user", strconv.FormatInt(userID, 10)), name: user.Name}
	for _, cred := range creds {
		webAuthnUser.creds = append(webAuthnUser.creds, cred.credential())
	}
	return webAuthnUser, creds, nil
}

func cleanLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) > 64 || !utf8.ValidString(label) || strings.IndexFunc(label, unicode.IsControl) >= 0 {
		return "", errBadLabel
	}
	return label, nil
}

func (store *authStore) WebAuthnBeginRegister(userID int64, sessionID string) (any, error) {
	if sessionID == "" {
		return nil, errNoChallenge
	}
	relyingParty, err := newWebAuthn()
	if err != nil {
		return nil, err
	}
	webAuthnUser, _, err := store.waUserFor(userID)
	if err != nil {
		return nil, err
	}
	// La vérification de l'utilisateur est exigée dès l'enrôlement : une clé incapable de la fournir serait refusée à la connexion.
	opts, sessionData, err := relyingParty.BeginRegistration(webAuthnUser,
		webauthn.WithExclusions(webauthn.Credentials(webAuthnUser.creds).CredentialDescriptors()),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementDiscouraged,
			UserVerification: protocol.VerificationRequired,
		}))
	if err != nil {
		return nil, err
	}
	waPut("register", sessionID, userID, sessionData)
	return opts, nil
}

func (store *authStore) WebAuthnFinishRegister(userID int64, sessionID, label string, request *http.Request) error {
	sessionData, err := waTake("register", sessionID, userID)
	if err != nil {
		return err
	}
	if label, err = cleanLabel(label); err != nil {
		return err
	}
	relyingParty, err := newWebAuthn()
	if err != nil {
		return err
	}
	webAuthnUser, _, err := store.waUserFor(userID)
	if err != nil {
		return err
	}
	credential, err := relyingParty.FinishRegistration(webAuthnUser, sessionData, request)
	if err != nil {
		journal("auth: webauthn register: %v", err)
		return errWebAuthnFailed
	}
	transports := make([]string, 0, len(credential.Transport))
	for _, transport := range credential.Transport {
		transports = append(transports, string(transport))
	}
	row := &credRow{uid: userID, credID: credential.ID, pub: credential.PublicKey, signCount: int64(credential.Authenticator.SignCount),
		transports: strings.Join(transports, ","), label: label, createdAt: authNow().Unix(), flags: int64(credential.Flags.ProtocolValue())}
	err = store.inTx(func(transaction *sql.Tx) error {
		user, txErr := store.userByID(transaction, userID)
		if txErr != nil {
			return txErr
		}
		_, txErr = transaction.Exec(`INSERT INTO webauthn_creds(user_id,cred_id,public_key,sign_count,transports,label,created_at,flags,sig) VALUES(?,?,?,?,?,?,?,?,?)`,
			userID, row.credID, row.pub, row.signCount, row.transports, row.label, row.createdAt, row.flags, store.credSig(row))
		var constraintErr interface{ Code() int }
		if errors.As(txErr, &constraintErr) && constraintErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return errWebAuthnFailed
		}
		if txErr != nil {
			return txErr
		}
		return store.markEnrolled(transaction, user)
	})
	if err != nil {
		return err
	}
	store.event(userID, "webauthn", "enrolled")
	return nil
}

func (store *authStore) WebAuthnBeginLogin(userID int64, sessionID string) (any, error) {
	if sessionID == "" {
		return nil, errNoChallenge
	}
	relyingParty, err := newWebAuthn()
	if err != nil {
		return nil, err
	}
	webAuthnUser, _, err := store.waUserFor(userID)
	if err != nil {
		return nil, err
	}
	if len(webAuthnUser.creds) == 0 {
		return nil, errWebAuthnFailed
	}
	opts, sessionData, err := relyingParty.BeginLogin(webAuthnUser, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, err
	}
	waPut("login", sessionID, userID, sessionData)
	return opts, nil
}

// WebAuthnFinishLogin refuse un compteur qui recule (clé clonée probable) et avance le compteur
// par un UPDATE conditionnel sur l'ancienne signature : de deux assertions concurrentes, une seule gagne.
func (store *authStore) WebAuthnFinishLogin(userID int64, sessionID string, request *http.Request) error {
	sessionData, err := waTake("login", sessionID, userID)
	if err != nil {
		return err
	}
	relyingParty, err := newWebAuthn()
	if err != nil {
		return err
	}
	webAuthnUser, creds, err := store.waUserFor(userID)
	if err != nil {
		return err
	}
	credential, err := relyingParty.FinishLogin(webAuthnUser, sessionData, request)
	if err != nil {
		journal("auth: webauthn login: %v", err)
		return errWebAuthnFailed
	}
	if credential.Authenticator.CloneWarning {
		store.event(userID, "webauthn", "clone")
		return errWebAuthnFailed
	}
	var matchedCred *credRow
	for _, candidate := range creds {
		if string(candidate.credID) == string(credential.ID) {
			matchedCred = candidate
		}
	}
	if matchedCred == nil {
		return errWebAuthnFailed
	}
	updatedCred := *matchedCred
	updatedCred.signCount, updatedCred.flags = int64(credential.Authenticator.SignCount), int64(credential.Flags.ProtocolValue())
	err = store.inTx(func(transaction *sql.Tx) error {
		result, txErr := transaction.Exec(`UPDATE webauthn_creds SET sign_count=?, flags=?, last_used_at=?, sig=? WHERE user_id=? AND cred_id=? AND sig=?`,
			updatedCred.signCount, updatedCred.flags, authNow().Unix(), store.credSig(&updatedCred), userID, matchedCred.credID, matchedCred.sig)
		if txErr != nil {
			return txErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
			return affectedErr
		} else if affected == 0 {
			return errWebAuthnFailed
		}
		return nil
	})
	if err != nil {
		return err
	}
	store.event(userID, "webauthn", "used")
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

func waTamperFirstSeen(cred *credRow) bool {
	seenKey := strconv.FormatInt(cred.id, 10) + "\x00" + string(cred.sig)
	waTamperSeen.Lock()
	defer waTamperSeen.Unlock()
	if waTamperSeen.m[seenKey] {
		return false
	}
	waTamperSeen.m[seenKey] = true
	return true
}

func (store *authStore) WebAuthnList(userID int64) ([]CredInfo, error) {
	creds, err := store.loadCreds(store.db, userID)
	if err != nil {
		return nil, err
	}
	infos := make([]CredInfo, 0, len(creds))
	for _, cred := range creds {
		infos = append(infos, CredInfo{ID: cred.id, Label: cred.label, CreatedAt: cred.createdAt, LastUsedAt: cred.lastUsed.Int64, Bad: cred.bad})
		if cred.bad && waTamperFirstSeen(cred) {
			store.event(userID, "tamper", "webauthn_creds")
		}
	}
	return infos, nil
}

// WebAuthnRemove ne compte que les facteurs utilisables : TOTP confirmé et clés à la signature valide.
func (store *authStore) WebAuthnRemove(userID, credID int64) error {
	err := store.inTx(func(transaction *sql.Tx) error {
		if _, lookupErr := store.userByID(transaction, userID); lookupErr != nil {
			return lookupErr
		}
		creds, txErr := store.loadCreds(transaction, userID)
		if txErr != nil {
			return txErr
		}
		found, usableCount := false, 0
		for _, cred := range creds {
			if cred.id == credID {
				found = true
			} else if !cred.bad {
				usableCount++
			}
		}
		if !found {
			return errWebAuthnFailed
		}
		totp, txErr := store.loadTOTP(transaction, userID)
		if txErr != nil && !errors.Is(txErr, errNoTOTP) {
			return txErr
		}
		if txErr == nil && totp.confirmed.Valid {
			usableCount++
		}
		if usableCount == 0 {
			return errLastFactor
		}
		_, txErr = transaction.Exec(`DELETE FROM webauthn_creds WHERE id=? AND user_id=?`, credID, userID)
		return txErr
	})
	if err != nil {
		return err
	}
	store.event(userID, "webauthn", "removed")
	return nil
}
