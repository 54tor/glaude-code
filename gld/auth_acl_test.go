package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type gateRig struct {
	s   *authStore
	h   http.Handler
	cfg string
}

// newGateRig : chaque motif de la table reçoit un handler témoin qui répond « ok <motif> ».
func newGateRig(t *testing.T, extra ...string) *gateRig {
	t.Helper()
	s := testStore(t)
	cfg := t.TempDir()
	use(cfg)
	mux := http.NewServeMux()
	for pat := range routeRights {
		mux.HandleFunc(pat, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok " + pat)) })
	}
	for _, pat := range extra {
		mux.HandleFunc(pat, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok " + pat)) })
	}
	return &gateRig{s: s, h: authGate(mux, s, mux), cfg: cfg}
}

type gateUser struct {
	id           int64
	cookie, csrf string
}

func (g *gateRig) user(t *testing.T, name string, admin bool) gateUser {
	t.Helper()
	id, err := g.s.AddUser(name, testPw, admin)
	if err != nil {
		t.Fatal(err)
	}
	c, err := g.s.NewSession(id, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	p, err := g.s.Session(c)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err = g.s.PromoteMFA(p.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if p, err = g.s.Session(c); err != nil {
		t.Fatal(err)
	}
	return gateUser{id, c, p.CSRF}
}

func (g *gateRig) grant(t *testing.T, u gateUser, home string, rights ...Right) {
	t.Helper()
	id, ok := homeIDFor(home)
	if !ok {
		t.Fatalf("home %q inconnu", home)
	}
	if err := g.rawGrant(u.id, id, rights...); err != nil {
		t.Fatal(err)
	}
}

// rawGrant écrit la ligne d'ACL sans l'invariant « écrire exige lire » de SetRights : la porte doit aussi tenir devant une
// ligne héritée qui le viole.
func (g *gateRig) rawGrant(userID int64, homeID string, rights ...Right) error {
	return g.s.inTx(func(tx *sql.Tx) error {
		m, err := g.s.rights(tx, userID, homeID)
		if err != nil {
			return err
		}
		for _, r := range rights {
			m[r] = true
		}
		args := []any{userID, homeID}
		for _, r := range allRights {
			args = append(args, m[r])
		}
		args = append(args, g.s.aclSig(userID, homeID, m))
		_, err = tx.Exec(`INSERT OR REPLACE INTO home_acl(user_id,home_id,`+rightCols()+`,sig) VALUES(?,?`+strings.Repeat(",?", len(allRights)+1)+`)`, args...)
		return err
	})
}

func (g *gateRig) req(method, target string, u *gateUser, home, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if u != nil {
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: u.cookie})
		r.Header.Set(csrfHeader, u.csrf)
	}
	if home != "" {
		r.AddCookie(&http.Cookie{Name: homeCookie, Value: url.QueryEscape(home)})
	}
	return r
}

func (g *gateRig) do(method, target string, u *gateUser, home, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, g.req(method, target, u, home, body))
	return w
}

func reached(w *httptest.ResponseRecorder) bool { return strings.HasPrefix(w.Body.String(), "ok ") }

func addHomes(t *testing.T, names ...string) {
	t.Helper()
	var hs []Home
	for _, n := range names {
		hs = append(hs, Home{Name: n, Dir: t.TempDir()})
	}
	if err := saveHomes(hs); err != nil {
		t.Fatal(err)
	}
}

func TestEveryRouteClassified(t *testing.T) {
	re := regexp.MustCompile(`"((?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS) /[^"\s]*)"`)
	files, _ := filepath.Glob("*.go")
	inCode := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "auth_acl.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			inCode[m[1]] = true
			if _, ok := routeRights[m[1]]; !ok {
				t.Errorf("%s : route %q absente de routeRights", f, m[1])
			}
		}
	}
	if len(inCode) < 100 {
		t.Fatalf("analyse suspecte : %d routes trouvées", len(inCode))
	}
	for pat := range routeRights {
		if !inCode[pat] {
			t.Errorf("routeRights : %q n'existe pas dans le code", pat)
		}
	}
}

func TestGateDeniesByDefault(t *testing.T) {
	g := newGateRig(t, "POST /fictive", "GET /fictive-lecture")
	a := g.user(t, "admin", true)
	g.grant(t, a, "", allRights...)
	for _, m := range [][2]string{{"POST", "/fictive"}, {"GET", "/fictive-lecture"}} {
		if w := g.do(m[0], m[1], &a, "", "{}"); w.Code != 403 || reached(w) {
			t.Errorf("%s %s non classée : %d %s", m[0], m[1], w.Code, w.Body)
		}
	}
	if w := g.do("GET", "/absente", &a, "", ""); w.Code != 404 || reached(w) {
		t.Errorf("route inconnue : %d", w.Code)
	}
	if w := g.do("DELETE", "/v1/memory/files/x", &a, "", ""); w.Code != 404 || reached(w) {
		t.Errorf("méthode inconnue : %d", w.Code)
	}
}

func TestGatePathVariants(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "lecteur", false)
	g.grant(t, u, "", RightMemRead)
	if w := g.do("PUT", "/v1/memory/files/x", &u, "", "{}"); w.Code != 403 {
		t.Fatalf("témoin : %d", w.Code)
	}
	for _, p := range []string{"//v1/memory/files/x", "/V1/MEMORY/FILES/x", "/v1/memory/files/x/", "/v1/./memory/files/x", "/v1/y/../memory/files/x",
		"/v1/memory/files%2Fx", "/v1/%6Demory/files/x", "/v1/memory/files/x%00", "/v1/memory/files/x/y", "/v1/memory/files/x/../y"} {
		w := g.do("PUT", p, &u, "", "{}") // cible lue comme une ligne de requête : « //x » reste un chemin
		if reached(w) || w.Code < 400 {
			t.Errorf("%s : %d %q obtenu sans mem_write", p, w.Code, w.Body)
		}
	}
	if w := g.do("GET", "/v1/memory/files", &u, "", ""); !reached(w) {
		t.Fatalf("lecture refusée : %d", w.Code)
	}
}

func TestGateHomeCookieNoRights(t *testing.T) {
	g := newGateRig(t)
	addHomes(t, "Pro")
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightMemRead)
	for _, home := range []string{"Pro", "inconnu"} {
		if w := g.do("GET", "/v1/memory/files", &u, home, ""); w.Code != 403 || reached(w) {
			t.Errorf("cookie %q : %d %s", home, w.Code, w.Body)
		}
	}
	if w := g.do("PUT", "/v1/homes/current", &u, "", `{"name":"Pro"}`); w.Code != 403 {
		t.Errorf("choix d'un home sans droit : %d", w.Code)
	}
	if w := g.do("PUT", "/v1/homes/current", &u, "", `{"name":""}`); !reached(w) {
		t.Errorf("choix du home principal : %d", w.Code)
	}

	// Droit vérifié pour Pro, enfant en panne : jamais servi par le home principal.
	g.grant(t, u, "Pro", RightMemRead)
	old := homeStart
	t.Cleanup(func() { homeStart = old })
	homeStart = func(Home) (*homeProc, error) { return nil, errors.New("panne") }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/memory/files", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok principal")) })
	h := authGate(mux, g.s, withHomes(mux))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, g.req("GET", "/v1/memory/files", &u, "Pro", ""))
	if w.Code != 503 || reached(w) {
		t.Fatalf("repli sur le home principal : %d %s", w.Code, w.Body)
	}
}

func TestGateMemSyncNeedsBoth(t *testing.T) {
	g := newGateRig(t)
	addHomes(t, "Pro")
	u := g.user(t, "bob", false)
	g.grant(t, u, "", RightMemSync, RightMemRead)
	g.grant(t, u, "Pro", RightMemWrite)
	body := `{"from":"","to":"Pro"}`
	for _, p := range []string{"/v1/homes/sync-plans", "/v1/homes/syncs"} {
		if w := g.do("POST", p, &u, "", body); w.Code != 403 {
			t.Errorf("%s sans mem_sync sur la destination : %d", p, w.Code)
		}
	}
	g.grant(t, u, "Pro", RightMemSync, RightMemRead)
	if w := g.do("POST", "/v1/homes/sync-plans", &u, "", body); !reached(w) {
		t.Fatalf("synchro permise refusée : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/v1/homes/sync-plans", &u, "", `{"from":"Pro","to":""}`); w.Code != 403 {
		t.Errorf("sens inverse sans mem_read sur la source ni mem_write sur la destination : %d", w.Code)
	}
	if err := g.s.Revoke(u.id, mainHomeID, RightMemRead); err != nil {
		t.Fatal(err)
	}
	if w := g.do("POST", "/v1/homes/syncs", &u, "", body); w.Code != 403 {
		t.Errorf("source sans mem_read : %d", w.Code)
	}
	if w := g.do("POST", "/v1/homes/sync-plans", &u, "", `{"from":"","to":"absent"}`); w.Code != 403 {
		t.Errorf("home inconnu : %d", w.Code)
	}
}

func TestGateRightsMatrix(t *testing.T) {
	g := newGateRig(t)
	r := g.user(t, "lecteur", false)
	g.grant(t, r, "", RightMemRead)
	u := g.user(t, "compta", false)
	g.grant(t, u, "", RightUsageRead)
	a := g.user(t, "admin", true)
	wr := g.user(t, "redacteur", false)
	g.grant(t, wr, "", RightMemWrite)
	cw := g.user(t, "reglages", false)
	g.grant(t, cw, "", RightConfigWrite)
	sr := g.user(t, "historien", false)
	g.grant(t, sr, "", RightSessionsRead)
	cases := []struct {
		u      *gateUser
		m, p   string
		reach  bool
		status int
	}{
		{&r, "GET", "/v1/memory/files", true, 200},
		{&r, "PUT", "/v1/memory/files/x", false, 403},
		{&r, "POST", "/v1/sessions/x/chat", false, 403},
		{&r, "PUT", "/v1/homes", false, 403},
		{&u, "GET", "/v1/account", true, 200},
		{&u, "GET", "/v1/sessions/x/conversation", false, 403},
		{&u, "GET", "/v1/info", false, 403},
		{&a, "GET", "/v1/memory/files", false, 403}, // pas de super-droit implicite
		{&a, "PUT", "/v1/homes", true, 200},
		{nil, "GET", "/v1/memory/files", false, 401},
		{nil, "PUT", "/v1/homes", false, 401},
		{nil, "GET", "/", true, 200},
		{nil, "GET", "/assets/x.js", true, 200},
		{nil, "POST", "/v1/auth/sessions", true, 200},
		{&r, "GET", "/v1/homes", true, 200},
		{&r, "GET", "/v1/sessions", false, 403},
		{&wr, "PUT", "/v1/tabs", true, 200},
		{&wr, "DELETE", "/v1/sessions", false, 403},
		{&wr, "PATCH", "/v1/sessions/x", false, 403},
		{&wr, "PATCH", "/v1/sessions", false, 403},
		{&wr, "PUT", "/v1/keepwarm", false, 403},
		{&wr, "DELETE", "/v1/trash/sessions", false, 403},
		{&wr, "PUT", "/v1/snippets/u%2Fa.md", false, 403},
		{&wr, "DELETE", "/v1/snippets", false, 403},
		{&wr, "PATCH", "/v1/snippets/u%2Fa.md", false, 403},
		{&wr, "POST", "/v1/snippets/imports", false, 403},
		{&wr, "POST", "/v1/snippets/drafts", true, 200},
		{&r, "GET", "/v1/snippets", true, 200},
		{&r, "GET", "/v1/config/permissions/suggestions", false, 403},
		{&cw, "PUT", "/v1/snippets/u%2Fa.md", true, 200},
		{&sr, "GET", "/v1/config/permissions/suggestions", true, 200},
	}
	for _, c := range cases {
		w := g.do(c.m, c.p, c.u, "", "{}")
		if reached(w) != c.reach || w.Code != c.status {
			t.Errorf("%s %s : %d %s", c.m, c.p, w.Code, w.Body)
		}
	}

	// Session sans second facteur : refusée hors /auth/*.
	id, _ := g.s.AddUser("partiel", testPw, false)
	g.s.Grant(id, mainHomeID, RightMemRead)
	c, _ := g.s.NewSession(id, "127.0.0.1", "test")
	p, _ := g.s.Session(c)
	if w := g.do("GET", "/v1/memory/files", &gateUser{id, c, p.CSRF}, "", ""); w.Code != 401 {
		t.Errorf("session partielle : %d", w.Code)
	}
	// Révocation immédiate : aucun cache.
	g.s.Revoke(r.id, mainHomeID, RightMemRead)
	if w := g.do("GET", "/v1/memory/files", &r, "", ""); w.Code != 403 {
		t.Errorf("droit révoqué encore accepté : %d", w.Code)
	}
}

func TestGateCSRF(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightMemWrite)
	for name, tok := range map[string]string{"absent": "", "faux": strings.Repeat("0", 64)} {
		r := g.req("PUT", "/v1/memory/files/x", &u, "", "{}")
		r.Header.Set(csrfHeader, tok)
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		if w.Code != 403 || reached(w) {
			t.Errorf("CSRF %s : %d", name, w.Code)
		}
	}
	if w := g.do("PUT", "/v1/memory/files/x", &u, "", "{}"); !reached(w) {
		t.Fatalf("CSRF valide refusé : %d", w.Code)
	}

	bearer := func(m, p string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(m, p, strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	if w := bearer("PUT", "/v1/memory/files/x"); w.Code != 401 {
		t.Fatalf("Bearer sans jeton branché : %d", w.Code)
	}
	old := tokenPrincipal
	t.Cleanup(func() { tokenPrincipal = old })
	tokenPrincipal = func(*http.Request) *Principal { return &Principal{Token: true} }
	if w := bearer("PUT", "/v1/memory/files/x"); !reached(w) {
		t.Errorf("Bearer : CSRF exigé à tort : %d", w.Code)
	}
	if w := bearer("PUT", "/v1/homes"); w.Code != 403 {
		t.Errorf("Bearer sur une route admin : %d", w.Code)
	}
}

func TestGateTamperedRights(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightMemRead)
	g.s.db.Exec(`UPDATE home_acl SET exec=1 WHERE user_id=?`, u.id)
	w := g.do("GET", "/v1/memory/files", &u, "", "")
	if w.Code != 403 || reached(w) || !strings.Contains(w.Body.String(), L("srv.auth.tamper")) {
		t.Fatalf("ligne altérée : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/v1/sessions/x/chat", &u, "", "{}"); w.Code != 403 {
		t.Fatalf("droit ajouté par SQL accepté : %d", w.Code)
	}
}

func TestGatePassiveNoTouch(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightSessionsRead)
	seen := func() (n int64) {
		g.s.db.QueryRow(`SELECT expires_at FROM sessions WHERE user_id=?`, u.id).Scan(&n)
		return
	}
	before := seen()
	old := authNow
	t.Cleanup(func() { authNow = old })
	authNow = func() time.Time { return old().Add(5 * time.Minute) }
	if w := g.do("GET", "/v1/state/stamp", &u, "", ""); !reached(w) || seen() != before {
		t.Fatalf("sondage compté comme activité : %d", w.Code)
	}
	for _, p := range []string{"/v1/state", "/v1/sessions/x/conversation", "/v1/sessions/x/screen"} {
		if w := g.do("GET", p, &u, "", ""); !reached(w) || seen() != before {
			t.Fatalf("%s compté comme activité : %d", p, w.Code)
		}
	}
	if w := g.do("GET", "/v1/info", &u, "", ""); !reached(w) || seen() == before {
		t.Fatalf("activité non comptée : %d", w.Code)
	}
}

func TestHomeIDStable(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	a, b := t.TempDir(), t.TempDir()
	old, _ := json.Marshal(map[string]any{"homes": []map[string]string{{"name": "A", "dir": a}, {"name": "main", "dir": b}}})
	if err := os.WriteFile(homesFile(), old, 0o600); err != nil {
		t.Fatal(err)
	}
	hs := loadHomes()
	if len(hs) != 2 || len(hs[0].ID) != 16 || len(hs[1].ID) != 16 || hs[0].ID == hs[1].ID {
		t.Fatalf("identifiants non attribués : %+v", hs)
	}
	if again := loadHomes(); again[0].ID != hs[0].ID || again[1].ID != hs[1].ID {
		t.Fatal("identifiants non persistés")
	}
	if id, _ := homeIDFor(""); id != mainHomeID {
		t.Fatal("home principal")
	}
	if id, ok := homeIDFor("main"); !ok || id != hs[1].ID {
		t.Fatalf("un home nommé main pris pour le principal : %s", id)
	}
	if _, ok := homeIDFor("absent"); ok {
		t.Fatal("home inconnu résolu")
	}
	// Sans identifiant fourni (interface actuelle), le nom garde le sien ; un identifiant inconnu est remplacé.
	if err := saveHomes([]Home{{Name: "A", Dir: a}, {Name: "Renommé", Dir: b, ID: hs[1].ID}, {Name: "C", Dir: b, ID: "deadbeefdeadbeef"}}); err != nil {
		t.Fatal(err)
	}
	got := loadHomes()
	if got[0].ID != hs[0].ID || got[1].ID != hs[1].ID || got[2].ID == "deadbeefdeadbeef" || got[2].ID == "" {
		t.Fatalf("%+v", got)
	}
	// Supprimé puis recréé sous le même nom : nouvel identifiant, les anciens droits ne reviennent pas.
	if err := saveHomes([]Home{}); err != nil {
		t.Fatal(err)
	}
	if err := saveHomes([]Home{{Name: "A", Dir: a, ID: hs[0].ID}}); err != nil {
		t.Fatal(err)
	}
	if loadHomes()[0].ID == hs[0].ID {
		t.Fatal("identifiant réattribué")
	}
}

func TestHomesListFiltered(t *testing.T) {
	s := testStore(t)
	use(t.TempDir())
	addHomes(t, "A", "B")
	mux := http.NewServeMux()
	routesHomes(mux)
	h := authGate(mux, s, mux)
	g := &gateRig{s: s, h: h}
	u := g.user(t, "alice", false)
	g.grant(t, u, "A", RightMemRead)
	var out struct {
		Main, MainID string
		Homes        []Home
	}
	list := func(h http.Handler) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, g.req("GET", "/v1/homes", &u, "", ""))
		out.Homes = nil
		var raw map[string]json.RawMessage
		json.Unmarshal(w.Body.Bytes(), &raw)
		json.Unmarshal(raw["homes"], &out.Homes)
		json.Unmarshal(raw["main"], &out.Main)
		json.Unmarshal(raw["mainId"], &out.MainID)
	}
	list(h)
	if len(out.Homes) != 1 || out.Homes[0].Name != "A" || out.Homes[0].ID == "" || out.Main != "" || out.MainID != mainHomeID {
		t.Fatalf("liste filtrée : %+v", out)
	}
	list(mux)
	if len(out.Homes) != 2 || out.Main == "" {
		t.Fatalf("sans garde, tout est listé : %+v", out)
	}
}

func TestGateSyncApplyConfigItems(t *testing.T) {
	g := newGateRig(t)
	addHomes(t, "Pro")
	u := g.user(t, "bob", false)
	g.grant(t, u, "", RightMemSync, RightMemRead)
	g.grant(t, u, "Pro", RightMemSync, RightMemWrite, RightMemRead)
	apply := func(ids ...string) int {
		var picks []map[string]string
		for _, id := range ids {
			picks = append(picks, map[string]string{"id": id, "mode": "replace"})
		}
		b, _ := json.Marshal(map[string]any{"from": "", "to": "Pro", "picks": picks})
		return g.do("POST", "/v1/homes/syncs", &u, "", string(b)).Code
	}
	for _, ok := range [][]string{{"file:CLAUDE.md"}, {"file:rules/a.md", "file:rules/sous/b.md"}, nil} {
		if c := apply(ok...); c != 200 {
			t.Errorf("%v sans config_write : %d", ok, c)
		}
	}
	for _, bad := range []string{"set:hooks", "perm:allow", "skill:x", "file:commands/x.md", "file:rules/../commands/x.md",
		"file:rules/a.sh", "file:rules\\a.md", "inconnu", ""} {
		if c := apply("file:CLAUDE.md", bad); c != 403 {
			t.Errorf("%q sans config_write : %d", bad, c)
		}
	}
	g.grant(t, u, "Pro", RightConfigWrite)
	if c := apply("set:hooks"); c != 200 {
		t.Errorf("set:hooks avec config_write : %d", c)
	}
	// Le plan exige mem_read sur la destination (il en renvoie des aperçus).
	g.s.SetRights(u.id, mustHomeID(t, "Pro"), nil, []Right{RightMemRead, RightMemWrite, RightConfigWrite})
	if w := g.do("POST", "/v1/homes/sync-plans", &u, "", `{"from":"","to":"Pro"}`); w.Code != 403 {
		t.Errorf("plan sans mem_read sur la destination : %d", w.Code)
	}
}

func mustHomeID(t *testing.T, name string) string {
	t.Helper()
	id, ok := homeIDFor(name)
	if !ok {
		t.Fatalf("home %q inconnu", name)
	}
	return id
}

func TestHomeNameMainReserved(t *testing.T) {
	use(t.TempDir())
	for _, n := range []string{"main", "Main", " MAIN "} {
		if err := saveHomes([]Home{{Name: n, Dir: t.TempDir()}}); err == nil || !isMsg(err, "srv.homes.nomReserve") {
			t.Errorf("%q accepté : %v", n, err)
		}
	}
	mux := http.NewServeMux()
	routesHomes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PUT", "/v1/homes", strings.NewReader(`{"homes":[{"name":"main","dir":"`+t.TempDir()+`"}]}`)))
	if w.Code != 400 {
		t.Fatalf("PUT /v1/homes : %d %s", w.Code, w.Body)
	}
}

func TestUserCLIGrantHomeName(t *testing.T) {
	s := testStore(t)
	use(t.TempDir())
	addHomes(t, "Pro")
	id, _ := s.AddUser("alice", testPw, false)
	s.db.Close()
	pro := mustHomeID(t, "Pro")
	for _, args := range [][]string{{"main", "mem_read"}, {"Pro", "mem_read"}, {pro, "mem_write"}, {"0123456789abcdef", "mem_read"}} {
		if _, err := run(t, cliUser, append([]string{"grant", "alice"}, args...)...); err != nil {
			t.Fatalf("%v : %v", args, err)
		}
	}
	if _, err := run(t, cliUser, "grant", "alice", "Absent", "mem_read"); err == nil || !isMsg(err, "srv.user.homeInconnu") {
		t.Fatalf("home inconnu : %v", err)
	}
	s2, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.db.Close()
	m, _ := s2.Rights(id, mainHomeID)
	p, _ := s2.Rights(id, pro)
	if !m[RightMemRead] || !p[RightMemRead] || !p[RightMemWrite] {
		t.Fatalf("main %v, Pro %v", m, p)
	}
	if n, _ := s2.Rights(id, "Pro"); anyRight(n) {
		t.Fatal("droit écrit sous le nom au lieu de l'identifiant")
	}
}

// Restaurer un snippet ou un skill depuis la corbeille recrée du contenu exécutable : config_write exigé.
func TestGateTrashRestoreNeedsConfig(t *testing.T) {
	g := newGateRig(t)
	ids := map[string]string{}
	for _, k := range []string{"s/u/x.md", "k/outil/SKILL.md", "m/projet/note.md", "z/inconnu"} {
		if err := saveTrash(k, "/p", "supprimé", "texte"); err != nil {
			t.Fatal(err)
		}
		for _, it := range trashList() {
			if it.Key == k {
				ids[k] = it.ID
			}
		}
	}
	mw := g.user(t, "memoire", false)
	g.grant(t, mw, "", RightMemWrite)
	cw := g.user(t, "reglages", false)
	g.grant(t, cw, "", RightMemWrite, RightConfigWrite)
	for _, c := range []struct {
		u    *gateUser
		key  string
		code int
	}{
		{&mw, "s/u/x.md", 403}, {&mw, "k/outil/SKILL.md", 403}, {&mw, "z/inconnu", 403}, {&mw, "m/projet/note.md", 200},
		{&cw, "s/u/x.md", 200}, {&cw, "k/outil/SKILL.md", 200},
	} {
		if w := g.do("PATCH", "/v1/trash/memory/"+ids[c.key], c.u, "", `{"trashed":false}`); w.Code != c.code {
			t.Errorf("%s : %d", c.key, w.Code)
		}
	}
	if w := g.do("PATCH", "/v1/trash/memory/..%2Fx", &mw, "", `{"trashed":false}`); w.Code != 403 {
		t.Errorf("identifiant hors corbeille : %d", w.Code)
	}
}

// POST /v1/memory/merges avec launch ouvre une session claude aux arguments de l'appelant : exec exigé. Sans launch, mem_write suffit.
func TestGateMemoryMergeLaunchNeedsExec(t *testing.T) {
	g := newGateRig(t)
	mw := g.user(t, "memoire", false)
	g.grant(t, mw, "", RightMemWrite)
	ex := g.user(t, "lanceur", false)
	g.grant(t, ex, "", RightMemWrite, RightExec)
	for _, c := range []struct {
		u    *gateUser
		body string
		code int
	}{
		{&mw, `{"Launch":true,"Args":"--dangerously-skip-permissions"}`, 403},
		{&mw, `{"Launch":false}`, 200},
		{&ex, `{"Launch":true}`, 200},
	} {
		if w := g.do("POST", "/v1/memory/merges", c.u, "", c.body); w.Code != c.code {
			t.Errorf("%s : %d", c.body, w.Code)
		}
	}
}

// PUT /v1/vgroups remplace aussi les dossiers enregistrés, racines de l'IDE : en ajouter demande exec, comme POST /dirs.
func TestGateVgroupsNewDirNeedsExec(t *testing.T) {
	g := newGateRig(t)
	known := t.TempDir()
	if err := saveStore(Store{Groups: []VGroup{}, Favs: []string{}, Projects: []Project{}, Dirs: []string{known}}); err != nil {
		t.Fatal(err)
	}
	mw := g.user(t, "memoire", false)
	g.grant(t, mw, "", RightMemWrite)
	ex := g.user(t, "lanceur", false)
	g.grant(t, ex, "", RightMemWrite, RightExec)
	keep := `{"groups":[],"favs":[],"dirs":["` + known + `"]}`
	add := `{"groups":[],"favs":[],"dirs":["` + known + `","/etc"]}`
	addProj := `{"groups":[],"favs":[],"projects":[{"name":"p","dirs":["/etc"]}]}`
	for _, c := range []struct {
		u    *gateUser
		body string
		code int
	}{
		{&mw, keep, 200}, {&mw, add, 403}, {&mw, addProj, 403}, {&ex, add, 200},
	} {
		if w := g.do("PUT", "/v1/vgroups", c.u, "", c.body); w.Code != c.code {
			t.Errorf("%s : %d", c.body, w.Code)
		}
	}
}

// fs_write écrit les fichiers du projet, mais ceux que Claude Code ou git exécutent (.claude/, .git/, .mcp.json…) exigent config_write.
func TestFsWriteCodePathsNeedConfigWrite(t *testing.T) {
	s := testStore(t)
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	if err := saveStore(Store{Groups: []VGroup{}, Favs: []string{}, Projects: []Project{}, Dirs: []string{proj}}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	mux := http.NewServeMux()
	routesIDEWrite(mux)
	g := &gateRig{s: s, h: authGate(mux, s, mux), cfg: cfg}
	fw := g.user(t, "ecrit", false)
	g.grant(t, fw, "", RightFsRead, RightFsWrite)
	cw := g.user(t, "reglages", false)
	g.grant(t, cw, "", RightFsRead, RightFsWrite, RightConfigWrite)
	save := func(p string) string {
		return "/v1/fs/entries/content?dir=" + url.QueryEscape(proj) + "&path=" + url.QueryEscape(p)
	}
	const saveBody = `{"content":"x","force":true}`
	for _, c := range []struct {
		u    *gateUser
		path string
		code int
	}{
		{&fw, "a.txt", 200}, {&fw, ".claude/settings.json", 403}, {&fw, ".mcp.json", 403}, {&fw, "sub/.git/config", 403},
		{&cw, ".mcp.json", 200}, {&cw, ".claude/settings.json", 200},
	} {
		if w := g.do("PUT", save(c.path), c.u, "", saveBody); w.Code != c.code {
			t.Errorf("%s : %d %s", c.path, w.Code, w.Body.String())
		}
	}
	rn := `{"from":"a.txt","to":".git/config"}`
	if w := g.do("PATCH", "/v1/fs/entries?dir="+url.QueryEscape(proj), &fw, "", rn); w.Code != 403 {
		t.Errorf("renommage vers .git : %d", w.Code)
	}
}

// Les brouillons de mémoire et de skill lisent des fichiers côté serveur : mem_read est exigé en plus du droit d'écriture.
func TestGateDraftsNeedMemRead(t *testing.T) {
	g := newGateRig(t)
	w := g.user(t, "ecrivain", false)
	g.grant(t, w, "", RightMemWrite, RightConfigWrite)
	r := g.user(t, "lecteur", false)
	g.grant(t, r, "", RightMemWrite, RightConfigWrite, RightMemRead)
	for _, c := range []struct {
		u    *gateUser
		path string
		code int
	}{
		{&w, "/v1/memory/drafts", 403}, {&w, "/v1/skills/drafts", 403},
		{&r, "/v1/memory/drafts", 200}, {&r, "/v1/skills/drafts", 200},
	} {
		if got := g.do("POST", c.path, c.u, "", `{}`).Code; got != c.code {
			t.Errorf("%s : %d, attendu %d", c.path, got, c.code)
		}
	}
}

func TestSetRightsWriteNeedsRead(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "ecrivain", false)
	id, _ := homeIDFor("")
	for _, w := range []Right{RightMemWrite, RightConfigWrite} {
		if err := g.s.Grant(u.id, id, w); err == nil {
			t.Fatalf("%s sans mem_read accordé", w)
		}
		if err := g.s.SetRights(u.id, id, []Right{w, RightMemRead}, nil); err != nil {
			t.Fatalf("%s avec mem_read : %v", w, err)
		}
		if err := g.s.Revoke(u.id, id, RightMemRead); err == nil {
			t.Fatalf("mem_read retiré alors que %s reste", w)
		}
		if err := g.s.SetRights(u.id, id, nil, []Right{w, RightMemRead}); err != nil {
			t.Fatalf("retrait groupé : %v", err)
		}
	}
}
