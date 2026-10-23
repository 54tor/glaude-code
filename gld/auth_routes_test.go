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
	return g.do("POST", "/auth/login", "", "", map[string]string{"name": name, "password": pw})
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
	w := g.do("POST", "/auth/mfa/totp", c, csrf, map[string]string{"code": g.code(secret)})
	if w.Code != 200 {
		g.t.Fatalf("mfa : %d %s", w.Code, w.Body)
	}
	nc, _ := newCookie(w)
	return nc, jbody(g.t, w)["csrf"].(string)
}

func assertGenericFail(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != 401 || w.Header().Get("Retry-After") == "" || jbody(t, w)["out"] != L("srv.auth.invalide") {
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
	if w := g.do("POST", "/auth/login", "", "", big); w.Code != 400 || n.Load() != 0 {
		t.Fatalf("corps trop gros : %d", w.Code)
	}
	if w := g.do("POST", "/auth/login", "", "", []byte(`{"name":"alice","password":"x","extra":1}`)); w.Code != 400 {
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
	me := g.do("GET", "/auth/me", c, "", nil)
	mb := jbody(t, me)
	if me.Code != 200 || mb["mfa"] != false || mb["out"] != L("srv.auth.mfaRequis") || mb["csrf"] != csrf {
		t.Fatalf("me = %d %v", me.Code, mb)
	}
	for _, p := range []struct{ m, path string }{
		{"POST", "/auth/password"}, {"GET", "/auth/sessions"}, {"POST", "/auth/sessions/revoke"}, {"POST", "/auth/factor/remove"},
		{"POST", "/auth/totp/begin"}, {"POST", "/auth/webauthn/register/begin"}, {"POST", "/auth/totp/confirm"},
	} {
		if w := g.do(p.m, p.path, c, csrf, map[string]string{}); w.Code != 403 {
			t.Fatalf("%s %s en session partielle : %d %s", p.m, p.path, w.Code, w.Body)
		}
	}
	if w := g.do("GET", "/auth/me", "", "", nil); w.Code != 401 || jbody(t, w)["ok"] != false {
		t.Fatalf("me sans session : %d", w.Code)
	}
}

func TestLoginFullTOTP(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c1, csrf, _ := g.partial()
	if w := g.do("POST", "/auth/mfa/totp", c1, csrf, map[string]string{"code": "000000"}); w.Code != 401 {
		t.Fatalf("faux code : %d", w.Code)
	}
	w := g.do("POST", "/auth/mfa/totp", c1, csrf, map[string]string{"code": g.code(secret)})
	if w.Code != 200 {
		t.Fatalf("mfa : %d %s", w.Code, w.Body)
	}
	c2, _ := newCookie(w)
	if c2 == "" || c2 == c1 {
		t.Fatal("cookie non renouvelé")
	}
	me := jbody(t, g.do("GET", "/auth/me", c2, "", nil))
	if me["mfa"] != true || me["user"].(map[string]any)["name"] != "alice" {
		t.Fatalf("me = %v", me)
	}
	if w := g.do("GET", "/auth/me", c1, "", nil); w.Code != 401 {
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
	w := g.do("POST", "/auth/mfa/recovery", c, csrf, map[string]string{"code": codes[3]})
	if w.Code != 200 {
		t.Fatalf("secours : %d %s", w.Code, w.Body)
	}
	nc, _ := newCookie(w)
	if jbody(t, g.do("GET", "/auth/me", nc, "", nil))["mfa"] != true {
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
		w := g.do("POST", "/auth/mfa/totp", c, csrf, map[string]string{"code": "000000"})
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
	if w := g.do("GET", "/auth/me", c, "", nil); w.Code != 401 {
		t.Fatalf("session partielle survivante : %d", w.Code)
	}
	// Au-delà du seuil, même un bon code sur une autre session partielle n'est pas vérifié.
	w := g.do("POST", "/auth/mfa/totp", c2, csrf2, map[string]string{"code": g.code(secret)})
	assertGenericFail(t, w)
	if _, cleared := newCookie(w); !cleared {
		t.Fatal("cookie non effacé")
	}
}

func TestAuthCSRFMissing(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, _, _ := g.partial()
	if w := g.do("POST", "/auth/mfa/totp", c, "", map[string]string{"code": "000000"}); w.Code != 403 {
		t.Fatalf("mfa sans CSRF : %d", w.Code)
	}
	if w := g.do("POST", "/auth/mfa/totp", c, "bad", map[string]string{"code": "000000"}); w.Code != 403 {
		t.Fatalf("mfa CSRF faux : %d", w.Code)
	}
	fc, _ := g.full(secret)
	if w := g.do("POST", "/auth/logout", fc, "", nil); w.Code != 403 {
		t.Fatalf("logout sans CSRF : %d", w.Code)
	}
	if w := g.do("POST", "/auth/logout", "", "x", nil); w.Code != 401 {
		t.Fatalf("logout sans session : %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, csrf := g.full(secret)
	w := g.do("POST", "/auth/logout", c, csrf, nil)
	if _, cleared := newCookie(w); w.Code != 200 || !cleared {
		t.Fatalf("logout : %d", w.Code)
	}
	if w := g.do("GET", "/auth/me", c, "", nil); w.Code != 401 {
		t.Fatal("session survivante")
	}
}

func TestEnrollFirstLogin(t *testing.T) {
	g := newAuthRig(t)
	c, csrf, m := g.partial()
	if m["enroll"] != true || len(m["mfa"].([]any)) != 0 {
		t.Fatalf("login = %v", m)
	}
	if w := g.do("GET", "/auth/sessions", c, "", nil); w.Code != 403 {
		t.Fatalf("sessions avant enrôlement : %d", w.Code)
	}
	w := g.do("POST", "/auth/totp/begin", c, csrf, map[string]string{})
	if w.Code != 200 {
		t.Fatalf("begin : %d %s", w.Code, w.Body)
	}
	secret := jbody(t, w)["secret"].(string)
	if w := g.do("POST", "/auth/totp/confirm", c, csrf, map[string]string{"code": "000000"}); w.Code != 400 {
		t.Fatalf("faux code : %d", w.Code)
	}
	w = g.do("POST", "/auth/totp/confirm", c, csrf, map[string]string{"code": codeAt(t, secret, *g.clock)})
	if w.Code != 200 {
		t.Fatalf("confirm : %d %s", w.Code, w.Body)
	}
	b := jbody(t, w)
	if len(b["recovery"].([]any)) != recoveryCount {
		t.Fatalf("codes de secours = %v", b["recovery"])
	}
	nc, _ := newCookie(w)
	if nc == "" || nc == c || jbody(t, g.do("GET", "/auth/me", nc, "", nil))["mfa"] != true {
		t.Fatal("session non promue")
	}
	// Un facteur existe désormais : une nouvelle session partielle ne peut plus enrôler.
	c2, csrf2, _ := g.partial()
	if w := g.do("POST", "/auth/totp/begin", c2, csrf2, map[string]string{}); w.Code != 403 {
		t.Fatalf("enrôlement partiel avec facteur : %d", w.Code)
	}
}

func TestEnrollRequiresReauth(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, csrf := g.full(secret)
	assertGenericFail(t, g.do("POST", "/auth/webauthn/register/begin", c, csrf, map[string]string{}))
	g.tick(time.Minute)
	assertGenericFail(t, g.do("POST", "/auth/webauthn/register/begin", c, csrf, map[string]string{"password": testPw}))
	g.tick(time.Minute)
	assertGenericFail(t, g.do("POST", "/auth/webauthn/register/begin", c, csrf, map[string]string{"password": "wrong password here", "code": g.code(secret)}))
	g.tick(thWindow + time.Minute)
	w := g.do("POST", "/auth/webauthn/register/begin", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)})
	if w.Code != 200 || jbody(t, w)["options"] == nil {
		t.Fatalf("begin réauthentifié : %d %s", w.Code, w.Body)
	}
	w = g.do("POST", "/auth/totp/begin", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)})
	if w.Code != 409 || jbody(t, w)["out"] != L("srv.auth.totpDejaActif") {
		t.Fatalf("second TOTP : %d %s", w.Code, w.Body)
	}
}

// waReauth signe une assertion WebAuthn sur une session complète via les routes.
func (g *authRig) waReauth(k *softKey, c, csrf string) {
	g.t.Helper()
	w := g.do("POST", "/auth/mfa/webauthn/begin", c, csrf, nil)
	if w.Code != 200 {
		g.t.Fatalf("begin : %d %s", w.Code, w.Body)
	}
	var b struct{ Options protocol.CredentialAssertion }
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		g.t.Fatal(err)
	}
	k.count++
	raw, _ := io.ReadAll(k.assert(g.t, &b.Options).Body)
	if w := g.do("POST", "/auth/mfa/webauthn/finish", c, csrf, raw); w.Code != 200 {
		g.t.Fatalf("finish : %d %s", w.Code, w.Body)
	}
}

func TestFactorRemoveLast(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, csrf := g.full(secret)
	w := g.do("POST", "/auth/factor/remove", c, csrf, map[string]any{"kind": "totp", "password": testPw, "code": g.code(secret)})
	if w.Code != 409 || jbody(t, w)["out"] != L("srv.auth.dernierFacteur") {
		t.Fatalf("retrait du dernier facteur : %d %s", w.Code, w.Body)
	}
	k := newSoftKey(t)
	if err := waRegister(t, g.s, g.uid, k, "x"); err != nil {
		t.Fatal(err)
	}
	if w := g.do("POST", "/auth/factor/remove", c, csrf, map[string]any{"kind": "totp", "password": testPw, "code": g.code(secret)}); w.Code != 200 {
		t.Fatalf("retrait TOTP : %d %s", w.Code, w.Body)
	}
	creds, _ := g.s.WebAuthnList(g.uid)
	g.waReauth(k, c, csrf)
	w = g.do("POST", "/auth/factor/remove", c, csrf, map[string]any{"kind": "webauthn", "id": creds[0].ID, "password": testPw})
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
	if w := g.do("POST", "/auth/password", c, csrf, map[string]string{"password": testPw, "code": g.code(secret), "new": "short"}); w.Code != 400 {
		t.Fatalf("mot de passe court : %d", w.Code)
	}
	w := g.do("POST", "/auth/password", c, csrf, map[string]string{"password": testPw, "code": g.code(secret), "new": newPw})
	if _, cleared := newCookie(w); w.Code != 200 || !cleared {
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
	w := g.do("GET", "/auth/sessions", c, "", nil)
	var b struct{ Sessions []map[string]any }
	json.Unmarshal(w.Body.Bytes(), &b)
	if w.Code != 200 || len(b.Sessions) != 2 {
		t.Fatalf("sessions : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/auth/sessions/revoke", c, csrf, map[string]string{"id": other.SessionID, "password": testPw}); w.Code != 401 {
		t.Fatalf("révocation sans facteur : %d", w.Code)
	}
	g.tick(time.Minute)
	if w := g.do("POST", "/auth/sessions/revoke", c, csrf, map[string]string{"id": other.SessionID, "password": testPw, "code": g.code(secret)}); w.Code != 200 {
		t.Fatalf("révocation : %d %s", w.Code, w.Body)
	}
	if _, err := g.s.Session(c); err != nil {
		t.Fatal("session courante révoquée")
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
		first = g.do("POST", "/auth/mfa/recovery", c, csrf, map[string]string{"code": codes[0]})
	}()
	<-entered
	assertGenericFail(t, g.do("POST", "/auth/mfa/recovery", c, csrf, map[string]string{"code": codes[1]}))
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
	if w := g.do("POST", "/auth/recovery/regenerate", pc, pcsrf, map[string]string{"password": testPw, "code": g.code(secret)}); w.Code != 403 {
		t.Fatalf("session partielle : %d", w.Code)
	}
	c, csrf := g.full(secret)
	assertGenericFail(t, g.do("POST", "/auth/recovery/regenerate", c, csrf, map[string]string{"password": testPw}))
	g.tick(2 * time.Second)
	w := g.do("POST", "/auth/recovery/regenerate", c, csrf, map[string]string{"password": testPw, "code": g.code(secret)})
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
