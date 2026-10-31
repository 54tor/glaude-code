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
	authStore, id := sessUser(t)
	th := newThrottle(authStore.db)
	th.now = func() time.Time { return *now }
	mux := http.NewServeMux()
	routesAuth(mux, authStore, th)
	return &authRig{t: t, s: authStore, th: th, h: mux, clock: now, uid: id}
}

// argonCalls compte chaque calcul de mot de passe, réel ou factice.
func argonCalls(t *testing.T) *atomic.Int64 {
	var localInt64 atomic.Int64
	callback, localCallback := verifyPassword, dummyPassword
	verifyPassword = func(header, path string) bool { localInt64.Add(1); return callback(header, path) }
	dummyPassword = func() { localInt64.Add(1); localCallback() }
	t.Cleanup(func() { verifyPassword, dummyPassword = callback, localCallback })
	return &localInt64
}

func (localAuthRig *authRig) do(method, path, cookie, csrf string, body any) *httptest.ResponseRecorder {
	localAuthRig.t.Helper()
	var rd io.Reader
	switch value := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(value)
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			localAuthRig.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, rd)
	request.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: sessCookie, Value: cookie})
	}
	if csrf != "" {
		request.Header.Set(csrfHeader, csrf)
	}
	recorder := httptest.NewRecorder()
	localAuthRig.h.ServeHTTP(recorder, request)
	return recorder
}

// errMsg lit le message du corps d'erreur uniforme {"error":{"code","message"}}.
func errBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	lookup, _ := jbody(t, recorder)["error"].(map[string]any)
	return lookup
}

func errMsg(t *testing.T, recorder *httptest.ResponseRecorder) any {
	t.Helper()
	return errBody(t, recorder)["message"]
}

func fid(count int64) string { return strconv.FormatInt(count, 10) }

func jbody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	lookup := map[string]any{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &lookup); err != nil {
		t.Fatalf("corps %q : %v", recorder.Body.String(), err)
	}
	return lookup
}

// newCookie renvoie la valeur posée (vide si absente) et si le cookie a été effacé.
func newCookie(recorder *httptest.ResponseRecorder) (value string, cleared bool) {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessCookie {
			return cookie.Value, cookie.MaxAge < 0
		}
	}
	return "", false
}

func (localAuthRig *authRig) login(name, pw string) *httptest.ResponseRecorder {
	localAuthRig.t.Helper()
	return localAuthRig.do("POST", "/v1/auth/sessions", "", "", map[string]string{"name": name, "password": pw})
}

// partial ouvre une session après le mot de passe seul et renvoie cookie et jeton CSRF.
func (localAuthRig *authRig) partial() (string, string, map[string]any) {
	localAuthRig.t.Helper()
	recorder := localAuthRig.login("alice", testPw)
	if recorder.Code != 200 {
		localAuthRig.t.Fatalf("login : %d %s", recorder.Code, recorder.Body)
	}
	content, _ := newCookie(recorder)
	lookup := jbody(localAuthRig.t, recorder)
	return content, lookup["csrf"].(string), lookup
}

func (localAuthRig *authRig) enrolTOTP() string {
	localAuthRig.t.Helper()
	secret, _, err := localAuthRig.s.TOTPBegin(localAuthRig.uid, "alice")
	if err != nil {
		localAuthRig.t.Fatal(err)
	}
	if err := localAuthRig.s.TOTPConfirm(localAuthRig.uid, codeAt(localAuthRig.t, secret, *localAuthRig.clock)); err != nil {
		localAuthRig.t.Fatal(err)
	}
	localAuthRig.tick(totpPeriod * time.Second)
	return secret
}

func (localAuthRig *authRig) tick(duration time.Duration) {
	*localAuthRig.clock = localAuthRig.clock.Add(duration)
}

func (localAuthRig *authRig) code(secret string) string {
	localAuthRig.t.Helper()
	localAuthRig.tick(totpPeriod * time.Second)
	return codeAt(localAuthRig.t, secret, *localAuthRig.clock)
}

func (localAuthRig *authRig) full(secret string) (string, string) {
	localAuthRig.t.Helper()
	content, csrf, _ := localAuthRig.partial()
	recorder := localAuthRig.do("POST", "/v1/auth/mfa/totp", content, csrf, map[string]string{"code": localAuthRig.code(secret)})
	if recorder.Code != 200 {
		localAuthRig.t.Fatalf("mfa : %d %s", recorder.Code, recorder.Body)
	}
	nc, _ := newCookie(recorder)
	return nc, jbody(localAuthRig.t, recorder)["csrf"].(string)
}

func assertGenericFail(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != 401 || recorder.Header().Get("Retry-After") == "" || errMsg(t, recorder) != L("srv.auth.invalide") {
		t.Fatalf("échec non générique : %d %v %s", recorder.Code, recorder.Header(), recorder.Body)
	}
}

func headerKeys(header http.Header) []string {
	values := make([]string, 0, len(header))
	for name := range header {
		values = append(values, name)
	}
	sort.Strings(values)
	return values
}

func TestLoginUnknownVsWrong(t *testing.T) {
	authRig := newAuthRig(t)
	total, tt := argonMemKiB, argonTime
	argonMemKiB, argonTime = 8*1024, 1
	t.Cleanup(func() { argonMemKiB, argonTime = total, tt })
	if _, err := authRig.s.AddUser("bob", testPw, false); err != nil {
		t.Fatal(err)
	}
	median := func(name string) (time.Duration, *httptest.ResponseRecorder) {
		var ds []time.Duration
		var last *httptest.ResponseRecorder
		for index := 0; index < 5; index++ {
			authRig.tick(thWindow + time.Minute)
			t0 := time.Now()
			last = authRig.login(name, "wrong password here")
			ds = append(ds, time.Since(t0))
		}
		sort.Slice(ds, func(index, innerIndex int) bool { return ds[index] < ds[innerIndex] })
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
	authRig := newAuthRig(t)
	wrong := authRig.login("alice", "wrong password here")
	authRig.tick(thWindow + time.Minute)
	if err := authRig.s.SetDisabled(authRig.uid, true); err != nil {
		t.Fatal(err)
	}
	dis := authRig.login("alice", testPw)
	assertGenericFail(t, dis)
	if content, _ := newCookie(dis); content != "" {
		t.Fatal("cookie posé pour un compte désactivé")
	}
	if dis.Body.String() != wrong.Body.String() || !reflect.DeepEqual(headerKeys(dis.Header()), headerKeys(wrong.Header())) {
		t.Fatalf("désactivé distinct de mauvais mot de passe : %v %s / %v %s", dis.Header(), dis.Body, wrong.Header(), wrong.Body)
	}
}

func TestLoginThrottled(t *testing.T) {
	authRig := newAuthRig(t)
	localInt64 := argonCalls(t)
	for index := 0; index < 3; index++ {
		assertGenericFail(t, authRig.login("alice", "wrong password here"))
	}
	if localInt64.Load() != 3 {
		t.Fatalf("calculs = %d", localInt64.Load())
	}
	recorder := authRig.login("alice", testPw)
	assertGenericFail(t, recorder)
	ra, _ := strconv.Atoi(recorder.Header().Get("Retry-After"))
	if ra < 1 {
		t.Fatalf("Retry-After = %q", recorder.Header().Get("Retry-After"))
	}
	if localInt64.Load() != 3 {
		t.Fatal("mot de passe vérifié pendant l'attente")
	}
	authRig.tick(time.Duration(ra) * time.Second)
	if recorder := authRig.login("alice", testPw); recorder.Code != 200 {
		t.Fatalf("après l'attente : %d %s", recorder.Code, recorder.Body)
	}
}

func TestLoginPasswordLimits(t *testing.T) {
	authRig := newAuthRig(t)
	localInt64 := argonCalls(t)
	for _, pw := range []string{"", strings.Repeat("a", 5000), "correct horse\x00battery"} {
		assertGenericFail(t, authRig.login("alice", pw))
	}
	if localInt64.Load() != 0 {
		t.Fatalf("%d calculs argon2", localInt64.Load())
	}
	var fails int64
	if err := authRig.s.db.QueryRow(`SELECT fails FROM throttle WHERE key='user:alice'`).Scan(&fails); err != nil || fails != 3 {
		t.Fatalf("échecs comptés = %d (%v)", fails, err)
	}
	big := []byte(`{"name":"alice","password":"` + strings.Repeat("a", authBodyMax) + `"}`)
	if recorder := authRig.do("POST", "/v1/auth/sessions", "", "", big); recorder.Code != 400 || localInt64.Load() != 0 {
		t.Fatalf("corps trop gros : %d", recorder.Code)
	}
	if recorder := authRig.do("POST", "/v1/auth/sessions", "", "", []byte(`{"name":"alice","password":"x","extra":1}`)); recorder.Code != 400 {
		t.Fatalf("champ inconnu : %d", recorder.Code)
	}
}

func TestLoginThenMFAGate(t *testing.T) {
	authRig := newAuthRig(t)
	authRig.enrolTOTP()
	content, csrf, lookup := authRig.partial()
	if !reflect.DeepEqual(lookup["mfa"], []any{"totp"}) || lookup["enroll"] != false {
		t.Fatalf("login = %v", lookup)
	}
	me := authRig.do("GET", "/v1/auth/me", content, "", nil)
	mb := jbody(t, me)
	if me.Code != 200 || mb["mfa"] != false || mb["csrf"] != csrf {
		t.Fatalf("me = %d %v", me.Code, mb)
	}
	for _, record := range []struct{ m, path string }{
		{"PUT", "/v1/auth/password"}, {"GET", "/v1/auth/sessions"}, {"POST", "/v1/auth/sessions/x/revocations"}, {"POST", "/v1/auth/factors/totp/removals"},
		{"POST", "/v1/auth/totp/enrollments"}, {"POST", "/v1/auth/webauthn/registrations"}, {"PUT", "/v1/auth/totp/enrollments/current"},
	} {
		if recorder := authRig.do(record.m, record.path, content, csrf, map[string]string{}); recorder.Code != 403 {
			t.Fatalf("%s %s en session partielle : %d %s", record.m, record.path, recorder.Code, recorder.Body)
		}
	}
	if recorder := authRig.do("GET", "/v1/auth/me", "", "", nil); recorder.Code != 401 || errCode(jbody(t, recorder)) != "unauthorized" {
		t.Fatalf("me sans session : %d", recorder.Code)
	}
}

func TestLoginFullTOTP(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	c1, csrf, _ := authRig.partial()
	if recorder := authRig.do("POST", "/v1/auth/mfa/totp", c1, csrf, map[string]string{"code": "000000"}); recorder.Code != 401 {
		t.Fatalf("faux code : %d", recorder.Code)
	}
	localRecorder := authRig.do("POST", "/v1/auth/mfa/totp", c1, csrf, map[string]string{"code": authRig.code(secret)})
	if localRecorder.Code != 200 {
		t.Fatalf("mfa : %d %s", localRecorder.Code, localRecorder.Body)
	}
	c2, _ := newCookie(localRecorder)
	if c2 == "" || c2 == c1 {
		t.Fatal("cookie non renouvelé")
	}
	me := jbody(t, authRig.do("GET", "/v1/auth/me", c2, "", nil))
	if me["mfa"] != true || me["user"].(map[string]any)["name"] != "alice" {
		t.Fatalf("me = %v", me)
	}
	if recorder := authRig.do("GET", "/v1/auth/me", c1, "", nil); recorder.Code != 401 {
		t.Fatal("ancien cookie encore valide")
	}
	var count int
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND kind='login' AND outcome='success' AND ip='192.0.2.1'`, authRig.uid).Scan(&count)
	if count != 1 {
		t.Fatalf("événements login/success = %d", count)
	}
}

func TestLoginRecovery(t *testing.T) {
	authRig := newAuthRig(t)
	authRig.enrolTOTP()
	codes, err := authRig.s.NewRecoveryCodes(authRig.uid)
	if err != nil {
		t.Fatal(err)
	}
	content, csrf, lookup := authRig.partial()
	if !reflect.DeepEqual(lookup["mfa"], []any{"totp", "recovery"}) {
		t.Fatalf("méthodes = %v", lookup["mfa"])
	}
	recorder := authRig.do("POST", "/v1/auth/mfa/recovery", content, csrf, map[string]string{"code": codes[3]})
	if recorder.Code != 200 {
		t.Fatalf("secours : %d %s", recorder.Code, recorder.Body)
	}
	nc, _ := newCookie(recorder)
	if jbody(t, authRig.do("GET", "/v1/auth/me", nc, "", nil))["mfa"] != true {
		t.Fatal("session non complète")
	}
}

func TestMFAFailuresKillSession(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, csrf, _ := authRig.partial()
	c2, csrf2, _ := authRig.partial()
	ra := 0
	for index := 1; index <= thMFALimit; index++ {
		authRig.tick(time.Duration(ra) * time.Second)
		recorder := authRig.do("POST", "/v1/auth/mfa/totp", content, csrf, map[string]string{"code": "000000"})
		assertGenericFail(t, recorder)
		ra, _ = strconv.Atoi(recorder.Header().Get("Retry-After"))
		_, cleared := newCookie(recorder)
		if cleared != (index == thMFALimit) {
			t.Fatalf("essai %d : cookie effacé = %v (%s)", index, cleared, recorder.Body)
		}
	}
	// Chaque essai a été vérifié : les cinq échecs sont comptés, aucun n'a été refusé d'avance.
	if flags := authRig.th.mfaFails("alice"); flags != thMFALimit {
		t.Fatalf("échecs mfa comptés = %d", flags)
	}
	if recorder := authRig.do("GET", "/v1/auth/me", content, "", nil); recorder.Code != 401 {
		t.Fatalf("session partielle survivante : %d", recorder.Code)
	}
	// Au-delà du seuil, même un bon code sur une autre session partielle n'est pas vérifié.
	localRecorder := authRig.do("POST", "/v1/auth/mfa/totp", c2, csrf2, map[string]string{"code": authRig.code(secret)})
	assertGenericFail(t, localRecorder)
	if _, cleared := newCookie(localRecorder); !cleared {
		t.Fatal("cookie non effacé")
	}
}

func TestAuthCSRFMissing(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, _, _ := authRig.partial()
	if recorder := authRig.do("POST", "/v1/auth/mfa/totp", content, "", map[string]string{"code": "000000"}); recorder.Code != 403 {
		t.Fatalf("mfa sans CSRF : %d", recorder.Code)
	}
	if recorder := authRig.do("POST", "/v1/auth/mfa/totp", content, "bad", map[string]string{"code": "000000"}); recorder.Code != 403 {
		t.Fatalf("mfa CSRF faux : %d", recorder.Code)
	}
	fc, _ := authRig.full(secret)
	if recorder := authRig.do("DELETE", "/v1/auth/sessions/current", fc, "", nil); recorder.Code != 403 {
		t.Fatalf("logout sans CSRF : %d", recorder.Code)
	}
	if recorder := authRig.do("DELETE", "/v1/auth/sessions/current", "", "x", nil); recorder.Code != 401 {
		t.Fatalf("logout sans session : %d", recorder.Code)
	}
}

func TestLogout(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, csrf := authRig.full(secret)
	recorder := authRig.do("DELETE", "/v1/auth/sessions/current", content, csrf, nil)
	if _, cleared := newCookie(recorder); recorder.Code != 204 || !cleared {
		t.Fatalf("logout : %d", recorder.Code)
	}
	if recorder := authRig.do("GET", "/v1/auth/me", content, "", nil); recorder.Code != 401 {
		t.Fatal("session survivante")
	}
}

func TestEnrollFirstLogin(t *testing.T) {
	authRig := newAuthRig(t)
	content, csrf, lookup := authRig.partial()
	if lookup["enroll"] != true || len(lookup["mfa"].([]any)) != 0 {
		t.Fatalf("login = %v", lookup)
	}
	if recorder := authRig.do("GET", "/v1/auth/sessions", content, "", nil); recorder.Code != 403 {
		t.Fatalf("sessions avant enrôlement : %d", recorder.Code)
	}
	localRecorder := authRig.do("POST", "/v1/auth/totp/enrollments", content, csrf, map[string]string{})
	if localRecorder.Code != 200 {
		t.Fatalf("begin : %d %s", localRecorder.Code, localRecorder.Body)
	}
	secret := jbody(t, localRecorder)["secret"].(string)
	if recorder := authRig.do("PUT", "/v1/auth/totp/enrollments/current", content, csrf, map[string]string{"code": "000000"}); recorder.Code != 400 {
		t.Fatalf("faux code : %d", recorder.Code)
	}
	localRecorder = authRig.do("PUT", "/v1/auth/totp/enrollments/current", content, csrf, map[string]string{"code": codeAt(t, secret, *authRig.clock)})
	if localRecorder.Code != 200 {
		t.Fatalf("confirm : %d %s", localRecorder.Code, localRecorder.Body)
	}
	localLookup := jbody(t, localRecorder)
	if len(localLookup["recovery"].([]any)) != recoveryCount {
		t.Fatalf("codes de secours = %v", localLookup["recovery"])
	}
	nc, _ := newCookie(localRecorder)
	if nc == "" || nc == content || jbody(t, authRig.do("GET", "/v1/auth/me", nc, "", nil))["mfa"] != true {
		t.Fatal("session non promue")
	}
	// Un facteur existe désormais : une nouvelle session partielle ne peut plus enrôler.
	c2, csrf2, _ := authRig.partial()
	if recorder := authRig.do("POST", "/v1/auth/totp/enrollments", c2, csrf2, map[string]string{}); recorder.Code != 403 {
		t.Fatalf("enrôlement partiel avec facteur : %d", recorder.Code)
	}
}

func TestEnrollRequiresReauth(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, csrf := authRig.full(secret)
	assertGenericFail(t, authRig.do("POST", "/v1/auth/webauthn/registrations", content, csrf, map[string]string{}))
	authRig.tick(time.Minute)
	assertGenericFail(t, authRig.do("POST", "/v1/auth/webauthn/registrations", content, csrf, map[string]string{"password": testPw}))
	authRig.tick(time.Minute)
	assertGenericFail(t, authRig.do("POST", "/v1/auth/webauthn/registrations", content, csrf, map[string]string{"password": "wrong password here", "code": authRig.code(secret)}))
	authRig.tick(thWindow + time.Minute)
	recorder := authRig.do("POST", "/v1/auth/webauthn/registrations", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret)})
	if recorder.Code != 200 || jbody(t, recorder)["options"] == nil {
		t.Fatalf("begin réauthentifié : %d %s", recorder.Code, recorder.Body)
	}
	recorder = authRig.do("POST", "/v1/auth/totp/enrollments", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret)})
	if recorder.Code != 409 || errMsg(t, recorder) != L("srv.auth.totpDejaActif") {
		t.Fatalf("second TOTP : %d %s", recorder.Code, recorder.Body)
	}
}

// waReauth signe une assertion WebAuthn sur une session complète via les routes.
func (localAuthRig *authRig) waReauth(localSoftKey *softKey, content, csrf string) {
	localAuthRig.t.Helper()
	recorder := localAuthRig.do("POST", "/v1/auth/mfa/webauthn/challenges", content, csrf, nil)
	if recorder.Code != 200 {
		localAuthRig.t.Fatalf("begin : %d %s", recorder.Code, recorder.Body)
	}
	var record struct{ Options protocol.CredentialAssertion }
	if err := json.Unmarshal(recorder.Body.Bytes(), &record); err != nil {
		localAuthRig.t.Fatal(err)
	}
	localSoftKey.count++
	raw, _ := io.ReadAll(localSoftKey.assert(localAuthRig.t, &record.Options).Body)
	if recorder := localAuthRig.do("POST", "/v1/auth/mfa/webauthn/assertions", content, csrf, raw); recorder.Code != 200 {
		localAuthRig.t.Fatalf("finish : %d %s", recorder.Code, recorder.Body)
	}
}

func TestFactorRemoveLast(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, csrf := authRig.full(secret)
	recorder := authRig.do("POST", "/v1/auth/factors/totp/removals", content, csrf, map[string]any{"password": testPw, "code": authRig.code(secret)})
	if recorder.Code != 409 || errMsg(t, recorder) != L("srv.auth.dernierFacteur") {
		t.Fatalf("retrait du dernier facteur : %d %s", recorder.Code, recorder.Body)
	}
	softKey := newSoftKey(t)
	if err := waRegister(t, authRig.s, authRig.uid, softKey, "x"); err != nil {
		t.Fatal(err)
	}
	if recorder := authRig.do("POST", "/v1/auth/factors/totp/removals", content, csrf, map[string]any{"password": testPw, "code": authRig.code(secret)}); recorder.Code != 204 {
		t.Fatalf("retrait TOTP : %d %s", recorder.Code, recorder.Body)
	}
	creds, _ := authRig.s.WebAuthnList(authRig.uid)
	authRig.waReauth(softKey, content, csrf)
	recorder = authRig.do("POST", "/v1/auth/factors/webauthn-"+strconv.FormatInt(creds[0].ID, 10)+"/removals", content, csrf, map[string]any{"password": testPw})
	if recorder.Code != 409 {
		t.Fatalf("retrait de la dernière clé : %d %s", recorder.Code, recorder.Body)
	}
	if factorState, err := authRig.s.factors(authRig.uid); err != nil || factorState.WebAuthn != 1 || factorState.TOTP {
		t.Fatalf("facteurs = %+v %v", factorState, err)
	}
}

func TestPasswordChangeRevokes(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	other, _ := fullSession(t, authRig.s, authRig.uid)
	content, csrf := authRig.full(secret)
	const newPw = "a much better passphrase"
	if recorder := authRig.do("PUT", "/v1/auth/password", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret), "new": "short"}); recorder.Code != 400 {
		t.Fatalf("mot de passe court : %d", recorder.Code)
	}
	localRecorder := authRig.do("PUT", "/v1/auth/password", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret), "new": newPw})
	if _, cleared := newCookie(localRecorder); localRecorder.Code != 204 || !cleared {
		t.Fatalf("changement : %d %s", localRecorder.Code, localRecorder.Body)
	}
	for _, text := range []string{content, other} {
		if _, err := authRig.s.Session(text); err == nil {
			t.Fatal("session survivante")
		}
	}
	if recorder := authRig.login("alice", newPw); recorder.Code != 200 {
		t.Fatalf("nouveau mot de passe : %d", recorder.Code)
	}
}

func TestSessionsListRevoke(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	_, other := fullSession(t, authRig.s, authRig.uid)
	content, csrf := authRig.full(secret)
	recorder := authRig.do("GET", "/v1/auth/sessions", content, "", nil)
	var record struct{ Sessions []map[string]any }
	json.Unmarshal(recorder.Body.Bytes(), &record)
	if recorder.Code != 200 || len(record.Sessions) != 2 {
		t.Fatalf("sessions : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := authRig.do("POST", "/v1/auth/sessions/"+other.SessionID+"/revocations", content, csrf, map[string]string{"password": testPw}); recorder.Code != 401 {
		t.Fatalf("révocation sans facteur : %d", recorder.Code)
	}
	authRig.tick(time.Minute)
	if recorder := authRig.do("POST", "/v1/auth/sessions/"+other.SessionID+"/revocations", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret)}); recorder.Code != 204 {
		t.Fatalf("révocation : %d %s", recorder.Code, recorder.Body)
	}
	if _, err := authRig.s.Session(content); err != nil {
		t.Fatal("session courante révoquée")
	}
	bob, err := authRig.s.AddUser("bob", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	bc, bp := fullSession(t, authRig.s, bob)
	if recorder := authRig.do("POST", "/v1/auth/sessions/"+bp.SessionID+"/revocations", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret)}); recorder.Code != 404 {
		t.Fatalf("révocation de la session d'autrui : %d", recorder.Code)
	}
	if _, err := authRig.s.Session(bc); err != nil {
		t.Fatal("session d'autrui révoquée")
	}
	thirdC, _ := fullSession(t, authRig.s, authRig.uid)
	if recorder := authRig.do("DELETE", "/v1/auth/sessions", content, csrf, map[string]string{"password": testPw}); recorder.Code != 405 {
		t.Fatalf("l'ancienne route DELETE ne répond plus : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := authRig.do("POST", "/v1/auth/sessions/revocations", content, csrf, map[string]string{"password": testPw}); recorder.Code != 401 {
		t.Fatalf("nettoyage sans facteur : %d", recorder.Code)
	}
	if recorder := authRig.do("POST", "/v1/auth/sessions/revocations", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret)}); recorder.Code != 204 {
		t.Fatalf("nettoyage : %d %s", recorder.Code, recorder.Body)
	}
	if _, err := authRig.s.Session(content); err != nil {
		t.Fatal("session courante révoquée par le nettoyage")
	}
	if _, err := authRig.s.Session(thirdC); err == nil {
		t.Fatal("autre session de l'utilisateur survivante")
	}
	if _, err := authRig.s.Session(bc); err != nil {
		t.Fatal("le nettoyage a touché la session d'autrui")
	}
}

func TestRecoveryConcurrentSerialised(t *testing.T) {
	authRig := newAuthRig(t)
	authRig.enrolTOTP()
	codes, err := authRig.s.NewRecoveryCodes(authRig.uid)
	if err != nil {
		t.Fatal(err)
	}
	content, csrf, _ := authRig.partial()
	var calls atomic.Int64
	entered, release := make(chan struct{}, 2), make(chan struct{})
	orig := useRecovery
	useRecovery = func(localAuthStore *authStore, uid int64, code string) bool {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return orig(localAuthStore, uid, code)
	}
	t.Cleanup(func() { useRecovery = orig })
	var wg sync.WaitGroup
	var first *httptest.ResponseRecorder
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = authRig.do("POST", "/v1/auth/mfa/recovery", content, csrf, map[string]string{"code": codes[0]})
	}()
	<-entered
	assertGenericFail(t, authRig.do("POST", "/v1/auth/mfa/recovery", content, csrf, map[string]string{"code": codes[1]}))
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
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	old, err := authRig.s.NewRecoveryCodes(authRig.uid)
	if err != nil {
		t.Fatal(err)
	}
	pc, pcsrf, _ := authRig.partial()
	if recorder := authRig.do("PUT", "/v1/auth/recovery-codes", pc, pcsrf, map[string]string{"password": testPw, "code": authRig.code(secret)}); recorder.Code != 403 {
		t.Fatalf("session partielle : %d", recorder.Code)
	}
	content, csrf := authRig.full(secret)
	assertGenericFail(t, authRig.do("PUT", "/v1/auth/recovery-codes", content, csrf, map[string]string{"password": testPw}))
	authRig.tick(2 * time.Second)
	localRecorder := authRig.do("PUT", "/v1/auth/recovery-codes", content, csrf, map[string]string{"password": testPw, "code": authRig.code(secret)})
	if localRecorder.Code != 200 {
		t.Fatalf("régénération : %d %s", localRecorder.Code, localRecorder.Body)
	}
	codes := jbody(t, localRecorder)["recovery"].([]any)
	if len(codes) != recoveryCount {
		t.Fatalf("codes = %v", codes)
	}
	if authRig.s.UseRecoveryCode(authRig.uid, old[0]) {
		t.Fatal("ancien code encore valable")
	}
	if !authRig.s.UseRecoveryCode(authRig.uid, codes[0].(string)) {
		t.Fatal("nouveau code refusé")
	}
	var count int
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND kind='recovery' AND outcome='regenerated' AND ip='192.0.2.1'`, authRig.uid).Scan(&count)
	if count != 1 {
		t.Fatalf("événements = %d", count)
	}
}

// Vingt faux codes parallèles par vague : une seule vérification à la fois, et exactement cinq au total,
// car chaque vérification compte son échec avant de libérer la clé et le 6e détenteur trouve le seuil atteint.
func TestMFAParallelSerialised(t *testing.T) {
	authRig := newAuthRig(t)
	authRig.enrolTOTP()
	content, csrf, _ := authRig.partial()
	var active, peak, total atomic.Int64
	orig := verifyTOTP
	verifyTOTP = func(localAuthStore *authStore, uid int64, code string, at time.Time) bool {
		count := active.Add(1)
		for {
			port := peak.Load()
			if count <= port || peak.CompareAndSwap(port, count) {
				break
			}
		}
		total.Add(1)
		time.Sleep(2 * time.Millisecond)
		active.Add(-1)
		return orig(localAuthStore, uid, code, at)
	}
	t.Cleanup(func() { verifyTOTP = orig })
	for round := 0; round < 8; round++ {
		var wg sync.WaitGroup
		for index := 0; index < 20; index++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				authRig.do("POST", "/v1/auth/mfa/totp", content, csrf, map[string]string{"code": "000000"})
			}()
		}
		wg.Wait()
		if authRig.do("GET", "/v1/auth/me", content, "", nil).Code == 401 {
			break
		}
		authRig.tick(10 * time.Second)
	}
	if peak.Load() != 1 {
		t.Fatalf("vérifications simultanées : %d", peak.Load())
	}
	if total.Load() != thMFALimit || authRig.th.mfaFails("alice") != thMFALimit {
		t.Fatalf("vérifications %d, échecs comptés %d", total.Load(), authRig.th.mfaFails("alice"))
	}
	if authRig.do("GET", "/v1/auth/me", content, "", nil).Code != 401 {
		t.Fatal("session partielle survivante")
	}
}

func TestLoginBusyName(t *testing.T) {
	authRig := newAuthRig(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int64
	orig := verifyPassword
	verifyPassword = func(header, path string) bool {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return orig(header, path)
	}
	t.Cleanup(func() { verifyPassword = orig })
	var wg sync.WaitGroup
	var first *httptest.ResponseRecorder
	wg.Add(1)
	go func() { defer wg.Done(); first = authRig.login("alice", testPw) }()
	<-entered
	assertGenericFail(t, authRig.login("Alice", testPw))
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
	authRig := newAuthRig(t)
	c1, csrf1, _ := authRig.partial()
	c2, csrf2, _ := authRig.partial()
	recorder := authRig.do("POST", "/v1/auth/totp/enrollments", c1, csrf1, map[string]string{})
	secret := jbody(t, recorder)["secret"].(string)
	recorder = authRig.do("POST", "/v1/auth/webauthn/registrations", c2, csrf2, map[string]string{})
	if recorder.Code != 200 {
		t.Fatalf("webauthn begin : %d %s", recorder.Code, recorder.Body)
	}
	var record struct{ Options protocol.CredentialCreation }
	if err := json.Unmarshal(recorder.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	softKey := newSoftKey(t)
	att, _ := io.ReadAll(softKey.attest(t, &record.Options).Body)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		codes[0] = authRig.do("PUT", "/v1/auth/totp/enrollments/current", c1, csrf1, map[string]string{"code": codeAt(t, secret, *authRig.clock)}).Code
	}()
	go func() {
		defer wg.Done()
		codes[1] = authRig.do("PUT", "/v1/auth/webauthn/registrations/current?label=cle", c2, csrf2, att).Code
	}()
	wg.Wait()
	sort.Ints(codes)
	// Le perdant voit soit le facteur déjà écrit (403), soit l'enrôlement encore en cours (409).
	if codes[0] != 200 || (codes[1] != 403 && codes[1] != 409) {
		t.Fatalf("statuts = %v", codes)
	}
	var sets int
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND kind='recovery' AND outcome='generated'`, authRig.uid).Scan(&sets)
	if factorState, err := authRig.s.factors(authRig.uid); err != nil || sets != 1 || factorState.Recovery != recoveryCount || (factorState.TOTP == (factorState.WebAuthn == 1)) {
		t.Fatalf("jeux de codes %d, facteurs %+v %v", sets, factorState, err)
	}
}

func TestFactorRemoveTamperedTOTP(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	softKey := newSoftKey(t)
	if err := waRegister(t, authRig.s, authRig.uid, softKey, "x"); err != nil {
		t.Fatal(err)
	}
	content, csrf := authRig.full(secret)
	if _, err := authRig.s.db.Exec(`UPDATE totp SET last_step=last_step+1000 WHERE user_id=?`, authRig.uid); err != nil {
		t.Fatal(err)
	}
	before := tamperEvents(t, authRig.s)
	authRig.waReauth(softKey, content, csrf)
	recorder := authRig.do("POST", "/v1/auth/factors/totp/removals", content, csrf, map[string]any{"password": testPw})
	if recorder.Code != 409 || errMsg(t, recorder) != L("srv.auth.tamper") {
		t.Fatalf("retrait d'un TOTP altéré : %d %s", recorder.Code, recorder.Body)
	}
	if tamperEvents(t, authRig.s) <= before {
		t.Fatal("altération non journalisée")
	}
}

func TestAuthDeniedLogged(t *testing.T) {
	authRig := newAuthRig(t)
	count := func(kind string) int {
		var count int
		authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind=? AND outcome='denied' AND ip='192.0.2.1'`, kind).Scan(&count)
		return count
	}
	for index := 0; index < 2*rateBudget; index++ {
		authRig.do("DELETE", "/v1/auth/sessions/current", "", "", nil)
	}
	if count("auth") != rateBudget {
		t.Fatalf("refus journalisés = %d", count("auth"))
	}
	authRig.tick(time.Minute)
	authRig.do("GET", "/v1/auth/sessions", "", "", nil)
	if count("auth") != rateBudget+1 {
		t.Fatalf("après une minute = %d", count("auth"))
	}
	content, _, _ := authRig.partial()
	authRig.do("DELETE", "/v1/auth/sessions/current", content, "bad", nil)
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

func (localStallReader *stallReader) Read(data []byte) (int, error) {
	localStallReader.once.Do(func() { close(localStallReader.started) })
	<-localStallReader.gate
	return localStallReader.r.Read(data)
}

// Un corps qui traîne sur le premier enrôlement d'alice ne retient pas celui de bob.
func TestFirstEnrolStalledBody(t *testing.T) {
	authRig := newAuthRig(t)
	if _, err := authRig.s.AddUser("bob", testPw, false); err != nil {
		t.Fatal(err)
	}
	content, csrf, _ := authRig.partial()
	recorder := authRig.do("POST", "/v1/auth/webauthn/registrations", content, csrf, map[string]string{})
	var record struct{ Options protocol.CredentialCreation }
	if err := json.Unmarshal(recorder.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	att, _ := io.ReadAll(newSoftKey(t).attest(t, &record.Options).Body)
	sr := &stallReader{started: make(chan struct{}), gate: make(chan struct{}), r: bytes.NewReader(att)}
	request := httptest.NewRequest("PUT", "/v1/auth/webauthn/registrations/current?label=cle", sr)
	request.AddCookie(&http.Cookie{Name: sessCookie, Value: content})
	request.Header.Set(csrfHeader, csrf)
	slow := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { authRig.h.ServeHTTP(slow, request); close(done) }()
	<-sr.started

	fast := make(chan int, 1)
	go func() {
		lw := authRig.login("bob", testPw)
		bc, _ := newCookie(lw)
		bcsrf, _ := jbody(t, lw)["csrf"].(string)
		bw := authRig.do("POST", "/v1/auth/totp/enrollments", bc, bcsrf, map[string]string{})
		secret, _ := jbody(t, bw)["secret"].(string)
		fast <- authRig.do("PUT", "/v1/auth/totp/enrollments/current", bc, bcsrf, map[string]string{"code": codeAt(t, secret, *authRig.clock)}).Code
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
	authRig := newAuthRig(t)
	var gens atomic.Int64
	orig := genRecovery
	genRecovery = func() ([]string, []string, error) { gens.Add(1); return orig() }
	t.Cleanup(func() { genRecovery = orig })
	localInt64 := argonCalls(t)
	content, csrf, _ := authRig.partial()
	secret := jbody(t, authRig.do("POST", "/v1/auth/totp/enrollments", content, csrf, map[string]string{}))["secret"].(string)
	before := localInt64.Load()
	recorder := authRig.do("PUT", "/v1/auth/totp/enrollments/current", content, csrf, map[string]string{"code": "000000"})
	if recorder.Code != 400 || errMsg(t, recorder) != L("srv.auth.codeInvalide") || gens.Load() != 0 || localInt64.Load() != before {
		t.Fatalf("faux code : %d %s, générations %d", recorder.Code, recorder.Body, gens.Load())
	}
	if recorder := authRig.do("PUT", "/v1/auth/totp/enrollments/current", content, csrf, map[string]string{"code": codeAt(t, secret, *authRig.clock)}); recorder.Code != 200 || gens.Load() != 1 {
		t.Fatalf("bon code : %d, générations %d", recorder.Code, gens.Load())
	}
}

func TestMeRightsHomesKeys(t *testing.T) {
	authRig := newAuthRig(t)
	old := root
	t.Cleanup(func() { root = old })
	use(t.TempDir())
	addHomes(t, "work", "perso")
	ids := map[string]string{}
	for _, home := range loadHomes() {
		ids[home.Name] = home.ID
	}
	bob, _ := authRig.s.AddUser("bob", testPw, false)
	carol, _ := authRig.s.AddUser("carol", testPw, false)
	authRig.s.Grant(authRig.uid, mainHomeID, RightMemRead, RightExec)
	authRig.s.Grant(authRig.uid, ids["work"], RightMemRead, RightMemWrite)
	authRig.s.Grant(bob, ids["perso"], RightMemRead)
	softKey := newSoftKey(t)
	if err := waRegister(t, authRig.s, authRig.uid, softKey, "x"); err != nil {
		t.Fatal(err)
	}
	me := func(uid int64) map[string]any {
		t.Helper()
		content, _ := fullSession(t, authRig.s, uid)
		recorder := authRig.do("GET", "/v1/auth/me", content, "", nil)
		if recorder.Code != 200 {
			t.Fatalf("me : %d %s", recorder.Code, recorder.Body)
		}
		return jbody(t, recorder)
	}
	lookup := me(authRig.uid)
	want := map[string]any{mainHomeID: []any{"mem_read", "exec"}, ids["work"]: []any{"mem_read", "mem_write"}}
	if !reflect.DeepEqual(lookup["rights"], want) {
		t.Fatalf("droits = %v", lookup["rights"])
	}
	homes := lookup["homes"].([]any)
	if len(homes) != 2 || homes[0].(map[string]any)["name"] != mainHomeID || homes[1].(map[string]any)["name"] != "work" {
		t.Fatalf("homes = %v", homes)
	}
	localLookup := lookup["factors"].(map[string]any)
	keys := localLookup["keys"].([]any)
	if localLookup["webauthn"] != 1.0 || len(keys) != 1 || keys[0].(map[string]any)["label"] != "clé" || keys[0].(map[string]any)["id"] == nil {
		t.Fatalf("facteurs = %v", localLookup)
	}
	if lookup := me(carol); !reflect.DeepEqual(lookup["rights"], map[string]any{}) || len(lookup["homes"].([]any)) != 0 {
		t.Fatalf("sans droit : %v %v", lookup["rights"], lookup["homes"])
	}
	if lookup := me(bob); !reflect.DeepEqual(lookup["rights"], map[string]any{ids["perso"]: []any{"mem_read"}}) {
		t.Fatalf("bob : %v", lookup["rights"])
	}
	before := tamperEvents(t, authRig.s)
	if _, err := authRig.s.db.Exec(`UPDATE home_acl SET exec=1 WHERE user_id=? AND home_id=?`, authRig.uid, ids["work"]); err != nil {
		t.Fatal(err)
	}
	if lookup := me(authRig.uid)["rights"].(map[string]any); !reflect.DeepEqual(lookup[ids["work"]], []any{}) || len(lookup[mainHomeID].([]any)) != 2 {
		t.Fatalf("ligne altérée : %v", lookup)
	}
	if tamperEvents(t, authRig.s) <= before {
		t.Fatal("altération non journalisée")
	}
	localContent, _, _ := authRig.partial()
	pm := jbody(t, authRig.do("GET", "/v1/auth/me", localContent, "", nil))
	var got []string
	for key := range pm {
		got = append(got, key)
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
	authRig := newAuthRig(t)
	ins := func(age time.Duration, kind string) {
		if _, err := authRig.s.db.Exec(`INSERT INTO auth_events(at,user_id,kind,outcome) VALUES(?,?,?,?)`, authRig.clock.Add(-age).Unix(), authRig.uid, kind, "x"); err != nil {
			t.Fatal(err)
		}
	}
	count := func() (count int) {
		authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events`).Scan(&count)
		return
	}
	authRig.th.Success("alice", "10.0.0.1")
	day := 24 * time.Hour
	ins(89*day, "tamper")
	ins(91*day, "login")
	old := purgeAt
	t.Cleanup(func() { purgeAt = old })
	purgeAt = time.Time{}
	purgeDue(authRig.s, *authRig.clock)
	if localCount := count(); localCount != 1 {
		t.Fatalf("après purge : %d lignes", localCount)
	}
	var kind string
	authRig.s.db.QueryRow(`SELECT kind FROM auth_events`).Scan(&kind)
	var known int
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key LIKE 'known:%'`).Scan(&known)
	if kind != "tamper" || known != 1 {
		t.Fatalf("conservés : %q, known %d", kind, known)
	}
	ins(91*day, "login")
	purgeDue(authRig.s, authRig.clock.Add(time.Hour))
	if count() != 2 {
		t.Fatal("purge avant 24 h")
	}
	authRig.tick(day)
	purgeDue(authRig.s, *authRig.clock)
	if count() != 1 {
		t.Fatal("pas de purge après 24 h")
	}
	authRig.s.db.Close()
	if _, err := authRig.s.PurgeEvents(eventsKeep); err == nil {
		t.Fatal("base fermée sans erreur")
	}
}

// Facteurs supprimés d'un compte enrôlé : altération, jamais un nouvel enrôlement au seul mot de passe.
func TestEnrolledFactorsDeleted(t *testing.T) {
	authRig := newAuthRig(t)
	authRig.enrolTOTP()
	content, csrf, _ := authRig.partial()
	authRig.s.db.Exec(`DELETE FROM totp WHERE user_id=?`, authRig.uid)
	authRig.s.db.Exec(`DELETE FROM webauthn_creds WHERE user_id=?`, authRig.uid)
	before := tamperEvents(t, authRig.s)
	authRig.tick(time.Minute)
	assertGenericFail(t, authRig.login("alice", testPw))
	if tamperEvents(t, authRig.s) <= before {
		t.Fatal("altération non journalisée")
	}
	if recorder := authRig.do("POST", "/v1/auth/totp/enrollments", content, csrf, map[string]string{}); recorder.Code == 200 {
		t.Fatalf("enrôlement rouvert : %d %s", recorder.Code, recorder.Body)
	}
	if err := authRig.s.ResetMFA(authRig.uid); err != nil {
		t.Fatal(err)
	}
	authRig.tick(thWindow)
	content, csrf, lookup := authRig.partial()
	if lookup["enroll"] != true {
		t.Fatalf("après ResetMFA : %v", lookup)
	}
	if recorder := authRig.do("POST", "/v1/auth/totp/enrollments", content, csrf, map[string]string{}); recorder.Code != 200 {
		t.Fatalf("enrôlement après ResetMFA : %d %s", recorder.Code, recorder.Body)
	}
}

// Nom trop long : échec générique, compté sous la clé commune, jamais écrit tel quel.
func TestLoginOversizeName(t *testing.T) {
	authRig := newAuthRig(t)
	long := strings.Repeat("é", 65)
	assertGenericFail(t, authRig.login(long, testPw))
	var count int
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key LIKE ?`, "%"+long+"%").Scan(&count)
	if count != 0 {
		t.Fatal("clé de throttle au nom trop long")
	}
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE name_tried LIKE ?`, "%"+long+"%").Scan(&count)
	if count != 0 {
		t.Fatal("événement au nom trop long")
	}
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key=?`, "user:"+oversizeName).Scan(&count)
	if count != 1 {
		t.Fatal("échec non compté sous la clé commune")
	}
}

// Les refus « blocked » ne s'écrivent qu'une fois par minute pour un même nom et une même adresse.
func TestBlockedEventsRateLimited(t *testing.T) {
	authRig := newAuthRig(t)
	for range thLockAfter {
		authRig.th.Fail("alice", "198.51.100.9", "password")
	}
	for range 20 {
		authRig.login("alice", "wrong password here")
	}
	var count int
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE outcome='blocked'`).Scan(&count)
	if count != rateBudget {
		t.Fatalf("événements blocked : %d", count)
	}
	authRig.tick(time.Minute)
	authRig.login("alice", "wrong password here")
	authRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE outcome='blocked'`).Scan(&count)
	if count != rateBudget+1 {
		t.Fatalf("après une minute : %d", count)
	}
	var sum string
	authRig.s.db.QueryRow(`SELECT outcome FROM auth_events WHERE kind='suppressed'`).Scan(&sum)
	if sum != "anon=15 overflow=0 uid=0" {
		t.Fatalf("résumé : %q", sum)
	}
}

func rlRig(t *testing.T) (*authRig, *authAPI, func(string, ...any) int, func() string) {
	localAuthRig := newAuthRig(t)
	localAuthAPI := routesAuth(http.NewServeMux(), localAuthRig.s, localAuthRig.th)
	localAuthRig.s.db.Exec(`DELETE FROM auth_events`)
	count := func(query string, args ...any) (count int) {
		localAuthRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE `+query, args...).Scan(&count)
		return
	}
	// summary passe à la minute suivante et rend le résumé écrit.
	summary := func() (text string) {
		localAuthRig.tick(time.Minute)
		localAuthAPI.rlFlush()
		localAuthRig.s.db.QueryRow(`SELECT outcome FROM auth_events WHERE kind='suppressed' ORDER BY rowid DESC LIMIT 1`).Scan(&text)
		return
	}
	return localAuthRig, localAuthAPI, count, summary
}

func TestRateLogNameFlood(t *testing.T) {
	_, authAPI, count, summary := rlRig(t)
	for index := range 5000 {
		authAPI.rateLog(0, "nom"+strconv.Itoa(index), "198.51.100.1", evLogin, "blocked")
	}
	if localCount := count(`1`); localCount != rateBudget {
		t.Fatalf("une adresse : %d lignes", localCount)
	}
	if text := summary(); text != "anon=4995 overflow=0 uid=0" {
		t.Fatalf("résumé : %q", text)
	}
	if localCount := count(`1`); localCount != rateBudget+1 {
		t.Fatalf("avec le résumé : %d lignes", localCount)
	}
	authAPI.rateLog(0, strings.Repeat("é", 100), "192.0.2.1", evLogin, "busy")
	var got string
	authAPI.s.db.QueryRow(`SELECT name_tried FROM auth_events WHERE ip='192.0.2.1'`).Scan(&got)
	if len([]rune(got)) != 64 {
		t.Fatalf("nom tronqué : %d", len([]rune(got)))
	}
}

func TestRateLogGlobalAndOverflow(t *testing.T) {
	_, authAPI, count, summary := rlRig(t)
	for ip := range 300 {
		for index := range 10 {
			authAPI.rateLog(0, "n"+strconv.Itoa(index), "10.0."+strconv.Itoa(ip/256)+"."+strconv.Itoa(ip%256), evLogin, "blocked")
		}
	}
	if localCount := count(`kind=?`, evLogin); localCount != rlGlobal {
		t.Fatalf("plafond global : %d lignes", localCount)
	}
	if text := summary(); text != "anon=2800 overflow=0 uid=0" {
		t.Fatalf("résumé : %q", text)
	}
	// Table pleine : les adresses suivies gardent leur budget, les nouvelles vont au compte commun.
	authAPI.mu.Lock()
	for index := range rlCap {
		authAPI.rl.src["fill"+strconv.Itoa(index)] = 0
	}
	authAPI.mu.Unlock()
	before := count(`1`)
	authAPI.rateLog(0, "", "203.0.113.1", evLogin, "busy")
	authAPI.rateLog(0, "", "fill3", evLogin, "busy")
	if count(`1`)-before != 1 || count(`ip='fill3'`) != 1 {
		t.Fatal("source suivie touchée par la table pleine")
	}
	if text := summary(); text != "anon=0 overflow=1 uid=0" {
		t.Fatalf("résumé : %q", text)
	}
}

func TestRateLogUIDDedup(t *testing.T) {
	authRig, authAPI, count, summary := rlRig(t)
	for range 10 {
		authAPI.rateLog(authRig.uid, "alice", "198.51.100.1", "mfa_totp", "blocked")
		authAPI.rateLog(authRig.uid, "alice", "198.51.100.1", "mfa_totp", "busy")
	}
	if localCount := count(`user_id=?`, authRig.uid); localCount != 2 {
		t.Fatalf("dédoublonnage par compte : %d", localCount)
	}
	if text := summary(); text != "anon=0 overflow=0 uid=18" {
		t.Fatalf("résumé : %q", text)
	}
	// Les échecs du second facteur sont toujours écrits.
	authRig.s.db.Exec(`DELETE FROM auth_events`)
	authRig.enrolTOTP()
	content, csrf, _ := authRig.partial()
	for range 3 {
		authRig.do("POST", "/v1/auth/mfa/totp", content, csrf, map[string]string{"code": "000000"})
		authRig.tick(time.Minute)
	}
	if localCount := count(`kind='mfa_totp' AND outcome='failure'`); localCount != 3 {
		t.Fatalf("échecs du second facteur : %d", localCount)
	}
}

// Une connexion tenue ne bloque que l'écriture, ni la décision du limiteur ni a.mu.
func TestRateLogNoDBUnderLock(t *testing.T) {
	authRig, authAPI, _, _ := rlRig(t)
	for range rateBudget {
		authAPI.rateLog(0, "", "198.51.100.1", evLogin, "blocked")
	}
	tx, err := authRig.s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	go authAPI.rateLog(0, "", "198.51.100.7", evLogin, "blocked") // écriture bloquée par la transaction, hors verrou
	time.Sleep(20 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		authAPI.rateLog(0, "", "198.51.100.1", evLogin, "blocked") // tu : aucune écriture
		if rel, ok := authAPI.claim("x"); ok {
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
		key, id, ok := parseFactor(in)
		if ok != want.ok || (ok && (key != want.kind || id != want.id)) {
			t.Errorf("parseFactor(%q) = %q %d %v", in, key, id, ok)
		}
	}
}
