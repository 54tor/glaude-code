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
	p, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &softKey{priv: p, credID: id, origin: "http://localhost:" + port}
}

var b64 = base64.RawURLEncoding.EncodeToString

func (k *softKey) authData(rpID string, flags protocol.AuthenticatorFlags) []byte {
	h := sha256.Sum256([]byte(rpID))
	b := append(h[:], byte(flags))
	return binary.BigEndian.AppendUint32(b, k.count)
}

func jsonReq(t *testing.T, v any) *http.Request {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest("POST", "/", bytes.NewReader(b))
}

func (k *softKey) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": k.origin})
	return b
}

func (k *softKey) attest(t *testing.T, opts any) *http.Request {
	t.Helper()
	cc := opts.(*protocol.CredentialCreation)
	pub, err := k.priv.PublicKey.Bytes()
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
	ad := k.authData(cc.Response.RelyingParty.ID, protocol.FlagUserPresent|protocol.FlagUserVerified|protocol.FlagAttestedCredentialData)
	ad = append(ad, make([]byte, 16)...)
	ad = binary.BigEndian.AppendUint16(ad, uint16(len(k.credID)))
	ad = append(append(ad, k.credID...), cose...)
	att, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	if err != nil {
		t.Fatal(err)
	}
	return jsonReq(t, map[string]any{"id": b64(k.credID), "rawId": b64(k.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64(k.clientData("webauthn.create", cc.Response.Challenge.String())), "attestationObject": b64(att)}})
}

func (k *softKey) assert(t *testing.T, opts any) *http.Request {
	t.Helper()
	ca := opts.(*protocol.CredentialAssertion)
	if ca.Response.UserVerification != protocol.VerificationRequired {
		t.Fatalf("login user verification = %q", ca.Response.UserVerification)
	}
	ad := k.authData(ca.Response.RelyingPartyID, protocol.FlagUserPresent|protocol.FlagUserVerified)
	cd := k.clientData("webauthn.get", ca.Response.Challenge.String())
	ch := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), ch[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return jsonReq(t, map[string]any{"id": b64(k.credID), "rawId": b64(k.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64(cd), "authenticatorData": b64(ad), "signature": b64(sig)}})
}

func waRegister(t *testing.T, s *authStore, id int64, k *softKey, sess string) error {
	t.Helper()
	opts, err := s.WebAuthnBeginRegister(id, sess)
	if err != nil {
		t.Fatal(err)
	}
	return s.WebAuthnFinishRegister(id, sess, "clé", k.attest(t, opts))
}

func waLogin(t *testing.T, s *authStore, id int64, k *softKey, sess string) error {
	t.Helper()
	opts, err := s.WebAuthnBeginLogin(id, sess)
	if err != nil {
		t.Fatal(err)
	}
	return s.WebAuthnFinishLogin(id, sess, k.assert(t, opts))
}

func waEvents(t *testing.T, s *authStore, outcome string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='webauthn' AND outcome=?`, outcome).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func enrolledKey(t *testing.T) (*authStore, int64, *softKey, *time.Time) {
	t.Helper()
	now := testClock(t)
	s, id := sessUser(t)
	k := newSoftKey(t)
	if err := waRegister(t, s, id, k, "s1"); err != nil {
		t.Fatal(err)
	}
	return s, id, k, now
}

func TestWebAuthnRegisterLogin(t *testing.T) {
	s, id, k, _ := enrolledKey(t)
	if waEvents(t, s, "enrolled") != 1 {
		t.Fatal("enrolment not logged")
	}
	if u, err := s.userByID(s.db, id); err != nil || !u.MFAEnrolled {
		t.Fatalf("mfa_enrolled non posé : %+v %v", u, err)
	}
	opts, err := s.WebAuthnBeginRegister(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	ex := opts.(*protocol.CredentialCreation).Response.CredentialExcludeList
	if len(ex) != 1 || !bytes.Equal(ex[0].CredentialID, k.credID) {
		t.Fatalf("exclusions = %v", ex)
	}
	for i := 1; i <= 2; i++ {
		k.count = uint32(i)
		if err := waLogin(t, s, id, k, "s1"); err != nil {
			t.Fatalf("login %d: %v", i, err)
		}
	}
	var n int64
	var used *int64
	if err := s.db.QueryRow(`SELECT sign_count,last_used_at FROM webauthn_creds WHERE user_id=?`, id).Scan(&n, &used); err != nil || n != 2 || used == nil {
		t.Fatalf("sign_count %d last_used %v: %v", n, used, err)
	}
	l, err := s.WebAuthnList(id)
	if err != nil || len(l) != 1 || l[0].Label != "clé" || l[0].Bad || l[0].LastUsedAt == 0 {
		t.Fatalf("list %+v: %v", l, err)
	}
	other := newSoftKey(t)
	other.credID = k.credID
	if err := waLogin(t, s, id, other, "s1"); !errors.Is(err, errWebAuthnFailed) {
		t.Fatalf("assertion signed by another key: %v", err)
	}
}

func TestWebAuthnCounterRegression(t *testing.T) {
	s, id, k, _ := enrolledKey(t)
	k.count = 5
	if err := waLogin(t, s, id, k, "s1"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []uint32{5, 3, 0} {
		k.count = c
		if err := waLogin(t, s, id, k, "s1"); !errors.Is(err, errWebAuthnFailed) {
			t.Fatalf("counter %d accepted: %v", c, err)
		}
	}
	if waEvents(t, s, "clone") != 3 {
		t.Fatal("clone not logged")
	}
	k.count = 6
	if err := waLogin(t, s, id, k, "s1"); err != nil {
		t.Fatalf("higher counter refused: %v", err)
	}

	z := newSoftKey(t)
	if err := waRegister(t, s, id, z, "s2"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := waLogin(t, s, id, z, "s2"); err != nil {
			t.Fatalf("authenticator without counter refused: %v", err)
		}
	}
}

func TestWebAuthnChallengeExpiry(t *testing.T) {
	s, id, k, now := enrolledKey(t)
	k.count = 1
	opts, err := s.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(waChallengeTTL + time.Second)
	if err := s.WebAuthnFinishLogin(id, "s1", k.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("expired challenge: %v", err)
	}
	opts, err = s.WebAuthnBeginRegister(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(waChallengeTTL + time.Second)
	if err := s.WebAuthnFinishRegister(id, "s1", "", newSoftKey(t).attest(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("expired registration challenge: %v", err)
	}
}

func TestWebAuthnChallengeOnce(t *testing.T) {
	s, id, k, _ := enrolledKey(t)
	k.count = 1
	opts, err := s.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WebAuthnFinishLogin(id, "s1", k.assert(t, opts)); err != nil {
		t.Fatal(err)
	}
	k.count = 2
	if err := s.WebAuthnFinishLogin(id, "s1", k.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("challenge reused: %v", err)
	}
	opts, _ = s.WebAuthnBeginLogin(id, "s1")
	k.origin = "http://evil.localhost:" + port
	if err := s.WebAuthnFinishLogin(id, "s1", k.assert(t, opts)); !errors.Is(err, errWebAuthnFailed) {
		t.Fatal(err)
	}
	k.origin = "http://localhost:" + port
	if err := s.WebAuthnFinishLogin(id, "s1", k.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("challenge reused after a failure: %v", err)
	}
}

func TestWebAuthnChallengeBoundToSession(t *testing.T) {
	s, id, k, _ := enrolledKey(t)
	bob, err := s.AddUser("bob", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	k.count = 1
	opts, err := s.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WebAuthnFinishLogin(id, "s2", k.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("other session: %v", err)
	}
	opts, _ = s.WebAuthnBeginLogin(id, "s1")
	if err := s.WebAuthnFinishLogin(bob, "s1", k.assert(t, opts)); !errors.Is(err, errNoChallenge) {
		t.Fatalf("other user: %v", err)
	}
	if _, err := s.WebAuthnBeginLogin(id, ""); err == nil {
		t.Fatal("empty session id accepted")
	}
}

func TestWebAuthnWrongOrigin(t *testing.T) {
	testClock(t)
	s, id := sessUser(t)
	for _, o := range []string{"http://localhost:1", "https://localhost:" + port, "http://evil.example"} {
		k := newSoftKey(t)
		k.origin = o
		if err := waRegister(t, s, id, k, "s1"); !errors.Is(err, errWebAuthnFailed) {
			t.Fatalf("origin %s: %v", o, err)
		}
	}
	if l, _ := s.WebAuthnList(id); len(l) != 0 {
		t.Fatalf("credential stored: %+v", l)
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
	for _, c := range cases {
		t.Setenv("GLD_PUBLIC_URL", c.url)
		wa, err := newWebAuthn()
		if !errors.Is(err, c.want) || (err == nil && (wa.Config.RPID != c.rpid || len(wa.Config.RPOrigins) != 1 || wa.Config.RPOrigins[0] != c.origin)) {
			t.Errorf("%q: %v %+v", c.url, err, wa)
		}
	}
	t.Setenv("GLD_PUBLIC_URL", "https://gld.example.com")
	testClock(t)
	s, id := sessUser(t)
	k := newSoftKey(t)
	k.origin = "https://gld.example.com"
	if err := waRegister(t, s, id, k, "s1"); err != nil {
		t.Fatal(err)
	}
	k.count = 1
	if err := waLogin(t, s, id, k, "s1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLD_PUBLIC_URL", "http://gld.example.com")
	if _, err := s.WebAuthnBeginLogin(id, "s1"); !errors.Is(err, errWebAuthnNeedsHTTPS) {
		t.Fatalf("begin over plain http: %v", err)
	}
}

func TestWebAuthnTamper(t *testing.T) {
	s, id, k, _ := enrolledKey(t)
	k.count = 1
	opts, err := s.WebAuthnBeginLogin(id, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE webauthn_creds SET label='x' WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.WebAuthnFinishLogin(id, "s1", k.assert(t, opts)); !errors.Is(err, errTamper) || tamperEvents(t, s) != 1 {
		t.Fatalf("tampered row: %v", err)
	}
	if _, err := s.WebAuthnBeginLogin(id, "s1"); !errors.Is(err, errTamper) {
		t.Fatalf("begin on tampered row: %v", err)
	}
	if _, err := s.WebAuthnBeginRegister(id, "s1"); !errors.Is(err, errTamper) {
		t.Fatalf("register beside a tampered row: %v", err)
	}
	before := tamperEvents(t, s)
	for i := 0; i < 2; i++ {
		l, err := s.WebAuthnList(id)
		if err != nil || len(l) != 1 || !l[0].Bad {
			t.Fatalf("list %+v: %v", l, err)
		}
	}
	if n := tamperEvents(t, s) - before; n != 1 {
		t.Fatalf("tamper events from two lists = %d, want 1", n)
	}
	var used *int64
	s.db.QueryRow(`SELECT last_used_at FROM webauthn_creds WHERE user_id=?`, id).Scan(&used)
	if used != nil {
		t.Fatal("tampered row updated")
	}
}

func TestWebAuthnRemoveLastFactor(t *testing.T) {
	s, id, k, _ := enrolledKey(t)
	l, _ := s.WebAuthnList(id)
	if err := s.WebAuthnRemove(id, l[0].ID); !errors.Is(err, errLastFactor) {
		t.Fatalf("last key removed: %v", err)
	}
	if err := waRegister(t, s, id, newSoftKey(t), "s1"); err != nil {
		t.Fatal(err)
	}
	bob, _ := s.AddUser("bob", testPw, false)
	if err := s.WebAuthnRemove(bob, l[0].ID); err == nil {
		t.Fatal("removed another user's key")
	}
	if err := s.WebAuthnRemove(id, l[0].ID); err != nil {
		t.Fatal(err)
	}
	k.count = 1
	if err := waLogin(t, s, id, k, "s1"); !errors.Is(err, errWebAuthnFailed) {
		t.Fatalf("removed key still accepted: %v", err)
	}
	l, _ = s.WebAuthnList(id)
	if err := s.WebAuthnRemove(id, l[0].ID); !errors.Is(err, errLastFactor) {
		t.Fatalf("last key removed: %v", err)
	}

	s2, id2, _, _ := enrolled(t)
	if err := waRegister(t, s2, id2, newSoftKey(t), "s1"); err != nil {
		t.Fatal(err)
	}
	l, _ = s2.WebAuthnList(id2)
	if err := s2.WebAuthnRemove(id2, l[0].ID); err != nil {
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
