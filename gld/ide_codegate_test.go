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
	g := newGateRig(t)
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, ".gld-trash", "7"), 0o755)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "7", "from"), []byte(".claude/settings.json"), 0o600)
	os.MkdirAll(filepath.Join(proj, ".gld-trash", "8"), 0o755)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "8", "from"), []byte("a.txt"), 0o600)
	fw := g.user(t, "alice", false)
	g.grant(t, fw, "", RightFsRead, RightFsWrite)
	cw := g.user(t, "bob", false)
	g.grant(t, cw, "", RightFsRead, RightFsWrite, RightConfigWrite)
	d := "?dir=" + url.QueryEscape(proj)
	for _, c := range []struct {
		method, target, body string
		code                 bool
	}{
		{"PUT", "/v1/fs/entries/content" + d + "&path=.CLAUDE/settings.json", `{"content":"x"}`, false},
		{"PUT", "/v1/fs/entries/content" + d + "&path=a.txt", `{"content":"x"}`, true},
		{"POST", "/v1/fs/entries" + d, `{"path":".git/hooks/pre-commit","kind":"file"}`, false},
		{"POST", "/v1/fs/entries" + d, `{"path":"a.txt","kind":"file"}`, true},
		{"PATCH", "/v1/fs/entries" + d, `{"from":"a.txt","to":".mcp.json"}`, false},
		{"PATCH", "/v1/fs/entries" + d, `{"from":".gitattributes","to":"b.txt"}`, false},
		{"PATCH", "/v1/fs/entries" + d, `{"from":"a.txt","to":"b.txt"}`, true},
		{"DELETE", "/v1/fs/entries" + d + "&item=a.txt&item=sub/.git", ``, false},
		{"DELETE", "/v1/fs/entries" + d + "&item=a.txt", ``, true},
		{"PATCH", "/v1/fs/trash/7" + d, `{"trashed":false}`, false},
		{"PATCH", "/v1/fs/trash/8" + d, `{"trashed":false}`, true},
		{"PATCH", "/v1/fs/trash/9" + d, `{"trashed":false}`, true},
	} {
		w := g.do(c.method, c.target, &fw, "", c.body)
		if reached(w) != c.code || (!c.code && w.Code != 403) {
			t.Errorf("fs_write %s %s %s : %d %s", c.method, c.target, c.body, w.Code, w.Body.String())
		}
		if w := g.do(c.method, c.target, &cw, "", c.body); !reached(w) {
			t.Errorf("config_write %s %s : %d", c.method, c.target, w.Code)
		}
	}
}

// F3/F7/F16 côté handler (processus principal, avec passerelle) : corbeille forgée par variante de casse, suppression et restauration.
func TestIdeCodeDeleteRestoreAndTrashAlias(t *testing.T) {
	g := newGateRig(t)
	mux := http.NewServeMux()
	routesIDEWrite(mux)
	proj := t.TempDir()
	if err := saveStore(Store{Groups: []VGroup{}, Favs: []string{}, Projects: []Project{}, Dirs: []string{proj}}); err != nil {
		t.Fatal(err)
	}
	g.h = authGate(mux, g.s, mux)
	fw := g.user(t, "alice", false)
	g.grant(t, fw, "", RightFsRead, RightFsWrite)
	d := "?dir=" + url.QueryEscape(proj)
	os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	os.WriteFile(filepath.Join(proj, ".claude", "settings.json"), []byte("{}"), 0o600)
	if w := g.do("DELETE", "/v1/fs/entries"+d+"&item=.claude", &fw, "", ""); w.Code != 403 {
		t.Errorf("suppression de .claude : %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "settings.json")); err != nil {
		t.Error(err)
	}
	for _, p := range []string{".GLD-TRASH/1/from", ".gld-trash./1/from"} {
		if w := g.do("PUT", "/v1/fs/entries/content"+d+"&path="+url.QueryEscape(p), &fw, "", `{"content":".claude/settings.json","force":true}`); w.Code != 400 {
			t.Errorf("%s : %d %s", p, w.Code, w.Body.String())
		}
	}
	// Entrée de corbeille dont le from est un chemin exécutable : la restauration passe aussi par le handler.
	os.MkdirAll(filepath.Join(proj, ".gld-trash", "5"), 0o755)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "5", "from"), []byte(".git/config"), 0o600)
	os.WriteFile(filepath.Join(proj, ".gld-trash", "5", "item"), []byte("x"), 0o600)
	if w := g.do("PATCH", "/v1/fs/trash/5"+d, &fw, "", `{"trashed":false}`); w.Code != 403 {
		t.Errorf("restauration : %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(proj, ".git", "config")); err == nil {
		t.Error(".git/config restauré")
	}
}
