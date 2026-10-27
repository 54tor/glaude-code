package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const shSess = "11111111-1111-1111-1111-111111111111"

func newShares(t *testing.T) (*shareStore, string) {
	t.Helper()
	d := t.TempDir()
	s, err := openShares(d)
	if err != nil {
		t.Fatal(err)
	}
	return s, d
}

func TestShareRedeemOnce(t *testing.T) {
	s, _ := newShares(t)
	tok := mkShare(t, s)
	c, sh, inv, ok := s.Redeem(tok)
	if !ok || c == "" || sh.Session != shSess || inv.CookieHash != hashSecret(c) {
		t.Fatalf("1er Redeem : %v %q %+v", ok, c, sh)
	}
	if _, _, _, ok := s.Redeem(tok); ok {
		t.Fatal("2e Redeem accepté")
	}
	if _, _, _, ok := s.Redeem("n-importe-quoi"); ok {
		t.Fatal("jeton inconnu accepté")
	}
}

func TestShareCookieSurvivesReload(t *testing.T) {
	s, d := newShares(t)
	tok := mkShare(t, s)
	c, _, _, _ := s.Redeem(tok)
	if _, _, ok := s.ByCookie(c); !ok {
		t.Fatal("cookie refusé")
	}
	s2, err := openShares(d)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if sh, _, ok := s2.ByCookie(c); !ok || sh.Session != shSess {
			t.Fatalf("après rechargement : %v %+v", ok, sh)
		}
	}
	if _, _, ok := s2.ByCookie("autre"); ok {
		t.Fatal("mauvais cookie accepté")
	}
	if _, _, _, ok := s2.Redeem(tok); ok {
		t.Fatal("jeton rejouable après rechargement")
	}
}

func TestShareEndDropsGuestQueueItems(t *testing.T) {
	s, _ := newShares(t)
	oldSt := queueSt
	q, _ := openQueue(t.TempDir())
	queueSt = q
	t.Cleanup(func() { queueSt = oldSt })
	_, a := mkInv(t, s)
	_, b := mkInv(t, s)
	q.Add(shSess, "guest", a.ID, "g")
	q.Add(shSess, "guest", b.ID, "g2")
	q.Add(shSess, "host", "", "h")
	if err := s.End(shSess); err != nil {
		t.Fatal(err)
	}
	l := q.List(shSess)
	if len(l) != 1 || l[0].Author != "host" {
		t.Fatalf("file : %+v", l)
	}
	if _, ok := s.Get(shSess); ok {
		t.Fatal("partage toujours là")
	}
	if !errors.Is(s.End(shSess), errShareNone) {
		t.Fatal("2e End doit renvoyer errShareNone")
	}
}

func TestShareFileNotLeakingToken(t *testing.T) {
	s, d := newShares(t)
	tok := mkShare(t, s)
	c, _, _, _ := s.Redeem(tok)
	b, err := os.ReadFile(filepath.Join(d, "shares.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), tok) || strings.Contains(string(b), c) {
		t.Fatalf("secret en clair : %s", b)
	}
}

func TestShareSetModeAndGet(t *testing.T) {
	s, _ := newShares(t)
	if !errors.Is(s.SetInviteMode(shSess, "0123456789abcdef", "ro"), errInviteNone) {
		t.Fatal("SetInviteMode sans partage")
	}
	_, in := mkInv(t, s)
	if err := s.SetInviteMode(shSess, in.ID, "ro"); err != nil {
		t.Fatal(err)
	}
	if sh, _ := s.Get(shSess); sh.Invites[0].Mode != "ro" {
		t.Fatalf("mode %q", sh.Invites[0].Mode)
	}
}

func shareMux(t *testing.T) *http.ServeMux {
	t.Helper()
	oldSt, oldDir, oldRoot := shareSt, shareDir, root
	d := t.TempDir()
	shareDir = func() string { return d }
	root = filepath.Join(t.TempDir(), "projects")
	os.MkdirAll(filepath.Join(root, "proj"), 0o755)
	os.WriteFile(filepath.Join(root, "proj", shSess+".jsonl"), []byte("{}\n"), 0o644)
	t.Cleanup(func() { shareSt, shareDir, root = oldSt, oldDir, oldRoot })
	mux := http.NewServeMux()
	routesShare(mux)
	return mux
}

func mkShare(t *testing.T, s *shareStore) string {
	t.Helper()
	tok, _ := mkInv(t, s)
	return tok
}

func mkInv(t *testing.T, s *shareStore) (string, Invite) {
	t.Helper()
	tok, in, err := s.CreateInvite(shSess, "dir", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return tok, in
}

func roDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignore les permissions")
	}
	return t.TempDir()
}

func lock(t *testing.T, d string) {
	os.Chmod(d, 0o500)
	t.Cleanup(func() { os.Chmod(d, 0o700) })
}

func TestShareCreateRollsBackOnWriteFailure(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	old := mkShare(t, s)
	lock(t, d)
	if tok, _, err := s.CreateInvite(shSess, "dir2", "titre", ""); err == nil || tok != "" {
		t.Fatalf("erreur attendue : %q %v", tok, err)
	}
	if sh, ok := s.Get(shSess); !ok || sh.Dir != "dir" || sh.Title != "" || sh.Seq != 1 || len(sh.Invites) != 1 {
		t.Fatalf("ancien partage perdu : %+v", sh)
	}
	if sh, _ := s.Get(shSess); sh.Invites[0].TokenHash != hashSecret(old) {
		t.Fatal("ancien jeton invalidé")
	}
}

func TestShareCreateFailureLeavesNoShare(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	lock(t, d)
	if _, _, err := s.CreateInvite(shSess, "dir", "", ""); err == nil {
		t.Fatal("erreur attendue")
	}
	if _, ok := s.Get(shSess); ok {
		t.Fatal("partage non persisté laissé en mémoire")
	}
}

func TestShareSetModeRollsBackOnWriteFailure(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	_, in := mkInv(t, s)
	lock(t, d)
	if err := s.SetInviteMode(shSess, in.ID, "ro"); err == nil || errors.Is(err, errInviteNone) {
		t.Fatalf("erreur attendue : %v", err)
	}
	if sh, _ := s.Get(shSess); sh.Invites[0].Mode != "rw" {
		t.Fatalf("mode %q", sh.Invites[0].Mode)
	}
}

func TestShareEndRevokesInMemoryOnWriteFailure(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	tok := mkShare(t, s)
	c, _, _, _ := s.Redeem(tok)
	lock(t, d)
	if err := s.End(shSess); err == nil || errors.Is(err, errShareNone) {
		t.Fatalf("erreur attendue : %v", err)
	}
	if _, _, ok := s.ByCookie(c); ok {
		t.Fatal("cookie encore valide en mémoire")
	}
}

func TestShareOpenNullEntries(t *testing.T) {
	for _, c := range []string{"null", `{"x":null}`} {
		d := t.TempDir()
		os.WriteFile(filepath.Join(d, "shares.json"), []byte(c), 0o600)
		s, err := openShares(d)
		if err != nil {
			t.Fatal(err)
		}
		s.ByCookie("x")
		s.SetInviteMode("x", "0123456789abcdef", "ro")
		if _, _, err := s.CreateInvite(shSess, "dir", "", ""); err != nil {
			t.Fatal(c, err)
		}
		if _, ok := s.Get("x"); ok {
			t.Fatal("entrée nulle conservée")
		}
	}
}

func TestShareCorruptedFile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "shares.json")
	os.WriteFile(p, []byte("{pas du json"), 0o600)
	if _, err := openShares(d); err == nil {
		t.Fatal("erreur attendue")
	}
	oldSt, oldDir := shareSt, shareDir
	shareDir = func() string { return d }
	t.Cleanup(func() { shareSt, shareDir = oldSt, oldDir })
	mux := http.NewServeMux()
	routesShare(mux)
	if w := post(mux, "/share-end", `{"id":"`+shSess+`"}`); w.Code != 503 {
		t.Fatalf("code %d", w.Code)
	}
	if b, _ := os.ReadFile(p); string(b) != "{pas du json" {
		t.Fatalf("fichier modifié : %q", b)
	}
}

func TestShareEndRouteDropsGuestItems(t *testing.T) {
	mux := shareMux(t)
	oldSt := queueSt
	q, _ := openQueue(t.TempDir())
	queueSt = q
	t.Cleanup(func() { queueSt = oldSt })
	_, in, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	q.Add(shSess, "guest", in.ID, "g")
	q.Add(shSess, "host", "", "h")
	if w := post(mux, "/share-end", `{"id":"`+shSess+`"}`); w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	if l := q.List(shSess); len(l) != 1 || l[0].Author != "host" {
		t.Fatalf("file : %+v", l)
	}
}

func TestShareCreateRouteFailsOnWrite(t *testing.T) {
	mux := shareMux(t)
	d := roDir(t)
	shareDir = func() string { return d }
	routesShare(http.NewServeMux())
	lock(t, d)
	if w := post(mux, "/invite-create", `{"id":"`+shSess+`","dir":"proj"}`); w.Code != 500 || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("code %d %s", w.Code, w.Body)
	}
}

func TestShareReadOnlyDropsQueuedGuestItems(t *testing.T) {
	mux := shareMux(t)
	oldSt := queueSt
	qd := t.TempDir()
	q, _ := openQueue(qd)
	queueSt = q
	t.Cleanup(func() { queueSt = oldSt })
	_, in, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	mode := `{"id":"` + shSess + `","invite":"` + in.ID + `","mode":"ro"}`
	s, _ := q.Add(shSess, "guest", in.ID, "en cours")
	q.MarkSending(shSess, s.ID)
	q.Add(shSess, "guest", in.ID, "g")
	q.Add(shSess, "host", "", "h")
	if w := post(mux, "/invite-mode", mode); w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	l := q.List(shSess)
	if len(l) != 2 || l[0].ID != s.ID || l[1].Author != "host" {
		t.Fatalf("file : %+v", l)
	}
	q.Add(shSess, "host", "", "h2")
	q.Add(shSess, "guest", in.ID, "g2") // la garde refuse l'ajout invité en lecture seule ; ici on teste l'échec d'écriture
	if os.Geteuid() == 0 {
		return
	}
	os.Chmod(qd, 0o500)
	t.Cleanup(func() { os.Chmod(qd, 0o700) })
	if w := post(mux, "/invite-mode", mode); w.Code != 500 {
		t.Fatalf("échec d'écriture de la file : %d", w.Code)
	}
	if sh, _ := shareSt.Get(shSess); sh.Invites[0].Mode != "ro" {
		t.Fatalf("mode = %q", sh.Invites[0].Mode)
	}
	for _, it := range q.List(shSess) {
		if it.Author == "guest" && it.State == "queued" {
			t.Fatalf("item invité encore en file : %+v", it)
		}
	}
}

func TestShareTitleSanitized(t *testing.T) {
	long := strings.Repeat("é", 130)
	for in, want := range map[string]string{
		"": "", "Titre": "Titre", long: long[:240],
		"  a\x00b\nc\u0085\u001b[31m  ": "ab c [31m", // contrôles retirés, NEL comme espace
		"a\tb\r\nc  \n d":               "a b c d",   // espaces de contrôle : une seule espace
		"x\u202Ey\u202Az\u202D":         "xyz",       // surcharges bidi
		"x\u2066y\u2067z\u2068w\u2069":  "xyzw",      // isolats bidi
		"x\u200By\u200Cz\u200Dw\u2060v": "xyzwv",     // largeur nulle
		"\uFEFFtitre\u00AD":             "titre",     // BOM, trait d'union conditionnel
		"\u202E   \t ":                  "",
	} {
		if got := shareTitle(in); got != want {
			t.Errorf("%q : %q, attendu %q", in, got, want)
		}
	}
}
