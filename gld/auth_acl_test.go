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
	authStore := testStore(t)
	cfg := t.TempDir()
	use(cfg)
	mux := http.NewServeMux()
	for pat := range routeRights {
		mux.HandleFunc(pat, func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Write([]byte("ok " + pat))
		})
	}
	for _, pat := range extra {
		mux.HandleFunc(pat, func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Write([]byte("ok " + pat))
		})
	}
	return &gateRig{s: authStore, h: authGate(mux, authStore, mux), cfg: cfg}
}

type gateUser struct {
	id           int64
	cookie, csrf string
}

func (localGateRig *gateRig) user(t *testing.T, name string, admin bool) gateUser {
	t.Helper()
	id, err := localGateRig.s.AddUser(name, testPw, admin)
	if err != nil {
		t.Fatal(err)
	}
	content, err := localGateRig.s.NewSession(id, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := localGateRig.s.Session(content)
	if err != nil {
		t.Fatal(err)
	}
	content, _, err = localGateRig.s.PromoteMFA(principal.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if principal, err = localGateRig.s.Session(content); err != nil {
		t.Fatal(err)
	}
	return gateUser{id, content, principal.CSRF}
}

func (localGateRig *gateRig) grant(t *testing.T, localGateUser gateUser, home string, rights ...Right) {
	t.Helper()
	id, ok := homeIDFor(home)
	if !ok {
		t.Fatalf("home %q inconnu", home)
	}
	if err := localGateRig.rawGrant(localGateUser.id, id, rights...); err != nil {
		t.Fatal(err)
	}
}

// rawGrant écrit la ligne d'ACL sans l'invariant « écrire exige lire » de SetRights : la porte doit aussi tenir devant une
// ligne héritée qui le viole.
func (localGateRig *gateRig) rawGrant(userID int64, homeID string, rights ...Right) error {
	return localGateRig.s.inTx(func(tx *sql.Tx) error {
		lookup, err := localGateRig.s.rights(tx, userID, homeID)
		if err != nil {
			return err
		}
		for _, right := range rights {
			lookup[right] = true
		}
		args := []any{userID, homeID}
		for _, right := range allRights {
			args = append(args, lookup[right])
		}
		args = append(args, localGateRig.s.aclSig(userID, homeID, lookup))
		_, err = tx.Exec(`INSERT OR REPLACE INTO home_acl(user_id,home_id,`+rightCols()+`,sig) VALUES(?,?`+strings.Repeat(",?", len(allRights)+1)+`)`, args...)
		return err
	})
}

func (localGateRig *gateRig) req(method, target string, localGateUser *gateUser, home, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if localGateUser != nil {
		request.AddCookie(&http.Cookie{Name: sessCookie, Value: localGateUser.cookie})
		request.Header.Set(csrfHeader, localGateUser.csrf)
	}
	if home != "" {
		request.AddCookie(&http.Cookie{Name: homeCookie, Value: url.QueryEscape(home)})
	}
	return request
}

func (localGateRig *gateRig) do(method, target string, localGateUser *gateUser, home, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	localGateRig.h.ServeHTTP(recorder, localGateRig.req(method, target, localGateUser, home, body))
	return recorder
}

func reached(recorder *httptest.ResponseRecorder) bool {
	return strings.HasPrefix(recorder.Body.String(), "ok ")
}

func addHomes(t *testing.T, names ...string) {
	t.Helper()
	var hs []Home
	for _, name := range names {
		hs = append(hs, Home{Name: name, Dir: t.TempDir()})
	}
	if err := saveHomes(hs); err != nil {
		t.Fatal(err)
	}
}

func TestEveryRouteClassified(t *testing.T) {
	re := regexp.MustCompile(`"((?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS) /[^"\s]*)"`)
	files, _ := filepath.Glob("*.go")
	inCode := map[string]bool{}
	for _, filePath := range files {
		if strings.HasSuffix(filePath, "_test.go") || filePath == "auth_acl.go" {
			continue
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		for _, values := range re.FindAllStringSubmatch(string(data), -1) {
			inCode[values[1]] = true
			if _, ok := routeRights[values[1]]; !ok {
				t.Errorf("%s : route %q absente de routeRights", filePath, values[1])
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
	gateRig := newGateRig(t, "POST /fictive", "GET /fictive-lecture")
	gateUser := gateRig.user(t, "admin", true)
	gateRig.grant(t, gateUser, "", allRights...)
	for _, values := range [][2]string{{"POST", "/fictive"}, {"GET", "/fictive-lecture"}} {
		if recorder := gateRig.do(values[0], values[1], &gateUser, "", "{}"); recorder.Code != 403 || reached(recorder) {
			t.Errorf("%s %s non classée : %d %s", values[0], values[1], recorder.Code, recorder.Body)
		}
	}
	if recorder := gateRig.do("GET", "/absente", &gateUser, "", ""); recorder.Code != 404 || reached(recorder) {
		t.Errorf("route inconnue : %d", recorder.Code)
	}
	if recorder := gateRig.do("DELETE", "/v1/memory/files/x", &gateUser, "", ""); recorder.Code != 404 || reached(recorder) {
		t.Errorf("méthode inconnue : %d", recorder.Code)
	}
}

func TestGatePathVariants(t *testing.T) {
	gateRig := newGateRig(t)
	gateUser := gateRig.user(t, "lecteur", false)
	gateRig.grant(t, gateUser, "", RightMemRead)
	if recorder := gateRig.do("PUT", "/v1/memory/files/x", &gateUser, "", "{}"); recorder.Code != 403 {
		t.Fatalf("témoin : %d", recorder.Code)
	}
	for _, path := range []string{"//v1/memory/files/x", "/V1/MEMORY/FILES/x", "/v1/memory/files/x/", "/v1/./memory/files/x", "/v1/y/../memory/files/x",
		"/v1/memory/files%2Fx", "/v1/%6Demory/files/x", "/v1/memory/files/x%00", "/v1/memory/files/x/y", "/v1/memory/files/x/../y"} {
		recorder := gateRig.do("PUT", path, &gateUser, "", "{}") // cible lue comme une ligne de requête : « //x » reste un chemin
		if reached(recorder) || recorder.Code < 400 {
			t.Errorf("%s : %d %q obtenu sans mem_write", path, recorder.Code, recorder.Body)
		}
	}
	if recorder := gateRig.do("GET", "/v1/memory/files", &gateUser, "", ""); !reached(recorder) {
		t.Fatalf("lecture refusée : %d", recorder.Code)
	}
}

func TestGateHomeCookieNoRights(t *testing.T) {
	gateRig := newGateRig(t)
	addHomes(t, "Pro")
	gateUser := gateRig.user(t, "alice", false)
	gateRig.grant(t, gateUser, "", RightMemRead)
	for _, home := range []string{"Pro", "inconnu"} {
		if recorder := gateRig.do("GET", "/v1/memory/files", &gateUser, home, ""); recorder.Code != 403 || reached(recorder) {
			t.Errorf("cookie %q : %d %s", home, recorder.Code, recorder.Body)
		}
	}
	if recorder := gateRig.do("PUT", "/v1/homes/current", &gateUser, "", `{"name":"Pro"}`); recorder.Code != 403 {
		t.Errorf("choix d'un home sans droit : %d", recorder.Code)
	}
	if recorder := gateRig.do("PUT", "/v1/homes/current", &gateUser, "", `{"name":""}`); !reached(recorder) {
		t.Errorf("choix du home principal : %d", recorder.Code)
	}

	// Droit vérifié pour Pro, enfant en panne : jamais servi par le home principal.
	gateRig.grant(t, gateUser, "Pro", RightMemRead)
	old := homeStart
	t.Cleanup(func() { homeStart = old })
	homeStart = func(Home) (*homeProc, error) { return nil, errors.New("panne") }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/memory/files", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Write([]byte("ok principal"))
	})
	handler := authGate(mux, gateRig.s, withHomes(mux))
	localRecorder := httptest.NewRecorder()
	handler.ServeHTTP(localRecorder, gateRig.req("GET", "/v1/memory/files", &gateUser, "Pro", ""))
	if localRecorder.Code != 503 || reached(localRecorder) {
		t.Fatalf("repli sur le home principal : %d %s", localRecorder.Code, localRecorder.Body)
	}
}

func TestGateMemSyncNeedsBoth(t *testing.T) {
	gateRig := newGateRig(t)
	addHomes(t, "Pro")
	gateUser := gateRig.user(t, "bob", false)
	gateRig.grant(t, gateUser, "", RightMemSync, RightMemRead)
	gateRig.grant(t, gateUser, "Pro", RightMemWrite)
	body := `{"from":"","to":"Pro"}`
	for _, path := range []string{"/v1/homes/sync-plans", "/v1/homes/syncs"} {
		if recorder := gateRig.do("POST", path, &gateUser, "", body); recorder.Code != 403 {
			t.Errorf("%s sans mem_sync sur la destination : %d", path, recorder.Code)
		}
	}
	gateRig.grant(t, gateUser, "Pro", RightMemSync, RightMemRead)
	if recorder := gateRig.do("POST", "/v1/homes/sync-plans", &gateUser, "", body); !reached(recorder) {
		t.Fatalf("synchro permise refusée : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := gateRig.do("POST", "/v1/homes/sync-plans", &gateUser, "", `{"from":"Pro","to":""}`); recorder.Code != 403 {
		t.Errorf("sens inverse sans mem_read sur la source ni mem_write sur la destination : %d", recorder.Code)
	}
	if err := gateRig.s.Revoke(gateUser.id, mainHomeID, RightMemRead); err != nil {
		t.Fatal(err)
	}
	if recorder := gateRig.do("POST", "/v1/homes/syncs", &gateUser, "", body); recorder.Code != 403 {
		t.Errorf("source sans mem_read : %d", recorder.Code)
	}
	if recorder := gateRig.do("POST", "/v1/homes/sync-plans", &gateUser, "", `{"from":"","to":"absent"}`); recorder.Code != 403 {
		t.Errorf("home inconnu : %d", recorder.Code)
	}
}

func TestGateRightsMatrix(t *testing.T) {
	gateRig := newGateRig(t)
	localGateUser := gateRig.user(t, "lecteur", false)
	gateRig.grant(t, localGateUser, "", RightMemRead)
	anotherGateUser := gateRig.user(t, "compta", false)
	gateRig.grant(t, anotherGateUser, "", RightUsageRead)
	extraGateUser := gateRig.user(t, "admin", true)
	wr := gateRig.user(t, "redacteur", false)
	gateRig.grant(t, wr, "", RightMemWrite)
	cw := gateRig.user(t, "reglages", false)
	gateRig.grant(t, cw, "", RightConfigWrite)
	sr := gateRig.user(t, "historien", false)
	gateRig.grant(t, sr, "", RightSessionsRead)
	cases := []struct {
		u      *gateUser
		m, p   string
		reach  bool
		status int
	}{
		{&localGateUser, "GET", "/v1/memory/files", true, 200},
		{&localGateUser, "PUT", "/v1/memory/files/x", false, 403},
		{&localGateUser, "POST", "/v1/sessions/x/chat", false, 403},
		{&localGateUser, "PUT", "/v1/homes", false, 403},
		{&anotherGateUser, "GET", "/v1/account", true, 200},
		{&anotherGateUser, "GET", "/v1/sessions/x/conversation", false, 403},
		{&anotherGateUser, "GET", "/v1/info", false, 403},
		{&extraGateUser, "GET", "/v1/memory/files", false, 403}, // pas de super-droit implicite
		{&extraGateUser, "PUT", "/v1/homes", true, 200},
		{nil, "GET", "/v1/memory/files", false, 401},
		{nil, "PUT", "/v1/homes", false, 401},
		{nil, "GET", "/", true, 200},
		{nil, "GET", "/assets/x.js", true, 200},
		{nil, "POST", "/v1/auth/sessions", true, 200},
		{&localGateUser, "GET", "/v1/homes", true, 200},
		{&localGateUser, "GET", "/v1/sessions", false, 403},
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
		{&wr, "GET", "/v1/backup/export?cats=skills", false, 403},
		{&wr, "POST", "/v1/backup/imports", false, 403},
		{&wr, "POST", "/v1/backup/imports/x/applications", false, 403},
		{&wr, "DELETE", "/v1/backup/imports/x", false, 403},
		{&cw, "GET", "/v1/backup/export?cats=skills", true, 200},
		{&wr, "POST", "/v1/snippets/drafts", true, 200},
		{&localGateUser, "GET", "/v1/snippets", true, 200},
		{&localGateUser, "GET", "/v1/config/permissions/suggestions", false, 403},
		{&cw, "PUT", "/v1/snippets/u%2Fa.md", true, 200},
		{&sr, "GET", "/v1/config/permissions/suggestions", true, 200},
	}
	for _, record := range cases {
		recorder := gateRig.do(record.m, record.p, record.u, "", "{}")
		if reached(recorder) != record.reach || recorder.Code != record.status {
			t.Errorf("%s %s : %d %s", record.m, record.p, recorder.Code, recorder.Body)
		}
	}

	// Session sans second facteur : refusée hors /auth/*.
	id, _ := gateRig.s.AddUser("partiel", testPw, false)
	gateRig.s.Grant(id, mainHomeID, RightMemRead)
	content, _ := gateRig.s.NewSession(id, "127.0.0.1", "test")
	principal, _ := gateRig.s.Session(content)
	if recorder := gateRig.do("GET", "/v1/memory/files", &gateUser{id, content, principal.CSRF}, "", ""); recorder.Code != 401 {
		t.Errorf("session partielle : %d", recorder.Code)
	}
	// Révocation immédiate : aucun cache.
	gateRig.s.Revoke(localGateUser.id, mainHomeID, RightMemRead)
	if recorder := gateRig.do("GET", "/v1/memory/files", &localGateUser, "", ""); recorder.Code != 403 {
		t.Errorf("droit révoqué encore accepté : %d", recorder.Code)
	}
}

func TestGateCSRF(t *testing.T) {
	gateRig := newGateRig(t)
	gateUser := gateRig.user(t, "alice", false)
	gateRig.grant(t, gateUser, "", RightMemWrite)
	for name, tok := range map[string]string{"absent": "", "faux": strings.Repeat("0", 64)} {
		request := gateRig.req("PUT", "/v1/memory/files/x", &gateUser, "", "{}")
		request.Header.Set(csrfHeader, tok)
		recorder := httptest.NewRecorder()
		gateRig.h.ServeHTTP(recorder, request)
		if recorder.Code != 403 || reached(recorder) {
			t.Errorf("CSRF %s : %d", name, recorder.Code)
		}
	}
	if recorder := gateRig.do("PUT", "/v1/memory/files/x", &gateUser, "", "{}"); !reached(recorder) {
		t.Fatalf("CSRF valide refusé : %d", recorder.Code)
	}

	bearer := func(message, path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(message, path, strings.NewReader("{}"))
		request.Header.Set("Authorization", "Bearer x")
		recorder := httptest.NewRecorder()
		gateRig.h.ServeHTTP(recorder, request)
		return recorder
	}
	if recorder := bearer("PUT", "/v1/memory/files/x"); recorder.Code != 401 {
		t.Fatalf("Bearer sans jeton branché : %d", recorder.Code)
	}
	old := tokenPrincipal
	t.Cleanup(func() { tokenPrincipal = old })
	tokenPrincipal = func(*http.Request) *Principal { return &Principal{Token: true} }
	if recorder := bearer("PUT", "/v1/memory/files/x"); !reached(recorder) {
		t.Errorf("Bearer : CSRF exigé à tort : %d", recorder.Code)
	}
	if recorder := bearer("PUT", "/v1/homes"); recorder.Code != 403 {
		t.Errorf("Bearer sur une route admin : %d", recorder.Code)
	}
}

// En mode comptes, un Bearer faux use les essais de l'adresse comme en mode ouvert ; le bon jeton passe encore.
func TestGateThrottlesWrongBearer(t *testing.T) {
	resetTokenTries()
	t.Cleanup(resetTokenTries)
	gateRig := newGateRig(t)
	do := func(tok string) int {
		request := httptest.NewRequest("GET", "/v1/memory/files", nil)
		request.RemoteAddr = "192.0.2.9:1234"
		request.Header.Set("Authorization", "Bearer "+tok)
		recorder := httptest.NewRecorder()
		gateRig.h.ServeHTTP(recorder, request)
		return recorder.Code
	}
	for index := 0; index < tokenMaxTries+3; index++ {
		if counter := do("faux"); index < tokenMaxTries && counter != 401 || index >= tokenMaxTries && counter != 429 {
			t.Fatalf("essai %d : %d", index, counter)
		}
	}
	old := tokenPrincipal
	t.Cleanup(func() { tokenPrincipal = old })
	tokenPrincipal = func(*http.Request) *Principal { return &Principal{Token: true} }
	if counter := do("bon"); counter == 401 || counter == 429 {
		t.Errorf("bon jeton après l'épuisement : %d", counter)
	}
}

func TestGateTamperedRights(t *testing.T) {
	gateRig := newGateRig(t)
	gateUser := gateRig.user(t, "alice", false)
	gateRig.grant(t, gateUser, "", RightMemRead)
	gateRig.s.db.Exec(`UPDATE home_acl SET exec=1 WHERE user_id=?`, gateUser.id)
	recorder := gateRig.do("GET", "/v1/memory/files", &gateUser, "", "")
	if recorder.Code != 403 || reached(recorder) || !strings.Contains(recorder.Body.String(), L("srv.auth.tamper")) {
		t.Fatalf("ligne altérée : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := gateRig.do("POST", "/v1/sessions/x/chat", &gateUser, "", "{}"); recorder.Code != 403 {
		t.Fatalf("droit ajouté par SQL accepté : %d", recorder.Code)
	}
}

func TestGatePassiveNoTouch(t *testing.T) {
	gateRig := newGateRig(t)
	gateUser := gateRig.user(t, "alice", false)
	gateRig.grant(t, gateUser, "", RightSessionsRead)
	seen := func() (count int64) {
		gateRig.s.db.QueryRow(`SELECT expires_at FROM sessions WHERE user_id=?`, gateUser.id).Scan(&count)
		return
	}
	before := seen()
	old := authNow
	t.Cleanup(func() { authNow = old })
	authNow = func() time.Time { return old().Add(5 * time.Minute) }
	if recorder := gateRig.do("GET", "/v1/state/stamp", &gateUser, "", ""); !reached(recorder) || seen() != before {
		t.Fatalf("sondage compté comme activité : %d", recorder.Code)
	}
	for _, path := range []string{"/v1/state", "/v1/sessions/x/conversation", "/v1/sessions/x/screen"} {
		if recorder := gateRig.do("GET", path, &gateUser, "", ""); !reached(recorder) || seen() != before {
			t.Fatalf("%s compté comme activité : %d", path, recorder.Code)
		}
	}
	if recorder := gateRig.do("GET", "/v1/info", &gateUser, "", ""); !reached(recorder) || seen() == before {
		t.Fatalf("activité non comptée : %d", recorder.Code)
	}
}

func TestHomeIDStable(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	argument, body := t.TempDir(), t.TempDir()
	old, _ := json.Marshal(map[string]any{"homes": []map[string]string{{"name": "A", "dir": argument}, {"name": "main", "dir": body}}})
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
	if err := saveHomes([]Home{{Name: "A", Dir: argument}, {Name: "Renommé", Dir: body, ID: hs[1].ID}, {Name: "C", Dir: body, ID: "deadbeefdeadbeef"}}); err != nil {
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
	if err := saveHomes([]Home{{Name: "A", Dir: argument, ID: hs[0].ID}}); err != nil {
		t.Fatal(err)
	}
	if loadHomes()[0].ID == hs[0].ID {
		t.Fatal("identifiant réattribué")
	}
}

func TestHomesListFiltered(t *testing.T) {
	authStore := testStore(t)
	use(t.TempDir())
	addHomes(t, "A", "B")
	mux := http.NewServeMux()
	routesHomes(mux)
	handler := authGate(mux, authStore, mux)
	localGateRig := &gateRig{s: authStore, h: handler}
	gateUser := localGateRig.user(t, "alice", false)
	localGateRig.grant(t, gateUser, "A", RightMemRead)
	var out struct {
		Main, MainID string
		Homes        []Home
	}
	list := func(handler http.Handler) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, localGateRig.req("GET", "/v1/homes", &gateUser, "", ""))
		out.Homes = nil
		var raw map[string]json.RawMessage
		json.Unmarshal(recorder.Body.Bytes(), &raw)
		json.Unmarshal(raw["homes"], &out.Homes)
		json.Unmarshal(raw["main"], &out.Main)
		json.Unmarshal(raw["mainId"], &out.MainID)
	}
	list(handler)
	if len(out.Homes) != 1 || out.Homes[0].Name != "A" || out.Homes[0].ID == "" || out.Main != "" || out.MainID != mainHomeID {
		t.Fatalf("liste filtrée : %+v", out)
	}
	list(mux)
	if len(out.Homes) != 2 || out.Main == "" {
		t.Fatalf("sans garde, tout est listé : %+v", out)
	}
}

func TestGateSyncApplyConfigItems(t *testing.T) {
	gateRig := newGateRig(t)
	addHomes(t, "Pro")
	gateUser := gateRig.user(t, "bob", false)
	gateRig.grant(t, gateUser, "", RightMemSync, RightMemRead)
	gateRig.grant(t, gateUser, "Pro", RightMemSync, RightMemWrite, RightMemRead)
	apply := func(ids ...string) int {
		var picks []map[string]string
		for _, id := range ids {
			picks = append(picks, map[string]string{"id": id, "mode": "replace"})
		}
		data, _ := json.Marshal(map[string]any{"from": "", "to": "Pro", "picks": picks})
		return gateRig.do("POST", "/v1/homes/syncs", &gateUser, "", string(data)).Code
	}
	for _, ok := range [][]string{{"file:CLAUDE.md"}, {"file:rules/a.md", "file:rules/sous/b.md"}, nil} {
		if counter := apply(ok...); counter != 200 {
			t.Errorf("%v sans config_write : %d", ok, counter)
		}
	}
	for _, bad := range []string{"set:hooks", "perm:allow", "skill:x", "file:commands/x.md", "file:rules/../commands/x.md",
		"file:rules/a.sh", "file:rules\\a.md", "inconnu", ""} {
		if counter := apply("file:CLAUDE.md", bad); counter != 403 {
			t.Errorf("%q sans config_write : %d", bad, counter)
		}
	}
	gateRig.grant(t, gateUser, "Pro", RightConfigWrite)
	if counter := apply("set:hooks"); counter != 200 {
		t.Errorf("set:hooks avec config_write : %d", counter)
	}
	// Le plan exige mem_read sur la destination (il en renvoie des aperçus).
	gateRig.s.SetRights(gateUser.id, mustHomeID(t, "Pro"), nil, []Right{RightMemRead, RightMemWrite, RightConfigWrite})
	if recorder := gateRig.do("POST", "/v1/homes/sync-plans", &gateUser, "", `{"from":"","to":"Pro"}`); recorder.Code != 403 {
		t.Errorf("plan sans mem_read sur la destination : %d", recorder.Code)
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
	for _, name := range []string{"main", "Main", " MAIN "} {
		if err := saveHomes([]Home{{Name: name, Dir: t.TempDir()}}); err == nil || !isMsg(err, "srv.homes.nomReserve") {
			t.Errorf("%q accepté : %v", name, err)
		}
	}
	mux := http.NewServeMux()
	routesHomes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("PUT", "/v1/homes", strings.NewReader(`{"homes":[{"name":"main","dir":"`+t.TempDir()+`"}]}`)))
	if recorder.Code != 400 {
		t.Fatalf("PUT /v1/homes : %d %s", recorder.Code, recorder.Body)
	}
}

func TestUserCLIGrantHomeName(t *testing.T) {
	authStore := testStore(t)
	use(t.TempDir())
	addHomes(t, "Pro")
	id, _ := authStore.AddUser("alice", testPw, false)
	authStore.db.Close()
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
	lookup, _ := s2.Rights(id, mainHomeID)
	localLookup, _ := s2.Rights(id, pro)
	if !lookup[RightMemRead] || !localLookup[RightMemRead] || !localLookup[RightMemWrite] {
		t.Fatalf("main %v, Pro %v", lookup, localLookup)
	}
	if lookup, _ := s2.Rights(id, "Pro"); anyRight(lookup) {
		t.Fatal("droit écrit sous le nom au lieu de l'identifiant")
	}
}

// Restaurer un snippet ou un skill depuis la corbeille recrée du contenu exécutable : config_write exigé.
func TestGateTrashRestoreNeedsConfig(t *testing.T) {
	gateRig := newGateRig(t)
	ids := map[string]string{}
	for _, key := range []string{"s/u/x.md", "k/outil/SKILL.md", "m/projet/note.md", "z/inconnu"} {
		if err := saveTrash(key, "/p", "supprimé", "texte"); err != nil {
			t.Fatal(err)
		}
		for _, it := range trashList() {
			if it.Key == key {
				ids[key] = it.ID
			}
		}
	}
	mw := gateRig.user(t, "memoire", false)
	gateRig.grant(t, mw, "", RightMemWrite)
	cw := gateRig.user(t, "reglages", false)
	gateRig.grant(t, cw, "", RightMemWrite, RightConfigWrite)
	for _, record := range []struct {
		u    *gateUser
		key  string
		code int
	}{
		{&mw, "s/u/x.md", 403}, {&mw, "k/outil/SKILL.md", 403}, {&mw, "z/inconnu", 403}, {&mw, "m/projet/note.md", 200},
		{&cw, "s/u/x.md", 200}, {&cw, "k/outil/SKILL.md", 200},
	} {
		if recorder := gateRig.do("PATCH", "/v1/trash/memory/"+ids[record.key], record.u, "", `{"trashed":false}`); recorder.Code != record.code {
			t.Errorf("%s : %d", record.key, recorder.Code)
		}
	}
	if recorder := gateRig.do("PATCH", "/v1/trash/memory/..%2Fx", &mw, "", `{"trashed":false}`); recorder.Code != 403 {
		t.Errorf("identifiant hors corbeille : %d", recorder.Code)
	}
}

// POST /v1/memory/merges avec launch ouvre une session claude aux arguments de l'appelant : exec exigé. Sans launch, mem_write suffit.
func TestGateMemoryMergeLaunchNeedsExec(t *testing.T) {
	gateRig := newGateRig(t)
	mw := gateRig.user(t, "memoire", false)
	gateRig.grant(t, mw, "", RightMemWrite)
	ex := gateRig.user(t, "lanceur", false)
	gateRig.grant(t, ex, "", RightMemWrite, RightExec)
	for _, record := range []struct {
		u    *gateUser
		body string
		code int
	}{
		{&mw, `{"Launch":true,"Args":"--dangerously-skip-permissions"}`, 403},
		{&mw, `{"Launch":false}`, 200},
		{&ex, `{"Launch":true}`, 200},
	} {
		if recorder := gateRig.do("POST", "/v1/memory/merges", record.u, "", record.body); recorder.Code != record.code {
			t.Errorf("%s : %d", record.body, recorder.Code)
		}
	}
}

// PUT /v1/vgroups remplace aussi les dossiers enregistrés, racines de l'IDE : en ajouter demande exec, comme POST /dirs.
func TestGateVgroupsNewDirNeedsExec(t *testing.T) {
	gateRig := newGateRig(t)
	known := t.TempDir()
	if err := saveStore(Store{Groups: []VGroup{}, Favs: []string{}, Projects: []Project{}, Dirs: []string{known}}); err != nil {
		t.Fatal(err)
	}
	mw := gateRig.user(t, "memoire", false)
	gateRig.grant(t, mw, "", RightMemWrite)
	ex := gateRig.user(t, "lanceur", false)
	gateRig.grant(t, ex, "", RightMemWrite, RightExec)
	keep := `{"groups":[],"favs":[],"dirs":["` + known + `"]}`
	add := `{"groups":[],"favs":[],"dirs":["` + known + `","/etc"]}`
	addProj := `{"groups":[],"favs":[],"projects":[{"name":"p","dirs":["/etc"]}]}`
	for _, record := range []struct {
		u    *gateUser
		body string
		code int
	}{
		{&mw, keep, 200}, {&mw, add, 403}, {&mw, addProj, 403}, {&ex, add, 200},
	} {
		if recorder := gateRig.do("PUT", "/v1/vgroups", record.u, "", record.body); recorder.Code != record.code {
			t.Errorf("%s : %d", record.body, recorder.Code)
		}
	}
}

// fs_write écrit les fichiers du projet, mais ceux que Claude Code ou git exécutent (.claude/, .git/, .mcp.json…) exigent config_write.
func TestFsWriteCodePathsNeedConfigWrite(t *testing.T) {
	authStore := testStore(t)
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	if err := saveStore(Store{Groups: []VGroup{}, Favs: []string{}, Projects: []Project{}, Dirs: []string{proj}}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	mux := http.NewServeMux()
	routesIDEWrite(mux)
	localGateRig := &gateRig{s: authStore, h: authGate(mux, authStore, mux), cfg: cfg}
	fw := localGateRig.user(t, "ecrit", false)
	localGateRig.grant(t, fw, "", RightFsRead, RightFsWrite)
	cw := localGateRig.user(t, "reglages", false)
	localGateRig.grant(t, cw, "", RightFsRead, RightFsWrite, RightConfigWrite)
	save := func(path string) string {
		return "/v1/fs/entries/content?dir=" + url.QueryEscape(proj) + "&path=" + url.QueryEscape(path)
	}
	const saveBody = `{"content":"x","force":true}`
	for _, record := range []struct {
		u    *gateUser
		path string
		code int
	}{
		{&fw, "a.txt", 200}, {&fw, ".claude/settings.json", 403}, {&fw, ".mcp.json", 403}, {&fw, "sub/.git/config", 403},
		{&cw, ".mcp.json", 200}, {&cw, ".claude/settings.json", 200},
	} {
		if recorder := localGateRig.do("PUT", save(record.path), record.u, "", saveBody); recorder.Code != record.code {
			t.Errorf("%s : %d %s", record.path, recorder.Code, recorder.Body.String())
		}
	}
	rn := `{"from":"a.txt","to":".git/config"}`
	if recorder := localGateRig.do("PATCH", "/v1/fs/entries?dir="+url.QueryEscape(proj), &fw, "", rn); recorder.Code != 403 {
		t.Errorf("renommage vers .git : %d", recorder.Code)
	}
}

// Les brouillons de mémoire et de skill lisent des fichiers côté serveur : mem_read est exigé en plus du droit d'écriture.
func TestGateDraftsNeedMemRead(t *testing.T) {
	gateRig := newGateRig(t)
	localGateUser := gateRig.user(t, "ecrivain", false)
	gateRig.grant(t, localGateUser, "", RightMemWrite, RightConfigWrite)
	anotherGateUser := gateRig.user(t, "lecteur", false)
	gateRig.grant(t, anotherGateUser, "", RightMemWrite, RightConfigWrite, RightMemRead)
	for _, record := range []struct {
		u    *gateUser
		path string
		code int
	}{
		{&localGateUser, "/v1/memory/drafts", 403}, {&localGateUser, "/v1/skills/drafts", 403},
		{&anotherGateUser, "/v1/memory/drafts", 200}, {&anotherGateUser, "/v1/skills/drafts", 200},
	} {
		if got := gateRig.do("POST", record.path, record.u, "", `{}`).Code; got != record.code {
			t.Errorf("%s : %d, attendu %d", record.path, got, record.code)
		}
	}
}

func TestSetRightsWriteNeedsRead(t *testing.T) {
	gateRig := newGateRig(t)
	gateUser := gateRig.user(t, "ecrivain", false)
	id, _ := homeIDFor("")
	for _, right := range []Right{RightMemWrite, RightConfigWrite} {
		if err := gateRig.s.Grant(gateUser.id, id, right); err == nil {
			t.Fatalf("%s sans mem_read accordé", right)
		}
		if err := gateRig.s.SetRights(gateUser.id, id, []Right{right, RightMemRead}, nil); err != nil {
			t.Fatalf("%s avec mem_read : %v", right, err)
		}
		if err := gateRig.s.Revoke(gateUser.id, id, RightMemRead); err == nil {
			t.Fatalf("mem_read retiré alors que %s reste", right)
		}
		if err := gateRig.s.SetRights(gateUser.id, id, nil, []Right{right, RightMemRead}); err != nil {
			t.Fatalf("retrait groupé : %v", err)
		}
	}
}

func TestChatStopPathRejectsTraversal(t *testing.T) {
	if path, ok := chatStopPath("0123abcd-0123-4abc-8def-0123456789ab"); !ok || path != "/v1/sessions/0123abcd-0123-4abc-8def-0123456789ab/chat" {
		t.Fatalf("valid id refused: %q %v", path, ok)
	}
	request := httptest.NewRequest("POST", "/v1/sessions/..%2F..%2Fv1%2Ffs%2Fentries%3Fdir%3DX%26item%3Dy/chat", nil)
	id := pathSeg(request, 2)
	if id == "" {
		t.Fatal("pathSeg did not decode the segment")
	}
	if path, ok := chatStopPath(id); ok {
		t.Fatalf("malicious id accepted: %q", path)
	}
}
