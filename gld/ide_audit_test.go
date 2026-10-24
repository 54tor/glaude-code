package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type auditRig struct {
	*adminRig
	proj string
	u    adminUser
}

func newAuditRig(t *testing.T) *auditRig {
	t.Helper()
	s := testStore(t)
	resetAuth(t, s)
	h, _, proj := web(t)
	g := &adminRig{t: t, s: s, h: withTokenOpen(modeTok, h), clock: testClock(t)}
	u := g.login("root", true)
	if err := s.Grant(u.id, mainHomeID, RightFsWrite); err != nil {
		t.Fatal(err)
	}
	return &auditRig{g, proj, u}
}

func (a *auditRig) post(path string, body map[string]any) int {
	body["dir"] = a.proj
	return a.do("POST", path, &a.u, body).Code
}

func (a *auditRig) events() []string {
	rows, err := a.s.db.Query(`SELECT outcome FROM auth_events WHERE kind='ide_write' ORDER BY rowid`)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		rows.Scan(&o)
		out = append(out, o)
	}
	return out
}

func (a *auditRig) want(t *testing.T, w ...string) {
	t.Helper()
	got := a.events()
	if strings.Join(got, "|") != strings.Join(w, "|") {
		t.Fatalf("événements %q, attendu %q", got, w)
	}
}

func TestFsAuditWrites(t *testing.T) {
	a := newAuditRig(t)
	b := filepath.Base(a.proj)
	if c := a.post("/fs-save", map[string]any{"path": "a.txt", "content": "SECRET-CONTENT"}); c != 200 {
		t.Fatal(c)
	}
	a.want(t, "create "+b+":a.txt 14")
	fi, _ := os.Lstat(filepath.Join(a.proj, "a.txt"))
	if c := a.post("/fs-save", map[string]any{"path": "a.txt", "content": "xy", "etag": ideEtag(fi)}); c != 200 {
		t.Fatal(c)
	}
	if c := a.post("/fs-new", map[string]any{"path": "d", "kind": "dir"}); c != 200 {
		t.Fatal(c)
	}
	if c := a.post("/fs-rename", map[string]any{"from": "a.txt", "to": "d/b.txt"}); c != 200 {
		t.Fatal(c)
	}
	if c := a.post("/fs-rm", map[string]any{"path": "d/b.txt"}); c != 200 {
		t.Fatal(c)
	}
	ents, _ := os.ReadDir(filepath.Join(a.proj, ideTrash))
	var id string
	for _, de := range ents {
		if de.IsDir() {
			id = de.Name()
		}
	}
	if c := a.post("/fs-trash-restore", map[string]any{"id": id}); c != 200 {
		t.Fatal(c)
	}
	a.post("/fs-rm", map[string]any{"path": "d/b.txt"})
	ents, _ = os.ReadDir(filepath.Join(a.proj, ideTrash))
	for _, de := range ents {
		if de.IsDir() {
			id = de.Name()
		}
	}
	if c := a.post("/fs-trash-purge", map[string]any{"id": id}); c != 200 {
		t.Fatal(c)
	}
	got := a.events()
	want := []string{"create " + b + ":a.txt 14", "save " + b + ":a.txt 2", "new " + b + ":d", "rename " + b + ":a.txt -> d/b.txt",
		"rm " + b + ":d/b.txt", "restore " + b + ":d/b.txt", "rm " + b + ":d/b.txt", "purge " + b + ":d/b.txt"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	for _, o := range got {
		if strings.Contains(o, "SECRET") || strings.Contains(o, a.proj) {
			t.Fatal("fuite", o)
		}
	}
}

func TestFsAuditNotOnFailure(t *testing.T) {
	a := newAuditRig(t)
	os.WriteFile(filepath.Join(a.proj, "a.txt"), []byte("old"), 0o644)
	if c := a.post("/fs-save", map[string]any{"path": "a.txt", "content": "n", "etag": "stale"}); c != 409 {
		t.Fatal(c)
	}
	a.post("/fs-save", map[string]any{"path": "../x", "content": "n"})
	bob := a.login("bob", false)
	a.s.Grant(bob.id, mainHomeID, RightFsRead)
	if c := a.do("POST", "/fs-save", &bob, map[string]any{"dir": a.proj, "path": "b.txt", "content": "n"}).Code; c != 403 {
		t.Fatal(c)
	}
	a.want(t)
}

func TestFsAuditSanitised(t *testing.T) {
	a := newAuditRig(t)
	if c := a.post("/fs-new", map[string]any{"path": "x\ny", "kind": "file"}); c != 200 {
		t.Fatal(c)
	}
	seg := strings.Repeat("é", 80)
	long := strings.TrimSuffix(strings.Repeat(seg+"/", 20), "/")
	os.MkdirAll(filepath.Join(a.proj, long), 0o755)
	if c := a.post("/fs-new", map[string]any{"path": long + "/f", "kind": "file"}); c != 200 {
		t.Fatal(c)
	}
	ev := a.events()
	if !strings.HasSuffix(ev[0], ":x?y") {
		t.Fatal(ev[0])
	}
	for _, o := range ev {
		if len(o) > 300 {
			t.Fatal(len(o))
		}
	}
	if len(ev) != 2 || !strings.HasSuffix(ev[1], "…") {
		t.Fatal(ev[1])
	}
}

func TestFsAuditVisibleToAdmin(t *testing.T) {
	a := newAuditRig(t)
	a.post("/fs-new", map[string]any{"path": "z", "kind": "file"})
	r := a.ok(a.do("GET", "/admin/events?kind=ide_write", &a.u, nil))["events"].([]any)
	if len(r) != 1 || r[0].(map[string]any)["user"] != "root" {
		t.Fatal(r)
	}
}

func TestFsAuditOpenMode(t *testing.T) {
	h, _, proj := web(t)
	if c, _, raw := jpost(t, h, "/fs-new", map[string]any{"dir": proj, "path": "a", "kind": "file"}); c != 200 {
		t.Fatal(c, raw)
	}
}
