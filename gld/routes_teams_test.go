package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// teamsRig : les vraies routes d'équipes derrière le garde, sur le cfgRoot des fixtures de teams_run_test.go.
func teamsRig(t *testing.T) (*gateRig, *fakeIO) {
	t.Helper()
	cfg, f := setup(t)
	s := testStore(t)
	use(cfg)
	mux := http.NewServeMux()
	routesTeams(mux)
	return &gateRig{s: s, h: authGate(mux, s, mux), cfg: cfg}, f
}

func TestTeamsActionNeedsExec(t *testing.T) {
	g, _ := teamsRig(t)
	u := g.user(t, "lecteur", false)
	g.grant(t, u, "", RightSessionsRead)
	for _, c := range []struct{ m, p, body string }{
		{"POST", "/team-send", `{"name":"` + trName + `","to":"ada","text":"x"}`},
		{"POST", "/team-key", `{"name":"` + trName + `","agent":"ada","key":"Enter"}`},
		{"POST", "/team-close", `{"name":"` + trName + `"}`},
		{"POST", "/teams-launch", `{"teamIds":["a"],"goal":"x","dir":"/tmp"}`},
	} {
		if w := g.do(c.m, c.p, &u, "", c.body); w.Code != 403 {
			t.Errorf("%s : %d", c.p, w.Code)
		}
	}
	for _, p := range []string{"/team?name=" + trName, "/teams-catalog", "/teams"} {
		if w := g.do("GET", p, &u, "", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s : %d %q", p, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}

func TestTeamsEditNeedsConfigWrite(t *testing.T) {
	g, _ := teamsRig(t)
	u := g.user(t, "lanceur", false)
	g.grant(t, u, "", RightExec)
	for _, p := range []string{"/teams-agent-save", "/teams-agent-rm", "/teams-team-save", "/teams-team-rm"} {
		if w := g.do("POST", p, &u, "", `{}`); w.Code != 403 {
			t.Errorf("%s : %d", p, w.Code)
		}
	}
}

func TestTeamBadNameRejected(t *testing.T) {
	g, f := teamsRig(t)
	u := g.user(t, "admin", true)
	g.grant(t, u, "", RightExec, RightSessionsRead)
	for _, n := range []string{"../x", ""} {
		for _, c := range []struct{ m, p, body string }{
			{"GET", "/team?name=" + n, ""},
			{"GET", "/team-screen?agent=ada&name=" + n, ""},
			{"POST", "/team-send", `{"name":"` + n + `","to":"ada","text":"x"}`},
			{"POST", "/team-key", `{"name":"` + n + `","agent":"ada","key":"Enter"}`},
			{"POST", "/team-close", `{"name":"` + n + `"}`},
		} {
			w := g.do(c.m, c.p, &u, "", c.body)
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"ok":false`) {
				t.Errorf("%s %q : %d %s", c.p, n, w.Code, w.Body)
			}
		}
	}
	if len(f.pastes)+len(f.keys)+len(f.closed) != 0 {
		t.Fatal("un appel a atteint les panes")
	}
}

func TestTeamSendUsesPrincipalName(t *testing.T) {
	g, f := teamsRig(t)
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightExec)
	w := g.do("POST", "/team-send", &u, "", `{"name":"`+trName+`","to":"ada","text":"salut","author":"mallory"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal(w.Code, w.Body)
	}
	d, _ := journalRead(g.cfg, trName)
	if len(d) != 1 || d[0].Author != "alice" || len(f.pastes) != 1 {
		t.Fatalf("journal : %v", d)
	}
	// Sans garde : mode mono-utilisateur.
	mux := http.NewServeMux()
	routesTeams(mux)
	r := httptest.NewRequest("POST", "/team-send", strings.NewReader(`{"name":"`+trName+`","to":"ada","text":"b","author":"mallory"}`))
	mux.ServeHTTP(httptest.NewRecorder(), r)
	d, _ = journalRead(g.cfg, trName)
	if len(d) != 2 || d[1].Author != "local" {
		t.Fatalf("journal : %v", d)
	}
}

func TestTeamsPassiveRoutes(t *testing.T) {
	for _, p := range []string{"GET /team", "GET /teams"} {
		if !passiveRoutes[p] {
			t.Errorf("%s non passive", p)
		}
	}
}

func TestTeamsLaunchConflictShape(t *testing.T) {
	g, _ := teamsRig(t)
	fakeLaunchEnv(t)
	u := g.user(t, "admin", true)
	g.grant(t, u, "", RightExec)
	// Un agent perso homonyme sans marqueur : conflit avant tout lancement.
	os.MkdirAll(filepath.Join(g.cfg, "agents"), 0o755)
	os.WriteFile(filepath.Join(g.cfg, "agents", "ada.md"), []byte("---\nname: ada\n---\nmien\n"), 0o644)
	w := g.do("POST", "/teams-launch", &u, "", `{"teamIds":["feature"],"goal":"x","dir":"`+t.TempDir()+`"}`)
	var out struct {
		Ok  bool
		Out string
		Run Run
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Ok || out.Out == "" || len(out.Run.Conflicts) != 1 || out.Run.Conflicts[0] != "ada" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestTeamsRightsMatrix(t *testing.T) {
	g, _ := teamsRig(t)
	cw := g.user(t, "editeur", false)
	g.grant(t, cw, "", RightConfigWrite)
	if w := g.do("POST", "/teams-agent-rm", &cw, "", `{"id":"x"}`); w.Code == 403 {
		t.Errorf("config_write refusé : %d", w.Code)
	}
	rd := g.user(t, "lecteur", false)
	g.grant(t, rd, "", RightSessionsRead)
	for _, p := range []string{"/teams-agent-save", "/teams-agent-rm", "/teams-team-save", "/teams-team-rm"} {
		if w := g.do("POST", p, &rd, "", `{}`); w.Code != 403 {
			t.Errorf("%s : %d", p, w.Code)
		}
	}
	if w := g.do("GET", "/team-screen?name="+trName+"&agent=ada", &rd, "", ""); w.Code != 200 {
		t.Errorf("team-screen : %d", w.Code)
	}
}

func TestTeamSendBearerIsToken(t *testing.T) {
	g, _ := teamsRig(t)
	old := tokenPrincipal
	tokenPrincipal = func(*http.Request) *Principal { return &Principal{Token: true, MFA: true} }
	t.Cleanup(func() { tokenPrincipal = old })
	r := g.req("POST", "/team-send", nil, "", `{"name":"`+trName+`","to":"ada","text":"x"}`)
	r.Header.Set("Authorization", "Bearer abc")
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	d, _ := journalRead(g.cfg, trName)
	if w.Code != 200 || len(d) != 1 || d[0].Author != "token" {
		t.Fatalf("%d %s %v", w.Code, w.Body, d)
	}
}

func TestAuthorHonoursHeaderOnlyWithChildToken(t *testing.T) {
	mk := func(tok string) *http.Request {
		r := httptest.NewRequest("POST", "/team-send", nil)
		r.Header.Set(userHeader, "bob\x07"+strings.Repeat("é", 80))
		if tok != "" {
			r.Header.Set(childHeader, tok)
		}
		return r
	}
	if a := teamAuthor(mk("")); a != "local" { // processus principal : en-tête forgeable, ignoré
		t.Errorf("principal : %q", a)
	}
	childToken = "secret"
	t.Cleanup(func() { childToken = "" })
	if a := teamAuthor(mk("autre")); a != "local" {
		t.Errorf("mauvais jeton : %q", a)
	}
	if a := teamAuthor(mk("secret")); a != "bob"+strings.Repeat("é", 61) {
		t.Errorf("jeton valide : %q", a)
	}
}

func proxyHeader(t *testing.T, r *http.Request) http.Header {
	t.Helper()
	var got http.Header
	back := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer back.Close()
	u, _ := url.Parse(back.URL)
	childProxy(u, "jeton").ServeHTTP(httptest.NewRecorder(), r)
	if got == nil {
		t.Fatal("requête non relayée")
	}
	return got
}

func withPrincipal(r *http.Request, p *Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), gateKey{}, &gateInfo{p: p}))
}

func TestChildProxySetsUserHeader(t *testing.T) {
	r := withPrincipal(httptest.NewRequest("GET", "/api", nil), &Principal{User: &User{Name: "alice"}})
	if h := proxyHeader(t, r); h.Get(userHeader) != "alice" {
		t.Fatalf("%q", h.Get(userHeader))
	}
	r = withPrincipal(httptest.NewRequest("GET", "/api", nil), &Principal{Token: true})
	if h := proxyHeader(t, r); h.Get(userHeader) != "token" {
		t.Fatalf("%q", h.Get(userHeader))
	}
}

func TestChildProxyOverwritesClientUserHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/api", nil)
	r.Header.Set(userHeader, "mallory")
	if h := proxyHeader(t, r); h.Get(userHeader) != "" { // sans principal : effacé
		t.Fatalf("sans principal : %q", h.Get(userHeader))
	}
	r = withPrincipal(r, &Principal{User: &User{Name: "alice"}})
	if h := proxyHeader(t, r); h.Get(userHeader) != "alice" {
		t.Fatalf("%q", h.Get(userHeader))
	}
}

func TestTeamsComposeDedup(t *testing.T) {
	g, _ := teamsRig(t)
	u := g.user(t, "lecteur", false)
	g.grant(t, u, "", RightSessionsRead)
	cat, _ := loadCatalog(g.cfg)
	if len(cat.Teams) == 0 {
		t.Skip("catalogue vide")
	}
	id := cat.Teams[0].ID
	w1 := g.do("POST", "/teams-compose", &u, "", `{"teamIds":["`+id+`"]}`)
	w2 := g.do("POST", "/teams-compose", &u, "", `{"teamIds":["`+id+`","`+id+`"]}`)
	var a, b struct {
		Ok    bool
		Mates []Agent
	}
	json.Unmarshal(w1.Body.Bytes(), &a)
	json.Unmarshal(w2.Body.Bytes(), &b)
	if !a.Ok || !b.Ok || len(a.Mates) != len(b.Mates) {
		t.Fatalf("%s / %s", w1.Body, w2.Body)
	}
}
