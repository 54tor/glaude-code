package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type adminRig struct {
	t     *testing.T
	s     *authStore
	h     http.Handler
	clock *time.Time
}

type adminUser struct {
	id           int64
	cookie, csrf string
	codes        []string
}

// newAdminRig : le serveur complet (gardes, montage paresseux), base de comptes ouverte, horloge des sessions figée.
func newAdminRig(t *testing.T) *adminRig {
	t.Helper()
	authStore := testStore(t)
	resetAuth(t, authStore)
	handler, _, _ := web(t)
	return &adminRig{t: t, s: authStore, h: withTokenOpen(modeTok, handler), clock: testClock(t)}
}

// login ouvre une session complète ; les codes de secours servent aux réauthentifications.
func (localAdminRig *adminRig) login(name string, admin bool) adminUser {
	localAdminRig.t.Helper()
	id, err := localAdminRig.s.AddUser(name, testPw, admin)
	if err != nil {
		localAdminRig.t.Fatal(err)
	}
	codes, err := localAdminRig.s.NewRecoveryCodes(id)
	if err != nil {
		localAdminRig.t.Fatal(err)
	}
	localAdminUser := localAdminRig.session(id)
	localAdminUser.codes = codes
	return localAdminUser
}

func (localAdminRig *adminRig) session(id int64) adminUser {
	localAdminRig.t.Helper()
	content, principal := fullSession(localAdminRig.t, localAdminRig.s, id)
	return adminUser{id: id, cookie: content, csrf: principal.CSRF}
}

func (localAdminUser *adminUser) reauth() map[string]string {
	content := localAdminUser.codes[0]
	localAdminUser.codes = localAdminUser.codes[1:]
	return map[string]string{"password": testPw, "code": content}
}

func (localAdminRig *adminRig) do(method, path string, localAdminUser *adminUser, body any) *httptest.ResponseRecorder {
	localAdminRig.t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Host = testHost
	request.Header.Set("Content-Type", "application/json")
	if localAdminUser != nil {
		request.AddCookie(&http.Cookie{Name: sessCookie, Value: localAdminUser.cookie})
		request.Header.Set(csrfHeader, localAdminUser.csrf)
	}
	recorder := httptest.NewRecorder()
	localAdminRig.h.ServeHTTP(recorder, request)
	return recorder
}

func (localAdminRig *adminRig) ok(recorder *httptest.ResponseRecorder) map[string]any {
	localAdminRig.t.Helper()
	if recorder.Code == 204 {
		return map[string]any{}
	}
	if recorder.Code != 200 {
		localAdminRig.t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
	return jbody(localAdminRig.t, recorder)
}

func (localAdminRig *adminRig) aclRows(home string) int {
	var count int
	if err := localAdminRig.s.db.QueryRow(`SELECT COUNT(*) FROM home_acl WHERE home_id=?`, home).Scan(&count); err != nil {
		localAdminRig.t.Fatal(err)
	}
	return count
}

var adminRoutes = [][2]string{
	{"GET", "/v1/admin/users"}, {"POST", "/v1/admin/users"}, {"PATCH", "/v1/admin/users/1"}, {"POST", "/v1/admin/users/1/mfa-resets"},
	{"PUT", "/v1/admin/users/1/password"}, {"PUT", "/v1/admin/acl"}, {"GET", "/v1/admin/events"}, {"GET", "/v1/admin/homes"}, {"GET", "/v1/admin/sessions?user_id=1"},
	{"POST", "/v1/admin/sessions/revocations?user_id=1"},
}

func TestAdminOnly(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	adminRig.s.Grant(bob.id, mainHomeID, allRights...)
	pid, _ := adminRig.s.AddUser("partiel", testPw, true)
	pc, _ := adminRig.s.NewSession(pid, "127.0.0.1", "ua")
	pp, _ := adminRig.s.Session(pc)
	partial := adminUser{cookie: pc, csrf: pp.CSRF}
	for _, rt := range adminRoutes {
		if recorder := adminRig.do(rt[0], rt[1], &bob, map[string]any{}); recorder.Code != 403 {
			t.Errorf("non-admin %s %s : %d", rt[0], rt[1], recorder.Code)
		}
		if recorder := adminRig.do(rt[0], rt[1], &partial, map[string]any{}); recorder.Code != 401 {
			t.Errorf("session partielle %s %s : %d", rt[0], rt[1], recorder.Code)
		}
		if recorder := adminRig.do(rt[0], rt[1], nil, map[string]any{}); recorder.Code != 401 {
			t.Errorf("anonyme %s %s : %d", rt[0], rt[1], recorder.Code)
		}
		localRecorder := (modeReq{method: rt[0], path: rt[1], bearer: modeTok}).do(adminRig.h)
		if localRecorder.Code != 403 {
			t.Errorf("Bearer %s %s : %d", rt[0], rt[1], localRecorder.Code)
		}
	}
	root.csrf = "faux"
	if recorder := adminRig.do("POST", "/v1/admin/sessions/revocations?user_id="+fid(bob.id), &root, map[string]any{}); recorder.Code != 403 {
		t.Errorf("CSRF faux : %d", recorder.Code)
	}
}

func TestAdminRoutesLazy(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", filepath.Join(t.TempDir(), "gld"))
	resetAuth(t, nil)
	handler, _, _ := web(t)
	for _, rt := range adminRoutes {
		if recorder := (modeReq{method: rt[0], path: rt[1], tokCookie: modeTok}).do(withTokenOpen(modeTok, handler)); recorder.Code != 404 {
			t.Errorf("%s %s sans base : %d", rt[0], rt[1], recorder.Code)
		}
	}
}

func TestAdminCreateUserThenLogin(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	if recorder := adminRig.do("POST", "/v1/admin/users", &root, map[string]any{"name": "carol", "password": "court"}); recorder.Code != 400 || errMsg(t, recorder) != L("srv.user.mdpCourt") {
		t.Fatalf("mot de passe court : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := adminRig.do("POST", "/v1/admin/users", &root, map[string]any{"name": "carol", "password": testPw}); recorder.Code != 403 {
		t.Fatalf("sans réauth : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("POST", "/v1/admin/users", &root, map[string]any{"name": "carol", "password": testPw, "reauth": root.reauth()}))
	if recorder := adminRig.do("POST", "/v1/admin/users", &root, map[string]any{"name": "Carol", "password": testPw, "reauth": root.reauth()}); recorder.Code != 409 {
		t.Fatalf("nom pris : %d", recorder.Code)
	}
	lookup := adminRig.ok(adminRig.do("POST", "/v1/auth/sessions", nil, map[string]string{"name": "carol", "password": testPw}))
	if lookup["enroll"] != true {
		t.Fatalf("premier login : %v", lookup)
	}
	var count int
	adminRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='admin_user' AND user_id=? AND ip<>'' AND outcome LIKE 'add %carol%'`, root.id).Scan(&count)
	if count != 1 {
		t.Fatalf("événement de création : %d", count)
	}
}

func TestDisableRevokesSessions(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	if recorder := adminRig.do("PATCH", "/v1/admin/users/"+fid(root.id), &root, map[string]any{"disabled": true}); recorder.Code != 400 {
		t.Fatalf("se désactiver soi-même : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("PATCH", "/v1/admin/users/"+fid(bob.id), &root, map[string]any{"disabled": true}))
	if recorder := adminRig.do("GET", "/v1/homes", &bob, nil); recorder.Code != 401 {
		t.Fatalf("session de bob après désactivation : %d", recorder.Code)
	}
	if recorder := adminRig.do("PATCH", "/v1/admin/users/"+fid(bob.id), &root, map[string]any{"disabled": false}); recorder.Code != 403 {
		t.Fatalf("réactiver sans réauth : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("PATCH", "/v1/admin/users/"+fid(bob.id), &root, map[string]any{"disabled": false, "reauth": root.reauth()}))
	if recorder := adminRig.do("PATCH", "/v1/admin/users/"+fid(999), &root, map[string]any{"disabled": true}); recorder.Code != 404 {
		t.Fatalf("compte inconnu : %d", recorder.Code)
	}
}

func TestLastAdmin(t *testing.T) {
	authStore := testStore(t)
	number, _ := authStore.AddUser("a", testPw, true)
	limit, _ := authStore.AddUser("b", testPw, true)
	counter, _ := authStore.AddUser("c", testPw, false)
	if last, err := authStore.lastAdmin(number); last || err != nil {
		t.Fatalf("deux admins actifs : %v %v", last, err)
	}
	authStore.SetDisabled(limit, true)
	if last, _ := authStore.lastAdmin(number); !last {
		t.Fatal("b désactivé : a est le dernier")
	}
	if last, _ := authStore.lastAdmin(counter); last {
		t.Fatal("c n'est pas admin : a reste")
	}
	authStore.SetDisabled(limit, false)
	authStore.db.Exec(`UPDATE users SET name='b2' WHERE id=?`, limit)
	before := tamperEvents(t, authStore)
	for range 3 {
		if last, _ := authStore.lastAdmin(number); !last {
			t.Fatal("un admin altéré ne compte pas")
		}
	}
	if count := tamperEvents(t, authStore); count != before {
		t.Fatalf("lastAdmin journalise : %d -> %d", before, count)
	}
}

// Cookie d'admin volé, sans réauth : il ne peut ni désactiver un autre admin ni couper ses sessions ; un compte ordinaire,
// si.
func TestDisableAdminNeedsReauth(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	other := adminRig.login("other", true)
	bob := adminRig.login("bob", false)
	if recorder := adminRig.do("PATCH", "/v1/admin/users/"+fid(other.id), &root, map[string]any{"disabled": true}); recorder.Code != 403 {
		t.Fatalf("désactiver un admin sans réauth : %d", recorder.Code)
	}
	if recorder := adminRig.do("POST", "/v1/admin/sessions/revocations?user_id="+fid(other.id), &root, map[string]any{}); recorder.Code != 403 {
		t.Fatalf("révoquer un admin sans réauth : %d", recorder.Code)
	}
	if recorder := adminRig.do("GET", "/v1/admin/users", &other, nil); recorder.Code != 200 {
		t.Fatalf("l'autre admin a été écarté : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("POST", "/v1/admin/sessions/revocations?user_id="+fid(bob.id), &root, map[string]any{}))
	bob = adminRig.session(bob.id)
	adminRig.ok(adminRig.do("PATCH", "/v1/admin/users/"+fid(bob.id), &root, map[string]any{"disabled": true}))
	adminRig.ok(adminRig.do("POST", "/v1/admin/sessions/revocations?user_id="+fid(other.id), &root, map[string]any{"reauth": root.reauth()}))
	adminRig.ok(adminRig.do("PATCH", "/v1/admin/users/"+fid(other.id), &root, map[string]any{"disabled": true, "reauth": root.reauth()}))
	if user, _ := adminRig.s.UserByName("other"); !user.Disabled {
		t.Fatal("admin non désactivé après réauth")
	}
}

// Accorder et retirer dans la même requête : une écriture refusée n'applique ni l'un ni l'autre.
func TestACLAtomic(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	adminRig.s.Grant(bob.id, mainHomeID, RightMemRead, RightMemWrite)
	if _, err := adminRig.s.db.Exec(`CREATE TRIGGER no_final BEFORE INSERT ON home_acl WHEN NEW.mem_read=1 AND NEW.mem_write=0 BEGIN SELECT RAISE(ABORT,'refus'); END`); err != nil {
		t.Fatal(err)
	}
	recorder := adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "revoke": []string{"mem_write"}})
	if recorder.Code != 500 {
		t.Fatalf("écriture refusée : %d %s", recorder.Code, recorder.Body)
	}
	if lookup, err := adminRig.s.Rights(bob.id, mainHomeID); err != nil || !lookup[RightMemRead] || !lookup[RightMemWrite] {
		t.Fatalf("application partielle : %v %v", lookup, err)
	}
	adminRig.s.db.Exec(`DROP TRIGGER no_final`)
	localLookup := adminRig.ok(adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "revoke": []string{"mem_write"}}))
	if got := localLookup["rights"].([]any); len(got) != 1 || got[0] != "mem_read" {
		t.Fatalf("droits : %v", got)
	}
	var count int
	adminRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='acl' AND user_id=? AND outcome=?`, bob.id, "grant main mem_read; revoke main mem_write").Scan(&count)
	if count != 1 {
		t.Fatalf("un seul événement du store attendu : %d", count)
	}
}

func TestGrantExecNeedsConfirm(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	grant := func(body map[string]any) *httptest.ResponseRecorder {
		body["user_id"], body["home_id"], body["grant"] = bob.id, mainHomeID, []string{"exec", "mem_read"}
		return adminRig.do("PUT", "/v1/admin/acl", &root, body)
	}
	if recorder := grant(map[string]any{"reauth": root.reauth()}); recorder.Code != 400 || errMsg(t, recorder) != L("srv.admin.confirmerExec") {
		t.Fatalf("sans confirm : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := grant(map[string]any{"confirm": true}); recorder.Code != 403 {
		t.Fatalf("sans réauth : %d", recorder.Code)
	}
	// Second facteur validé sur la session : réauth fraîche cinq minutes, mot de passe toujours exigé.
	adminRig.ok(adminRig.do("POST", "/v1/auth/mfa/recovery", &root, map[string]string{"code": root.codes[0]}))
	root.codes = root.codes[1:]
	*adminRig.clock = adminRig.clock.Add(reauthFor + time.Second)
	if recorder := grant(map[string]any{"confirm": true, "reauth": map[string]string{"password": testPw}}); recorder.Code != 403 {
		t.Fatalf("réauth périmée : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("POST", "/v1/auth/mfa/recovery", &root, map[string]string{"code": root.codes[0]}))
	root.codes = root.codes[1:]
	if recorder := grant(map[string]any{"confirm": true, "reauth": map[string]string{"password": "mauvais mot de passe"}}); recorder.Code != 401 {
		t.Fatalf("mauvais mot de passe : %d", recorder.Code)
	}
	lookup := adminRig.ok(grant(map[string]any{"confirm": true, "reauth": map[string]string{"password": testPw}}))
	if got := lookup["rights"].([]any); len(got) != 2 {
		t.Fatalf("droits : %v", got)
	}
	if lookup, _ := adminRig.s.Rights(bob.id, mainHomeID); !lookup[RightExec] || !lookup[RightMemRead] {
		t.Fatalf("en base : %v", lookup)
	}
}

func TestACLChangeLogged(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	lookup := adminRig.ok(adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read", "mem_write"}}))
	if len(lookup["rights"].([]any)) != 2 {
		t.Fatalf("après grant : %v", lookup)
	}
	lookup = adminRig.ok(adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "revoke": []string{"mem_write"}}))
	if got := lookup["rights"].([]any); len(got) != 1 || got[0] != "mem_read" {
		t.Fatalf("après revoke : %v", got)
	}
	var count int
	adminRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='acl' AND user_id=? AND ip='192.0.2.1' AND outcome LIKE 'user=%home=main%'`, root.id).Scan(&count)
	if count != 2 {
		t.Fatalf("événements acl de l'admin : %d", count)
	}
	for _, body := range []map[string]any{
		{"user_id": bob.id, "home_id": "0123456789abcdef", "grant": []string{"mem_read"}},
		{"user_id": bob.id, "home_id": "", "grant": []string{"mem_read"}},
		{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"root"}},
		{"user_id": bob.id, "home_id": mainHomeID},
		{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "revoke": []string{"mem_read"}},
	} {
		if recorder := adminRig.do("PUT", "/v1/admin/acl", &root, body); recorder.Code != 400 {
			t.Errorf("%v : %d %s", body, recorder.Code, recorder.Body)
		}
	}
	if recorder := adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": 999, "home_id": mainHomeID, "grant": []string{"mem_read"}}); recorder.Code != 404 {
		t.Errorf("compte inconnu : %d", recorder.Code)
	}
}

func TestGrantSessionsReadNeedsReauth(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	body := map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"sessions_read"}}
	if recorder := adminRig.do("PUT", "/v1/admin/acl", &root, body); recorder.Code != 403 {
		t.Fatalf("sessions_read sans réauth : %d", recorder.Code)
	}
	body["reauth"] = root.reauth()
	adminRig.ok(adminRig.do("PUT", "/v1/admin/acl", &root, body))
	// Retirer reste immédiat.
	adminRig.ok(adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "revoke": []string{"sessions_read"}}))
}

func TestAdminNoImplicitHomeAccess(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	if recorder := adminRig.do("GET", "/v1/memory/files", &root, nil); recorder.Code != 403 {
		t.Fatalf("admin sans ligne home_acl : %d", recorder.Code)
	}
	if lookup := adminRig.ok(adminRig.do("GET", "/v1/homes", &root, nil)); lookup["main"] != "" {
		t.Fatalf("home principal visible sans droit : %v", lookup)
	}
	if recorder := adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": root.id, "home_id": mainHomeID, "grant": []string{"mem_read"}}); recorder.Code != 403 {
		t.Fatalf("octroi à soi-même sans réauth : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": root.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "reauth": root.reauth()}))
	if recorder := adminRig.do("GET", "/v1/memory/files", &root, nil); recorder.Code != 200 {
		t.Fatalf("après /admin/acl : %d %s", recorder.Code, recorder.Body)
	}
}

func TestAdminUsersList(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	adminRig.s.Grant(bob.id, mainHomeID, RightMemRead)
	adminRig.s.db.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, bob.id)
	lookup := adminRig.ok(adminRig.do("GET", "/v1/admin/users", &root, nil))
	body, _ := json.Marshal(lookup)
	for _, secret := range []string{"pw_hash", "argon2", "secret"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("secret %q dans la liste : %s", secret, body)
		}
	}
	users := lookup["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("comptes : %v", users)
	}
	localLookup := users[1].(map[string]any)
	if localLookup["name"] != "bob" || localLookup["tampered"] != true || localLookup["sessions"].(float64) != 1 {
		t.Fatalf("bob altéré non signalé : %v", localLookup)
	}
	acl := localLookup["acl"].([]any)[0].(map[string]any)
	if acl["home_id"] != mainHomeID || acl["home_name"] != mainHomeID || acl["rights"].([]any)[0] != "mem_read" {
		t.Fatalf("acl : %v", acl)
	}
	anotherLookup := users[0].(map[string]any)
	if anotherLookup["tampered"] != false || anotherLookup["admin"] != true || anotherLookup["factors"].(map[string]any)["recovery"].(float64) != 10 {
		t.Fatalf("root : %v", anotherLookup)
	}
	if tamperEvents(t, adminRig.s) == 0 {
		t.Fatal("altération non journalisée")
	}
}

func TestAdminResetMFAAndPassword(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	if recorder := adminRig.do("POST", "/v1/admin/users/"+fid(bob.id)+"/mfa-resets", &root, map[string]any{}); recorder.Code != 403 {
		t.Fatalf("reset-mfa sans réauth : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("POST", "/v1/admin/users/"+fid(bob.id)+"/mfa-resets", &root, map[string]any{"reauth": root.reauth()}))
	if factorState, _ := adminRig.s.factors(bob.id); !factorState.none() || factorState.Recovery != 0 {
		t.Fatalf("facteurs après reset : %+v", factorState)
	}
	if recorder := adminRig.do("GET", "/v1/homes", &bob, nil); recorder.Code != 401 {
		t.Fatalf("session après reset-mfa : %d", recorder.Code)
	}
	bob = adminRig.session(bob.id)
	const newPw = "un tout autre mot de passe"
	adminRig.ok(adminRig.do("PUT", "/v1/admin/users/"+fid(bob.id)+"/password", &root, map[string]any{"password": newPw, "reauth": root.reauth()}))
	if recorder := adminRig.do("GET", "/v1/homes", &bob, nil); recorder.Code != 401 {
		t.Fatalf("session après changement de mot de passe : %d", recorder.Code)
	}
	user, _ := adminRig.s.UserByName("bob")
	if !checkPassword(user.PwHash, newPw) {
		t.Fatal("mot de passe non changé")
	}
	var count int
	adminRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND ip<>'' AND kind IN ('mfa_reset','admin_user')`, root.id).Scan(&count)
	if count != 2 {
		t.Fatalf("événements de l'admin : %d", count)
	}
}

func TestAdminSessions(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	lookup := adminRig.ok(adminRig.do("GET", "/v1/admin/sessions?user_id="+strconv.FormatInt(bob.id, 10), &root, nil))
	ss := lookup["sessions"].([]any)
	if len(ss) != 1 || len(ss[0].(map[string]any)["id"].(string)) != 8 {
		t.Fatalf("sessions : %v", ss)
	}
	adminRig.ok(adminRig.do("POST", "/v1/admin/sessions/revocations?user_id="+fid(bob.id), &root, map[string]any{}))
	if recorder := adminRig.do("GET", "/v1/homes", &bob, nil); recorder.Code != 401 {
		t.Fatalf("session révoquée : %d", recorder.Code)
	}
	if recorder := adminRig.do("GET", "/v1/admin/sessions?user_id=x", &root, nil); recorder.Code != 400 {
		t.Fatalf("user_id invalide : %d", recorder.Code)
	}
}

func TestAdminEvents(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	tx, _ := adminRig.s.db.Begin()
	for index := range 600 {
		tx.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`, index, root.id, "198.51.100.7", "login", "success")
	}
	tx.Exec(`INSERT INTO auth_events(at,user_id,kind,outcome) VALUES(1,NULL,'tamper','users')`)
	tx.Commit()
	events := func(query string) []any {
		return adminRig.ok(adminRig.do("GET", "/v1/admin/events"+query, &root, nil))["events"].([]any)
	}
	if count := len(events("")); count != eventsDefault {
		t.Fatalf("défaut : %d", count)
	}
	if count := len(events("?limit=1000")); count != eventsMax {
		t.Fatalf("plafond : %d", count)
	}
	tam := events("?kind=tamper")
	if len(tam) != 1 || tam[0].(map[string]any)["outcome"] != "users" {
		t.Fatalf("filtre : %v", tam)
	}
	page := events("?kind=login&limit=2")
	e0, e1 := page[0].(map[string]any), page[1].(map[string]any)
	if e0["id"].(float64) <= e1["id"].(float64) || e0["user"] != "root" || e0["ip"] != "198.51.100.7" {
		t.Fatalf("ordre ou résolution : %v", page)
	}
	next := events("?kind=login&limit=1&before=" + strconv.FormatFloat(e1["id"].(float64), 'f', 0, 64))
	if next[0].(map[string]any)["id"].(float64) >= e1["id"].(float64) {
		t.Fatalf("pagination : %v", next)
	}
	for _, query := range []string{"?limit=0", "?limit=x", "?before=-1"} {
		if recorder := adminRig.do("GET", "/v1/admin/events"+query, &root, nil); recorder.Code != 400 {
			t.Errorf("%s : %d", query, recorder.Code)
		}
	}
}

// Retirer un home par PUT /v1/homes supprime ses lignes home_acl, et seulement les siennes.
func TestHomesSaveCascadeACL(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	da, db := t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "a", Dir: da}, {Name: "b", Dir: db}}); err != nil {
		t.Fatal(err)
	}
	ida, idb := mustHomeID(t, "a"), mustHomeID(t, "b")
	for _, id := range []string{mainHomeID, ida, idb} {
		adminRig.s.Grant(bob.id, id, RightMemRead)
		adminRig.s.Grant(root.id, id, RightMemRead)
	}
	// Liste partielle sans Removed (l'admin ne voit pas tous les homes) : rien n'est perdu.
	save := func(body map[string]any) *httptest.ResponseRecorder {
		recorder := adminRig.do("PUT", "/v1/homes", &root, body)
		if recorder.Code != 204 {
			t.Fatalf("PUT /v1/homes : %d %s", recorder.Code, recorder.Body)
		}
		return recorder
	}
	save(map[string]any{"Homes": []Home{{Name: "a", Dir: da, ID: ida}}})
	if len(readHomes()) != 2 || adminRig.aclRows(idb) != 2 || adminRig.aclRows(ida) != 2 {
		t.Fatalf("liste partielle : homes=%d b=%d a=%d", len(readHomes()), adminRig.aclRows(idb), adminRig.aclRows(ida))
	}
	save(map[string]any{"Homes": []Home{{Name: "a", Dir: da, ID: ida}}, "Removed": []string{idb}})
	if len(readHomes()) != 1 || adminRig.aclRows(idb) != 0 || adminRig.aclRows(ida) != 2 || adminRig.aclRows(mainHomeID) != 2 {
		t.Fatalf("cascade : b=%d a=%d main=%d", adminRig.aclRows(idb), adminRig.aclRows(ida), adminRig.aclRows(mainHomeID))
	}
	var count int
	adminRig.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='acl' AND user_id=? AND ip<>'' AND outcome=?`, root.id, "homes_removed="+idb+" rows=2").Scan(&count)
	if count != 1 {
		t.Fatalf("événement de cascade : %d", count)
	}
	if recorder := adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": idb, "grant": []string{"mem_read"}}); recorder.Code != 400 {
		t.Fatalf("home retiré : %d", recorder.Code)
	}
}

// Mode multi-utilisateur : un nom déjà pris ne se reprend qu'avec l'identifiant de son home, même invisible de l'admin.
func TestHomesSaveNomPris(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	dfoo, dx := t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "foo", Dir: dfoo}}); err != nil {
		t.Fatal(err)
	}
	h1 := mustHomeID(t, "foo")
	adminRig.s.Grant(bob.id, h1, RightMemRead)
	for _, body := range []map[string]any{
		{"homes": []Home{{Name: "foo", Dir: dx}}},
		{"homes": []Home{{Name: " foo ", Dir: dx}}},
		{"homes": []Home{{Name: "foo", Dir: dx, ID: "autre"}}},
	} {
		if recorder := adminRig.do("PUT", "/v1/homes", &root, body); recorder.Code != 409 || !strings.Contains(recorder.Body.String(), "foo") {
			t.Fatalf("%v : %d %s", body, recorder.Code, recorder.Body)
		}
	}
	if hs := readHomes(); len(hs) != 1 || hs[0].ID != h1 || hs[0].Dir != dfoo || adminRig.aclRows(h1) != 1 {
		t.Fatalf("home touché : %+v acl=%d", hs, adminRig.aclRows(h1))
	}
	if recorder := adminRig.do("PUT", "/v1/homes", &root, map[string]any{"homes": []Home{{Name: "foo", Dir: dx, ID: h1}}}); recorder.Code != 204 {
		t.Fatalf("mise à jour : %d %s", recorder.Code, recorder.Body)
	}
	if home, _ := homeByName("foo"); home.ID != h1 || home.Dir != dx {
		t.Fatalf("mise à jour : %+v", home)
	}
	if recorder := adminRig.do("PUT", "/v1/homes", &root, map[string]any{"homes": []Home{{Name: "bar", Dir: dfoo}}}); recorder.Code != 204 {
		t.Fatalf("création : %d %s", recorder.Code, recorder.Body)
	}
	if id := mustHomeID(t, "bar"); id == h1 || len(readHomes()) != 2 || adminRig.aclRows(h1) != 1 {
		t.Fatalf("création : %s %+v", id, readHomes())
	}
}

func TestAdminHomesAll(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	da, db := t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "a", Dir: da}, {Name: "b", Dir: db}}); err != nil {
		t.Fatal(err)
	}
	recorder := adminRig.do("GET", "/v1/admin/homes", &root, nil)
	lookup := adminRig.ok(recorder)
	if strings.Contains(recorder.Body.String(), da) || strings.Contains(recorder.Body.String(), db) {
		t.Fatalf("chemin exposé : %s", recorder.Body)
	}
	hs := lookup["homes"].([]any)
	want := []string{mainHomeID + "/" + mainHomeID, mustHomeID(t, "a") + "/a", mustHomeID(t, "b") + "/b"}
	if len(hs) != len(want) {
		t.Fatalf("homes : %v", hs)
	}
	for index, value := range hs {
		lookup := value.(map[string]any)
		if got := lookup["id"].(string) + "/" + lookup["name"].(string); got != want[index] || len(lookup) != 2 {
			t.Fatalf("home %d : %v, attendu %s", index, lookup, want[index])
		}
	}
}

func TestAdminGrantFsRules(t *testing.T) {
	adminRig := newAdminRig(t)
	root := adminRig.login("root", true)
	bob := adminRig.login("bob", false)
	recorder := adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_write"}, "reauth": root.reauth()})
	if recorder.Code != 400 {
		t.Fatalf("fs_write sans confirm : %d", recorder.Code)
	}
	recorder = adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_write"}, "confirm": true})
	if recorder.Code != 403 {
		t.Fatalf("fs_write sans réauth : %d", recorder.Code)
	}
	recorder = adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_read"}})
	if recorder.Code != 403 {
		t.Fatalf("fs_read sans réauth : %d", recorder.Code)
	}
	adminRig.ok(adminRig.do("PUT", "/v1/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_read", "fs_write"}, "confirm": true, "reauth": root.reauth()}))
}
