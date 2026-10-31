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
	cfg, localFakeIO := setup(t)
	authStore := testStore(t)
	use(cfg)
	mux := http.NewServeMux()
	routesTeams(mux)
	return &gateRig{s: authStore, h: authGate(mux, authStore, mux), cfg: cfg}, localFakeIO
}

func TestTeamsActionNeedsExec(t *testing.T) {
	gateRig, _ := teamsRig(t)
	gateUser := gateRig.user(t, "lecteur", false)
	gateRig.grant(t, gateUser, "", RightSessionsRead)
	for _, record := range []struct{ m, p, body string }{
		{"POST", "/v1/teams/" + trName + "/run/messages", `{"to":"ada","text":"x"}`},
		{"POST", "/v1/teams/" + trName + "/run/keystrokes", `{"agent":"ada","key":"Enter"}`},
		{"DELETE", "/v1/teams/" + trName + "/run", ``},
		{"POST", "/v1/teams/a/runs", `{"goal":"x","dir":"/tmp"}`},
	} {
		if recorder := gateRig.do(record.m, record.p, &gateUser, "", record.body); recorder.Code != 403 {
			t.Errorf("%s : %d", record.p, recorder.Code)
		}
	}
	for _, path := range []string{"/v1/teams/" + trName + "/run", "/v1/teams/catalog", "/v1/teams"} {
		if recorder := gateRig.do("GET", path, &gateUser, "", ""); recorder.Code != 200 || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s : %d %q", path, recorder.Code, recorder.Header().Get("Cache-Control"))
		}
	}
}

func TestTeamsEditNeedsConfigWrite(t *testing.T) {
	gateRig, _ := teamsRig(t)
	gateUser := gateRig.user(t, "lanceur", false)
	gateRig.grant(t, gateUser, "", RightExec)
	for _, values := range [][2]string{{"PUT", "/v1/agents/x"}, {"DELETE", "/v1/agents/x"}, {"PUT", "/v1/teams/x"}, {"DELETE", "/v1/teams/x"}} {
		if recorder := gateRig.do(values[0], values[1], &gateUser, "", `{}`); recorder.Code != 403 {
			t.Errorf("%s : %d", values[1], recorder.Code)
		}
	}
}

func TestTeamBadNameRejected(t *testing.T) {
	gateRig, fakeIO := teamsRig(t)
	gateUser := gateRig.user(t, "admin", true)
	gateRig.grant(t, gateUser, "", RightExec, RightSessionsRead)
	for _, name := range []string{"a%2F..%2Fb", "a"} {
		for _, record := range []struct{ m, p, body string }{
			{"GET", "/v1/teams/" + name + "/run", ""},
			{"GET", "/v1/teams/" + name + "/run/screen?agent=ada", ""},
			{"POST", "/v1/teams/" + name + "/run/messages", `{"to":"ada","text":"x"}`},
			{"POST", "/v1/teams/" + name + "/run/keystrokes", `{"agent":"ada","key":"Enter"}`},
			{"DELETE", "/v1/teams/" + name + "/run", ""},
		} {
			recorder := gateRig.do(record.m, record.p, &gateUser, "", record.body)
			if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), `"bad_name"`) {
				t.Errorf("%s %q : %d %s", record.p, name, recorder.Code, recorder.Body)
			}
		}
	}
	if len(fakeIO.pastes)+len(fakeIO.keys)+len(fakeIO.closed) != 0 {
		t.Fatal("un appel a atteint les panes")
	}
}

func TestTeamSendUsesPrincipalName(t *testing.T) {
	gateRig, fakeIO := teamsRig(t)
	gateUser := gateRig.user(t, "alice", false)
	gateRig.grant(t, gateUser, "", RightExec)
	recorder := gateRig.do("POST", "/v1/teams/"+trName+"/run/messages", &gateUser, "", `{"to":"ada","text":"salut","author":"mallory"}`)
	if recorder.Code != 200 {
		t.Fatal(recorder.Code, recorder.Body)
	}
	directMsgs, _ := journalRead(gateRig.cfg, trName)
	if len(directMsgs) != 1 || directMsgs[0].Author != "alice" || len(fakeIO.pastes) != 1 {
		t.Fatalf("journal : %v", directMsgs)
	}
	// Sans garde : mode mono-utilisateur.
	mux := http.NewServeMux()
	routesTeams(mux)
	request := httptest.NewRequest("POST", "/v1/teams/"+trName+"/run/messages", strings.NewReader(`{"to":"ada","text":"b","author":"mallory"}`))
	mux.ServeHTTP(httptest.NewRecorder(), request)
	directMsgs, _ = journalRead(gateRig.cfg, trName)
	if len(directMsgs) != 2 || directMsgs[1].Author != "local" {
		t.Fatalf("journal : %v", directMsgs)
	}
}

func TestTeamsPassiveRoutes(t *testing.T) {
	for _, path := range []string{"GET /v1/teams/{team}/run", "GET /v1/teams"} {
		if !passiveRoutes[path] {
			t.Errorf("%s non passive", path)
		}
	}
}

func TestTeamsLaunchConflictShape(t *testing.T) {
	gateRig, _ := teamsRig(t)
	fakeLaunchEnv(t)
	gateUser := gateRig.user(t, "admin", true)
	gateRig.grant(t, gateUser, "", RightExec)
	// Un agent perso homonyme sans marqueur : conflit avant tout lancement.
	os.MkdirAll(filepath.Join(gateRig.cfg, "agents"), 0o755)
	os.WriteFile(filepath.Join(gateRig.cfg, "agents", "backend.md"), []byte("---\nname: backend\n---\nmien\n"), 0o644)
	recorder := gateRig.do("POST", "/v1/teams/feature/runs", &gateUser, "", `{"goal":"x","dir":"`+t.TempDir()+`"}`)
	var out struct {
		Error struct{ Code, Message string }
		Run   Run
	}
	json.Unmarshal(recorder.Body.Bytes(), &out)
	if recorder.Code != 409 || out.Error.Code != "conflict" || len(out.Run.Conflicts) != 1 || out.Run.Conflicts[0] != "backend" {
		t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
}

func TestTeamsRightsMatrix(t *testing.T) {
	gateRig, _ := teamsRig(t)
	cw := gateRig.user(t, "editeur", false)
	gateRig.grant(t, cw, "", RightConfigWrite)
	if recorder := gateRig.do("DELETE", "/v1/agents/x", &cw, "", ``); recorder.Code == 403 {
		t.Errorf("config_write refusé : %d", recorder.Code)
	}
	rd := gateRig.user(t, "lecteur", false)
	gateRig.grant(t, rd, "", RightSessionsRead)
	for _, values := range [][2]string{{"PUT", "/v1/agents/x"}, {"DELETE", "/v1/agents/x"}, {"PUT", "/v1/teams/x"}, {"DELETE", "/v1/teams/x"}} {
		if recorder := gateRig.do(values[0], values[1], &rd, "", `{}`); recorder.Code != 403 {
			t.Errorf("%s : %d", values[1], recorder.Code)
		}
	}
	if recorder := gateRig.do("GET", "/v1/teams/"+trName+"/run/screen?agent=ada", &rd, "", ""); recorder.Code != 200 {
		t.Errorf("team-screen : %d", recorder.Code)
	}
}

func TestTeamSendBearerIsToken(t *testing.T) {
	gateRig, _ := teamsRig(t)
	old := tokenPrincipal
	tokenPrincipal = func(*http.Request) *Principal { return &Principal{Token: true, MFA: true} }
	t.Cleanup(func() { tokenPrincipal = old })
	request := gateRig.req("POST", "/v1/teams/"+trName+"/run/messages", nil, "", `{"to":"ada","text":"x"}`)
	request.Header.Set("Authorization", "Bearer abc")
	recorder := httptest.NewRecorder()
	gateRig.h.ServeHTTP(recorder, request)
	directMsgs, _ := journalRead(gateRig.cfg, trName)
	if recorder.Code != 200 || len(directMsgs) != 1 || directMsgs[0].Author != "token" {
		t.Fatalf("%d %s %v", recorder.Code, recorder.Body, directMsgs)
	}
}

func TestAuthorHonoursHeaderOnlyWithChildToken(t *testing.T) {
	mk := func(tok string) *http.Request {
		request := httptest.NewRequest("POST", "/v1/teams/"+trName+"/run/messages", nil)
		request.Header.Set(userHeader, "bob\x07"+strings.Repeat("é", 80))
		if tok != "" {
			request.Header.Set(childHeader, tok)
		}
		return request
	}
	if argument := teamAuthor(mk("")); argument != "local" { // processus principal : en-tête forgeable, ignoré
		t.Errorf("principal : %q", argument)
	}
	childToken = "secret"
	t.Cleanup(func() { childToken = "" })
	if argument := teamAuthor(mk("autre")); argument != "local" {
		t.Errorf("mauvais jeton : %q", argument)
	}
	if argument := teamAuthor(mk("secret")); argument != "bob"+strings.Repeat("é", 61) {
		t.Errorf("jeton valide : %q", argument)
	}
}

func proxyHeader(t *testing.T, request *http.Request) http.Header {
	t.Helper()
	var got http.Header
	back := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { got = request.Header.Clone() }))
	defer back.Close()
	uRL, _ := url.Parse(back.URL)
	childProxy(uRL, "jeton").ServeHTTP(httptest.NewRecorder(), request)
	if got == nil {
		t.Fatal("requête non relayée")
	}
	return got
}

func withPrincipal(request *http.Request, principal *Principal) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), gateKey{}, &gateInfo{p: principal}))
}

func TestChildProxySetsUserHeader(t *testing.T) {
	request := withPrincipal(httptest.NewRequest("GET", "/v1/state", nil), &Principal{User: &User{Name: "alice"}})
	if header := proxyHeader(t, request); header.Get(userHeader) != "alice" {
		t.Fatalf("%q", header.Get(userHeader))
	}
	request = withPrincipal(httptest.NewRequest("GET", "/v1/state", nil), &Principal{Token: true})
	if header := proxyHeader(t, request); header.Get(userHeader) != "token" {
		t.Fatalf("%q", header.Get(userHeader))
	}
}

func TestChildProxyOverwritesClientUserHeader(t *testing.T) {
	request := httptest.NewRequest("GET", "/v1/state", nil)
	request.Header.Set(userHeader, "mallory")
	if header := proxyHeader(t, request); header.Get(userHeader) != "" { // sans principal : effacé
		t.Fatalf("sans principal : %q", header.Get(userHeader))
	}
	request = withPrincipal(request, &Principal{User: &User{Name: "alice"}})
	if header := proxyHeader(t, request); header.Get(userHeader) != "alice" {
		t.Fatalf("%q", header.Get(userHeader))
	}
}

func TestTeamsComposeDedup(t *testing.T) {
	gateRig, _ := teamsRig(t)
	gateUser := gateRig.user(t, "lecteur", false)
	gateRig.grant(t, gateUser, "", RightSessionsRead)
	cat, _ := loadCatalog(gateRig.cfg)
	if len(cat.Teams) == 0 {
		t.Skip("catalogue vide")
	}
	id := cat.Teams[0].ID
	w1 := gateRig.do("POST", "/v1/teams/compositions", &gateUser, "", `{"teamIds":["`+id+`"]}`)
	w2 := gateRig.do("POST", "/v1/teams/compositions", &gateUser, "", `{"teamIds":["`+id+`","`+id+`"]}`)
	var record, localRecord struct {
		Mates []Agent
	}
	json.Unmarshal(w1.Body.Bytes(), &record)
	json.Unmarshal(w2.Body.Bytes(), &localRecord)
	if w1.Code != 200 || w2.Code != 200 || len(record.Mates) != len(localRecord.Mates) {
		t.Fatalf("%s / %s", w1.Body, w2.Body)
	}
}

func TestRoutesRunMatesShape(t *testing.T) {
	gateRig, _ := teamsRig(t)
	fakeLaunchEnv(t)
	gateUser := gateRig.user(t, "admin", true)
	gateRig.grant(t, gateUser, "", RightExec)
	recorder := gateRig.do("POST", "/v1/teams/feature/runs", &gateUser, "", `{"goal":"x","dir":"`+t.TempDir()+`"}`)
	var out struct {
		Mates []map[string]any
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil || recorder.Code != 201 || len(out.Mates) == 0 {
		t.Fatalf("%v %s", err, recorder.Body)
	}
	for _, lookup := range out.Mates {
		id, _ := lookup["ID"].(string)
		nm, _ := lookup["Name"].(string)
		if len(lookup) != 2 || id == "" || nm == "" {
			t.Errorf("forme : %v", lookup)
		}
	}
}

func TestTeamsAgentCreateAndRemove(t *testing.T) {
	gateRig, _ := teamsRig(t)
	gateUser := gateRig.user(t, "editeur", true)
	gateRig.grant(t, gateUser, "", RightConfigWrite)
	recorder := gateRig.do("PUT", "/v1/agents/new", &gateUser, "", `{"Name":"zed","Role":"qa","Access":"read"}`)
	var agent Agent
	json.Unmarshal(recorder.Body.Bytes(), &agent)
	if recorder.Code != 201 || agent.ID == "" || agent.ID == "new" {
		t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
	if recorder := gateRig.do("DELETE", "/v1/agents/"+agent.ID, &gateUser, "", ""); recorder.Code != 204 {
		t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
	if recorder := gateRig.do("DELETE", "/v1/agents/"+agent.ID, &gateUser, "", ""); recorder.Code != 400 || !strings.Contains(recorder.Body.String(), `"error"`) {
		t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
}
