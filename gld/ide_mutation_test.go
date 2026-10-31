package main

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// Le champ prefix de GET /v1/git/changes ramène les chemins de git (relatifs à la racine du dépôt) au dossier ouvert.
func TestGitChangesPrefixJSON(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git absent")
	}
	handler, cfg, proj := web(t)
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Dir(gitBin))
	git(t, proj, "init", "-q", "-b", "main")
	sub := filepath.Join(proj, "sub", "dir")
	put(t, filepath.Join(sub, "f.txt"), "x\n", old)
	put(t, filepath.Join(cfg, "projects", "-p-sub-dir", id3+".jsonl"),
		`{"type":"user","cwd":"`+sub+`","timestamp":"2026-01-01T00:00:00Z","message":{"content":"x"}}`+"\n", old)
	other := t.TempDir()
	git(t, other, "init", "-q")
	for dir, want := range map[string]string{proj: `"prefix":""`, sub: `"prefix":"sub/dir/"`, other: `"prefix":""`} {
		if _, _, raw := call(t, handler, "GET", "/v1/git/changes?dir="+dir, ""); !strings.Contains(raw, want) {
			t.Errorf("%s : %s", dir, raw)
		}
	}
}

func TestFsRenameTrashRefused(t *testing.T) {
	handler, _, proj := web(t)
	mk(t, proj, "a.txt", "b.txt")
	id := trashOne(t, handler, proj, "b.txt")
	for _, values := range [][2]string{{"a.txt", ideTrash + "/x"}, {ideTrash + "/" + id + "/item", "back.txt"}, {ideTrash, "t2"}, {"a.txt", ideTrash}} {
		if code, _, raw := fsop(t, handler, "rename", map[string]any{"dir": proj, "from": values[0], "to": values[1]}); code != 400 {
			t.Errorf("%v : %d %s", values, code, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, "a.txt")); err != nil {
		t.Fatal(err)
	}
}

// Un dossier sorti de la racine (un seul évènement rename) ne laisse ni entrée fantôme ni veille sur l'ancien chemin.
func TestWatchRemovedDirNoGhosts(t *testing.T) {
	handler, _, proj := web(t)
	wsTest(t)
	mk(t, proj, "keep.go", "d/a.go", "d/e/b.go")
	ws, _ := ideWS(proj)
	until(t, "index", func() bool { return indexHas(ws, "d/e/b.go") })
	_, cancel := ws.Subscribe([]string{"d/e"})
	if err := os.Rename(filepath.Join(proj, "d"), filepath.Join(t.TempDir(), "d")); err != nil {
		t.Fatal(err)
	}
	until(t, "fantômes dans /v1/fs/index", func() bool {
		_, _, raw := call(t, handler, "GET", "/v1/fs/index?dir="+proj, "")
		return !strings.Contains(raw, `"d/`) && strings.Contains(raw, "keep.go")
	})
	cancel()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.idxDirs["d"] || ws.idxDirs["d/e"] {
		t.Fatal("dossiers retirés toujours comptés")
	}
	for _, localPath := range ws.watcher.WatchList() {
		if localPath == filepath.Join(proj, "d") || strings.HasPrefix(localPath, filepath.Join(proj, "d")+"/") {
			t.Fatal("veille gardée :", localPath)
		}
	}
}

func TestFsAuditBytesNotRunes(t *testing.T) {
	auditRig := newAuditRig(t)
	if counter := auditRig.post("save", map[string]any{"path": "u.txt", "content": "ééé"}); counter != 200 {
		t.Fatal(counter)
	}
	auditRig.want(t, "create "+filepath.Base(auditRig.proj)+":u.txt 6")
}

func TestFsTrashPurgeWithoutFrom(t *testing.T) {
	auditRig := newAuditRig(t)
	mk(t, auditRig.proj, "f.txt")
	if counter := auditRig.post("rm", map[string]any{"path": "f.txt"}); counter != 200 {
		t.Fatal(counter)
	}
	ents, _ := os.ReadDir(filepath.Join(auditRig.proj, ideTrash))
	var id string
	for _, de := range ents {
		if de.IsDir() {
			id = de.Name()
		}
	}
	os.Remove(filepath.Join(auditRig.proj, ideTrash, id, "from"))
	if counter := auditRig.post("purge", map[string]any{"id": id}); counter != 200 {
		t.Fatal(counter)
	}
	if _, err := os.Stat(filepath.Join(auditRig.proj, ideTrash, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("gardé", err)
	}
	body := filepath.Base(auditRig.proj)
	auditRig.want(t, "rm "+body+":f.txt", "purge "+body+":"+path.Join(ideTrash, id))
}

// Sans veille (sondage), l'index garde a.go ; remplacé ensuite par un lien, il ne doit plus être servi.
func TestFsFilesSwappedForSymlink(t *testing.T) {
	handler, _, proj := web(t)
	wsTest(t)
	old := ideNewWatcher
	ideNewWatcher = func() (*fsnotify.Watcher, error) { return nil, errors.New("sans inotify") }
	t.Cleanup(func() { ideNewWatcher = old })
	mk(t, proj, "a.go")
	ws, _ := ideWS(proj)
	until(t, "index", func() bool { return indexHas(ws, "a.go") })
	// cible interne : os.Root suivrait le lien, seuls ideLstat et SameFile l'arrêtent
	mk(t, proj, ".env")
	os.WriteFile(filepath.Join(proj, ".env"), []byte("SECRET"), 0o644)
	os.Remove(filepath.Join(proj, "a.go"))
	if err := os.Symlink(".env", filepath.Join(proj, "a.go")); err != nil {
		t.Fatal(err)
	}
	if !indexHas(ws, "a.go") {
		t.Fatal("préalable : l'index devait garder a.go")
	}
	_, lookup, raw := fsop(t, handler, "files", map[string]any{"dir": proj, "paths": []string{"a.go"}})
	if len(lookup["files"].([]any)) != 0 || strings.Contains(raw, "SECRET") {
		t.Fatal(raw)
	}
}

func FuzzIdeRel(fuzzer *testing.F) {
	for _, text := range []string{"a/b", "", ".", "..", "a/../b", "/x", "a\\b", "a\x00", "./a//b/", "...", "a/./.."} {
		fuzzer.Add(text, true)
		fuzzer.Add(text, false)
	}
	fuzzer.Fuzz(func(t *testing.T, in string, allowRoot bool) {
		got, err := ideRel(in, allowRoot)
		if err != nil {
			return
		}
		if strings.HasPrefix(got, "/") || strings.ContainsAny(got, "\x00\\") || got != path.Clean(got) {
			t.Fatalf("%q -> %q", in, got)
		}
		for _, text := range strings.Split(got, "/") {
			if text == ".." {
				t.Fatalf("%q -> %q", in, got)
			}
		}
		if got == "." && !allowRoot {
			t.Fatalf("%q -> racine", in)
		}
		if again, err := ideRel(got, allowRoot); err != nil || again != got {
			t.Fatalf("%q -> %q -> %q %v", in, got, again, err)
		}
	})
}
