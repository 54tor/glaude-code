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
	for _, f := range files {
		p := filepath.Join(base, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func build(t *testing.T, dir string, lim ideLimits) ([]string, bool) {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	es, partial := ideBuildIndex(r, lim)
	var out []string
	for _, e := range es {
		out = append(out, e.Path)
	}
	return out, partial
}

func setLimits(t *testing.T, l ideLimits) {
	old := ideIndexLimits
	ideIndexLimits = l
	t.Cleanup(func() { ideIndexLimits = old })
}

var big = ideLimits{20000, 12, time.Minute}

func TestIdeIgnored(t *testing.T) {
	for _, n := range []string{".git", ".env", ".gld-trash", "vendor", "node_modules"} {
		if !ideIgnored(n) {
			t.Error(n)
		}
	}
	for _, n := range []string{"src", "main.go"} {
		if ideIgnored(n) {
			t.Error(n)
		}
	}
}

func TestIndexSkipsIgnoredAtAnyDepth(t *testing.T) {
	d := t.TempDir()
	mk(t, d, "a/b.ts", "a/node_modules/x.js", "a/.cache/y", "a/vendor/z", ".gld-trash/1/from", ".env", "top.go")
	got, partial := build(t, d, big)
	if strings.Join(got, ",") != "a/b.ts,top.go" || partial {
		t.Fatal(got, partial)
	}
}

func TestIndexIgnoredFileAndDirNamedLikeDeps(t *testing.T) {
	d := t.TempDir()
	mk(t, d, "a/vendor", "a/keep.txt", "a/b/c/node_modules/x")
	got, _ := build(t, d, big)
	if strings.Join(got, ",") != "a/keep.txt" {
		t.Fatal(got)
	}
}

func TestIndexSkipsSymlinks(t *testing.T) {
	d := t.TempDir()
	mk(t, d, "real/f.txt", "plain.txt")
	os.Symlink("real", filepath.Join(d, "lnkdir"))
	os.Symlink("plain.txt", filepath.Join(d, "lnkfile"))
	got, _ := build(t, d, big)
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
	d := t.TempDir()
	mk(t, d, "ok/a.txt", "locked/b.txt", "z.txt")
	lk := filepath.Join(d, "locked")
	os.Chmod(lk, 0)
	t.Cleanup(func() { os.Chmod(lk, 0o755) })
	got, partial := build(t, d, big)
	if strings.Join(got, ",") != "ok/a.txt,z.txt" || partial {
		t.Fatal(got, partial)
	}
}

func TestIndexPartialOnEntries(t *testing.T) {
	d := t.TempDir()
	mk(t, d, "a", "b", "c", "d")
	got, partial := build(t, d, ideLimits{2, 12, time.Minute})
	if len(got) != 2 || !partial {
		t.Fatal(got, partial)
	}
}

func TestIndexPartialOnDepth(t *testing.T) {
	d := t.TempDir()
	mk(t, d, "top", "a/b/deep.txt", "a/mid.txt")
	got, partial := build(t, d, ideLimits{100, 1, time.Minute})
	if strings.Join(got, ",") != "a/mid.txt,top" || !partial {
		t.Fatal(got, partial)
	}
}

func TestIndexPartialOnTime(t *testing.T) {
	d := t.TempDir()
	mk(t, d, "a/b.txt", "c.txt")
	_, partial := build(t, d, ideLimits{100, 12, time.Nanosecond})
	if !partial {
		t.Fatal("pas partiel")
	}
}

func TestFsIndexEndpoint(t *testing.T) {
	h, _, proj := web(t)
	mk(t, proj, "src/a.go", "node_modules/x.js", ".env")
	var m map[string]any
	for i := 0; i < 200; i++ {
		_, m, _ = call(t, h, "GET", "/fs-index?dir="+proj, "")
		if m["state"] != "building" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fs := m["files"].([]any)
	if m["state"] != "ready" || len(fs) != 1 || fs[0].(map[string]any)["path"] != "src/a.go" {
		t.Fatal(m)
	}
	if c, _, _ := call(t, h, "GET", "/fs-index?dir=/nope", ""); c != 404 {
		t.Fatal(c)
	}
}

func TestIndexSingleBuild(t *testing.T) {
	h, _, proj := web(t)
	mk(t, proj, "a.txt")
	var n atomic.Int32
	old := ideBuildHook
	ideBuildHook = func() { n.Add(1); time.Sleep(50 * time.Millisecond) }
	t.Cleanup(func() { ideBuildHook = old })
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); call(t, h, "GET", "/fs-index?dir="+proj, "") }()
	}
	wg.Wait()
	time.Sleep(150 * time.Millisecond)
	call(t, h, "GET", "/fs-index?dir="+proj, "")
	if n.Load() != 1 {
		t.Fatal("constructions:", n.Load())
	}
}

func TestFsFilesOnlyIndexed(t *testing.T) {
	h, _, proj := web(t)
	mk(t, proj, "a.go", ".env", "node_modules/x.js")
	os.WriteFile(filepath.Join(proj, "bin.dat"), []byte{0, 1, 2}, 0o644)
	os.WriteFile(filepath.Join(proj, "huge.txt"), []byte(strings.Repeat("a", ideFeedMax+1)), 0o644)
	for i := 0; i < 200; i++ {
		_, m, _ := call(t, h, "GET", "/fs-index?dir="+proj, "")
		if m["state"] != "building" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	c, m, raw := jpost(t, h, "/fs-files", map[string]any{"dir": proj, "paths": []string{"a.go", ".env", "node_modules/x.js", "../x", "bin.dat", "huge.txt", "absent"}})
	fs := m["files"].([]any)
	if c != 200 || len(fs) != 1 || fs[0].(map[string]any)["path"] != "a.go" || fs[0].(map[string]any)["content"] != "x" {
		t.Fatal(c, raw)
	}
}

func TestFsFilesBudget(t *testing.T) {
	h, _, proj := web(t)
	var paths []string
	for i := 0; i < ideFeedPaths+20; i++ {
		n := fmt.Sprintf("f%04d", i)
		mk(t, proj, n)
		paths = append(paths, n)
	}
	for i := 0; i < 200; i++ {
		_, m, _ := call(t, h, "GET", "/fs-index?dir="+proj, "")
		if m["state"] != "building" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, m, _ := jpost(t, h, "/fs-files", map[string]any{"dir": proj, "paths": paths})
	if len(m["files"].([]any)) != ideFeedPaths {
		t.Fatal(len(m["files"].([]any)))
	}
}

func TestFsIndexRequiresFsRead(t *testing.T) {
	g := newGateRig(t)
	no := g.user(t, "sans", false)
	g.grant(t, no, "", RightMemRead)
	yes := g.user(t, "avec", false)
	g.grant(t, yes, "", RightFsRead)
	for _, c := range [][2]string{{"GET", "/fs-index"}, {"POST", "/fs-files"}} {
		if w := g.do(c[0], c[1], &no, "", "{}"); w.Code != 403 {
			t.Errorf("%s sans fs_read : %d", c[1], w.Code)
		}
		if w := g.do(c[0], c[1], &yes, "", "{}"); w.Code != 200 || !reached(w) {
			t.Errorf("%s avec fs_read : %d", c[1], w.Code)
		}
	}
}
