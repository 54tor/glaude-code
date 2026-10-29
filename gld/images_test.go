package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// 1 pixel PNG valide
const png1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestDepotImage(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	b, _ := base64.StdEncoding.DecodeString(png1)
	p, err := saveImage(b, "")
	if err != nil || !strings.HasPrefix(p, imagesDir()+string(filepath.Separator)) || !strings.HasSuffix(p, ".png") {
		t.Fatal(p, err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("droits : %v %v", fi, err)
	}
	if fi, _ := os.Stat(imagesDir()); fi.Mode().Perm() != 0o700 {
		t.Fatalf("dossier : %v", fi.Mode().Perm())
	}
	for name, data := range map[string][]byte{
		"vide":  nil,
		"texte": []byte("pas une image"),
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":  []byte("<html><script>x</script></html>"),
		"gros":  append(slices.Clone(b), make([]byte, imgMax)...),
	} {
		if _, err := saveImage(data, ""); err == nil {
			t.Errorf("%s accepté", name)
		}
	}
	// une image de plus de 14 jours est retirée au passage
	old := filepath.Join(imagesDir(), "ancienne.png")
	os.WriteFile(old, b, 0o600)
	os.Chtimes(old, time.Now().Add(-15*24*time.Hour), time.Now().Add(-15*24*time.Hour))
	if _, err := saveImage(b, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("ancienne image gardée")
	}
}

func TestImagesDesConversations(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	line1 := `{"type":"user","timestamp":"2026-10-22T00:00:00Z","message":{"role":"user","content":[{"type":"text","text":"regarde"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + png1 + `"}}]}}` + "\n"
	line2 := `{"type":"assistant","message":{"content":[{"type":"text","text":"vu"}]}}` + "\n"
	fake := `{"type":"user","message":{"content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1)>")) + `"}}]}}` + "\n"
	tp := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	put(t, tp, line1+line2+fake, old)

	ms := conv(tp, false)
	var img *Msg
	for i := range ms {
		if ms[i].Img != "" {
			img = &ms[i]
			break
		}
	}
	if len(ms) < 3 || img == nil || img.Img != "image/png" || img.K != 1 || img.T != "" || img.R != "user" {
		t.Fatalf("messages : %+v", ms)
	}
	data, typ, err := convImage(tp, img.O, img.K)
	if err != nil || typ != "image/png" || len(data) < 60 {
		t.Fatal(typ, len(data), err)
	}
	off2 := int64(len(line1) + len(line2))
	for name, f := range map[string]func() error{
		"texte":           func() error { _, _, e := convImage(tp, 0, 0); return e },
		"hors liste":      func() error { _, _, e := convImage(tp, 0, 9); return e },
		"décalage nég.":   func() error { _, _, e := convImage(tp, -1, 0); return e },
		"milieu de ligne": func() error { _, _, e := convImage(tp, 7, 1); return e },
		"faux contenu":    func() error { _, _, e := convImage(tp, off2, 0); return e },
	} {
		if f() == nil {
			t.Errorf("%s accepté", name)
		}
	}
	// route : les paramètres invalides ne passent pas, le type est fixé par le contenu
	hosts[testHost] = true
	h := handler()
	w := getRaw(h, "/v1/sessions/"+id1+"/conversation/images/0-1?dir=-p")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("route : %d %v", w.Code, w.Header())
	}
	for _, q := range []string{"dir=..&id=" + id1 + "&o=0&k=1", "dir=-p&id=" + id1 + "&o=x&k=1", "dir=-p&id=%2e%2e&o=0&k=1"} {
		if w := getRaw(h, "/conv-image?"+q); w.Code != 404 {
			t.Errorf("%s : %d", q, w.Code)
		}
	}
}

func TestChatImageDossier(t *testing.T) {
	use(t.TempDir())
	img := filepath.Join(imagesDir(), "x.png")
	a := chatArgs(nil, id1, "", "décris @"+img)
	i := slices.Index(a, "--add-dir")
	if i < 0 || a[i+1] != imagesDir() || slices.Index(a, "--") < i {
		t.Fatalf("dossier non ajouté avant « -- » : %v", a)
	}
	if slices.Contains(chatArgs(nil, id1, "", "sans image"), "--add-dir") {
		t.Fatal("dossier ajouté sans image")
	}
}

func getRaw(h http.Handler, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = testHost
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestFileExt(t *testing.T) {
	for in, want := range map[string]string{"a.PDF": ".pdf", "x": ".bin", "a/../b.t$t": ".bin", "r.tar.gz": ".gz", "n.averyveryl": ".bin"} {
		if got := fileExt(in); got != want {
			t.Errorf("fileExt(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestImageDeposeSousSessionEstRetireeAvecElle(t *testing.T) {
	use(t.TempDir())
	b, _ := base64.StdEncoding.DecodeString(png1)
	other := "11111111-1111-1111-1111-111111111111"
	p1, err := saveImage(b, sid)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := saveImage(b, other)
	if !strings.HasPrefix(filepath.Base(p1), sid+"-") {
		t.Fatalf("nom sans préfixe : %s", p1)
	}
	dropSessionFiles([]string{sid})
	if _, err := os.Stat(p1); err == nil {
		t.Fatal("l'image de la session est restée")
	}
	if _, err := os.Stat(p2); err != nil {
		t.Fatal("l'image d'une autre session a été retirée")
	}
}

func TestUploadImageConfinee(t *testing.T) {
	use(t.TempDir())
	b, _ := base64.StdEncoding.DecodeString(png1)
	p, err := saveImage(b, "")
	if err != nil {
		t.Fatal(err)
	}
	f, typ, err := uploadImage(filepath.Base(p))
	if err != nil || typ != "image/png" {
		t.Fatal(typ, err)
	}
	f.Close()
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "secret.png"), b, 0o600)
	os.WriteFile(filepath.Join(out, "texte.txt"), []byte("pas une image"), 0o600)
	os.Symlink(filepath.Join(out, "secret.png"), filepath.Join(imagesDir(), "lien.png"))
	os.Symlink(out, filepath.Join(imagesDir(), "dossier"))
	os.WriteFile(filepath.Join(imagesDir(), "faux.png"), []byte("pas une image"), 0o600)
	for _, name := range []string{"lien.png", "dossier/secret.png", "../secret.png", "/etc/passwd", ".cache", "faux.png", "absent.png", ""} {
		if f, _, err := uploadImage(name); err == nil {
			f.Close()
			t.Errorf("%q servi", name)
		}
	}
}
