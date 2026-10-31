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
	dir := t.TempDir()
	localShareStore, err := openShares(dir)
	if err != nil {
		t.Fatal(err)
	}
	return localShareStore, dir
}

func TestShareRedeemOnce(t *testing.T) {
	shareStore, _ := newShares(t)
	tok := mkShare(t, shareStore)
	content, sh, inv, ok := shareStore.Redeem(tok)
	if !ok || content == "" || sh.Session != shSess || inv.CookieHash != hashSecret(content) {
		t.Fatalf("1er Redeem : %v %q %+v", ok, content, sh)
	}
	if _, _, _, ok := shareStore.Redeem(tok); ok {
		t.Fatal("2e Redeem accepté")
	}
	if _, _, _, ok := shareStore.Redeem("n-importe-quoi"); ok {
		t.Fatal("jeton inconnu accepté")
	}
}

func TestShareCookieSurvivesReload(t *testing.T) {
	shareStore, dir := newShares(t)
	tok := mkShare(t, shareStore)
	content, _, _, _ := shareStore.Redeem(tok)
	if _, _, ok := shareStore.ByCookie(content); !ok {
		t.Fatal("cookie refusé")
	}
	s2, err := openShares(dir)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if sh, _, ok := s2.ByCookie(content); !ok || sh.Session != shSess {
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
	shareStore, _ := newShares(t)
	oldSt := queueSt
	queueStore, _ := openQueue(t.TempDir())
	queueSt = queueStore
	t.Cleanup(func() { queueSt = oldSt })
	_, invite := mkInv(t, shareStore)
	_, localInvite := mkInv(t, shareStore)
	queueStore.Add(shSess, "guest", invite.ID, "g")
	queueStore.Add(shSess, "guest", localInvite.ID, "g2")
	queueStore.Add(shSess, "host", "", "h")
	if err := shareStore.End(shSess); err != nil {
		t.Fatal(err)
	}
	qItems := queueStore.List(shSess)
	if len(qItems) != 1 || qItems[0].Author != "host" {
		t.Fatalf("file : %+v", qItems)
	}
	if _, ok := shareStore.Get(shSess); ok {
		t.Fatal("partage toujours là")
	}
	if !errors.Is(shareStore.End(shSess), errShareNone) {
		t.Fatal("2e End doit renvoyer errShareNone")
	}
}

func TestShareFileNotLeakingToken(t *testing.T) {
	shareStore, dir := newShares(t)
	tok := mkShare(t, shareStore)
	content, _, _, _ := shareStore.Redeem(tok)
	data, err := os.ReadFile(filepath.Join(dir, "shares.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), tok) || strings.Contains(string(data), content) {
		t.Fatalf("secret en clair : %s", data)
	}
}

func TestShareSetModeAndGet(t *testing.T) {
	shareStore, _ := newShares(t)
	if !errors.Is(shareStore.SetInviteMode(shSess, "0123456789abcdef", "ro"), errInviteNone) {
		t.Fatal("SetInviteMode sans partage")
	}
	_, in := mkInv(t, shareStore)
	if err := shareStore.SetInviteMode(shSess, in.ID, "ro"); err != nil {
		t.Fatal(err)
	}
	if sh, _ := shareStore.Get(shSess); sh.Invites[0].Mode != "ro" {
		t.Fatalf("mode %q", sh.Invites[0].Mode)
	}
}

func shareMux(t *testing.T) *http.ServeMux {
	t.Helper()
	oldSt, oldDir, oldRoot := shareSt, shareDir, root
	dir := t.TempDir()
	shareDir = func() string { return dir }
	root = filepath.Join(t.TempDir(), "projects")
	os.MkdirAll(filepath.Join(root, "proj"), 0o755)
	os.WriteFile(filepath.Join(root, "proj", shSess+".jsonl"), []byte("{}\n"), 0o644)
	t.Cleanup(func() { shareSt, shareDir, root = oldSt, oldDir, oldRoot })
	mux := http.NewServeMux()
	routesShare(mux)
	return mux
}

func mkShare(t *testing.T, localShareStore *shareStore) string {
	t.Helper()
	tok, _ := mkInv(t, localShareStore)
	return tok
}

func mkInv(t *testing.T, localShareStore *shareStore) (string, Invite) {
	t.Helper()
	tok, in, err := localShareStore.CreateInvite(shSess, "dir", "", "")
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

func lock(t *testing.T, dir string) {
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
}

func TestShareCreateRollsBackOnWriteFailure(t *testing.T) {
	dir := roDir(t)
	shareStore, _ := openShares(dir)
	old := mkShare(t, shareStore)
	lock(t, dir)
	if tok, _, err := shareStore.CreateInvite(shSess, "dir2", "titre", ""); err == nil || tok != "" {
		t.Fatalf("erreur attendue : %q %v", tok, err)
	}
	if sh, ok := shareStore.Get(shSess); !ok || sh.Dir != "dir" || sh.Title != "" || sh.Seq != 1 || len(sh.Invites) != 1 {
		t.Fatalf("ancien partage perdu : %+v", sh)
	}
	if sh, _ := shareStore.Get(shSess); sh.Invites[0].TokenHash != hashSecret(old) {
		t.Fatal("ancien jeton invalidé")
	}
}

func TestShareCreateFailureLeavesNoShare(t *testing.T) {
	dir := roDir(t)
	shareStore, _ := openShares(dir)
	lock(t, dir)
	if _, _, err := shareStore.CreateInvite(shSess, "dir", "", ""); err == nil {
		t.Fatal("erreur attendue")
	}
	if _, ok := shareStore.Get(shSess); ok {
		t.Fatal("partage non persisté laissé en mémoire")
	}
}

func TestShareSetModeRollsBackOnWriteFailure(t *testing.T) {
	dir := roDir(t)
	shareStore, _ := openShares(dir)
	_, in := mkInv(t, shareStore)
	lock(t, dir)
	if err := shareStore.SetInviteMode(shSess, in.ID, "ro"); err == nil || errors.Is(err, errInviteNone) {
		t.Fatalf("erreur attendue : %v", err)
	}
	if sh, _ := shareStore.Get(shSess); sh.Invites[0].Mode != "rw" {
		t.Fatalf("mode %q", sh.Invites[0].Mode)
	}
}

func TestShareEndRevokesInMemoryOnWriteFailure(t *testing.T) {
	dir := roDir(t)
	shareStore, _ := openShares(dir)
	tok := mkShare(t, shareStore)
	content, _, _, _ := shareStore.Redeem(tok)
	lock(t, dir)
	if err := shareStore.End(shSess); err == nil || errors.Is(err, errShareNone) {
		t.Fatalf("erreur attendue : %v", err)
	}
	if _, _, ok := shareStore.ByCookie(content); ok {
		t.Fatal("cookie encore valide en mémoire")
	}
}

func TestShareOpenNullEntries(t *testing.T) {
	for _, content := range []string{"null", `{"x":null}`} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "shares.json"), []byte(content), 0o600)
		shareStore, err := openShares(dir)
		if err != nil {
			t.Fatal(err)
		}
		shareStore.ByCookie("x")
		shareStore.SetInviteMode("x", "0123456789abcdef", "ro")
		if _, _, err := shareStore.CreateInvite(shSess, "dir", "", ""); err != nil {
			t.Fatal(content, err)
		}
		if _, ok := shareStore.Get("x"); ok {
			t.Fatal("entrée nulle conservée")
		}
	}
}

func TestShareCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shares.json")
	os.WriteFile(path, []byte("{pas du json"), 0o600)
	if _, err := openShares(dir); err == nil {
		t.Fatal("erreur attendue")
	}
	oldSt, oldDir := shareSt, shareDir
	shareDir = func() string { return dir }
	t.Cleanup(func() { shareSt, shareDir = oldSt, oldDir })
	mux := http.NewServeMux()
	routesShare(mux)
	if recorder := send(mux, "DELETE", "/v1/shares/"+shSess, ""); recorder.Code != 503 {
		t.Fatalf("code %d", recorder.Code)
	}
	if data, _ := os.ReadFile(path); string(data) != "{pas du json" {
		t.Fatalf("fichier modifié : %q", data)
	}
}

func TestShareEndRouteDropsGuestItems(t *testing.T) {
	mux := shareMux(t)
	oldSt := queueSt
	queueStore, _ := openQueue(t.TempDir())
	queueSt = queueStore
	t.Cleanup(func() { queueSt = oldSt })
	_, in, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	queueStore.Add(shSess, "guest", in.ID, "g")
	queueStore.Add(shSess, "host", "", "h")
	if recorder := send(mux, "DELETE", "/v1/shares/"+shSess, ""); recorder.Code != 204 {
		t.Fatalf("code %d", recorder.Code)
	}
	if qItems := queueStore.List(shSess); len(qItems) != 1 || qItems[0].Author != "host" {
		t.Fatalf("file : %+v", qItems)
	}
}

func TestShareCreateRouteFailsOnWrite(t *testing.T) {
	mux := shareMux(t)
	dir := roDir(t)
	shareDir = func() string { return dir }
	routesShare(http.NewServeMux())
	lock(t, dir)
	if recorder := post(mux, "/v1/invites", `{"id":"`+shSess+`","dir":"proj"}`); recorder.Code != 500 || strings.Contains(recorder.Body.String(), "token") {
		t.Fatalf("code %d %s", recorder.Code, recorder.Body)
	}
}

func TestShareReadOnlyDropsQueuedGuestItems(t *testing.T) {
	mux := shareMux(t)
	oldSt := queueSt
	qd := t.TempDir()
	queueStore, _ := openQueue(qd)
	queueSt = queueStore
	t.Cleanup(func() { queueSt = oldSt })
	_, in, _ := shareSt.CreateInvite(shSess, "proj", "", "")
	mode := `{"id":"` + shSess + `","mode":"ro"}`
	modePath := "/v1/invites/" + in.ID
	qItem, _ := queueStore.Add(shSess, "guest", in.ID, "en cours")
	queueStore.MarkSending(shSess, qItem.ID)
	queueStore.Add(shSess, "guest", in.ID, "g")
	queueStore.Add(shSess, "host", "", "h")
	if recorder := send(mux, "PATCH", modePath, mode); recorder.Code != 204 {
		t.Fatalf("code %d", recorder.Code)
	}
	qItems := queueStore.List(shSess)
	if len(qItems) != 2 || qItems[0].ID != qItem.ID || qItems[1].Author != "host" {
		t.Fatalf("file : %+v", qItems)
	}
	queueStore.Add(shSess, "host", "", "h2")
	queueStore.Add(shSess, "guest", in.ID, "g2") // la garde refuse l'ajout invité en lecture seule ; ici on teste l'échec d'écriture
	if os.Geteuid() == 0 {
		return
	}
	os.Chmod(qd, 0o500)
	t.Cleanup(func() { os.Chmod(qd, 0o700) })
	if recorder := send(mux, "PATCH", modePath, mode); recorder.Code != 500 {
		t.Fatalf("échec d'écriture de la file : %d", recorder.Code)
	}
	if sh, _ := shareSt.Get(shSess); sh.Invites[0].Mode != "ro" {
		t.Fatalf("mode = %q", sh.Invites[0].Mode)
	}
	for _, it := range queueStore.List(shSess) {
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
