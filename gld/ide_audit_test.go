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
	authStore := testStore(t)
	resetAuth(t, authStore)
	handler, _, proj := web(t)
	localAdminRig := &adminRig{t: t, s: authStore, h: withTokenOpen(modeTok, handler), clock: testClock(t)}
	adminUser := localAdminRig.login("root", true)
	if err := authStore.Grant(adminUser.id, mainHomeID, RightFsWrite); err != nil {
		t.Fatal(err)
	}
	return &auditRig{localAdminRig, proj, adminUser}
}

func (localAuditRig *auditRig) post(op string, body map[string]any) int {
	body["dir"] = localAuditRig.proj
	return localAuditRig.fsDo(op, &localAuditRig.u, body).Code
}

func (localAuditRig *auditRig) fsDo(op string, localAdminUser *adminUser, body map[string]any) *httptest.ResponseRecorder {
	message, target, lookup := fsReq(op, body)
	if lookup == nil {
		return localAuditRig.do(message, target, localAdminUser, nil)
	}
	return localAuditRig.do(message, target, localAdminUser, lookup)
}

func (localAuditRig *auditRig) events() []string {
	rows, err := localAuditRig.s.db.Query(`SELECT outcome FROM auth_events WHERE kind='ide_write' ORDER BY rowid`)
	if err != nil {
		localAuditRig.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var output string
		rows.Scan(&output)
		out = append(out, output)
	}
	return out
}

func (localAuditRig *auditRig) want(t *testing.T, values ...string) {
	t.Helper()
	got := localAuditRig.events()
	if strings.Join(got, "|") != strings.Join(values, "|") {
		t.Fatalf("événements %q, attendu %q", got, values)
	}
}

func TestFsAuditWrites(t *testing.T) {
	auditRig := newAuditRig(t)
	body := filepath.Base(auditRig.proj)
	if counter := auditRig.post("save", map[string]any{"path": "a.txt", "content": "SECRET-CONTENT"}); counter != 200 {
		t.Fatal(counter)
	}
	auditRig.want(t, "create "+body+":a.txt 14")
	fi, _ := os.Lstat(filepath.Join(auditRig.proj, "a.txt"))
	if counter := auditRig.post("save", map[string]any{"path": "a.txt", "content": "xy", "etag": ideEtag(fi)}); counter != 200 {
		t.Fatal(counter)
	}
	if counter := auditRig.post("new", map[string]any{"path": "d", "kind": "dir"}); counter != 201 {
		t.Fatal(counter)
	}
	if counter := auditRig.post("rename", map[string]any{"from": "a.txt", "to": "d/b.txt"}); counter != 204 {
		t.Fatal(counter)
	}
	if counter := auditRig.post("rm", map[string]any{"path": "d/b.txt"}); counter != 200 {
		t.Fatal(counter)
	}
	ents, _ := os.ReadDir(filepath.Join(auditRig.proj, ideTrash))
	var id string
	for _, de := range ents {
		if de.IsDir() {
			id = de.Name()
		}
	}
	if counter := auditRig.post("restore", map[string]any{"id": id}); counter != 204 {
		t.Fatal(counter)
	}
	auditRig.post("rm", map[string]any{"path": "d/b.txt"})
	ents, _ = os.ReadDir(filepath.Join(auditRig.proj, ideTrash))
	for _, de := range ents {
		if de.IsDir() {
			id = de.Name()
		}
	}
	if counter := auditRig.post("purge", map[string]any{"id": id}); counter != 200 {
		t.Fatal(counter)
	}
	got := auditRig.events()
	want := []string{"create " + body + ":a.txt 14", "save " + body + ":a.txt 2", "new " + body + ":d", "rename " + body + ":a.txt -> d/b.txt",
		"rm " + body + ":d/b.txt", "restore " + body + ":d/b.txt", "rm " + body + ":d/b.txt", "purge " + body + ":d/b.txt"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	for _, output := range got {
		if strings.Contains(output, "SECRET") || strings.Contains(output, auditRig.proj) {
			t.Fatal("fuite", output)
		}
	}
}

func TestFsAuditNotOnFailure(t *testing.T) {
	auditRig := newAuditRig(t)
	os.WriteFile(filepath.Join(auditRig.proj, "a.txt"), []byte("old"), 0o644)
	if counter := auditRig.post("save", map[string]any{"path": "a.txt", "content": "n", "etag": "stale"}); counter != 409 {
		t.Fatal(counter)
	}
	auditRig.post("save", map[string]any{"path": "../x", "content": "n"})
	bob := auditRig.login("bob", false)
	auditRig.s.Grant(bob.id, mainHomeID, RightFsRead)
	if counter := auditRig.fsDo("save", &bob, map[string]any{"dir": auditRig.proj, "path": "b.txt", "content": "n"}).Code; counter != 403 {
		t.Fatal(counter)
	}
	auditRig.want(t)
}

func TestFsAuditSanitised(t *testing.T) {
	auditRig := newAuditRig(t)
	if counter := auditRig.post("new", map[string]any{"path": "x\ny", "kind": "file"}); counter != 201 {
		t.Fatal(counter)
	}
	seg := strings.Repeat("é", 80)
	long := strings.TrimSuffix(strings.Repeat(seg+"/", 20), "/")
	os.MkdirAll(filepath.Join(auditRig.proj, long), 0o755)
	if counter := auditRig.post("new", map[string]any{"path": long + "/f", "kind": "file"}); counter != 201 {
		t.Fatal(counter)
	}
	ev := auditRig.events()
	if !strings.HasSuffix(ev[0], ":x?y") {
		t.Fatal(ev[0])
	}
	for _, output := range ev {
		if len(output) > 300 {
			t.Fatal(len(output))
		}
	}
	if len(ev) != 2 || !strings.HasSuffix(ev[1], "…") {
		t.Fatal(ev[1])
	}
}

func TestFsAuditVisibleToAdmin(t *testing.T) {
	auditRig := newAuditRig(t)
	auditRig.post("new", map[string]any{"path": "z", "kind": "file"})
	values := auditRig.ok(auditRig.do("GET", "/v1/admin/events?kind=ide_write", &auditRig.u, nil))["events"].([]any)
	if len(values) != 1 || values[0].(map[string]any)["user"] != "root" {
		t.Fatal(values)
	}
}

func TestFsAuditOpenMode(t *testing.T) {
	handler, _, proj := web(t)
	if counter, _, raw := fsop(t, handler, "new", map[string]any{"dir": proj, "path": "a", "kind": "file"}); counter != 201 {
		t.Fatal(counter, raw)
	}
}

// Un enfant n'a pas de base de comptes : il renvoie la ligne d'audit nettoyée à sa passerelle, jamais sur un échec.
func TestFsAuditChildHeader(t *testing.T) {
	handler, _, proj := web(t)
	childToken = "tok"
	t.Cleanup(func() { childToken = "" })
	post := func(body map[string]any) *httptest.ResponseRecorder {
		message, target, nb := fsReq("new", body)
		data, _ := json.Marshal(nb)
		request := httptest.NewRequest(message, target, bytes.NewReader(data))
		request.Host = testHost
		request.Header.Set(childHeader, "tok")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	localRecorder := post(map[string]any{"dir": proj, "path": "x\ny", "kind": "file"})
	if localRecorder.Code != 201 {
		t.Fatal(localRecorder.Code, localRecorder.Body)
	}
	got, err := url.QueryUnescape(localRecorder.Header().Get(ideAuditHeader))
	if err != nil || got != "new "+filepath.Base(proj)+":x?y" {
		t.Fatalf("%q %v", localRecorder.Header().Get(ideAuditHeader), err)
	}
	if localRecorder = post(map[string]any{"dir": proj, "path": "x\ny", "kind": "file"}); localRecorder.Code != 409 || localRecorder.Header().Get(ideAuditHeader) != "" {
		t.Fatal(localRecorder.Code, localRecorder.Header())
	}
}

// La passerelle écrit l'évènement relayé par l'enfant pour l'utilisateur de la requête et retire l'en-tête.
func TestFsAuditRelayedFromChild(t *testing.T) {
	auditRig := newAuditRig(t)
	pro := t.TempDir()
	if err := saveHomes([]Home{{Name: "pro", Dir: pro}}); err != nil {
		t.Fatal(err)
	}
	id, _ := homeIDFor("pro")
	if err := auditRig.s.Grant(auditRig.u.id, id, RightFsWrite); err != nil {
		t.Fatal(err)
	}
	var forged atomic.Int32
	child := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.Header.Get(ideAuditHeader) != "" {
			forged.Add(1)
		}
		switch message := request.Method + " " + request.URL.Path; message {
		case "PUT /v1/fs/entries/content":
			responseWriter.Header().Set(ideAuditHeader, url.QueryEscape("save pro:a.txt 6"))
			responseWriter.WriteHeader(200)
		case "POST /v1/fs/entries":
			responseWriter.Header().Set(ideAuditHeader, url.QueryEscape("new pro:refusé"))
			responseWriter.WriteHeader(409)
		default:
			responseWriter.WriteHeader(200)
		}
		responseWriter.Write([]byte(`{"ok":true}`))
	}))
	defer child.Close()
	uRL, _ := url.Parse(child.URL)
	old := homeStart
	t.Cleanup(func() { homeStart = old; homesMu.Lock(); clear(homeProcs); homesMu.Unlock() })
	homeStart = func(home Home) (*homeProc, error) {
		return &homeProc{dir: home.Dir, proxy: childProxy(uRL, "tok"), done: make(chan struct{})}, nil
	}
	do := func(op string, forge bool) *httptest.ResponseRecorder {
		message, target, nb := fsReq(op, map[string]any{"dir": pro, "path": "a.txt", "content": "ééé", "kind": "file"})
		data, _ := json.Marshal(nb)
		request := httptest.NewRequest(message, target, bytes.NewReader(data))
		request.Host = testHost
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: sessCookie, Value: auditRig.u.cookie})
		request.AddCookie(&http.Cookie{Name: homeCookie, Value: "pro"})
		request.Header.Set(csrfHeader, auditRig.u.csrf)
		if forge {
			request.Header.Set(ideAuditHeader, url.QueryEscape("save pro:forgé 1"))
		}
		recorder := httptest.NewRecorder()
		auditRig.h.ServeHTTP(recorder, request)
		return recorder
	}
	if recorder := do("save", false); recorder.Code != 200 || recorder.Header().Get(ideAuditHeader) != "" {
		t.Fatal(recorder.Code, recorder.Header())
	}
	if recorder := do("new", false); recorder.Code != 409 || recorder.Header().Get(ideAuditHeader) != "" {
		t.Fatal(recorder.Code, recorder.Header())
	}
	if recorder := do("rm", true); recorder.Code != 200 {
		t.Fatal(recorder.Code)
	}
	auditRig.want(t, "save pro:a.txt 6")
	if forged.Load() != 0 {
		t.Fatal("en-tête forgé relayé à l'enfant")
	}
	var uid int64
	if err := auditRig.s.db.QueryRow(`SELECT user_id FROM auth_events WHERE kind='ide_write'`).Scan(&uid); err != nil || uid != auditRig.u.id {
		t.Fatal(uid, err)
	}
}

func TestFsAuditCleanFormatControls(t *testing.T) {
	for _, char := range []rune{0x202a, 0x202e, 0x2066, 0x2069, 0x200e, 0x200f, 0x061c, 0x2028, 0x2029, 0xfeff} {
		if got := ideAuditClean("a" + string(char) + "b"); got != "a?b" {
			t.Errorf("U+%04X : %q", char, got)
		}
	}
	if got := ideAuditClean("é/ü"); got != "é/ü" {
		t.Fatal(got)
	}
}
