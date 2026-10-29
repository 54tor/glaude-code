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
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const modeTok = "0123456789abcdef0123"

// resetAuth remet l'état du mode à zéro sur s (nil : aucune base ouverte) et fige l'horloge ; tick la fait avancer d'une fenêtre.
func resetAuth(t *testing.T, s *authStore) (tick func()) {
	t.Helper()
	oSt, oErr, oUsers, oSeen, oAt, oNow, oTP, oFI, oFn := authSt, authErr, authUsers, authSeen, authAt, modeNow, tokenPrincipal, authFI, authStateFn
	t.Cleanup(func() {
		authSt, authErr, authUsers, authSeen, authAt, modeNow, tokenPrincipal, authFI, authStateFn = oSt, oErr, oUsers, oSeen, oAt, oNow, oTP, oFI, oFn
	})
	now := time.Now()
	authSt, authErr, authUsers, authSeen, authAt, authFI = s, nil, false, false, time.Time{}, nil
	if s != nil {
		authFI, _ = os.Lstat(filepath.Join(os.Getenv("GLD_AUTH_DIR"), "auth.db"))
	}
	modeNow = func() time.Time { return now }
	tokenPrincipal = tokenForChildren(modeTok, false)
	return func() { now = now.Add(modeEvery) }
}

// authWeb : le serveur complet de web(t), avec une base de comptes ouverte et le jeton branché comme dans main.
func authWeb(t *testing.T) (*authStore, http.Handler, func()) {
	t.Helper()
	s := testStore(t)
	tick := resetAuth(t, s)
	h, _, _ := web(t)
	return s, withTokenOpen(modeTok, h), tick
}

type modeReq struct {
	method, path, bearer, tokCookie, origin, accept string
}

func (q modeReq) do(h http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(q.method, q.path, strings.NewReader("{}"))
	r.Host = testHost
	if q.bearer != "" {
		r.Header.Set("Authorization", "Bearer "+q.bearer)
	}
	if q.tokCookie != "" {
		r.AddCookie(&http.Cookie{Name: tokenCookie, Value: q.tokCookie})
	}
	if q.origin != "" {
		r.Header.Set("Origin", q.origin)
	}
	if q.accept != "" {
		r.Header.Set("Accept", q.accept)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Sans compte en base, rien ne change : mêmes réponses sans base et avec une base vide, /auth/* exclu.
func TestOpenModeUnchanged(t *testing.T) {
	resetAuth(t, nil)
	h, _, _ := web(t)
	srv := withTokenOpen(modeTok, h)
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
		{method: "GET", path: "/homes", tokCookie: modeTok},
		{method: "GET", path: "/inexistant", tokCookie: modeTok},
		{method: "POST", path: "/memory-save", tokCookie: modeTok, origin: "https://ailleurs.example"},
		{method: "POST", path: "/homes-save", bearer: modeTok, origin: "http://" + testHost},
	}
	var before []*httptest.ResponseRecorder
	for _, q := range reqs {
		before = append(before, q.do(srv))
	}
	if before[0].Code != 401 || before[1].Code != 303 || before[4].Code != 200 || before[6].Code != 200 {
		t.Fatalf("withToken : %d %d %d %d", before[0].Code, before[1].Code, before[4].Code, before[6].Code)
	}
	resetAuth(t, testStore(t))
	if _, on, _ := authState(); on {
		t.Fatal("mode multi-utilisateur sans compte")
	}
	for i, q := range reqs {
		a, b := before[i], q.do(srv)
		if a.Code != b.Code || a.Body.String() != b.Body.String() ||
			a.Header().Get("Set-Cookie") != b.Header().Get("Set-Cookie") || a.Header().Get("Location") != b.Header().Get("Location") {
			t.Errorf("%s %s : sans base %d %q, base vide %d %q", q.method, q.path, a.Code, cut(a.Body.String(), 120), b.Code, cut(b.Body.String(), 120))
		}
	}
}

// Sans auth.db : ni dossier, ni base, ni clé, ni coffre-fort, et /auth/* n'existe pas.
func TestNoAuthDBNoSideEffects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gld")
	t.Setenv("GLD_AUTH_DIR", dir)
	t.Setenv("GLD_AUTH_KEY", "")
	g, st := keyringGet, keyringSet
	t.Cleanup(func() { keyringGet, keyringSet = g, st })
	keyringGet = func() (string, error) { t.Error("coffre-fort lu"); return "", errors.New("non") }
	keyringSet = func(string) error { t.Error("coffre-fort écrit"); return errors.New("non") }
	tick := resetAuth(t, nil)
	h, _, _ := web(t)
	srv := withTokenOpen(modeTok, h)
	for range 3 {
		tick()
		if w := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv); w.Code != 200 {
			t.Fatalf("mode ouvert : %d", w.Code)
		}
		if w := (modeReq{method: "GET", path: "/auth/me", tokCookie: modeTok}).do(srv); w.Code != 404 {
			t.Fatalf("/auth/me sans base : %d", w.Code)
		}
	}
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dossier des comptes créé : %v", err)
	}

	// La base et un compte apparaissent pendant que le serveur tourne : bascule au plus tard une fenêtre plus tard.
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("cd", 32))
	s, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	q := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
	if w := q.do(srv); w.Code != 200 {
		t.Fatalf("dans la fenêtre du cache : %d", w.Code)
	}
	tick()
	if w := q.do(srv); w.Code != 401 {
		t.Fatalf("après la fenêtre : %d", w.Code)
	}
	if w := (modeReq{method: "GET", path: "/auth/me"}).do(srv); w.Code != 401 {
		t.Fatalf("/auth/me monté : %d", w.Code)
	}
}

// Une base avec des comptes ne rend jamais le mode ouvert sur une erreur : clé fausse, base fermée, fichier supprimé.
func TestAuthFailClosed(t *testing.T) {
	t.Run("TestStartupRefusesWrongKey", func(t *testing.T) {
		s := testStore(t)
		s.AddUser("alice", testPw, true)
		s.db.Close()
		t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
		tick := resetAuth(t, nil)
		h, _, _ := web(t)
		srv := withTokenOpen(modeTok, h)
		if _, _, err := authState(); err == nil {
			t.Fatal("démarrage accepté")
		}
		tick()
		w := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv)
		if w.Code != 503 || !strings.Contains(w.Body.String(), jsonEsc(L("srv.auth.baseInutilisable"))) {
			t.Fatalf("service : %d %s", w.Code, w.Body)
		}
	})
	t.Run("base fermée", func(t *testing.T) {
		s, srv, tick := authWeb(t)
		s.AddUser("alice", testPw, true)
		q := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
		if w := q.do(srv); w.Code != 401 {
			t.Fatalf("mode actif : %d", w.Code)
		}
		s.db.Close()
		tick()
		if w := q.do(srv); w.Code != 503 {
			t.Fatalf("base fermée : %d", w.Code)
		}
	})
	t.Run("fichier supprimé", func(t *testing.T) {
		s, srv, tick := authWeb(t)
		s.AddUser("alice", testPw, true)
		q := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
		if w := q.do(srv); w.Code != 401 {
			t.Fatalf("mode actif : %d", w.Code)
		}
		if err := os.Remove(filepath.Join(os.Getenv("GLD_AUTH_DIR"), "auth.db")); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			tick()
			if w := q.do(srv); w.Code != 503 {
				t.Fatalf("base supprimée : %d", w.Code)
			}
		}
	})
	t.Run("table vidée", func(t *testing.T) {
		s, srv, tick := authWeb(t)
		s.AddUser("alice", testPw, true)
		q := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
		if w := q.do(srv); w.Code != 401 {
			t.Fatalf("mode actif : %d", w.Code)
		}
		if _, err := s.db.Exec(`DELETE FROM users`); err != nil {
			t.Fatal(err)
		}
		// Le marqueur des comptes créés reste : des comptes supprimés ferment le service, jamais le mode ouvert.
		for range 2 {
			tick()
			if w := q.do(srv); w.Code != 503 {
				t.Fatalf("plus aucun compte : %d", w.Code)
			}
		}
	})
	t.Run("marqueur altéré", func(t *testing.T) {
		s, srv, tick := authWeb(t)
		s.AddUser("alice", testPw, true)
		s.db.Exec(`UPDATE meta SET value=x'00' WHERE key=?`, accountsKey)
		tick()
		if w := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv); w.Code != 503 {
			t.Fatalf("marqueur altéré : %d", w.Code)
		}
		s.db.Exec(`DELETE FROM users`)
		tick()
		if w := (modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(srv); w.Code != 503 {
			t.Fatalf("marqueur altéré sans compte : %d", w.Code)
		}
	})
}

func jsonEsc(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}

func TestTokenIgnoredInURLWhenAuth(t *testing.T) {
	s, h, _ := authWeb(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	for _, q := range []modeReq{
		{method: "GET", path: "/v1/state?token=" + modeTok},
		{method: "GET", path: "/memoire?token=" + modeTok + "&x=1", accept: "application/json"},
		{method: "GET", path: "/v1/state", tokCookie: modeTok},
	} {
		w := q.do(h)
		if q.path[1] == 'm' { // page publique : servie, mais sans cookie de jeton ni redirection
			if w.Code != 200 || w.Header().Get("Set-Cookie") != "" {
				t.Errorf("%s : %d %q", q.path, w.Code, w.Header().Get("Set-Cookie"))
			}
			continue
		}
		if w.Code != 401 {
			t.Errorf("%s cookie=%q : %d", q.path, q.tokCookie, w.Code)
		}
	}
}

func TestBearerStillWorks(t *testing.T) {
	s, h, _ := authWeb(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	if w := (modeReq{method: "GET", path: "/v1/state", bearer: modeTok}).do(h); w.Code != 200 {
		t.Fatalf("Bearer sur une route de home : %d %s", w.Code, w.Body)
	}
	if w := (modeReq{method: "GET", path: "/v1/state", bearer: "faux-jeton-de-vingt-car"}).do(h); w.Code != 401 {
		t.Fatalf("mauvais Bearer : %d", w.Code)
	}
	if w := (modeReq{method: "GET", path: "/auth/me", bearer: modeTok}).do(h); w.Code != 401 {
		t.Fatalf("/auth/me avec Bearer : %d %s", w.Code, w.Body)
	}
	if w := (modeReq{method: "POST", path: "/homes-save", bearer: modeTok}).do(h); w.Code != 403 {
		t.Fatalf("route admin avec Bearer : %d %s", w.Code, w.Body)
	}
}

// Un jeton généré (hors localhost sans GLD_WEB_TOKEN) ne vaut pas en Bearer quand les comptes existent, ni ne s'imprime.
func TestGeneratedTokenNoBearer(t *testing.T) {
	s, h, _ := authWeb(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	tokenPrincipal = tokenForChildren(modeTok, true)
	if w := (modeReq{method: "GET", path: "/v1/state", bearer: modeTok}).do(h); w.Code != 401 {
		t.Fatalf("Bearer d'un jeton généré : %d %s", w.Code, w.Body)
	}
	for _, u := range urlsFor("http", "192.0.2.1", "8799", "") {
		if strings.Contains(u, "token") {
			t.Fatalf("adresse sans jeton : %q", u)
		}
	}
}

func TestChildHasNoGate(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	resetAuth(t, nil)
	t.Cleanup(func() { childToken = "" })
	childToken = "secret-enfant"
	if st, on, err := authState(); st != nil || on || err != nil {
		t.Fatalf("un enfant n'ouvre pas la base : %v %v %v", st, on, err)
	}
	h, _, _ := web(t)
	r := httptest.NewRequest("GET", "/v1/state", nil)
	r.Host = testHost
	r.Header.Set(childHeader, "secret-enfant")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("enfant : %d %s", w.Code, w.Body)
	}
}

func TestAuthModeSwitchPerRequest(t *testing.T) {
	s, h, tick := authWeb(t)
	q := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
	if w := q.do(h); w.Code != 200 {
		t.Fatalf("mode ouvert : %d", w.Code)
	}
	if _, err := s.AddUser("alice", testPw, true); err != nil { // comme `gld user add` pendant que le serveur tourne
		t.Fatal(err)
	}
	tick()
	if w := q.do(h); w.Code != 401 {
		t.Fatalf("après le premier compte : %d", w.Code)
	}
}

func TestUnauthShellServed(t *testing.T) {
	s, h, _ := authWeb(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/", "/memoire", "/configuration/x"} {
		w := (modeReq{method: "GET", path: p, accept: "text/html"}).do(h)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s : %d %q", p, w.Code, w.Header().Get("Content-Type"))
		}
	}
	w := (modeReq{method: "GET", path: "/v1/state", accept: "text/html"}).do(h)
	if w.Code != 401 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API sans session : %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestChildProxyStripsAuthorization(t *testing.T) {
	var got http.Header
	back := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer back.Close()
	u, _ := url.Parse(back.URL)
	r := httptest.NewRequest("GET", "/v1/state", nil)
	r.Header.Set("Authorization", "Bearer "+modeTok)
	r.Header.Set("Cookie", sessCookie+"=x; "+tokenCookie+"=y")
	r.Header.Set("Origin", "http://"+testHost)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	childProxy(u, "jeton-enfant").ServeHTTP(httptest.NewRecorder(), r)
	if got == nil {
		t.Fatal("requête non relayée")
	}
	for _, k := range []string{"Authorization", "Cookie", "Origin", "Sec-Fetch-Site"} {
		if got.Get(k) != "" {
			t.Errorf("%s relayé à l'enfant", k)
		}
	}
	if got.Get(childHeader) != "jeton-enfant" {
		t.Error("jeton de la passerelle absent")
	}
}

// wsHold : un WebSocket qui lit jusqu'à la fin de son contexte, comme /term-ws.
func wsHold(skipOrigin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: skipOrigin})
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}
}

// wsCut ouvre le flux, le vérifie, appelle revoke et renvoie le délai avant la fermeture vue par le client.
func wsCut(t *testing.T, srv *httptest.Server, u gateUser, home string, revoke func()) time.Duration {
	t.Helper()
	hd := http.Header{"Origin": {srv.URL}}
	hd.Add("Cookie", (&http.Cookie{Name: sessCookie, Value: u.cookie}).String())
	if home != "" {
		hd.Add("Cookie", (&http.Cookie{Name: homeCookie, Value: url.QueryEscape(home)}).String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/terminal/ws", &websocket.DialOptions{HTTPHeader: hd})
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("ouverture : %v (%d)", err, code)
	}
	defer c.CloseNow()
	if err := c.Write(ctx, websocket.MessageText, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	revoke()
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("message reçu au lieu de la fermeture")
	}
	return time.Since(start)
}

func TestWSCutOnRevoke(t *testing.T) {
	t.Run("passerelle", func(t *testing.T) {
		g := newGateRig(t)
		u := g.user(t, "alice", false)
		g.grant(t, u, "", RightExec)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/terminal/ws", wsHold(false))
		srv := httptest.NewServer(authGate(mux, g.s, withAuthWatch(withHomes(mux))))
		defer srv.Close()
		if d := wsCut(t, srv, u, "", func() { g.s.RevokeUser(u.id) }); d > 6*time.Second {
			t.Fatalf("coupé en %v", d)
		}
	})
	t.Run("home enfant", func(t *testing.T) {
		old := authWatchEvery
		t.Cleanup(func() { authWatchEvery = old })
		authWatchEvery = 100 * time.Millisecond
		g := newGateRig(t)
		addHomes(t, "Pro")
		u := g.user(t, "bob", false)
		g.grant(t, u, "Pro", RightExec)
		child := httptest.NewServer(wsHold(true))
		defer child.Close()
		cu, _ := url.Parse(child.URL)
		oldStart := homeStart
		t.Cleanup(func() { homeStart = oldStart; stopHomes() })
		homeStart = func(h Home) (*homeProc, error) {
			return &homeProc{dir: h.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
		}
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/terminal/ws", func(w http.ResponseWriter, r *http.Request) { t.Error("servi par la passerelle") })
		srv := httptest.NewServer(authGate(mux, g.s, withAuthWatch(withHomes(mux))))
		defer srv.Close()
		pro, _ := homeIDFor("Pro")
		if d := wsCut(t, srv, u, "Pro", func() { g.s.Revoke(u.id, pro, RightExec) }); d > 6*time.Second {
			t.Fatalf("coupé en %v", d)
		}
	})
}

// /chat relayé à un home enfant : la révocation coupe le client et demande l'arrêt du tour à l'enfant.
func TestChildChatStoppedOnRevoke(t *testing.T) {
	old := authWatchEvery
	t.Cleanup(func() { authWatchEvery = old })
	authWatchEvery = 100 * time.Millisecond
	g := newGateRig(t)
	addHomes(t, "Pro")
	u := g.user(t, "bob", false)
	g.grant(t, u, "Pro", RightExec)
	stopped, quit := make(chan string, 1), make(chan struct{})
	cm := http.NewServeMux()
	cm.HandleFunc("POST /v1/sessions/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{\"text\":\"début\"}\n"))
		w.(http.Flusher).Flush()
		select { // un tour d'enfant ne suit pas le contexte de la requête
		case <-time.After(8 * time.Second):
		case <-quit:
		}
	})
	cm.HandleFunc("DELETE /v1/sessions/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(childHeader) != "x" {
			t.Error("arrêt envoyé sans le jeton de la passerelle")
		}
		stopped <- r.PathValue("id")
		close(quit)
	})
	child := httptest.NewServer(cm)
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(h Home) (*homeProc, error) {
		return &homeProc{dir: h.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions/{id}/chat", func(w http.ResponseWriter, r *http.Request) { t.Error("servi par la passerelle") })
	srv := httptest.NewServer(authGate(mux, g.s, withAuthWatch(withHomes(mux))))
	defer srv.Close()

	r, _ := http.NewRequest("POST", srv.URL+"/v1/sessions/"+id1+"/chat", strings.NewReader(`{"Prompt":"x"}`))
	r.AddCookie(&http.Cookie{Name: sessCookie, Value: u.cookie})
	r.AddCookie(&http.Cookie{Name: homeCookie, Value: "Pro"})
	r.Header.Set(csrfHeader, u.csrf)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	if n, err := resp.Body.Read(buf); n == 0 || err != nil {
		t.Fatalf("début du flux : %d %v", n, err)
	}
	pro, _ := homeIDFor("Pro")
	start := time.Now()
	g.s.Revoke(u.id, pro, RightExec)
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
	s, srv, _ := authWeb(t)
	s.AddUser("alice", testPw, true)
	for _, first := range []bool{true, false} {
		n := 0
		authStateFn = func() (*authStore, bool, error) {
			n++
			return s, first == (n%2 == 1), nil
		}
		w := (modeReq{method: "GET", path: "/v1/state"}).do(srv)
		if w.Code != 401 || n != 1 {
			t.Fatalf("premier état %v : %d, %d évaluation(s)", first, w.Code, n)
		}
	}
}

// Une base remplacée pendant l'exécution n'est plus servie depuis l'ancien fichier : refus, puis réouverture.
func TestAuthDBReplaced(t *testing.T) {
	s := testStore(t)
	s.AddUser("alice", testPw, true)
	s.db.Close()
	dir := os.Getenv("GLD_AUTH_DIR")
	tick := resetAuth(t, nil)
	h, _, _ := web(t)
	srv := withTokenOpen(modeTok, h)
	q := modeReq{method: "GET", path: "/v1/state", tokCookie: modeTok}
	if w := q.do(srv); w.Code != 401 {
		t.Fatalf("mode actif : %d", w.Code)
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
	if w := q.do(srv); w.Code != 503 {
		t.Fatalf("base remplacée : %d", w.Code)
	}
	tick()
	if w := q.do(srv); w.Code != 401 || authSt == nil || authSt == old {
		t.Fatalf("réouverture : %d", w.Code)
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
	var v struct{ ID string }
	if !errors.Is(peekJSON(rr, &v), errBodyTooBig) {
		t.Fatal("dépassement non signalé")
	}
	if b, _ := io.ReadAll(rr.Body); string(b) != body {
		t.Fatalf("corps restitué : %d octets sur %d", len(b), len(body))
	}
}

// Base présente sans compte : /auth/* répond comme une route inconnue ; le premier compte l'ouvre dans la fenêtre du cache.
func TestAuthRoutesHiddenWithoutUsers(t *testing.T) {
	s, srv, tick := authWeb(t)
	me := modeReq{method: "GET", path: "/auth/me", tokCookie: modeTok}
	unknown := modeReq{method: "GET", path: "/inexistant", tokCookie: modeTok}
	w, u := me.do(srv), unknown.do(srv)
	if w.Code != 404 || w.Body.String() != u.Body.String() {
		t.Fatalf("/auth/me sans compte : %d %q", w.Code, w.Body)
	}
	if w := (modeReq{method: "POST", path: "/auth/login", tokCookie: modeTok}).do(srv); w.Code != 404 {
		t.Fatalf("/auth/login sans compte : %d", w.Code)
	}
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	tick()
	if w := (modeReq{method: "GET", path: "/auth/me"}).do(srv); w.Code != 401 {
		t.Fatalf("/auth/me après le premier compte : %d", w.Code)
	}
}
