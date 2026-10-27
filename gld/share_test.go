package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
	c, sh, ok := s.Redeem(tok)
	if !ok || c == "" || sh.Session != shSess {
		t.Fatalf("1er Redeem : %v %q %+v", ok, c, sh)
	}
	if _, _, ok := s.Redeem(tok); ok {
		t.Fatal("2e Redeem accepté")
	}
	if _, _, ok := s.Redeem("n-importe-quoi"); ok {
		t.Fatal("jeton inconnu accepté")
	}
}

func TestShareCookieSurvivesReload(t *testing.T) {
	s, d := newShares(t)
	tok := mkShare(t, s)
	c, _, _ := s.Redeem(tok)
	if _, ok := s.ByCookie(c); !ok {
		t.Fatal("cookie refusé")
	}
	s2, err := openShares(d)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if sh, ok := s2.ByCookie(c); !ok || sh.Session != shSess {
			t.Fatalf("après rechargement : %v %+v", ok, sh)
		}
	}
	if _, ok := s2.ByCookie("autre"); ok {
		t.Fatal("mauvais cookie accepté")
	}
	if _, _, ok := s2.Redeem(tok); ok {
		t.Fatal("jeton rejouable après rechargement")
	}
}

func TestShareCreateRevokesPrevious(t *testing.T) {
	s, _ := newShares(t)
	old := mkShare(t, s)
	c, _, _ := s.Redeem(old)
	nw := mkShare(t, s)
	if _, ok := s.ByCookie(c); ok {
		t.Fatal("ancien cookie encore valide")
	}
	if _, _, ok := s.Redeem(old); ok {
		t.Fatal("ancien jeton encore valide")
	}
	if _, _, ok := s.Redeem(nw); !ok {
		t.Fatal("nouveau jeton refusé")
	}
}

func TestShareEndDropsGuestQueueItems(t *testing.T) {
	s, _ := newShares(t)
	oldSt := queueSt
	q, _ := openQueue(t.TempDir())
	queueSt = q
	t.Cleanup(func() { queueSt = oldSt })
	mkShare(t, s)
	q.Add(shSess, "guest", "g")
	q.Add(shSess, "host", "h")
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
	c, _, _ := s.Redeem(tok)
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
	if !errors.Is(s.SetMode(shSess, "ro"), errShareNone) {
		t.Fatal("SetMode sans partage")
	}
	mkShare(t, s)
	if err := s.SetMode(shSess, "ro"); err != nil {
		t.Fatal(err)
	}
	if sh, _ := s.Get(shSess); sh.Mode != "ro" {
		t.Fatalf("mode %q", sh.Mode)
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

func TestShareRoutes(t *testing.T) {
	mux := shareMux(t)
	if w := post(mux, "/share-create", `{"id":"nope","dir":"proj"}`); w.Code != 400 {
		t.Fatalf("id invalide : %d", w.Code)
	}
	if w := post(mux, "/share-create", `{"id":"`+shSess+`","dir":"absent"}`); w.Code != 404 {
		t.Fatalf("session absente : %d", w.Code)
	}
	w := post(mux, "/share-create", `{"id":"`+shSess+`","dir":"proj"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token":"`) {
		t.Fatalf("create : %d %s", w.Code, w.Body)
	}
	info := func() string {
		r := httptest.NewRequest("GET", "/share-info?id="+shSess, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Body.String()
	}
	if got := info(); !strings.Contains(got, `"shared":true`) || !strings.Contains(got, `"mode":"rw"`) || !strings.Contains(got, `"connected":false`) {
		t.Fatalf("info : %s", got)
	}
	if w := post(mux, "/share-mode", `{"id":"`+shSess+`","mode":"x"}`); w.Code != 400 {
		t.Fatalf("mode invalide : %d", w.Code)
	}
	if w := post(mux, "/share-mode", `{"id":"`+shSess+`","mode":"ro"}`); w.Code != 200 {
		t.Fatalf("mode : %d", w.Code)
	}
	if !strings.Contains(info(), `"mode":"ro"`) {
		t.Fatal("mode non appliqué")
	}
	if w := post(mux, "/share-end", `{"id":"`+shSess+`"}`); w.Code != 200 {
		t.Fatalf("end : %d", w.Code)
	}
	if !strings.Contains(info(), `"shared":false`) {
		t.Fatal("toujours partagé")
	}
}

func TestShareUnavailable(t *testing.T) {
	mux := shareMux(t)
	shareSt = nil
	if w := post(mux, "/share-create", `{"id":"`+shSess+`","dir":"proj"}`); w.Code != 503 {
		t.Fatalf("code %d", w.Code)
	}
}

func mkShare(t *testing.T, s *shareStore) string {
	t.Helper()
	tok, err := s.Create(shSess, "dir")
	if err != nil {
		t.Fatal(err)
	}
	return tok
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
	if tok, err := s.Create(shSess, "dir2"); err == nil || tok != "" {
		t.Fatalf("erreur attendue : %q %v", tok, err)
	}
	if sh, ok := s.Get(shSess); !ok || sh.Dir != "dir" {
		t.Fatalf("ancien partage perdu : %+v", sh)
	}
	if sh, _ := s.Get(shSess); sh.TokenHash != hashSecret(old) {
		t.Fatal("ancien jeton invalidé")
	}
}

func TestShareCreateFailureLeavesNoShare(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	lock(t, d)
	if _, err := s.Create(shSess, "dir"); err == nil {
		t.Fatal("erreur attendue")
	}
	if _, ok := s.Get(shSess); ok {
		t.Fatal("partage non persisté laissé en mémoire")
	}
}

func TestShareSetModeRollsBackOnWriteFailure(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	mkShare(t, s)
	lock(t, d)
	if err := s.SetMode(shSess, "ro"); err == nil || errors.Is(err, errShareNone) {
		t.Fatalf("erreur attendue : %v", err)
	}
	if sh, _ := s.Get(shSess); sh.Mode != "rw" {
		t.Fatalf("mode %q", sh.Mode)
	}
}

func TestShareEndRevokesInMemoryOnWriteFailure(t *testing.T) {
	d := roDir(t)
	s, _ := openShares(d)
	tok := mkShare(t, s)
	c, _, _ := s.Redeem(tok)
	lock(t, d)
	if err := s.End(shSess); err == nil || errors.Is(err, errShareNone) {
		t.Fatalf("erreur attendue : %v", err)
	}
	if _, ok := s.ByCookie(c); ok {
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
		s.SetMode("x", "ro")
		if _, err := s.Create(shSess, "dir"); err != nil {
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

func TestShareRoutes503WhenNil(t *testing.T) {
	mux := shareMux(t)
	shareSt = nil
	for _, p := range []string{"/share-mode", "/share-end"} {
		if w := post(mux, p, `{"id":"`+shSess+`","mode":"ro"}`); w.Code != 503 {
			t.Fatalf("%s : %d", p, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/share-info?id="+shSess, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("info : %d", w.Code)
	}
}

func TestShareEndRouteDropsGuestItems(t *testing.T) {
	mux := shareMux(t)
	oldSt := queueSt
	q, _ := openQueue(t.TempDir())
	queueSt = q
	t.Cleanup(func() { queueSt = oldSt })
	post(mux, "/share-create", `{"id":"`+shSess+`","dir":"proj"}`)
	q.Add(shSess, "guest", "g")
	q.Add(shSess, "host", "h")
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
	if w := post(mux, "/share-create", `{"id":"`+shSess+`","dir":"proj"}`); w.Code != 500 || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("code %d %s", w.Code, w.Body)
	}
}
