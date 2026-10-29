package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func etagOf(t *testing.T, h http.Handler, dir, p string) string {
	t.Helper()
	_, m, raw := call(t, h, "GET", fsq("/v1/fs/entries/content", dir, p), "")
	e, _ := m["etag"].(string)
	if e == "" {
		t.Fatal(raw)
	}
	return e
}

func readF(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFsSaveAtomicAndMode(t *testing.T) {
	h, _, proj := web(t)
	f := filepath.Join(proj, "a.sh")
	os.WriteFile(f, []byte("old"), 0o600)
	os.Chmod(f, 0o600)
	e := etagOf(t, h, proj, "a.sh")
	code, m, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.sh", "content": "new", "etag": e})
	if code != 200 || m["etag"] == "" {
		t.Fatal(code, raw)
	}
	fi, _ := os.Stat(f)
	if readF(t, f) != "new" || fi.Mode().Perm() != 0o600 {
		t.Fatal(readF(t, f), fi.Mode())
	}
	if m["etag"] != etagOf(t, h, proj, "a.sh") {
		t.Fatal("returned etag differs from GET")
	}
	ents, _ := os.ReadDir(proj)
	if len(ents) != 1 {
		t.Fatal("leftover", ents)
	}
}

func TestFsSaveConflictChanged(t *testing.T) {
	h, _, proj := web(t)
	f := filepath.Join(proj, "a.txt")
	os.WriteFile(f, []byte("v1"), 0o644)
	e := etagOf(t, h, proj, "a.txt")
	os.WriteFile(f, []byte("v2-longer"), 0o644)
	code, m, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": e})
	if code != 409 || errCode(m) != "conflict" {
		t.Fatal(code, raw)
	}
	if readF(t, f) != "v2-longer" {
		t.Fatal("disk modified")
	}
	ee, _ := m["error"].(map[string]any)
	if ee["etag"] != etagOf(t, h, proj, "a.txt") || ee["deleted"] != false {
		t.Fatal("conflict must carry the disk etag", raw)
	}
}

func TestFsSaveConflictDeleted(t *testing.T) {
	h, _, proj := web(t)
	f := filepath.Join(proj, "a.txt")
	os.WriteFile(f, []byte("v1"), 0o644)
	e := etagOf(t, h, proj, "a.txt")
	os.Remove(f)
	code, m, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": e})
	if code != 409 || errCode(m) != "deleted" {
		t.Fatal(code, raw)
	}
	if ee, _ := m["error"].(map[string]any); ee["etag"] != "" || ee["deleted"] != true {
		t.Fatal("deleted flag", raw)
	}
	if _, err := os.Stat(f); err == nil {
		t.Fatal("recreated")
	}
}

func TestFsSaveForce(t *testing.T) {
	h, _, proj := web(t)
	f := filepath.Join(proj, "a.txt")
	os.WriteFile(f, []byte("v1"), 0o644)
	code, _, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "forced", "etag": "stale", "force": true})
	if code != 200 || readF(t, f) != "forced" {
		t.Fatal(code, raw)
	}
	os.Remove(f)
	code, _, raw = fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "back", "etag": "stale", "force": true})
	if code != 200 || readF(t, f) != "back" {
		t.Fatal(code, raw)
	}
}

func TestFsSaveCreate(t *testing.T) {
	h, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "d"), 0o755)
	code, m, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "d/n.txt", "content": "hi", "etag": ""})
	if code != 200 || m["etag"] == "" {
		t.Fatal(code, raw)
	}
	fi, _ := os.Stat(filepath.Join(proj, "d", "n.txt"))
	if readF(t, filepath.Join(proj, "d", "n.txt")) != "hi" || fi.Mode().Perm() != 0o644 {
		t.Fatal(fi.Mode())
	}
	// existing file with an empty etag is a conflict, not an overwrite
	code, m, _ = fsop(t, h, "save", map[string]any{"dir": proj, "path": "d/n.txt", "content": "x", "etag": ""})
	if code != 409 || errCode(m) != "conflict" {
		t.Fatal(code, m)
	}
}

func TestFsSaveCRLFExact(t *testing.T) {
	h, _, proj := web(t)
	fsop(t, h, "save", map[string]any{"dir": proj, "path": "w.txt", "content": "a\r\nb\r\n", "etag": ""})
	if readF(t, filepath.Join(proj, "w.txt")) != "a\r\nb\r\n" {
		t.Fatalf("%q", readF(t, filepath.Join(proj, "w.txt")))
	}
}

func TestFsSaveRefusesSymlink(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "real.txt"), []byte("keep"), 0o644)
	os.Symlink("real.txt", filepath.Join(proj, "lnk"))
	code, _, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "lnk", "content": "x", "force": true})
	if code != 403 || readF(t, filepath.Join(proj, "real.txt")) != "keep" {
		t.Fatal(code, raw)
	}
}

func TestFsSaveRefusesSymlinkedParent(t *testing.T) {
	h, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "real"), 0o755)
	os.Symlink("real", filepath.Join(proj, "lnk"))
	code, _, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "lnk/f.txt", "content": "x", "etag": ""})
	if code != 403 {
		t.Fatal(code, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, "real", "f.txt")); err == nil {
		t.Fatal("written through link")
	}
}

func TestFsSaveTooBig(t *testing.T) {
	h, _, proj := web(t)
	code, _, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "b.txt", "content": strings.Repeat("a", ideMaxFile+1), "etag": ""})
	if code != 413 {
		t.Fatal(code, raw[:min(len(raw), 200)])
	}
	code, _, _ = fsop(t, h, "save", map[string]any{"dir": proj, "path": "b.txt", "content": strings.Repeat("a", ideMaxFile+(100<<10)), "etag": ""})
	if code != 413 {
		t.Fatal("huge body", code)
	}
	if _, err := os.Stat(filepath.Join(proj, "b.txt")); err == nil {
		t.Fatal("written")
	}
}

func TestFsSaveFailureLeavesNoTemp(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	h, _, proj := web(t)
	d := filepath.Join(proj, "ro")
	os.Mkdir(d, 0o755)
	os.WriteFile(filepath.Join(d, "f.txt"), []byte("keep"), 0o644)
	os.Chmod(d, 0o555)
	t.Cleanup(func() { os.Chmod(d, 0o755) })
	code, m, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "ro/f.txt", "content": "x", "force": true})
	if code == 200 || errCode(m) == "" || strings.Contains(raw, proj) {
		t.Fatal(code, raw)
	}
	ents, _ := os.ReadDir(d)
	if len(ents) != 1 || readF(t, filepath.Join(d, "f.txt")) != "keep" {
		t.Fatal(ents)
	}
}

func TestFsNewAndRename(t *testing.T) {
	h, _, proj := web(t)
	if c, _, raw := fsop(t, h, "new", map[string]any{"dir": proj, "path": "f.txt", "kind": "file"}); c != 201 {
		t.Fatal(c, raw)
	}
	if c, _, raw := fsop(t, h, "new", map[string]any{"dir": proj, "path": "sub", "kind": "dir"}); c != 201 {
		t.Fatal(c, raw)
	}
	if c, _, _ := fsop(t, h, "new", map[string]any{"dir": proj, "path": "f.txt", "kind": "file"}); c != 409 {
		t.Fatal("exists", c)
	}
	if c, _, _ := fsop(t, h, "new", map[string]any{"dir": proj, "path": "x", "kind": "bogus"}); c != 400 {
		t.Fatal("kind", c)
	}
	if c, _, raw := fsop(t, h, "rename", map[string]any{"dir": proj, "from": "f.txt", "to": "sub/g.txt"}); c != 204 {
		t.Fatal(c, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, "sub", "g.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestFsRenameNoOverwrite(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "a"), []byte("A"), 0o644)
	os.WriteFile(filepath.Join(proj, "b"), []byte("B"), 0o644)
	c, m, raw := fsop(t, h, "rename", map[string]any{"dir": proj, "from": "a", "to": "b"})
	if c != 409 || errCode(m) != "exists" {
		t.Fatal(c, raw)
	}
	if readF(t, filepath.Join(proj, "a")) != "A" || readF(t, filepath.Join(proj, "b")) != "B" {
		t.Fatal("changed")
	}
}

func TestFsWriteRefusesSymlinkedParent(t *testing.T) {
	h, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "real"), 0o755)
	os.WriteFile(filepath.Join(proj, "a"), nil, 0o644)
	os.Symlink("real", filepath.Join(proj, "lnk"))
	if c, _, _ := fsop(t, h, "new", map[string]any{"dir": proj, "path": "lnk/n", "kind": "file"}); c != 403 {
		t.Fatal("new", c)
	}
	if c, _, _ := fsop(t, h, "rename", map[string]any{"dir": proj, "from": "a", "to": "lnk/a"}); c != 403 {
		t.Fatal("rename to", c)
	}
	os.WriteFile(filepath.Join(proj, "real", "in"), nil, 0o644)
	if c, _, _ := fsop(t, h, "rename", map[string]any{"dir": proj, "from": "lnk/in", "to": "out"}); c != 403 {
		t.Fatal("rename from", c)
	}
	if c, _, _ := fsop(t, h, "rm", map[string]any{"dir": proj, "path": "lnk/in"}); c != 403 {
		t.Fatal("rm", c)
	}
	if _, err := os.Stat(filepath.Join(proj, "real", "in")); err != nil {
		t.Fatal("moved")
	}
}

func TestFsRmMovesToTrash(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git absent")
	}
	h, _, proj := web(t)
	if out, err := exec.Command(git, "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal("git", string(out))
	}
	os.MkdirAll(filepath.Join(proj, "d", "e"), 0o755)
	os.WriteFile(filepath.Join(proj, "d", "e", "f.txt"), []byte("x"), 0o644)
	if c, _, raw := fsop(t, h, "rm", map[string]any{"dir": proj, "path": "d/e"}); c != 200 {
		t.Fatal(c, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, "d", "e")); err == nil {
		t.Fatal("still there")
	}
	if readF(t, filepath.Join(proj, ".gld-trash", ".gitignore")) != "*\n" {
		t.Fatal("gitignore")
	}
	_, m, raw := call(t, h, "GET", "/v1/fs/trash?dir="+proj, "")
	items := m["items"].([]any)
	if len(items) != 1 {
		t.Fatal(raw)
	}
	it := items[0].(map[string]any)
	id := it["id"].(string)
	if it["path"] != "d/e" || it["mtime"].(float64) <= 0 {
		t.Fatal(raw)
	}
	if readF(t, filepath.Join(proj, ".gld-trash", id, "from")) != "d/e" {
		t.Fatal("from")
	}
	if readF(t, filepath.Join(proj, ".gld-trash", id, "item", "f.txt")) != "x" {
		t.Fatal("item")
	}
	out, _ := exec.Command(git, "-C", proj, "status", "--porcelain").Output()
	if strings.Contains(string(out), "gld-trash") {
		t.Fatal(string(out))
	}
	// second deletion keeps the .gitignore and gets its own id
	os.WriteFile(filepath.Join(proj, "z"), nil, 0o644)
	fsop(t, h, "rm", map[string]any{"dir": proj, "path": "z"})
	_, m, _ = call(t, h, "GET", "/v1/fs/trash?dir="+proj, "")
	if len(m["items"].([]any)) != 2 {
		t.Fatal(m)
	}
}

func TestFsRmRefusals(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "r.txt"), nil, 0o644)
	fsop(t, h, "rm", map[string]any{"dir": proj, "path": "r.txt"})
	for _, p := range []string{"", ".", ".gld-trash", ".gld-trash/.gitignore", "../x", "absent"} {
		c, _, raw := fsop(t, h, "rm", map[string]any{"dir": proj, "path": p})
		if c == 200 {
			t.Errorf("%q accepted: %s", p, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, ".gld-trash")); err != nil {
		t.Fatal("trash gone")
	}
}

func trashOne(t *testing.T, h http.Handler, proj, p string) string {
	t.Helper()
	if c, _, raw := fsop(t, h, "rm", map[string]any{"dir": proj, "path": p}); c != 200 {
		t.Fatal(c, raw)
	}
	_, m, raw := call(t, h, "GET", "/v1/fs/trash?dir="+proj, "")
	items := m["items"].([]any)
	if len(items) == 0 {
		t.Fatal(raw)
	}
	var id string
	for _, i := range items {
		x := i.(map[string]any)
		if x["path"] == p {
			id = x["id"].(string)
		}
	}
	if id == "" {
		t.Fatal(raw)
	}
	return id
}

func TestFsTrashRestore(t *testing.T) {
	h, _, proj := web(t)
	os.MkdirAll(filepath.Join(proj, "deep", "er"), 0o755)
	os.WriteFile(filepath.Join(proj, "deep", "er", "f.txt"), []byte("x"), 0o644)
	id := trashOne(t, h, proj, "deep/er/f.txt")
	os.RemoveAll(filepath.Join(proj, "deep"))
	if c, _, raw := fsop(t, h, "restore", map[string]any{"dir": proj, "id": id}); c != 204 {
		t.Fatal(c, raw)
	}
	if readF(t, filepath.Join(proj, "deep", "er", "f.txt")) != "x" {
		t.Fatal("not restored")
	}
	if _, err := os.Stat(filepath.Join(proj, ".gld-trash", id)); err == nil {
		t.Fatal("trash entry kept")
	}
}

func TestFsTrashRestoreRefusesOverwrite(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f.txt"), []byte("old"), 0o644)
	id := trashOne(t, h, proj, "f.txt")
	os.WriteFile(filepath.Join(proj, "f.txt"), []byte("new"), 0o644)
	c, m, raw := fsop(t, h, "restore", map[string]any{"dir": proj, "id": id})
	if c != 409 || errCode(m) != "exists" {
		t.Fatal(c, raw)
	}
	if readF(t, filepath.Join(proj, "f.txt")) != "new" || readF(t, filepath.Join(proj, ".gld-trash", id, "item")) != "old" {
		t.Fatal("changed")
	}
}

func TestFsTrashPurge(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f.txt"), nil, 0o644)
	id := trashOne(t, h, proj, "f.txt")
	if c, _, raw := fsop(t, h, "purge", map[string]any{"dir": proj, "id": id}); c != 204 {
		t.Fatal(c, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, ".gld-trash", id)); err == nil {
		t.Fatal("kept")
	}
	_, m, _ := call(t, h, "GET", "/v1/fs/trash?dir="+proj, "")
	if len(m["items"].([]any)) != 0 {
		t.Fatal(m)
	}
}

func TestFsTrashPurgeRejectsBadID(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "keep.txt"), []byte("k"), 0o644)
	trashOne(t, h, proj, "keep.txt")
	os.WriteFile(filepath.Join(proj, "victim"), []byte("v"), 0o644)
	for _, id := range []string{"../victim", "..", ".", "", "1/2", "12a", ".gitignore"} {
		for _, r := range []string{"purge", "restore"} {
			// le mux normalise « . » et « .. » avant le handler (redirection ou 404) : aucun ne doit réussir
			if c, _, _ := fsop(t, h, r, map[string]any{"dir": proj, "id": id}); c != 400 && c != 404 && c != 307 {
				t.Errorf("%s %q: %d", r, id, c)
			}
		}
	}
	if readF(t, filepath.Join(proj, "victim")) != "v" {
		t.Fatal("victim touched")
	}
	if c, _, _ := fsop(t, h, "purge", map[string]any{"dir": proj, "id": "123"}); c != 404 {
		t.Fatal("unknown id", c)
	}
}

func TestFsTrashListEmpty(t *testing.T) {
	h, _, proj := web(t)
	c, m, raw := call(t, h, "GET", "/v1/fs/trash?dir="+proj, "")
	if c != 200 || len(m["items"].([]any)) != 0 {
		t.Fatal(c, raw)
	}
}

func TestFsWriteRequiresFsWrite(t *testing.T) {
	g := newGateRig(t)
	ro := g.user(t, "lecteur", false)
	g.grant(t, ro, "", RightFsRead)
	rw := g.user(t, "ecrivain", false)
	g.grant(t, rw, "", RightFsWrite)
	for _, p := range []string{"PUT /v1/fs/entries/content", "POST /v1/fs/entries", "PATCH /v1/fs/entries", "DELETE /v1/fs/entries", "PATCH /v1/fs/trash/1", "DELETE /v1/fs/trash"} {
		m, u, _ := strings.Cut(p, " ")
		if w := g.do(m, u, &ro, "", "{}"); w.Code != 403 {
			t.Errorf("%s avec fs_read seul : %d", p, w.Code)
		}
		if w := g.do(m, u, &rw, "", "{}"); w.Code != 200 || !reached(w) {
			t.Errorf("%s avec fs_write : %d", p, w.Code)
		}
	}
	if w := g.do("GET", "/v1/fs/trash", &ro, "", ""); w.Code != 200 || !reached(w) {
		t.Errorf("/v1/fs/trash avec fs_read : %d", w.Code)
	}
}

func TestFsSaveRefusesTrash(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f"), nil, 0o644)
	id := trashOne(t, h, proj, "f")
	for _, p := range []string{".gld-trash/" + id + "/from", ".gld-trash/.gitignore", ".gld-trash/new"} {
		if c, _, _ := fsop(t, h, "save", map[string]any{"dir": proj, "path": p, "content": "x", "force": true}); c != 400 {
			t.Errorf("%s: %d", p, c)
		}
	}
	if readF(t, filepath.Join(proj, ".gld-trash", id, "from")) != "f" || readF(t, filepath.Join(proj, ".gld-trash", ".gitignore")) != "*\n" {
		t.Fatal("trash modified")
	}
}

func TestFsSaveJSONEscapedBodyAtLimit(t *testing.T) {
	h, _, proj := web(t)
	body := strings.Repeat("\n\"", ideMaxFile/2)
	if c, _, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "e.txt", "content": body, "etag": ""}); c != 200 {
		t.Fatal(c, raw[:min(len(raw), 200)])
	}
	if readF(t, filepath.Join(proj, "e.txt")) != body {
		t.Fatal("content differs")
	}
	if c, _, _ := fsop(t, h, "save", map[string]any{"dir": proj, "path": "e2.txt", "content": body + "x", "etag": ""}); c != 413 {
		t.Fatal(c)
	}
}

func TestFsSaveEtagRecheckBeforeRename(t *testing.T) {
	h, _, proj := web(t)
	f := filepath.Join(proj, "a.txt")
	os.WriteFile(f, []byte("v1"), 0o644)
	e := etagOf(t, h, proj, "a.txt")
	t.Cleanup(func() { ideBeforeRename = func() {} })
	ideBeforeRename = func() { os.WriteFile(f, []byte("agent-wrote-this"), 0o644) }
	c, m, raw := fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": e})
	if c != 409 || errCode(m) != "conflict" {
		t.Fatal(c, raw)
	}
	if readF(t, f) != "agent-wrote-this" {
		t.Fatal("overwritten")
	}
	// deleted in the window
	os.WriteFile(f, []byte("v1"), 0o644)
	e = etagOf(t, h, proj, "a.txt")
	ideBeforeRename = func() { os.Remove(f) }
	c, m, _ = fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": e})
	if c != 409 || errCode(m) != "deleted" {
		t.Fatal(c, m)
	}
	if _, err := os.Stat(f); err == nil {
		t.Fatal("recreated")
	}
	// creation raced by a new file
	ideBeforeRename = func() { os.WriteFile(f, []byte("theirs"), 0o644) }
	c, m, _ = fsop(t, h, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": ""})
	if c != 409 || errCode(m) != "conflict" || readF(t, f) != "theirs" {
		t.Fatal(c, m)
	}
	ents, _ := os.ReadDir(proj)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".gld-tmp-") {
			t.Fatal("temp left", e.Name())
		}
	}
}

func TestFsTrashRestoreTamperedFrom(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f"), []byte("x"), 0o644)
	id := trashOne(t, h, proj, "f")
	fromF := filepath.Join(proj, ".gld-trash", id, "from")
	for _, bad := range []string{"../x", "/etc/x", ".gld-trash/foo", ""} {
		os.WriteFile(fromF, []byte(bad), 0o644)
		if c, _, raw := fsop(t, h, "restore", map[string]any{"dir": proj, "id": id}); c != 400 {
			t.Errorf("%q: %d %s", bad, c, raw)
		}
		if readF(t, filepath.Join(proj, ".gld-trash", id, "item")) != "x" {
			t.Fatal("moved")
		}
	}
	if _, err := os.Stat(filepath.Join(proj, ".gld-trash", "foo")); err == nil {
		t.Fatal("created")
	}
}

func TestFsTrashRestoreRefusesSymlinks(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f"), []byte("x"), 0o644)
	id := trashOne(t, h, proj, "f")
	base := filepath.Join(proj, ".gld-trash", id)

	// symlinked from
	os.Rename(filepath.Join(base, "from"), filepath.Join(proj, "realfrom"))
	os.Symlink("../../realfrom", filepath.Join(base, "from"))
	if c, _, _ := fsop(t, h, "restore", map[string]any{"dir": proj, "id": id}); c != 403 {
		t.Fatal("from", c)
	}
	os.Remove(filepath.Join(base, "from"))
	os.Rename(filepath.Join(proj, "realfrom"), filepath.Join(base, "from"))

	// symlinked item
	os.Rename(filepath.Join(base, "item"), filepath.Join(proj, "realitem"))
	os.Symlink("../../realitem", filepath.Join(base, "item"))
	if c, _, _ := fsop(t, h, "restore", map[string]any{"dir": proj, "id": id}); c != 403 {
		t.Fatal("item", c)
	}
	if _, err := os.Stat(filepath.Join(proj, "f")); err == nil {
		t.Fatal("restored through link")
	}
	os.Remove(filepath.Join(base, "item"))
	os.Rename(filepath.Join(proj, "realitem"), filepath.Join(base, "item"))

	// symlinked id
	os.Rename(base, filepath.Join(proj, "realid"))
	os.Symlink("../realid", base)
	if c, _, _ := fsop(t, h, "restore", map[string]any{"dir": proj, "id": id}); c != 403 {
		t.Fatal("id", c)
	}
	if _, err := os.Stat(filepath.Join(proj, "f")); err == nil {
		t.Fatal("restored through link")
	}
	if readF(t, filepath.Join(proj, "realid", "item")) != "x" {
		t.Fatal("moved")
	}
}

func TestIdeCodePathCasse(t *testing.T) {
	for _, p := range []string{".git/config", ".GIT/config", "a/.Claude/settings.json", ".git./config", ".mcp.json ", "GIT~1/config", "x/CLAUDE~1/hooks", ".mcp.json::$DATA", ".gi\u200ct/config", "\ufeff.claude/settings.json", ".gitmodule\u017f"} {
		if !ideCodePath(p) {
			t.Errorf("%q non reconnu", p)
		}
	}
	for _, p := range []string{"src/git.go", "gitignore", "claude.md", "docs/.github/x"} {
		if ideCodePath(p) {
			t.Errorf("%q refusé à tort", p)
		}
	}
}
