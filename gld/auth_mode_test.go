package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const modeTok = "0123456789abcdef0123"

// resetAuth remet l'état du mode à zéro sur s (nil : aucune base ouverte) et fige l'horloge ; tick la fait avancer d'une fenêtre.
func resetAuth(t *testing.T, localAuthStore *authStore) (tick func()) {
	t.Helper()
	oSt, oErr, oUsers, oSeen, oAt, oNow, oTP, oFI, oFn := authSt, authErr, authUsers, authSeen, authAt, modeNow, tokenPrincipal, authFI, authStateFn
	t.Cleanup(func() {
		authSt, authErr, authUsers, authSeen, authAt, modeNow, tokenPrincipal, authFI, authStateFn = oSt, oErr, oUsers, oSeen, oAt, oNow, oTP, oFI, oFn
	})
	now := time.Now()
	authSt, authErr, authUsers, authSeen, authAt, authFI = localAuthStore, nil, false, false, time.Time{}, nil
	if localAuthStore != nil {
		authFI, _ = os.Lstat(filepath.Join(os.Getenv("GLD_AUTH_DIR"), "auth.db"))
	}
	modeNow = func() time.Time { return now }
	tokenPrincipal = tokenForChildren(modeTok, false)
	return func() { now = now.Add(modeEvery) }
}

// authWeb : le serveur complet de web(t), avec une base de comptes ouverte et le jeton branché comme dans main.
func authWeb(t *testing.T) (*authStore, http.Handler, func()) {
	t.Helper()
	localAuthStore := testStore(t)
	tick := resetAuth(t, localAuthStore)
	handler, _, _ := web(t)
	return localAuthStore, withTokenOpen(modeTok, handler), tick
}

type modeReq struct {
	method, path, bearer, tokCookie, origin, accept string
}

func (localModeReq modeReq) do(handler http.Handler) *httptest.ResponseRecorder {
	request := httptest.NewRequest(localModeReq.method, localModeReq.path, strings.NewReader("{}"))
	request.Host = testHost
	if localModeReq.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+localModeReq.bearer)
	}
	if localModeReq.tokCookie != "" {
		request.AddCookie(&http.Cookie{Name: tokenCookie, Value: localModeReq.tokCookie})
	}
	if localModeReq.origin != "" {
		request.Header.Set("Origin", localModeReq.origin)
	}
	if localModeReq.accept != "" {
		request.Header.Set("Accept", localModeReq.accept)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// Sans compte en base, rien ne change : mêmes réponses sans base et avec une base vide, /auth/* exclu.
func TestOpenModeUnchanged(t *testing.T) {
	resetAuth(t, nil)
	handler, _, _ := web(t)
	srv := withTokenOpen(modeTok, handler)
	reqs := []modeReq{
		{method: "GET", path: "/v1/state"},
		{method: "GET", path: "/v1/state?token=" + modeTok},
		{method: "GET", path: "/memoire?token=" + modeTok + "&x=1"},
		{method: "GET", path: "/v1/state?token=faux"},
		{method: "GET", path: "/v1/state", tokCookie: modeTok},
		{method: "GET", path: "/v1/state", tokCookie: "faux"},
		{method: "GET", path: "/v1/state", bearer: modeTok},
		{method: "GET", path: "/v1/state", bearer: "faux"},
		{method: "GET", path: "/", tokCookie: modeTok, accept: "text/html"},
		{method: "GET", path: "/v1/homes", tokCookie: modeTok},
		{method: "GET", path: "/inexistant", tokCookie: modeTok},
		{method: "PUT", path: "/v1/memory/files/x", tokCookie: modeTok, origin: "https://ailleurs.example"},
		{method: "PUT", path: "/v1/homes", bearer: modeTok, origin: "http://" + testHost},
	}
	var before []*httptest.ResponseRecorder
	for _, modeReq := range reqs {
		before = append(before, modeReq.do(srv))
	}
	if before[0].Code != 401 || before[1].Code != 303 || before[4].Code != 200 || before[6].Code != 200 {
		t.Fatalf("withToken : %d %d %d %d", before[0].Code, before[1].Code, before[4].Code, before[6].Code)
	}
	resetAuth(t, testStore(t))
	if _, on, _ := authState(); on {
		t.Fatal("mode multi-utilisateur sans compte")
	}
	for index, modeReq := range reqs {
		recorder, localRecorder := before[index], modeReq.do(srv)
		if recorder.Code != localRecorder.Code || recorder.Body.String() != localRecorder.Body.String() ||
			recorder.Header().Get("Set-Cookie") != localRecorder.Header().Get("Set-Cookie") || recorder.Header().Get("Location") != localRecorder.Header().Get("Location") {
			t.Errorf("%s %s : sans base %d %q, base vide %d %q", modeReq.method, modeReq.path, recorder.Code, cut(recorder.Body.String(), 120), localRecorder.Code, cut(localRecorder.Body.String(), 120))
		}
	}
}

// Sans auth.db : ni dossier, ni base, ni clé, ni coffre-fort, et /auth/* n'existe pas.
func TestNoAuthDBNoSideEffects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gld")
	t.Setenv("GLD_AUTH_DIR", dir)
	t.Setenv("GLD_AUTH_KEY", "")
	callback, st := keyringGet, keyringSet
	t.Cleanup(func() { keyringGet, keyringSet = callback, st })
	keyringGet = func() (string, error) { t.Error("coffre-fort lu"); return "", errors.New("non") }
	keyringSet = func(string) error { t.Error("coffre-fort écrit"); return errors.New("non") }
	tick := resetAuth(t, nil)
	handler, _, _ := web(t)
	srv := withTokenOpen(modeTok, handler)
	for range 3 {
		tick()
		if recorder := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv); recorder.Code != 200 {
			t.Fatalf("mode ouvert : %d", recorder.Code)
		}
		if recorder := (modeReq{method: "GET", path: "/v1/auth/me", tokCookie: modeTok}).do(srv); recorder.Code != 404 {
			t.Fatalf("/v1/auth/me sans base : %d", recorder.Code)
		}
	}
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dossier des comptes créé : %v", err)
	}

	// La base et un compte apparaissent pendant que le serveur tourne : bascule au plus tard une fenêtre plus tard.
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("cd", 32))
	authStore, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	defer authStore.db.Close()
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	localModeReq := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
	if recorder := localModeReq.do(srv); recorder.Code != 200 {
		t.Fatalf("dans la fenêtre du cache : %d", recorder.Code)
	}
	tick()
	if recorder := localModeReq.do(srv); recorder.Code != 401 {
		t.Fatalf("après la fenêtre : %d", recorder.Code)
	}
	if recorder := (modeReq{method: "GET", path: "/v1/auth/me"}).do(srv); recorder.Code != 401 {
		t.Fatalf("/v1/auth/me monté : %d", recorder.Code)
	}
}

// Une base avec des comptes ne rend jamais le mode ouvert sur une erreur : clé fausse, base fermée, fichier supprimé.
func TestAuthFailClosed(t *testing.T) {
	t.Run("TestStartupRefusesWrongKey", func(t *testing.T) {
		authStore := testStore(t)
		authStore.AddUser("alice", testPw, true)
		authStore.db.Close()
		t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
		tick := resetAuth(t, nil)
		handler, _, _ := web(t)
		srv := withTokenOpen(modeTok, handler)
		if _, _, err := authState(); err == nil {
			t.Fatal("démarrage accepté")
		}
		tick()
		recorder := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv)
		if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), jsonEsc(L("srv.auth.baseInutilisable"))) {
			t.Fatalf("service : %d %s", recorder.Code, recorder.Body)
		}
	})
	t.Run("base fermée", func(t *testing.T) {
		authStore, srv, tick := authWeb(t)
		authStore.AddUser("alice", testPw, true)
		localModeReq := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
		if recorder := localModeReq.do(srv); recorder.Code != 401 {
			t.Fatalf("mode actif : %d", recorder.Code)
		}
		authStore.db.Close()
		tick()
		if recorder := localModeReq.do(srv); recorder.Code != 503 {
			t.Fatalf("base fermée : %d", recorder.Code)
		}
	})
	t.Run("fichier supprimé", func(t *testing.T) {
		authStore, srv, tick := authWeb(t)
		authStore.AddUser("alice", testPw, true)
		localModeReq := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
		if recorder := localModeReq.do(srv); recorder.Code != 401 {
			t.Fatalf("mode actif : %d", recorder.Code)
		}
		if err := os.Remove(filepath.Join(os.Getenv("GLD_AUTH_DIR"), "auth.db")); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			tick()
			if recorder := localModeReq.do(srv); recorder.Code != 503 {
				t.Fatalf("base supprimée : %d", recorder.Code)
			}
		}
	})
	t.Run("table vidée", func(t *testing.T) {
		authStore, srv, tick := authWeb(t)
		authStore.AddUser("alice", testPw, true)
		localModeReq := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
		if recorder := localModeReq.do(srv); recorder.Code != 401 {
			t.Fatalf("mode actif : %d", recorder.Code)
		}
		if _, err := authStore.db.Exec(`DELETE FROM users`); err != nil {
			t.Fatal(err)
		}
		// Le marqueur des comptes créés reste : des comptes supprimés ferment le service, jamais le mode ouvert.
		for range 2 {
			tick()
			if recorder := localModeReq.do(srv); recorder.Code != 503 {
				t.Fatalf("plus aucun compte : %d", recorder.Code)
			}
		}
	})
	t.Run("marqueur altéré", func(t *testing.T) {
		authStore, srv, tick := authWeb(t)
		authStore.AddUser("alice", testPw, true)
		authStore.db.Exec(`UPDATE meta SET value=x'00' WHERE key=?`, accountsKey)
		tick()
		if recorder := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv); recorder.Code != 503 {
			t.Fatalf("marqueur altéré : %d", recorder.Code)
		}
		authStore.db.Exec(`DELETE FROM users`)
		tick()
		if recorder := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv); recorder.Code != 503 {
			t.Fatalf("marqueur altéré sans compte : %d", recorder.Code)
		}
	})
}

func jsonEsc(text string) string {
	data, _ := json.Marshal(text)
	return strings.Trim(string(data), `"`)
}

func TestTokenIgnoredInURLWhenAuth(t *testing.T) {
	authStore, handler, _ := authWeb(t)
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	for _, localModeReq := range []modeReq{
		{method: "GET", path: "/v1/state?token=" + modeTok},
		{method: "GET", path: "/memoire?token=" + modeTok + "&x=1", accept: "application/json"},
		{method: "GET", path: "/v1/state", tokCookie: modeTok},
	} {
		recorder := localModeReq.do(handler)
		if localModeReq.path[1] == 'm' { // page publique : servie, mais sans cookie de jeton ni redirection
			if recorder.Code != 200 || recorder.Header().Get("Set-Cookie") != "" {
				t.Errorf("%s : %d %q", localModeReq.path, recorder.Code, recorder.Header().Get("Set-Cookie"))
			}
			continue
		}
		if recorder.Code != 401 {
			t.Errorf("%s cookie=%q : %d", localModeReq.path, localModeReq.tokCookie, recorder.Code)
		}
	}
}

func TestBearerStillWorks(t *testing.T) {
	authStore, handler, _ := authWeb(t)
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	if recorder := (modeReq{method: "GET", path: "/v1/state", bearer: modeTok}).do(handler); recorder.Code != 200 {
		t.Fatalf("Bearer sur une route de home : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := (modeReq{method: "GET", path: "/v1/state", bearer: "faux-jeton-de-vingt-car"}).do(handler); recorder.Code != 401 {
		t.Fatalf("mauvais Bearer : %d", recorder.Code)
	}
	if recorder := (modeReq{method: "GET", path: "/v1/auth/me", bearer: modeTok}).do(handler); recorder.Code != 401 {
		t.Fatalf("/v1/auth/me avec Bearer : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := (modeReq{method: "PUT", path: "/v1/homes", bearer: modeTok}).do(handler); recorder.Code != 403 {
		t.Fatalf("route admin avec Bearer : %d %s", recorder.Code, recorder.Body)
	}
}

// Un jeton généré (hors localhost sans GLD_WEB_TOKEN) ne vaut pas en Bearer quand les comptes existent, ni ne s'imprime.
func TestGeneratedTokenNoBearer(t *testing.T) {
	authStore, handler, _ := authWeb(t)
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	tokenPrincipal = tokenForChildren(modeTok, true)
	if recorder := (modeReq{method: "GET", path: "/v1/state", bearer: modeTok}).do(handler); recorder.Code != 401 {
		t.Fatalf("Bearer d'un jeton généré : %d %s", recorder.Code, recorder.Body)
	}
	for _, rawURL := range urlsFor("http", "192.0.2.1", "8799", "") {
		if strings.Contains(rawURL, "token") {
			t.Fatalf("adresse sans jeton : %q", rawURL)
		}
	}
}

func TestChildHasNoGate(t *testing.T) {
	authStore := testStore(t)
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	resetAuth(t, nil)
	t.Cleanup(func() { childToken = "" })
	childToken = "secret-enfant"
	if st, on, err := authState(); st != nil || on || err != nil {
		t.Fatalf("un enfant n'ouvre pas la base : %v %v %v", st, on, err)
	}
	handler, _, _ := web(t)
	request := httptest.NewRequest("GET", "/v1/state", nil)
	request.Host = testHost
	request.Header.Set(childHeader, "secret-enfant")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("enfant : %d %s", recorder.Code, recorder.Body)
	}
}

func TestAuthModeSwitchPerRequest(t *testing.T) {
	authStore, handler, tick := authWeb(t)
	localModeReq := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
	if recorder := localModeReq.do(handler); recorder.Code != 200 {
		t.Fatalf("mode ouvert : %d", recorder.Code)
	}
	if _, err := authStore.AddUser("alice", testPw, true); err != nil { // comme `gld user add` pendant que le serveur tourne
		t.Fatal(err)
	}
	tick()
	if recorder := localModeReq.do(handler); recorder.Code != 401 {
		t.Fatalf("après le premier compte : %d", recorder.Code)
	}
}

func TestUnauthShellServed(t *testing.T) {
	authStore, handler, _ := authWeb(t)
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	for _, localPath := range []string{"/", "/memoire", "/configuration/x"} {
		recorder := (modeReq{method: "GET", path: localPath, accept: "text/html"}).do(handler)
		if recorder.Code != 200 || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s : %d %q", localPath, recorder.Code, recorder.Header().Get("Content-Type"))
		}
	}
	localRecorder := (modeReq{method: "GET", path: "/v1/state", accept: "text/html"}).do(handler)
	if localRecorder.Code != 401 || !strings.HasPrefix(localRecorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API sans session : %d %q", localRecorder.Code, localRecorder.Header().Get("Content-Type"))
	}
}

func TestChildProxyStripsAuthorization(t *testing.T) {
	var got http.Header
	back := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { got = request.Header.Clone() }))
	defer back.Close()
	uRL, _ := url.Parse(back.URL)
	localRequest := httptest.NewRequest("GET", "/v1/state", nil)
	localRequest.Header.Set("Authorization", "Bearer "+modeTok)
	localRequest.Header.Set("Cookie", sessCookie+"=x; "+tokenCookie+"=y")
	localRequest.Header.Set("Origin", "http://"+testHost)
	localRequest.Header.Set("Sec-Fetch-Site", "same-origin")
	childProxy(uRL, "jeton-enfant").ServeHTTP(httptest.NewRecorder(), localRequest)
	if got == nil {
		t.Fatal("requête non relayée")
	}
	for _, key := range []string{"Authorization", "Cookie", "Origin", "Sec-Fetch-Site"} {
		if got.Get(key) != "" {
			t.Errorf("%s relayé à l'enfant", key)
		}
	}
	if got.Get(childHeader) != "jeton-enfant" {
		t.Error("jeton de la passerelle absent")
	}
}

// wsHold : un WebSocket qui lit jusqu'à la fin de son contexte, comme /term-ws.
func wsHold(skipOrigin bool) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		conn, err := websocket.Accept(responseWriter, request, &websocket.AcceptOptions{InsecureSkipVerify: skipOrigin})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			if _, _, err := conn.Read(request.Context()); err != nil {
				return
			}
		}
	}
}

// wsCut ouvre le flux, le vérifie, appelle revoke et renvoie le délai avant la fermeture vue par le client.
func wsCut(t *testing.T, srv *httptest.Server, localGateUser gateUser, home string, revoke func()) time.Duration {
	t.Helper()
	hd := http.Header{"Origin": {srv.URL}}
	hd.Add("Cookie", (&http.Cookie{Name: sessCookie, Value: localGateUser.cookie}).String())
	if home != "" {
		hd.Add("Cookie", (&http.Cookie{Name: homeCookie, Value: url.QueryEscape(home)}).String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/terminal/ws", &websocket.DialOptions{HTTPHeader: hd})
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("ouverture : %v (%d)", err, code)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	revoke()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("message reçu au lieu de la fermeture")
	}
	return time.Since(start)
}

func TestWSCutOnRevoke(t *testing.T) {
	t.Run("passerelle", func(t *testing.T) {
		gateRig := newGateRig(t)
		gateUser := gateRig.user(t, "alice", false)
		gateRig.grant(t, gateUser, "", RightExec)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/terminal/ws", wsHold(false))
		srv := httptest.NewServer(authGate(mux, gateRig.s, withAuthWatch(withHomes(mux))))
		defer srv.Close()
		if duration := wsCut(t, srv, gateUser, "", func() { gateRig.s.RevokeUser(gateUser.id) }); duration > 6*time.Second {
			t.Fatalf("coupé en %v", duration)
		}
	})
	t.Run("home enfant", func(t *testing.T) {
		old := authWatchEvery
		t.Cleanup(func() { authWatchEvery = old })
		authWatchEvery = 100 * time.Millisecond
		gateRig := newGateRig(t)
		addHomes(t, "Pro")
		gateUser := gateRig.user(t, "bob", false)
		gateRig.grant(t, gateUser, "Pro", RightExec)
		child := httptest.NewServer(wsHold(true))
		defer child.Close()
		cu, _ := url.Parse(child.URL)
		oldStart := homeStart
		t.Cleanup(func() { homeStart = oldStart; stopHomes() })
		homeStart = func(home Home) (*homeProc, error) {
			return &homeProc{dir: home.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
		}
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/terminal/ws", func(responseWriter http.ResponseWriter, request *http.Request) { t.Error("servi par la passerelle") })
		srv := httptest.NewServer(authGate(mux, gateRig.s, withAuthWatch(withHomes(mux))))
		defer srv.Close()
		pro, _ := homeIDFor("Pro")
		if duration := wsCut(t, srv, gateUser, "Pro", func() { gateRig.s.Revoke(gateUser.id, pro, RightExec) }); duration > 6*time.Second {
			t.Fatalf("coupé en %v", duration)
		}
	})
}

// POST .../chat relayé à un home enfant : la révocation coupe le client et demande l'arrêt du tour à l'enfant.
func TestChildChatStoppedOnRevoke(t *testing.T) {
	old := authWatchEvery
	t.Cleanup(func() { authWatchEvery = old })
	authWatchEvery = 100 * time.Millisecond
	gateRig := newGateRig(t)
	addHomes(t, "Pro")
	gateUser := gateRig.user(t, "bob", false)
	gateRig.grant(t, gateUser, "Pro", RightExec)
	stopped, quit := make(chan string, 1), make(chan struct{})
	cm := http.NewServeMux()
	cm.HandleFunc("POST /v1/sessions/{id}/chat", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Write([]byte("{\"text\":\"début\"}\n"))
		responseWriter.(http.Flusher).Flush()
		select { // un tour d'enfant ne suit pas le contexte de la requête
		case <-time.After(8 * time.Second):
		case <-quit:
		}
	})
	cm.HandleFunc("DELETE /v1/sessions/{id}/chat", func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.Header.Get(childHeader) != "x" {
			t.Error("arrêt envoyé sans le jeton de la passerelle")
		}
		stopped <- request.PathValue("id")
		close(quit)
	})
	child := httptest.NewServer(cm)
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(home Home) (*homeProc, error) {
		return &homeProc{dir: home.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions/{id}/chat", func(responseWriter http.ResponseWriter, request *http.Request) { t.Error("servi par la passerelle") })
	srv := httptest.NewServer(authGate(mux, gateRig.s, withAuthWatch(withHomes(mux))))
	defer srv.Close()

	localRequest, _ := http.NewRequest("POST", srv.URL+"/v1/sessions/"+id1+"/chat", strings.NewReader(`{"Prompt":"x"}`))
	localRequest.AddCookie(&http.Cookie{Name: sessCookie, Value: gateUser.cookie})
	localRequest.AddCookie(&http.Cookie{Name: homeCookie, Value: "Pro"})
	localRequest.Header.Set(csrfHeader, gateUser.csrf)
	resp, err := http.DefaultClient.Do(localRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	if count, err := resp.Body.Read(buf); count == 0 || err != nil {
		t.Fatalf("début du flux : %d %v", count, err)
	}
	pro, _ := homeIDFor("Pro")
	start := time.Now()
	gateRig.s.Revoke(gateUser.id, pro, RightExec)
	select {
	case id := <-stopped:
		if id != id1 {
			t.Fatalf("arrêt demandé pour %q", id)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("arrêt jamais demandé à l'enfant")
	}
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(resp.Body); done <- err }()
	select {
	case <-done:
	case <-time.After(6*time.Second - time.Since(start)):
		t.Fatal("flux du client non coupé")
	}
}

// Le mode est décidé une fois par requête : un état qui change entre le garde du jeton et le handler ne peut ni sauter le
// jeton ni servir la branche ouverte.
func TestModeDecidedOncePerRequest(t *testing.T) {
	localAuthStore, srv, _ := authWeb(t)
	localAuthStore.AddUser("alice", testPw, true)
	for _, first := range []bool{true, false} {
		count := 0
		authStateFn = func() (*authStore, bool, error) {
			count++
			return localAuthStore, first == (count%2 == 1), nil
		}
		recorder := (modeReq{method: "GET", path: "/v1/state"}).do(srv)
		if recorder.Code != 401 || count != 1 {
			t.Fatalf("premier état %v : %d, %d évaluation(s)", first, recorder.Code, count)
		}
	}
}

// Une base remplacée pendant l'exécution n'est plus servie depuis l'ancien fichier : refus, puis réouverture.
func TestAuthDBReplaced(t *testing.T) {
	authStore := testStore(t)
	authStore.AddUser("alice", testPw, true)
	authStore.db.Close()
	dir := os.Getenv("GLD_AUTH_DIR")
	tick := resetAuth(t, nil)
	handler, _, _ := web(t)
	srv := withTokenOpen(modeTok, handler)
	localModeReq := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
	if recorder := localModeReq.do(srv); recorder.Code != 401 {
		t.Fatalf("mode actif : %d", recorder.Code)
	}
	old := authSt

	other := t.TempDir()
	t.Setenv("GLD_AUTH_DIR", other)
	s2, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	s2.AddUser("bob", testPw, true)
	s2.db.Close()
	t.Setenv("GLD_AUTH_DIR", dir)
	if err := os.Rename(filepath.Join(other, "auth.db"), filepath.Join(dir, "auth.db")); err != nil {
		t.Fatal(err)
	}
	tick()
	if recorder := localModeReq.do(srv); recorder.Code != 503 {
		t.Fatalf("base remplacée : %d", recorder.Code)
	}
	tick()
	if recorder := localModeReq.do(srv); recorder.Code != 401 || authSt == nil || authSt == old {
		t.Fatalf("réouverture : %d", recorder.Code)
	}
	if _, err := authSt.UserByName("bob"); err != nil {
		t.Fatalf("nouvelle base non lue : %v", err)
	}
	if err := old.db.Ping(); err == nil {
		t.Fatal("ancienne base restée ouverte")
	}
}

// Un corps lu pour décider (peekJSON) au-delà de la limite : signalé, et le handler le reçoit en entier.
func TestPeekJSONBodyTooBig(t *testing.T) {
	body := `{"ID":"` + id1 + `","Prompt":"` + strings.Repeat("a", 70<<10) + `"}`
	// Corps restitué en entier quand il n'est pas lu jusqu'au bout pour décider.
	rr := httptest.NewRequest("POST", "/", strings.NewReader(body))
	var record struct{ ID string }
	if !errors.Is(peekJSON(rr, &record), errBodyTooBig) {
		t.Fatal("dépassement non signalé")
	}
	if data, _ := io.ReadAll(rr.Body); string(data) != body {
		t.Fatalf("corps restitué : %d octets sur %d", len(data), len(body))
	}
}

// Base présente sans compte : /auth/* répond comme une route inconnue ; le premier compte l'ouvre dans la fenêtre du cache.
func TestAuthRoutesHiddenWithoutUsers(t *testing.T) {
	authStore, srv, tick := authWeb(t)
	me := modeReq{method: "GET", path: "/v1/auth/me", tokCookie: modeTok}
	unknown := modeReq{method: "GET", path: "/inexistant", tokCookie: modeTok}
	recorder, localRecorder := me.do(srv), unknown.do(srv)
	if recorder.Code != 404 || recorder.Body.String() != localRecorder.Body.String() {
		t.Fatalf("/v1/auth/me sans compte : %d %q", recorder.Code, recorder.Body)
	}
	if recorder := (modeReq{method: "POST", path: "/v1/auth/sessions", tokCookie: modeTok}).do(srv); recorder.Code != 404 {
		t.Fatalf("/v1/auth/sessions sans compte : %d", recorder.Code)
	}
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	tick()
	if recorder := (modeReq{method: "GET", path: "/v1/auth/me"}).do(srv); recorder.Code != 401 {
		t.Fatalf("/v1/auth/me après le premier compte : %d", recorder.Code)
	}
}

// Sans cookie ni jeton, toute route non publique répond 401 : le garde ne dépend pas de ce que le handler ferait.
func TestUnauthNonPublicRoutes401(t *testing.T) {
	authStore, handler, _ := authWeb(t)
	if _, err := authStore.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	for pat, need := range routeRights {
		if need == rightPublic {
			continue
		}
		message, path, _ := strings.Cut(pat, " ")
		path = strings.TrimSuffix(path, "{$}")
		for _, seg := range regexp.MustCompile(`\{[^}]+\}`).FindAllString(path, -1) {
			path = strings.Replace(path, seg, "x", 1)
		}
		if recorder := (modeReq{method: message, path: path}).do(handler); recorder.Code != 401 {
			t.Errorf("%s sans session : %d", pat, recorder.Code)
		}
	}
}

// Épingle ce que le garde laisse passer sans session (rightPublic) et ce qu'un invité atteint (guestRoutes) : ajouter une
// entrée oblige à toucher ce test, donc à relire l'exposition.
func TestPublicAndGuestSurfacePinned(t *testing.T) {
	var pub, guest []string
	for path, right := range routeRights {
		page := strings.HasPrefix(path, "GET /") && !strings.HasPrefix(path, "GET /v1/") // pages et statiques : HTML fixe
		if right == rightPublic && !page {
			pub = append(pub, path)
		}
	}
	for path := range guestRoutes {
		guest = append(guest, path)
	}
	sort.Strings(pub)
	sort.Strings(guest)
	wantPub := "DELETE /v1/auth/sessions/current|DELETE /v1/guest/me|GET /v1/auth/alerts|GET /v1/auth/me|GET /v1/auth/sessions|GET /v1/guest/me|GET /v1/guest/sessions|" +
		"POST /invite/{home}/{token}|POST /pair|POST /v1/auth/factors/{factor}/removals|POST /v1/auth/mfa/recovery|POST /v1/auth/mfa/totp|POST /v1/auth/mfa/webauthn/assertions|" +
		"POST /v1/auth/mfa/webauthn/challenges|POST /v1/auth/sessions|POST /v1/auth/sessions/revocations|POST /v1/auth/sessions/{sid}/revocations|POST /v1/auth/totp/enrollments|" +
		"POST /v1/auth/webauthn/registrations|PUT /v1/auth/password|PUT /v1/auth/recovery-codes|PUT /v1/auth/totp/enrollments/current|PUT /v1/auth/webauthn/registrations/current"
	wantGuest := "DELETE /v1/guest/me|DELETE /v1/sessions/{id}/queue/{qid}|GET /assets/|GET /favicon.ico|GET /guest|GET /icons/|GET /site.webmanifest|GET /v1/fs/entries/content|GET /v1/fs/tree|" +
		"GET /v1/guest/me|GET /v1/sessions/{id}/conversation|GET /v1/sessions/{id}/conversation/images/{img}|GET /v1/sessions/{id}/conversation/text|GET /v1/sessions/{id}/conversation/tools|" +
		"GET /v1/sessions/{id}/live|GET /v1/sessions/{id}/queue|GET /v1/sessions/{id}/state|GET /v1/sessions/{id}/tasks/{task}/output|GET /v1/sessions/{id}/workflows|POST /v1/sessions/{id}/queue"
	if got := strings.Join(pub, "|"); got != wantPub {
		t.Errorf("routes publiques (hors pages) modifiées :\n%s", got)
	}
	if got := strings.Join(guest, "|"); got != wantGuest {
		t.Errorf("guestRoutes modifié :\n%s", got)
	}
}
