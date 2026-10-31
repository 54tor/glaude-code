package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mk(t *testing.T, base string, files ...string) {
	t.Helper()
	for _, filePath := range files {
		path := filepath.Join(base, filePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func build(t *testing.T, dir string, lim ideLimits) ([]string, bool) {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	es, partial := ideBuildIndex(root, lim)
	var out []string
	for _, ideIndexEntry := range es {
		out = append(out, ideIndexEntry.Path)
	}
	return out, partial
}

func setLimits(t *testing.T, localIdeLimits ideLimits) {
	old := ideIndexLimits
	ideIndexLimits = localIdeLimits
	t.Cleanup(func() { ideIndexLimits = old })
}

var big = ideLimits{20000, 12, time.Minute}

func TestIdeIgnored(t *testing.T) {
	for _, name := range []string{".git", ".env", ".gld-trash", "vendor", "node_modules"} {
		if !ideIgnored(name) {
			t.Error(name)
		}
	}
	for _, name := range []string{"src", "main.go"} {
		if ideIgnored(name) {
			t.Error(name)
		}
	}
}

func TestIndexSkipsIgnoredAtAnyDepth(t *testing.T) {
	dir := t.TempDir()
	mk(t, dir, "a/b.ts", "a/node_modules/x.js", "a/.cache/y", "a/vendor/z", ".gld-trash/1/from", ".env", "top.go")
	got, partial := build(t, dir, big)
	if strings.Join(got, ",") != "a/b.ts,top.go" || partial {
		t.Fatal(got, partial)
	}
}

func TestIndexIgnoredFileAndDirNamedLikeDeps(t *testing.T) {
	dir := t.TempDir()
	mk(t, dir, "a/vendor", "a/keep.txt", "a/b/c/node_modules/x")
	got, _ := build(t, dir, big)
	if strings.Join(got, ",") != "a/keep.txt" {
		t.Fatal(got)
	}
}

func TestIndexSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	mk(t, dir, "real/f.txt", "plain.txt")
	os.Symlink("real", filepath.Join(dir, "lnkdir"))
	os.Symlink("plain.txt", filepath.Join(dir, "lnkfile"))
	got, _ := build(t, dir, big)
	if strings.Join(got, ",") != "plain.txt,real/f.txt" {
		t.Fatal(got)
	}
}

func TestIndexEmptyProject(t *testing.T) {
	got, partial := build(t, t.TempDir(), big)
	if len(got) != 0 || partial {
		t.Fatal(got, partial)
	}
}

func TestIndexUnreadableSubdirSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lit tout")
	}
	dir := t.TempDir()
	mk(t, dir, "ok/a.txt", "locked/b.txt", "z.txt")
	lk := filepath.Join(dir, "locked")
	os.Chmod(lk, 0)
	t.Cleanup(func() { os.Chmod(lk, 0o755) })
	got, partial := build(t, dir, big)
	if strings.Join(got, ",") != "ok/a.txt,z.txt" || partial {
		t.Fatal(got, partial)
	}
}

func TestIndexPartialOnEntries(t *testing.T) {
	dir := t.TempDir()
	mk(t, dir, "a", "b", "c", "d")
	got, partial := build(t, dir, ideLimits{2, 12, time.Minute})
	if len(got) != 2 || !partial {
		t.Fatal(got, partial)
	}
}

func TestIndexPartialOnDepth(t *testing.T) {
	dir := t.TempDir()
	mk(t, dir, "top", "a/b/deep.txt", "a/mid.txt")
	got, partial := build(t, dir, ideLimits{100, 1, time.Minute})
	if strings.Join(got, ",") != "a/mid.txt,top" || !partial {
		t.Fatal(got, partial)
	}
}

func TestIndexPartialOnTime(t *testing.T) {
	dir := t.TempDir()
	mk(t, dir, "a/b.txt", "c.txt")
	_, partial := build(t, dir, ideLimits{100, 12, time.Nanosecond})
	if !partial {
		t.Fatal("pas partiel")
	}
}

func TestFsIndexEndpoint(t *testing.T) {
	handler, _, proj := web(t)
	mk(t, proj, "src/a.go", "node_modules/x.js", ".env")
	var lookup map[string]any
	for index := 0; index < 200; index++ {
		_, lookup, _ = call(t, handler, "GET", "/v1/fs/index?dir="+proj, "")
		if lookup["state"] != "building" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fs := lookup["files"].([]any)
	if lookup["state"] != "ready" || len(fs) != 1 || fs[0].(map[string]any)["path"] != "src/a.go" {
		t.Fatal(lookup)
	}
	if counter, _, _ := call(t, handler, "GET", "/v1/fs/index?dir=/nope", ""); counter != 404 {
		t.Fatal(counter)
	}
}

func TestIndexSingleBuild(t *testing.T) {
	handler, _, proj := web(t)
	mk(t, proj, "a.txt")
	var localInt32 atomic.Int32
	old := ideBuildHook
	ideBuildHook = func() { localInt32.Add(1); time.Sleep(50 * time.Millisecond) }
	t.Cleanup(func() { ideBuildHook = old })
	var wg sync.WaitGroup
	for index := 0; index < 10; index++ {
		wg.Add(1)
		go func() { defer wg.Done(); call(t, handler, "GET", "/v1/fs/index?dir="+proj, "") }()
	}
	wg.Wait()
	time.Sleep(150 * time.Millisecond)
	call(t, handler, "GET", "/v1/fs/index?dir="+proj, "")
	if localInt32.Load() != 1 {
		t.Fatal("constructions:", localInt32.Load())
	}
}

func TestFsFilesOnlyIndexed(t *testing.T) {
	handler, _, proj := web(t)
	mk(t, proj, "a.go", ".env", "node_modules/x.js")
	os.WriteFile(filepath.Join(proj, "bin.dat"), []byte{0, 1, 2}, 0o644)
	os.WriteFile(filepath.Join(proj, "huge.txt"), []byte(strings.Repeat("a", ideFeedMax+1)), 0o644)
	for index := 0; index < 200; index++ {
		_, lookup, _ := call(t, handler, "GET", "/v1/fs/index?dir="+proj, "")
		if lookup["state"] != "building" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	counter, localLookup, raw := fsop(t, handler, "files", map[string]any{"dir": proj, "paths": []string{"a.go", ".env", "node_modules/x.js", "../x", "bin.dat", "huge.txt", "absent"}})
	fs := localLookup["files"].([]any)
	if counter != 200 || len(fs) != 1 || fs[0].(map[string]any)["path"] != "a.go" || fs[0].(map[string]any)["content"] != "x" {
		t.Fatal(counter, raw)
	}
}

func TestFsFilesBudget(t *testing.T) {
	handler, _, proj := web(t)
	var paths []string
	for index := 0; index < ideFeedPaths+20; index++ {
		name := fmt.Sprintf("f%04d", index)
		mk(t, proj, name)
		paths = append(paths, name)
	}
	for index := 0; index < 200; index++ {
		_, lookup, _ := call(t, handler, "GET", "/v1/fs/index?dir="+proj, "")
		if lookup["state"] != "building" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, localLookup, _ := fsop(t, handler, "files", map[string]any{"dir": proj, "paths": paths})
	if len(localLookup["files"].([]any)) != ideFeedPaths {
		t.Fatal(len(localLookup["files"].([]any)))
	}
}

func TestFsIndexRequiresFsRead(t *testing.T) {
	gateRig := newGateRig(t)
	no := gateRig.user(t, "sans", false)
	gateRig.grant(t, no, "", RightMemRead)
	yes := gateRig.user(t, "avec", false)
	gateRig.grant(t, yes, "", RightFsRead)
	for _, values := range [][2]string{{"GET", "/v1/fs/index"}, {"POST", "/v1/fs/files"}} {
		if recorder := gateRig.do(values[0], values[1], &no, "", "{}"); recorder.Code != 403 {
			t.Errorf("%s sans fs_read : %d", values[1], recorder.Code)
		}
		if recorder := gateRig.do(values[0], values[1], &yes, "", "{}"); recorder.Code != 200 || !reached(recorder) {
			t.Errorf("%s avec fs_read : %d", values[1], recorder.Code)
		}
	}
}
