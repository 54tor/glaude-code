package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	old := teamTmux
	teamTmux = func() string { return "tmux" }
	t.Cleanup(func() { teamTmux = old })
	u := g.user(t, "admin", true)
	g.grant(t, u, "", RightExec)
	// Une définition perso déjà présente sur disque avec un autre contenu donne un conflit.
	cat, _ := loadCatalog(g.cfg)
	if len(cat.Teams) == 0 {
		t.Skip("catalogue vide")
	}
	w := g.do("POST", "/teams-launch", &u, "", `{"teamIds":["inconnue"],"goal":"x","dir":"`+t.TempDir()+`"}`)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["ok"] != false || out["out"] == "" {
		t.Fatalf("%d %s", w.Code, w.Body)
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
