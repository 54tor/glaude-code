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
	for _, record := range cases {
		got, err := ideRel(record.in, record.allowRoot)
		if record.bad {
			if !errors.Is(err, errIdePath) {
				t.Errorf("%q: want errIdePath, got %v", record.in, err)
			}
			continue
		}
		if err != nil || got != record.want {
			t.Errorf("%q: got %q, %v want %q", record.in, got, err, record.want)
		}
	}
}

func TestIdeKnownDir(t *testing.T) {
	handler, _, proj := web(t)
	if got, ok := ideKnownDir(proj); !ok || got != proj {
		t.Fatalf("proj: %q %v", got, ok)
	}
	extra := t.TempDir()
	if _, ok := ideKnownDir(extra); ok {
		t.Fatal("unregistered dir accepted")
	}
	v1ok(t, handler, "POST", "/v1/directories", `{"dir":"`+extra+`"}`, "")
	if _, ok := ideKnownDir(extra); !ok {
		t.Fatal("added dir refused")
	}
	for _, dir := range []string{"", "relatif", proj + "/../p", proj + "/", proj + "\x00", filepath.Join(proj, "sub")} {
		if _, ok := ideKnownDir(dir); ok {
			t.Errorf("%q accepted", dir)
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

	for _, path := range []string{"plain.txt", "real", "real/f.txt", "."} {
		if _, err := ideLstat(root, path); err != nil {
			t.Errorf("%q: %v", path, err)
		}
	}
	for _, path := range []string{"lnkfile", "lnkdir", "lnkdir/f.txt", "dangling", "esc", "esc/secret", "escfile"} {
		if _, err := ideLstat(root, path); !errors.Is(err, errIdeSymlink) {
			t.Errorf("%q: want errIdeSymlink, got %v", path, err)
		}
	}
	for _, path := range []string{"absent", "real/absent", "absent/x"} {
		if _, err := ideLstat(root, path); !errors.Is(err, errIdeNotFound) {
			t.Errorf("%q: want errIdeNotFound, got %v", path, err)
		}
	}
}

func TestIdeOpenRootAndFail(t *testing.T) {
	_, _, proj := web(t)
	root, err := ideOpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if _, err := ideOpenRoot(t.TempDir()); !errors.Is(err, errIdeDir) {
		t.Fatalf("got %v", err)
	}
	cases := []struct {
		code int
		e    error
	}{{404, errIdeDir}, {404, errIdeNotFound}, {400, errIdePath}, {403, errIdeSymlink}, {413, errIdeTooBig}, {415, errIdeBinary},
		{409, errIdeConflict}, {409, errIdeExists}, {500, errors.New("open /secret/disk/path: boom")}}
	for _, record := range cases {
		code, err := record.code, record.e
		recorder := httptest.NewRecorder()
		ideFail(recorder, err)
		if recorder.Code != code || strings.Contains(recorder.Body.String(), "/secret") || !strings.Contains(recorder.Body.String(), `"error":{"code"`) ||
			(code != 500 && !strings.Contains(recorder.Body.String(), err.Error())) {
			t.Errorf("%d: got %d %s", code, recorder.Code, recorder.Body)
		}
	}
	localRecorder := httptest.NewRecorder()
	ideFail(localRecorder, errors.New("openat /x: path escapes from parent"))
	if localRecorder.Code != http.StatusForbidden {
		t.Errorf("escape: %d", localRecorder.Code)
	}
}

func TestIdeForbiddenRoot(t *testing.T) {
	_, cfg, proj := web(t)
	home, _ := os.UserHomeDir()
	auth := filepath.Join(t.TempDir(), "authx")
	os.MkdirAll(filepath.Join(auth, "sub"), 0o755)
	t.Setenv("GLD_AUTH_DIR", auth)
	os.MkdirAll(filepath.Join(cfg, "projects"), 0o755)
	for _, dir := range []string{filepath.Join(home, ".claudex"), filepath.Join(home, "pp")} {
		os.MkdirAll(dir, 0o755)
	}
	link := filepath.Join(t.TempDir(), "lienhome")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	bad := []string{home, filepath.Dir(home), "/", cfg, filepath.Join(cfg, "projects"),
		auth, filepath.Join(auth, "sub"), filepath.Dir(auth), link}
	for _, dir := range bad {
		if !ideForbiddenRoot(dir) {
			t.Errorf("%q should be forbidden", dir)
		}
	}
	for _, dir := range []string{proj, filepath.Join(home, ".claudex"), filepath.Join(home, "pp"), t.TempDir() + "/z"} {
		if ideForbiddenRoot(dir) {
			t.Errorf("%q should be allowed", dir)
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
	for _, dir := range []string{cfg, filepath.Dir(home)} {
		if _, ok := ideKnownDir(dir); ok {
			t.Errorf("%q accepted", dir)
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
	for _, dir := range []string{pro, perso, filepath.Join(pro, "projects"), filepath.Join(home, ".claude-prox")} {
		os.MkdirAll(dir, 0o755)
	}
	if err := saveHomes([]Home{{Name: "pro", Dir: pro}, {Name: "perso", Dir: perso}}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{pro, filepath.Join(pro, "projects"), perso} {
		if !ideForbiddenRoot(dir) {
			t.Errorf("main : %q accepté", dir)
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
	for _, dir := range []string{cfg, filepath.Join(cfg, "projects"), perso, pro} {
		if !ideForbiddenRoot(dir) {
			t.Errorf("enfant : %q accepté", dir)
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
