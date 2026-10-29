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
		{"POST", "/v1/teams/" + trName + "/run/messages", `{"to":"ada","text":"x"}`},
		{"POST", "/v1/teams/" + trName + "/run/keystrokes", `{"agent":"ada","key":"Enter"}`},
		{"DELETE", "/v1/teams/" + trName + "/run", ``},
		{"POST", "/v1/teams/a/runs", `{"goal":"x","dir":"/tmp"}`},
	} {
		if w := g.do(c.m, c.p, &u, "", c.body); w.Code != 403 {
			t.Errorf("%s : %d", c.p, w.Code)
		}
	}
	for _, p := range []string{"/v1/teams/" + trName + "/run", "/v1/teams/catalog", "/v1/teams"} {
		if w := g.do("GET", p, &u, "", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s : %d %q", p, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}

func TestTeamsEditNeedsConfigWrite(t *testing.T) {
	g, _ := teamsRig(t)
	u := g.user(t, "lanceur", false)
	g.grant(t, u, "", RightExec)
	for _, c := range [][2]string{{"PUT", "/v1/agents/x"}, {"DELETE", "/v1/agents/x"}, {"PUT", "/v1/teams/x"}, {"DELETE", "/v1/teams/x"}} {
		if w := g.do(c[0], c[1], &u, "", `{}`); w.Code != 403 {
			t.Errorf("%s : %d", c[1], w.Code)
		}
	}
}

func TestTeamBadNameRejected(t *testing.T) {
	g, f := teamsRig(t)
	u := g.user(t, "admin", true)
	g.grant(t, u, "", RightExec, RightSessionsRead)
	for _, n := range []string{"a%2F..%2Fb", "a"} {
		for _, c := range []struct{ m, p, body string }{
			{"GET", "/v1/teams/" + n + "/run", ""},
			{"GET", "/v1/teams/" + n + "/run/screen?agent=ada", ""},
			{"POST", "/v1/teams/" + n + "/run/messages", `{"to":"ada","text":"x"}`},
			{"POST", "/v1/teams/" + n + "/run/keystrokes", `{"agent":"ada","key":"Enter"}`},
			{"DELETE", "/v1/teams/" + n + "/run", ""},
		} {
			w := g.do(c.m, c.p, &u, "", c.body)
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"bad_name"`) {
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
	w := g.do("POST", "/v1/teams/"+trName+"/run/messages", &u, "", `{"to":"ada","text":"salut","author":"mallory"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	d, _ := journalRead(g.cfg, trName)
	if len(d) != 1 || d[0].Author != "alice" || len(f.pastes) != 1 {
		t.Fatalf("journal : %v", d)
	}
	// Sans garde : mode mono-utilisateur.
	mux := http.NewServeMux()
	routesTeams(mux)
	r := httptest.NewRequest("POST", "/v1/teams/"+trName+"/run/messages", strings.NewReader(`{"to":"ada","text":"b","author":"mallory"}`))
	mux.ServeHTTP(httptest.NewRecorder(), r)
	d, _ = journalRead(g.cfg, trName)
	if len(d) != 2 || d[1].Author != "local" {
		t.Fatalf("journal : %v", d)
	}
}

func TestTeamsPassiveRoutes(t *testing.T) {
	for _, p := range []string{"GET /v1/teams/{team}/run", "GET /v1/teams"} {
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
	os.WriteFile(filepath.Join(g.cfg, "agents", "backend.md"), []byte("---\nname: backend\n---\nmien\n"), 0o644)
	w := g.do("POST", "/v1/teams/feature/runs", &u, "", `{"goal":"x","dir":"`+t.TempDir()+`"}`)
	var out struct {
		Error struct{ Code, Message string }
		Run   Run
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 409 || out.Error.Code != "conflict" || len(out.Run.Conflicts) != 1 || out.Run.Conflicts[0] != "backend" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestTeamsRightsMatrix(t *testing.T) {
	g, _ := teamsRig(t)
	cw := g.user(t, "editeur", false)
	g.grant(t, cw, "", RightConfigWrite)
	if w := g.do("DELETE", "/v1/agents/x", &cw, "", ``); w.Code == 403 {
		t.Errorf("config_write refusé : %d", w.Code)
	}
	rd := g.user(t, "lecteur", false)
	g.grant(t, rd, "", RightSessionsRead)
	for _, c := range [][2]string{{"PUT", "/v1/agents/x"}, {"DELETE", "/v1/agents/x"}, {"PUT", "/v1/teams/x"}, {"DELETE", "/v1/teams/x"}} {
		if w := g.do(c[0], c[1], &rd, "", `{}`); w.Code != 403 {
			t.Errorf("%s : %d", c[1], w.Code)
		}
	}
	if w := g.do("GET", "/v1/teams/"+trName+"/run/screen?agent=ada", &rd, "", ""); w.Code != 200 {
		t.Errorf("team-screen : %d", w.Code)
	}
}

func TestTeamSendBearerIsToken(t *testing.T) {
	g, _ := teamsRig(t)
	old := tokenPrincipal
	tokenPrincipal = func(*http.Request) *Principal { return &Principal{Token: true, MFA: true} }
	t.Cleanup(func() { tokenPrincipal = old })
	r := g.req("POST", "/v1/teams/"+trName+"/run/messages", nil, "", `{"to":"ada","text":"x"}`)
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
		r := httptest.NewRequest("POST", "/v1/teams/"+trName+"/run/messages", nil)
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
	r := withPrincipal(httptest.NewRequest("GET", "/v1/state", nil), &Principal{User: &User{Name: "alice"}})
	if h := proxyHeader(t, r); h.Get(userHeader) != "alice" {
		t.Fatalf("%q", h.Get(userHeader))
	}
	r = withPrincipal(httptest.NewRequest("GET", "/v1/state", nil), &Principal{Token: true})
	if h := proxyHeader(t, r); h.Get(userHeader) != "token" {
		t.Fatalf("%q", h.Get(userHeader))
	}
}

func TestChildProxyOverwritesClientUserHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/state", nil)
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
	w1 := g.do("POST", "/v1/teams/compositions", &u, "", `{"teamIds":["`+id+`"]}`)
	w2 := g.do("POST", "/v1/teams/compositions", &u, "", `{"teamIds":["`+id+`","`+id+`"]}`)
	var a, b struct {
		Mates []Agent
	}
	json.Unmarshal(w1.Body.Bytes(), &a)
	json.Unmarshal(w2.Body.Bytes(), &b)
	if w1.Code != 200 || w2.Code != 200 || len(a.Mates) != len(b.Mates) {
		t.Fatalf("%s / %s", w1.Body, w2.Body)
	}
}

func TestRoutesRunMatesShape(t *testing.T) {
	g, _ := teamsRig(t)
	fakeLaunchEnv(t)
	u := g.user(t, "admin", true)
	g.grant(t, u, "", RightExec)
	w := g.do("POST", "/v1/teams/feature/runs", &u, "", `{"goal":"x","dir":"`+t.TempDir()+`"}`)
	var out struct {
		Mates []map[string]any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 201 || len(out.Mates) == 0 {
		t.Fatalf("%v %s", err, w.Body)
	}
	for _, m := range out.Mates {
		id, _ := m["ID"].(string)
		nm, _ := m["Name"].(string)
		if len(m) != 2 || id == "" || nm == "" {
			t.Errorf("forme : %v", m)
		}
	}
}

func TestTeamsAgentCreateAndRemove(t *testing.T) {
	g, _ := teamsRig(t)
	u := g.user(t, "editeur", true)
	g.grant(t, u, "", RightConfigWrite)
	w := g.do("PUT", "/v1/agents/new", &u, "", `{"Name":"zed","Role":"qa","Access":"read"}`)
	var a Agent
	json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 201 || a.ID == "" || a.ID == "new" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := g.do("DELETE", "/v1/agents/"+a.ID, &u, "", ""); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := g.do("DELETE", "/v1/agents/"+a.ID, &u, "", ""); w.Code != 400 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
