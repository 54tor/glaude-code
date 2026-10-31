package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
)

// softKey est un authentificateur logiciel ES256 : la bibliothèque vérifie réellement ses signatures.
type softKey struct {
	priv   *ecdsa.PrivateKey
	credID []byte
	count  uint32
	origin string
}

func newSoftKey(t *testing.T) *softKey {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &softKey{priv: privateKey, credID: id, origin: "http://localhost:" + port}
}

var b64 = base64.RawURLEncoding.EncodeToString

func (localSoftKey *softKey) authData(rpID string, flags protocol.AuthenticatorFlags) []byte {
	data := sha256.Sum256([]byte(rpID))
	localData := append(data[:], byte(flags))
	return binary.BigEndian.AppendUint32(localData, localSoftKey.count)
}

func jsonReq(t *testing.T, value any) *http.Request {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest("POST", "/", bytes.NewReader(data))
}

func (localSoftKey *softKey) clientData(typ, challenge string) []byte {
	data, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": localSoftKey.origin})
	return data
}

func (localSoftKey *softKey) attest(t *testing.T, opts any) *http.Request {
	t.Helper()
	cc := opts.(*protocol.CredentialCreation)
	pub, err := localSoftKey.priv.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         int64(webauthncose.P256), XCoord: pub[1:33], YCoord: pub[33:],
	})
	if err != nil {
		t.Fatal(err)
	}
	ad := localSoftKey.authData(cc.Response.RelyingParty.ID, protocol.FlagUserPresent|protocol.FlagUserVerified|protocol.FlagAttestedCredentialData)
	ad = append(ad, make([]byte, 16)...)
	ad = binary.BigEndian.AppendUint16(ad, uint16(len(localSoftKey.credID)))
	ad = append(append(ad, localSoftKey.credID...), cose...)
	att, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	if err != nil {
		t.Fatal(err)
	}
	return jsonReq(t, map[string]any{"id": b64(localSoftKey.credID), "rawId": b64(localSoftKey.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64(localSoftKey.clientData("webauthn.create", cc.Response.Challenge.String())), "attestationObject": b64(att)}})
}

func (localSoftKey *softKey) assert(t *testing.T, opts any) *http.Request {
	t.Helper()
	ca := opts.(*protocol.CredentialAssertion)
	if ca.Response.UserVerification != protocol.VerificationRequired {
		t.Fatalf("login user verification = %q", ca.Response.UserVerification)
	}
	ad := localSoftKey.authData(ca.Response.RelyingPartyID, protocol.FlagUserPresent|protocol.FlagUserVerified)
	cd := localSoftKey.clientData("webauthn.get", ca.Response.Challenge.String())
	ch := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), ch[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, localSoftKey.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return jsonReq(t, map[string]any{"id": b64(localSoftKey.credID), "rawId": b64(localSoftKey.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64(cd), "authenticatorData": b64(ad), "signature": b64(sig)}})
}

func waRegister(t *testing.T, localAuthStore *authStore, id int64, localSoftKey *softKey, sess string) error {
	t.Helper()
	opts, err := localAuthStore.WebAuthnBeginRegister(id, sess)
	if err != nil {
		t.Fatal(err)
	}
	return localAuthStore.WebAuthnFinishRegister(id, sess, "clé", localSoftKey.attest(t, opts))
}

func waLogin(t *testing.T, localAuthStore *authStore, id int64, localSoftKey *softKey, sess string) error {
	t.Helper()
	opts, err := localAuthStore.WebAuthnBeginLogin(id, sess)
	if err != nil {
		t.Fatal(err)
	}
	return localAuthStore.WebAuthnFinishLogin(id, sess, localSoftKey.assert(t, opts))
}

func waEvents(t *testing.T, localAuthStore *authStore, outcome string) int {
	t.Helper()
	var count int
	if err := localAuthStore.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='webauthn' AND outcome=?`, outcome).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func enrolledKey(t *testing.T) (*authStore, int64, *softKey, *time.Time) {
	t.Helper()
	now := testClock(t)
	localAuthStore, id := sessUser(t)
	localSoftKey := newSoftKey(t)
	if err := waRegister(t, localAuthStore, id, localSoftKey, "s1"); err != nil {
		t.Fatal(err)
	}
	return localAuthStore, id, localSoftKey, now
}

func TestWebAuthnRegisterLogin(t *testing.T) {
	authStore, id, softKey, _ := enrolledKey(t)
	if waEvents(t, authStore, "enrolled") != 1 {
		t.Fatal("enrolment not logged")
	}
	if user, err := authStore.userByID(authStore.db, id); err != nil || !user.MFAEnrolled {
		t.Fatalf("mfa_enrolled non posé : %+v %v", user, err)
	}
	opts, err := authStore.WebAuthnBeginRegister(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	ex := opts.(*protocol.CredentialCreation).Response.CredentialExcludeList
	if len(ex) != 1 || !bytes.Equal(ex[0].CredentialID, softKey.credID) {
		t.Fatalf("exclusions = %v", ex)
	}
	for index := 1; index <= 2; index++ {
		softKey.count = uint32(index)
		if err := waLogin(t, authStore, id, softKey, "s1"); err != nil {
			t.Fatalf("login %d: %v", index, err)
		}
	}
	var localCount int64
	var used *int64
	if err := authStore.db.QueryRow(`SELECT sign_count,last_used_at FROM webauthn_creds WHERE user_id=?`, id).Scan(&localCount, &used); err != nil || localCount != 2 || used == nil {
		t.Fatalf("sign_count %d last_used %v: %v", localCount, used, err)
	}
	credInfos, err := authStore.WebAuthnList(id)
	if err != nil || len(credInfos) != 1 || credInfos[0].Label != "clé" || credInfos[0].Bad || credInfos[0].LastUsedAt == 0 {
		t.Fatalf("list %+v: %v", credInfos, err)
	}
	other := newSoftKey(t)
	other.credID = softKey.credID
	if err := waLogin(t, authStore, id, other, "s1"); !errors.Is(err, errWebAuthnFailed) {
		t.Fatalf("assertion signed by another key: %v", err)
	}
}

func TestWebAuthnCounterRegression(t *testing.T) {
	authStore, id, softKey, _ := enrolledKey(t)
	softKey.count = 5
	if err := waLogin(t, authStore, id, softKey, "s1"); err != nil {
		t.Fatal(err)
	}
	for _, counter := range []uint32{5, 3, 0} {
		softKey.count = counter
		if err := waLogin(t, authStore, id, softKey, "s1"); !errors.Is(err, errWebAuthnFailed) {
			t.Fatalf("counter %d accepted: %v", counter, err)
		}
	}
	if waEvents(t, authStore, "clone") != 3 {
		t.Fatal("clone not logged")
	}
	softKey.count = 6
	if err := waLogin(t, authStore, id, softKey, "s1"); err != nil {
		t.Fatalf("higher counter refused: %v", err)
	}

	localSoftKey := newSoftKey(t)
	if err := waRegister(t, authStore, id, localSoftKey, "s2"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := waLogin(t, authStore, id, localSoftKey, "s2"); err != nil {
			t.Fatalf("authenticator without counter refused: %v", err)
		}
	}
}

func TestWebAuthnChallengeExpiry(t *testing.T) {
	authStore, id, softKey, now := enrolledKey(t)
	softKey.count = 1
	opts, err := authStore.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(waChallengeTTL + time.Second)
	if err := authStore.WebAuthnFinishLogin(id, "s1", softKey.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("expired challenge: %v", err)
	}
	opts, err = authStore.WebAuthnBeginRegister(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(waChallengeTTL + time.Second)
	if err := authStore.WebAuthnFinishRegister(id, "s1", "", newSoftKey(t).attest(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("expired registration challenge: %v", err)
	}
}

func TestWebAuthnChallengeOnce(t *testing.T) {
	authStore, id, softKey, _ := enrolledKey(t)
	softKey.count = 1
	opts, err := authStore.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := authStore.WebAuthnFinishLogin(id, "s1", softKey.assert(t, opts)); err != nil {
		t.Fatal(err)
	}
	softKey.count = 2
	if err := authStore.WebAuthnFinishLogin(id, "s1", softKey.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("challenge reused: %v", err)
	}
	opts, _ = authStore.WebAuthnBeginLogin(id, "s1")
	softKey.origin = "http://evil.localhost:" + port
	if err := authStore.WebAuthnFinishLogin(id, "s1", softKey.assert(t, opts)); !errors.Is(err, errWebAuthnFailed) {
		t.Fatal(err)
	}
	softKey.origin = "http://localhost:" + port
	if err := authStore.WebAuthnFinishLogin(id, "s1", softKey.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("challenge reused after a failure: %v", err)
	}
}

func TestWebAuthnChallengeBoundToSession(t *testing.T) {
	authStore, id, softKey, _ := enrolledKey(t)
	bob, err := authStore.AddUser("bob", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	softKey.count = 1
	opts, err := authStore.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := authStore.WebAuthnFinishLogin(id, "s2", softKey.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("other session: %v", err)
	}
	opts, _ = authStore.WebAuthnBeginLogin(id, "s1")
	if err := authStore.WebAuthnFinishLogin(bob, "s1", softKey.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("other user: %v", err)
	}
	if _, err := authStore.WebAuthnBeginLogin(id, ""); err == nil {
		t.Fatal("empty session id accepted")
	}
}

func TestWebAuthnWrongOrigin(t *testing.T) {
	testClock(t)
	authStore, id := sessUser(t)
	for _, output := range []string{"http://localhost:1", "https://localhost:" + port, "http://evil.example"} {
		softKey := newSoftKey(t)
		softKey.origin = output
		if err := waRegister(t, authStore, id, softKey, "s1"); !errors.Is(err, errWebAuthnFailed) {
			t.Fatalf("origin %s: %v", output, err)
		}
	}
	if credInfos, _ := authStore.WebAuthnList(id); len(credInfos) != 0 {
		t.Fatalf("credential stored: %+v", credInfos)
	}
}

func TestWebAuthnNeedsHTTPS(t *testing.T) {
	cases := []struct {
		url, rpid, origin string
		want              error
	}{
		{"", "localhost", "http://localhost:" + port, nil},
		{"https://gld.example.com", "gld.example.com", "https://gld.example.com", nil},
		{"https://GLD.Example.com", "gld.example.com", "https://gld.example.com", nil},
		{"https://gld.example.com:443", "gld.example.com", "https://gld.example.com", nil},
		{"https://gld.example.com:8443/x", "gld.example.com", "https://gld.example.com:8443", nil},
		{"http://localhost:9000", "localhost", "http://localhost:9000", nil},
		{"http://LocalHost:80", "localhost", "http://localhost", nil},
		{"http://127.0.0.1:9000", "", "", errWebAuthnBadURL},
		{"http://[::1]:9000", "", "", errWebAuthnBadURL},
		{"https://[::1]", "", "", errWebAuthnBadURL},
		{"http://gld.example.com", "", "", errWebAuthnNeedsHTTPS},
		{"http://192.168.1.10:8799", "", "", errWebAuthnBadURL},
		{"https://10.0.0.1", "", "", errWebAuthnBadURL},
		{"ftp://gld.example.com", "", "", errWebAuthnBadURL},
		{"gld.example.com", "", "", errWebAuthnBadURL},
	}
	for _, record := range cases {
		t.Setenv("GLD_PUBLIC_URL", record.url)
		wa, err := newWebAuthn()
		if !errors.Is(err, record.want) || (err == nil && (wa.Config.RPID != record.rpid || len(wa.Config.RPOrigins) != 1 || wa.Config.RPOrigins[0] != record.origin)) {
			t.Errorf("%q: %v %+v", record.url, err, wa)
		}
	}
	t.Setenv("GLD_PUBLIC_URL", "https://gld.example.com")
	testClock(t)
	authStore, id := sessUser(t)
	softKey := newSoftKey(t)
	softKey.origin = "https://gld.example.com"
	if err := waRegister(t, authStore, id, softKey, "s1"); err != nil {
		t.Fatal(err)
	}
	softKey.count = 1
	if err := waLogin(t, authStore, id, softKey, "s1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLD_PUBLIC_URL", "http://gld.example.com")
	if _, err := authStore.WebAuthnBeginLogin(id, "s1"); !errors.Is(err, errWebAuthnNeedsHTTPS) {
		t.Fatalf("begin over plain http: %v", err)
	}
}

func TestWebAuthnTamper(t *testing.T) {
	authStore, id, softKey, _ := enrolledKey(t)
	softKey.count = 1
	opts, err := authStore.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.db.Exec(`UPDATE webauthn_creds SET label='x' WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := authStore.WebAuthnFinishLogin(id, "s1", softKey.assert(t, opts)); !errors.Is(err, errTamper) || tamperEvents(t, authStore) != 1 {
		t.Fatalf("tampered row: %v", err)
	}
	if _, err := authStore.WebAuthnBeginLogin(id, "s1"); !errors.Is(err, errTamper) {
		t.Fatalf("begin on tampered row: %v", err)
	}
	if _, err := authStore.WebAuthnBeginRegister(id, "s1"); !errors.Is(err, errTamper) {
		t.Fatalf("register beside a tampered row: %v", err)
	}
	before := tamperEvents(t, authStore)
	for index := 0; index < 2; index++ {
		credInfos, err := authStore.WebAuthnList(id)
		if err != nil || len(credInfos) != 1 || !credInfos[0].Bad {
			t.Fatalf("list %+v: %v", credInfos, err)
		}
	}
	if count := tamperEvents(t, authStore) - before; count != 1 {
		t.Fatalf("tamper events from two lists = %d, want 1", count)
	}
	var used *int64
	authStore.db.QueryRow(`SELECT last_used_at FROM webauthn_creds WHERE user_id=?`, id).Scan(&used)
	if used != nil {
		t.Fatal("tampered row updated")
	}
}

func TestWebAuthnRemoveLastFactor(t *testing.T) {
	authStore, id, softKey, _ := enrolledKey(t)
	credInfos, _ := authStore.WebAuthnList(id)
	if err := authStore.WebAuthnRemove(id, credInfos[0].ID); !errors.Is(err, errLastFactor) {
		t.Fatalf("last key removed: %v", err)
	}
	if err := waRegister(t, authStore, id, newSoftKey(t), "s1"); err != nil {
		t.Fatal(err)
	}
	bob, _ := authStore.AddUser("bob", testPw, false)
	if err := authStore.WebAuthnRemove(bob, credInfos[0].ID); err == nil {
		t.Fatal("removed another user's key")
	}
	if err := authStore.WebAuthnRemove(id, credInfos[0].ID); err != nil {
		t.Fatal(err)
	}
	softKey.count = 1
	if err := waLogin(t, authStore, id, softKey, "s1"); !errors.Is(err, errWebAuthnFailed) {
		t.Fatalf("removed key still accepted: %v", err)
	}
	credInfos, _ = authStore.WebAuthnList(id)
	if err := authStore.WebAuthnRemove(id, credInfos[0].ID); !errors.Is(err, errLastFactor) {
		t.Fatalf("last key removed: %v", err)
	}

	s2, id2, _, _ := enrolled(t)
	if err := waRegister(t, s2, id2, newSoftKey(t), "s1"); err != nil {
		t.Fatal(err)
	}
	credInfos, _ = s2.WebAuthnList(id2)
	if err := s2.WebAuthnRemove(id2, credInfos[0].ID); err != nil {
		t.Fatalf("key removal with a confirmed TOTP: %v", err)
	}
}

func TestWebAuthnChallengeCap(t *testing.T) {
	now := testClock(t)
	oldMax, oldMap := waMaxChallenges, waChallenges.m
	waMaxChallenges, waChallenges.m = 3, map[string]waPending{}
	t.Cleanup(func() { waMaxChallenges, waChallenges.m = oldMax, oldMap })
	sd := &webauthn.SessionData{}
	waPut("login", "expired", 1, sd)
	*now = now.Add(waChallengeTTL + time.Second)
	for _, id := range []string{"a", "b", "c"} {
		waPut("login", id, 1, sd)
		*now = now.Add(time.Second)
	}
	if _, err := waTake("login", "expired", 1); err == nil {
		t.Fatal("expired entry kept over a fresh one")
	}
	waPut("login", "d", 1, sd)
	if len(waChallenges.m) != 3 {
		t.Fatalf("map size %d", len(waChallenges.m))
	}
	if _, err := waTake("login", "a", 1); err == nil {
		t.Fatal("oldest entry kept past the cap")
	}
	for _, id := range []string{"b", "c", "d"} {
		if _, err := waTake("login", id, 1); err != nil {
			t.Fatalf("%s dropped: %v", id, err)
		}
	}
}
