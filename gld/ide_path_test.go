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

func TestIdeRel(t *testing.T) {
	cases := []struct {
		in        string
		allowRoot bool
		want      string
		bad       bool
	}{
		{"a/b", false, "a/b", false},
		{"a//b/./c", false, "a/b/c", false},
		{"", false, "", true},
		{".", false, ".", true},
		{"", true, ".", false},
		{".", true, ".", false},
		{"./", false, "", true},
		{".//", false, "", true},
		{"./.", false, "", true},
		{"./", true, ".", false},
		{".//", true, ".", false},
		{"./.", true, ".", false},
		{"/etc", false, "", true},
		{"/", true, "", true},
		{"a/../../x", false, "", true},
		{"..", true, "", true},
		{"a/..", true, "", true},
		{"a/\x00", false, "", true},
		{"a\\b", false, "", true},
	}
	for _, c := range cases {
		got, err := ideRel(c.in, c.allowRoot)
		if c.bad {
			if !errors.Is(err, errIdePath) {
				t.Errorf("%q: want errIdePath, got %v", c.in, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got %q, %v want %q", c.in, got, err, c.want)
		}
	}
}

func TestIdeKnownDir(t *testing.T) {
	h, _, proj := web(t)
	if got, ok := ideKnownDir(proj); !ok || got != proj {
		t.Fatalf("proj: %q %v", got, ok)
	}
	extra := t.TempDir()
	if _, ok := ideKnownDir(extra); ok {
		t.Fatal("unregistered dir accepted")
	}
	ok(t, h, "/dirs", `{"op":"add","dir":"`+extra+`"}`, true, "")
	if _, ok := ideKnownDir(extra); !ok {
		t.Fatal("added dir refused")
	}
	for _, d := range []string{"", "relatif", proj + "/../p", proj + "/", proj + "\x00", filepath.Join(proj, "sub")} {
		if _, ok := ideKnownDir(d); ok {
			t.Errorf("%q accepted", d)
		}
	}
}

func TestIdeLstat(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "real"), 0o755)
	os.WriteFile(filepath.Join(dir, "real", "f.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("x"), 0o644)
	os.Symlink("plain.txt", filepath.Join(dir, "lnkfile"))
	os.Symlink("real", filepath.Join(dir, "lnkdir"))
	os.Symlink("nowhere", filepath.Join(dir, "dangling"))
	os.Symlink(outside, filepath.Join(dir, "esc"))
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "escfile"))
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	for _, p := range []string{"plain.txt", "real", "real/f.txt", "."} {
		if _, err := ideLstat(root, p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"lnkfile", "lnkdir", "lnkdir/f.txt", "dangling", "esc", "esc/secret", "escfile"} {
		if _, err := ideLstat(root, p); !errors.Is(err, errIdeSymlink) {
			t.Errorf("%q: want errIdeSymlink, got %v", p, err)
		}
	}
	for _, p := range []string{"absent", "real/absent", "absent/x"} {
		if _, err := ideLstat(root, p); !errors.Is(err, errIdeNotFound) {
			t.Errorf("%q: want errIdeNotFound, got %v", p, err)
		}
	}
}

func TestIdeOpenRootAndFail(t *testing.T) {
	_, _, proj := web(t)
	r, err := ideOpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, err := ideOpenRoot(t.TempDir()); !errors.Is(err, errIdeDir) {
		t.Fatalf("got %v", err)
	}
	cases := map[int]error{404: errIdeDir, 400: errIdePath, 403: errIdeSymlink, 413: errIdeTooBig, 415: errIdeBinary, 409: errIdeConflict, 500: errors.New("open /secret/disk/path: boom")}
	for code, e := range cases {
		w := httptest.NewRecorder()
		ideFail(w, e)
		if w.Code != code || strings.Contains(w.Body.String(), "/secret") || !strings.Contains(w.Body.String(), `"ok":false`) {
			t.Errorf("%d: got %d %s", code, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	ideFail(w, errors.New("openat /x: path escapes from parent"))
	if w.Code != http.StatusForbidden {
		t.Errorf("escape: %d", w.Code)
	}
}
