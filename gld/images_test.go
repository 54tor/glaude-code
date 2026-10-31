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
	localData, _ := base64.StdEncoding.DecodeString(png1)
	path, err := saveImage(localData, "")
	if err != nil || !strings.HasPrefix(path, imagesDir()+string(filepath.Separator)) || !strings.HasSuffix(path, ".png") {
		t.Fatal(path, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
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
		"gros":  append(slices.Clone(localData), make([]byte, imgMax)...),
	} {
		if _, err := saveImage(data, ""); err == nil {
			t.Errorf("%s accepté", name)
		}
	}
	// une image de plus de 14 jours est retirée au passage
	old := filepath.Join(imagesDir(), "ancienne.png")
	os.WriteFile(old, localData, 0o600)
	os.Chtimes(old, time.Now().Add(-15*24*time.Hour), time.Now().Add(-15*24*time.Hour))
	if _, err := saveImage(localData, ""); err != nil {
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
	for index := range ms {
		if ms[index].Img != "" {
			img = &ms[index]
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
	for name, callback := range map[string]func() error{
		"texte":           func() error { _, _, err := convImage(tp, 0, 0); return err },
		"hors liste":      func() error { _, _, err := convImage(tp, 0, 9); return err },
		"décalage nég.":   func() error { _, _, err := convImage(tp, -1, 0); return err },
		"milieu de ligne": func() error { _, _, err := convImage(tp, 7, 1); return err },
		"faux contenu":    func() error { _, _, err := convImage(tp, off2, 0); return err },
	} {
		if callback() == nil {
			t.Errorf("%s accepté", name)
		}
	}
	// route : les paramètres invalides ne passent pas, le type est fixé par le contenu
	hosts[testHost] = true
	localHandler := handler()
	recorder := getRaw(localHandler, "/v1/sessions/"+id1+"/conversation/images/0-1?dir=-p")
	if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "image/png" || recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("route : %d %v", recorder.Code, recorder.Header())
	}
	for rawURL, want := range map[string]int{
		"/v1/sessions/" + id1 + "/conversation/images/0-1?dir=..": 404,
		"/v1/sessions/" + id1 + "/conversation/images/x-1?dir=-p": 404,
		"/v1/sessions/%2e%2e/conversation/images/0-1?dir=-p":      400,
	} {
		if recorder := getRaw(localHandler, rawURL); recorder.Code != want {
			t.Errorf("%s : %d", rawURL, recorder.Code)
		}
	}
}

func TestChatImageDossier(t *testing.T) {
	use(t.TempDir())
	img := filepath.Join(imagesDir(), "x.png")
	values := chatArgs(nil, id1, "", "décris @"+img)
	index := slices.Index(values, "--add-dir")
	if index < 0 || values[index+1] != imagesDir() || slices.Index(values, "--") < index {
		t.Fatalf("dossier non ajouté avant « -- » : %v", values)
	}
	if slices.Contains(chatArgs(nil, id1, "", "sans image"), "--add-dir") {
		t.Fatal("dossier ajouté sans image")
	}
}

func getRaw(handler http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", path, nil)
	request.Host = testHost
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
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
	data, _ := base64.StdEncoding.DecodeString(png1)
	other := "11111111-1111-1111-1111-111111111111"
	p1, err := saveImage(data, sid)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := saveImage(data, other)
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
	data, _ := base64.StdEncoding.DecodeString(png1)
	path, err := saveImage(data, "")
	if err != nil {
		t.Fatal(err)
	}
	file, typ, err := uploadImage(filepath.Base(path))
	if err != nil || typ != "image/png" {
		t.Fatal(typ, err)
	}
	file.Close()
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "secret.png"), data, 0o600)
	os.WriteFile(filepath.Join(out, "texte.txt"), []byte("pas une image"), 0o600)
	os.Symlink(filepath.Join(out, "secret.png"), filepath.Join(imagesDir(), "lien.png"))
	os.Symlink(out, filepath.Join(imagesDir(), "dossier"))
	os.WriteFile(filepath.Join(imagesDir(), "faux.png"), []byte("pas une image"), 0o600)
	for _, name := range []string{"lien.png", "dossier/secret.png", "../secret.png", "/etc/passwd", ".cache", "faux.png", "absent.png", ""} {
		if file, _, err := uploadImage(name); err == nil {
			file.Close()
			t.Errorf("%q servi", name)
		}
	}
}
