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
	s := testStore(t)
	resetAuth(t, s)
	h, _, _ := web(t)
	return &adminRig{t: t, s: s, h: withTokenOpen(modeTok, h), clock: testClock(t)}
}

// login ouvre une session complète ; les codes de secours servent aux réauthentifications.
func (g *adminRig) login(name string, admin bool) adminUser {
	g.t.Helper()
	id, err := g.s.AddUser(name, testPw, admin)
	if err != nil {
		g.t.Fatal(err)
	}
	codes, err := g.s.NewRecoveryCodes(id)
	if err != nil {
		g.t.Fatal(err)
	}
	u := g.session(id)
	u.codes = codes
	return u
}

func (g *adminRig) session(id int64) adminUser {
	g.t.Helper()
	c, p := fullSession(g.t, g.s, id)
	return adminUser{id: id, cookie: c, csrf: p.CSRF}
}

func (u *adminUser) reauth() map[string]string {
	c := u.codes[0]
	u.codes = u.codes[1:]
	return map[string]string{"password": testPw, "code": c}
}

func (g *adminRig) do(method, path string, u *adminUser, body any) *httptest.ResponseRecorder {
	g.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Host = testHost
	r.Header.Set("Content-Type", "application/json")
	if u != nil {
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: u.cookie})
		r.Header.Set(csrfHeader, u.csrf)
	}
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	return w
}

func (g *adminRig) ok(w *httptest.ResponseRecorder) map[string]any {
	g.t.Helper()
	if w.Code != 200 {
		g.t.Fatalf("%d %s", w.Code, w.Body)
	}
	return jbody(g.t, w)
}

func (g *adminRig) aclRows(home string) int {
	var n int
	if err := g.s.db.QueryRow(`SELECT COUNT(*) FROM home_acl WHERE home_id=?`, home).Scan(&n); err != nil {
		g.t.Fatal(err)
	}
	return n
}

var adminRoutes = [][2]string{
	{"GET", "/admin/users"}, {"POST", "/admin/users"}, {"POST", "/admin/users/disable"}, {"POST", "/admin/users/reset-mfa"},
	{"POST", "/admin/users/password"}, {"POST", "/admin/acl"}, {"GET", "/admin/events"}, {"GET", "/admin/homes"}, {"GET", "/admin/sessions?user_id=1"},
	{"POST", "/admin/sessions/revoke"},
}

func TestAdminOnly(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	g.s.Grant(bob.id, mainHomeID, allRights...)
	pid, _ := g.s.AddUser("partiel", testPw, true)
	pc, _ := g.s.NewSession(pid, "127.0.0.1", "ua")
	pp, _ := g.s.Session(pc)
	partial := adminUser{cookie: pc, csrf: pp.CSRF}
	for _, rt := range adminRoutes {
		if w := g.do(rt[0], rt[1], &bob, map[string]any{}); w.Code != 403 {
			t.Errorf("non-admin %s %s : %d", rt[0], rt[1], w.Code)
		}
		if w := g.do(rt[0], rt[1], &partial, map[string]any{}); w.Code != 401 {
			t.Errorf("session partielle %s %s : %d", rt[0], rt[1], w.Code)
		}
		if w := g.do(rt[0], rt[1], nil, map[string]any{}); w.Code != 401 {
			t.Errorf("anonyme %s %s : %d", rt[0], rt[1], w.Code)
		}
		w := (modeReq{method: rt[0], path: rt[1], bearer: modeTok}).do(g.h)
		if w.Code != 403 {
			t.Errorf("Bearer %s %s : %d", rt[0], rt[1], w.Code)
		}
	}
	root.csrf = "faux"
	if w := g.do("POST", "/admin/sessions/revoke", &root, map[string]any{"user_id": bob.id}); w.Code != 403 {
		t.Errorf("CSRF faux : %d", w.Code)
	}
}

func TestAdminRoutesLazy(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", filepath.Join(t.TempDir(), "gld"))
	resetAuth(t, nil)
	h, _, _ := web(t)
	for _, rt := range adminRoutes {
		if w := (modeReq{method: rt[0], path: rt[1], tokCookie: modeTok}).do(withTokenOpen(modeTok, h)); w.Code != 404 {
			t.Errorf("%s %s sans base : %d", rt[0], rt[1], w.Code)
		}
	}
}

func TestAdminCreateUserThenLogin(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	if w := g.do("POST", "/admin/users", &root, map[string]any{"name": "carol", "password": "court"}); w.Code != 400 || jbody(t, w)["out"] != L("srv.user.mdpCourt") {
		t.Fatalf("mot de passe court : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/admin/users", &root, map[string]any{"name": "carol", "password": testPw}); w.Code != 403 {
		t.Fatalf("sans réauth : %d", w.Code)
	}
	g.ok(g.do("POST", "/admin/users", &root, map[string]any{"name": "carol", "password": testPw, "reauth": root.reauth()}))
	if w := g.do("POST", "/admin/users", &root, map[string]any{"name": "Carol", "password": testPw, "reauth": root.reauth()}); w.Code != 409 {
		t.Fatalf("nom pris : %d", w.Code)
	}
	m := g.ok(g.do("POST", "/auth/login", nil, map[string]string{"name": "carol", "password": testPw}))
	if m["enroll"] != true {
		t.Fatalf("premier login : %v", m)
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='admin_user' AND user_id=? AND ip<>'' AND outcome LIKE 'add %carol%'`, root.id).Scan(&n)
	if n != 1 {
		t.Fatalf("événement de création : %d", n)
	}
}

func TestDisableRevokesSessions(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	if w := g.do("POST", "/admin/users/disable", &root, map[string]any{"id": root.id, "disabled": true}); w.Code != 400 {
		t.Fatalf("se désactiver soi-même : %d", w.Code)
	}
	g.ok(g.do("POST", "/admin/users/disable", &root, map[string]any{"id": bob.id, "disabled": true}))
	if w := g.do("GET", "/v1/homes", &bob, nil); w.Code != 401 {
		t.Fatalf("session de bob après désactivation : %d", w.Code)
	}
	if w := g.do("POST", "/admin/users/disable", &root, map[string]any{"id": bob.id, "disabled": false}); w.Code != 403 {
		t.Fatalf("réactiver sans réauth : %d", w.Code)
	}
	g.ok(g.do("POST", "/admin/users/disable", &root, map[string]any{"id": bob.id, "disabled": false, "reauth": root.reauth()}))
	if w := g.do("POST", "/admin/users/disable", &root, map[string]any{"id": 999, "disabled": true}); w.Code != 404 {
		t.Fatalf("compte inconnu : %d", w.Code)
	}
}

func TestLastAdmin(t *testing.T) {
	s := testStore(t)
	a, _ := s.AddUser("a", testPw, true)
	b, _ := s.AddUser("b", testPw, true)
	c, _ := s.AddUser("c", testPw, false)
	if last, err := s.lastAdmin(a); last || err != nil {
		t.Fatalf("deux admins actifs : %v %v", last, err)
	}
	s.SetDisabled(b, true)
	if last, _ := s.lastAdmin(a); !last {
		t.Fatal("b désactivé : a est le dernier")
	}
	if last, _ := s.lastAdmin(c); last {
		t.Fatal("c n'est pas admin : a reste")
	}
	s.SetDisabled(b, false)
	s.db.Exec(`UPDATE users SET name='b2' WHERE id=?`, b)
	before := tamperEvents(t, s)
	for range 3 {
		if last, _ := s.lastAdmin(a); !last {
			t.Fatal("un admin altéré ne compte pas")
		}
	}
	if n := tamperEvents(t, s); n != before {
		t.Fatalf("lastAdmin journalise : %d -> %d", before, n)
	}
}

// Cookie d'admin volé, sans réauth : il ne peut ni désactiver un autre admin ni couper ses sessions ; un compte ordinaire,
// si.
func TestDisableAdminNeedsReauth(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	other := g.login("other", true)
	bob := g.login("bob", false)
	if w := g.do("POST", "/admin/users/disable", &root, map[string]any{"id": other.id, "disabled": true}); w.Code != 403 {
		t.Fatalf("désactiver un admin sans réauth : %d", w.Code)
	}
	if w := g.do("POST", "/admin/sessions/revoke", &root, map[string]any{"user_id": other.id}); w.Code != 403 {
		t.Fatalf("révoquer un admin sans réauth : %d", w.Code)
	}
	if w := g.do("GET", "/admin/users", &other, nil); w.Code != 200 {
		t.Fatalf("l'autre admin a été écarté : %d", w.Code)
	}
	g.ok(g.do("POST", "/admin/sessions/revoke", &root, map[string]any{"user_id": bob.id}))
	bob = g.session(bob.id)
	g.ok(g.do("POST", "/admin/users/disable", &root, map[string]any{"id": bob.id, "disabled": true}))
	g.ok(g.do("POST", "/admin/sessions/revoke", &root, map[string]any{"user_id": other.id, "reauth": root.reauth()}))
	g.ok(g.do("POST", "/admin/users/disable", &root, map[string]any{"id": other.id, "disabled": true, "reauth": root.reauth()}))
	if u, _ := g.s.UserByName("other"); !u.Disabled {
		t.Fatal("admin non désactivé après réauth")
	}
}

// Accorder et retirer dans la même requête : une écriture refusée n'applique ni l'un ni l'autre.
func TestACLAtomic(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	g.s.Grant(bob.id, mainHomeID, RightMemRead, RightMemWrite)
	if _, err := g.s.db.Exec(`CREATE TRIGGER no_final BEFORE INSERT ON home_acl WHEN NEW.mem_read=1 AND NEW.mem_write=0 BEGIN SELECT RAISE(ABORT,'refus'); END`); err != nil {
		t.Fatal(err)
	}
	w := g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "revoke": []string{"mem_write"}})
	if w.Code != 500 {
		t.Fatalf("écriture refusée : %d %s", w.Code, w.Body)
	}
	if m, err := g.s.Rights(bob.id, mainHomeID); err != nil || !m[RightMemRead] || !m[RightMemWrite] {
		t.Fatalf("application partielle : %v %v", m, err)
	}
	g.s.db.Exec(`DROP TRIGGER no_final`)
	m := g.ok(g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "revoke": []string{"mem_write"}}))
	if got := m["rights"].([]any); len(got) != 1 || got[0] != "mem_read" {
		t.Fatalf("droits : %v", got)
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='acl' AND user_id=? AND outcome=?`, bob.id, "grant main mem_read; revoke main mem_write").Scan(&n)
	if n != 1 {
		t.Fatalf("un seul événement du store attendu : %d", n)
	}
}

func TestGrantExecNeedsConfirm(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	grant := func(body map[string]any) *httptest.ResponseRecorder {
		body["user_id"], body["home_id"], body["grant"] = bob.id, mainHomeID, []string{"exec", "mem_read"}
		return g.do("POST", "/admin/acl", &root, body)
	}
	if w := grant(map[string]any{"reauth": root.reauth()}); w.Code != 400 || jbody(t, w)["out"] != L("srv.admin.confirmerExec") {
		t.Fatalf("sans confirm : %d %s", w.Code, w.Body)
	}
	if w := grant(map[string]any{"confirm": true}); w.Code != 403 {
		t.Fatalf("sans réauth : %d", w.Code)
	}
	// Second facteur validé sur la session : réauth fraîche cinq minutes, mot de passe toujours exigé.
	g.ok(g.do("POST", "/auth/mfa/recovery", &root, map[string]string{"code": root.codes[0]}))
	root.codes = root.codes[1:]
	*g.clock = g.clock.Add(reauthFor + time.Second)
	if w := grant(map[string]any{"confirm": true, "reauth": map[string]string{"password": testPw}}); w.Code != 403 {
		t.Fatalf("réauth périmée : %d", w.Code)
	}
	g.ok(g.do("POST", "/auth/mfa/recovery", &root, map[string]string{"code": root.codes[0]}))
	root.codes = root.codes[1:]
	if w := grant(map[string]any{"confirm": true, "reauth": map[string]string{"password": "mauvais mot de passe"}}); w.Code != 401 {
		t.Fatalf("mauvais mot de passe : %d", w.Code)
	}
	m := g.ok(grant(map[string]any{"confirm": true, "reauth": map[string]string{"password": testPw}}))
	if got := m["rights"].([]any); len(got) != 2 {
		t.Fatalf("droits : %v", got)
	}
	if r, _ := g.s.Rights(bob.id, mainHomeID); !r[RightExec] || !r[RightMemRead] {
		t.Fatalf("en base : %v", r)
	}
}

func TestACLChangeLogged(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	m := g.ok(g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read", "mem_write"}}))
	if len(m["rights"].([]any)) != 2 {
		t.Fatalf("après grant : %v", m)
	}
	m = g.ok(g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "revoke": []string{"mem_write"}}))
	if got := m["rights"].([]any); len(got) != 1 || got[0] != "mem_read" {
		t.Fatalf("après revoke : %v", got)
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='acl' AND user_id=? AND ip='192.0.2.1' AND outcome LIKE 'user=%home=main%'`, root.id).Scan(&n)
	if n != 2 {
		t.Fatalf("événements acl de l'admin : %d", n)
	}
	for _, body := range []map[string]any{
		{"user_id": bob.id, "home_id": "0123456789abcdef", "grant": []string{"mem_read"}},
		{"user_id": bob.id, "home_id": "", "grant": []string{"mem_read"}},
		{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"root"}},
		{"user_id": bob.id, "home_id": mainHomeID},
		{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "revoke": []string{"mem_read"}},
	} {
		if w := g.do("POST", "/admin/acl", &root, body); w.Code != 400 {
			t.Errorf("%v : %d %s", body, w.Code, w.Body)
		}
	}
	if w := g.do("POST", "/admin/acl", &root, map[string]any{"user_id": 999, "home_id": mainHomeID, "grant": []string{"mem_read"}}); w.Code != 404 {
		t.Errorf("compte inconnu : %d", w.Code)
	}
}

func TestGrantSessionsReadNeedsReauth(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	body := map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"sessions_read"}}
	if w := g.do("POST", "/admin/acl", &root, body); w.Code != 403 {
		t.Fatalf("sessions_read sans réauth : %d", w.Code)
	}
	body["reauth"] = root.reauth()
	g.ok(g.do("POST", "/admin/acl", &root, body))
	// Retirer reste immédiat.
	g.ok(g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "revoke": []string{"sessions_read"}}))
}

func TestAdminNoImplicitHomeAccess(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	if w := g.do("GET", "/v1/memory/files", &root, nil); w.Code != 403 {
		t.Fatalf("admin sans ligne home_acl : %d", w.Code)
	}
	if m := g.ok(g.do("GET", "/v1/homes", &root, nil)); m["main"] != "" {
		t.Fatalf("home principal visible sans droit : %v", m)
	}
	if w := g.do("POST", "/admin/acl", &root, map[string]any{"user_id": root.id, "home_id": mainHomeID, "grant": []string{"mem_read"}}); w.Code != 403 {
		t.Fatalf("octroi à soi-même sans réauth : %d", w.Code)
	}
	g.ok(g.do("POST", "/admin/acl", &root, map[string]any{"user_id": root.id, "home_id": mainHomeID, "grant": []string{"mem_read"}, "reauth": root.reauth()}))
	if w := g.do("GET", "/v1/memory/files", &root, nil); w.Code != 200 {
		t.Fatalf("après /admin/acl : %d %s", w.Code, w.Body)
	}
}

func TestAdminUsersList(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	g.s.Grant(bob.id, mainHomeID, RightMemRead)
	g.s.db.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, bob.id)
	m := g.ok(g.do("GET", "/admin/users", &root, nil))
	body, _ := json.Marshal(m)
	for _, secret := range []string{"pw_hash", "argon2", "secret"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("secret %q dans la liste : %s", secret, body)
		}
	}
	users := m["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("comptes : %v", users)
	}
	b := users[1].(map[string]any)
	if b["name"] != "bob" || b["tampered"] != true || b["sessions"].(float64) != 1 {
		t.Fatalf("bob altéré non signalé : %v", b)
	}
	acl := b["acl"].([]any)[0].(map[string]any)
	if acl["home_id"] != mainHomeID || acl["home_name"] != mainHomeID || acl["rights"].([]any)[0] != "mem_read" {
		t.Fatalf("acl : %v", acl)
	}
	r := users[0].(map[string]any)
	if r["tampered"] != false || r["admin"] != true || r["factors"].(map[string]any)["recovery"].(float64) != 10 {
		t.Fatalf("root : %v", r)
	}
	if tamperEvents(t, g.s) == 0 {
		t.Fatal("altération non journalisée")
	}
}

func TestAdminResetMFAAndPassword(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	if w := g.do("POST", "/admin/users/reset-mfa", &root, map[string]any{"id": bob.id}); w.Code != 403 {
		t.Fatalf("reset-mfa sans réauth : %d", w.Code)
	}
	g.ok(g.do("POST", "/admin/users/reset-mfa", &root, map[string]any{"id": bob.id, "reauth": root.reauth()}))
	if f, _ := g.s.factors(bob.id); !f.none() || f.Recovery != 0 {
		t.Fatalf("facteurs après reset : %+v", f)
	}
	if w := g.do("GET", "/v1/homes", &bob, nil); w.Code != 401 {
		t.Fatalf("session après reset-mfa : %d", w.Code)
	}
	bob = g.session(bob.id)
	const newPw = "un tout autre mot de passe"
	g.ok(g.do("POST", "/admin/users/password", &root, map[string]any{"id": bob.id, "password": newPw, "reauth": root.reauth()}))
	if w := g.do("GET", "/v1/homes", &bob, nil); w.Code != 401 {
		t.Fatalf("session après changement de mot de passe : %d", w.Code)
	}
	u, _ := g.s.UserByName("bob")
	if !checkPassword(u.PwHash, newPw) {
		t.Fatal("mot de passe non changé")
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE user_id=? AND ip<>'' AND kind IN ('mfa_reset','admin_user')`, root.id).Scan(&n)
	if n != 2 {
		t.Fatalf("événements de l'admin : %d", n)
	}
}

func TestAdminSessions(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	m := g.ok(g.do("GET", "/admin/sessions?user_id="+strconv.FormatInt(bob.id, 10), &root, nil))
	ss := m["sessions"].([]any)
	if len(ss) != 1 || len(ss[0].(map[string]any)["id"].(string)) != 8 {
		t.Fatalf("sessions : %v", ss)
	}
	g.ok(g.do("POST", "/admin/sessions/revoke", &root, map[string]any{"user_id": bob.id}))
	if w := g.do("GET", "/v1/homes", &bob, nil); w.Code != 401 {
		t.Fatalf("session révoquée : %d", w.Code)
	}
	if w := g.do("GET", "/admin/sessions?user_id=x", &root, nil); w.Code != 400 {
		t.Fatalf("user_id invalide : %d", w.Code)
	}
}

func TestAdminEvents(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	tx, _ := g.s.db.Begin()
	for i := range 600 {
		tx.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`, i, root.id, "198.51.100.7", "login", "success")
	}
	tx.Exec(`INSERT INTO auth_events(at,user_id,kind,outcome) VALUES(1,NULL,'tamper','users')`)
	tx.Commit()
	events := func(q string) []any {
		return g.ok(g.do("GET", "/admin/events"+q, &root, nil))["events"].([]any)
	}
	if n := len(events("")); n != eventsDefault {
		t.Fatalf("défaut : %d", n)
	}
	if n := len(events("?limit=1000")); n != eventsMax {
		t.Fatalf("plafond : %d", n)
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
	for _, q := range []string{"?limit=0", "?limit=x", "?before=-1"} {
		if w := g.do("GET", "/admin/events"+q, &root, nil); w.Code != 400 {
			t.Errorf("%s : %d", q, w.Code)
		}
	}
}

// Retirer un home par PUT /v1/homes supprime ses lignes home_acl, et seulement les siennes.
func TestHomesSaveCascadeACL(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	da, db := t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "a", Dir: da}, {Name: "b", Dir: db}}); err != nil {
		t.Fatal(err)
	}
	ida, idb := mustHomeID(t, "a"), mustHomeID(t, "b")
	for _, id := range []string{mainHomeID, ida, idb} {
		g.s.Grant(bob.id, id, RightMemRead)
		g.s.Grant(root.id, id, RightMemRead)
	}
	// Liste partielle sans Removed (l'admin ne voit pas tous les homes) : rien n'est perdu.
	save := func(body map[string]any) *httptest.ResponseRecorder {
		w := g.do("PUT", "/v1/homes", &root, body)
		if w.Code != 204 {
			t.Fatalf("PUT /v1/homes : %d %s", w.Code, w.Body)
		}
		return w
	}
	save(map[string]any{"Homes": []Home{{Name: "a", Dir: da, ID: ida}}})
	if len(readHomes()) != 2 || g.aclRows(idb) != 2 || g.aclRows(ida) != 2 {
		t.Fatalf("liste partielle : homes=%d b=%d a=%d", len(readHomes()), g.aclRows(idb), g.aclRows(ida))
	}
	save(map[string]any{"Homes": []Home{{Name: "a", Dir: da, ID: ida}}, "Removed": []string{idb}})
	if len(readHomes()) != 1 || g.aclRows(idb) != 0 || g.aclRows(ida) != 2 || g.aclRows(mainHomeID) != 2 {
		t.Fatalf("cascade : b=%d a=%d main=%d", g.aclRows(idb), g.aclRows(ida), g.aclRows(mainHomeID))
	}
	var n int
	g.s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='acl' AND user_id=? AND ip<>'' AND outcome=?`, root.id, "homes_removed="+idb+" rows=2").Scan(&n)
	if n != 1 {
		t.Fatalf("événement de cascade : %d", n)
	}
	if w := g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": idb, "grant": []string{"mem_read"}}); w.Code != 400 {
		t.Fatalf("home retiré : %d", w.Code)
	}
}

// Mode multi-utilisateur : un nom déjà pris ne se reprend qu'avec l'identifiant de son home, même invisible de l'admin.
func TestHomesSaveNomPris(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	dfoo, dx := t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "foo", Dir: dfoo}}); err != nil {
		t.Fatal(err)
	}
	h1 := mustHomeID(t, "foo")
	g.s.Grant(bob.id, h1, RightMemRead)
	for _, body := range []map[string]any{
		{"homes": []Home{{Name: "foo", Dir: dx}}},
		{"homes": []Home{{Name: " foo ", Dir: dx}}},
		{"homes": []Home{{Name: "foo", Dir: dx, ID: "autre"}}},
	} {
		if w := g.do("PUT", "/v1/homes", &root, body); w.Code != 409 || !strings.Contains(w.Body.String(), "foo") {
			t.Fatalf("%v : %d %s", body, w.Code, w.Body)
		}
	}
	if hs := readHomes(); len(hs) != 1 || hs[0].ID != h1 || hs[0].Dir != dfoo || g.aclRows(h1) != 1 {
		t.Fatalf("home touché : %+v acl=%d", hs, g.aclRows(h1))
	}
	if w := g.do("PUT", "/v1/homes", &root, map[string]any{"homes": []Home{{Name: "foo", Dir: dx, ID: h1}}}); w.Code != 204 {
		t.Fatalf("mise à jour : %d %s", w.Code, w.Body)
	}
	if h, _ := homeByName("foo"); h.ID != h1 || h.Dir != dx {
		t.Fatalf("mise à jour : %+v", h)
	}
	if w := g.do("PUT", "/v1/homes", &root, map[string]any{"homes": []Home{{Name: "bar", Dir: dfoo}}}); w.Code != 204 {
		t.Fatalf("création : %d %s", w.Code, w.Body)
	}
	if id := mustHomeID(t, "bar"); id == h1 || len(readHomes()) != 2 || g.aclRows(h1) != 1 {
		t.Fatalf("création : %s %+v", id, readHomes())
	}
}

func TestAdminHomesAll(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	da, db := t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "a", Dir: da}, {Name: "b", Dir: db}}); err != nil {
		t.Fatal(err)
	}
	w := g.do("GET", "/admin/homes", &root, nil)
	m := g.ok(w)
	if strings.Contains(w.Body.String(), da) || strings.Contains(w.Body.String(), db) {
		t.Fatalf("chemin exposé : %s", w.Body)
	}
	hs := m["homes"].([]any)
	want := []string{mainHomeID + "/" + mainHomeID, mustHomeID(t, "a") + "/a", mustHomeID(t, "b") + "/b"}
	if len(hs) != len(want) {
		t.Fatalf("homes : %v", hs)
	}
	for i, h := range hs {
		h := h.(map[string]any)
		if got := h["id"].(string) + "/" + h["name"].(string); got != want[i] || len(h) != 2 {
			t.Fatalf("home %d : %v, attendu %s", i, h, want[i])
		}
	}
}

func TestAdminGrantFsRules(t *testing.T) {
	g := newAdminRig(t)
	root := g.login("root", true)
	bob := g.login("bob", false)
	w := g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_write"}, "reauth": root.reauth()})
	if w.Code != 400 {
		t.Fatalf("fs_write sans confirm : %d", w.Code)
	}
	w = g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_write"}, "confirm": true})
	if w.Code != 403 {
		t.Fatalf("fs_write sans réauth : %d", w.Code)
	}
	w = g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_read"}})
	if w.Code != 403 {
		t.Fatalf("fs_read sans réauth : %d", w.Code)
	}
	g.ok(g.do("POST", "/admin/acl", &root, map[string]any{"user_id": bob.id, "home_id": mainHomeID, "grant": []string{"fs_read", "fs_write"}, "confirm": true, "reauth": root.reauth()}))
}
