package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestIdeInTrashVariants(t *testing.T) {
	for rel, want := range map[string]bool{
		".gld-trash": true, ".GLD-TRASH/1/from": true, ".Gld-Trash./1": true, ".gld-trash:$DATA/1": true,
		"GLD-TR~1/1": true, "a/.gld-trash": false, "gld-trash": false, "x.txt": false,
	} {
		if got := ideInTrash(rel); got != want {
			t.Errorf("%q : %v", rel, got)
		}
	}
}

// F1 : un home enfant n'a pas de passerelle dans le handler ; la décision config_write se prend avant le relais.
func TestGateFsCodeWriteBeforeProxy(t *testing.T) {
	gateRig := newGateRig(t)
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, ".gld-trash", "7"), 0o755)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "7", "from"), []byte(".claude/settings.json"), 0o600)
	os.MkdirAll(filepath.Join(proj, ".gld-trash", "8"), 0o755)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "8", "from"), []byte("a.txt"), 0o600)
	fw := gateRig.user(t, "alice", false)
	gateRig.grant(t, fw, "", RightFsRead, RightFsWrite)
	cw := gateRig.user(t, "bob", false)
	gateRig.grant(t, cw, "", RightFsRead, RightFsWrite, RightConfigWrite)
	dir := "?dir=" + url.QueryEscape(proj)
	for _, record := range []struct {
		method, target, body string
		code                 bool
	}{
		{"PUT", "/v1/fs/entries/content" + dir + "&path=.CLAUDE/settings.json", `{"content":"x"}`, false},
		{"PUT", "/v1/fs/entries/content" + dir + "&path=a.txt", `{"content":"x"}`, true},
		{"POST", "/v1/fs/entries" + dir, `{"path":".git/hooks/pre-commit","kind":"file"}`, false},
		{"POST", "/v1/fs/entries" + dir, `{"path":"a.txt","kind":"file"}`, true},
		{"PATCH", "/v1/fs/entries" + dir, `{"from":"a.txt","to":".mcp.json"}`, false},
		{"PATCH", "/v1/fs/entries" + dir, `{"from":".gitattributes","to":"b.txt"}`, false},
		{"PATCH", "/v1/fs/entries" + dir, `{"from":"a.txt","to":"b.txt"}`, true},
		{"DELETE", "/v1/fs/entries" + dir + "&item=a.txt&item=sub/.git", ``, false},
		{"DELETE", "/v1/fs/entries" + dir + "&item=a.txt", ``, true},
		{"PATCH", "/v1/fs/trash/7" + dir, `{"trashed":false}`, false},
		{"PATCH", "/v1/fs/trash/8" + dir, `{"trashed":false}`, true},
		{"PATCH", "/v1/fs/trash/9" + dir, `{"trashed":false}`, true},
	} {
		recorder := gateRig.do(record.method, record.target, &fw, "", record.body)
		if reached(recorder) != record.code || (!record.code && recorder.Code != 403) {
			t.Errorf("fs_write %s %s %s : %d %s", record.method, record.target, record.body, recorder.Code, recorder.Body.String())
		}
		if recorder := gateRig.do(record.method, record.target, &cw, "", record.body); !reached(recorder) {
			t.Errorf("config_write %s %s : %d", record.method, record.target, recorder.Code)
		}
	}
}

// F3/F7/F16 côté handler (processus principal, avec passerelle) : corbeille forgée par variante de casse, suppression et restauration.
func TestIdeCodeDeleteRestoreAndTrashAlias(t *testing.T) {
	gateRig := newGateRig(t)
	mux := http.NewServeMux()
	routesIDEWrite(mux)
	proj := t.TempDir()
	if err := saveStore(Store{Groups: []VGroup{}, Favs: []string{}, Projects: []Project{}, Dirs: []string{proj}}); err != nil {
		t.Fatal(err)
	}
	gateRig.h = authGate(mux, gateRig.s, mux)
	fw := gateRig.user(t, "alice", false)
	gateRig.grant(t, fw, "", RightFsRead, RightFsWrite)
	dir := "?dir=" + url.QueryEscape(proj)
	os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	os.WriteFile(filepath.Join(proj, ".claude", "settings.json"), []byte("{}"), 0o600)
	if recorder := gateRig.do("DELETE", "/v1/fs/entries"+dir+"&item=.claude", &fw, "", ""); recorder.Code != 403 {
		t.Errorf("suppression de .claude : %d", recorder.Code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "settings.json")); err != nil {
		t.Error(err)
	}
	for _, path := range []string{".GLD-TRASH/1/from", ".gld-trash./1/from"} {
		if recorder := gateRig.do("PUT", "/v1/fs/entries/content"+dir+"&path="+url.QueryEscape(path), &fw, "", `{"content":".claude/settings.json","force":true}`); recorder.Code != 400 {
			t.Errorf("%s : %d %s", path, recorder.Code, recorder.Body.String())
		}
	}
	// Entrée de corbeille dont le from est un chemin exécutable : la restauration passe aussi par le handler.
	os.MkdirAll(filepath.Join(proj, ".gld-trash", "5"), 0o755)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "5", "from"), []byte(".git/config"), 0o600)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "5", "item"), []byte("x"), 0o600)
	if recorder := gateRig.do("PATCH", "/v1/fs/trash/5"+dir, &fw, "", `{"trashed":false}`); recorder.Code != 403 {
		t.Errorf("restauration : %d %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(proj, ".git", "config")); err == nil {
		t.Error(".git/config restauré")
	}
}
