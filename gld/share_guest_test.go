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
func guestWeb(t *testing.T, localAuthStore *authStore, tok string) http.Handler {
	t.Helper()
	resetAuth(t, localAuthStore)
	resetTokenTries()
	oTok, oSD, oQD, oSS, oQS := hostToken, shareDir, queueDir, shareSt, queueSt
	t.Cleanup(func() { hostToken, shareDir, queueDir, shareSt, queueSt = oTok, oSD, oQD, oSS, oQS; resetTokenTries() })
	sd, qd := t.TempDir(), t.TempDir()
	shareDir, queueDir = func() string { return sd }, func() string { return qd }
	hostToken = tok
	guestAdds.Lock()
	guestAdds.by = map[string]*tryWin{}
	guestAdds.Unlock()
	handler, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "projects", "-q", gOther+".jsonl"),
		`{"type":"user","cwd":"/q","timestamp":"2026-01-01T00:00:00Z","message":{"content":"SECRET autre session"}}`+"\n", old)
	return handler
}

// gReq : cookie est la valeur du cookie invité de la poignée share (par défaut celle de id1), envoyée avec l'en-tête de
// partage sauf noShare ; jar ajoute des cookies bruts.
type gReq struct {
	method, path, body, cookie, csrf, tokCookie, share string
	noShare                                            bool
	hdr                                                map[string]string
	jar                                                []*http.Cookie
}

func (localGReq gReq) do(handler http.Handler) *httptest.ResponseRecorder {
	request := httptest.NewRequest(localGReq.method, localGReq.path, strings.NewReader(localGReq.body))
	request.Host = testHost
	if localGReq.share == "" {
		localGReq.share = hID1
	}
	if localGReq.cookie != "" {
		request.AddCookie(&http.Cookie{Name: guestCookieName(localGReq.share), Value: localGReq.cookie})
		if !localGReq.noShare {
			request.Header.Set(shareHeader, localGReq.share)
		}
	}
	for _, cookie := range localGReq.jar {
		request.AddCookie(cookie)
	}
	if localGReq.tokCookie != "" {
		request.AddCookie(&http.Cookie{Name: tokenCookie, Value: localGReq.tokCookie})
	}
	if localGReq.csrf != "" {
		request.Header.Set(csrfHeader, localGReq.csrf)
	}
	for key, value := range localGReq.hdr {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// guestJoin : partage de sess créé, lien ouvert ; rend le cookie et l'en-tête CSRF de l'invité.
func guestJoin(t *testing.T, handler http.Handler, sess, dir, title string) (cookie, csrf string) {
	t.Helper()
	tok, _, err := shareSt.CreateInvite(sess, dir, title, "")
	if err != nil {
		t.Fatal(err)
	}
	hd := shareHandle(sess)
	recorder := gReq{method: "POST", path: "/invite/main/" + tok}.do(handler)
	localCookie := cookieOf(recorder, guestCookieName(hd))
	if recorder.Code != 303 || localCookie == nil || recorder.Header().Get("Location") != "/guest/"+hd {
		t.Fatalf("invite : %d %q %v", recorder.Code, recorder.Header().Get("Location"), recorder.Result().Cookies())
	}
	recorder = gReq{method: "GET", path: "/v1/guest/me", cookie: localCookie.Value, share: hd}.do(handler)
	var me struct{ ID, Dir, Mode, CSRF string }
	if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &me) != nil || me.CSRF == "" || me.ID != sess {
		t.Fatalf("guest-me : %d %s", recorder.Code, recorder.Body)
	}
	return localCookie.Value, me.CSRF
}

// inv0 : la première invitation du partage de sess.
func inv0(sess string) string {
	shareSt.mu.Lock()
	defer shareSt.mu.Unlock()
	return shareSt.m[sess].Invites[0].ID
}

func guestIn(t *testing.T, handler http.Handler) (cookie, csrf string) {
	t.Helper()
	return guestJoin(t, handler, id1, "-p", "")
}

func TestGuestRedeemSetsCookieAndHidesToken(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	recorder := gReq{method: "POST", path: "/invite/main/" + tok}.do(handler)
	cookie := cookieOf(recorder, gName)
	if recorder.Code != 303 || recorder.Header().Get("Location") != "/guest/"+hID1 || strings.Contains(recorder.Header().Get("Location"), tok) {
		t.Fatalf("redirection : %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || strings.Contains(cookie.Value, tok) || !strings.HasPrefix(cookie.Value, ".") {
		t.Fatalf("cookie : %+v", cookie)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache : %q", recorder.Header().Get("Cache-Control"))
	}
	if recorder := (gReq{method: "POST", path: "/invite/main/" + tok}).do(handler); recorder.Code != 403 || cookieOf(recorder, gName) != nil {
		t.Fatalf("second usage : %d", recorder.Code)
	}
}

func TestGuestReloadWorksOtherBrowserRefused(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	localCookie := cookieOf(gReq{method: "POST", path: "/invite/main/" + tok}.do(handler), gName)
	for range 2 {
		recorder := gReq{method: "GET", path: "/v1/guest/me", cookie: localCookie.Value}.do(handler)
		var me map[string]any
		json.Unmarshal(recorder.Body.Bytes(), &me)
		if recorder.Code != 200 || me["id"] != id1 || me["dir"] != "-p" || me["mode"] != "rw" || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("rechargement : %d %s", recorder.Code, recorder.Body)
		}
	}
	if recorder := (gReq{method: "POST", path: "/invite/main/" + tok}).do(handler); recorder.Code != 403 {
		t.Fatalf("autre navigateur : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/guest/me"}).do(handler); recorder.Code == 200 {
		t.Fatalf("sans cookie : %d", recorder.Code)
	}
}

func TestGuestCannotChangeSession(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	for _, localPath := range []string{"/v1/sessions/" + gOther + "/conversation?dir=-q", "/v1/sessions/" + id1 + "/conversation?dir=-q", "/v1/sessions/" + gOther + "/conversation?id=" + gOther + "&dir=-q&id=" + gOther} {
		recorder := gReq{method: "GET", path: localPath, cookie: content}.do(handler)
		if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "Déploie caddy") || strings.Contains(recorder.Body.String(), "SECRET") {
			t.Fatalf("%s : %d %s", localPath, recorder.Code, recorder.Body)
		}
	}
	for _, localBody := range []string{`{"id":"` + gOther + `","text":"a"}`, `{"ID":"` + gOther + `","Id":"` + gOther + `","dir":"-q","text":"b"}`} {
		if recorder := (gReq{method: "POST", path: "/v1/sessions/" + gOther + "/queue?id=" + gOther, body: localBody, cookie: content, csrf: csrf}).do(handler); recorder.Code != 201 {
			t.Fatalf("%s : %d %s", localBody, recorder.Code, recorder.Body)
		}
	}
	if count, total := len(queueSt.List(id1)), len(queueSt.List(gOther)); count != 2 || total != 0 {
		t.Fatalf("partagée %d, autre %d", count, total)
	}
	localRecorder := gReq{method: "GET", path: "/v1/sessions/" + gOther + "/queue", cookie: content}.do(handler)
	if !strings.Contains(localRecorder.Body.String(), `"text":"a"`) {
		t.Fatalf("file : %s", localRecorder.Body)
	}
}

func TestGuestNonWhitelistedForbidden(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	for _, rt := range [][2]string{
		{"POST", "/v1/sessions/" + id1 + "/keystrokes"}, {"POST", "/v1/trust/answers"}, {"POST", "/v1/sessions/" + id1 + "/chat"},
		{"GET", "/v1/auth/me"}, {"GET", "/v1/config"}, {"PATCH", "/v1/sessions/" + id1 + "/queue/x"}, {"POST", "/v1/invites"}, {"DELETE", "/v1/shares/" + id1},
		{"GET", "/v1/state"}, {"GET", "/v1/sessions/" + id1 + "/conversation/document?path=/etc/x.md"}, {"POST", "/v1/sessions/" + id1 + "/messages"}, {"GET", "/v1/homes"},
		{"GET", "//v1/sessions/" + id1 + "/conversation"}, {"GET", "/v1/sessions/" + id1 + "/conversation/"}, {"GET", "/v1/sessions/" + id1 + "/%63onversation"},
		{"GET", "/v1/sessions/" + id1 + "/conversation/../../../state"}, {"POST", "/v1/sessions/" + id1 + "/conversation"}, {"GET", "/v1/sessions/" + id1 + "/queue/x"},
		{"DELETE", "/v1/sessions/" + id1 + "/conversation"}, {"PUT", "/v1/sessions/" + id1 + "/process"}, {"DELETE", "/v1/sessions"}, {"GET", "/v1/sessions"},
		{"GET", "/invite/../v1/state"}, {"GET", "/v1/sessions/" + id1 + "/screen"}, {"POST", "/v1/fs/entries/content"}, {"GET", "/v1/fs/entries/content?path=.env"}, {"GET", "/v1/fs/tree?path=.git"}, {"GET", "/ide-search"},
	} {
		recorder := gReq{method: rt[0], path: rt[1], body: `{"id":"` + id1 + `"}`, cookie: content, csrf: csrf}.do(handler)
		if recorder.Code != 403 || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s %s : %d %s", rt[0], rt[1], recorder.Code, recorder.Body)
		}
	}
}

func TestGuestReadOnlyCannotQueue(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	shareSt.SetInviteMode(id1, inv0(id1), "ro")
	if recorder := (gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"x"}`, cookie: content, csrf: csrf}).do(handler); recorder.Code != 403 {
		t.Fatalf("lecture seule : %d", recorder.Code)
	}
	if len(queueSt.List(id1)) != 0 {
		t.Fatal("file modifiée")
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}).do(handler); recorder.Code != 200 {
		t.Fatalf("lecture : %d", recorder.Code)
	}
}

func TestGuestCanRemoveOnlyOwnItems(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	host, _ := queueSt.Add(id1, "host", "", "de l'hôte")
	recorder := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"de l'invité"}`, cookie: content, csrf: csrf}.do(handler)
	var mine struct{ Item QItem }
	json.Unmarshal(recorder.Body.Bytes(), &mine)
	if recorder := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + host.ID, cookie: content, csrf: csrf}).do(handler); recorder.Code != 403 {
		t.Fatalf("item de l'hôte : %d", recorder.Code)
	}
	if recorder := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + mine.Item.ID, cookie: content, csrf: csrf}).do(handler); recorder.Code != 200 {
		t.Fatalf("son item : %d %s", recorder.Code, recorder.Body)
	}
	if qItems := queueSt.List(id1); len(qItems) != 1 || qItems[0].ID != host.ID {
		t.Fatalf("file : %+v", qItems)
	}
}

func TestGuestRateLimit(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	for index := range 11 {
		recorder := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"m"}`, cookie: content, csrf: csrf}.do(handler)
		if want := map[bool]int{true: 429, false: 201}[index == 10]; recorder.Code != want {
			t.Fatalf("ajout %d : %d", index+1, recorder.Code)
		}
	}
}

func TestGuestAuthorForgedOverwritten(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"m","author":"host"}`, cookie: content, csrf: csrf, hdr: map[string]string{"X-Gld-Author": "host"}}.do(handler)
	if qItems := queueSt.List(id1); len(qItems) != 1 || qItems[0].Author != "guest" {
		t.Fatalf("auteur : %+v", qItems)
	}
}

func TestGuestPostNeedsCSRF(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	for _, bad := range []string{"", "faux", strings.ToUpper(csrf)} {
		if recorder := (gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"m"}`, cookie: content, csrf: bad}).do(handler); recorder.Code != 403 {
			t.Fatalf("csrf %q : %d", bad, recorder.Code)
		}
	}
	localRecorder := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"m"}`, cookie: content, csrf: csrf, hdr: map[string]string{"Origin": "https://ailleurs.example", "Sec-Fetch-Site": "cross-site"}}.do(handler)
	if localRecorder.Code != 403 || len(queueSt.List(id1)) != 0 {
		t.Fatalf("autre origine : %d", localRecorder.Code)
	}
}

func TestGuestInviteRateLimit(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	for index := range 11 {
		recorder := gReq{method: "POST", path: "/invite/main/devine" + string(rune('a'+index))}.do(handler)
		if want := map[bool]int{true: 429, false: 403}[index == 10]; recorder.Code != want {
			t.Fatalf("essai %d : %d", index+1, recorder.Code)
		}
	}
}

func TestEndedShareCookieFallsThroughToHost(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	shareSt.End(id1)
	recorder := gReq{method: "GET", path: "/v1/state", cookie: content, tokCookie: modeTok}.do(handler)
	if recorder.Code != 200 {
		t.Fatalf("hôte : %d %s", recorder.Code, recorder.Body)
	}
	if cookie := cookieOf(recorder, gName); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("cookie non effacé : %+v", cookie)
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", cookie: "nimporte.quoi"}).do(handler); recorder.Code != 401 {
		t.Fatalf("cookie invalide sans jeton : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}).do(handler); recorder.Code != 401 {
		t.Fatalf("partage terminé : %d", recorder.Code)
	}
}

func TestGuestWorksInSingleUserTokenMode(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}).do(handler); recorder.Code != 200 || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invité : %d %q", recorder.Code, recorder.Header().Get("Cache-Control"))
	}
	if recorder := (gReq{method: "GET", path: "/v1/state"}).do(handler); recorder.Code != 401 {
		t.Fatalf("hôte sans jeton : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(handler); recorder.Code != 200 {
		t.Fatalf("hôte : %d", recorder.Code)
	}
}

func TestGuestWorksInMultiUserMode(t *testing.T) {
	authStore := testStore(t)
	handler := guestWeb(t, authStore, modeTok)
	id, err := authStore.AddUser("hote", testPw, true)
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := fullSession(t, authStore, id)
	content, _ := guestIn(t, handler)
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}).do(handler); recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "Déploie caddy") {
		t.Fatalf("invité : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", cookie: content}).do(handler); recorder.Code != 403 {
		t.Fatalf("invité hors liste : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/admin/users"}).do(handler); recorder.Code != 401 {
		t.Fatalf("hôte sans session : %d", recorder.Code)
	}
	request := httptest.NewRequest("GET", "/v1/admin/users", nil)
	request.Host = testHost
	request.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
	localRecorder := httptest.NewRecorder()
	handler.ServeHTTP(localRecorder, request)
	if localRecorder.Code != 200 {
		t.Fatalf("hôte : %d %s", localRecorder.Code, localRecorder.Body)
	}
}

// Passerelle : un cookie invité d'un home enfant est relayé à l'enfant avec le secret en en-tête, jamais servi ici.
func TestGuestRelayedToChildHome(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	type seen struct{ path, guest, cookie, https, share, quiet string }
	var got []seen
	child := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		got = append(got, seen{request.URL.RequestURI(), request.Header.Get(guestHeader), request.Header.Get("Cookie"), request.Header.Get(guestHTTPSHeader), request.Header.Get(shareHeader), request.Header.Get(guestQuietHeader)})
		if strings.HasPrefix(request.URL.Path, "/invite/") {
			responseWriter.WriteHeader(303)
			return
		}
		responseWriter.Write([]byte("enfant"))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(home Home) (*homeProc, error) {
		return &homeProc{dir: home.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	content := "perso." + sec
	if recorder := (gReq{method: "GET", path: "/v1/sessions/x/conversation", cookie: content, hdr: map[string]string{guestQuietHeader: "1"}}).do(handler); recorder.Code != 200 || recorder.Body.String() != "enfant" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("relais : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", cookie: content}).do(handler); recorder.Code != 403 {
		t.Fatalf("hors liste : %d", recorder.Code)
	}
	if recorder := (gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{}`, cookie: content, csrf: "faux"}).do(handler); recorder.Code != 403 {
		t.Fatalf("csrf : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", cookie: "perso.faux", tokCookie: modeTok}).do(handler); recorder.Code != 200 || cookieOf(recorder, gName) == nil {
		t.Fatalf("secret inconnu : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", tokCookie: modeTok, hdr: map[string]string{guestHeader: sec}}).do(handler); recorder.Code != 200 {
		t.Fatalf("hôte : %d", recorder.Code)
	}
	// Requête hôte relayée (gld_home) : en-têtes invité fournis par le client retirés par le proxy.
	localRequest := httptest.NewRequest("GET", "/v1/state", nil)
	localRequest.Host = testHost
	localRequest.AddCookie(&http.Cookie{Name: tokenCookie, Value: modeTok})
	localRequest.AddCookie(&http.Cookie{Name: homeCookie, Value: "perso"})
	localRequest.Header.Set(guestHeader, sec)
	localRequest.Header.Set(guestHTTPSHeader, "1")
	handler.ServeHTTP(httptest.NewRecorder(), localRequest)
	// Invitations relayées réussies : l'essai est rendu, la 11e passe encore.
	for index := range 11 {
		if recorder := (gReq{method: "POST", path: "/invite/perso/" + tok}).do(handler); recorder.Code != 303 {
			t.Fatalf("invitation relayée %d : %d", index+1, recorder.Code)
		}
	}
	if len(got) != 13 || got[0].guest != sec || got[0].share != hID1 || got[0].quiet != "" || got[0].cookie != "" || got[1].path != "/v1/state" || got[1].guest != "" || got[1].https != "" || got[2].guest != "" || got[2].path != "/invite/perso/"+tok {
		t.Fatalf("vu par l'enfant : %+v", got)
	}
	if recorder := (gReq{method: "POST", path: "/invite/inconnu/" + tok}).do(handler); recorder.Code != 403 {
		t.Fatalf("home inconnu : %d", recorder.Code)
	}
}

// Enfant : le secret relayé par la passerelle est validé ici ; invalide, jamais de repli sur l'hôte.
func TestGuestChildValidatesRelayedSecret(t *testing.T) {
	handler := guestWeb(t, nil, "")
	oc := childToken
	t.Cleanup(func() { childToken = oc })
	childToken = "ctok"
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	recorder := gReq{method: "POST", path: "/invite/perso/" + tok, hdr: map[string]string{childHeader: "ctok"}}.do(handler)
	cookie := cookieOf(recorder, gName)
	if recorder.Code != 303 || cookie == nil || !strings.HasPrefix(cookie.Value, "perso.") || recorder.Header().Get("Location") != "/guest/"+hID1 {
		t.Fatalf("invitation : %d %+v", recorder.Code, cookie)
	}
	_, sec, _ := strings.Cut(cookie.Value, ".")
	recorder = gReq{method: "GET", path: "/v1/guest/me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1}}.do(handler)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), id1) || !strings.Contains(recorder.Body.String(), hID1) {
		t.Fatalf("relayé : %d %s", recorder.Code, recorder.Body)
	}
	shareSt.mu.Lock()
	shareSt.m[id1].Invites[0].LastSeen = 1
	shareSt.mu.Unlock()
	gReq{method: "GET", path: "/v1/guest/me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1, guestQuietHeader: "1"}}.do(handler)
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen != 1 {
		t.Fatalf("vérification /v1/guest/sessions : LastSeen touché")
	}
	gReq{method: "GET", path: "/v1/guest/me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1}}.do(handler)
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen <= 1 {
		t.Fatalf("requête invitée : LastSeen non mis à jour")
	}
	for _, hd := range []string{"", shareHandle(gOther), "nimporte"} {
		if recorder := (gReq{method: "GET", path: "/v1/guest/me", hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hd}}).do(handler); recorder.Code != 403 {
			t.Fatalf("poignée %q : %d", hd, recorder.Code)
		}
	}
	for _, localPath := range []string{"/v1/state", "/v1/sessions/" + id1 + "/conversation"} {
		if recorder := (gReq{method: "GET", path: localPath, hdr: map[string]string{childHeader: "ctok", guestHeader: "faux"}}).do(handler); recorder.Code != 403 {
			t.Fatalf("%s secret faux : %d", localPath, recorder.Code)
		}
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", hdr: map[string]string{guestHeader: sec}}).do(handler); recorder.Code != 403 {
		t.Fatalf("sans la passerelle : %d", recorder.Code)
	}
}

func TestInviteGetDoesNotRedeem(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
	for range 2 {
		recorder := gReq{method: "GET", path: "/invite/main/" + tok}.do(handler)
		if recorder.Code != 200 || cookieOf(recorder, gName) != nil || recorder.Header().Get("Cache-Control") != "no-store" ||
			!strings.Contains(recorder.Body.String(), `method="post"`) || !strings.Contains(recorder.Body.String(), html.EscapeString(L("srv.share_guest.rejoindre"))) || strings.Contains(recorder.Body.String(), "<!--") {
			t.Fatalf("page : %d %s", recorder.Code, recorder.Body)
		}
	}
	if sh, _ := shareSt.Get(id1); sh.Invites[0].CookieHash != "" {
		t.Fatal("jeton consommé par un GET")
	}
	if recorder := (gReq{method: "POST", path: "/invite/main/" + tok}).do(handler); recorder.Code != 303 || cookieOf(recorder, gName) == nil {
		t.Fatalf("POST : %d", recorder.Code)
	}
	if recorder := (gReq{method: "POST", path: "/invite/main/" + tok}).do(handler); recorder.Code != 403 {
		t.Fatalf("second POST : %d", recorder.Code)
	}
	tok2, _, _ := shareSt.CreateInvite(gOther, "-q", "", "")
	localRecorder := gReq{method: "POST", path: "/invite/main/" + tok2, hdr: map[string]string{"Origin": "https://ailleurs.example", "Sec-Fetch-Site": "cross-site"}}.do(handler)
	if localRecorder.Code != 403 || cookieOf(localRecorder, guestCookieName(shareHandle(gOther))) != nil {
		t.Fatalf("autre origine : %d", localRecorder.Code)
	}
	localRecorder = gReq{method: "POST", path: "/invite/main/" + tok2, hdr: map[string]string{"Origin": "http://" + testHost, "Sec-Fetch-Site": "same-origin"}}.do(handler)
	if localRecorder.Code != 303 {
		t.Fatalf("même origine : %d", localRecorder.Code)
	}
}

func TestInviteHostCredentialDoesNotRedeem(t *testing.T) {
	t.Run("jeton", func(t *testing.T) {
		handler := guestWeb(t, nil, modeTok)
		tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
		for _, message := range []string{"GET", "POST"} {
			recorder := gReq{method: message, path: "/invite/main/" + tok, tokCookie: modeTok}.do(handler)
			if recorder.Code != 200 || cookieOf(recorder, gName) != nil || !strings.Contains(recorder.Body.String(), html.EscapeString(L("srv.share_guest.vousEtesHote"))) {
				t.Fatalf("%s hôte : %d %s", message, recorder.Code, recorder.Body)
			}
		}
		if recorder := (gReq{method: "POST", path: "/invite/main/" + tok}).do(handler); recorder.Code != 303 {
			t.Fatalf("jeton brûlé : %d", recorder.Code)
		}
	})
	t.Run("comptes", func(t *testing.T) {
		authStore := testStore(t)
		handler := guestWeb(t, authStore, modeTok)
		id, _ := authStore.AddUser("hote", testPw, true)
		if err := authStore.Grant(id, mainHomeID, allRights...); err != nil {
			t.Fatal(err)
		}
		sess, _ := fullSession(t, authStore, id)
		tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
		request := httptest.NewRequest("POST", "/invite/main/"+tok, nil)
		request.Host = testHost
		request.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != 200 || cookieOf(recorder, gName) != nil || !strings.Contains(recorder.Body.String(), html.EscapeString(L("srv.share_guest.vousEtesHote"))) {
			t.Fatalf("hôte : %d %s", recorder.Code, recorder.Body)
		}
		if recorder := (gReq{method: "POST", path: "/invite/main/" + tok}).do(handler); recorder.Code != 303 {
			t.Fatalf("jeton brûlé : %d", recorder.Code)
		}
	})
	t.Run("compte sans exec", func(t *testing.T) {
		authStore := testStore(t)
		handler := guestWeb(t, authStore, modeTok)
		id, _ := authStore.AddUser("collegue", testPw, false)
		sess, _ := fullSession(t, authStore, id)
		authStore.Revoke(id, mainHomeID, RightExec)
		tok, _, _ := shareSt.CreateInvite(id1, "-p", "", "")
		request := httptest.NewRequest("POST", "/invite/main/"+tok, nil)
		request.Host = testHost
		request.AddCookie(&http.Cookie{Name: sessCookie, Value: sess})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != 303 || cookieOf(recorder, gName) == nil {
			t.Fatalf("collègue sans exec refusé : %d %s", recorder.Code, recorder.Body)
		}
	})
}

func TestGuestLeaveClearsCookie(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	if recorder := (gReq{method: "DELETE", path: "/v1/guest/me", cookie: content}).do(handler); recorder.Code != 403 || cookieOf(recorder, gName) != nil {
		t.Fatalf("sans csrf : %d", recorder.Code)
	}
	localRecorder := gReq{method: "DELETE", path: "/v1/guest/me", cookie: content, csrf: csrf}.do(handler)
	if cookie := cookieOf(localRecorder, gName); localRecorder.Code != 204 || cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("départ : %d %s %+v", localRecorder.Code, localRecorder.Body, cookie)
	}
	if _, ok := shareSt.Get(id1); !ok {
		t.Fatal("partage supprimé")
	}
	if recorder := (gReq{method: "GET", path: "/v1/state", tokCookie: modeTok}).do(handler); recorder.Code != 200 {
		t.Fatalf("hôte : %d", recorder.Code)
	}
	if recorder := (gReq{method: "DELETE", path: "/v1/guest/me", tokCookie: modeTok}).do(handler); recorder.Code != 403 {
		t.Fatalf("non-invité : %d", recorder.Code)
	}
}

// Passerelle, partage d'un home enfant que l'enfant refuse (fin non écrite) : DELETE /v1/guest/me reste servi par la passerelle.
func TestGuestLeaveRelayedHandledByGateway(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(Home) (*homeProc, error) { t.Error("relayé"); return nil, http.ErrServerClosed }
	recorder := gReq{method: "DELETE", path: "/v1/guest/me", cookie: "perso." + sec, csrf: guestCSRF(sec)}.do(handler)
	if cookie := cookieOf(recorder, gName); recorder.Code != 204 || cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("départ : %d %+v", recorder.Code, cookie)
	}
}

func TestShareHandleStable(t *testing.T) {
	argument := shareHandle(id1)
	if argument != shareHandle(id1) || !validHandle(argument) || argument == shareHandle(gOther) || shareHandle("") == argument {
		t.Fatalf("poignée : %q %q", argument, shareHandle(gOther))
	}
	for _, bad := range []string{"", strings.ToUpper(argument), argument[:15], argument + "0", "0123456789abcdeg", "../../../../../x", "0123456789abcdé"} {
		if validHandle(bad) {
			t.Errorf("poignée acceptée : %q", bad)
		}
	}
}

func TestGuestTwoSharesTwoCookiesIndependent(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, _ := guestJoin(t, handler, id1, "-p", "A")
	cb, _ := guestJoin(t, handler, gOther, "-q", "B")
	hB := shareHandle(gOther)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	recorder := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", jar: jar, hdr: map[string]string{shareHeader: hID1}}.do(handler)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "Déploie caddy") || strings.Contains(recorder.Body.String(), "SECRET") {
		t.Fatalf("A : %d %s", recorder.Code, recorder.Body)
	}
	recorder = gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", jar: jar, hdr: map[string]string{shareHeader: hB}}.do(handler)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "SECRET autre session") || strings.Contains(recorder.Body.String(), "Déploie caddy") {
		t.Fatalf("B : %d %s", recorder.Code, recorder.Body)
	}
	// Cookie de A seul, en-tête de B : aucun cookie pour B, requête hôte (sans jeton : refusée), jamais les données de B.
	recorder = gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", jar: jar[:1], hdr: map[string]string{shareHeader: hB}}.do(handler)
	if recorder.Code != 401 || strings.Contains(recorder.Body.String(), "SECRET") || cookieOf(recorder, gName) != nil {
		t.Fatalf("A sous B : %d %s", recorder.Code, recorder.Body)
	}
}

func TestGuestCookieSwapRejected(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, _ := guestJoin(t, handler, id1, "-p", "")
	guestJoin(t, handler, gOther, "-q", "")
	hB := shareHandle(gOther)
	recorder := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: ca, share: hB}.do(handler)
	if recorder.Code != 401 || strings.Contains(recorder.Body.String(), "SECRET") || strings.Contains(recorder.Body.String(), "Déploie") {
		t.Fatalf("secret de A sous le nom de B : %d %s", recorder.Code, recorder.Body)
	}
	if cookie := cookieOf(recorder, guestCookieName(hB)); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("cookie échangé non effacé : %+v", cookie)
	}
	if recorder := (gReq{method: "GET", path: "/v1/guest/me", cookie: ca, share: hB}).do(handler); recorder.Code != 401 {
		t.Fatalf("guest-me échangé : %d", recorder.Code)
	}
}

func TestNoHeaderMeansHostEvenWithGuestCookies(t *testing.T) {
	t.Run("jeton", func(t *testing.T) {
		handler := guestWeb(t, nil, modeTok)
		content, _ := guestIn(t, handler)
		recorder := gReq{method: "GET", path: "/v1/state", cookie: content, noShare: true, tokCookie: modeTok}.do(handler)
		if recorder.Code != 200 || cookieOf(recorder, gName) != nil {
			t.Fatalf("hôte verrouillé : %d %s", recorder.Code, recorder.Body)
		}
		for _, localPath := range []string{"/v1/sessions/" + id1 + "/conversation", "/v1/sessions/" + id1 + "/queue", "/v1/state"} {
			if recorder := (gReq{method: "GET", path: localPath, cookie: content, noShare: true}).do(handler); recorder.Code != 401 {
				t.Fatalf("%s sans en-tête ni jeton : %d", localPath, recorder.Code)
			}
		}
		if recorder := (gReq{method: "GET", path: "/v1/guest/me", cookie: content, noShare: true, tokCookie: modeTok}).do(handler); recorder.Code != 403 {
			t.Fatalf("guest-me sans en-tête : %d %s", recorder.Code, recorder.Body)
		}
	})
	t.Run("ouvert", func(t *testing.T) {
		handler := guestWeb(t, nil, "")
		content, _ := guestIn(t, handler)
		if recorder := (gReq{method: "GET", path: "/v1/state", cookie: content, noShare: true}).do(handler); recorder.Code != 200 {
			t.Fatalf("hôte verrouillé : %d %s", recorder.Code, recorder.Body)
		}
		if recorder := (gReq{method: "GET", path: "/v1/guest/me", cookie: content, noShare: true}).do(handler); recorder.Code != 403 {
			t.Fatalf("guest-me sans en-tête : %d", recorder.Code)
		}
	})
}

func TestGuestInvalidHeaderFormatIgnored(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	jar := []*http.Cookie{{Name: gName, Value: content}}
	for _, hd := range []string{strings.ToUpper(hID1), hID1 + "0", hID1[:15], "x", "-" + hID1[1:]} {
		recorder := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", jar: jar, hdr: map[string]string{shareHeader: hd}}.do(handler)
		if recorder.Code != 401 || cookieOf(recorder, gName) != nil {
			t.Fatalf("en-tête %q : %d", hd, recorder.Code)
		}
		if recorder := (gReq{method: "GET", path: "/v1/state", jar: jar, tokCookie: modeTok, hdr: map[string]string{shareHeader: hd}}).do(handler); recorder.Code != 200 {
			t.Fatalf("hôte, en-tête %q : %d", hd, recorder.Code)
		}
	}
}

type gItem struct{ Handle, Title, Mode string }

func TestGuestListReturnsOnlyValidAndClearsInvalid(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA := guestJoin(t, handler, id1, "-p", "Titre A")
	cb, _ := guestJoin(t, handler, gOther, "-q", "Titre B")
	shareSt.SetInviteMode(gOther, inv0(gOther), "ro")
	hX := shareHandle("33333333-3333-3333-3333-333333333333")
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hX), Value: ca}, {Name: guestCookieName("pasunepoignee"), Value: "x"},
		{Name: guestCookieName(shareHandle(gOther)), Value: cb}}
	recorder := gReq{method: "GET", path: "/v1/guest/sessions", jar: jar}.do(handler)
	var got []gItem
	if recorder.Code != 200 || recorder.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(recorder.Body.Bytes(), &got) != nil {
		t.Fatalf("liste : %d %s", recorder.Code, recorder.Body)
	}
	want := map[gItem]bool{{hID1, "Titre A", "rw"}: true, {shareHandle(gOther), "Titre B", "ro"}: true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] || got[0] == got[1] {
		t.Fatalf("liste : %+v", got)
	}
	_, secA, _ := strings.Cut(ca, ".")
	_, secB, _ := strings.Cut(cb, ".")
	for _, leak := range []string{secA, secB, csrfA, id1, gOther, "-p", "-q", "dir", "csrf", "session"} {
		if strings.Contains(recorder.Body.String(), leak) {
			t.Errorf("fuite %q : %s", leak, recorder.Body)
		}
	}
	for _, name := range []string{guestCookieName(hX), guestCookieName("pasunepoignee")} {
		if cookie := cookieOf(recorder, name); cookie == nil || cookie.MaxAge >= 0 {
			t.Errorf("%s non effacé : %+v", name, cookie)
		}
	}
	if cookieOf(recorder, gName) != nil || cookieOf(recorder, guestCookieName(shareHandle(gOther))) != nil {
		t.Fatal("cookie valide effacé")
	}
	// Plafond : au-delà de guestListMax cookies invités, les suivants ne sont pas examinés.
	var many []*http.Cookie
	for index := range guestListMax + 5 {
		many = append(many, &http.Cookie{Name: guestCookieName(shareHandle(strings.Repeat("9", index+1))), Value: ".faux"})
	}
	recorder = gReq{method: "GET", path: "/v1/guest/sessions", jar: append(many, jar[0])}.do(handler)
	if recorder.Code != 403 || len(recorder.Result().Cookies()) != guestListMax {
		t.Fatalf("plafond : %d, %d cookies", recorder.Code, len(recorder.Result().Cookies()))
	}
}

func TestGuestListNoCookie403(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	for _, localGReq := range []gReq{{method: "GET", path: "/v1/guest/sessions"}, {method: "GET", path: "/v1/guest/sessions", tokCookie: modeTok}} {
		if recorder := localGReq.do(handler); recorder.Code != 403 || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("sans cookie : %d %s", recorder.Code, recorder.Body)
		}
	}
}

func TestGuestMeHandleTitle(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestJoin(t, handler, id1, "-p", "Mon titre")
	recorder := gReq{method: "GET", path: "/v1/guest/me", cookie: content}.do(handler)
	var me map[string]any
	json.Unmarshal(recorder.Body.Bytes(), &me)
	if recorder.Code != 200 || me["handle"] != hID1 || me["title"] != "Mon titre" {
		t.Fatalf("guest-me : %d %s", recorder.Code, recorder.Body)
	}
}

func TestGuestLeaveClearsOnlyThatShare(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA := guestJoin(t, handler, id1, "-p", "")
	cb, csrfB := guestJoin(t, handler, gOther, "-q", "")
	hB := shareHandle(gOther)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	if recorder := (gReq{method: "DELETE", path: "/v1/guest/me", jar: jar, csrf: csrfB, hdr: map[string]string{shareHeader: hID1}}).do(handler); recorder.Code != 403 {
		t.Fatalf("csrf de B pour A : %d", recorder.Code)
	}
	localRecorder := gReq{method: "DELETE", path: "/v1/guest/me", jar: jar, csrf: csrfA, hdr: map[string]string{shareHeader: hID1}}.do(handler)
	if cookie := cookieOf(localRecorder, gName); localRecorder.Code != 204 || cookie == nil || cookie.MaxAge >= 0 || cookieOf(localRecorder, guestCookieName(hB)) != nil {
		t.Fatalf("départ de A : %d %v", localRecorder.Code, localRecorder.Result().Cookies())
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cb, share: hB}).do(handler); recorder.Code != 200 {
		t.Fatalf("B après départ de A : %d", recorder.Code)
	}
}

func TestEndedShareOnlyThatShareFallsThrough(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, _ := guestJoin(t, handler, id1, "-p", "")
	cb, _ := guestJoin(t, handler, gOther, "-q", "")
	hB := shareHandle(gOther)
	shareSt.End(id1)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	recorder := gReq{method: "GET", path: "/v1/state", jar: jar, tokCookie: modeTok, hdr: map[string]string{shareHeader: hID1}}.do(handler)
	if cookie := cookieOf(recorder, gName); recorder.Code != 200 || cookie == nil || cookie.MaxAge >= 0 || cookieOf(recorder, guestCookieName(hB)) != nil {
		t.Fatalf("A terminé : %d %v", recorder.Code, recorder.Result().Cookies())
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", jar: jar, hdr: map[string]string{shareHeader: hB}}).do(handler); recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "SECRET autre") {
		t.Fatalf("B : %d", recorder.Code)
	}
}

// L'interface invité (page et fichiers) se charge sans en-tête de partage ; en mode jeton, seul un cookie invité valide l'ouvre.
func TestInviteRedirectsToHandleURL(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	for _, localPath := range []string{"/guest/" + hID1, "/guest"} {
		if recorder := (gReq{method: "GET", path: localPath, cookie: content, noShare: true}).do(handler); recorder.Code != 200 || recorder.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("%s invité : %d", localPath, recorder.Code)
		}
		if recorder := (gReq{method: "GET", path: localPath}).do(handler); recorder.Code != 401 {
			t.Fatalf("%s anonyme : %d", localPath, recorder.Code)
		}
		if recorder := (gReq{method: "GET", path: localPath, cookie: ".faux", noShare: true}).do(handler); recorder.Code != 401 {
			t.Fatalf("%s cookie faux : %d", localPath, recorder.Code)
		}
	}
	if recorder := (gReq{method: "GET", path: "/guest/" + hID1, tokCookie: modeTok}).do(handler); recorder.Code != 200 {
		t.Fatalf("hôte : %d", recorder.Code)
	}
}

// Passerelle : /v1/guest/sessions juge un cookie de home enfant d'après son fichier, puis le fait revalider par l'enfant.
func TestGuestListChildRevalidated(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "Enfant", "")
	sec, _, _, _ := cs.Redeem(tok)
	tok2, _, _ := cs.CreateInvite(gOther, "-q", "", "")
	sec2, _, _, _ := cs.Redeem(tok2)
	refuse, plain, down := false, false, false
	child := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		hd := request.Header.Get(shareHeader)
		if plain { // refus de childAuthorized ou de la garde Host : pas un jugement du secret
			http.Error(responseWriter, "forbidden", http.StatusForbidden)
			return
		}
		if refuse {
			guestDeny(responseWriter, 403, "srv.share_guest.refuse")
			return
		}
		if down {
			guestDeny(responseWriter, 503, "srv.share.indisponible")
			return
		}
		if request.URL.Path != "/v1/guest/me" || request.Header.Get(guestHeader) == "" || request.Header.Get("Cookie") != "" || request.Header.Get(guestQuietHeader) != "1" {
			responseWriter.WriteHeader(403)
			return
		}
		responseWriter.Write([]byte(`{"handle":"` + hd + `","title":"Enfant","mode":"rw","id":"` + id1 + `","csrf":"c"}`))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(home Home) (*homeProc, error) {
		return &homeProc{dir: home.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	jar := []*http.Cookie{{Name: gName, Value: "perso." + sec}, {Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec}}
	recorder := gReq{method: "GET", path: "/v1/guest/sessions", jar: jar}.do(handler)
	var got []gItem
	json.Unmarshal(recorder.Body.Bytes(), &got)
	if recorder.Code != 200 || len(got) != 1 || got[0] != (gItem{hID1, "Enfant", "rw"}) || strings.Contains(recorder.Body.String(), id1) || strings.Contains(recorder.Body.String(), sec) {
		t.Fatalf("liste : %d %s", recorder.Code, recorder.Body)
	}
	if cookie := cookieOf(recorder, guestCookieName(shareHandle(gOther))); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("cookie échangé non effacé : %+v", cookie)
	}
	plain = true
	recorder = gReq{method: "GET", path: "/v1/guest/sessions", jar: []*http.Cookie{{Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec2}}}.do(handler)
	if recorder.Code != 403 || len(recorder.Result().Cookies()) != 0 {
		t.Fatalf("403 brut de l'enfant : %d %v", recorder.Code, recorder.Result().Cookies())
	}
	plain, down = false, true
	recorder = gReq{method: "GET", path: "/v1/guest/sessions", jar: []*http.Cookie{{Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec2}}}.do(handler)
	if recorder.Code != 403 || len(recorder.Result().Cookies()) != 0 {
		t.Fatalf("partages indisponibles dans l'enfant : %d %v", recorder.Code, recorder.Result().Cookies())
	}
	down, refuse = false, true
	recorder = gReq{method: "GET", path: "/v1/guest/sessions", jar: []*http.Cookie{{Name: guestCookieName(shareHandle(gOther)), Value: "perso." + sec2}}}.do(handler)
	if cookie := cookieOf(recorder, guestCookieName(shareHandle(gOther))); recorder.Code != 403 || cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("refus de l'enfant : %d %+v", recorder.Code, cookie)
	}
}

// Le statique sans en-tête n'est servi qu'aux motifs de guestStatics sur un chemin canonique ; le reste va au jeton (401).
func TestGuestStaticExceptionTable(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	asset, _ := filepath.Glob("ui/dist/assets/*.js")
	if len(asset) == 0 {
		t.Skip("ui/dist absent")
	}
	jar := []*http.Cookie{{Name: gName, Value: content}}
	// La racine nue sert la page du jeton à tout anonyme (icône iOS) : jamais l'interface de l'hôte.
	if recorder := (gReq{method: "GET", path: "/", jar: jar}).do(handler); recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `action="/token-login"`) {
		t.Errorf("/ : %d, page du jeton attendue", recorder.Code)
	}
	for _, localPath := range []string{"/sw.js", "/guest/", "/assets/", "/guest/x/y", "/assets/%2e%2e/x", "/assets/../x", "/guest/..%2f", "/v1/sessions/" + id1 + "/conversation", "/v1/state"} {
		if recorder := (gReq{method: "GET", path: localPath, jar: jar}).do(handler); recorder.Code != 401 {
			t.Errorf("%s servi : %d", localPath, recorder.Code)
		}
	}
	for _, localPath := range []string{"/guest", "/guest/" + hID1, "/assets/" + filepath.Base(asset[0]), "/icons/x", "/favicon.ico", "/site.webmanifest"} {
		if recorder := (gReq{method: "GET", path: localPath, jar: jar}).do(handler); recorder.Code == 401 {
			t.Errorf("%s refusé", localPath)
		}
	}
	if recorder := (gReq{method: "GET", path: "/assets/" + filepath.Base(asset[0])}).do(handler); recorder.Code != 401 {
		t.Fatalf("asset anonyme : %d", recorder.Code)
	}
}

// Des cookies d'un home enfant forgés coûtent une lecture de fichier chacun : au plus guestChildMax par requête, après ceux du home principal.
func TestGuestChildCookieChecksCapped(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	addHomes(t, "perso")
	count := 0
	old := guestOpenShares
	t.Cleanup(func() { guestOpenShares = old })
	guestOpenShares = func(dir string) (*shareStore, error) { count++; return old(dir) }
	var jar []*http.Cookie
	for index := range guestListMax {
		jar = append(jar, &http.Cookie{Name: guestCookieName(shareHandle(strings.Repeat("8", index+1))), Value: "perso.faux" + strings.Repeat("x", index)})
	}
	all := append([]*http.Cookie{jar[0], jar[1], {Name: gName, Value: content}}, jar[2:guestListMax-1]...)
	recorder := gReq{method: "GET", path: "/v1/guest/sessions", jar: all}.do(handler)
	var got []gItem
	json.Unmarshal(recorder.Body.Bytes(), &got)
	if recorder.Code != 200 || len(got) != 1 || got[0].Handle != hID1 || count > guestChildMax {
		t.Fatalf("liste : %d %s, %d lectures", recorder.Code, recorder.Body, count)
	}
	count = 0
	if recorder := (gReq{method: "GET", path: "/guest", jar: jar}).do(handler); recorder.Code != 401 || count > guestChildMax {
		t.Fatalf("statique : %d, %d lectures", recorder.Code, count)
	}
	count = 0
	if recorder := (gReq{method: "GET", path: "/guest", jar: all}).do(handler); recorder.Code != 200 || count != 0 {
		t.Fatalf("statique, cookie principal d'abord : %d, %d lectures", recorder.Code, count)
	}
}

// LastSeen ne bouge que sur une vraie requête invitée (en-tête de partage), jamais sur /v1/guest/sessions ni le statique.
func TestGuestLastSeenOnlyOnGuardedRequests(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	shareSt.mu.Lock()
	shareSt.m[id1].Invites[0].LastSeen = 1
	shareSt.mu.Unlock()
	jar := []*http.Cookie{{Name: gName, Value: content}}
	for _, localPath := range []string{"/v1/guest/sessions", "/guest", "/guest/" + hID1, "/favicon.ico"} {
		gReq{method: "GET", path: localPath, jar: jar}.do(handler)
	}
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen != 1 {
		t.Fatalf("LastSeen touché sans requête invitée : %d", sh.Invites[0].LastSeen)
	}
	gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}.do(handler)
	if sh, _ := shareSt.Get(id1); sh.Invites[0].LastSeen <= 1 {
		t.Fatalf("LastSeen non mis à jour : %d", sh.Invites[0].LastSeen)
	}
}

// <img> ne peut pas porter d'en-tête : seules les images de la conversation acceptent la poignée en paramètre share (l'en-tête l'emporte).
func TestGuestConvImageShareQuery(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA := guestJoin(t, handler, id1, "-p", "")
	cb, _ := guestJoin(t, handler, gOther, "-q", "")
	hB := shareHandle(gOther)
	put(t, filepath.Join(root, "-q", gOther+".jsonl"), `{"type":"user","cwd":"/q","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":[{"type":"text","text":"x"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+png1+`"}}]}}`+"\n", old)
	jar := []*http.Cookie{{Name: gName, Value: ca}, {Name: guestCookieName(hB), Value: cb}}
	img := func(share string, hdr map[string]string, jar []*http.Cookie) *httptest.ResponseRecorder {
		return gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation/images/0-1?dir=-p&share=" + share, jar: jar, hdr: hdr}.do(handler)
	}
	if recorder := img(hB, nil, jar); recorder.Code != 200 || recorder.Header().Get("Content-Type") != "image/png" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("image de B : %d %v", recorder.Code, recorder.Header())
	}
	// Partage A choisi : id/dir imposés à A, dont la ligne 0 n'est pas une image (404 invité, pas 401 hôte).
	if recorder := img(hID1, nil, jar); recorder.Code != 404 || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("image de A : %d", recorder.Code)
	}
	if recorder := img(hB, map[string]string{shareHeader: hID1}, jar); recorder.Code != 404 {
		t.Fatalf("l'en-tête doit l'emporter : %d", recorder.Code)
	}
	for _, text := range []string{strings.ToUpper(hB), hB + "0", "x", shareHandle("33333333-3333-3333-3333-333333333333")} {
		if recorder := img(text, nil, jar); recorder.Code != 401 {
			t.Fatalf("share %q : %d", text, recorder.Code)
		}
	}
	if recorder := img(hB, nil, jar[:1]); recorder.Code != 401 {
		t.Fatalf("sans cookie de B : %d", recorder.Code)
	}
	localRecorder := img(hB, nil, []*http.Cookie{{Name: guestCookieName(hB), Value: ca}})
	if cookie := cookieOf(localRecorder, guestCookieName(hB)); localRecorder.Code != 401 || cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("secret de A sous B : %d %+v", localRecorder.Code, cookie)
	}
	// Aucune autre route ne lit share.
	for _, localPath := range []string{"/v1/sessions/" + id1 + "/conversation?share=" + hID1, "/v1/sessions/" + id1 + "/queue?share=" + hID1, "/v1/guest/me?share=" + hID1} {
		if recorder := (gReq{method: "GET", path: localPath, jar: jar}).do(handler); recorder.Code != 401 {
			t.Fatalf("%s : %d", localPath, recorder.Code)
		}
	}
	if recorder := (gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue?share=" + hID1, body: `{"text":"m"}`, jar: jar, csrf: csrfA}).do(handler); recorder.Code == 201 || len(queueSt.List(id1)) != 0 {
		t.Fatalf("POST avec share : %d", recorder.Code)
	}
}

// Passerelle : la poignée en paramètre suit le même relais que l'en-tête ; l'enfant reçoit l'en-tête, jamais share.
func TestGuestConvImageShareQueryRelayed(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	var path, share, guest string
	child := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		path, share, guest = request.URL.RequestURI(), request.Header.Get(shareHeader), request.Header.Get(guestHeader)
		responseWriter.Write([]byte("img"))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(home Home) (*homeProc, error) {
		return &homeProc{dir: home.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	jar := []*http.Cookie{{Name: gName, Value: "perso." + sec}}
	recorder := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation/images/0-1?share=" + hID1, jar: jar}.do(handler)
	if recorder.Code != 200 || recorder.Body.String() != "img" || share != hID1 || guest != sec || strings.Contains(path, "share=") || path != "/v1/sessions/"+id1+"/conversation/images/0-1" {
		t.Fatalf("relais : %d %q %q %q", recorder.Code, path, share, guest)
	}
}

// Quatre partages d'un home enfant, les trois plus anciens terminés : la page /guest/<poignée> du quatrième s'ouvre (son
// cookie est lu en premier) ; /guest passe par le balayage plafonné, qui efface les morts et sert au chargement suivant.
func TestGuestStaticHandleFirstAndClearsDead(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	var jar []*http.Cookie
	var hds []string
	for index := range 4 {
		sess := strings.Repeat(string(rune('a'+index)), 8) + "-1111-1111-1111-111111111111"
		tok, _, _ := cs.CreateInvite(sess, "-p", "", "")
		sec, _, _, _ := cs.Redeem(tok)
		hds = append(hds, shareHandle(sess))
		jar = append(jar, &http.Cookie{Name: guestCookieName(hds[index]), Value: "perso." + sec})
	}
	for index := range 3 {
		sess := strings.Repeat(string(rune('a'+index)), 8) + "-1111-1111-1111-111111111111"
		if err := cs.End(sess); err != nil {
			t.Fatal(err)
		}
	}
	if recorder := (gReq{method: "GET", path: "/guest/" + hds[3], jar: jar}).do(handler); recorder.Code != 200 || recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("page du partage vivant : %d", recorder.Code)
	}
	localRecorder := gReq{method: "GET", path: "/guest", jar: jar}.do(handler)
	if localRecorder.Code != 401 || cookieOf(localRecorder, jar[3].Name) != nil {
		t.Fatalf("/guest, trois morts d'abord : %d %v", localRecorder.Code, localRecorder.Result().Cookies())
	}
	for _, cookie := range jar[:3] {
		if localCookie := cookieOf(localRecorder, cookie.Name); localCookie == nil || localCookie.MaxAge >= 0 {
			t.Fatalf("cookie mort non effacé : %s", cookie.Name)
		}
	}
	if recorder := (gReq{method: "GET", path: "/guest", jar: jar[3:]}).do(handler); recorder.Code != 200 {
		t.Fatalf("/guest après effacement : %d", recorder.Code)
	}
	localRecorder = gReq{method: "GET", path: "/guest/" + hds[0], jar: jar}.do(handler)
	if cookie := cookieOf(localRecorder, jar[0].Name); localRecorder.Code != 401 || cookie == nil || cookie.MaxAge >= 0 || cookieOf(localRecorder, jar[3].Name) != nil {
		t.Fatalf("page d'un partage mort : %d %v", localRecorder.Code, localRecorder.Result().Cookies())
	}
}

func noClear(t *testing.T, what string, recorder *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.MaxAge < 0 {
			t.Errorf("%s : cookie %s effacé sans réponse certaine", what, cookie.Name)
		}
	}
}

// Ne pas pouvoir répondre n'est pas « secret invalide » : aucun effacement (le lien était à usage unique), aucun accès invité.
func TestGuestUnsureNeverClears(t *testing.T) {
	probe := func(t *testing.T, handler http.Handler, content string) {
		t.Helper()
		jar := []*http.Cookie{{Name: gName, Value: content}}
		for _, localGReq := range []gReq{{method: "GET", path: "/guest/" + hID1, jar: jar}, {method: "GET", path: "/guest", jar: jar}, {method: "GET", path: "/v1/guest/sessions", jar: jar}, {method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}} {
			recorder := localGReq.do(handler)
			noClear(t, localGReq.path, recorder)
			if recorder.Code == 200 {
				t.Errorf("%s : servi sans réponse certaine", localGReq.path)
			}
		}
	}
	t.Run("fichier des partages illisible", func(t *testing.T) {
		handler := guestWeb(t, nil, modeTok)
		addHomes(t, "perso")
		hm, _ := homeByName("perso")
		cs, _ := openShares(hm.Dir)
		tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
		sec, _, _, _ := cs.Redeem(tok)
		old := guestOpenShares
		t.Cleanup(func() { guestOpenShares = old })
		guestOpenShares = func(string) (*shareStore, error) { return nil, os.ErrPermission }
		probe(t, handler, "perso."+sec)
	})
	t.Run("partages indisponibles", func(t *testing.T) {
		handler := guestWeb(t, nil, modeTok)
		content, _ := guestIn(t, handler)
		shareSt = nil
		probe(t, handler, content)
	})
	t.Run("fichier des homes illisible", func(t *testing.T) {
		handler := guestWeb(t, nil, modeTok)
		addHomes(t, "perso")
		hm, _ := homeByName("perso")
		cs, _ := openShares(hm.Dir)
		tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
		sec, _, _, _ := cs.Redeem(tok)
		if err := os.WriteFile(homesFile(), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		probe(t, handler, "perso."+sec)
	})
}

// Réponses certaines : partage terminé, remplacé, home inconnu, secret faux ; effacés sur les pages et la requête gardée.
func TestGuestDefinitiveStillClears(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	shareSt.Regen(id1, inv0(id1)) // régénéré : l'ancien cookie meurt
	for _, value := range []string{content, "inconnu." + strings.Split(content, ".")[1], ".faux"} {
		jar := []*http.Cookie{{Name: gName, Value: value}}
		for _, localGReq := range []gReq{{method: "GET", path: "/guest/" + hID1, jar: jar}, {method: "GET", path: "/guest", jar: jar}, {method: "GET", path: "/v1/guest/sessions", jar: jar}, {method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: value}} {
			recorder := localGReq.do(handler)
			if cookie := cookieOf(recorder, gName); cookie == nil || cookie.MaxAge >= 0 || recorder.Code == 200 {
				t.Errorf("%s %q : %d %+v", localGReq.path, value, recorder.Code, cookie)
			}
		}
	}
}

// Les fichiers publics, mis en cache, ne portent jamais d'effacement ; les pages qui effacent sont no-store.
func TestGuestStaticClearsOnlyOnPages(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	jar := []*http.Cookie{{Name: gName, Value: ".mort"}}
	paths := []string{"/icons/x", "/favicon.ico", "/site.webmanifest"}
	if asset, _ := filepath.Glob("ui/dist/assets/*.js"); len(asset) > 0 {
		paths = append(paths, "/assets/"+filepath.Base(asset[0]))
	} else {
		t.Log("ui/dist absent : /assets/ non vérifié")
	}
	for _, localPath := range paths {
		if recorder := (gReq{method: "GET", path: localPath, jar: jar}).do(handler); len(recorder.Result().Cookies()) != 0 {
			t.Errorf("%s : %v", localPath, recorder.Result().Cookies())
		}
	}
	for _, localPath := range []string{"/guest", "/guest/" + hID1} {
		recorder := gReq{method: "GET", path: localPath, jar: jar, hdr: map[string]string{"Accept": "text/html"}}.do(handler)
		if cookie := cookieOf(recorder, gName); cookie == nil || cookie.MaxAge >= 0 || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s : %+v %q", localPath, cookie, recorder.Header().Get("Cache-Control"))
		}
	}
}

// Une seule lecture du fichier des homes : une seconde lecture ratée (rendue vide par readHomesFile) ne doit pas faire croire
// à un home inconnu et effacer un cookie valide.
func TestGuestHomesReadOnce(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	data, _ := json.Marshal(map[string]any{"homes": []Home{{Name: "perso", Dir: hm.Dir}}})
	os.Remove(homesFile()) // toute autre lecture que la première ne verrait plus aucun home
	count := 0
	old := guestReadHomes
	t.Cleanup(func() { guestReadHomes = old })
	guestReadHomes = func() ([]byte, error) { count++; return data, nil }
	recorder := gReq{method: "GET", path: "/guest/" + hID1, jar: []*http.Cookie{{Name: gName, Value: "perso." + sec}}}.do(handler)
	if recorder.Code != 200 || len(recorder.Result().Cookies()) != 0 || count != 1 {
		t.Fatalf("page : %d %v, %d lectures", recorder.Code, recorder.Result().Cookies(), count)
	}
}

// Enfant : un magasin de partages absent n'est pas un refus du secret (503) ; un magasin lu sans ce secret, si (403 ok:false).
func TestGuestChildUnsureAnswers503(t *testing.T) {
	handler := guestWeb(t, nil, "")
	oc := childToken
	t.Cleanup(func() { childToken = oc })
	childToken = "ctok"
	hdr := map[string]string{childHeader: "ctok", guestHeader: "secret", shareHeader: hID1}
	recorder := gReq{method: "GET", path: "/v1/guest/me", hdr: hdr}.do(handler)
	if recorder.Code != 403 || !strings.Contains(recorder.Body.String(), `"error"`) {
		t.Fatalf("secret inconnu : %d %s", recorder.Code, recorder.Body)
	}
	shareSt = nil
	if recorder := (gReq{method: "GET", path: "/v1/guest/me", hdr: hdr}).do(handler); recorder.Code != 503 || !strings.Contains(recorder.Body.String(), `"error"`) {
		t.Fatalf("magasin absent : %d %s", recorder.Code, recorder.Body)
	}
}

// La garde efface en enveloppant la réponse : un handler hôte qui diffuse doit garder Flush.
func TestNoStoreWriterFlushes(t *testing.T) {
	rec := httptest.NewRecorder()
	var responseWriter http.ResponseWriter = &noStoreWriter{ResponseWriter: rec}
	flusher, ok := responseWriter.(http.Flusher)
	if !ok {
		t.Fatal("pas de Flush")
	}
	flusher.Flush()
	if !rec.Flushed || rec.Header().Get("Cache-Control") != "no-store" || http.NewResponseController(responseWriter).Flush() != nil {
		t.Fatalf("flush : %v %q", rec.Flushed, rec.Header().Get("Cache-Control"))
	}
}

func TestGuestSecretPath(t *testing.T) {
	for path, want := range map[string]bool{"": false, "src/a.go": false, ".git/config": true, "a/.env.local": true, "k/server.PEM": true, "id_rsa": true, "envx": false, ".ssh/x": true, "a/my-secrets.txt": true, "b/.aws/credentials": true} {
		if guestSecretPath(path) != want {
			t.Errorf("%q: want %v", path, want)
		}
	}
}

func TestGuestFsEscapeNeverServed(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf := guestIn(t, handler)
	for _, localPath := range []string{"/etc/passwd", "../x", "a/../../x", "%2e%2e/x"} {
		recorder := gReq{method: "GET", path: "/v1/fs/entries/content?path=" + localPath, cookie: content, csrf: csrf}.do(handler)
		if recorder.Code == 200 {
			t.Errorf("%s : %d %s", localPath, recorder.Code, recorder.Body)
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
		uRL := &url.URL{Path: in}
		guestSessionPath(uRL, sid)
		if uRL.Path != want {
			t.Errorf("%s: got %s, want %s", in, uRL.Path, want)
		}
	}
}

func TestGuestCited(t *testing.T) {
	dir := t.TempDir()
	enc := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(dir)
	sid := "11111111-1111-1111-1111-111111111111"
	old := root
	root = filepath.Join(t.TempDir(), "projects")
	t.Cleanup(func() { root = old })
	os.MkdirAll(filepath.Join(root, enc), 0o700)
	os.WriteFile(filepath.Join(root, enc, sid+".jsonl"), []byte(`{"input":{"file_path":"`+dir+`/src/a.go"}}`+"\n"+`{"text":"voir `+dir+`/b.md."}`+"\n"+`{"text":"`+dir+`/../etc/passwd"}`+"\n"), 0o600)
	lookup := guestCited(Share{Dir: enc, Session: sid})
	for path, want := range map[string]bool{"src/a.go": true, "b.md": true, "src/a": false, "c.go": false, "../etc/passwd": false, "etc/passwd": false} {
		if lookup[path] != want {
			t.Errorf("%s: want %v (%v)", path, want, lookup)
		}
	}
	if !citedDir(lookup, "src") || citedDir(lookup, "lib") {
		t.Error("citedDir")
	}
}

func TestGuestTreeFilter(t *testing.T) {
	cited := map[string]bool{"src/a.go": true, "b.md": true}
	run := func(dir, body string, code int) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		gt := &guestTree{ResponseWriter: recorder, code: code}
		gt.buf.WriteString(body)
		gt.flush(recorder, cited, dir)
		return recorder
	}
	localRecorder := run("", `{"entries":[{"name":"src","type":"dir"},{"name":"lib","type":"dir"},{"name":"b.md","type":"file"},{"name":"c.go","type":"file"}],"truncated":false}`, 200)
	if localRecorder.Code != 200 || !strings.Contains(localRecorder.Body.String(), `"src"`) || !strings.Contains(localRecorder.Body.String(), `"b.md"`) || strings.Contains(localRecorder.Body.String(), `"lib"`) || strings.Contains(localRecorder.Body.String(), `"c.go"`) {
		t.Fatalf("racine : %d %s", localRecorder.Code, localRecorder.Body)
	}
	localRecorder = run("src", `{"entries":[{"name":"a.go","type":"file"},{"name":"z.go","type":"file"}]}`, 200)
	if strings.Contains(localRecorder.Body.String(), "z.go") || !strings.Contains(localRecorder.Body.String(), "a.go") {
		t.Fatalf("src : %s", localRecorder.Body)
	}
	if localRecorder = run("", `{"error":{}}`, 404); localRecorder.Code != 404 || strings.Contains(localRecorder.Body.String(), "entries") {
		t.Fatalf("erreur relayée : %d %s", localRecorder.Code, localRecorder.Body)
	}
}

func TestGuestInside(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o600)
	os.WriteFile(filepath.Join(out, "x"), nil, 0o600)
	os.Symlink(out, filepath.Join(dir, "lnk"))
	os.Symlink(filepath.Join(dir, "a.go"), filepath.Join(dir, "ok"))
	for rel, want := range map[string]bool{"": true, "a.go": true, "ok": true, "lnk": false, "lnk/x": false, "../x": false, "absent": false} {
		if guestInside(dir, rel) != want {
			t.Errorf("%q: want %v", rel, want)
		}
	}
}
