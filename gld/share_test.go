package main

import (
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
	tok := s.Create(shSess, "dir")
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
	tok := s.Create(shSess, "dir")
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
	old := s.Create(shSess, "dir")
	c, _, _ := s.Redeem(old)
	nw := s.Create(shSess, "dir")
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
	s.Create(shSess, "dir")
	q.Add(shSess, "guest", "g")
	q.Add(shSess, "host", "h")
	if !s.End(shSess) {
		t.Fatal("End a échoué")
	}
	l := q.List(shSess)
	if len(l) != 1 || l[0].Author != "host" {
		t.Fatalf("file : %+v", l)
	}
	if _, ok := s.Get(shSess); ok {
		t.Fatal("partage toujours là")
	}
	if s.End(shSess) {
		t.Fatal("2e End doit renvoyer false")
	}
}

func TestShareFileNotLeakingToken(t *testing.T) {
	s, d := newShares(t)
	tok := s.Create(shSess, "dir")
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
	if s.SetMode(shSess, "ro") {
		t.Fatal("SetMode sans partage")
	}
	s.Create(shSess, "dir")
	if !s.SetMode(shSess, "ro") {
		t.Fatal("SetMode")
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
