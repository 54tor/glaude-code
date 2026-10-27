package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListPlugins(t *testing.T) {
	old := pluginRaw
	defer func() { pluginRaw = old }()
	pluginRaw = func() ([]byte, error) {
		return []byte(`{"installed":[{"id":"a@m","version":"1","scope":"user","enabled":true}],
"available":[{"pluginId":"a@m","name":"a","marketplaceName":"m","installCount":5},{"pluginId":"b@m","name":"b","description":"Outil Go","marketplaceName":"m","installCount":9}]}`), nil
	}
	inst, av, err := listPlugins("", 10)
	if err != nil || len(inst) != 1 || len(av) != 2 || av[0].ID != "b@m" || !av[1].Installed {
		t.Fatalf("%v %+v %+v", err, inst, av)
	}
	if _, av, _ = listPlugins("go", 10); len(av) != 1 || av[0].ID != "b@m" {
		t.Fatalf("filtre : %+v", av)
	}
	if _, av, _ = listPlugins("", 1); len(av) != 1 {
		t.Fatalf("limite : %+v", av)
	}
}

func TestPluginActRefuse(t *testing.T) {
	for _, c := range [][4]string{{"rm", "a@m", "user", ""}, {"install", "a b", "user", ""}, {"install", "a@m", "global", ""}, {"install", "a@m", "project", ""}, {"install", "a@m", "project", "../x"}} {
		if _, err := pluginAct(c[0], c[1], c[2], c[3]); err == nil {
			t.Errorf("accepté : %v", c)
		}
	}
}

func TestMarketplaceActRefuse(t *testing.T) {
	for _, c := range [][3]string{{"add", "/etc", ""}, {"add", "../x", ""}, {"add", "http://a.b/c", ""}, {"add", "a/b;rm", ""}, {"remove", "", "a b"}, {"drop", "", "x"}} {
		if _, err := marketplaceAct(c[0], c[1], c[2]); err == nil {
			t.Errorf("accepté : %v", c)
		}
	}
}

func TestProjectMcpRefuse(t *testing.T) {
	for _, d := range []string{"", "..", "a/b", "dossier-inconnu-xyz"} {
		if _, err := projectMcp(d); err == nil {
			t.Errorf("accepté : %q", d)
		}
	}
}

func TestProjectTrusted(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	if projectTrusted("/a/b") {
		t.Fatal("sans ~/.claude.json")
	}
	os.WriteFile(filepath.Join(h, ".claude.json"), []byte(`{"projects":{"/a":{"hasTrustDialogAccepted":true},"/c":{"hasTrustDialogAccepted":false}}}`), 0o600)
	if !projectTrusted("/a/b") || projectTrusted("/c") || projectTrusted("/d") {
		t.Fatal("confiance héritée du parent, refusée sinon")
	}
}

func TestProjectTrustedHomeSecondaire(t *testing.T) {
	h, cfg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", h)
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	os.WriteFile(filepath.Join(h, ".claude.json"), []byte(`{"projects":{"/y":{"hasTrustDialogAccepted":true}},"mcpServers":{"autre":{}}}`), 0o600)
	os.WriteFile(filepath.Join(cfg, ".claude.json"), []byte(`{"projects":{"/x":{"hasTrustDialogAccepted":true}},"mcpServers":{"moi":{}}}`), 0o600)
	if !projectTrusted("/x") || projectTrusted("/y") {
		t.Fatal("confiance lue dans le .claude.json d'un autre compte")
	}
	for _, s := range mcpServers() {
		if s.Name == "autre" {
			t.Fatal("serveur MCP d'un autre compte")
		}
	}
}
