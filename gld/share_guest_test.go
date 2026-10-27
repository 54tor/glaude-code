package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

const gOther = "22222222-2222-2222-2222-222222222222"

// guestWeb : le serveur complet, jeton hôte branché comme dans main (vide : sans jeton), magasins dans des dossiers jetables.
func guestWeb(t *testing.T, s *authStore, tok string) http.Handler {
	t.Helper()
	resetAuth(t, s)
	resetTokenTries()
	oTok, oSD, oQD, oSS, oQS := hostToken, shareDir, queueDir, shareSt, queueSt
	t.Cleanup(func() { hostToken, shareDir, queueDir, shareSt, queueSt = oTok, oSD, oQD, oSS, oQS; resetTokenTries() })
	sd, qd := t.TempDir(), t.TempDir()
	shareDir, queueDir = func() string { return sd }, func() string { return qd }
	hostToken = tok
	guestAdds.Lock()
	guestAdds.by = map[string]*tryWin{}
	guestAdds.Unlock()
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "projects", "-q", gOther+".jsonl"),
		`{"type":"user","cwd":"/q","timestamp":"2026-01-01T00:00:00Z","message":{"content":"SECRET autre session"}}`+"\n", old)
	return h
}

type gReq struct {
	method, path, body, cookie, csrf, tokCookie string
	hdr                                         map[string]string
}

func (q gReq) do(h http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(q.method, q.path, strings.NewReader(q.body))
	r.Host = testHost
	if q.cookie != "" {
		r.AddCookie(&http.Cookie{Name: guestCookie, Value: q.cookie})
	}
	if q.tokCookie != "" {
		r.AddCookie(&http.Cookie{Name: tokenCookie, Value: q.tokCookie})
	}
	if q.csrf != "" {
		r.Header.Set(csrfHeader, q.csrf)
	}
	for k, v := range q.hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// guestIn : partage de id1 créé, lien ouvert ; rend le cookie et l'en-tête CSRF de l'invité.
func guestIn(t *testing.T, h http.Handler) (cookie, csrf string) {
	t.Helper()
	tok, err := shareSt.Create(id1, "-p")
	if err != nil {
		t.Fatal(err)
	}
	w := gReq{method: "POST", path: "/invite/main/" + tok}.do(h)
	c := cookieOf(w, guestCookie)
	if w.Code != 303 || c == nil {
		t.Fatalf("invite : %d %v", w.Code, w.Result().Cookies())
	}
	w = gReq{method: "GET", path: "/guest-me", cookie: c.Value}.do(h)
	var me struct{ ID, Dir, Mode, CSRF string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil || me.CSRF == "" {
		t.Fatalf("guest-me : %d %s", w.Code, w.Body)
	}
	return c.Value, me.CSRF
}

func TestGuestRedeemSetsCookieAndHidesToken(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	tok, _ := shareSt.Create(id1, "-p")
	w := gReq{method: "POST", path: "/invite/main/" + tok}.do(h)
	c := cookieOf(w, guestCookie)
	if w.Code != 303 || w.Header().Get("Location") != "/guest" || strings.Contains(w.Header().Get("Location"), tok) {
		t.Fatalf("redirection : %d %q", w.Code, w.Header().Get("Location"))
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || strings.Contains(c.Value, tok) || !strings.HasPrefix(c.Value, ".") {
		t.Fatalf("cookie : %+v", c)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache : %q", w.Header().Get("Cache-Control"))
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 403 || cookieOf(w, guestCookie) != nil {
		t.Fatalf("second usage : %d", w.Code)
	}
}

func TestGuestReloadWorksOtherBrowserRefused(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	tok, _ := shareSt.Create(id1, "-p")
	c := cookieOf(gReq{method: "POST", path: "/invite/main/" + tok}.do(h), guestCookie)
	for range 2 {
		w := gReq{method: "GET", path: "/guest-me", cookie: c.Value}.do(h)
		var me map[string]any
		json.Unmarshal(w.Body.Bytes(), &me)
		if w.Code != 200 || me["id"] != id1 || me["dir"] != "-p" || me["mode"] != "rw" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("rechargement : %d %s", w.Code, w.Body)
		}
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 403 {
		t.Fatalf("autre navigateur : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/guest-me"}).do(h); w.Code == 200 {
		t.Fatalf("sans cookie : %d", w.Code)
	}
}

func TestGuestCannotChangeSession(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	for _, p := range []string{"/conv?id=" + gOther + "&dir=-q", "/conv?dir=-q", "/conv?id=" + gOther + "&dir=-q&id=" + gOther} {
		w := gReq{method: "GET", path: p, cookie: c}.do(h)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Déploie caddy") || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("%s : %d %s", p, w.Code, w.Body)
		}
	}
	for _, b := range []string{`{"id":"` + gOther + `","text":"a"}`, `{"ID":"` + gOther + `","Id":"` + gOther + `","dir":"-q","text":"b"}`} {
		if w := (gReq{method: "POST", path: "/queue-add?id=" + gOther, body: b, cookie: c, csrf: csrf}).do(h); w.Code != 200 {
			t.Fatalf("%s : %d %s", b, w.Code, w.Body)
		}
	}
	if n, m := len(queueSt.List(id1)), len(queueSt.List(gOther)); n != 2 || m != 0 {
		t.Fatalf("partagée %d, autre %d", n, m)
	}
	w := gReq{method: "GET", path: "/queue?id=" + gOther, cookie: c}.do(h)
	if !strings.Contains(w.Body.String(), `"text":"a"`) {
		t.Fatalf("file : %s", w.Body)
	}
}

func TestGuestNonWhitelistedForbidden(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	for _, rt := range [][2]string{
		{"POST", "/session-key"}, {"POST", "/trust-answer"}, {"GET", "/fs-file?path=/etc/passwd"}, {"POST", "/chat"},
		{"GET", "/auth/me"}, {"GET", "/config"}, {"POST", "/queue-retry"}, {"POST", "/share-create"}, {"POST", "/share-end"},
		{"GET", "/api"}, {"GET", "/conv-doc?path=/etc/x.md"}, {"POST", "/session-send"}, {"GET", "/homes"},
		{"GET", "//conv"}, {"GET", "/conv/"}, {"GET", "/%63onv"}, {"GET", "/conv/../api"}, {"POST", "/conv"}, {"GET", "/queue-add"},
		{"GET", "/invite/../api"}, {"GET", "/session-screen"},
	} {
		w := gReq{method: rt[0], path: rt[1], body: `{"id":"` + id1 + `"}`, cookie: c, csrf: csrf}.do(h)
		if w.Code != 403 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s %s : %d %s", rt[0], rt[1], w.Code, w.Body)
		}
	}
}

func TestGuestReadOnlyCannotQueue(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	shareSt.SetMode(id1, "ro")
	if w := (gReq{method: "POST", path: "/queue-add", body: `{"text":"x"}`, cookie: c, csrf: csrf}).do(h); w.Code != 403 {
		t.Fatalf("lecture seule : %d", w.Code)
	}
	if len(queueSt.List(id1)) != 0 {
		t.Fatal("file modifiée")
	}
	if w := (gReq{method: "GET", path: "/conv", cookie: c}).do(h); w.Code != 200 {
		t.Fatalf("lecture : %d", w.Code)
	}
}

func TestGuestCanRemoveOnlyOwnItems(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	host, _ := queueSt.Add(id1, "host", "de l'hôte")
	w := gReq{method: "POST", path: "/queue-add", body: `{"text":"de l'invité"}`, cookie: c, csrf: csrf}.do(h)
	var mine struct{ Item QItem }
	json.Unmarshal(w.Body.Bytes(), &mine)
	if w := (gReq{method: "POST", path: "/queue-rm", body: `{"item":"` + host.ID + `"}`, cookie: c, csrf: csrf}).do(h); w.Code != 403 {
		t.Fatalf("item de l'hôte : %d", w.Code)
	}
	if w := (gReq{method: "POST", path: "/queue-rm", body: `{"item":"` + mine.Item.ID + `"}`, cookie: c, csrf: csrf}).do(h); w.Code != 200 {
		t.Fatalf("son item : %d %s", w.Code, w.Body)
	}
	if l := queueSt.List(id1); len(l) != 1 || l[0].ID != host.ID {
		t.Fatalf("file : %+v", l)
	}
}

func TestGuestRateLimit(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	for i := range 11 {
		w := gReq{method: "POST", path: "/queue-add", body: `{"text":"m"}`, cookie: c, csrf: csrf}.do(h)
		if want := map[bool]int{true: 429, false: 200}[i == 10]; w.Code != want {
			t.Fatalf("ajout %d : %d", i+1, w.Code)
		}
	}
}

func TestGuestAuthorForgedOverwritten(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	gReq{method: "POST", path: "/queue-add", body: `{"text":"m","author":"host"}`, cookie: c, csrf: csrf, hdr: map[string]string{"X-Gld-Author": "host"}}.do(h)
	if l := queueSt.List(id1); len(l) != 1 || l[0].Author != "guest" {
		t.Fatalf("auteur : %+v", l)
	}
}

func TestGuestPostNeedsCSRF(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	for _, bad := range []string{"", "faux", strings.ToUpper(csrf)} {
		if w := (gReq{method: "POST", path: "/queue-add", body: `{"text":"m"}`, cookie: c, csrf: bad}).do(h); w.Code != 403 {
			t.Fatalf("csrf %q : %d", bad, w.Code)
		}
	}
	w := gReq{method: "POST", path: "/queue-add", body: `{"text":"m"}`, cookie: c, csrf: csrf, hdr: map[string]string{"Origin": "https://ailleurs.example", "Sec-Fetch-Site": "cross-site"}}.do(h)
	if w.Code != 403 || len(queueSt.List(id1)) != 0 {
		t.Fatalf("autre origine : %d", w.Code)
	}
}

func TestGuestInviteRateLimit(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	for i := range 11 {
		w := gReq{method: "POST", path: "/invite/main/devine" + string(rune('a'+i))}.do(h)
		if want := map[bool]int{true: 429, false: 403}[i == 10]; w.Code != want {
			t.Fatalf("essai %d : %d", i+1, w.Code)
		}
	}
}

func TestEndedShareCookieFallsThroughToHost(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	shareSt.End(id1)
	w := gReq{method: "GET", path: "/api", cookie: c, tokCookie: modeTok}.do(h)
	if w.Code != 200 {
		t.Fatalf("hôte : %d %s", w.Code, w.Body)
	}
	if k := cookieOf(w, guestCookie); k == nil || k.MaxAge >= 0 {
		t.Fatalf("cookie non effacé : %+v", k)
	}
	if w := (gReq{method: "GET", path: "/api", cookie: "nimporte.quoi"}).do(h); w.Code != 401 {
		t.Fatalf("cookie invalide sans jeton : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/conv", cookie: c}).do(h); w.Code != 401 {
		t.Fatalf("partage terminé : %d", w.Code)
	}
}

func TestGuestWorksInSingleUserTokenMode(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	if w := (gReq{method: "GET", path: "/conv", cookie: c}).do(h); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invité : %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := (gReq{method: "GET", path: "/api"}).do(h); w.Code != 401 {
		t.Fatalf("hôte sans jeton : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/api", tokCookie: modeTok}).do(h); w.Code != 200 {
		t.Fatalf("hôte : %d", w.Code)
	}
}

func TestGuestWorksInMultiUserMode(t *testing.T) {
	s := testStore(t)
	h := guestWeb(t, s, modeTok)
	id, err := s.AddUser("hote", testPw, true)
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := fullSession(t, s, id)
	c, _ := guestIn(t, h)
	if w := (gReq{method: "GET", path: "/conv", cookie: c}).do(h); w.Code != 200 || !strings.Contains(w.Body.String(), "Déploie caddy") {
		t.Fatalf("invité : %d %s", w.Code, w.Body)
	}
	if w := (gReq{method: "GET", path: "/api", cookie: c}).do(h); w.Code != 403 {
		t.Fatalf("invité hors liste : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/admin/users"}).do(h); w.Code != 401 {
		t.Fatalf("hôte sans session : %d", w.Code)
	}
	r := httptest.NewRequest("GET", "/admin/users", nil)
	r.Host = testHost
	r.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("hôte : %d %s", w.Code, w.Body)
	}
}

// Passerelle : un cookie invité d'un home enfant est relayé à l'enfant avec le secret en en-tête, jamais servi ici.
func TestGuestRelayedToChildHome(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _ := cs.Create(id1, "-p")
	sec, _, _ := cs.Redeem(tok)
	type seen struct{ path, guest, cookie, https string }
	var got []seen
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.URL.RequestURI(), r.Header.Get(guestHeader), r.Header.Get("Cookie"), r.Header.Get(guestHTTPSHeader)})
		if strings.HasPrefix(r.URL.Path, "/invite/") {
			w.WriteHeader(303)
			return
		}
		w.Write([]byte("enfant"))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(h Home) (*homeProc, error) {
		return &homeProc{dir: h.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	c := "perso." + sec
	if w := (gReq{method: "GET", path: "/conv?id=x", cookie: c}).do(h); w.Code != 200 || w.Body.String() != "enfant" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("relais : %d %s", w.Code, w.Body)
	}
	if w := (gReq{method: "GET", path: "/api", cookie: c}).do(h); w.Code != 403 {
		t.Fatalf("hors liste : %d", w.Code)
	}
	if w := (gReq{method: "POST", path: "/queue-add", body: `{}`, cookie: c, csrf: "faux"}).do(h); w.Code != 403 {
		t.Fatalf("csrf : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/api", cookie: "perso.faux", tokCookie: modeTok}).do(h); w.Code != 200 || cookieOf(w, guestCookie) == nil {
		t.Fatalf("secret inconnu : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/api", tokCookie: modeTok, hdr: map[string]string{guestHeader: sec}}).do(h); w.Code != 200 {
		t.Fatalf("hôte : %d", w.Code)
	}
	// Requête hôte relayée (gld_home) : en-têtes invité fournis par le client retirés par le proxy.
	r := httptest.NewRequest("GET", "/api", nil)
	r.Host = testHost
	r.AddCookie(&http.Cookie{Name: tokenCookie, Value: modeTok})
	r.AddCookie(&http.Cookie{Name: homeCookie, Value: "perso"})
	r.Header.Set(guestHeader, sec)
	r.Header.Set(guestHTTPSHeader, "1")
	h.ServeHTTP(httptest.NewRecorder(), r)
	// Invitations relayées réussies : l'essai est rendu, la 11e passe encore.
	for i := range 11 {
		if w := (gReq{method: "POST", path: "/invite/perso/" + tok}).do(h); w.Code != 303 {
			t.Fatalf("invitation relayée %d : %d", i+1, w.Code)
		}
	}
	if len(got) != 13 || got[0].guest != sec || got[0].cookie != "" || got[1].path != "/api" || got[1].guest != "" || got[1].https != "" || got[2].guest != "" || got[2].path != "/invite/perso/"+tok {
		t.Fatalf("vu par l'enfant : %+v", got)
	}
	if w := (gReq{method: "POST", path: "/invite/inconnu/" + tok}).do(h); w.Code != 403 {
		t.Fatalf("home inconnu : %d", w.Code)
	}
}

// Enfant : le secret relayé par la passerelle est validé ici ; invalide, jamais de repli sur l'hôte.
func TestGuestChildValidatesRelayedSecret(t *testing.T) {
	h := guestWeb(t, nil, "")
	oc := childToken
	t.Cleanup(func() { childToken = oc })
	childToken = "ctok"
	tok, _ := shareSt.Create(id1, "-p")
	w := gReq{method: "POST", path: "/invite/perso/" + tok, hdr: map[string]string{childHeader: "ctok"}}.do(h)
	c := cookieOf(w, guestCookie)
	if w.Code != 303 || c == nil || !strings.HasPrefix(c.Value, "perso.") {
		t.Fatalf("invitation : %d %+v", w.Code, c)
	}
	_, sec, _ := strings.Cut(c.Value, ".")
	w = gReq{method: "GET", path: "/guest-me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec}}.do(h)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id1) {
		t.Fatalf("relayé : %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/api", "/conv"} {
		if w := (gReq{method: "GET", path: p, hdr: map[string]string{childHeader: "ctok", guestHeader: "faux"}}).do(h); w.Code != 403 {
			t.Fatalf("%s secret faux : %d", p, w.Code)
		}
	}
	if w := (gReq{method: "GET", path: "/api", hdr: map[string]string{guestHeader: sec}}).do(h); w.Code != 403 {
		t.Fatalf("sans la passerelle : %d", w.Code)
	}
}

func TestInviteGetDoesNotRedeem(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	tok, _ := shareSt.Create(id1, "-p")
	for range 2 {
		w := gReq{method: "GET", path: "/invite/main/" + tok}.do(h)
		if w.Code != 200 || cookieOf(w, guestCookie) != nil || w.Header().Get("Cache-Control") != "no-store" ||
			!strings.Contains(w.Body.String(), `method="post"`) || !strings.Contains(w.Body.String(), html.EscapeString(L("srv.share_guest.rejoindre"))) || strings.Contains(w.Body.String(), "<!--") {
			t.Fatalf("page : %d %s", w.Code, w.Body)
		}
	}
	if sh, _ := shareSt.Get(id1); sh.CookieHash != "" {
		t.Fatal("jeton consommé par un GET")
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 303 || cookieOf(w, guestCookie) == nil {
		t.Fatalf("POST : %d", w.Code)
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 403 {
		t.Fatalf("second POST : %d", w.Code)
	}
	tok2, _ := shareSt.Create(gOther, "-q")
	w := gReq{method: "POST", path: "/invite/main/" + tok2, hdr: map[string]string{"Origin": "https://ailleurs.example", "Sec-Fetch-Site": "cross-site"}}.do(h)
	if w.Code != 403 || cookieOf(w, guestCookie) != nil {
		t.Fatalf("autre origine : %d", w.Code)
	}
	w = gReq{method: "POST", path: "/invite/main/" + tok2, hdr: map[string]string{"Origin": "http://" + testHost, "Sec-Fetch-Site": "same-origin"}}.do(h)
	if w.Code != 303 {
		t.Fatalf("même origine : %d", w.Code)
	}
}

func TestInviteHostCredentialDoesNotRedeem(t *testing.T) {
	t.Run("jeton", func(t *testing.T) {
		h := guestWeb(t, nil, modeTok)
		tok, _ := shareSt.Create(id1, "-p")
		for _, m := range []string{"GET", "POST"} {
			w := gReq{method: m, path: "/invite/main/" + tok, tokCookie: modeTok}.do(h)
			if w.Code != 200 || cookieOf(w, guestCookie) != nil || !strings.Contains(w.Body.String(), html.EscapeString(L("srv.share_guest.vousEtesHote"))) {
				t.Fatalf("%s hôte : %d %s", m, w.Code, w.Body)
			}
		}
		if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 303 {
			t.Fatalf("jeton brûlé : %d", w.Code)
		}
	})
	t.Run("comptes", func(t *testing.T) {
		s := testStore(t)
		h := guestWeb(t, s, modeTok)
		id, _ := s.AddUser("hote", testPw, true)
		if err := s.Grant(id, mainHomeID, allRights...); err != nil {
			t.Fatal(err)
		}
		sess, _ := fullSession(t, s, id)
		tok, _ := shareSt.Create(id1, "-p")
		r := httptest.NewRequest("POST", "/invite/main/"+tok, nil)
		r.Host = testHost
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || cookieOf(w, guestCookie) != nil || !strings.Contains(w.Body.String(), html.EscapeString(L("srv.share_guest.vousEtesHote"))) {
			t.Fatalf("hôte : %d %s", w.Code, w.Body)
		}
		if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 303 {
			t.Fatalf("jeton brûlé : %d", w.Code)
		}
	})
	t.Run("compte sans exec", func(t *testing.T) {
		s := testStore(t)
		h := guestWeb(t, s, modeTok)
		id, _ := s.AddUser("collegue", testPw, false)
		sess, _ := fullSession(t, s, id)
		s.Revoke(id, mainHomeID, RightExec)
		tok, _ := shareSt.Create(id1, "-p")
		r := httptest.NewRequest("POST", "/invite/main/"+tok, nil)
		r.Host = testHost
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 303 || cookieOf(w, guestCookie) == nil {
			t.Fatalf("collègue sans exec refusé : %d %s", w.Code, w.Body)
		}
	})
}

func TestGuestLeaveClearsCookie(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	if w := (gReq{method: "POST", path: "/guest-leave", cookie: c}).do(h); w.Code != 403 || cookieOf(w, guestCookie) != nil {
		t.Fatalf("sans csrf : %d", w.Code)
	}
	w := gReq{method: "POST", path: "/guest-leave", cookie: c, csrf: csrf}.do(h)
	if k := cookieOf(w, guestCookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) || k == nil || k.MaxAge >= 0 {
		t.Fatalf("départ : %d %s %+v", w.Code, w.Body, k)
	}
	if _, ok := shareSt.Get(id1); !ok {
		t.Fatal("partage supprimé")
	}
	if w := (gReq{method: "GET", path: "/api", tokCookie: modeTok}).do(h); w.Code != 200 {
		t.Fatalf("hôte : %d", w.Code)
	}
	if w := (gReq{method: "POST", path: "/guest-leave", tokCookie: modeTok}).do(h); w.Code != 403 {
		t.Fatalf("non-invité : %d", w.Code)
	}
}

// Passerelle, partage d'un home enfant que l'enfant refuse (fin non écrite) : /guest-leave reste servi par la passerelle.
func TestGuestLeaveRelayedHandledByGateway(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _ := cs.Create(id1, "-p")
	sec, _, _ := cs.Redeem(tok)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(Home) (*homeProc, error) { t.Error("relayé"); return nil, http.ErrServerClosed }
	w := gReq{method: "POST", path: "/guest-leave", cookie: "perso." + sec, csrf: guestCSRF(sec)}.do(h)
	if k := cookieOf(w, guestCookie); w.Code != 200 || k == nil || k.MaxAge >= 0 {
		t.Fatalf("départ : %d %+v", w.Code, k)
	}
}
