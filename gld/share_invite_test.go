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
func joinAs(t *testing.T, h http.Handler, sess, dir, title, name string) (cookie, csrf, inv string) {
	t.Helper()
	tok, in, err := shareSt.CreateInvite(sess, dir, title, name)
	if err != nil {
		t.Fatal(err)
	}
	hd := shareHandle(sess)
	w := gReq{method: "POST", path: "/invite/main/" + tok}.do(h)
	c := cookieOf(w, guestCookieName(hd))
	if w.Code != 303 || c == nil {
		t.Fatalf("invite : %d", w.Code)
	}
	w = gReq{method: "GET", path: "/v1/guest/me", cookie: c.Value, share: hd}.do(h)
	var me struct{ ID, CSRF, Invite, Label, Mode string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil || me.Invite != in.ID || me.ID != sess {
		t.Fatalf("guest-me : %d %s", w.Code, w.Body)
	}
	return c.Value, me.CSRF, in.ID
}

func addAs(h http.Handler, c, csrf, text string, hdr map[string]string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]string{"text": text})
	return gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: string(b), cookie: c, csrf: csrf, hdr: hdr}.do(h)
}

func itemOf(t *testing.T, w *httptest.ResponseRecorder) QItem {
	t.Helper()
	var r struct{ Item QItem }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatalf("ajout : %d %s", w.Code, w.Body)
	}
	return r.Item
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
	s, _ := newShares(t)
	_, a, _ := s.CreateInvite(shSess, "dir", "", "[x]\n")
	_, b, _ := s.CreateInvite(shSess, "dir", "", "😀")
	if a.label() != "x" || b.label() != "Invité 2" || b.Name != "" {
		t.Fatalf("libellés : %q %q", a.label(), b.label())
	}
}

func TestInviteCap10(t *testing.T) {
	mux := shareMux(t)
	var first string
	for i := range inviteMax {
		w := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj"}`)
		var r struct {
			Token  string
			Invite struct {
				ID    string
				Seq   int
				Label string
			}
		}
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Token == "" || r.Invite.Seq != i+1 || !validHandle(r.Invite.ID) {
			t.Fatalf("invitation %d : %d %s", i+1, w.Code, w.Body)
		}
		if i == 0 {
			first = r.Invite.ID
		}
	}
	w := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj"}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error"`) || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("11e : %d %s", w.Code, w.Body)
	}
	if sh, _ := shareSt.Get(shSess); len(sh.Invites) != inviteMax {
		t.Fatalf("%d invitations", len(sh.Invites))
	}
	if w := send(mux, "DELETE", "/v1/invites/"+first+"?id="+shSess, ""); w.Code != 204 {
		t.Fatalf("révocation : %d", w.Code)
	}
	_, in, err := shareSt.CreateInvite(shSess, "proj", "", "")
	if err != nil || in.Seq != inviteMax+1 || in.label() != "Invité 11" {
		t.Fatalf("après révocation : %+v %v", in, err)
	}
}

func TestTwoInvitesIndependentCookiesAndModes(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, h, id1, "-p", "", "Léa")
	cb, csrfB, invB := joinAs(t, h, id1, "-p", "", "")
	if invA == invB || ca == cb {
		t.Fatal("invitations confondues")
	}
	for _, c := range []struct{ cookie, inv, label string }{{ca, invA, "Léa"}, {cb, invB, "Invité 2"}} {
		w := gReq{method: "GET", path: "/v1/guest/me", cookie: c.cookie}.do(h)
		var me map[string]any
		json.Unmarshal(w.Body.Bytes(), &me)
		if me["invite"] != c.inv || me["label"] != c.label || me["mode"] != "rw" || me["id"] != id1 {
			t.Fatalf("guest-me : %s", w.Body)
		}
	}
	host, _ := queueSt.Add(id1, "host", "", "de l'hôte")
	a := itemOf(t, addAs(h, ca, csrfA, "de A", nil))
	b := itemOf(t, addAs(h, cb, csrfB, "de B", nil))
	if a.Guest != invA || b.Guest != invB {
		t.Fatalf("auteurs : %+v %+v", a, b)
	}
	for _, it := range []string{a.ID, host.ID} {
		if w := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + it, cookie: cb, csrf: csrfB}).do(h); w.Code != 403 {
			t.Fatalf("B retire %s : %d", it, w.Code)
		}
	}
	if w := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + a.ID, cookie: ca, csrf: csrfB}).do(h); w.Code != 403 {
		t.Fatalf("csrf de B pour A : %d", w.Code)
	}
	if w := (gReq{method: "DELETE", path: "/v1/sessions/" + id1 + "/queue/" + a.ID, cookie: ca, csrf: csrfA}).do(h); w.Code != 200 {
		t.Fatalf("A retire le sien : %d", w.Code)
	}
	if err := shareSt.SetInviteMode(id1, invB, "ro"); err != nil {
		t.Fatal(err)
	}
	if w := addAs(h, cb, csrfB, "x", nil); w.Code != 403 {
		t.Fatalf("B en lecture seule : %d", w.Code)
	}
	if w := addAs(h, ca, csrfA, "y", nil); w.Code != 201 {
		t.Fatalf("A en écriture : %d", w.Code)
	}
	w := gReq{method: "GET", path: "/v1/guest/me", cookie: cb}.do(h)
	if !strings.Contains(w.Body.String(), `"mode":"ro"`) {
		t.Fatalf("mode de B : %s", w.Body)
	}
	if w := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cb}).do(h); w.Code != 200 {
		t.Fatalf("B lit : %d", w.Code)
	}
	// B ne voit que l'hôte et lui-même : ni texte, ni libellé, ni identifiant d'invitation de A, par aucune route.
	seen := queueSeen(t, h, gReq{cookie: cb})
	for _, p := range []string{"/queue", "/live"} {
		got := seen[p]
		if _, ok := got["de B"]; !ok || p == "/queue" && got["de B"] != "Invité 2" || got["de l'hôte"] != "" || len(got) != 2 {
			t.Fatalf("%s vu par B : %v", p, got)
		}
	}
	for p, raw := range queueRaw(h, gReq{cookie: cb}) {
		for _, leak := range []string{`"y"`, "Léa", invA} {
			if strings.Contains(raw, leak) {
				t.Fatalf("%s : fuite %q : %s", p, leak, raw)
			}
		}
	}
	hostSeen := queueSeen(t, h, gReq{tokCookie: modeTok})
	for _, p := range []string{"/queue", "/live"} {
		if _, ok := hostSeen[p]["y"]; len(hostSeen[p]) != 3 || !ok || p == "/queue" && hostSeen[p]["y"] != "Léa" {
			t.Fatalf("%s vu par l'hôte : %v", p, hostSeen[p])
		}
	}
}

func queueRaw(h http.Handler, base gReq) map[string]string {
	out := map[string]string{}
	for _, p := range []string{"/queue", "/live"} {
		q := base
		q.method, q.path = "GET", "/v1/sessions/"+id1+p+"?dir=-p"
		out[p] = q.do(h).Body.String()
	}
	return out
}

// queueSeen : texte -> libellé des éléments rendus par /queue (items) et /live (pending).
func queueSeen(t *testing.T, h http.Handler, base gReq) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for p, raw := range queueRaw(h, base) {
		var r struct {
			Items, Pending []struct{ Text, Label string }
		}
		if json.Unmarshal([]byte(raw), &r) != nil {
			t.Fatalf("%s : %s", p, raw)
		}
		m := map[string]string{}
		for _, it := range append(r.Items, r.Pending...) {
			m[it.Text] = it.Label
		}
		out[p] = m
	}
	return out
}

func TestRegenKillsOnlyThatInvite(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, h, id1, "-p", "", "Léa")
	cb, csrfB, _ := joinAs(t, h, id1, "-p", "", "")
	sending := itemOf(t, addAs(h, ca, csrfA, "en cours", nil))
	queueSt.MarkSending(id1, sending.ID)
	itemOf(t, addAs(h, ca, csrfA, "fuité", nil))
	kb := itemOf(t, addAs(h, cb, csrfB, "de B", nil))
	shareSt.SetInviteMode(id1, invA, "ro")
	tok, err := shareSt.Regen(id1, invA)
	if err != nil {
		t.Fatal(err)
	}
	// Un lien régénéré est un lien fuité : les éléments encore en file de cette invitation partent avec lui.
	if l := queueSt.List(id1); len(l) != 2 || l[0].ID != sending.ID || l[1].ID != kb.ID {
		t.Fatalf("file après régénération : %+v", l)
	}
	w := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: ca}.do(h)
	if k := cookieOf(w, gName); w.Code != 401 || k == nil || k.MaxAge >= 0 {
		t.Fatalf("ancien cookie de A : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cb}).do(h); w.Code != 200 {
		t.Fatalf("B : %d", w.Code)
	}
	sh, _ := shareSt.Get(id1)
	in, _ := sh.invite(invA)
	if in.Mode != "ro" || in.Name != "Léa" || in.CookieHash != "" {
		t.Fatalf("après régénération : %+v", in)
	}
	w = gReq{method: "POST", path: "/invite/main/" + tok}.do(h)
	if c := cookieOf(w, gName); w.Code != 303 || c == nil {
		t.Fatalf("nouveau lien : %d", w.Code)
	}
	tokC, inC, _ := shareSt.CreateInvite(id1, "-p", "", "")
	tokC2, _ := shareSt.Regen(id1, inC.ID)
	if w := (gReq{method: "POST", path: "/invite/main/" + tokC}).do(h); w.Code != 403 {
		t.Fatalf("ancien jeton : %d", w.Code)
	}
	if w := (gReq{method: "POST", path: "/invite/main/" + tokC2}).do(h); w.Code != 303 {
		t.Fatalf("jeton régénéré : %d", w.Code)
	}
	if _, err := shareSt.Regen(id1, "0000000000000000"); !errors.Is(err, errInviteNone) {
		t.Fatalf("invitation inconnue : %v", err)
	}
}

func TestRevokeDropsItsQueueAndKeepsOthers(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, h, id1, "-p", "", "")
	cb, csrfB, invB := joinAs(t, h, id1, "-p", "", "")
	host, _ := queueSt.Add(id1, "host", "", "h")
	itemOf(t, addAs(h, ca, csrfA, "a", nil))
	b := itemOf(t, addAs(h, cb, csrfB, "b", nil))
	if err := shareSt.Revoke(id1, invA); err != nil {
		t.Fatal(err)
	}
	if l := queueSt.List(id1); len(l) != 2 || l[0].ID != host.ID || l[1].ID != b.ID {
		t.Fatalf("file : %+v", l)
	}
	if w := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: ca}).do(h); w.Code != 401 {
		t.Fatalf("A révoqué : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cb}).do(h); w.Code != 200 {
		t.Fatalf("B : %d", w.Code)
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
	if l := queueSt.List(id1); len(l) != 1 || l[0].ID != host.ID {
		t.Fatalf("file : %+v", l)
	}
}

func TestRevokeInMemoryOnWriteFailure(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	tok, in, _ := s.CreateInvite(shSess, "dir", "", "")
	c, _, _, _ := s.Redeem(tok)
	_, other, _ := s.CreateInvite(shSess, "dir", "", "")
	lock(t, d)
	if err := s.Revoke(shSess, in.ID); err == nil {
		t.Fatal("erreur attendue")
	}
	if _, _, ok := s.ByCookie(c); ok {
		t.Fatal("cookie révoqué encore valide en mémoire")
	}
	if _, err := s.Regen(shSess, other.ID); err == nil {
		t.Fatal("régénération sans écriture acceptée")
	}
	if _, _, err := s.CreateInvite(shSess, "dir", "", ""); err == nil {
		t.Fatal("création sans écriture acceptée")
	}
	if sh, _ := s.Get(shSess); len(sh.Invites) != 1 || sh.Invites[0].TokenHash != other.TokenHash || sh.Seq != 2 {
		t.Fatalf("état après échecs : %+v", sh)
	}
}

func TestInviteRoQueuedItemsRemoved(t *testing.T) {
	mux := shareMux(t)
	oq := queueSt
	queueSt, _ = openQueue(t.TempDir())
	t.Cleanup(func() { queueSt = oq })
	_, a, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	_, b, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	s, _ := queueSt.Add(shSess, "guest", a.ID, "en cours")
	queueSt.MarkSending(shSess, s.ID)
	queueSt.Add(shSess, "guest", a.ID, "a")
	kb, _ := queueSt.Add(shSess, "guest", b.ID, "b")
	kh, _ := queueSt.Add(shSess, "host", "", "h")
	if w := send(mux, "PATCH", "/v1/invites/"+a.ID, `{"id":"`+shSess+`","mode":"x"}`); w.Code != 400 {
		t.Fatalf("mode invalide : %d", w.Code)
	}
	if w := send(mux, "PATCH", "/v1/invites/"+a.ID, `{"id":"`+shSess+`","mode":"ro"}`); w.Code != 204 {
		t.Fatalf("mode : %d %s", w.Code, w.Body)
	}
	l := queueSt.List(shSess)
	if len(l) != 3 || l[0].ID != s.ID || l[1].ID != kb.ID || l[2].ID != kh.ID {
		t.Fatalf("file : %+v", l)
	}
	sh, _ := shareSt.Get(shSess)
	if ia, _ := sh.invite(a.ID); ia.Mode != "ro" {
		t.Fatal("mode non appliqué")
	}
	if ib, _ := sh.invite(b.ID); ib.Mode != "rw" {
		t.Fatal("mode de B touché")
	}
	if w := send(mux, "PATCH", "/v1/invites/0000000000000000", `{"id":"`+shSess+`","mode":"ro"}`); w.Code != 404 {
		t.Fatalf("invitation inconnue : %d", w.Code)
	}
}

// drainInvites : partage de shSess avec Léa (rw) et une invitation sans nom (rw), sessions ouvertes et au repos.
func drainInvites(t *testing.T) (*queueStore, *[]string, *shareStore, Invite, Invite) {
	t.Helper()
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	s, _ := newShares(t)
	_, a, _ := s.CreateInvite(shSess, "dir", "", "Léa")
	_, b, _ := s.CreateInvite(shSess, "dir", "", "")
	shareSt = s
	return q, sent, s, a, b
}

func TestDrainLabelPerInvite(t *testing.T) {
	q, sent, s, a, b := drainInvites(t)
	q.Add(shSess, "guest", a.ID, "/exit")
	q.Add(shSess, "guest", b.ID, "/clear")
	q.Add(shSess, "host", "", "/compact")
	now := time.Now()
	for i := range 3 {
		drainOnce(q, now.Add(time.Duration(i)*5*time.Second))
	}
	want := []string{shSess + "|" + followPrefix + "[Invité Léa] /exit", shSess + "|" + followPrefix + "[Invité 2] /clear", shSess + "|/compact"}
	if strings.Join(*sent, "\n") != strings.Join(want, "\n") {
		t.Fatalf("envoyés : %q", *sent)
	}
	if _, err := q.Add(shSess, "guest", "", "x"); err != errQueueText {
		t.Fatalf("élément invité sans invitation accepté : %v", err)
	}
	// Nom le plus long (24 caractères de 4 octets) et texte à la borne exacte : étiquette et préfixe tiennent dans sendMax.
	big := strings.Repeat("\U00020000", inviteNameMax)
	_, c, _ := s.CreateInvite(shSess, "dir", "", big)
	if c.Name != big || len(guestTag(c)) != guestLabelMax {
		t.Fatalf("nom long : %q %d/%d", c.Name, len(guestTag(c)), guestLabelMax)
	}
	max := sendMax - guestLabelMax - len(followPrefix)
	if _, err := q.Add(shSess, "guest", c.ID, strings.Repeat("x", max+1)); err != errQueueText {
		t.Fatalf("un octet de trop : %v", err)
	}
	text := strings.Repeat("x", max)
	q.Add(shSess, "guest", c.ID, text)
	q.Add(shSess, "host", "", "suite")
	*sent = nil
	drainOnce(q, now.Add(time.Minute))
	got := strings.TrimPrefix((*sent)[0], shSess+"|")
	if got != followPrefix+guestTag(c)+text || len(got) != sendMax || !utf8.ValidString(got) {
		t.Fatalf("borne : %d octets", len(got))
	}
}

// Une étiquette lue d'un fichier altéré est réassainie : jamais de crochet ni de saut de ligne, jamais plus que guestLabelMax.
func TestDrainTagResanitized(t *testing.T) {
	for _, n := range []string{"a]\n/exit", strings.Repeat("\U00020000", 200), "\u202E"} {
		tag := guestTag(Invite{Seq: 3, Name: n})
		if len(tag) > guestLabelMax || strings.ContainsAny(tag[1:len(tag)-2], "[]\n") || !strings.HasPrefix(tag, "[Invité ") {
			t.Fatalf("%q : %q", n, tag)
		}
	}
	if guestTag(Invite{Seq: 3, Name: "\u202E"}) != "[Invité 3] " {
		t.Fatal("nom vide après assainissement")
	}
}

func TestDrainDropsRevokedOrRoInvite(t *testing.T) {
	for _, c := range []string{"révoquée", "lecture seule", "partage absent", "bascule"} {
		q, sent, s, a, b := drainInvites(t)
		q.Add(shSess, "guest", a.ID, "x")
		switch c {
		case "révoquée":
			s.Revoke(shSess, a.ID)
			q.Add(shSess, "guest", a.ID, "y") // ajouté après la révocation (reprise d'une écriture ratée)
		case "lecture seule":
			s.SetInviteMode(shSess, a.ID, "ro")
		case "partage absent":
			s.End(shSess)
			q.Add(shSess, "guest", a.ID, "y")
		case "bascule":
			screenFn = func(string) (string, error) { s.SetInviteMode(shSess, a.ID, "ro"); return "", nil }
		}
		q.Add(shSess, "guest", b.ID, "de B")
		for i := range 4 {
			drainOnce(q, time.Now().Add(time.Duration(i)*20*time.Second))
		}
		for _, m := range *sent {
			if !strings.Contains(m, "de B") {
				t.Fatalf("%s : envoyé %q", c, m)
			}
		}
		for _, it := range q.List(shSess) {
			if it.Guest == a.ID {
				t.Fatalf("%s : élément de A resté %+v", c, it)
			}
		}
		if c != "partage absent" && len(*sent) != 1 {
			t.Fatalf("%s : B non envoyé %q", c, *sent)
		}
	}
}

func TestGuestRateLimitPerInvite(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA, _ := joinAs(t, h, id1, "-p", "", "")
	cb, csrfB, _ := joinAs(t, h, id1, "-p", "", "")
	for i := range guestAddMax + 1 {
		want := map[bool]int{true: 429, false: 201}[i == guestAddMax]
		if w := addAs(h, ca, csrfA, "m", nil); w.Code != want {
			t.Fatalf("A ajout %d : %d", i+1, w.Code)
		}
	}
	if w := addAs(h, cb, csrfB, "m", nil); w.Code != 201 {
		t.Fatalf("B limité par A : %d", w.Code)
	}
}

func TestSharesOverview(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	oo := shareOpen
	t.Cleanup(func() { shareOpen = oo })
	shareOpen = func(id string) bool { return id == id1 }
	ca, csrfA, invA := joinAs(t, h, id1, "-p", "Titre", "Léa")
	_, _, invB := joinAs(t, h, id1, "-p", "", "")
	_, inC, _ := shareSt.CreateInvite(id1, "-p", "", "")
	joinAs(t, h, gOther, "-q", "", "")
	shareSt.mu.Lock()
	sh := shareSt.m[id1]
	sh.Invites[sh.inv(invB)].LastSeen = time.Now().Add(-time.Minute).Unix()
	shareSt.mu.Unlock()
	long := strings.Repeat("é", 250)
	itemOf(t, addAs(h, ca, csrfA, long, nil))
	queueSt.Add(id1, "host", "", "de l'hôte")
	w := gReq{method: "GET", path: "/v1/shares", tokCookie: modeTok}.do(h)
	var r struct {
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
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &r) != nil || len(r.Shares) != 2 {
		t.Fatalf("vue : %d %s", w.Code, w.Body)
	}
	s := r.Shares[0]
	if s.ID != id1 {
		s = r.Shares[1]
	}
	if s.Dir != "-p" || s.Title != "Titre" || s.Handle != hID1 || !s.Open || len(s.Invites) != 3 {
		t.Fatalf("partage : %+v", s)
	}
	a, b, c := s.Invites[0], s.Invites[1], s.Invites[2]
	if a.ID != invA || a.Label != "Léa" || a.Name != "Léa" || a.State != "connected" || !a.Redeemed || a.Mode != "rw" || a.Seq != 1 {
		t.Fatalf("A : %+v", a)
	}
	if len(a.Queue) != 1 || a.Queue[0].Text != strings.Repeat("é", 200) || a.Queue[0].State != "queued" || a.Queue[0].At == 0 {
		t.Fatalf("file de A : %+v", a.Queue)
	}
	if b.ID != invB || b.State != "away" || b.Label != "Invité 2" || len(b.Queue) != 0 {
		t.Fatalf("B : %+v", b)
	}
	if c.ID != inC.ID || c.State != "pending" || c.Redeemed {
		t.Fatalf("C : %+v", c)
	}
	if strings.Contains(w.Body.String(), "de l'hôte") || strings.Contains(w.Body.String(), "Hash") {
		t.Fatalf("fuite : %s", w.Body)
	}
	for _, other := range r.Shares {
		if other.ID == gOther && other.Open {
			t.Fatal("open de l'autre session")
		}
	}
	if w := (gReq{method: "GET", path: "/v1/shares", cookie: ca}).do(h); w.Code != 403 || strings.Contains(w.Body.String(), long[:20]) {
		t.Fatalf("invité : %d", w.Code)
	}
	if w := (gReq{method: "GET", path: "/v1/shares"}).do(h); w.Code != 401 {
		t.Fatalf("anonyme : %d", w.Code)
	}
	shareSt = nil
	if w := (gReq{method: "GET", path: "/v1/shares", tokCookie: modeTok}).do(h); w.Code != 503 {
		t.Fatalf("magasin absent : %d", w.Code)
	}
}

func TestHostRoutesNotInGuestWhitelist(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	c, csrf, inv := joinAs(t, h, id1, "-p", "", "")
	body := `{"id":"` + id1 + `","dir":"-p","invite":"` + inv + `","mode":"rw"}`
	for _, rt := range [][3]string{{"POST", "/v1/invites", "/v1/invites"}, {"PUT", "/v1/invites/" + inv + "/token", "/v1/invites/{invite}/token"}, {"PATCH", "/v1/invites/" + inv, "/v1/invites/{invite}"},
		{"DELETE", "/v1/invites/" + inv, "/v1/invites/{invite}"}, {"DELETE", "/v1/shares/" + id1, "/v1/shares/{share}"}, {"GET", "/v1/shares", "/v1/shares"}} {
		if guestRoutes[rt[0]+" "+rt[2]] {
			t.Errorf("%s %s dans la liste blanche", rt[0], rt[2])
		}
		if w := (gReq{method: rt[0], path: rt[1], body: body, cookie: c, csrf: csrf}).do(h); w.Code != 403 {
			t.Errorf("%s %s invité : %d", rt[0], rt[1], w.Code)
		}
	}
	if sh, _ := shareSt.Get(id1); len(sh.Invites) != 1 || sh.Invites[0].CookieHash == "" {
		t.Fatalf("partage modifié par l'invité : %+v", sh)
	}
	for _, p := range []string{"POST /share-create", "GET /share-info", "POST /share-mode"} {
		if _, ok := routeRights[p]; ok {
			t.Errorf("%s encore déclarée", p)
		}
	}
}

func TestOldSharesFileIgnored(t *testing.T) {
	d := t.TempDir()
	c := "secret-ancien"
	old := `{"` + shSess + `":{"session":"` + shSess + `","dir":"dir","mode":"rw","tokenHash":"` + hashSecret("tok") + `","cookieHash":"` + hashSecret(c) + `"},` +
		`"` + gOther + `":{"session":"` + gOther + `","dir":"-p","seq":1,"invites":[{"id":"0123456789abcdef","seq":1,"mode":"rw","tokenHash":"x"}]},"z":null}`
	os.WriteFile(filepath.Join(d, "shares.json"), []byte(old), 0o600)
	s, err := openShares(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(shSess); ok {
		t.Fatal("ancien partage conservé")
	}
	if _, _, ok := s.ByCookie(c); ok {
		t.Fatal("ancien cookie accepté")
	}
	if _, _, _, ok := s.Redeem("tok"); ok {
		t.Fatal("ancien jeton accepté")
	}
	if sh, ok := s.Get(gOther); !ok || len(sh.Invites) != 1 {
		t.Fatal("partage au nouveau format perdu")
	}
	if _, _, err := s.CreateInvite(shSess, "dir", "", ""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "shares.json")); strings.Contains(string(b), hashSecret(c)) {
		t.Fatal("ancienne entrée réécrite")
	}
}

func TestInviteHeaderSpoofIgnored(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, h, id1, "-p", "", "")
	_, _, invB := joinAs(t, h, id1, "-p", "", "")
	a := itemOf(t, addAs(h, ca, csrfA, "a", map[string]string{authorHeader: "host", inviteHeader: invB}))
	if a.Author != "guest" || a.Guest != invA {
		t.Fatalf("invité forgé : %+v", a)
	}
	w := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"id":"` + id1 + `","text":"h"}`, tokCookie: modeTok, hdr: map[string]string{authorHeader: "guest", inviteHeader: invB}}.do(h)
	if it := itemOf(t, w); it.Author != "host" || it.Guest != "" {
		t.Fatalf("hôte forgé : %+v", it)
	}
}

// Passerelle vers un home enfant : les en-têtes internes du client sont retirés ; l'enfant déduit l'invitation du secret.
func TestInviteHeaderStrippedAtRelayAndChildDerivesInvite(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	addHomes(t, "perso")
	hm, _ := homeByName("perso")
	cs, _ := openShares(hm.Dir)
	tok, _, _ := cs.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := cs.Redeem(tok)
	var author, invite []string
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		author, invite = append(author, r.Header.Get(authorHeader)), append(invite, r.Header.Get(inviteHeader))
		w.Write([]byte(`{"item":{}}`))
	}))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	oldStart := homeStart
	t.Cleanup(func() { homeStart = oldStart; stopHomes() })
	homeStart = func(h Home) (*homeProc, error) {
		return &homeProc{dir: h.Dir, proxy: childProxy(cu, "x"), done: make(chan struct{})}, nil
	}
	spoof := map[string]string{authorHeader: "guest", inviteHeader: "0123456789abcdef"}
	addAs(h, "perso."+sec, guestCSRF(sec), "x", spoof)
	r := httptest.NewRequest("POST", "/v1/sessions/"+id1+"/queue", strings.NewReader(`{"text":"h"}`))
	r.Host = testHost
	r.AddCookie(&http.Cookie{Name: tokenCookie, Value: modeTok})
	r.AddCookie(&http.Cookie{Name: homeCookie, Value: "perso"})
	for k, v := range spoof {
		r.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
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
	w := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"text":"m"}`, hdr: hdr}.do(hc)
	if it := itemOf(t, w); it.Author != "guest" || it.Guest != in2.ID {
		t.Fatalf("enfant : %+v", it)
	}
}

func TestInviteRoutes(t *testing.T) {
	mux := shareMux(t)
	for _, c := range [][2]string{{"/v1/invites/0123456789abcdef/token", `{"id":"nope","dir":"proj"}`}, {"/v1/invites/x/token", `{"id":"` + shSess + `","dir":"proj"}`}} {
		if w := send(mux, "PUT", c[0], c[1]); w.Code != 400 {
			t.Fatalf("%s : %d", c[1], w.Code)
		}
	}
	if w := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"absent"}`); w.Code != 404 {
		t.Fatalf("session absente : %d", w.Code)
	}
	if w := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj","name":"`+strings.Repeat("x", 5<<10)+`"}`); w.Code != 400 {
		t.Fatalf("corps trop gros : %d", w.Code)
	}
	w := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj","title":" Ma\tsession ","name":"[Léa]"}`)
	var r struct {
		Token  string
		Invite struct{ ID, Label string }
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Invite.Label != "Léa" {
		t.Fatalf("création : %d %s", w.Code, w.Body)
	}
	if sh, _ := shareSt.Get(shSess); sh.Title != "Ma session" || sh.Dir != "proj" {
		t.Fatalf("partage : %+v", sh)
	}
	post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj"}`)
	if sh, _ := shareSt.Get(shSess); sh.Title != "Ma session" {
		t.Fatalf("titre effacé sans titre fourni : %q", sh.Title)
	}
	w = send(mux, "PUT", "/v1/invites/"+r.Invite.ID+"/token", `{"id":"`+shSess+`"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token":"`) || strings.Contains(w.Body.String(), r.Token) {
		t.Fatalf("régénération : %d %s", w.Code, w.Body)
	}
	for _, c := range [][3]string{{"PUT", "/v1/invites/" + r.Invite.ID + "/token", `{"id":"` + gOther + `"}`}, {"DELETE", "/v1/invites/" + r.Invite.ID + "?id=" + gOther, ""}} {
		if w := send(mux, c[0], c[1], c[2]); w.Code != 404 {
			t.Fatalf("%s autre session : %d", c[1], w.Code)
		}
	}
	if w := send(mux, "DELETE", "/v1/invites/"+r.Invite.ID+"?id="+shSess, ""); w.Code != 204 {
		t.Fatalf("révocation : %d", w.Code)
	}
	if w := send(mux, "DELETE", "/v1/shares/"+shSess, ""); w.Code != 204 {
		t.Fatalf("fin : %d", w.Code)
	}
	if _, ok := shareSt.Get(shSess); ok {
		t.Fatal("partage encore là")
	}
	shareSt = nil
	for _, c := range [][2]string{{"POST", "/v1/invites"}, {"PUT", "/v1/invites/0123456789abcdef/token"}, {"PATCH", "/v1/invites/0123456789abcdef"}, {"DELETE", "/v1/invites/0123456789abcdef?id=" + shSess}, {"DELETE", "/v1/shares/" + shSess}} {
		if w := send(mux, c[0], c[1], `{"id":"`+shSess+`","dir":"proj","mode":"ro"}`); w.Code != 503 {
			t.Fatalf("%s %s : %d", c[0], c[1], w.Code)
		}
	}
}

// Fuite de lien ou invités multiples : les invités ne remplissent jamais toute la file, l'hôte peut toujours ajouter.
func TestQueueGuestCapPerSession(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	ca, csrfA, invA := joinAs(t, h, id1, "-p", "", "")
	cb, csrfB, _ := joinAs(t, h, id1, "-p", "", "")
	for i := range queueGuestMax - 1 {
		if _, err := queueSt.Add(id1, "guest", invA, "g"); err != nil {
			t.Fatalf("élément invité %d : %v", i+1, err)
		}
	}
	if w := addAs(h, cb, csrfB, "30e", nil); w.Code != 201 {
		t.Fatalf("30e : %d", w.Code)
	}
	for _, q := range []struct{ c, csrf string }{{ca, csrfA}, {cb, csrfB}} {
		if w := addAs(h, q.c, q.csrf, "31e", nil); w.Code != 409 || !strings.Contains(w.Body.String(), L("srv.queue.pleine")) {
			t.Fatalf("31e invité : %d %s", w.Code, w.Body)
		}
	}
	w := gReq{method: "POST", path: "/v1/sessions/" + id1 + "/queue", body: `{"id":"` + id1 + `","text":"hôte"}`, tokCookie: modeTok}.do(h)
	if it := itemOf(t, w); it.Author != "host" {
		t.Fatalf("hôte : %+v", it)
	}
}

// Requête hôte relayée à un enfant : le proxy retire lui-même les en-têtes internes, sans compter sur la garde de la passerelle.
func TestChildProxyStripsInviteHeaders(t *testing.T) {
	var got http.Header
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer child.Close()
	cu, _ := url.Parse(child.URL)
	r := httptest.NewRequest("POST", "/v1/sessions/"+id1+"/queue", strings.NewReader(`{}`))
	r.Header.Set(authorHeader, "guest")
	r.Header.Set(inviteHeader, "0123456789abcdef")
	childProxy(cu, "x").ServeHTTP(httptest.NewRecorder(), r)
	if got == nil || len(got.Values(authorHeader)) != 0 || len(got.Values(inviteHeader)) != 0 {
		t.Fatalf("vu par l'enfant : %v", got)
	}
}

// Côté enfant (invité relayé par la passerelle) : même filtrage, la garde de l'enfant pose les mêmes en-têtes.
func TestChildGuestSeesOnlyOwnQueue(t *testing.T) {
	h := guestWeb(t, nil, "")
	oc := childToken
	t.Cleanup(func() { childToken = oc })
	childToken = "ctok"
	_, a, _ := shareSt.CreateInvite(id1, "-p", "", "Léa")
	tok, b, _ := shareSt.CreateInvite(id1, "-p", "", "")
	sec, _, _, _ := shareSt.Redeem(tok)
	queueSt.Add(id1, "guest", a.ID, "de A")
	queueSt.Add(id1, "guest", b.ID, "de B")
	queueSt.Add(id1, "host", "", "de l'hôte")
	seen := queueSeen(t, h, gReq{hdr: map[string]string{childHeader: "ctok", guestHeader: sec, shareHeader: hID1}})
	for _, p := range []string{"/queue", "/live"} {
		_, b := seen[p]["de B"]
		_, a := seen[p]["de A"]
		if got := seen[p]; len(got) != 2 || !b || a || p == "/queue" && got["de B"] != "Invité 2" {
			t.Fatalf("%s enfant : %v", p, got)
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
	w := send(mux, "PUT", "/v1/invites/"+in.ID+"/token", `{"id":"`+shSess+`"}`)
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("régénération : %d %s", w.Code, w.Body)
	}
	if sh, _ := shareSt.Get(shSess); sh.Invites[0].TokenHash == old {
		t.Fatal("jeton non changé")
	}
	if l := queueSt.List(shSess); len(l) != 0 {
		t.Fatalf("purge annulée en mémoire : %+v", l)
	}
}

func TestRedeemedLinkReopensOnSameBrowser(t *testing.T) {
	h := guestWeb(t, nil, modeTok)
	tok, _, err := shareSt.CreateInvite(id1, "-p", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hd := shareHandle(id1)
	w := gReq{method: "POST", path: "/invite/main/" + tok}.do(h)
	c := cookieOf(w, guestCookieName(hd))
	if w.Code != 303 || c == nil {
		t.Fatalf("invite : %d", w.Code)
	}
	w = gReq{method: "POST", path: "/invite/main/" + tok, jar: []*http.Cookie{c}}.do(h)
	if w.Code != 303 || w.Header().Get("Location") != "/guest/"+hd {
		t.Fatalf("même navigateur : %d %q", w.Code, w.Header().Get("Location"))
	}
	if w = (gReq{method: "POST", path: "/invite/main/" + tok}).do(h); w.Code != 403 {
		t.Fatalf("autre navigateur : %d", w.Code)
	}
}
