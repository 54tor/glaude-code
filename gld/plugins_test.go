package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	for _, values := range [][4]string{{"rm", "a@m", "user", ""}, {"install", "a b", "user", ""}, {"install", "a@m", "global", ""}, {"install", "a@m", "project", ""}, {"install", "a@m", "project", "../x"}} {
		if _, err := pluginAct(values[0], values[1], values[2], values[3]); err == nil {
			t.Errorf("accepté : %v", values)
		}
	}
}

func TestMarketplaceActRefuse(t *testing.T) {
	for _, values := range [][3]string{{"add", "/etc", ""}, {"add", "../x", ""}, {"add", "http://a.b/c", ""}, {"add", "a/b;rm", ""}, {"add", "https://127.0.0.1/x", ""}, {"add", "https://localhost:8080/x", ""}, {"add", "https://10.0.0.5/r", ""}, {"add", "https://169.254.169.254/latest", ""}, {"add", "https://api.localhost/x", ""}, {"add", "https://0.0.0.0/x", ""}, {"remove", "", "a b"}, {"drop", "", "x"}} {
		if _, err := marketplaceAct(values[0], values[1], values[2]); err == nil {
			t.Errorf("accepté : %v", values)
		}
	}
}

func TestProjectMcpRefuse(t *testing.T) {
	for _, dir := range []string{"", "..", "a/b", "dossier-inconnu-xyz"} {
		if _, err := projectMcp(dir); err == nil {
			t.Errorf("accepté : %q", dir)
		}
	}
}

func TestProjectTrusted(t *testing.T) {
	header := t.TempDir()
	t.Setenv("HOME", header)
	if projectTrusted("/a/b") {
		t.Fatal("sans ~/.claude.json")
	}
	os.WriteFile(filepath.Join(header, ".claude.json"), []byte(`{"projects":{"/a":{"hasTrustDialogAccepted":true},"/c":{"hasTrustDialogAccepted":false}}}`), 0o600)
	if !projectTrusted("/a/b") || projectTrusted("/c") || projectTrusted("/d") {
		t.Fatal("confiance héritée du parent, refusée sinon")
	}
}

func TestProjectTrustedHomeSecondaire(t *testing.T) {
	header, cfg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", header)
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	os.WriteFile(filepath.Join(header, ".claude.json"), []byte(`{"projects":{"/y":{"hasTrustDialogAccepted":true}},"mcpServers":{"autre":{}}}`), 0o600)
	os.WriteFile(filepath.Join(cfg, ".claude.json"), []byte(`{"projects":{"/x":{"hasTrustDialogAccepted":true}},"mcpServers":{"moi":{}}}`), 0o600)
	if !projectTrusted("/x") || projectTrusted("/y") {
		t.Fatal("confiance lue dans le .claude.json d'un autre compte")
	}
	for _, mcpServer := range mcpServers() {
		if mcpServer.Name == "autre" {
			t.Fatal("serveur MCP d'un autre compte")
		}
	}
}

// Un dossier dont `claude mcp list` traîne ne fait plus attendre les autres ; un échec est gardé comme un succès.
func TestMcpParDossier(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	count := filepath.Join(home, "appels")
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho x >> "+count+"\ncase \"$PWD\" in *lent) sleep 2;; *echec) exit 1;; esac\necho 'srv: cmd - ✓ Connected'\n"), 0o755)
	lent, vite, echec := filepath.Join(home, "lent"), filepath.Join(home, "vite"), filepath.Join(home, "echec")
	for _, dir := range []string{lent, vite, echec} {
		os.Mkdir(dir, 0o755)
	}
	done := make(chan struct{})
	go func() { projectMcpStates(lent); close(done) }()
	defer func() { <-done }() // le dossier temporaire ne disparaît pas sous le faux claude encore lancé
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	if out, err := projectMcpStates(vite); err != nil || len(out) != 1 || time.Since(start) > time.Second {
		t.Fatalf("dossier rapide : %v %v en %v", out, err, time.Since(start))
	}
	projectMcpStates(echec)
	projectMcpStates(echec)
	data, _ := os.ReadFile(count)
	if count := strings.Count(string(data), "x"); count != 3 {
		t.Fatalf("%d lancements de claude, attendu 3 (lent, vite, echec une seule fois)", count)
	}
}
