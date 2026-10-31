package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func etagOf(t *testing.T, handler http.Handler, dir, path string) string {
	t.Helper()
	_, lookup, raw := call(t, handler, "GET", fsq("/v1/fs/entries/content", dir, path), "")
	entry, _ := lookup["etag"].(string)
	if entry == "" {
		t.Fatal(raw)
	}
	return entry
}

func readF(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFsSaveAtomicAndMode(t *testing.T) {
	handler, _, proj := web(t)
	filePath := filepath.Join(proj, "a.sh")
	os.WriteFile(filePath, []byte("old"), 0o600)
	os.Chmod(filePath, 0o600)
	entry := etagOf(t, handler, proj, "a.sh")
	code, lookup, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.sh", "content": "new", "etag": entry})
	if code != 200 || lookup["etag"] == "" {
		t.Fatal(code, raw)
	}
	fi, _ := os.Stat(filePath)
	if readF(t, filePath) != "new" || fi.Mode().Perm() != 0o600 {
		t.Fatal(readF(t, filePath), fi.Mode())
	}
	if lookup["etag"] != etagOf(t, handler, proj, "a.sh") {
		t.Fatal("returned etag differs from GET")
	}
	ents, _ := os.ReadDir(proj)
	if len(ents) != 1 {
		t.Fatal("leftover", ents)
	}
}

func TestFsSaveConflictChanged(t *testing.T) {
	handler, _, proj := web(t)
	filePath := filepath.Join(proj, "a.txt")
	os.WriteFile(filePath, []byte("v1"), 0o644)
	entry := etagOf(t, handler, proj, "a.txt")
	os.WriteFile(filePath, []byte("v2-longer"), 0o644)
	code, lookup, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": entry})
	if code != 409 || errCode(lookup) != "conflict" {
		t.Fatal(code, raw)
	}
	if readF(t, filePath) != "v2-longer" {
		t.Fatal("disk modified")
	}
	ee, _ := lookup["error"].(map[string]any)
	if ee["etag"] != etagOf(t, handler, proj, "a.txt") || ee["deleted"] != false {
		t.Fatal("conflict must carry the disk etag", raw)
	}
}

func TestFsSaveConflictDeleted(t *testing.T) {
	handler, _, proj := web(t)
	filePath := filepath.Join(proj, "a.txt")
	os.WriteFile(filePath, []byte("v1"), 0o644)
	entry := etagOf(t, handler, proj, "a.txt")
	os.Remove(filePath)
	code, lookup, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": entry})
	if code != 409 || errCode(lookup) != "deleted" {
		t.Fatal(code, raw)
	}
	if ee, _ := lookup["error"].(map[string]any); ee["etag"] != "" || ee["deleted"] != true {
		t.Fatal("deleted flag", raw)
	}
	if _, err := os.Stat(filePath); err == nil {
		t.Fatal("recreated")
	}
}

func TestFsSaveForce(t *testing.T) {
	handler, _, proj := web(t)
	filePath := filepath.Join(proj, "a.txt")
	os.WriteFile(filePath, []byte("v1"), 0o644)
	code, _, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "forced", "etag": "stale", "force": true})
	if code != 200 || readF(t, filePath) != "forced" {
		t.Fatal(code, raw)
	}
	os.Remove(filePath)
	code, _, raw = fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "back", "etag": "stale", "force": true})
	if code != 200 || readF(t, filePath) != "back" {
		t.Fatal(code, raw)
	}
}

func TestFsSaveCreate(t *testing.T) {
	handler, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "d"), 0o755)
	code, lookup, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "d/n.txt", "content": "hi", "etag": ""})
	if code != 200 || lookup["etag"] == "" {
		t.Fatal(code, raw)
	}
	fi, _ := os.Stat(filepath.Join(proj, "d", "n.txt"))
	if readF(t, filepath.Join(proj, "d", "n.txt")) != "hi" || fi.Mode().Perm() != 0o644 {
		t.Fatal(fi.Mode())
	}
	// existing file with an empty etag is a conflict, not an overwrite
	code, lookup, _ = fsop(t, handler, "save", map[string]any{"dir": proj, "path": "d/n.txt", "content": "x", "etag": ""})
	if code != 409 || errCode(lookup) != "conflict" {
		t.Fatal(code, lookup)
	}
}

func TestFsSaveCRLFExact(t *testing.T) {
	handler, _, proj := web(t)
	fsop(t, handler, "save", map[string]any{"dir": proj, "path": "w.txt", "content": "a\r\nb\r\n", "etag": ""})
	if readF(t, filepath.Join(proj, "w.txt")) != "a\r\nb\r\n" {
		t.Fatalf("%q", readF(t, filepath.Join(proj, "w.txt")))
	}
}

func TestFsSaveRefusesSymlink(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "real.txt"), []byte("keep"), 0o644)
	os.Symlink("real.txt", filepath.Join(proj, "lnk"))
	code, _, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "lnk", "content": "x", "force": true})
	if code != 403 || readF(t, filepath.Join(proj, "real.txt")) != "keep" {
		t.Fatal(code, raw)
	}
}

func TestFsSaveRefusesSymlinkedParent(t *testing.T) {
	handler, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "real"), 0o755)
	os.Symlink("real", filepath.Join(proj, "lnk"))
	code, _, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "lnk/f.txt", "content": "x", "etag": ""})
	if code != 403 {
		t.Fatal(code, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, "real", "f.txt")); err == nil {
		t.Fatal("written through link")
	}
}

func TestFsSaveTooBig(t *testing.T) {
	handler, _, proj := web(t)
	code, _, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "b.txt", "content": strings.Repeat("a", ideMaxFile+1), "etag": ""})
	if code != 413 {
		t.Fatal(code, raw[:min(len(raw), 200)])
	}
	code, _, _ = fsop(t, handler, "save", map[string]any{"dir": proj, "path": "b.txt", "content": strings.Repeat("a", ideMaxFile+(100<<10)), "etag": ""})
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
	handler, _, proj := web(t)
	dir := filepath.Join(proj, "ro")
	os.Mkdir(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("keep"), 0o644)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	code, lookup, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "ro/f.txt", "content": "x", "force": true})
	if code == 200 || errCode(lookup) == "" || strings.Contains(raw, proj) {
		t.Fatal(code, raw)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || readF(t, filepath.Join(dir, "f.txt")) != "keep" {
		t.Fatal(ents)
	}
}

func TestFsNewAndRename(t *testing.T) {
	handler, _, proj := web(t)
	if counter, _, raw := fsop(t, handler, "new", map[string]any{"dir": proj, "path": "f.txt", "kind": "file"}); counter != 201 {
		t.Fatal(counter, raw)
	}
	if counter, _, raw := fsop(t, handler, "new", map[string]any{"dir": proj, "path": "sub", "kind": "dir"}); counter != 201 {
		t.Fatal(counter, raw)
	}
	if counter, _, _ := fsop(t, handler, "new", map[string]any{"dir": proj, "path": "f.txt", "kind": "file"}); counter != 409 {
		t.Fatal("exists", counter)
	}
	if counter, _, _ := fsop(t, handler, "new", map[string]any{"dir": proj, "path": "x", "kind": "bogus"}); counter != 400 {
		t.Fatal("kind", counter)
	}
	if counter, _, raw := fsop(t, handler, "rename", map[string]any{"dir": proj, "from": "f.txt", "to": "sub/g.txt"}); counter != 204 {
		t.Fatal(counter, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, "sub", "g.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestFsRenameNoOverwrite(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "a"), []byte("A"), 0o644)
	os.WriteFile(filepath.Join(proj, "b"), []byte("B"), 0o644)
	counter, lookup, raw := fsop(t, handler, "rename", map[string]any{"dir": proj, "from": "a", "to": "b"})
	if counter != 409 || errCode(lookup) != "exists" {
		t.Fatal(counter, raw)
	}
	if readF(t, filepath.Join(proj, "a")) != "A" || readF(t, filepath.Join(proj, "b")) != "B" {
		t.Fatal("changed")
	}
}

func TestFsWriteRefusesSymlinkedParent(t *testing.T) {
	handler, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "real"), 0o755)
	os.WriteFile(filepath.Join(proj, "a"), nil, 0o644)
	os.Symlink("real", filepath.Join(proj, "lnk"))
	if counter, _, _ := fsop(t, handler, "new", map[string]any{"dir": proj, "path": "lnk/n", "kind": "file"}); counter != 403 {
		t.Fatal("new", counter)
	}
	if counter, _, _ := fsop(t, handler, "rename", map[string]any{"dir": proj, "from": "a", "to": "lnk/a"}); counter != 403 {
		t.Fatal("rename to", counter)
	}
	os.WriteFile(filepath.Join(proj, "real", "in"), nil, 0o644)
	if counter, _, _ := fsop(t, handler, "rename", map[string]any{"dir": proj, "from": "lnk/in", "to": "out"}); counter != 403 {
		t.Fatal("rename from", counter)
	}
	if counter, _, _ := fsop(t, handler, "rm", map[string]any{"dir": proj, "path": "lnk/in"}); counter != 403 {
		t.Fatal("rm", counter)
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
	handler, _, proj := web(t)
	if out, err := exec.Command(git, "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal("git", string(out))
	}
	os.MkdirAll(filepath.Join(proj, "d", "e"), 0o755)
	os.WriteFile(filepath.Join(proj, "d", "e", "f.txt"), []byte("x"), 0o644)
	if counter, _, raw := fsop(t, handler, "rm", map[string]any{"dir": proj, "path": "d/e"}); counter != 200 {
		t.Fatal(counter, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, "d", "e")); err == nil {
		t.Fatal("still there")
	}
	if readF(t, filepath.Join(proj, ".gld-trash", ".gitignore")) != "*\n" {
		t.Fatal("gitignore")
	}
	_, lookup, raw := call(t, handler, "GET", "/v1/fs/trash?dir="+proj, "")
	items := lookup["items"].([]any)
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
	fsop(t, handler, "rm", map[string]any{"dir": proj, "path": "z"})
	_, lookup, _ = call(t, handler, "GET", "/v1/fs/trash?dir="+proj, "")
	if len(lookup["items"].([]any)) != 2 {
		t.Fatal(lookup)
	}
}

func TestFsRmRefusals(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "r.txt"), nil, 0o644)
	fsop(t, handler, "rm", map[string]any{"dir": proj, "path": "r.txt"})
	for _, path := range []string{"", ".", ".gld-trash", ".gld-trash/.gitignore", "../x", "absent"} {
		counter, _, raw := fsop(t, handler, "rm", map[string]any{"dir": proj, "path": path})
		if counter == 200 {
			t.Errorf("%q accepted: %s", path, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, ".gld-trash")); err != nil {
		t.Fatal("trash gone")
	}
}

func trashOne(t *testing.T, handler http.Handler, proj, path string) string {
	t.Helper()
	if counter, _, raw := fsop(t, handler, "rm", map[string]any{"dir": proj, "path": path}); counter != 200 {
		t.Fatal(counter, raw)
	}
	_, lookup, raw := call(t, handler, "GET", "/v1/fs/trash?dir="+proj, "")
	items := lookup["items"].([]any)
	if len(items) == 0 {
		t.Fatal(raw)
	}
	var id string
	for _, value := range items {
		lookup := value.(map[string]any)
		if lookup["path"] == path {
			id = lookup["id"].(string)
		}
	}
	if id == "" {
		t.Fatal(raw)
	}
	return id
}

func TestFsTrashRestore(t *testing.T) {
	handler, _, proj := web(t)
	os.MkdirAll(filepath.Join(proj, "deep", "er"), 0o755)
	os.WriteFile(filepath.Join(proj, "deep", "er", "f.txt"), []byte("x"), 0o644)
	id := trashOne(t, handler, proj, "deep/er/f.txt")
	os.RemoveAll(filepath.Join(proj, "deep"))
	if counter, _, raw := fsop(t, handler, "restore", map[string]any{"dir": proj, "id": id}); counter != 204 {
		t.Fatal(counter, raw)
	}
	if readF(t, filepath.Join(proj, "deep", "er", "f.txt")) != "x" {
		t.Fatal("not restored")
	}
	if _, err := os.Stat(filepath.Join(proj, ".gld-trash", id)); err == nil {
		t.Fatal("trash entry kept")
	}
}

func TestFsTrashRestoreRefusesOverwrite(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f.txt"), []byte("old"), 0o644)
	id := trashOne(t, handler, proj, "f.txt")
	os.WriteFile(filepath.Join(proj, "f.txt"), []byte("new"), 0o644)
	counter, lookup, raw := fsop(t, handler, "restore", map[string]any{"dir": proj, "id": id})
	if counter != 409 || errCode(lookup) != "exists" {
		t.Fatal(counter, raw)
	}
	if readF(t, filepath.Join(proj, "f.txt")) != "new" || readF(t, filepath.Join(proj, ".gld-trash", id, "item")) != "old" {
		t.Fatal("changed")
	}
}

func TestFsTrashPurge(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f.txt"), nil, 0o644)
	id := trashOne(t, handler, proj, "f.txt")
	if counter, _, raw := fsop(t, handler, "purge", map[string]any{"dir": proj, "id": id}); counter != 200 {
		t.Fatal(counter, raw)
	}
	if _, err := os.Stat(filepath.Join(proj, ".gld-trash", id)); err == nil {
		t.Fatal("kept")
	}
	_, lookup, _ := call(t, handler, "GET", "/v1/fs/trash?dir="+proj, "")
	if len(lookup["items"].([]any)) != 0 {
		t.Fatal(lookup)
	}
}

func TestFsTrashPurgeRejectsBadID(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "keep.txt"), []byte("k"), 0o644)
	trashOne(t, handler, proj, "keep.txt")
	os.WriteFile(filepath.Join(proj, "victim"), []byte("v"), 0o644)
	for _, id := range []string{"../victim", "..", ".", "", "1/2", "12a", ".gitignore"} {
		for _, raw := range []string{"purge", "restore"} {
			// le mux normalise « . » et « .. » avant le handler (redirection ou 404) : aucun ne doit réussir
			if counter, _, _ := fsop(t, handler, raw, map[string]any{"dir": proj, "id": id}); counter != 400 && counter != 404 && counter != 307 {
				t.Errorf("%s %q: %d", raw, id, counter)
			}
		}
	}
	if readF(t, filepath.Join(proj, "victim")) != "v" {
		t.Fatal("victim touched")
	}
	if counter, _, _ := fsop(t, handler, "purge", map[string]any{"dir": proj, "id": "123"}); counter != 404 {
		t.Fatal("unknown id", counter)
	}
}

func TestFsTrashListEmpty(t *testing.T) {
	handler, _, proj := web(t)
	counter, lookup, raw := call(t, handler, "GET", "/v1/fs/trash?dir="+proj, "")
	if counter != 200 || len(lookup["items"].([]any)) != 0 {
		t.Fatal(counter, raw)
	}
}

func TestFsWriteRequiresFsWrite(t *testing.T) {
	gateRig := newGateRig(t)
	ro := gateRig.user(t, "lecteur", false)
	gateRig.grant(t, ro, "", RightFsRead)
	rw := gateRig.user(t, "ecrivain", false)
	gateRig.grant(t, rw, "", RightFsWrite)
	for _, path := range []string{"PUT /v1/fs/entries/content", "POST /v1/fs/entries", "PATCH /v1/fs/entries", "DELETE /v1/fs/entries", "PATCH /v1/fs/trash/1", "DELETE /v1/fs/trash"} {
		message, rawURL, _ := strings.Cut(path, " ")
		if recorder := gateRig.do(message, rawURL, &ro, "", "{}"); recorder.Code != 403 {
			t.Errorf("%s avec fs_read seul : %d", path, recorder.Code)
		}
		if recorder := gateRig.do(message, rawURL, &rw, "", "{}"); recorder.Code != 200 || !reached(recorder) {
			t.Errorf("%s avec fs_write : %d", path, recorder.Code)
		}
	}
	if recorder := gateRig.do("GET", "/v1/fs/trash", &ro, "", ""); recorder.Code != 200 || !reached(recorder) {
		t.Errorf("/v1/fs/trash avec fs_read : %d", recorder.Code)
	}
}

func TestFsSaveRefusesTrash(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f"), nil, 0o644)
	id := trashOne(t, handler, proj, "f")
	for _, path := range []string{".gld-trash/" + id + "/from", ".gld-trash/.gitignore", ".gld-trash/new"} {
		if counter, _, _ := fsop(t, handler, "save", map[string]any{"dir": proj, "path": path, "content": "x", "force": true}); counter != 400 {
			t.Errorf("%s: %d", path, counter)
		}
	}
	if readF(t, filepath.Join(proj, ".gld-trash", id, "from")) != "f" || readF(t, filepath.Join(proj, ".gld-trash", ".gitignore")) != "*\n" {
		t.Fatal("trash modified")
	}
}

func TestFsSaveJSONEscapedBodyAtLimit(t *testing.T) {
	handler, _, proj := web(t)
	body := strings.Repeat("\n\"", ideMaxFile/2)
	if counter, _, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "e.txt", "content": body, "etag": ""}); counter != 200 {
		t.Fatal(counter, raw[:min(len(raw), 200)])
	}
	if readF(t, filepath.Join(proj, "e.txt")) != body {
		t.Fatal("content differs")
	}
	if counter, _, _ := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "e2.txt", "content": body + "x", "etag": ""}); counter != 413 {
		t.Fatal(counter)
	}
}

func TestFsSaveEtagRecheckBeforeRename(t *testing.T) {
	handler, _, proj := web(t)
	filePath := filepath.Join(proj, "a.txt")
	os.WriteFile(filePath, []byte("v1"), 0o644)
	entry := etagOf(t, handler, proj, "a.txt")
	t.Cleanup(func() { ideBeforeRename = func() {} })
	ideBeforeRename = func() { os.WriteFile(filePath, []byte("agent-wrote-this"), 0o644) }
	counter, lookup, raw := fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": entry})
	if counter != 409 || errCode(lookup) != "conflict" {
		t.Fatal(counter, raw)
	}
	if readF(t, filePath) != "agent-wrote-this" {
		t.Fatal("overwritten")
	}
	// deleted in the window
	os.WriteFile(filePath, []byte("v1"), 0o644)
	entry = etagOf(t, handler, proj, "a.txt")
	ideBeforeRename = func() { os.Remove(filePath) }
	counter, lookup, _ = fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": entry})
	if counter != 409 || errCode(lookup) != "deleted" {
		t.Fatal(counter, lookup)
	}
	if _, err := os.Stat(filePath); err == nil {
		t.Fatal("recreated")
	}
	// creation raced by a new file
	ideBeforeRename = func() { os.WriteFile(filePath, []byte("theirs"), 0o644) }
	counter, lookup, _ = fsop(t, handler, "save", map[string]any{"dir": proj, "path": "a.txt", "content": "mine", "etag": ""})
	if counter != 409 || errCode(lookup) != "conflict" || readF(t, filePath) != "theirs" {
		t.Fatal(counter, lookup)
	}
	ents, _ := os.ReadDir(proj)
	for _, value := range ents {
		if strings.HasPrefix(value.Name(), ".gld-tmp-") {
			t.Fatal("temp left", value.Name())
		}
	}
}

func TestFsTrashRestoreTamperedFrom(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f"), []byte("x"), 0o644)
	id := trashOne(t, handler, proj, "f")
	fromF := filepath.Join(proj, ".gld-trash", id, "from")
	for _, bad := range []string{"../x", "/etc/x", ".gld-trash/foo", ""} {
		os.WriteFile(fromF, []byte(bad), 0o644)
		if counter, _, raw := fsop(t, handler, "restore", map[string]any{"dir": proj, "id": id}); counter != 400 {
			t.Errorf("%q: %d %s", bad, counter, raw)
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
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "f"), []byte("x"), 0o644)
	id := trashOne(t, handler, proj, "f")
	base := filepath.Join(proj, ".gld-trash", id)

	// symlinked from
	os.Rename(filepath.Join(base, "from"), filepath.Join(proj, "realfrom"))
	os.Symlink("../../realfrom", filepath.Join(base, "from"))
	if counter, _, _ := fsop(t, handler, "restore", map[string]any{"dir": proj, "id": id}); counter != 403 {
		t.Fatal("from", counter)
	}
	os.Remove(filepath.Join(base, "from"))
	os.Rename(filepath.Join(proj, "realfrom"), filepath.Join(base, "from"))

	// symlinked item
	os.Rename(filepath.Join(base, "item"), filepath.Join(proj, "realitem"))
	os.Symlink("../../realitem", filepath.Join(base, "item"))
	if counter, _, _ := fsop(t, handler, "restore", map[string]any{"dir": proj, "id": id}); counter != 403 {
		t.Fatal("item", counter)
	}
	if _, err := os.Stat(filepath.Join(proj, "f")); err == nil {
		t.Fatal("restored through link")
	}
	os.Remove(filepath.Join(base, "item"))
	os.Rename(filepath.Join(proj, "realitem"), filepath.Join(base, "item"))

	// symlinked id
	os.Rename(base, filepath.Join(proj, "realid"))
	os.Symlink("../realid", base)
	if counter, _, _ := fsop(t, handler, "restore", map[string]any{"dir": proj, "id": id}); counter != 403 {
		t.Fatal("id", counter)
	}
	if _, err := os.Stat(filepath.Join(proj, "f")); err == nil {
		t.Fatal("restored through link")
	}
	if readF(t, filepath.Join(proj, "realid", "item")) != "x" {
		t.Fatal("moved")
	}
}

func TestIdeCodePathCasse(t *testing.T) {
	for _, path := range []string{".git/config", ".GIT/config", "a/.Claude/settings.json", ".git./config", ".mcp.json ", "GIT~1/config", "x/CLAUDE~1/hooks", ".mcp.json::$DATA", ".gi\u200ct/config", "\ufeff.claude/settings.json", ".gitmodule\u017f"} {
		if !ideCodePath(path) {
			t.Errorf("%q non reconnu", path)
		}
	}
	for _, path := range []string{"src/git.go", "gitignore", "claude.md", "docs/.github/x"} {
		if ideCodePath(path) {
			t.Errorf("%q refusé à tort", path)
		}
	}
}
