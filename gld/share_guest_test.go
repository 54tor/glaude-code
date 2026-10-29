package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gOther = "22222222-2222-2222-2222-222222222222"

var (
	hID1  = shareHandle(id1)
	gName = guestCookieName(hID1)
)

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

// gReq : cookie est la valeur du cookie invité de la poignée share (par défaut celle de id1), envoyée avec l'en-tête de
// partage sauf noShare ; jar ajoute des cookies bruts.
type gReq struct {
	method, path, body, cookie, csrf, tokCookie, share string
	noShare                                            bool
	hdr                                                map[string]string
	jar                                                []*http.Cookie
}

func (q gReq) do(h http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(q.method, q.path, strings.NewReader(q.body))
	r.Host = testHost
	if q.share == "" {
		q.share = hID1
	}
	if q.cookie != "" {
		r.AddCookie(&http.Cookie{Name: guestCookieName(q.share), Value: q.cookie})
		if !q.noShare {
			r.Header.Set(shareHeader, q.share)
		}
	}
	for _, c := range q.jar {
		r.AddCookie(c)
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

// guestJoin : partage de sess créé, lien ouvert ; rend le cookie et l'en-tête CSRF de l'invité.
func guestJoin(t *testing.T, h http.Handler, sess, dir, title string) (cookie, csrf string) {
	t.Helper()
	tok, _, err := shareSt.CreateInvite(sess, dir, title, "")
	if err != nil {
		t.Fatal(err)
	}
	hd := shareHandle(sess)
	w := gReq{method: "POST", path: "/invite/main/" + tok}.do(h)
	c := cookieOf(w, guestCookieName(hd))
	if w.Code != 303 || c == nil || w.Header().Get("Location") != "/guest/"+hd {
		t.Fatalf("invite : %d %q %v", w.Code, w.Header().Get("Location"), w.Result().Cookies())
	}
	w = gReq{method: "GET", path: "/guest-me", cookie: c.Value, share: hd}.do(h)
	var me struct{ ID, Dir, Mode, CSRF string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil || me.CSRF == "" || me.ID != sess {
		t.Fatalf("guest-me : %d %s", w.Code, w.Body)
	}
	return c.Value, me.CSRF
}

// inv0 : la première invitation du partage de sess.
func inv0(sess string) string {
	shareSt.mu.Lock()
	defer shareSt.mu.Unlock()
	return shareSt.m[sess].Invites[0].ID
}

func guestIn(t *testing.T, h http.Handler) (cookie, csrf string) {
	t.Helper()
	return guestJoin(t, h, id1, "-p", "")
}

func TestGuestRedeemSetsCookieAndHidesToken(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	w := gReq{method: "POST", path: "/invite/main/" + tok}.do(h)
	c := cookieOf(w, gName)
	if w.Code != 303 || w.Header().Get("Location") != "/guest/"+hID1 || strings.Contains(w.Header().Get("Location"), tok) {
		t.Fatalf("redirection : %d %q", w.Code, w.Header().Get("Location"))
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || strings.Contains(c.Value, tok) || !strings.HasPrefix(c.Value, ".") {
		t.Fatalf("cookie : %+v", c)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache : %q", w.Header().Get("Cache-Control"))
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 403 || cookieOf(w, gName) != nil {
		t.Fatalf("second usage : %d", w.Code)
	}
}

func TestGuestReloadWorksOtherBrowserRefused(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	c := cookieOf(gReq{method: "POST", path: "/invite/main/" + tok}.do(h), gName)
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
		{"POST", "/session-key"}, {"POST", "/trust-answer"}, {"POST", "/chat"},
		{"GET", "/auth/me"}, {"GET", "/config"}, {"POST", "/queue-retry"}, {"POST", "/invite-create"}, {"POST", "/share-end"},
		{"GET", "/api"}, {"GET", "/conv-doc?path=/etc/x.md"}, {"POST", "/session-send"}, {"GET", "/homes"},
		{"GET", "//conv"}, {"GET", "/conv/"}, {"GET", "/%63onv"}, {"GET", "/conv/../api"}, {"POST", "/conv"}, {"GET", "/queue-add"},
		{"GET", "/invite/../api"}, {"GET", "/session-screen"}, {"POST", "/fs-file"}, {"GET", "/fs-file?path=.env"}, {"GET", "/fs-tree?path=.git"}, {"GET", "/ide-search"},
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
	shareSt.SetInviteMode(id1, inv0(id1), "ro")
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
	host, _ := queueSt.Add(id1, "host", "", "de l'hôte")
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
	if k := cookieOf(w, gName); k == nil || k.MaxAge >= 0 {
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
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	type seen struct{ path, guest, cookie, https, share, quiet string }
	var got []seen
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.URL.RequestURI(), r.Header.Get(guestHeader), r.Header.Get("Cookie"), r.Header.Get(guestHTTPSHeader), r.Header.Get(shareHeader), r.Header.Get(guestQuietHeader)})
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
	if w := (gReq{method: "GET", path: "/conv?id=x", cookie: c, hdr: map[string]string{guestQuietHeader: "1"}}).do(h); w.Code != 200 || w.Body.String() != "enfant" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("relais : %d %s", w.Code, w.Body)
	}
	if w := (gReq{method: "GET", path: "/api", cookie: c}).do(h); w.Code != 403 {
		t.Fatalf("hors liste : %d", w.Code)
	}
	if w := (gReq{method: "POST", path: "/queue-add", body: `{}`, cookie: c, csrf: "faux"}).do(h); w.Code != 403 {
		t.Fatalf("csrf : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/api", cookie: "perso.faux", tokCookie: modeTok}).do(h); w.Code != 200 || cookieOf(w, gName) == nil {
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
	if len(got) != 13 || got[0].guest != sec || got[0].share != hID1 || got[0].quiet != "" || got[0].cookie != "" || got[1].path != "/api" || got[1].guest != "" || got[1].https != "" || got[2].guest != "" || got[2].path != "/invite/perso/"+tok {
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
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	w := gReq{method: "POST", path: "/invite/perso/" + tok, hdr: map[string]string{childHeader: "ctok"}}.do(h)
	c := cookieOf(w, gName)
	if w.Code != 303 || c == nil || !strings.HasPrefix(c.Value, "perso.") || w.Header().Get("Location") != "/guest/"+hID1 {
		t.Fatalf("invitation : %d %+v", w.Code, c)
	}
	_, sec, _ := strings.Cut(c.Value, ".")
	w = gReq{method: "GET", path: "/guest-me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1}}.do(h)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id1) || !strings.Contains(w.Body.String(), hID1) {
		t.Fatalf("relayé : %d %s", w.Code, w.Body)
	}
	shareSt.mu.Lock()
	shareSt.m[id1].Invites[0].LastSeen = 1
	shareSt.mu.Unlock()
	gReq{method: "GET", path: "/guest-me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1, guestQuietHeader: "1"}}.do(h)
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen != 1 {
		t.Fatalf("vérification /guest-list : LastSeen touché")
	}
	gReq{method: "GET", path: "/guest-me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1}}.do(h)
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen <= 1 {
		t.Fatalf("requête invitée : LastSeen non mis à jour")
	}
	for _, hd := range []string{"", shareHandle(gOther), "nimporte"} {
		if w := (gReq{method: "GET", path: "/guest-me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hd}}).do(h); w.Code != 403 {
			t.Fatalf("poignée %q : %d", hd, w.Code)
		}
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
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	for range 2 {
		w := gReq{method: "GET", path: "/invite/main/" + tok}.do(h)
		if w.Code != 200 || cookieOf(w, gName) != nil || w.Header().Get("Cache-Control") != "no-store" ||
			!strings.Contains(w.Body.String(), `method="post"`) || !strings.Contains(w.Body.String(), html.EscapeString(L("srv.share_guest.rejoindre"))) || strings.Contains(w.Body.String(), "<!--") {
			t.Fatalf("page : %d %s", w.Code, w.Body)
		}
	}
	if sh, _ := shareSt.Get(id1); sh.Invites[0].CookieHash != "" {
		t.Fatal("jeton consommé par un GET")
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 303 || cookieOf(w, gName) == nil {
		t.Fatalf("POST : %d", w.Code)
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 403 {
		t.Fatalf("second POST : %d", w.Code)
	}
	tok2, _, _ := shareSt.CreateInvite(gOther, "-q", "", "")
	w := gReq{method: "POST", path: "/invite/main/" + tok2, hdr: map[string]string{"Origin": "https://ailleurs.example", "Sec-Fetch-Site": "cross-site"}}.do(h)
	if w.Code != 403 || cookieOf(w, guestCookieName(shareHandle(gOther))) != nil {
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
		tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
		for _, m := range []string{"GET", "POST"} {
			w := gReq{method: m, path: "/invite/main/" + tok, tokCookie: modeTok}.do(h)
			if w.Code != 200 || cookieOf(w, gName) != nil || !strings.Contains(w.Body.String(), html.EscapeString(L("srv.share_guest.vousEtesHote"))) {
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
		tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
		r := httptest.NewRequest("POST", "/invite/main/"+tok, nil)
		r.Host = testHost
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || cookieOf(w, gName) != nil || !strings.Contains(w.Body.String(), html.EscapeString(L("srv.share_guest.vousEtesHote"))) {
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
		tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
		r := httptest.NewRequest("POST", "/invite/main/"+tok, nil)
		r.Host = testHost
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 303 || cookieOf(w, gName) == nil {
			t.Fatalf("collègue sans exec refusé : %d %s", w.Code, w.Body)
		}
	})
}

func TestGuestLeaveClearsCookie(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	if w := (gReq{method: "POST", path: "/guest-leave", cookie: c}).do(h); w.Code != 403 || cookieOf(w, gName) != nil {
		t.Fatalf("sans csrf : %d", w.Code)
	}
	w := gReq{method: "POST", path: "/guest-leave", cookie: c, csrf: csrf}.do(h)
	if k := cookieOf(w, gName); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) || k == nil || k.MaxAge >= 0 {
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
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(Home) (*homeProc, error) { t.Error("relayé"); return nil, http.ErrServerClosed }
	w := gReq{method: "POST", path: "/guest-leave", cookie: "perso." + sec, csrf: guestCSRF(sec)}.do(h)
	if k := cookieOf(w, gName); w.Code != 200 || k == nil || k.MaxAge >= 0 {
		t.Fatalf("départ : %d %+v", w.Code, k)
	}
}

func TestShareHandleStable(t *testing.T) {
	a := shareHandle(id1)
	if a != shareHandle(id1) || !validHandle(a) || a == shareHandle(gOther) || shareHandle("") == a {
		t.Fatalf("poignée : %q %q", a, shareHandle(gOther))
	}
	for _, bad := range []string{"", strings.ToUpper(a), a[:15], a + "0", "0123456789abcdeg", "../../../../../x", "0123456789abcdé"} {
		if validHandle(bad) {
			t.Errorf("poignée acceptée : %q", bad)
		}
	}
}

func TestGuestTwoSharesTwoCookiesIndependent(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, _ := guestJoin(t, h, id1, "-p", "A")
	cb, _ := guestJoin(t, h, gOther, "-q", "B")
	hB := shareHandle(gOther)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	w := gReq{method: "GET", path: "/conv", jar: jar, hdr: map[string]string{shareHeader: hID1}}.do(h)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Déploie caddy") || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("A : %d %s", w.Code, w.Body)
	}
	w = gReq{method: "GET", path: "/conv", jar: jar, hdr: map[string]string{shareHeader: hB}}.do(h)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "SECRET autre session") || strings.Contains(w.Body.String(), "Déploie caddy") {
		t.Fatalf("B : %d %s", w.Code, w.Body)
	}
	// Cookie de A seul, en-tête de B : aucun cookie pour B, requête hôte (sans jeton : refusée), jamais les données de B.
	w = gReq{method: "GET", path: "/conv", jar: jar[:1], hdr: map[string]string{shareHeader: hB}}.do(h)
	if w.Code != 401 || strings.Contains(w.Body.String(), "SECRET") || cookieOf(w, gName) != nil {
		t.Fatalf("A sous B : %d %s", w.Code, w.Body)
	}
}

func TestGuestCookieSwapRejected(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, _ := guestJoin(t, h, id1, "-p", "")
	guestJoin(t, h, gOther, "-q", "")
	hB := shareHandle(gOther)
	w := gReq{method: "GET", path: "/conv", cookie: ca, share: hB}.do(h)
	if w.Code != 401 || strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "Déploie") {
		t.Fatalf("secret de A sous le nom de B : %d %s", w.Code, w.Body)
	}
	if k := cookieOf(w, guestCookieName(hB)); k == nil || k.MaxAge >= 0 {
		t.Fatalf("cookie échangé non effacé : %+v", k)
	}
	if w := (gReq{method: "GET", path: "/guest-me", cookie: ca, share: hB}).do(h); w.Code != 401 {
		t.Fatalf("guest-me échangé : %d", w.Code)
	}
}

func TestNoHeaderMeansHostEvenWithGuestCookies(t *testing.T) {
	t.Run("jeton", func(t *testing.T) {
		h := guestWeb(t, nil, modeTok)
		c, _ := guestIn(t, h)
		w := gReq{method: "GET", path: "/api", cookie: c, noShare: true, tokCookie: modeTok}.do(h)
		if w.Code != 200 || cookieOf(w, gName) != nil {
			t.Fatalf("hôte verrouillé : %d %s", w.Code, w.Body)
		}
		for _, p := range []string{"/conv", "/queue", "/api"} {
			if w := (gReq{method: "GET", path: p, cookie: c, noShare: true}).do(h); w.Code != 401 {
				t.Fatalf("%s sans en-tête ni jeton : %d", p, w.Code)
			}
		}
		if w := (gReq{method: "GET", path: "/guest-me", cookie: c, noShare: true, tokCookie: modeTok}).do(h); w.Code != 403 {
			t.Fatalf("guest-me sans en-tête : %d %s", w.Code, w.Body)
		}
	})
	t.Run("ouvert", func(t *testing.T) {
		h := guestWeb(t, nil, "")
		c, _ := guestIn(t, h)
		if w := (gReq{method: "GET", path: "/api", cookie: c, noShare: true}).do(h); w.Code != 200 {
			t.Fatalf("hôte verrouillé : %d %s", w.Code, w.Body)
		}
		if w := (gReq{method: "GET", path: "/guest-me", cookie: c, noShare: true}).do(h); w.Code != 403 {
			t.Fatalf("guest-me sans en-tête : %d", w.Code)
		}
	})
}

func TestGuestInvalidHeaderFormatIgnored(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	jar := []*http.Cookie{{Name: gName, Value: c}}
	for _, hd := range []string{strings.ToUpper(hID1), hID1 + "0", hID1[:15], "x", "-" + hID1[1:]} {
		w := gReq{method: "GET", path: "/conv", jar: jar, hdr: map[string]string{shareHeader: hd}}.do(h)
		if w.Code != 401 || cookieOf(w, gName) != nil {
			t.Fatalf("en-tête %q : %d", hd, w.Code)
		}
		if w := (gReq{method: "GET", path: "/api", jar: jar, tokCookie: modeTok, hdr: map[string]string{shareHeader: hd}}).do(h); w.Code != 200 {
			t.Fatalf("hôte, en-tête %q : %d", hd, w.Code)
		}
	}
}

type gItem struct{ Handle, Title, Mode string }

func TestGuestListReturnsOnlyValidAndClearsInvalid(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA := guestJoin(t, h, id1, "-p", "Titre A")
	cb, _ := guestJoin(t, h, gOther, "-q", "Titre B")
	shareSt.SetInviteMode(gOther, inv0(gOther), "ro")
	hX := shareHandle("33333333-3333-3333-3333-333333333333")
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hX), Value: ca}, {Name: guestCookieName("pasunepoignee"), Value: "x"},
		{Name: guestCookieName(shareHandle(gOther)), Value: cb}}
	w := gReq{method: "GET", path: "/guest-list", jar: jar}.do(h)
	var got []gItem
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("liste : %d %s", w.Code, w.Body)
	}
	want := map[gItem]bool{{hID1, "Titre A", "rw"}: true, {shareHandle(gOther), "Titre B", "ro"}: true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] || got[0] == got[1] {
		t.Fatalf("liste : %+v", got)
	}
	_, secA, _ := strings.Cut(ca, ".")
	_, secB, _ := strings.Cut(cb, ".")
	for _, leak := range []string{secA, secB, csrfA, id1, gOther, "-p", "-q", "dir", "csrf", "session"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("fuite %q : %s", leak, w.Body)
		}
	}
	for _, n := range []string{guestCookieName(hX), guestCookieName("pasunepoignee")} {
		if k := cookieOf(w, n); k == nil || k.MaxAge >= 0 {
			t.Errorf("%s non effacé : %+v", n, k)
		}
	}
	if cookieOf(w, gName) != nil || cookieOf(w, guestCookieName(shareHandle(gOther))) != nil {
		t.Fatal("cookie valide effacé")
	}
	// Plafond : au-delà de guestListMax cookies invités, les suivants ne sont pas examinés.
	var many []*http.Cookie
	for i := range guestListMax + 5 {
		many = append(many, &http.Cookie{Name: guestCookieName(shareHandle(strings.Repeat("9", i+1))), Value: ".faux"})
	}
	w = gReq{method: "GET", path: "/guest-list", jar: append(many, jar[0])}.do(h)
	if w.Code != 403 || len(w.Result().Cookies()) != guestListMax {
		t.Fatalf("plafond : %d, %d cookies", w.Code, len(w.Result().Cookies()))
	}
}

func TestGuestListNoCookie403(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	for _, q := range []gReq{{method: "GET", path: "/guest-list"}, {method: "GET", path: "/guest-list", tokCookie: modeTok}} {
		if w := q.do(h); w.Code != 403 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("sans cookie : %d %s", w.Code, w.Body)
		}
	}
}

func TestGuestMeHandleTitle(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestJoin(t, h, id1, "-p", "Mon titre")
	w := gReq{method: "GET", path: "/guest-me", cookie: c}.do(h)
	var me map[string]any
	json.Unmarshal(w.Body.Bytes(), &me)
	if w.Code != 200 || me["handle"] != hID1 || me["title"] != "Mon titre" {
		t.Fatalf("guest-me : %d %s", w.Code, w.Body)
	}
}

func TestGuestLeaveClearsOnlyThatShare(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA := guestJoin(t, h, id1, "-p", "")
	cb, csrfB := guestJoin(t, h, gOther, "-q", "")
	hB := shareHandle(gOther)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	if w := (gReq{method: "POST", path: "/guest-leave", jar: jar, csrf: csrfB, hdr: map[string]string{shareHeader: hID1}}).do(h); w.Code != 403 {
		t.Fatalf("csrf de B pour A : %d", w.Code)
	}
	w := gReq{method: "POST", path: "/guest-leave", jar: jar, csrf: csrfA, hdr: map[string]string{shareHeader: hID1}}.do(h)
	if k := cookieOf(w, gName); w.Code != 200 || k == nil || k.MaxAge >= 0 || cookieOf(w, guestCookieName(hB)) != nil {
		t.Fatalf("départ de A : %d %v", w.Code, w.Result().Cookies())
	}
	if w := (gReq{method: "GET", path: "/conv", cookie: cb, share: hB}).do(h); w.Code != 200 {
		t.Fatalf("B après départ de A : %d", w.Code)
	}
}

func TestEndedShareOnlyThatShareFallsThrough(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, _ := guestJoin(t, h, id1, "-p", "")
	cb, _ := guestJoin(t, h, gOther, "-q", "")
	hB := shareHandle(gOther)
	shareSt.End(id1)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	w := gReq{method: "GET", path: "/api", jar: jar, tokCookie: modeTok, hdr: map[string]string{shareHeader: hID1}}.do(h)
	if k := cookieOf(w, gName); w.Code != 200 || k == nil || k.MaxAge >= 0 || cookieOf(w, guestCookieName(hB)) != nil {
		t.Fatalf("A terminé : %d %v", w.Code, w.Result().Cookies())
	}
	if w := (gReq{method: "GET", path: "/conv", jar: jar, hdr: map[string]string{shareHeader: hB}}).do(h); w.Code != 200 || !strings.Contains(w.Body.String(), "SECRET autre") {
		t.Fatalf("B : %d", w.Code)
	}
}

// L'interface invité (page et fichiers) se charge sans en-tête de partage ; en mode jeton, seul un cookie invité valide l'ouvre.
func TestInviteRedirectsToHandleURL(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	for _, p := range []string{"/guest/" + hID1, "/guest"} {
		if w := (gReq{method: "GET", path: p, cookie: c, noShare: true}).do(h); w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("%s invité : %d", p, w.Code)
		}
		if w := (gReq{method: "GET", path: p}).do(h); w.Code != 401 {
			t.Fatalf("%s anonyme : %d", p, w.Code)
		}
		if w := (gReq{method: "GET", path: p, cookie: ".faux", noShare: true}).do(h); w.Code != 401 {
			t.Fatalf("%s cookie faux : %d", p, w.Code)
		}
	}
	if w := (gReq{method: "GET", path: "/guest/" + hID1, tokCookie: modeTok}).do(h); w.Code != 200 {
		t.Fatalf("hôte : %d", w.Code)
	}
}

// Passerelle : /guest-list juge un cookie de home enfant d'après son fichier, puis le fait revalider par l'enfant.
func TestGuestListChildRevalidated(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "Enfant", "")
	sec, _, _, _ := cs.Redeem(tok)
	tok2, _, _ := cs.CreateInvite(gOther, "-q", "", "")
	sec2, _, _, _ := cs.Redeem(tok2)
	refuse, plain, down := false, false, false
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := r.Header.Get(shareHeader)
		if plain { // refus de childAuthorized ou de la garde Host : pas un jugement du secret
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if refuse {
			guestDeny(w, 403, "srv.share_guest.refuse")
			return
		}
		if down {
			guestDeny(w, 503, "srv.share.indisponible")
			return
		}
		if r.URL.Path != "/guest-me" || r.Header.Get(guestHeader) == "" || r.Header.Get("Cookie") != "" || r.Header.Get(guestQuietHeader) != "1" {
			w.WriteHeader(403)
			return
		}
		w.Write([]byte(`{"handle":"` + hd + `","title":"Enfant","mode":"rw","id":"` + id1 + `","csrf":"c"}`))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(h Home) (*homeProc, error) {
		return &homeProc{dir: h.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	jar := []*http.Cookie{{Name: gName, Value: "perso." + sec}, {Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec}}
	w := gReq{method: "GET", path: "/guest-list", jar: jar}.do(h)
	var got []gItem
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || len(got) != 1 || got[0] != (gItem{hID1, "Enfant", "rw"}) || strings.Contains(w.Body.String(), id1) || strings.Contains(w.Body.String(), sec) {
		t.Fatalf("liste : %d %s", w.Code, w.Body)
	}
	if k := cookieOf(w, guestCookieName(shareHandle(gOther))); k == nil || k.MaxAge >= 0 {
		t.Fatalf("cookie échangé non effacé : %+v", k)
	}
	plain = true
	w = gReq{method: "GET", path: "/guest-list", jar: []*http.Cookie{{Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec2}}}.do(h)
	if w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("403 brut de l'enfant : %d %v", w.Code, w.Result().Cookies())
	}
	plain, down = false, true
	w = gReq{method: "GET", path: "/guest-list", jar: []*http.Cookie{{Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec2}}}.do(h)
	if w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("partages indisponibles dans l'enfant : %d %v", w.Code, w.Result().Cookies())
	}
	down, refuse = false, true
	w = gReq{method: "GET", path: "/guest-list", jar: []*http.Cookie{{Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec2}}}.do(h)
	if k := cookieOf(w, guestCookieName(shareHandle(gOther))); w.Code != 403 || k == nil || k.MaxAge >= 0 {
		t.Fatalf("refus de l'enfant : %d %+v", w.Code, k)
	}
}

// Le statique sans en-tête n'est servi qu'aux motifs de guestStatics sur un chemin canonique ; le reste va au jeton (401).
func TestGuestStaticExceptionTable(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	asset, _ := filepath.Glob("ui/dist/assets/*.js")
	if len(asset) == 0 {
		t.Skip("ui/dist absent")
	}
	jar := []*http.Cookie{{Name: gName, Value: c}}
	for _, p := range []string{"/sw.js", "/", "/guest/", "/assets/", "/guest/x/y", "/assets/%2e%2e/x", "/assets/../x", "/guest/..%2f", "/conv", "/api"} {
		if w := (gReq{method: "GET", path: p, jar: jar}).do(h); w.Code != 401 {
			t.Errorf("%s servi : %d", p, w.Code)
		}
	}
	for _, p := range []string{"/guest", "/guest/" + hID1, "/assets/" + filepath.Base(asset[0]), "/icons/x", "/favicon.ico", "/manifest.webmanifest"} {
		if w := (gReq{method: "GET", path: p, jar: jar}).do(h); w.Code == 401 {
			t.Errorf("%s refusé", p)
		}
	}
	if w := (gReq{method: "GET", path: "/assets/" + filepath.Base(asset[0])}).do(h); w.Code != 401 {
		t.Fatalf("asset anonyme : %d", w.Code)
	}
}

// Des cookies d'un home enfant forgés coûtent une lecture de fichier chacun : au plus guestChildMax par requête, après ceux du home principal.
func TestGuestChildCookieChecksCapped(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	addHomes(t, "perso")
	n := 0
	old := guestOpenShares
	t.Cleanup(func() { guestOpenShares = old })
	guestOpenShares = func(d string) (*shareStore, error) { n++; return old(d) }
	var jar []*http.Cookie
	for i := range guestListMax {
		jar = append(jar, &http.Cookie{Name: guestCookieName(shareHandle(strings.Repeat("8", i+1))), Value: "perso.faux" + strings.Repeat("x", i)})
	}
	all := append([]*http.Cookie{jar[0], jar[1], {Name: gName, Value: c}}, jar[2:guestListMax-1]...)
	w := gReq{method: "GET", path: "/guest-list", jar: all}.do(h)
	var got []gItem
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || len(got) != 1 || got[0].Handle != hID1 || n > guestChildMax {
		t.Fatalf("liste : %d %s, %d lectures", w.Code, w.Body, n)
	}
	n = 0
	if w := (gReq{method: "GET", path: "/guest", jar: jar}).do(h); w.Code != 401 || n > guestChildMax {
		t.Fatalf("statique : %d, %d lectures", w.Code, n)
	}
	n = 0
	if w := (gReq{method: "GET", path: "/guest", jar: all}).do(h); w.Code != 200 || n != 0 {
		t.Fatalf("statique, cookie principal d'abord : %d, %d lectures", w.Code, n)
	}
}

// LastSeen ne bouge que sur une vraie requête invitée (en-tête de partage), jamais sur /guest-list ni le statique.
func TestGuestLastSeenOnlyOnGuardedRequests(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	shareSt.mu.Lock()
	shareSt.m[id1].Invites[0].LastSeen = 1
	shareSt.mu.Unlock()
	jar := []*http.Cookie{{Name: gName, Value: c}}
	for _, p := range []string{"/guest-list", "/guest", "/guest/" + hID1, "/favicon.ico"} {
		gReq{method: "GET", path: p, jar: jar}.do(h)
	}
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen != 1 {
		t.Fatalf("LastSeen touché sans requête invitée : %d", sh.Invites[0].LastSeen)
	}
	gReq{method: "GET", path: "/conv", cookie: c}.do(h)
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen <= 1 {
		t.Fatalf("LastSeen non mis à jour : %d", sh.Invites[0].LastSeen)
	}
}

// <img> ne peut pas porter d'en-tête : /conv-image seul accepte la poignée en paramètre share (l'en-tête l'emporte).
func TestGuestConvImageShareQuery(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA := guestJoin(t, h, id1, "-p", "")
	cb, _ := guestJoin(t, h, gOther, "-q", "")
	hB := shareHandle(gOther)
	put(t, filepath.Join(root, "-q", gOther+".jsonl"), `{"type":"user","cwd":"/q","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":[{"type":"text","text":"x"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+png1+`"}}]}}`+"\n", old)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	img := func(share string, hdr map[string]string, jar []*http.Cookie) *httptest.ResponseRecorder {
		return gReq{method: "GET", path: "/conv-image?id=" + id1 + "&dir=-p&o=0&k=1&share=" + share, jar: jar, hdr: hdr}.do(h)
	}
	if w := img(hB, nil, jar); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("image de B : %d %v", w.Code, w.Header())
	}
	// Partage A choisi : id/dir imposés à A, dont la ligne 0 n'est pas une image (404 invité, pas 401 hôte).
	if w := img(hID1, nil, jar); w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("image de A : %d", w.Code)
	}
	if w := img(hB, map[string]string{shareHeader: hID1}, jar); w.Code != 404 {
		t.Fatalf("l'en-tête doit l'emporter : %d", w.Code)
	}
	for _, s := range []string{strings.ToUpper(hB), hB + "0", "x", shareHandle("33333333-3333-3333-3333-333333333333")} {
		if w := img(s, nil, jar); w.Code != 401 {
			t.Fatalf("share %q : %d", s, w.Code)
		}
	}
	if w := img(hB, nil, jar[:1]); w.Code != 401 {
		t.Fatalf("sans cookie de B : %d", w.Code)
	}
	w := img(hB, nil, []*http.Cookie{{Name: guestCookieName(hB), Value: ca}})
	if k := cookieOf(w, guestCookieName(hB)); w.Code != 401 || k == nil || k.MaxAge >= 0 {
		t.Fatalf("secret de A sous B : %d %+v", w.Code, k)
	}
	// Aucune autre route ne lit share.
	for _, p := range []string{"/conv?share=" + hID1, "/queue?share=" + hID1, "/guest-me?share=" + hID1} {
		if w := (gReq{method: "GET", path: p, jar: jar}).do(h); w.Code != 401 {
			t.Fatalf("%s : %d", p, w.Code)
		}
	}
	if w := (gReq{method: "POST", path: "/queue-add?share=" + hID1, body: `{"text":"m"}`, jar: jar, csrf: csrfA}).do(h); w.Code == 200 || len(queueSt.List(id1)) != 0 {
		t.Fatalf("POST avec share : %d", w.Code)
	}
}

// Passerelle : la poignée en paramètre suit le même relais que l'en-tête ; l'enfant reçoit l'en-tête, jamais share.
func TestGuestConvImageShareQueryRelayed(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	var path, share, guest string
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, share, guest = r.URL.RequestURI(), r.Header.Get(shareHeader), r.Header.Get(guestHeader)
		w.Write([]byte("img"))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(h Home) (*homeProc, error) {
		return &homeProc{dir: h.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	jar := []*http.Cookie{{Name: gName, Value: "perso." + sec}}
	w := gReq{method: "GET", path: "/conv-image?o=0&k=1&share=" + hID1, jar: jar}.do(h)
	if w.Code != 200 || w.Body.String() != "img" || share != hID1 || guest != sec || strings.Contains(path, "share=") || !strings.HasPrefix(path, "/conv-image?") {
		t.Fatalf("relais : %d %q %q %q", w.Code, path, share, guest)
	}
}

// Quatre partages d'un home enfant, les trois plus anciens terminés : la page /guest/<poignée> du quatrième s'ouvre (son
// cookie est lu en premier) ; /guest passe par le balayage plafonné, qui efface les morts et sert au chargement suivant.
func TestGuestStaticHandleFirstAndClearsDead(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	var jar []*http.Cookie
	var hds []string
	for i := range 4 {
		sess := strings.Repeat(string(rune('a'+i)), 8) + "-1111-1111-1111-111111111111"
		tok, _, _ := cs.CreateInvite(sess, "-p", "", "")
		sec, _, _, _ := cs.Redeem(tok)
		hds = append(hds, shareHandle(sess))
		jar = append(jar, &http.Cookie{Name: guestCookieName(hds[i]), Value: "perso." + sec})
	}
	for i := range 3 {
		sess := strings.Repeat(string(rune('a'+i)), 8) + "-1111-1111-1111-111111111111"
		if err := cs.End(sess); err != nil {
			t.Fatal(err)
		}
	}
	if w := (gReq{method: "GET", path: "/guest/" + hds[3], jar: jar}).do(h); w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("page du partage vivant : %d", w.Code)
	}
	w := gReq{method: "GET", path: "/guest", jar: jar}.do(h)
	if w.Code != 401 || cookieOf(w, jar[3].Name) != nil {
		t.Fatalf("/guest, trois morts d'abord : %d %v", w.Code, w.Result().Cookies())
	}
	for _, c := range jar[:3] {
		if k := cookieOf(w, c.Name); k == nil || k.MaxAge >= 0 {
			t.Fatalf("cookie mort non effacé : %s", c.Name)
		}
	}
	if w := (gReq{method: "GET", path: "/guest", jar: jar[3:]}).do(h); w.Code != 200 {
		t.Fatalf("/guest après effacement : %d", w.Code)
	}
	w = gReq{method: "GET", path: "/guest/" + hds[0], jar: jar}.do(h)
	if k := cookieOf(w, jar[0].Name); w.Code != 401 || k == nil || k.MaxAge >= 0 || cookieOf(w, jar[3].Name) != nil {
		t.Fatalf("page d'un partage mort : %d %v", w.Code, w.Result().Cookies())
	}
}

func noClear(t *testing.T, what string, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, k := range w.Result().Cookies() {
		if k.MaxAge < 0 {
			t.Errorf("%s : cookie %s effacé sans réponse certaine", what, k.Name)
		}
	}
}

// Ne pas pouvoir répondre n'est pas « secret invalide » : aucun effacement (le lien était à usage unique), aucun accès invité.
func TestGuestUnsureNeverClears(t *testing.T) {
	probe := func(t *testing.T, h http.Handler, c string) {
		t.Helper()
		jar := []*http.Cookie{{Name: gName, Value: c}}
		for _, q := range []gReq{{method: "GET", path: "/guest/" + hID1, jar: jar}, {method: "GET", path: "/guest", jar: jar}, {method: "GET", path: "/guest-list", jar: jar}, {method: "GET", path: "/conv", cookie: c}} {
			w := q.do(h)
			noClear(t, q.path, w)
			if w.Code == 200 {
				t.Errorf("%s : servi sans réponse certaine", q.path)
			}
		}
	}
	t.Run("fichier des partages illisible", func(t *testing.T) {
		h := guestWeb(t, nil, modeTok)
		addHomes(t, "perso")
		hm, _ := homeByName("perso")
		cs, _ := openShares(hm.Dir)
		tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
		sec, _, _, _ := cs.Redeem(tok)
		old := guestOpenShares
		t.Cleanup(func() { guestOpenShares = old })
		guestOpenShares = func(string) (*shareStore, error) { return nil, os.ErrPermission }
		probe(t, h, "perso."+sec)
	})
	t.Run("partages indisponibles", func(t *testing.T) {
		h := guestWeb(t, nil, modeTok)
		c, _ := guestIn(t, h)
		shareSt = nil
		probe(t, h, c)
	})
	t.Run("fichier des homes illisible", func(t *testing.T) {
		h := guestWeb(t, nil, modeTok)
		addHomes(t, "perso")
		hm, _ := homeByName("perso")
		cs, _ := openShares(hm.Dir)
		tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
		sec, _, _, _ := cs.Redeem(tok)
		if err := os.WriteFile(homesFile(), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		probe(t, h, "perso."+sec)
	})
}

// Réponses certaines : partage terminé, remplacé, home inconnu, secret faux ; effacés sur les pages et la requête gardée.
func TestGuestDefinitiveStillClears(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, _ := guestIn(t, h)
	shareSt.Regen(id1, inv0(id1)) // régénéré : l'ancien cookie meurt
	for _, v := range []string{c, "inconnu." + strings.Split(c, ".")[1], ".faux"} {
		jar := []*http.Cookie{{Name: gName, Value: v}}
		for _, q := range []gReq{{method: "GET", path: "/guest/" + hID1, jar: jar}, {method: "GET", path: "/guest", jar: jar}, {method: "GET", path: "/guest-list", jar: jar}, {method: "GET", path: "/conv", cookie: v}} {
			w := q.do(h)
			if k := cookieOf(w, gName); k == nil || k.MaxAge >= 0 || w.Code == 200 {
				t.Errorf("%s %q : %d %+v", q.path, v, w.Code, k)
			}
		}
	}
}

// Les fichiers publics, mis en cache, ne portent jamais d'effacement ; les pages qui effacent sont no-store.
func TestGuestStaticClearsOnlyOnPages(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	jar := []*http.Cookie{{Name: gName, Value: ".mort"}}
	paths := []string{"/icons/x", "/favicon.ico", "/manifest.webmanifest"}
	if asset, _ := filepath.Glob("ui/dist/assets/*.js"); len(asset) > 0 {
		paths = append(paths, "/assets/"+filepath.Base(asset[0]))
	} else {
		t.Log("ui/dist absent : /assets/ non vérifié")
	}
	for _, p := range paths {
		if w := (gReq{method: "GET", path: p, jar: jar}).do(h); len(w.Result().Cookies()) != 0 {
			t.Errorf("%s : %v", p, w.Result().Cookies())
		}
	}
	for _, p := range []string{"/guest", "/guest/" + hID1} {
		w := gReq{method: "GET", path: p, jar: jar, hdr: map[string]string{"Accept": "text/html"}}.do(h)
		if k := cookieOf(w, gName); k == nil || k.MaxAge >= 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s : %+v %q", p, k, w.Header().Get("Cache-Control"))
		}
	}
}

// Une seule lecture du fichier des homes : une seconde lecture ratée (rendue vide par readHomesFile) ne doit pas faire croire
// à un home inconnu et effacer un cookie valide.
func TestGuestHomesReadOnce(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	b, _ := json.Marshal(map[string]any{"homes": []Home{{Name: "perso", Dir: hm.Dir}}})
	os.Remove(homesFile()) // toute autre lecture que la première ne verrait plus aucun home
	n := 0
	old := guestReadHomes
	t.Cleanup(func() { guestReadHomes = old })
	guestReadHomes = func() ([]byte, error) { n++; return b, nil }
	w := gReq{method: "GET", path: "/guest/" + hID1, jar: []*http.Cookie{{Name: gName, Value: "perso." + sec}}}.do(h)
	if w.Code != 200 || len(w.Result().Cookies()) != 0 || n != 1 {
		t.Fatalf("page : %d %v, %d lectures", w.Code, w.Result().Cookies(), n)
	}
}

// Enfant : un magasin de partages absent n'est pas un refus du secret (503) ; un magasin lu sans ce secret, si (403 ok:false).
func TestGuestChildUnsureAnswers503(t *testing.T) {
	h := guestWeb(t, nil, "")
	oc := childToken
	t.Cleanup(func() { childToken = oc })
	childToken = "ctok"
	hdr := map[string]string{childHeader: "ctok", guestHeader: "secret", shareHeader: hID1}
	w := gReq{method: "GET", path: "/guest-me", hdr: hdr}.do(h)
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("secret inconnu : %d %s", w.Code, w.Body)
	}
	shareSt = nil
	if w := (gReq{method: "GET", path: "/guest-me", hdr: hdr}).do(h); w.Code != 503 || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("magasin absent : %d %s", w.Code, w.Body)
	}
}

// La garde efface en enveloppant la réponse : un handler hôte qui diffuse doit garder Flush.
func TestNoStoreWriterFlushes(t *testing.T) {
	rec := httptest.NewRecorder()
	var w http.ResponseWriter = &noStoreWriter{ResponseWriter: rec}
	f, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("pas de Flush")
	}
	f.Flush()
	if !rec.Flushed || rec.Header().Get("Cache-Control") != "no-store" || http.NewResponseController(w).Flush() != nil {
		t.Fatalf("flush : %v %q", rec.Flushed, rec.Header().Get("Cache-Control"))
	}
}

func TestGuestSecretPath(t *testing.T) {
	for p, want := range map[string]bool{"": false, "src/a.go": false, ".git/config": true, "a/.env.local": true, "k/server.PEM": true, "id_rsa": true, "envx": false, ".ssh/x": true, "a/my-secrets.txt": true, "b/.aws/credentials": true} {
		if guestSecretPath(p) != want {
			t.Errorf("%q: want %v", p, want)
		}
	}
}

func TestGuestFsEscapeNeverServed(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf := guestIn(t, h)
	for _, p := range []string{"/etc/passwd", "../x", "a/../../x", "%2e%2e/x"} {
		w := gReq{method: "GET", path: "/fs-file?path=" + p, cookie: c, csrf: csrf}.do(h)
		if w.Code == 200 {
			t.Errorf("%s : %d %s", p, w.Code, w.Body)
		}
	}
}

func TestGuestSessionPath(t *testing.T) {
	const sid = "11111111-1111-1111-1111-111111111111"
	for in, want := range map[string]string{
		"/v1/sessions/other/conversation":      "/v1/sessions/" + sid + "/conversation",
		"/v1/sessions/other":                   "/v1/sessions/" + sid,
		"/v1/sessions/other/loaded/files/a.md": "/v1/sessions/" + sid + "/loaded/files/a.md",
		"/v1/queue/x":                          "/v1/queue/x",
	} {
		u := &url.URL{Path: in}
		guestSessionPath(u, sid)
		if u.Path != want {
			t.Errorf("%s: got %s, want %s", in, u.Path, want)
		}
	}
}
