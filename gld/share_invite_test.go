package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// joinAs : invitation nommée créée sur sess, lien ouvert ; rend le cookie, l'en-tête CSRF et l'identifiant d'invitation.
func joinAs(t *testing.T, handler http.Handler, sess, dir, title, name string) (cookie, csrf, inv string) {
	t.Helper()
	tok, in, err := shareSt.CreateInvite(sess, dir, title, name)
	if err != nil {
		t.Fatal(err)
	}
	hd := shareHandle(sess)
	recorder := gReq{method: "POST", path: "/invite/main/" + tok}.do(handler)
	localCookie := cookieOf(recorder, guestCookieName(hd))
	if recorder.Code != 303 || localCookie == nil {
		t.Fatalf("invite : %d", recorder.Code)
	}
	recorder = gReq{method: "GET", path: "/v1/guest/me", cookie: localCookie.Value, share: hd}.do(handler)
	var me struct{ ID, CSRF, Invite, Label, Mode string }
	if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &me) != nil || me.Invite != in.ID || me.ID != sess {
		t.Fatalf("guest-me : %d %s", recorder.Code, recorder.Body)
	}
	return localCookie.Value, me.CSRF, in.ID
}

func addAs(handler http.Handler, content, csrf, text string, hdr map[string]string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(map[string]string{"text": text})
	return gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: string(data), cookie: content, csrf: csrf, hdr: hdr}.do(handler)
}

func itemOf(t *testing.T, recorder *httptest.ResponseRecorder) QItem {
	t.Helper()
	var record struct{ Item QItem }
	if recorder.Code != 201 || json.Unmarshal(recorder.Body.Bytes(), &record) != nil {
		t.Fatalf("ajout : %d %s", recorder.Code, recorder.Body)
	}
	return record.Item
}

func TestInviteNameSanitized(t *testing.T) {
	long := strings.Repeat("é", 30)
	for in, want := range map[string]string{
		"": "", "Léa": "Léa", "  O'Neil-Jr_2.0  ": "O'Neil-Jr_2.0",
		"[Hôte] x":        "Hôte x",
		"a]\n[Invité b":   "a Invité b",
		"a\r\n\tb":        "a b",
		"x\u202Ey\u2066z": "xyz",
		"x\u200Bw\uFEFF":  "xw",
		"😀 Bob":           "Bob",
		"😀":               "",
		"<b>x</b>":        "bxb",
		"\x00\x1b[31m":    "31m",
		long:              strings.Repeat("é", inviteNameMax),
	} {
		if got := inviteName(in); got != want {
			t.Errorf("%q : %q, attendu %q", in, got, want)
		}
	}
	shareStore, _ := newShares(t)
	_, invite, _ := shareStore.CreateInvite(shSess, "dir", "", "[x]\n")
	_, localInvite, _ := shareStore.CreateInvite(shSess, "dir", "", "😀")
	if invite.label() != "x" || localInvite.label() != "Invité 2" || localInvite.Name != "" {
		t.Fatalf("libellés : %q %q", invite.label(), localInvite.label())
	}
}

func TestInviteCap10(t *testing.T) {
	mux := shareMux(t)
	var first string
	for index := range inviteMax {
		recorder := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj"}`)
		var record struct {
			Token  string
			Invite struct {
				ID    string
				Seq   int
				Label string
			}
		}
		if recorder.Code != 201 || json.Unmarshal(recorder.Body.Bytes(), &record) != nil || record.Token == "" || record.Invite.Seq != index+1 || !validHandle(record.Invite.ID) {
			t.Fatalf("invitation %d : %d %s", index+1, recorder.Code, recorder.Body)
		}
		if index == 0 {
			first = record.Invite.ID
		}
	}
	localRecorder := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj"}`)
	if localRecorder.Code != 409 || !strings.Contains(localRecorder.Body.String(), `"error"`) || strings.Contains(localRecorder.Body.String(), "token") {
		t.Fatalf("11e : %d %s", localRecorder.Code, localRecorder.Body)
	}
	if sh, _ := shareSt.Get(shSess); len(sh.Invites) != inviteMax {
		t.Fatalf("%d invitations", len(sh.Invites))
	}
	if recorder := send(mux, "DELETE", "/v1/invites/"+first+"?id="+shSess, ""); recorder.Code != 204 {
		t.Fatalf("révocation : %d", recorder.Code)
	}
	_, in, err := shareSt.CreateInvite(shSess, "proj", "", "")
	if err != nil || in.Seq != inviteMax+1 || in.label() != "Invité 11" {
		t.Fatalf("après révocation : %+v %v", in, err)
	}
}

func TestTwoInvitesIndependentCookiesAndModes(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, handler, id1, "-p", "", "Léa")
	cb, csrfB, invB := joinAs(t, handler, id1, "-p", "", "")
	if invA == invB || ca == cb {
		t.Fatal("invitations confondues")
	}
	for _, record := range []struct{ cookie, inv, label string }{{ca, invA, "Léa"}, {cb, invB, "Invité 2"}} {
		recorder := gReq{method: "GET", path: "/v1/guest/me", cookie: record.cookie}.do(handler)
		var me map[string]any
		json.Unmarshal(recorder.Body.Bytes(), &me)
		if me["invite"] != record.inv || me["label"] != record.label || me["mode"] != "rw" || me["id"] != id1 {
			t.Fatalf("guest-me : %s", recorder.Body)
		}
	}
	host, _ := queueSt.Add(id1, "host", "", "de l'hôte")
	qItem := itemOf(t, addAs(handler, ca, csrfA, "de A", nil))
	localQItem := itemOf(t, addAs(handler, cb, csrfB, "de B", nil))
	if qItem.Guest != invA || localQItem.Guest != invB {
		t.Fatalf("auteurs : %+v %+v", qItem, localQItem)
	}
	for _, it := range []string{qItem.ID, host.ID} {
		if recorder := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + it, cookie: cb, csrf: csrfB}).do(handler); recorder.Code != 403 {
			t.Fatalf("B retire %s : %d", it, recorder.Code)
		}
	}
	if recorder := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + qItem.ID, cookie: ca, csrf: csrfB}).do(handler); recorder.Code != 403 {
		t.Fatalf("csrf de B pour A : %d", recorder.Code)
	}
	if recorder := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + qItem.ID, cookie: ca, csrf: csrfA}).do(handler); recorder.Code != 200 {
		t.Fatalf("A retire le sien : %d", recorder.Code)
	}
	if err := shareSt.SetInviteMode(id1, invB, "ro"); err != nil {
		t.Fatal(err)
	}
	if recorder := addAs(handler, cb, csrfB, "x", nil); recorder.Code != 403 {
		t.Fatalf("B en lecture seule : %d", recorder.Code)
	}
	if recorder := addAs(handler, ca, csrfA, "y", nil); recorder.Code != 201 {
		t.Fatalf("A en écriture : %d", recorder.Code)
	}
	localRecorder := gReq{method: "GET", path: "/v1/guest/me", cookie: cb}.do(handler)
	if !strings.Contains(localRecorder.Body.String(), `"mode":"ro"`) {
		t.Fatalf("mode de B : %s", localRecorder.Body)
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cb}).do(handler); recorder.Code != 200 {
		t.Fatalf("B lit : %d", recorder.Code)
	}
	// B ne voit que l'hôte et lui-même : ni texte, ni libellé, ni identifiant d'invitation de A, par aucune route.
	seen := queueSeen(t, handler, gReq{cookie: cb})
	for _, path := range []string{"/queue", "/live"} {
		got := seen[path]
		if _, ok := got["de B"]; !ok || path == "/queue" && got["de B"] != "Invité 2" || got["de l'hôte"] != "" || len(got) != 2 {
			t.Fatalf("%s vu par B : %v", path, got)
		}
	}
	for path, raw := range queueRaw(handler, gReq{cookie: cb}) {
		for _, leak := range []string{`"y"`, "Léa", invA} {
			if strings.Contains(raw, leak) {
				t.Fatalf("%s : fuite %q : %s", path, leak, raw)
			}
		}
	}
	hostSeen := queueSeen(t, handler, gReq{tokCookie: modeTok})
	for _, path := range []string{"/queue", "/live"} {
		if _, ok := hostSeen[path]["y"]; len(hostSeen[path]) != 3 || !ok || path == "/queue" && hostSeen[path]["y"] != "Léa" {
			t.Fatalf("%s vu par l'hôte : %v", path, hostSeen[path])
		}
	}
}

func queueRaw(handler http.Handler, base gReq) map[string]string {
	out := map[string]string{}
	for _, localPath := range []string{"/queue", "/live"} {
		gReq := base
		gReq.method, gReq.path = "GET", "/v1/sessions/"+id1+localPath+"?dir=-p"
		out[localPath] = gReq.do(handler).Body.String()
	}
	return out
}

// queueSeen : texte -> libellé des éléments rendus par /queue (items) et /live (pending).
func queueSeen(t *testing.T, handler http.Handler, base gReq) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for path, raw := range queueRaw(handler, base) {
		var record struct {
			Items, Pending []struct{ Text, Label string }
		}
		if json.Unmarshal([]byte(raw), &record) != nil {
			t.Fatalf("%s : %s", path, raw)
		}
		lookup := map[string]string{}
		for _, it := range append(record.Items, record.Pending...) {
			lookup[it.Text] = it.Label
		}
		out[path] = lookup
	}
	return out
}

func TestRegenKillsOnlyThatInvite(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, handler, id1, "-p", "", "Léa")
	cb, csrfB, _ := joinAs(t, handler, id1, "-p", "", "")
	sending := itemOf(t, addAs(handler, ca, csrfA, "en cours", nil))
	queueSt.MarkSending(id1, sending.ID)
	itemOf(t, addAs(handler, ca, csrfA, "fuité", nil))
	kb := itemOf(t, addAs(handler, cb, csrfB, "de B", nil))
	shareSt.SetInviteMode(id1, invA, "ro")
	tok, err := shareSt.Regen(id1, invA)
	if err != nil {
		t.Fatal(err)
	}
	// Un lien régénéré est un lien fuité : les éléments encore en file de cette invitation partent avec lui.
	if qItems := queueSt.List(id1); len(qItems) != 2 || qItems[0].ID != sending.ID || qItems[1].ID != kb.ID {
		t.Fatalf("file après régénération : %+v", qItems)
	}
	recorder := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: ca}.do(handler)
	if cookie := cookieOf(recorder, gName); recorder.Code != 401 || cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("ancien cookie de A : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cb}).do(handler); recorder.Code != 200 {
		t.Fatalf("B : %d", recorder.Code)
	}
	sh, _ := shareSt.Get(id1)
	in, _ := sh.invite(invA)
	if in.Mode != "ro" || in.Name != "Léa" || in.CookieHash != "" {
		t.Fatalf("après régénération : %+v", in)
	}
	recorder = gReq{method: "POST", path: "/invite/main/" + tok}.do(handler)
	if cookie := cookieOf(recorder, gName); recorder.Code != 303 || cookie == nil {
		t.Fatalf("nouveau lien : %d", recorder.Code)
	}
	tokC, inC, _ := shareSt.CreateInvite(id1, "-p", "", "")
	tokC2, _ := shareSt.Regen(id1, inC.ID)
	if recorder := (gReq{method: "POST", path: "/invite/main/" + tokC}).do(handler); recorder.Code != 403 {
		t.Fatalf("ancien jeton : %d", recorder.Code)
	}
	if recorder := (gReq{method: "POST", path: "/invite/main/" + tokC2}).do(handler); recorder.Code != 303 {
		t.Fatalf("jeton régénéré : %d", recorder.Code)
	}
	if _, err := shareSt.Regen(id1, "0000000000000000"); !errors.Is(err, errInviteNone) {
		t.Fatalf("invitation inconnue : %v", err)
	}
}

func TestRevokeDropsItsQueueAndKeepsOthers(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, handler, id1, "-p", "", "")
	cb, csrfB, invB := joinAs(t, handler, id1, "-p", "", "")
	host, _ := queueSt.Add(id1, "host", "", "h")
	itemOf(t, addAs(handler, ca, csrfA, "a", nil))
	qItem := itemOf(t, addAs(handler, cb, csrfB, "b", nil))
	if err := shareSt.Revoke(id1, invA); err != nil {
		t.Fatal(err)
	}
	if qItems := queueSt.List(id1); len(qItems) != 2 || qItems[0].ID != host.ID || qItems[1].ID != qItem.ID {
		t.Fatalf("file : %+v", qItems)
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: ca}).do(handler); recorder.Code != 401 {
		t.Fatalf("A révoqué : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cb}).do(handler); recorder.Code != 200 {
		t.Fatalf("B : %d", recorder.Code)
	}
	if !errors.Is(shareSt.Revoke(id1, invA), errInviteNone) {
		t.Fatal("double révocation")
	}
	if err := shareSt.Revoke(id1, invB); err != nil {
		t.Fatal(err)
	}
	if _, ok := shareSt.Get(id1); ok {
		t.Fatal("partage sans invitation conservé")
	}
	if qItems := queueSt.List(id1); len(qItems) != 1 || qItems[0].ID != host.ID {
		t.Fatalf("file : %+v", qItems)
	}
}

func TestRevokeInMemoryOnWriteFailure(t *testing.T) {
	dir := roDir(t)
	shareStore, _ := openShares(dir)
	tok, in, _ := shareStore.CreateInvite(shSess, "dir", "", "")
	content, _, _, _ := shareStore.Redeem(tok)
	_, other, _ := shareStore.CreateInvite(shSess, "dir", "", "")
	lock(t, dir)
	if err := shareStore.Revoke(shSess, in.ID); err == nil {
		t.Fatal("erreur attendue")
	}
	if _, _, ok := shareStore.ByCookie(content); ok {
		t.Fatal("cookie révoqué encore valide en mémoire")
	}
	if _, err := shareStore.Regen(shSess, other.ID); err == nil {
		t.Fatal("régénération sans écriture acceptée")
	}
	if _, _, err := shareStore.CreateInvite(shSess, "dir", "", ""); err == nil {
		t.Fatal("création sans écriture acceptée")
	}
	if sh, _ := shareStore.Get(shSess); len(sh.Invites) != 1 || sh.Invites[0].TokenHash != other.TokenHash || sh.Seq != 2 {
		t.Fatalf("état après échecs : %+v", sh)
	}
}

func TestInviteRoQueuedItemsRemoved(t *testing.T) {
	mux := shareMux(t)
	oq := queueSt
	queueSt, _ = openQueue(t.TempDir())
	t.Cleanup(func() { queueSt = oq })
	_, localInvite, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	_, anotherInvite, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	qItem, _ := queueSt.Add(shSess, "guest", localInvite.ID, "en cours")
	queueSt.MarkSending(shSess, qItem.ID)
	queueSt.Add(shSess, "guest", localInvite.ID, "a")
	kb, _ := queueSt.Add(shSess, "guest", anotherInvite.ID, "b")
	kh, _ := queueSt.Add(shSess, "host", "", "h")
	if recorder := send(mux, "PATCH", "/v1/invites/"+localInvite.ID, `{"id":"`+shSess+`","mode":"x"}`); recorder.Code != 400 {
		t.Fatalf("mode invalide : %d", recorder.Code)
	}
	if recorder := send(mux, "PATCH", "/v1/invites/"+localInvite.ID, `{"id":"`+shSess+`","mode":"ro"}`); recorder.Code != 204 {
		t.Fatalf("mode : %d %s", recorder.Code, recorder.Body)
	}
	qItems := queueSt.List(shSess)
	if len(qItems) != 3 || qItems[0].ID != qItem.ID || qItems[1].ID != kb.ID || qItems[2].ID != kh.ID {
		t.Fatalf("file : %+v", qItems)
	}
	sh, _ := shareSt.Get(shSess)
	if ia, _ := sh.invite(localInvite.ID); ia.Mode != "ro" {
		t.Fatal("mode non appliqué")
	}
	if ib, _ := sh.invite(anotherInvite.ID); ib.Mode != "rw" {
		t.Fatal("mode de B touché")
	}
	if recorder := send(mux, "PATCH", "/v1/invites/0000000000000000", `{"id":"`+shSess+`","mode":"ro"}`); recorder.Code != 404 {
		t.Fatalf("invitation inconnue : %d", recorder.Code)
	}
}

// drainInvites : partage de shSess avec Léa (rw) et une invitation sans nom (rw), sessions ouvertes et au repos.
func drainInvites(t *testing.T) (*queueStore, *[]string, *shareStore, Invite, Invite) {
	t.Helper()
	localQueueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	localShareStore, _ := newShares(t)
	_, invite, _ := localShareStore.CreateInvite(shSess, "dir", "", "Léa")
	_, localInvite, _ := localShareStore.CreateInvite(shSess, "dir", "", "")
	shareSt = localShareStore
	return localQueueStore, sent, localShareStore, invite, localInvite
}

func TestDrainLabelPerInvite(t *testing.T) {
	queueStore, sent, shareStore, invite, localInvite := drainInvites(t)
	queueStore.Add(shSess, "guest", invite.ID, "/exit")
	queueStore.Add(shSess, "guest", localInvite.ID, "/clear")
	queueStore.Add(shSess, "host", "", "/compact")
	now := time.Now()
	for index := range 3 {
		drainOnce(queueStore, now.Add(time.Duration(index)*5*time.Second))
	}
	want := []string{shSess + "|" + followPrefix + "[Invité Léa] /exit", shSess + "|" + followPrefix + "[Invité 2] /clear", shSess + "|/compact"}
	if strings.Join(*sent, "\n") != strings.Join(want, "\n") {
		t.Fatalf("envoyés : %q", *sent)
	}
	if _, err := queueStore.Add(shSess, "guest", "", "x"); err != errQueueText {
		t.Fatalf("élément invité sans invitation accepté : %v", err)
	}
	// Nom le plus long (24 caractères de 4 octets) et texte à la borne exacte : étiquette et préfixe tiennent dans sendMax.
	big := strings.Repeat("\U00020000", inviteNameMax)
	_, anotherInvite, _ := shareStore.CreateInvite(shSess, "dir", "", big)
	if anotherInvite.Name != big || len(guestTag(anotherInvite)) != guestLabelMax {
		t.Fatalf("nom long : %q %d/%d", anotherInvite.Name, len(guestTag(anotherInvite)), guestLabelMax)
	}
	max := sendMax - guestLabelMax - len(followPrefix)
	if _, err := queueStore.Add(shSess, "guest", anotherInvite.ID, strings.Repeat("x", max+1)); err != errQueueText {
		t.Fatalf("un octet de trop : %v", err)
	}
	text := strings.Repeat("x", max)
	queueStore.Add(shSess, "guest", anotherInvite.ID, text)
	queueStore.Add(shSess, "host", "", "suite")
	*sent = nil
	drainOnce(queueStore, now.Add(time.Minute))
	got := strings.TrimPrefix((*sent)[0], shSess+"|")
	if got != followPrefix+guestTag(anotherInvite)+text || len(got) != sendMax || !utf8.ValidString(got) {
		t.Fatalf("borne : %d octets", len(got))
	}
}

// Une étiquette lue d'un fichier altéré est réassainie : jamais de crochet ni de saut de ligne, jamais plus que guestLabelMax.
func TestDrainTagResanitized(t *testing.T) {
	for _, name := range []string{"a]\n/exit", strings.Repeat("\U00020000", 200), "\u202E"} {
		tag := guestTag(Invite{Seq: 3, Name: name})
		if len(tag) > guestLabelMax || strings.ContainsAny(tag[1:len(tag)-2], "[]\n") || !strings.HasPrefix(tag, "[Invité ") {
			t.Fatalf("%q : %q", name, tag)
		}
	}
	if guestTag(Invite{Seq: 3, Name: "\u202E"}) != "[Invité 3] " {
		t.Fatal("nom vide après assainissement")
	}
}

func TestDrainDropsRevokedOrRoInvite(t *testing.T) {
	for _, content := range []string{"révoquée", "lecture seule", "partage absent", "bascule"} {
		queueStore, sent, shareStore, invite, localInvite := drainInvites(t)
		queueStore.Add(shSess, "guest", invite.ID, "x")
		switch content {
		case "révoquée":
			shareStore.Revoke(shSess, invite.ID)
			queueStore.Add(shSess, "guest", invite.ID, "y") // ajouté après la révocation (reprise d'une écriture ratée)
		case "lecture seule":
			shareStore.SetInviteMode(shSess, invite.ID, "ro")
		case "partage absent":
			shareStore.End(shSess)
			queueStore.Add(shSess, "guest", invite.ID, "y")
		case "bascule":
			screenFn = func(string) (string, error) { shareStore.SetInviteMode(shSess, invite.ID, "ro"); return "", nil }
		}
		queueStore.Add(shSess, "guest", localInvite.ID, "de B")
		for index := range 4 {
			drainOnce(queueStore, time.Now().Add(time.Duration(index)*20*time.Second))
		}
		for _, message := range *sent {
			if !strings.Contains(message, "de B") {
				t.Fatalf("%s : envoyé %q", content, message)
			}
		}
		for _, it := range queueStore.List(shSess) {
			if it.Guest == invite.ID {
				t.Fatalf("%s : élément de A resté %+v", content, it)
			}
		}
		if content != "partage absent" && len(*sent) != 1 {
			t.Fatalf("%s : B non envoyé %q", content, *sent)
		}
	}
}

func TestGuestRateLimitPerInvite(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA, _ := joinAs(t, handler, id1, "-p", "", "")
	cb, csrfB, _ := joinAs(t, handler, id1, "-p", "", "")
	for index := range guestAddMax + 1 {
		want := map[bool]int{true: 429, false: 201}[index == guestAddMax]
		if recorder := addAs(handler, ca, csrfA, "m", nil); recorder.Code != want {
			t.Fatalf("A ajout %d : %d", index+1, recorder.Code)
		}
	}
	if recorder := addAs(handler, cb, csrfB, "m", nil); recorder.Code != 201 {
		t.Fatalf("B limité par A : %d", recorder.Code)
	}
}

func TestSharesOverview(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	oo := shareOpen
	t.Cleanup(func() { shareOpen = oo })
	shareOpen = func(id string) bool { return id == id1 }
	ca, csrfA, invA := joinAs(t, handler, id1, "-p", "Titre", "Léa")
	_, _, invB := joinAs(t, handler, id1, "-p", "", "")
	_, inC, _ := shareSt.CreateInvite(id1, "-p", "", "")
	joinAs(t, handler, gOther, "-q", "", "")
	shareSt.mu.Lock()
	sh := shareSt.m[id1]
	sh.Invites[sh.inv(invB)].LastSeen = time.Now().Add(-time.Minute).Unix()
	shareSt.mu.Unlock()
	long := strings.Repeat("é", 250)
	itemOf(t, addAs(handler, ca, csrfA, long, nil))
	queueSt.Add(id1, "host", "", "de l'hôte")
	recorder := gReq{method: "GET", path: "/v1/shares", tokCookie: modeTok}.do(handler)
	var record struct {
		Shares []struct {
			ID, Dir, Title, Handle string
			Open                   bool
			Invites                []struct {
				ID, Name, Label, Mode, State string
				Seq                          int
				LastSeen                     int64
				Redeemed                     bool
				Queue                        []struct {
					ID, Text, State string
					At              int64
				}
			}
		}
	}
	if recorder.Code != 200 || recorder.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(recorder.Body.Bytes(), &record) != nil || len(record.Shares) != 2 {
		t.Fatalf("vue : %d %s", recorder.Code, recorder.Body)
	}
	localRecord := record.Shares[0]
	if localRecord.ID != id1 {
		localRecord = record.Shares[1]
	}
	if localRecord.Dir != "-p" || localRecord.Title != "Titre" || localRecord.Handle != hID1 || !localRecord.Open || len(localRecord.Invites) != 3 {
		t.Fatalf("partage : %+v", localRecord)
	}
	anotherRecord, extraRecord, record2 := localRecord.Invites[0], localRecord.Invites[1], localRecord.Invites[2]
	if anotherRecord.ID != invA || anotherRecord.Label != "Léa" || anotherRecord.Name != "Léa" || anotherRecord.State != "connected" || !anotherRecord.Redeemed || anotherRecord.Mode != "rw" || anotherRecord.Seq != 1 {
		t.Fatalf("A : %+v", anotherRecord)
	}
	if len(anotherRecord.Queue) != 1 || anotherRecord.Queue[0].Text != strings.Repeat("é", 200) || anotherRecord.Queue[0].State != "queued" || anotherRecord.Queue[0].At == 0 {
		t.Fatalf("file de A : %+v", anotherRecord.Queue)
	}
	if extraRecord.ID != invB || extraRecord.State != "away" || extraRecord.Label != "Invité 2" || len(extraRecord.Queue) != 0 {
		t.Fatalf("B : %+v", extraRecord)
	}
	if record2.ID != inC.ID || record2.State != "pending" || record2.Redeemed {
		t.Fatalf("C : %+v", record2)
	}
	if strings.Contains(recorder.Body.String(), "de l'hôte") || strings.Contains(recorder.Body.String(), "Hash") {
		t.Fatalf("fuite : %s", recorder.Body)
	}
	for _, other := range record.Shares {
		if other.ID == gOther && other.Open {
			t.Fatal("open de l'autre session")
		}
	}
	if recorder := (gReq{method: "GET", path: "/v1/shares", cookie: ca}).do(handler); recorder.Code != 403 || strings.Contains(recorder.Body.String(), long[:20]) {
		t.Fatalf("invité : %d", recorder.Code)
	}
	if recorder := (gReq{method: "GET", path: "/v1/shares"}).do(handler); recorder.Code != 401 {
		t.Fatalf("anonyme : %d", recorder.Code)
	}
	shareSt = nil
	if recorder := (gReq{method: "GET", path: "/v1/shares", tokCookie: modeTok}).do(handler); recorder.Code != 503 {
		t.Fatalf("magasin absent : %d", recorder.Code)
	}
}

func TestHostRoutesNotInGuestWhitelist(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	content, csrf, inv := joinAs(t, handler, id1, "-p", "", "")
	body := `{"id":"` + id1 + `","dir":"-p","invite":"` + inv + `","mode":"rw"}`
	for _, rt := range [][3]string{{"POST", "/v1/invites", "/v1/invites"}, {"PUT", "/v1/invites/" + inv + "/token", "/v1/invites/{invite}/token"}, {"PATCH", "/v1/invites/" + inv, "/v1/invites/{invite}"},
		{"DELETE", "/v1/invites/" + inv, "/v1/invites/{invite}"}, {"DELETE", "/v1/shares/" + id1, "/v1/shares/{share}"}, {"GET", "/v1/shares", "/v1/shares"}} {
		if guestRoutes[rt[0]+" "+rt[2]] {
			t.Errorf("%s %s dans la liste blanche", rt[0], rt[2])
		}
		if recorder := (gReq{method: rt[0], path: rt[1], body: body, cookie: content, csrf: csrf}).do(handler); recorder.Code != 403 {
			t.Errorf("%s %s invité : %d", rt[0], rt[1], recorder.Code)
		}
	}
	if sh, _ := shareSt.Get(id1); len(sh.Invites) != 1 || sh.Invites[0].CookieHash == "" {
		t.Fatalf("partage modifié par l'invité : %+v", sh)
	}
	for _, path := range []string{"POST /share-create", "GET /share-info", "POST /share-mode"} {
		if _, ok := routeRights[path]; ok {
			t.Errorf("%s encore déclarée", path)
		}
	}
}

func TestOldSharesFileIgnored(t *testing.T) {
	dir := t.TempDir()
	content := "secret-ancien"
	old := `{"` + shSess + `":{"session":"` + shSess + `","dir":"dir","mode":"rw","tokenHash":"` + hashSecret("tok") + `","cookieHash":"` + hashSecret(content) + `"},` +
		`"` + gOther + `":{"session":"` + gOther + `","dir":"-p","seq":1,"invites":[{"id":"0123456789abcdef","seq":1,"mode":"rw","tokenHash":"x"}]},"z":null}`
	os.WriteFile(filepath.Join(dir, "shares.json"), []byte(old), 0o600)
	shareStore, err := openShares(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shareStore.Get(shSess); ok {
		t.Fatal("ancien partage conservé")
	}
	if _, _, ok := shareStore.ByCookie(content); ok {
		t.Fatal("ancien cookie accepté")
	}
	if _, _, _, ok := shareStore.Redeem("tok"); ok {
		t.Fatal("ancien jeton accepté")
	}
	if sh, ok := shareStore.Get(gOther); !ok || len(sh.Invites) != 1 {
		t.Fatal("partage au nouveau format perdu")
	}
	if _, _, err := shareStore.CreateInvite(shSess, "dir", "", ""); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "shares.json")); strings.Contains(string(data), hashSecret(content)) {
		t.Fatal("ancienne entrée réécrite")
	}
}

func TestInviteHeaderSpoofIgnored(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, handler, id1, "-p", "", "")
	_, _, invB := joinAs(t, handler, id1, "-p", "", "")
	qItem := itemOf(t, addAs(handler, ca, csrfA, "a", map[string]string{authorHeader: "host", inviteHeader: invB}))
	if qItem.Author != "guest" || qItem.Guest != invA {
		t.Fatalf("invité forgé : %+v", qItem)
	}
	recorder := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"id":"` + id1 + `","text":"h"}`, tokCookie: modeTok, hdr: map[string]string{authorHeader: "guest", inviteHeader: invB}}.do(handler)
	if it := itemOf(t, recorder); it.Author != "host" || it.Guest != "" {
		t.Fatalf("hôte forgé : %+v", it)
	}
}

// Passerelle vers un home enfant : les en-têtes internes du client sont retirés ; l'enfant déduit l'invitation du secret.
func TestInviteHeaderStrippedAtRelayAndChildDerivesInvite(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	var author, invite []string
	child := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		author, invite = append(author, request.Header.Get(authorHeader)), append(invite, request.Header.Get(inviteHeader))
		responseWriter.Write([]byte(`{"item":{}}`))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(home Home) (*homeProc, error) {
		return &homeProc{dir: home.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	spoof := map[string]string{authorHeader: "guest", inviteHeader: "0123456789abcdef"}
	addAs(handler, "perso."+sec, guestCSRF(sec), "x", spoof)
	localRequest := httptest.NewRequest("POST", "/v1/sessions/"+id1+"/queue", strings.NewReader(`{"text":"h"}`))
	localRequest.Host = testHost
	localRequest.AddCookie(&http.Cookie{Name: tokenCookie, Value: modeTok})
	localRequest.AddCookie(&http.Cookie{Name: homeCookie, Value: "perso"})
	for key, value := range spoof {
		localRequest.Header.Set(key, value)
	}
	handler.ServeHTTP(httptest.NewRecorder(), localRequest)
	if len(author) != 2 || strings.Join(author, "") != "" || strings.Join(invite, "") != "" {
		t.Fatalf("vu par l'enfant : %q %q", author, invite)
	}

	// Côté enfant : le secret relayé désigne l'invitation, l'ajout porte son identifiant.
	hc := guestWeb(t, nil, "")
	oc := childToken
	t.Cleanup(func() { childToken = oc })
	childToken = "ctok"
	tok2, in2, _ := shareSt.CreateInvite(id1, "-p", "", "Léa")
	sec2, _, _, _ := shareSt.Redeem(tok2)
	hdr := map[string]string{childHeader: "ctok", guestHeader: sec2, shareHeader: hID1, csrfHeader: guestCSRF(sec2), inviteHeader: "0123456789abcdef", authorHeader: "host"}
	recorder := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"m"}`, hdr: hdr}.do(hc)
	if it := itemOf(t, recorder); it.Author != "guest" || it.Guest != in2.ID {
		t.Fatalf("enfant : %+v", it)
	}
}

func TestInviteRoutes(t *testing.T) {
	mux := shareMux(t)
	for _, values := range [][2]string{{"/v1/invites/0123456789abcdef/token", `{"id":"nope","dir":"proj"}`}, {"/v1/invites/x/token", `{"id":"` + shSess + `","dir":"proj"}`}} {
		if recorder := send(mux, "PUT", values[0], values[1]); recorder.Code != 400 {
			t.Fatalf("%s : %d", values[1], recorder.Code)
		}
	}
	if recorder := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"absent"}`); recorder.Code != 404 {
		t.Fatalf("session absente : %d", recorder.Code)
	}
	if recorder := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj","name":"`+strings.Repeat("x", 5<<10)+`"}`); recorder.Code != 400 {
		t.Fatalf("corps trop gros : %d", recorder.Code)
	}
	localRecorder := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj","title":" Ma\tsession ","name":"[Léa]"}`)
	var record struct {
		Token  string
		Invite struct{ ID, Label string }
	}
	if localRecorder.Code != 201 || json.Unmarshal(localRecorder.Body.Bytes(), &record) != nil || record.Invite.Label != "Léa" {
		t.Fatalf("création : %d %s", localRecorder.Code, localRecorder.Body)
	}
	if sh, _ := shareSt.Get(shSess); sh.Title != "Ma session" || sh.Dir != "proj" {
		t.Fatalf("partage : %+v", sh)
	}
	post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj"}`)
	if sh, _ := shareSt.Get(shSess); sh.Title != "Ma session" {
		t.Fatalf("titre effacé sans titre fourni : %q", sh.Title)
	}
	localRecorder = send(mux, "PUT", "/v1/invites/"+record.Invite.ID+"/token", `{"id":"`+shSess+`"}`)
	if localRecorder.Code != 200 || !strings.Contains(localRecorder.Body.String(), `"token":"`) || strings.Contains(localRecorder.Body.String(), record.Token) {
		t.Fatalf("régénération : %d %s", localRecorder.Code, localRecorder.Body)
	}
	for _, values := range [][3]string{{"PUT", "/v1/invites/" + record.Invite.ID + "/token", `{"id":"` + gOther + `"}`}, {"DELETE", "/v1/invites/" + record.Invite.ID + "?id=" + gOther, ""}} {
		if recorder := send(mux, values[0], values[1], values[2]); recorder.Code != 404 {
			t.Fatalf("%s autre session : %d", values[1], recorder.Code)
		}
	}
	if recorder := send(mux, "DELETE", "/v1/invites/"+record.Invite.ID+"?id="+shSess, ""); recorder.Code != 204 {
		t.Fatalf("révocation : %d", recorder.Code)
	}
	if recorder := send(mux, "DELETE", "/v1/shares/"+shSess, ""); recorder.Code != 204 {
		t.Fatalf("fin : %d", recorder.Code)
	}
	if _, ok := shareSt.Get(shSess); ok {
		t.Fatal("partage encore là")
	}
	shareSt = nil
	for _, values := range [][2]string{{"POST", "/v1/invites"}, {"PUT", "/v1/invites/0123456789abcdef/token"}, {"PATCH", "/v1/invites/0123456789abcdef"}, {"DELETE", "/v1/invites/0123456789abcdef?id=" + shSess}, {"DELETE", "/v1/shares/" + shSess}} {
		if recorder := send(mux, values[0], values[1], `{"id":"`+shSess+`","dir":"proj","mode":"ro"}`); recorder.Code != 503 {
			t.Fatalf("%s %s : %d", values[0], values[1], recorder.Code)
		}
	}
}

// Fuite de lien ou invités multiples : les invités ne remplissent jamais toute la file, l'hôte peut toujours ajouter.
func TestQueueGuestCapPerSession(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, handler, id1, "-p", "", "")
	cb, csrfB, _ := joinAs(t, handler, id1, "-p", "", "")
	for index := range queueGuestMax - 1 {
		if _, err := queueSt.Add(id1, "guest", invA, "g"); err != nil {
			t.Fatalf("élément invité %d : %v", index+1, err)
		}
	}
	if recorder := addAs(handler, cb, csrfB, "30e", nil); recorder.Code != 201 {
		t.Fatalf("30e : %d", recorder.Code)
	}
	for _, record := range []struct{ c, csrf string }{{ca, csrfA}, {cb, csrfB}} {
		if recorder := addAs(handler, record.c, record.csrf, "31e", nil); recorder.Code != 409 || !strings.Contains(recorder.Body.String(), L("srv.queue.pleine")) {
			t.Fatalf("31e invité : %d %s", recorder.Code, recorder.Body)
		}
	}
	localRecorder := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"id":"` + id1 + `","text":"hôte"}`, tokCookie: modeTok}.do(handler)
	if it := itemOf(t, localRecorder); it.Author != "host" {
		t.Fatalf("hôte : %+v", it)
	}
}

// Requête hôte relayée à un enfant : le proxy retire lui-même les en-têtes internes, sans compter sur la garde de la passerelle.
func TestChildProxyStripsInviteHeaders(t *testing.T) {
	var got http.Header
	child := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { got = request.Header.Clone() }))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	localRequest := httptest.NewRequest("POST", "/v1/sessions/"+id1+"/queue", strings.NewReader(`{}`))
	localRequest.Header.Set(authorHeader, "guest")
	localRequest.Header.Set(inviteHeader, "0123456789abcdef")
	childProxy(cu, "x").ServeHTTP(httptest.NewRecorder(), localRequest)
	if got == nil || len(got.Values(authorHeader)) != 0 || len(got.Values(inviteHeader)) != 0 {
		t.Fatalf("vu par l'enfant : %v", got)
	}
}

// Côté enfant (invité relayé par la passerelle) : même filtrage, la garde de l'enfant pose les mêmes en-têtes.
func TestChildGuestSeesOnlyOwnQueue(t *testing.T) {
	handler := guestWeb(t, nil, "")
	oc := childToken
	t.Cleanup(func() { childToken = oc })
	childToken = "ctok"
	_, invite, _ := shareSt.CreateInvite(id1, "-p", "", "Léa")
	tok, localInvite, _ := shareSt.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := shareSt.Redeem(tok)
	queueSt.Add(id1, "guest", invite.ID, "de A")
	queueSt.Add(id1, "guest", localInvite.ID, "de B")
	queueSt.Add(id1, "host", "", "de l'hôte")
	seen := queueSeen(t, handler, gReq{hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1}})
	for _, path := range []string{"/queue", "/live"} {
		_, flag := seen[path]["de B"]
		_, localFlag := seen[path]["de A"]
		if got := seen[path]; len(got) != 2 || !flag || localFlag || path == "/queue" && got["de B"] != "Invité 2" {
			t.Fatalf("%s enfant : %v", path, got)
		}
	}
}

// Purge non écrite : la régénération reste appliquée et la purge tient en mémoire, mais l'hôte apprend l'échec (500).
func TestRegenQueuePurgeWriteFailure(t *testing.T) {
	mux := shareMux(t)
	qd := roDir(t)
	oq := queueSt
	queueSt, _ = openQueue(qd)
	t.Cleanup(func() { queueSt = oq })
	_, in, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	queueSt.Add(shSess, "guest", in.ID, "fuité")
	old := in.TokenHash
	lock(t, qd)
	recorder := send(mux, "PUT", "/v1/invites/"+in.ID+"/token", `{"id":"`+shSess+`"}`)
	if recorder.Code != 500 || !strings.Contains(recorder.Body.String(), `"error"`) {
		t.Fatalf("régénération : %d %s", recorder.Code, recorder.Body)
	}
	if sh, _ := shareSt.Get(shSess); sh.Invites[0].TokenHash == old {
		t.Fatal("jeton non changé")
	}
	if qItems := queueSt.List(shSess); len(qItems) != 0 {
		t.Fatalf("purge annulée en mémoire : %+v", qItems)
	}
}

func TestRedeemedLinkReopensOnSameBrowser(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	tok, _, err := shareSt.CreateInvite(id1, "-p", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hd := shareHandle(id1)
	recorder := gReq{method: "POST", path: "/invite/main/" + tok}.do(handler)
	cookie := cookieOf(recorder, guestCookieName(hd))
	if recorder.Code != 303 || cookie == nil {
		t.Fatalf("invite : %d", recorder.Code)
	}
	recorder = gReq{method: "POST", path: "/invite/main/" + tok, jar: []*http.Cookie{cookie}}.do(handler)
	if recorder.Code != 303 || recorder.Header().Get("Location") != "/guest/"+hd {
		t.Fatalf("même navigateur : %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if recorder = (gReq{method: "POST", path: "/invite/main/" + tok}).do(handler); recorder.Code != 403 {
		t.Fatalf("autre navigateur : %d", recorder.Code)
	}
}
