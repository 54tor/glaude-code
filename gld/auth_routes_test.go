package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

type authRig struct {
	t     *testing.T
	s     *authStore
	th    *throttle
	h     http.Handler
	clock *time.Time
	uid   int64
}

func newAuthRig(t *testing.T) *authRig {
	t.Helper()
	now := testClock(t)
	s, id := sessUser(t)
	th := newThrottle(s.db)
	th.now = func() time.Time { return *now }
	mux := http.NewServeMux()
	routesAuth(mux, s, th)
	return &authRig{t: t, s: s, th: th, h: mux, clock: now, uid: id}
}

// argonCalls compte chaque calcul de mot de passe, réel ou factice.
func argonCalls(t *testing.T) *atomic.Int64 {
	var n atomic.Int64
	v, d := verifyPassword, dummyPassword
	verifyPassword = func(h, p string) bool { n.Add(1); return v(h, p) }
	dummyPassword = func() { n.Add(1); d() }
	t.Cleanup(func() { verifyPassword, dummyPassword = v, d })
	return &n
}

func (g *authRig) do(method, path, cookie, csrf string, body any) *httptest.ResponseRecorder {
	g.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			g.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: cookie})
	}
	if csrf != "" {
		r.Header.Set(csrfHeader, csrf)
	}
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	return w
}

// errMsg lit le message du corps d'erreur uniforme {"error":{"code","message"}}.
func errBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	e, _ := jbody(t, w)["error"].(map[string]any)
	return e
}

func errMsg(t *testing.T, w *httptest.ResponseRecorder) any {
	t.Helper()
	return errBody(t, w)["message"]
}

func fid(n int64) string { return strconv.FormatInt(n, 10) }

func jbody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("corps %q : %v", w.Body.String(), err)
	}
	return m
}

// newCookie renvoie la valeur posée (vide si absente) et si le cookie a été effacé.
func newCookie(w *httptest.ResponseRecorder) (v string, cleared bool) {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessCookie {
			return c.Value, c.MaxAge < 0
		}
	}
	return "", false
}

func (g *authRig) login(name, pw string) *httptest.ResponseRecorder {
	g.t.Helper()
	return g.do("POST", "/v1/auth/sessions", "", "", map[string]string{"name": name, "password": pw})
}

// partial ouvre une session après le mot de passe seul et renvoie cookie et jeton CSRF.
func (g *authRig) partial() (string, string, map[string]any) {
	g.t.Helper()
	w := g.login("alice", testPw)
	if w.Code != 200 {
		g.t.Fatalf("login : %d %s", w.Code, w.Body)
	}
	c, _ := newCookie(w)
	m := jbody(g.t, w)
	return c, m["csrf"].(string), m
}

func (g *authRig) enrolTOTP() string {
	g.t.Helper()
	secret, _, err := g.s.TOTPBegin(g.uid, "alice")
	if err != nil {
		g.t.Fatal(err)
	}
	if err := g.s.TOTPConfirm(g.uid, codeAt(g.t, secret, *g.clock)); err != nil {
		g.t.Fatal(err)
	}
	g.tick(totpPeriod * time.Second)
	return secret
}

func (g *authRig) tick(d time.Duration) { *g.clock = g.clock.Add(d) }

func (g *authRig) code(secret string) string {
	g.t.Helper()
	g.tick(totpPeriod * time.Second)
	return codeAt(g.t, secret, *g.clock)
}

func (g *authRig) full(secret string) (string, string) {
	g.t.Helper()
	c, csrf, _ := g.partial()
	w := g.do("POST", "/v1/auth/mfa/totp", c, csrf, map[string]string{"code": g.code(secret)})
	if w.Code != 200 {
		g.t.Fatalf("mfa : %d %s", w.Code, w.Body)
	}
	nc, _ := newCookie(w)
	return nc, jbody(g.t, w)["csrf"].(string)
}

func assertGenericFail(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != 401 || w.Header().Get("Retry-After") == "" || errMsg(t, w) != L("srv.auth.invalide") {
		t.Fatalf("échec non générique : %d %v %s", w.Code, w.Header(), w.Body)
	}
}

func headerKeys(h http.Header) []string {
	k := make([]string, 0, len(h))
	for n := range h {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}

func TestLoginUnknownVsWrong(t *testing.T) {
	g := newAuthRig(t)
	m, tt := argonMemKiB, argonTime
	argonMemKiB, argonTime = 8*1024, 1
	t.Cleanup(func() { argonMemKiB, argonTime = m, tt })
	if _, err := g.s.AddUser("bob", testPw, false); err != nil {
		t.Fatal(err)
	}
	median := func(name string) (time.Duration, *httptest.ResponseRecorder) {
		var ds []time.Duration
		var last *httptest.ResponseRecorder
		for i := 0; i < 5; i++ {
			g.tick(thWindow + time.Minute)
			t0 := time.Now()
			last = g.login(name, "wrong password here")
			ds = append(ds, time.Since(t0))
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		return ds[2], last
	}
	du, wu := median("nobody")
	dw, ww := median("bob")
	assertGenericFail(t, wu)
	if wu.Code != ww.Code || wu.Body.String() != ww.Body.String() ||
		!reflect.DeepEqual(headerKeys(wu.Header()), headerKeys(ww.Header())) || wu.Header().Get("Retry-After") != ww.Header().Get("Retry-After") {
		t.Fatalf("réponses distinctes :\n%d %v %s\n%d %v %s", wu.Code, wu.Header(), wu.Body, ww.Code, ww.Header(), ww.Body)
	}
	if du > 3*dw || dw > 3*du {
		t.Fatalf("durées inconnu %v / mauvais %v", du, dw)
	}
}

func TestLoginDisabledSameAsWrong(t *testing.T) {
	g := newAuthRig(t)
	wrong := g.login("alice", "wrong password here")
	g.tick(thWindow + time.Minute)
	if err := g.s.SetDisabled(g.uid, true); err != nil {
		t.Fatal(err)
	}
	dis := g.login("alice", testPw)
	assertGenericFail(t, dis)
	if c, _ := newCookie(dis); c != "" {
		t.Fatal("cookie posé pour un compte désactivé")
	}
	if dis.Body.String() != wrong.Body.String() || !reflect.DeepEqual(headerKeys(dis.Header()), headerKeys(wrong.Header())) {
		t.Fatalf("désactivé distinct de mauvais mot de passe : %v %s / %v %s", dis.Header(), dis.Body, wrong.Header(), wrong.Body)
	}
}

func TestLoginThrottled(t *testing.T) {
	g := newAuthRig(t)
	n := argonCalls(t)
	for i := 0; i < 3; i++ {
		assertGenericFail(t, g.login("alice", "wrong password here"))
	}
	if n.Load() != 3 {
		t.Fatalf("calculs = %d", n.Load())
	}
	w := g.login("alice", testPw)
	assertGenericFail(t, w)
	ra, _ := strconv.Atoi(w.Header().Get("Retry-After"))
	if ra < 1 {
		t.Fatalf("Retry-After = %q", w.Header().Get("Retry-After"))
	}
	if n.Load() != 3 {
		t.Fatal("mot de passe vérifié pendant l'attente")
	}
	g.tick(time.Duration(ra) * time.Second)
	if w := g.login("alice", testPw); w.Code != 200 {
		t.Fatalf("après l'attente : %d %s", w.Code, w.Body)
	}
}

func TestLoginPasswordLimits(t *testing.T) {
	g := newAuthRig(t)
	n := argonCalls(t)
	for _, pw := range []string{"", strings.Repeat("a", 5000), "correct horse\x00battery"} {
		assertGenericFail(t, g.login("alice", pw))
	}
	if n.Load() != 0 {
		t.Fatalf("%d calculs argon2", n.Load())
	}
	var fails int64
	if err := g.s.db.QueryRow(`SELECT fails FROM throttle WHERE key='user:alice'`).Scan(&fails); err != nil || fails != 3 {
		t.Fatalf("échecs comptés = %d (%v)", fails, err)
	}
	big := []byte(`{"name":"alice","password":"` + strings.Repeat("a", authBodyMax) + `"}`)
	if w := g.do("POST", "/v1/auth/sessions", "", "", big); w.Code != 400 || n.Load() != 0 {
		t.Fatalf("corps trop gros : %d", w.Code)
	}
	if w := g.do("POST", "/v1/auth/sessions", "", "", []byte(`{"name":"alice","password":"x","extra":1}`)); w.Code != 400 {
		t.Fatalf("champ inconnu : %d", w.Code)
	}
}

func TestLoginThenMFAGate(t *testing.T) {
	g := newAuthRig(t)
	g.enrolTOTP()
	c, csrf, m := g.partial()
	if !reflect.DeepEqual(m["mfa"], []any{"totp"}) || m["enroll"] != false {
		t.Fatalf("login = %v", m)
	}
	me := g.do("GET", "/v1/auth/me", c, "", nil)
	mb := jbody(t, me)
	if me.Code != 200 || mb["mfa"] != false || mb["csrf"] != csrf {
		t.Fatalf("me = %d %v", me.Code, mb)
	}
	for _, p := range []struct{ m, path string }{
		{"PUT", "/v1/auth/password"}, {"GET", "/v1/auth/sessions"}, {"POST", "/v1/auth/sessions/x/revocations"}, {"POST", "/v1/auth/factors/totp/removals"},
		{"POST", "/v1/auth/totp/enrollments"}, {"POST", "/v1/auth/webauthn/registrations"}, {"PUT", "/v1/auth/totp/enrollments/current"},
	} {
		if w := g.do(p.m, p.path, c, csrf, map[string]string{}); w.Code != 403 {
			t.Fatalf("%s %s en session partielle : %d %s", p.m, p.path, w.Code, w.Body)
		}
	}
	if w := g.do("GET", "/v1/auth/me", "", "", nil); w.Code != 401 || errCode(jbody(t, w)) != "unauthorized" {
		t.Fatalf("me sans session : %d", w.Code)
	}
}

func TestLoginFullTOTP(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c1, csrf, _ := g.partial()
	if w := g.do("POST", "/v1/auth/mfa/totp", c1, csrf, map[string]string{"code": "000000"}); w.Code != 401 {
		t.Fatalf("faux code : %d", w.Code)
	}
	w := g.do("POST", "/v1/auth/mfa/totp", c1, csrf, map[string]string{"code": g.code(secret)})
	if w.Code != 200 {
		t.Fatalf("mfa : %d %s", w.Code, w.Body)
	}
	c2, _ := newCookie(w)
	if c2 == "" || c2 == c1 {
		t.Fatal("cookie non renouvelé")
	}
	me := jbody(t, g.do("GET", "/v1/auth/me", c2, "", nil))
	if me["mfa"] != true || me["user"].(map[string]any)["name"] != "alice" {
		t.Fatalf("me = %v", me)
	}
	if w := g.do("GET", "/v1/auth/me", c1, "", nil); w.Code != 401 {
		t.Fatal("ancien cookie encore valide")
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND kind='login' AND outcome='success' AND ip='192.0.2.1'`, g.uid).Scan(&n)
	if n != 1 {
		t.Fatalf("événements login/success = %d", n)
	}
}

func TestLoginRecovery(t *testing.T) {
	g := newAuthRig(t)
	g.enrolTOTP()
	codes, err := g.s.NewRecoveryCodes(g.uid)
	if err != nil {
		t.Fatal(err)
	}
	c, csrf, m := g.partial()
	if !reflect.DeepEqual(m["mfa"], []any{"totp", "recovery"}) {
		t.Fatalf("méthodes = %v", m["mfa"])
	}
	w := g.do("POST", "/v1/auth/mfa/recovery", c, csrf, map[string]string{"code": codes[3]})
	if w.Code != 200 {
		t.Fatalf("secours : %d %s", w.Code, w.Body)
	}
	nc, _ := newCookie(w)
	if jbody(t, g.do("GET", "/v1/auth/me", nc, "", nil))["mfa"] != true {
		t.Fatal("session non complète")
	}
}

func TestMFAFailuresKillSession(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, csrf, _ := g.partial()
	c2, csrf2, _ := g.partial()
	ra := 0
	for i := 1; i <= thMFALimit; i++ {
		g.tick(time.Duration(ra) * time.Second)
		w := g.do("POST", "/v1/auth/mfa/totp", c, csrf, map[string]string{"code": "000000"})
		assertGenericFail(t, w)
		ra, _ = strconv.Atoi(w.Header().Get("Retry-After"))
		_, cleared := newCookie(w)
		if cleared != (i == thMFALimit) {
			t.Fatalf("essai %d : cookie effacé = %v (%s)", i, cleared, w.Body)
		}
	}
	// Chaque essai a été vérifié : les cinq échecs sont comptés, aucun n'a été refusé d'avance.
	if f := g.th.mfaFails("alice"); f != thMFALimit {
		t.Fatalf("échecs mfa comptés = %d", f)
	}
	if w := g.do("GET", "/v1/auth/me", c, "", nil); w.Code != 401 {
		t.Fatalf("session partielle survivante : %d", w.Code)
	}
	// Au-delà du seuil, même un bon code sur une autre session partielle n'est pas vérifié.
	w := g.do("POST", "/v1/auth/mfa/totp", c2, csrf2, map[string]string{"code": g.code(secret)})
	assertGenericFail(t, w)
	if _, cleared := newCookie(w); !cleared {
		t.Fatal("cookie non effacé")
	}
}

func TestAuthCSRFMissing(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, _, _ := g.partial()
	if w := g.do("POST", "/v1/auth/mfa/totp", c, "", map[string]string{"code": "000000"}); w.Code != 403 {
		t.Fatalf("mfa sans CSRF : %d", w.Code)
	}
	if w := g.do("POST", "/v1/auth/mfa/totp", c, "bad", map[string]string{"code": "000000"}); w.Code != 403 {
		t.Fatalf("mfa CSRF faux : %d", w.Code)
	}
	fc, _ := g.full(secret)
	if w := g.do("DELETE", "/v1/auth/sessions/current", fc, "", nil); w.Code != 403 {
		t.Fatalf("logout sans CSRF : %d", w.Code)
	}
	if w := g.do("DELETE", "/v1/auth/sessions/current", "", "x", nil); w.Code != 401 {
		t.Fatalf("logout sans session : %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, csrf := g.full(secret)
	w := g.do("DELETE", "/v1/auth/sessions/current", c, csrf, nil)
	if _, cleared := newCookie(w); w.Code != 204 || !cleared {
		t.Fatalf("logout : %d", w.Code)
	}
	if w := g.do("GET", "/v1/auth/me", c, "", nil); w.Code != 401 {
		t.Fatal("session survivante")
	}
}

func TestEnrollFirstLogin(t *testing.T) {
	g := newAuthRig(t)
	c, csrf, m := g.partial()
	if m["enroll"] != true || len(m["mfa"].([]any)) != 0 {
		t.Fatalf("login = %v", m)
	}
	if w := g.do("GET", "/v1/auth/sessions", c, "", nil); w.Code != 403 {
		t.Fatalf("sessions avant enrôlement : %d", w.Code)
	}
	w := g.do("POST", "/v1/auth/totp/enrollments", c, csrf, map[string]string{})
	if w.Code != 200 {
		t.Fatalf("begin : %d %s", w.Code, w.Body)
	}
	secret := jbody(t, w)["secret"].(string)
	if w := g.do("PUT", "/v1/auth/totp/enrollments/current", c, csrf, map[string]string{"code": "000000"}); w.Code != 400 {
		t.Fatalf("faux code : %d", w.Code)
	}
	w = g.do("PUT", "/v1/auth/totp/enrollments/current", c, csrf, map[string]string{"code": codeAt(t, secret, *g.clock)})
	if w.Code != 200 {
		t.Fatalf("confirm : %d %s", w.Code, w.Body)
	}
	b := jbody(t, w)
	if len(b["recovery"].([]any)) != recoveryCount {
		t.Fatalf("codes de secours = %v", b["recovery"])
	}
	nc, _ := newCookie(w)
	if nc == "" || nc == c || jbody(t, g.do("GET", "/v1/auth/me", nc, "", nil))["mfa"] != true {
		t.Fatal("session non promue")
	}
	// Un facteur existe désormais : une nouvelle session partielle ne peut plus enrôler.
	c2, csrf2, _ := g.partial()
	if w := g.do("POST", "/v1/auth/totp/enrollments", c2, csrf2, map[string]string{}); w.Code != 403 {
		t.Fatalf("enrôlement partiel avec facteur : %d", w.Code)
	}
}

func TestEnrollRequiresReauth(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, csrf := g.full(secret)
	assertGenericFail(t, g.do("POST", "/v1/auth/webauthn/registrations", c, csrf, map[string]string{}))
	g.tick(time.Minute)
	assertGenericFail(t, g.do("POST", "/v1/auth/webauthn/registrations", c, csrf, map[string]string{"password": testPw}))
	g.tick(time.Minute)
	assertGenericFail(t, g.do("POST", "/v1/auth/webauthn/registrations", c, csrf, map[string]string{"password": "wrong password here", "code": g.code(secret)}))
	g.tick(thWindow + time.Minute)
	w := g.do("POST", "/v1/auth/webauthn/registrations", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)})
	if w.Code != 200 || jbody(t, w)["options"] == nil {
		t.Fatalf("begin réauthentifié : %d %s", w.Code, w.Body)
	}
	w = g.do("POST", "/v1/auth/totp/enrollments", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)})
	if w.Code != 409 || errMsg(t, w) != L("srv.auth.totpDejaActif") {
		t.Fatalf("second TOTP : %d %s", w.Code, w.Body)
	}
}

// waReauth signe une assertion WebAuthn sur une session complète via les routes.
func (g *authRig) waReauth(k *softKey, c, csrf string) {
	g.t.Helper()
	w := g.do("POST", "/v1/auth/mfa/webauthn/challenges", c, csrf, nil)
	if w.Code != 200 {
		g.t.Fatalf("begin : %d %s", w.Code, w.Body)
	}
	var b struct{ Options protocol.CredentialAssertion }
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		g.t.Fatal(err)
	}
	k.count++
	raw, _ := io.ReadAll(k.assert(g.t, &b.Options).Body)
	if w := g.do("POST", "/v1/auth/mfa/webauthn/assertions", c, csrf, raw); w.Code != 200 {
		g.t.Fatalf("finish : %d %s", w.Code, w.Body)
	}
}

func TestFactorRemoveLast(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, csrf := g.full(secret)
	w := g.do("POST", "/v1/auth/factors/totp/removals", c, csrf, map[string]any{"password": testPw, "code": g.code(secret)})
	if w.Code != 409 || errMsg(t, w) != L("srv.auth.dernierFacteur") {
		t.Fatalf("retrait du dernier facteur : %d %s", w.Code, w.Body)
	}
	k := newSoftKey(t)
	if err := waRegister(t, g.s, g.uid, k, "x"); err != nil {
		t.Fatal(err)
	}
	if w := g.do("POST", "/v1/auth/factors/totp/removals", c, csrf, map[string]any{"password": testPw, "code": g.code(secret)}); w.Code != 204 {
		t.Fatalf("retrait TOTP : %d %s", w.Code, w.Body)
	}
	creds, _ := g.s.WebAuthnList(g.uid)
	g.waReauth(k, c, csrf)
	w = g.do("POST", "/v1/auth/factors/webauthn-"+strconv.FormatInt(creds[0].ID, 10)+"/removals", c, csrf, map[string]any{"password": testPw})
	if w.Code != 409 {
		t.Fatalf("retrait de la dernière clé : %d %s", w.Code, w.Body)
	}
	if f, err := g.s.factors(g.uid); err != nil || f.WebAuthn != 1 || f.TOTP {
		t.Fatalf("facteurs = %+v %v", f, err)
	}
}

func TestPasswordChangeRevokes(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	other, _ := fullSession(t, g.s, g.uid)
	c, csrf := g.full(secret)
	const newPw = "a much better passphrase"
	if w := g.do("PUT", "/v1/auth/password", c, csrf, map[string]string{"password": testPw, "code": g.code(secret), "new": "short"}); w.Code != 400 {
		t.Fatalf("mot de passe court : %d", w.Code)
	}
	w := g.do("PUT", "/v1/auth/password", c, csrf, map[string]string{"password": testPw, "code": g.code(secret), "new": newPw})
	if _, cleared := newCookie(w); w.Code != 204 || !cleared {
		t.Fatalf("changement : %d %s", w.Code, w.Body)
	}
	for _, s := range []string{c, other} {
		if _, err := g.s.Session(s); err == nil {
			t.Fatal("session survivante")
		}
	}
	if w := g.login("alice", newPw); w.Code != 200 {
		t.Fatalf("nouveau mot de passe : %d", w.Code)
	}
}

func TestSessionsListRevoke(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	_, other := fullSession(t, g.s, g.uid)
	c, csrf := g.full(secret)
	w := g.do("GET", "/v1/auth/sessions", c, "", nil)
	var b struct{ Sessions []map[string]any }
	json.Unmarshal(w.Body.Bytes(), &b)
	if w.Code != 200 || len(b.Sessions) != 2 {
		t.Fatalf("sessions : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/v1/auth/sessions/"+other.SessionID+"/revocations", c, csrf, map[string]string{"password": testPw}); w.Code != 401 {
		t.Fatalf("révocation sans facteur : %d", w.Code)
	}
	g.tick(time.Minute)
	if w := g.do("POST", "/v1/auth/sessions/"+other.SessionID+"/revocations", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)}); w.Code != 204 {
		t.Fatalf("révocation : %d %s", w.Code, w.Body)
	}
	if _, err := g.s.Session(c); err != nil {
		t.Fatal("session courante révoquée")
	}
	bob, err := g.s.AddUser("bob", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	bc, bp := fullSession(t, g.s, bob)
	if w := g.do("POST", "/v1/auth/sessions/"+bp.SessionID+"/revocations", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)}); w.Code != 404 {
		t.Fatalf("révocation de la session d'autrui : %d", w.Code)
	}
	if _, err := g.s.Session(bc); err != nil {
		t.Fatal("session d'autrui révoquée")
	}
	thirdC, _ := fullSession(t, g.s, g.uid)
	if w := g.do("DELETE", "/v1/auth/sessions", c, csrf, map[string]string{"password": testPw}); w.Code != 405 {
		t.Fatalf("l'ancienne route DELETE ne répond plus : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/v1/auth/sessions/revocations", c, csrf, map[string]string{"password": testPw}); w.Code != 401 {
		t.Fatalf("nettoyage sans facteur : %d", w.Code)
	}
	if w := g.do("POST", "/v1/auth/sessions/revocations", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)}); w.Code != 204 {
		t.Fatalf("nettoyage : %d %s", w.Code, w.Body)
	}
	if _, err := g.s.Session(c); err != nil {
		t.Fatal("session courante révoquée par le nettoyage")
	}
	if _, err := g.s.Session(thirdC); err == nil {
		t.Fatal("autre session de l'utilisateur survivante")
	}
	if _, err := g.s.Session(bc); err != nil {
		t.Fatal("le nettoyage a touché la session d'autrui")
	}
}

func TestRecoveryConcurrentSerialised(t *testing.T) {
	g := newAuthRig(t)
	g.enrolTOTP()
	codes, err := g.s.NewRecoveryCodes(g.uid)
	if err != nil {
		t.Fatal(err)
	}
	c, csrf, _ := g.partial()
	var calls atomic.Int64
	entered, release := make(chan struct{}, 2), make(chan struct{})
	orig := useRecovery
	useRecovery = func(s *authStore, uid int64, code string) bool {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return orig(s, uid, code)
	}
	t.Cleanup(func() { useRecovery = orig })
	var wg sync.WaitGroup
	var first *httptest.ResponseRecorder
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = g.do("POST", "/v1/auth/mfa/recovery", c, csrf, map[string]string{"code": codes[0]})
	}()
	<-entered
	assertGenericFail(t, g.do("POST", "/v1/auth/mfa/recovery", c, csrf, map[string]string{"code": codes[1]}))
	if calls.Load() != 1 {
		t.Fatalf("vérifications simultanées = %d", calls.Load())
	}
	close(release)
	wg.Wait()
	if first.Code != 200 {
		t.Fatalf("première tentative : %d %s", first.Code, first.Body)
	}
}

func TestRecoveryRegenerate(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	old, err := g.s.NewRecoveryCodes(g.uid)
	if err != nil {
		t.Fatal(err)
	}
	pc, pcsrf, _ := g.partial()
	if w := g.do("PUT", "/v1/auth/recovery-codes", pc, pcsrf, map[string]string{"password": testPw, "code": g.code(secret)}); w.Code != 403 {
		t.Fatalf("session partielle : %d", w.Code)
	}
	c, csrf := g.full(secret)
	assertGenericFail(t, g.do("PUT", "/v1/auth/recovery-codes", c, csrf, map[string]string{"password": testPw}))
	g.tick(2 * time.Second)
	w := g.do("PUT", "/v1/auth/recovery-codes", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)})
	if w.Code != 200 {
		t.Fatalf("régénération : %d %s", w.Code, w.Body)
	}
	codes := jbody(t, w)["recovery"].([]any)
	if len(codes) != recoveryCount {
		t.Fatalf("codes = %v", codes)
	}
	if g.s.UseRecoveryCode(g.uid, old[0]) {
		t.Fatal("ancien code encore valable")
	}
	if !g.s.UseRecoveryCode(g.uid, codes[0].(string)) {
		t.Fatal("nouveau code refusé")
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND kind='recovery' AND outcome='regenerated' AND ip='192.0.2.1'`, g.uid).Scan(&n)
	if n != 1 {
		t.Fatalf("événements = %d", n)
	}
}

// Vingt faux codes parallèles par vague : une seule vérification à la fois, et exactement cinq au total,
// car chaque vérification compte son échec avant de libérer la clé et le 6e détenteur trouve le seuil atteint.
func TestMFAParallelSerialised(t *testing.T) {
	g := newAuthRig(t)
	g.enrolTOTP()
	c, csrf, _ := g.partial()
	var active, peak, total atomic.Int64
	orig := verifyTOTP
	verifyTOTP = func(s *authStore, uid int64, code string, at time.Time) bool {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		total.Add(1)
		time.Sleep(2 * time.Millisecond)
		active.Add(-1)
		return orig(s, uid, code, at)
	}
	t.Cleanup(func() { verifyTOTP = orig })
	for round := 0; round < 8; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				g.do("POST", "/v1/auth/mfa/totp", c, csrf, map[string]string{"code": "000000"})
			}()
		}
		wg.Wait()
		if g.do("GET", "/v1/auth/me", c, "", nil).Code == 401 {
			break
		}
		g.tick(10 * time.Second)
	}
	if peak.Load() != 1 {
		t.Fatalf("vérifications simultanées : %d", peak.Load())
	}
	if total.Load() != thMFALimit || g.th.mfaFails("alice") != thMFALimit {
		t.Fatalf("vérifications %d, échecs comptés %d", total.Load(), g.th.mfaFails("alice"))
	}
	if g.do("GET", "/v1/auth/me", c, "", nil).Code != 401 {
		t.Fatal("session partielle survivante")
	}
}

func TestLoginBusyName(t *testing.T) {
	g := newAuthRig(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int64
	orig := verifyPassword
	verifyPassword = func(h, p string) bool {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return orig(h, p)
	}
	t.Cleanup(func() { verifyPassword = orig })
	var wg sync.WaitGroup
	var first *httptest.ResponseRecorder
	wg.Add(1)
	go func() { defer wg.Done(); first = g.login("alice", testPw) }()
	<-entered
	assertGenericFail(t, g.login("Alice", testPw))
	if calls.Load() != 1 {
		t.Fatalf("vérifications simultanées = %d", calls.Load())
	}
	close(release)
	wg.Wait()
	if first.Code != 200 {
		t.Fatalf("première connexion : %d", first.Code)
	}
}

func TestFirstEnrolRace(t *testing.T) {
	g := newAuthRig(t)
	c1, csrf1, _ := g.partial()
	c2, csrf2, _ := g.partial()
	w := g.do("POST", "/v1/auth/totp/enrollments", c1, csrf1, map[string]string{})
	secret := jbody(t, w)["secret"].(string)
	w = g.do("POST", "/v1/auth/webauthn/registrations", c2, csrf2, map[string]string{})
	if w.Code != 200 {
		t.Fatalf("webauthn begin : %d %s", w.Code, w.Body)
	}
	var b struct{ Options protocol.CredentialCreation }
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	k := newSoftKey(t)
	att, _ := io.ReadAll(k.attest(t, &b.Options).Body)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		codes[0] = g.do("PUT", "/v1/auth/totp/enrollments/current", c1, csrf1, map[string]string{"code": codeAt(t, secret, *g.clock)}).Code
	}()
	go func() {
		defer wg.Done()
		codes[1] = g.do("PUT", "/v1/auth/webauthn/registrations/current?label=cle", c2, csrf2, att).Code
	}()
	wg.Wait()
	sort.Ints(codes)
	// Le perdant voit soit le facteur déjà écrit (403), soit l'enrôlement encore en cours (409).
	if codes[0] != 200 || (codes[1] != 403 && codes[1] != 409) {
		t.Fatalf("statuts = %v", codes)
	}
	var sets int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND kind='recovery' AND outcome='generated'`, g.uid).Scan(&sets)
	if f, err := g.s.factors(g.uid); err != nil || sets != 1 || f.Recovery != recoveryCount || (f.TOTP == (f.WebAuthn == 1)) {
		t.Fatalf("jeux de codes %d, facteurs %+v %v", sets, f, err)
	}
}

func TestFactorRemoveTamperedTOTP(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	k := newSoftKey(t)
	if err := waRegister(t, g.s, g.uid, k, "x"); err != nil {
		t.Fatal(err)
	}
	c, csrf := g.full(secret)
	if _, err := g.s.db.Exec(`UPDATE totp SET last_step=last_step+1000 WHERE user_id=?`, g.uid); err != nil {
		t.Fatal(err)
	}
	before := tamperEvents(t, g.s)
	g.waReauth(k, c, csrf)
	w := g.do("POST", "/v1/auth/factors/totp/removals", c, csrf, map[string]any{"password": testPw})
	if w.Code != 409 || errMsg(t, w) != L("srv.auth.tamper") {
		t.Fatalf("retrait d'un TOTP altéré : %d %s", w.Code, w.Body)
	}
	if tamperEvents(t, g.s) <= before {
		t.Fatal("altération non journalisée")
	}
}

func TestAuthDeniedLogged(t *testing.T) {
	g := newAuthRig(t)
	count := func(kind string) int {
		var n int
		g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind=? AND outcome='denied' AND ip='192.0.2.1'`, kind).Scan(&n)
		return n
	}
	for i := 0; i < 2*rateBudget; i++ {
		g.do("DELETE", "/v1/auth/sessions/current", "", "", nil)
	}
	if count("auth") != rateBudget {
		t.Fatalf("refus journalisés = %d", count("auth"))
	}
	g.tick(time.Minute)
	g.do("GET", "/v1/auth/sessions", "", "", nil)
	if count("auth") != rateBudget+1 {
		t.Fatalf("après une minute = %d", count("auth"))
	}
	c, _, _ := g.partial()
	g.do("DELETE", "/v1/auth/sessions/current", c, "bad", nil)
	if count("csrf") != 1 {
		t.Fatalf("refus CSRF journalisés = %d", count("csrf"))
	}
}

type stallReader struct {
	started chan struct{}
	gate    chan struct{}
	once    sync.Once
	r       io.Reader
}

func (s *stallReader) Read(p []byte) (int, error) {
	s.once.Do(func() { close(s.started) })
	<-s.gate
	return s.r.Read(p)
}

// Un corps qui traîne sur le premier enrôlement d'alice ne retient pas celui de bob.
func TestFirstEnrolStalledBody(t *testing.T) {
	g := newAuthRig(t)
	if _, err := g.s.AddUser("bob", testPw, false); err != nil {
		t.Fatal(err)
	}
	c, csrf, _ := g.partial()
	w := g.do("POST", "/v1/auth/webauthn/registrations", c, csrf, map[string]string{})
	var b struct{ Options protocol.CredentialCreation }
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	att, _ := io.ReadAll(newSoftKey(t).attest(t, &b.Options).Body)
	sr := &stallReader{started: make(chan struct{}), gate: make(chan struct{}), r: bytes.NewReader(att)}
	r := httptest.NewRequest("PUT", "/v1/auth/webauthn/registrations/current?label=cle", sr)
	r.AddCookie(&http.Cookie{Name: sessCookie, Value: c})
	r.Header.Set(csrfHeader, csrf)
	slow := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { g.h.ServeHTTP(slow, r); close(done) }()
	<-sr.started

	fast := make(chan int, 1)
	go func() {
		lw := g.login("bob", testPw)
		bc, _ := newCookie(lw)
		bcsrf, _ := jbody(t, lw)["csrf"].(string)
		bw := g.do("POST", "/v1/auth/totp/enrollments", bc, bcsrf, map[string]string{})
		secret, _ := jbody(t, bw)["secret"].(string)
		fast <- g.do("PUT", "/v1/auth/totp/enrollments/current", bc, bcsrf, map[string]string{"code": codeAt(t, secret, *g.clock)}).Code
	}()
	select {
	case code := <-fast:
		if code != 200 {
			t.Fatalf("enrôlement de bob : %d", code)
		}
	case <-time.After(5 * time.Second):
		close(sr.gate)
		t.Fatal("enrôlement de bob bloqué par le corps lent d'alice")
	}
	close(sr.gate)
	<-done
	if slow.Code != 200 {
		t.Fatalf("enrôlement d'alice : %d %s", slow.Code, slow.Body)
	}
}

func TestEnrollFailedConfirmNoArgon(t *testing.T) {
	g := newAuthRig(t)
	var gens atomic.Int64
	orig := genRecovery
	genRecovery = func() ([]string, []string, error) { gens.Add(1); return orig() }
	t.Cleanup(func() { genRecovery = orig })
	n := argonCalls(t)
	c, csrf, _ := g.partial()
	secret := jbody(t, g.do("POST", "/v1/auth/totp/enrollments", c, csrf, map[string]string{}))["secret"].(string)
	before := n.Load()
	w := g.do("PUT", "/v1/auth/totp/enrollments/current", c, csrf, map[string]string{"code": "000000"})
	if w.Code != 400 || errMsg(t, w) != L("srv.auth.codeInvalide") || gens.Load() != 0 || n.Load() != before {
		t.Fatalf("faux code : %d %s, générations %d", w.Code, w.Body, gens.Load())
	}
	if w := g.do("PUT", "/v1/auth/totp/enrollments/current", c, csrf, map[string]string{"code": codeAt(t, secret, *g.clock)}); w.Code != 200 || gens.Load() != 1 {
		t.Fatalf("bon code : %d, générations %d", w.Code, gens.Load())
	}
}

func TestMeRightsHomesKeys(t *testing.T) {
	g := newAuthRig(t)
	old := root
	t.Cleanup(func() { root = old })
	use(t.TempDir())
	addHomes(t, "work", "perso")
	ids := map[string]string{}
	for _, h := range loadHomes() {
		ids[h.Name] = h.ID
	}
	bob, _ := g.s.AddUser("bob", testPw, false)
	carol, _ := g.s.AddUser("carol", testPw, false)
	g.s.Grant(g.uid, mainHomeID, RightMemRead, RightExec)
	g.s.Grant(g.uid, ids["work"], RightMemRead, RightMemWrite)
	g.s.Grant(bob, ids["perso"], RightMemRead)
	k := newSoftKey(t)
	if err := waRegister(t, g.s, g.uid, k, "x"); err != nil {
		t.Fatal(err)
	}
	me := func(uid int64) map[string]any {
		t.Helper()
		c, _ := fullSession(t, g.s, uid)
		w := g.do("GET", "/v1/auth/me", c, "", nil)
		if w.Code != 200 {
			t.Fatalf("me : %d %s", w.Code, w.Body)
		}
		return jbody(t, w)
	}
	m := me(g.uid)
	want := map[string]any{mainHomeID: []any{"mem_read", "exec"}, ids["work"]: []any{"mem_read", "mem_write"}}
	if !reflect.DeepEqual(m["rights"], want) {
		t.Fatalf("droits = %v", m["rights"])
	}
	homes := m["homes"].([]any)
	if len(homes) != 2 || homes[0].(map[string]any)["name"] != mainHomeID || homes[1].(map[string]any)["name"] != "work" {
		t.Fatalf("homes = %v", homes)
	}
	f := m["factors"].(map[string]any)
	keys := f["keys"].([]any)
	if f["webauthn"] != 1.0 || len(keys) != 1 || keys[0].(map[string]any)["label"] != "clé" || keys[0].(map[string]any)["id"] == nil {
		t.Fatalf("facteurs = %v", f)
	}
	if m := me(carol); !reflect.DeepEqual(m["rights"], map[string]any{}) || len(m["homes"].([]any)) != 0 {
		t.Fatalf("sans droit : %v %v", m["rights"], m["homes"])
	}
	if m := me(bob); !reflect.DeepEqual(m["rights"], map[string]any{ids["perso"]: []any{"mem_read"}}) {
		t.Fatalf("bob : %v", m["rights"])
	}
	before := tamperEvents(t, g.s)
	if _, err := g.s.db.Exec(`UPDATE home_acl SET exec=1 WHERE user_id=? AND home_id=?`, g.uid, ids["work"]); err != nil {
		t.Fatal(err)
	}
	if r := me(g.uid)["rights"].(map[string]any); !reflect.DeepEqual(r[ids["work"]], []any{}) || len(r[mainHomeID].([]any)) != 2 {
		t.Fatalf("ligne altérée : %v", r)
	}
	if tamperEvents(t, g.s) <= before {
		t.Fatal("altération non journalisée")
	}
	c, _, _ := g.partial()
	pm := jbody(t, g.do("GET", "/v1/auth/me", c, "", nil))
	var got []string
	for k := range pm {
		got = append(got, k)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"csrf", "factors", "mfa", "user"}) {
		t.Fatalf("forme partielle : %v", got)
	}
	if _, has := pm["factors"].(map[string]any)["keys"]; has {
		t.Fatal("clés exposées en session partielle")
	}
}

func TestPurgeEvents(t *testing.T) {
	g := newAuthRig(t)
	ins := func(age time.Duration, kind string) {
		if _, err := g.s.db.Exec(`INSERT INTO auth_events(at,user_id,kind,outcome) VALUES(?,?,?,?)`, g.clock.Add(-age).Unix(), g.uid, kind, "x"); err != nil {
			t.Fatal(err)
		}
	}
	count := func() (n int) {
		g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events`).Scan(&n)
		return
	}
	g.th.Success("alice", "10.0.0.1")
	day := 24 * time.Hour
	ins(89*day, "tamper")
	ins(91*day, "login")
	old := purgeAt
	t.Cleanup(func() { purgeAt = old })
	purgeAt = time.Time{}
	purgeDue(g.s, *g.clock)
	if n := count(); n != 1 {
		t.Fatalf("après purge : %d lignes", n)
	}
	var kind string
	g.s.db.QueryRow(`SELECT kind FROM auth_events`).Scan(&kind)
	var known int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key LIKE 'known:%'`).Scan(&known)
	if kind != "tamper" || known != 1 {
		t.Fatalf("conservés : %q, known %d", kind, known)
	}
	ins(91*day, "login")
	purgeDue(g.s, g.clock.Add(time.Hour))
	if count() != 2 {
		t.Fatal("purge avant 24 h")
	}
	g.tick(day)
	purgeDue(g.s, *g.clock)
	if count() != 1 {
		t.Fatal("pas de purge après 24 h")
	}
	g.s.db.Close()
	if _, err := g.s.PurgeEvents(eventsKeep); err == nil {
		t.Fatal("base fermée sans erreur")
	}
}

// Facteurs supprimés d'un compte enrôlé : altération, jamais un nouvel enrôlement au seul mot de passe.
func TestEnrolledFactorsDeleted(t *testing.T) {
	g := newAuthRig(t)
	g.enrolTOTP()
	c, csrf, _ := g.partial()
	g.s.db.Exec(`DELETE FROM totp WHERE user_id=?`, g.uid)
	g.s.db.Exec(`DELETE FROM webauthn_creds WHERE user_id=?`, g.uid)
	before := tamperEvents(t, g.s)
	g.tick(time.Minute)
	assertGenericFail(t, g.login("alice", testPw))
	if tamperEvents(t, g.s) <= before {
		t.Fatal("altération non journalisée")
	}
	if w := g.do("POST", "/v1/auth/totp/enrollments", c, csrf, map[string]string{}); w.Code == 200 {
		t.Fatalf("enrôlement rouvert : %d %s", w.Code, w.Body)
	}
	if err := g.s.ResetMFA(g.uid); err != nil {
		t.Fatal(err)
	}
	g.tick(thWindow)
	c, csrf, m := g.partial()
	if m["enroll"] != true {
		t.Fatalf("après ResetMFA : %v", m)
	}
	if w := g.do("POST", "/v1/auth/totp/enrollments", c, csrf, map[string]string{}); w.Code != 200 {
		t.Fatalf("enrôlement après ResetMFA : %d %s", w.Code, w.Body)
	}
}

// Nom trop long : échec générique, compté sous la clé commune, jamais écrit tel quel.
func TestLoginOversizeName(t *testing.T) {
	g := newAuthRig(t)
	long := strings.Repeat("é", 65)
	assertGenericFail(t, g.login(long, testPw))
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key LIKE ?`, "%"+long+"%").Scan(&n)
	if n != 0 {
		t.Fatal("clé de throttle au nom trop long")
	}
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE name_tried LIKE ?`, "%"+long+"%").Scan(&n)
	if n != 0 {
		t.Fatal("événement au nom trop long")
	}
	g.s.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key=?`, "user:"+oversizeName).Scan(&n)
	if n != 1 {
		t.Fatal("échec non compté sous la clé commune")
	}
}

// Les refus « blocked » ne s'écrivent qu'une fois par minute pour un même nom et une même adresse.
func TestBlockedEventsRateLimited(t *testing.T) {
	g := newAuthRig(t)
	for range thLockAfter {
		g.th.Fail("alice", "198.51.100.9", "password")
	}
	for range 20 {
		g.login("alice", "wrong password here")
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE outcome='blocked'`).Scan(&n)
	if n != rateBudget {
		t.Fatalf("événements blocked : %d", n)
	}
	g.tick(time.Minute)
	g.login("alice", "wrong password here")
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE outcome='blocked'`).Scan(&n)
	if n != rateBudget+1 {
		t.Fatalf("après une minute : %d", n)
	}
	var sum string
	g.s.db.QueryRow(`SELECT outcome FROM auth_events WHERE kind='suppressed'`).Scan(&sum)
	if sum != "anon=15 overflow=0 uid=0" {
		t.Fatalf("résumé : %q", sum)
	}
}

func rlRig(t *testing.T) (*authRig, *authAPI, func(string, ...any) int, func() string) {
	g := newAuthRig(t)
	a := routesAuth(http.NewServeMux(), g.s, g.th)
	g.s.db.Exec(`DELETE FROM auth_events`)
	count := func(q string, args ...any) (n int) {
		g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE `+q, args...).Scan(&n)
		return
	}
	// summary passe à la minute suivante et rend le résumé écrit.
	summary := func() (s string) {
		g.tick(time.Minute)
		a.rlFlush()
		g.s.db.QueryRow(`SELECT outcome FROM auth_events WHERE kind='suppressed' ORDER BY rowid DESC LIMIT 1`).Scan(&s)
		return
	}
	return g, a, count, summary
}

func TestRateLogNameFlood(t *testing.T) {
	_, a, count, summary := rlRig(t)
	for i := range 5000 {
		a.rateLog(0, "nom"+strconv.Itoa(i), "198.51.100.1", evLogin, "blocked")
	}
	if n := count(`1`); n != rateBudget {
		t.Fatalf("une adresse : %d lignes", n)
	}
	if s := summary(); s != "anon=4995 overflow=0 uid=0" {
		t.Fatalf("résumé : %q", s)
	}
	if n := count(`1`); n != rateBudget+1 {
		t.Fatalf("avec le résumé : %d lignes", n)
	}
	a.rateLog(0, strings.Repeat("é", 100), "192.0.2.1", evLogin, "busy")
	var got string
	a.s.db.QueryRow(`SELECT name_tried FROM auth_events WHERE ip='192.0.2.1'`).Scan(&got)
	if len([]rune(got)) != 64 {
		t.Fatalf("nom tronqué : %d", len([]rune(got)))
	}
}

func TestRateLogGlobalAndOverflow(t *testing.T) {
	_, a, count, summary := rlRig(t)
	for ip := range 300 {
		for i := range 10 {
			a.rateLog(0, "n"+strconv.Itoa(i), "10.0."+strconv.Itoa(ip/256)+"."+strconv.Itoa(ip%256), evLogin, "blocked")
		}
	}
	if n := count(`kind=?`, evLogin); n != rlGlobal {
		t.Fatalf("plafond global : %d lignes", n)
	}
	if s := summary(); s != "anon=2800 overflow=0 uid=0" {
		t.Fatalf("résumé : %q", s)
	}
	// Table pleine : les adresses suivies gardent leur budget, les nouvelles vont au compte commun.
	a.mu.Lock()
	for i := range rlCap {
		a.rl.src["fill"+strconv.Itoa(i)] = 0
	}
	a.mu.Unlock()
	before := count(`1`)
	a.rateLog(0, "", "203.0.113.1", evLogin, "busy")
	a.rateLog(0, "", "fill3", evLogin, "busy")
	if count(`1`)-before != 1 || count(`ip='fill3'`) != 1 {
		t.Fatal("source suivie touchée par la table pleine")
	}
	if s := summary(); s != "anon=0 overflow=1 uid=0" {
		t.Fatalf("résumé : %q", s)
	}
}

func TestRateLogUIDDedup(t *testing.T) {
	g, a, count, summary := rlRig(t)
	for range 10 {
		a.rateLog(g.uid, "alice", "198.51.100.1", "mfa_totp", "blocked")
		a.rateLog(g.uid, "alice", "198.51.100.1", "mfa_totp", "busy")
	}
	if n := count(`user_id=?`, g.uid); n != 2 {
		t.Fatalf("dédoublonnage par compte : %d", n)
	}
	if s := summary(); s != "anon=0 overflow=0 uid=18" {
		t.Fatalf("résumé : %q", s)
	}
	// Les échecs du second facteur sont toujours écrits.
	g.s.db.Exec(`DELETE FROM auth_events`)
	g.enrolTOTP()
	c, csrf, _ := g.partial()
	for range 3 {
		g.do("POST", "/v1/auth/mfa/totp", c, csrf, map[string]string{"code": "000000"})
		g.tick(time.Minute)
	}
	if n := count(`kind='mfa_totp' AND outcome='failure'`); n != 3 {
		t.Fatalf("échecs du second facteur : %d", n)
	}
}

// Une connexion tenue ne bloque que l'écriture, ni la décision du limiteur ni a.mu.
func TestRateLogNoDBUnderLock(t *testing.T) {
	g, a, _, _ := rlRig(t)
	for range rateBudget {
		a.rateLog(0, "", "198.51.100.1", evLogin, "blocked")
	}
	tx, err := g.s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	go a.rateLog(0, "", "198.51.100.7", evLogin, "blocked") // écriture bloquée par la transaction, hors verrou
	time.Sleep(20 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		a.rateLog(0, "", "198.51.100.1", evLogin, "blocked") // tu : aucune écriture
		if rel, ok := a.claim("x"); ok {
			rel()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("limiteur bloqué par la base")
	}
	tx.Rollback()
}

func TestParseFactor(t *testing.T) {
	for in, want := range map[string]struct {
		kind string
		id   int64
		ok   bool
	}{"totp": {"totp", 0, true}, "webauthn-12": {"webauthn", 12, true}, "webauthn-x": {"", 0, false}, "sms": {"", 0, false}, "": {"", 0, false}} {
		k, id, ok := parseFactor(in)
		if ok != want.ok || (ok && (k != want.kind || id != want.id)) {
			t.Errorf("parseFactor(%q) = %q %d %v", in, k, id, ok)
		}
	}
}
