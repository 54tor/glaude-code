package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func (a *auditRig) post(op string, body map[string]any) int {
	body["dir"] = a.proj
	return a.fsDo(op, &a.u, body).Code
}

func (a *auditRig) fsDo(op string, u *adminUser, body map[string]any) *httptest.ResponseRecorder {
	m, target, b := fsReq(op, body)
	if b == nil {
		return a.do(m, target, u, nil)
	}
	return a.do(m, target, u, b)
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
	if c := a.post("save", map[string]any{"path": "a.txt", "content": "SECRET-CONTENT"}); c != 200 {
		t.Fatal(c)
	}
	a.want(t, "create "+b+":a.txt 14")
	fi, _ := os.Lstat(filepath.Join(a.proj, "a.txt"))
	if c := a.post("save", map[string]any{"path": "a.txt", "content": "xy", "etag": ideEtag(fi)}); c != 200 {
		t.Fatal(c)
	}
	if c := a.post("new", map[string]any{"path": "d", "kind": "dir"}); c != 201 {
		t.Fatal(c)
	}
	if c := a.post("rename", map[string]any{"from": "a.txt", "to": "d/b.txt"}); c != 204 {
		t.Fatal(c)
	}
	if c := a.post("rm", map[string]any{"path": "d/b.txt"}); c != 200 {
		t.Fatal(c)
	}
	ents, _ := os.ReadDir(filepath.Join(a.proj, ideTrash))
	var id string
	for _, de := range ents {
		if de.IsDir() {
			id = de.Name()
		}
	}
	if c := a.post("restore", map[string]any{"id": id}); c != 204 {
		t.Fatal(c)
	}
	a.post("rm", map[string]any{"path": "d/b.txt"})
	ents, _ = os.ReadDir(filepath.Join(a.proj, ideTrash))
	for _, de := range ents {
		if de.IsDir() {
			id = de.Name()
		}
	}
	if c := a.post("purge", map[string]any{"id": id}); c != 200 {
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
	if c := a.post("save", map[string]any{"path": "a.txt", "content": "n", "etag": "stale"}); c != 409 {
		t.Fatal(c)
	}
	a.post("save", map[string]any{"path": "../x", "content": "n"})
	bob := a.login("bob", false)
	a.s.Grant(bob.id, mainHomeID, RightFsRead)
	if c := a.fsDo("save", &bob, map[string]any{"dir": a.proj, "path": "b.txt", "content": "n"}).Code; c != 403 {
		t.Fatal(c)
	}
	a.want(t)
}

func TestFsAuditSanitised(t *testing.T) {
	a := newAuditRig(t)
	if c := a.post("new", map[string]any{"path": "x\ny", "kind": "file"}); c != 201 {
		t.Fatal(c)
	}
	seg := strings.Repeat("é", 80)
	long := strings.TrimSuffix(strings.Repeat(seg+"/", 20), "/")
	os.MkdirAll(filepath.Join(a.proj, long), 0o755)
	if c := a.post("new", map[string]any{"path": long + "/f", "kind": "file"}); c != 201 {
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
	a.post("new", map[string]any{"path": "z", "kind": "file"})
	r := a.ok(a.do("GET", "/v1/admin/events?kind=ide_write", &a.u, nil))["events"].([]any)
	if len(r) != 1 || r[0].(map[string]any)["user"] != "root" {
		t.Fatal(r)
	}
}

func TestFsAuditOpenMode(t *testing.T) {
	h, _, proj := web(t)
	if c, _, raw := fsop(t, h, "new", map[string]any{"dir": proj, "path": "a", "kind": "file"}); c != 201 {
		t.Fatal(c, raw)
	}
}

// Un enfant n'a pas de base de comptes : il renvoie la ligne d'audit nettoyée à sa passerelle, jamais sur un échec.
func TestFsAuditChildHeader(t *testing.T) {
	h, _, proj := web(t)
	childToken = "tok"
	t.Cleanup(func() { childToken = "" })
	post := func(body map[string]any) *httptest.ResponseRecorder {
		m, target, nb := fsReq("new", body)
		b, _ := json.Marshal(nb)
		r := httptest.NewRequest(m, target, bytes.NewReader(b))
		r.Host = testHost
		r.Header.Set(childHeader, "tok")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := post(map[string]any{"dir": proj, "path": "x\ny", "kind": "file"})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	got, err := url.QueryUnescape(w.Header().Get(ideAuditHeader))
	if err != nil || got != "new "+filepath.Base(proj)+":x?y" {
		t.Fatalf("%q %v", w.Header().Get(ideAuditHeader), err)
	}
	if w = post(map[string]any{"dir": proj, "path": "x\ny", "kind": "file"}); w.Code != 409 || w.Header().Get(ideAuditHeader) != "" {
		t.Fatal(w.Code, w.Header())
	}
}

// La passerelle écrit l'évènement relayé par l'enfant pour l'utilisateur de la requête et retire l'en-tête.
func TestFsAuditRelayedFromChild(t *testing.T) {
	a := newAuditRig(t)
	pro := t.TempDir()
	if err := saveHomes([]Home{{Name: "pro", Dir: pro}}); err != nil {
		t.Fatal(err)
	}
	id, _ := homeIDFor("pro")
	if err := a.s.Grant(a.u.id, id, RightFsWrite); err != nil {
		t.Fatal(err)
	}
	var forged atomic.Int32
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ideAuditHeader) != "" {
			forged.Add(1)
		}
		switch m := r.Method + " " + r.URL.Path; m {
		case "PUT /v1/fs/entries/content":
			w.Header().Set(ideAuditHeader, url.QueryEscape("save pro:a.txt 6"))
			w.WriteHeader(200)
		case "POST /v1/fs/entries":
			w.Header().Set(ideAuditHeader, url.QueryEscape("new pro:refusé"))
			w.WriteHeader(409)
		default:
			w.WriteHeader(200)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer child.Close()
	u, _ := url.Parse(child.URL)
	old := homeStart
	t.Cleanup(func() { homeStart = old; homesMu.Lock(); clear(homeProcs); homesMu.Unlock() })
	homeStart = func(h Home) (*homeProc, error) {
		return &homeProc{dir: h.Dir, proxy: childProxy(u, "tok"), done: make(chan struct{})}, nil
	}
	do := func(op string, forge bool) *httptest.ResponseRecorder {
		m, target, nb := fsReq(op, map[string]any{"dir": pro, "path": "a.txt", "content": "ééé", "kind": "file"})
		b, _ := json.Marshal(nb)
		r := httptest.NewRequest(m, target, bytes.NewReader(b))
		r.Host = testHost
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: a.u.cookie})
		r.AddCookie(&http.Cookie{Name: homeCookie, Value: "pro"})
		r.Header.Set(csrfHeader, a.u.csrf)
		if forge {
			r.Header.Set(ideAuditHeader, url.QueryEscape("save pro:forgé 1"))
		}
		w := httptest.NewRecorder()
		a.h.ServeHTTP(w, r)
		return w
	}
	if w := do("save", false); w.Code != 200 || w.Header().Get(ideAuditHeader) != "" {
		t.Fatal(w.Code, w.Header())
	}
	if w := do("new", false); w.Code != 409 || w.Header().Get(ideAuditHeader) != "" {
		t.Fatal(w.Code, w.Header())
	}
	if w := do("rm", true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	a.want(t, "save pro:a.txt 6")
	if forged.Load() != 0 {
		t.Fatal("en-tête forgé relayé à l'enfant")
	}
	var uid int64
	if err := a.s.db.QueryRow(`SELECT user_id FROM auth_events WHERE kind='ide_write'`).Scan(&uid); err != nil || uid != a.u.id {
		t.Fatal(uid, err)
	}
}

func TestFsAuditCleanFormatControls(t *testing.T) {
	for _, r := range []rune{0x202a, 0x202e, 0x2066, 0x2069, 0x200e, 0x200f, 0x061c, 0x2028, 0x2029, 0xfeff} {
		if got := ideAuditClean("a" + string(r) + "b"); got != "a?b" {
			t.Errorf("U+%04X : %q", r, got)
		}
	}
	if got := ideAuditClean("é/ü"); got != "é/ü" {
		t.Fatal(got)
	}
}
