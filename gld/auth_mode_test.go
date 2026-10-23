package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const modeTok = "0123456789abcdef0123"

// authWeb : le serveur complet de web(t), avec une base de comptes ouverte et le jeton branché comme dans main.
func authWeb(t *testing.T) (*authStore, http.Handler) {
	t.Helper()
	s := testStore(t)
	oldSt, oldTP := authSt, tokenPrincipal
	t.Cleanup(func() { authSt, tokenPrincipal = oldSt, oldTP })
	authSt, tokenPrincipal = s, tokenForChildren(modeTok)
	h, _, _ := web(t)
	return s, withTokenOpen(modeTok, h)
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

// Sans compte en base, la base ouverte ne change rien : mêmes réponses que withToken autour de l'ancien handler.
func TestOpenModeUnchanged(t *testing.T) {
	h0, _, _ := web(t) // authSt nil : le serveur d'avant
	before := withToken(modeTok, h0)
	s := testStore(t)
	oldSt, oldTP := authSt, tokenPrincipal
	t.Cleanup(func() { authSt, tokenPrincipal = oldSt, oldTP })
	authSt, tokenPrincipal = s, tokenForChildren(modeTok)
	after := withTokenOpen(modeTok, handler()) // même environnement que before
	if authMode(s) {
		t.Fatal("mode multi-utilisateur sans compte")
	}
	for _, q := range []modeReq{
		{method: "GET", path: "/api"},
		{method: "GET", path: "/api?token=" + modeTok},
		{method: "GET", path: "/memoire?token=" + modeTok + "&x=1"},
		{method: "GET", path: "/api?token=faux"},
		{method: "GET", path: "/api", tokCookie: modeTok},
		{method: "GET", path: "/api", tokCookie: "faux"},
		{method: "GET", path: "/api", bearer: modeTok},
		{method: "GET", path: "/api", bearer: "faux"},
		{method: "GET", path: "/", tokCookie: modeTok, accept: "text/html"},
		{method: "GET", path: "/homes", tokCookie: modeTok},
		{method: "GET", path: "/inexistant", tokCookie: modeTok},
		{method: "POST", path: "/memory-save", tokCookie: modeTok, origin: "https://ailleurs.example"},
		{method: "POST", path: "/homes-save", bearer: modeTok, origin: "http://" + testHost},
	} {
		a, b := q.do(before), q.do(after)
		if a.Code != b.Code || a.Body.String() != b.Body.String() ||
			a.Header().Get("Set-Cookie") != b.Header().Get("Set-Cookie") || a.Header().Get("Location") != b.Header().Get("Location") {
			t.Errorf("%s %s : avant %d %q, après %d %q", q.method, q.path, a.Code, cut(a.Body.String(), 120), b.Code, cut(b.Body.String(), 120))
		}
	}
}

func TestTokenIgnoredInURLWhenAuth(t *testing.T) {
	s, h := authWeb(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	for _, q := range []modeReq{
		{method: "GET", path: "/api?token=" + modeTok},
		{method: "GET", path: "/memoire?token=" + modeTok + "&x=1", accept: "application/json"},
		{method: "GET", path: "/api", tokCookie: modeTok},
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
	s, h := authWeb(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	if w := (modeReq{method: "GET", path: "/api", bearer: modeTok}).do(h); w.Code != 200 {
		t.Fatalf("Bearer sur une route de home : %d %s", w.Code, w.Body)
	}
	if w := (modeReq{method: "GET", path: "/api", bearer: "faux-jeton-de-vingt-car"}).do(h); w.Code != 401 {
		t.Fatalf("mauvais Bearer : %d", w.Code)
	}
	if w := (modeReq{method: "GET", path: "/auth/me", bearer: modeTok}).do(h); w.Code != 401 {
		t.Fatalf("/auth/me avec Bearer : %d %s", w.Code, w.Body)
	}
	if w := (modeReq{method: "POST", path: "/homes-save", bearer: modeTok}).do(h); w.Code != 403 {
		t.Fatalf("route admin avec Bearer : %d %s", w.Code, w.Body)
	}
}

func TestChildHasNoGate(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	oldSt := authSt
	t.Cleanup(func() { authSt, childToken = oldSt, "" })
	authSt, childToken = s, "secret-enfant"
	if authMode(s) {
		t.Fatal("un enfant n'est jamais en mode multi-utilisateur")
	}
	if st, err := startAuth(); st != nil || err != nil {
		t.Fatalf("un enfant n'ouvre pas la base : %v %v", st, err)
	}
	h, _, _ := web(t)
	r := httptest.NewRequest("GET", "/api", nil)
	r.Host = testHost
	r.Header.Set(childHeader, "secret-enfant")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("enfant : %d %s", w.Code, w.Body)
	}
}

func TestAuthModeSwitchPerRequest(t *testing.T) {
	s, h := authWeb(t)
	q := modeReq{method: "GET", path: "/api", tokCookie: modeTok}
	if w := q.do(h); w.Code != 200 {
		t.Fatalf("mode ouvert : %d", w.Code)
	}
	if _, err := s.AddUser("alice", testPw, true); err != nil { // comme `gld user add` pendant que le serveur tourne
		t.Fatal(err)
	}
	if w := q.do(h); w.Code != 401 {
		t.Fatalf("après le premier compte : %d", w.Code)
	}
}

func TestUnauthShellServed(t *testing.T) {
	s, h := authWeb(t)
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/", "/memoire", "/configuration/x"} {
		w := (modeReq{method: "GET", path: p, accept: "text/html"}).do(h)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s : %d %q", p, w.Code, w.Header().Get("Content-Type"))
		}
	}
	w := (modeReq{method: "GET", path: "/api", accept: "text/html"}).do(h)
	if w.Code != 401 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API sans session : %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestStartupRefusesWrongKey(t *testing.T) {
	s := testStore(t)
	s.db.Close()
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
	if st, err := startAuth(); err == nil || st != nil {
		t.Fatalf("clé fausse acceptée : %v", err)
	}
}

func TestChildProxyStripsAuthorization(t *testing.T) {
	var got http.Header
	back := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer back.Close()
	u, _ := url.Parse(back.URL)
	r := httptest.NewRequest("GET", "/api", nil)
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
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/term-ws", &websocket.DialOptions{HTTPHeader: hd})
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
		mux.HandleFunc("GET /term-ws", wsHold(false))
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
		mux.HandleFunc("GET /term-ws", func(w http.ResponseWriter, r *http.Request) { t.Error("servi par la passerelle") })
		srv := httptest.NewServer(authGate(mux, g.s, withAuthWatch(withHomes(mux))))
		defer srv.Close()
		pro, _ := homeIDFor("Pro")
		if d := wsCut(t, srv, u, "Pro", func() { g.s.Revoke(u.id, pro, RightExec) }); d > 6*time.Second {
			t.Fatalf("coupé en %v", d)
		}
	})
}
