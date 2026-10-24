package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	cases := []struct {
		code int
		e    error
	}{{404, errIdeDir}, {404, errIdeNotFound}, {400, errIdePath}, {403, errIdeSymlink}, {413, errIdeTooBig}, {415, errIdeBinary},
		{409, errIdeConflict}, {409, errIdeExists}, {500, errors.New("open /secret/disk/path: boom")}}
	for _, c := range cases {
		code, e := c.code, c.e
		w := httptest.NewRecorder()
		ideFail(w, e)
		if w.Code != code || strings.Contains(w.Body.String(), "/secret") || !strings.Contains(w.Body.String(), `"ok":false`) ||
			(code != 500 && !strings.Contains(w.Body.String(), e.Error())) {
			t.Errorf("%d: got %d %s", code, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	ideFail(w, errors.New("openat /x: path escapes from parent"))
	if w.Code != http.StatusForbidden {
		t.Errorf("escape: %d", w.Code)
	}
}

func TestIdeForbiddenRoot(t *testing.T) {
	_, cfg, proj := web(t)
	home, _ := os.UserHomeDir()
	auth := filepath.Join(t.TempDir(), "authx")
	os.MkdirAll(filepath.Join(auth, "sub"), 0o755)
	t.Setenv("GLD_AUTH_DIR", auth)
	os.MkdirAll(filepath.Join(cfg, "projects"), 0o755)
	for _, d := range []string{filepath.Join(home, ".claudex"), filepath.Join(home, "pp")} {
		os.MkdirAll(d, 0o755)
	}
	link := filepath.Join(t.TempDir(), "lienhome")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	bad := []string{home, filepath.Dir(home), "/", cfg, filepath.Join(cfg, "projects"),
		auth, filepath.Join(auth, "sub"), filepath.Dir(auth), link}
	for _, d := range bad {
		if !ideForbiddenRoot(d) {
			t.Errorf("%q should be forbidden", d)
		}
	}
	for _, d := range []string{proj, filepath.Join(home, ".claudex"), filepath.Join(home, "pp"), t.TempDir() + "/z"} {
		if ideForbiddenRoot(d) {
			t.Errorf("%q should be allowed", d)
		}
	}
}

func TestIdeKnownDirForbiddenAllWays(t *testing.T) {
	_, cfg, _ := web(t)
	home, _ := os.UserHomeDir()
	put(t, filepath.Join(cfg, "projects", "-h", id2+".jsonl"),
		`{"type":"user","cwd":"`+home+`","timestamp":"2026-01-01T00:00:00Z","message":{"content":"x"}}`+"\n", old)
	if _, ok := ideKnownDir(home); ok {
		t.Error("session cwd = HOME accepted")
	}
	st := loadStore()
	st.Dirs = append(st.Dirs, cfg)
	st.Projects = append(st.Projects, Project{Name: "h", Dirs: []string{filepath.Dir(home)}})
	if err := saveStore(st); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{cfg, filepath.Dir(home)} {
		if _, ok := ideKnownDir(d); ok {
			t.Errorf("%q accepted", d)
		}
	}
	if _, err := ideOpenRoot(cfg); !errors.Is(err, errIdeDir) {
		t.Errorf("ideOpenRoot: %v", err)
	}
}

func TestIdeForbiddenOtherHomes(t *testing.T) {
	_, cfg, proj := web(t)
	home, _ := os.UserHomeDir()
	pro, perso := filepath.Join(home, ".claude-pro"), filepath.Join(home, ".claude-perso")
	for _, d := range []string{pro, perso, filepath.Join(pro, "projects"), filepath.Join(home, ".claude-prox")} {
		os.MkdirAll(d, 0o755)
	}
	if err := saveHomes([]Home{{Name: "pro", Dir: pro}, {Name: "perso", Dir: perso}}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{pro, filepath.Join(pro, "projects"), perso} {
		if !ideForbiddenRoot(d) {
			t.Errorf("main : %q accepté", d)
		}
	}
	if ideForbiddenRoot(filepath.Join(home, ".claude-prox")) || ideForbiddenRoot(proj) {
		t.Error("voisin refusé")
	}
	if !slices.Contains(childEnv(), ideMainCfgEnv+"="+cfg) {
		t.Fatal("childEnv ne transmet pas la config principale")
	}
	// enfant simulé : sa config est celle de pro, la principale n'est connue que par la variable
	use(pro)
	t.Cleanup(func() { use(cfg) })
	if ideForbiddenRoot(cfg) {
		t.Fatal("sans la variable, rien ne protège la config principale (préalable du test)")
	}
	t.Setenv(ideMainCfgEnv, cfg)
	for _, d := range []string{cfg, filepath.Join(cfg, "projects"), perso, pro} {
		if !ideForbiddenRoot(d) {
			t.Errorf("enfant : %q accepté", d)
		}
	}
	if ideForbiddenRoot(proj) {
		t.Error("enfant : projet refusé")
	}
}

func TestIdeForbiddenHomeUnresolvable(t *testing.T) {
	_, _, proj := web(t)
	t.Setenv("HOME", "")
	if !ideForbiddenRoot(proj) {
		t.Fatal("HOME introuvable : la garde doit refuser")
	}
}
