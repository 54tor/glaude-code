package main

import (
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
	if err := g.s.Grant(u.id, id, rights...); err != nil {
		t.Fatal(err)
	}
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
	if w := g.do("PUT", "/memory-save", &a, "", ""); w.Code != 404 || reached(w) {
		t.Errorf("méthode inconnue : %d", w.Code)
	}
}

func TestGatePathVariants(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "lecteur", false)
	g.grant(t, u, "", RightMemRead)
	if w := g.do("POST", "/memory-save", &u, "", "{}"); w.Code != 403 {
		t.Fatalf("témoin : %d", w.Code)
	}
	for _, p := range []string{"//memory-save", "/MEMORY-SAVE", "/memory-save/", "/./memory-save", "/x/../memory-save",
		"/memory%2Dsave", "/memory%2dsave", "/%6Demory-save", "/memory-save%2F", "/memory-save?x=1", "/memory-save%00"} {
		w := g.do("POST", p, &u, "", "{}") // cible lue comme une ligne de requête : « //x » reste un chemin
		if reached(w) || w.Code < 400 {
			t.Errorf("%s : %d %q obtenu sans mem_write", p, w.Code, w.Body)
		}
	}
	if w := g.do("GET", "/memory-list", &u, "", ""); !reached(w) {
		t.Fatalf("lecture refusée : %d", w.Code)
	}
}

func TestGateHomeCookieNoRights(t *testing.T) {
	g := newGateRig(t)
	addHomes(t, "Pro")
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightMemRead)
	for _, home := range []string{"Pro", "inconnu"} {
		if w := g.do("GET", "/memory-list", &u, home, ""); w.Code != 403 || reached(w) {
			t.Errorf("cookie %q : %d %s", home, w.Code, w.Body)
		}
	}
	if w := g.do("POST", "/homes-select", &u, "", `{"name":"Pro"}`); w.Code != 403 {
		t.Errorf("choix d'un home sans droit : %d", w.Code)
	}
	if w := g.do("POST", "/homes-select", &u, "", `{"name":""}`); !reached(w) {
		t.Errorf("choix du home principal : %d", w.Code)
	}

	// Droit vérifié pour Pro, enfant en panne : jamais servi par le home principal.
	g.grant(t, u, "Pro", RightMemRead)
	old := homeStart
	t.Cleanup(func() { homeStart = old })
	homeStart = func(Home) (*homeProc, error) { return nil, errors.New("panne") }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /memory-list", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok principal")) })
	h := authGate(mux, g.s, withHomes(mux))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, g.req("GET", "/memory-list", &u, "Pro", ""))
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
	for _, p := range []string{"/homes-sync-plan", "/homes-sync-apply"} {
		if w := g.do("POST", p, &u, "", body); w.Code != 403 {
			t.Errorf("%s sans mem_sync sur la destination : %d", p, w.Code)
		}
	}
	g.grant(t, u, "Pro", RightMemSync, RightMemRead)
	if w := g.do("POST", "/homes-sync-plan", &u, "", body); !reached(w) {
		t.Fatalf("synchro permise refusée : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/homes-sync-plan", &u, "", `{"from":"Pro","to":""}`); w.Code != 403 {
		t.Errorf("sens inverse sans mem_read sur la source ni mem_write sur la destination : %d", w.Code)
	}
	if err := g.s.Revoke(u.id, mainHomeID, RightMemRead); err != nil {
		t.Fatal(err)
	}
	if w := g.do("POST", "/homes-sync-apply", &u, "", body); w.Code != 403 {
		t.Errorf("source sans mem_read : %d", w.Code)
	}
	if w := g.do("POST", "/homes-sync-plan", &u, "", `{"from":"","to":"absent"}`); w.Code != 403 {
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
		{&r, "GET", "/memory-list", true, 200},
		{&r, "POST", "/memory-save", false, 403},
		{&r, "POST", "/chat", false, 403},
		{&r, "POST", "/homes-save", false, 403},
		{&u, "GET", "/account", true, 200},
		{&u, "GET", "/conv", false, 403},
		{&u, "GET", "/info", false, 403},
		{&a, "GET", "/memory-list", false, 403}, // pas de super-droit implicite
		{&a, "POST", "/homes-save", true, 200},
		{nil, "GET", "/memory-list", false, 401},
		{nil, "POST", "/homes-save", false, 401},
		{nil, "GET", "/", true, 200},
		{nil, "GET", "/assets/x.js", true, 200},
		{nil, "POST", "/auth/login", true, 200},
		{&r, "GET", "/homes", true, 200},
		{&r, "GET", "/search", false, 403},
		{&wr, "POST", "/tabs-save", true, 200},
		{&wr, "POST", "/rm", false, 403},
		{&wr, "POST", "/rename", false, 403},
		{&wr, "POST", "/reassign", false, 403},
		{&wr, "POST", "/keepwarm", false, 403},
		{&wr, "POST", "/session-trash-purge", false, 403},
		{&wr, "POST", "/snippet-save", false, 403},
		{&wr, "POST", "/snippet-rm", false, 403},
		{&wr, "POST", "/snippets-import", false, 403},
		{&wr, "POST", "/snippet-draft", true, 200},
		{&r, "GET", "/snippet-list", true, 200},
		{&r, "GET", "/config-perm-suggest", false, 403},
		{&cw, "POST", "/snippet-save", true, 200},
		{&sr, "GET", "/config-perm-suggest", true, 200},
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
	if w := g.do("GET", "/memory-list", &gateUser{id, c, p.CSRF}, "", ""); w.Code != 401 {
		t.Errorf("session partielle : %d", w.Code)
	}
	// Révocation immédiate : aucun cache.
	g.s.Revoke(r.id, mainHomeID, RightMemRead)
	if w := g.do("GET", "/memory-list", &r, "", ""); w.Code != 403 {
		t.Errorf("droit révoqué encore accepté : %d", w.Code)
	}
}

func TestGateCSRF(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightMemWrite)
	for name, tok := range map[string]string{"absent": "", "faux": strings.Repeat("0", 64)} {
		r := g.req("POST", "/memory-save", &u, "", "{}")
		r.Header.Set(csrfHeader, tok)
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		if w.Code != 403 || reached(w) {
			t.Errorf("CSRF %s : %d", name, w.Code)
		}
	}
	if w := g.do("POST", "/memory-save", &u, "", "{}"); !reached(w) {
		t.Fatalf("CSRF valide refusé : %d", w.Code)
	}

	bearer := func(m, p string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(m, p, strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	if w := bearer("POST", "/memory-save"); w.Code != 401 {
		t.Fatalf("Bearer sans jeton branché : %d", w.Code)
	}
	old := tokenPrincipal
	t.Cleanup(func() { tokenPrincipal = old })
	tokenPrincipal = func(*http.Request) *Principal { return &Principal{Token: true} }
	if w := bearer("POST", "/memory-save"); !reached(w) {
		t.Errorf("Bearer : CSRF exigé à tort : %d", w.Code)
	}
	if w := bearer("POST", "/homes-save"); w.Code != 403 {
		t.Errorf("Bearer sur une route admin : %d", w.Code)
	}
}

func TestGateTamperedRights(t *testing.T) {
	g := newGateRig(t)
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightMemRead)
	g.s.db.Exec(`UPDATE home_acl SET exec=1 WHERE user_id=?`, u.id)
	w := g.do("GET", "/memory-list", &u, "", "")
	if w.Code != 403 || reached(w) || !strings.Contains(w.Body.String(), L("srv.auth.tamper")) {
		t.Fatalf("ligne altérée : %d %s", w.Code, w.Body)
	}
	if w := g.do("POST", "/chat", &u, "", "{}"); w.Code != 403 {
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
	if w := g.do("GET", "/sessions-stamp", &u, "", ""); !reached(w) || seen() != before {
		t.Fatalf("sondage compté comme activité : %d", w.Code)
	}
	for _, p := range []string{"/api", "/conv", "/session-screen"} {
		if w := g.do("GET", p, &u, "", ""); !reached(w) || seen() != before {
			t.Fatalf("%s compté comme activité : %d", p, w.Code)
		}
	}
	if w := g.do("GET", "/info", &u, "", ""); !reached(w) || seen() == before {
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
		h.ServeHTTP(w, g.req("GET", "/homes", &u, "", ""))
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
		return g.do("POST", "/homes-sync-apply", &u, "", string(b)).Code
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
	g.s.Revoke(u.id, mustHomeID(t, "Pro"), RightMemRead)
	if w := g.do("POST", "/homes-sync-plan", &u, "", `{"from":"","to":"Pro"}`); w.Code != 403 {
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
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/homes-save", strings.NewReader(`{"homes":[{"name":"main","dir":"`+t.TempDir()+`"}]}`)))
	if !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("/homes-save : %s", w.Body)
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
		if w := g.do("POST", "/trash-restore", c.u, "", `{"ID":"`+ids[c.key]+`"}`); w.Code != c.code {
			t.Errorf("%s : %d", c.key, w.Code)
		}
	}
	if w := g.do("POST", "/trash-restore", &mw, "", `{"ID":"../x"}`); w.Code != 403 {
		t.Errorf("identifiant hors corbeille : %d", w.Code)
	}
}
