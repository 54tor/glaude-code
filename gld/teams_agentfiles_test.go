package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestAgentFileRoundTrip(t *testing.T) {
	a := Agent{ID: "rex", Name: "Rex", Role: "Revue: stricte", Access: "write", Prompt: "Tu relis.", Model: "opus"}
	y, body, ok := frontMatter(string(agentFile(a)))
	if !ok {
		t.Fatal("pas de front matter")
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(y), &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "rex" || m["description"] != "Revue: stricte" || m["model"] != "opus" {
		t.Fatalf("front matter: %v", m)
	}
	if _, has := m["tools"]; has {
		t.Fatal("write ne doit pas limiter les outils")
	}
	if !strings.HasPrefix(body, "<!-- gld:managed -->\n\nTu relis.") {
		t.Fatalf("corps: %q", body)
	}
}

func TestAgentFileReadOnlyTools(t *testing.T) {
	y, _, _ := frontMatter(string(agentFile(Agent{ID: "lio", Role: "r", Access: "read", Prompt: "p"})))
	var m map[string]any
	if err := yaml.Unmarshal([]byte(y), &m); err != nil {
		t.Fatal(err)
	}
	tools, _ := m["tools"].(string)
	if tools == "" {
		t.Fatal("tools manquant")
	}
	for _, bad := range []string{"Edit", "Write", "Bash", "NotebookEdit", "Web"} {
		if strings.Contains(tools, bad) {
			t.Fatalf("%s dans %q", bad, tools)
		}
	}
	if _, has := m["model"]; has {
		t.Fatal("model vide doit être omis")
	}
}

func TestWriteAgentsConflict(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "agents"), 0o755)
	p := filepath.Join(root, "agents", "rex.md")
	os.WriteFile(p, []byte("mien"), 0o644)
	w, c, err := writeAgents(root, []Agent{{ID: "rex", Role: "r", Access: "read"}, {ID: "zed", Role: "r", Access: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 1 || c[0] != "rex" || len(w) != 1 || w[0] != "zed" {
		t.Fatalf("w=%v c=%v", w, c)
	}
	if b, _ := os.ReadFile(p); string(b) != "mien" {
		t.Fatalf("fichier modifié: %q", b)
	}
}

func TestWriteAgentsRewritesManaged(t *testing.T) {
	root := t.TempDir()
	if _, _, err := writeAgents(root, []Agent{{ID: "rex", Role: "v1", Access: "write", Prompt: "a"}}); err != nil {
		t.Fatal(err)
	}
	w, c, err := writeAgents(root, []Agent{{ID: "rex", Role: "v2", Access: "write", Prompt: "b"}})
	if err != nil || len(c) != 0 || len(w) != 1 {
		t.Fatalf("w=%v c=%v err=%v", w, c, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "agents", "rex.md"))
	if !strings.Contains(string(b), "v2") || strings.Contains(string(b), "v1") {
		t.Fatalf("pas remplacé: %s", b)
	}
}

func TestWriteAgentsRejectsBadID(t *testing.T) {
	root := t.TempDir()
	_, _, err := writeAgents(root, []Agent{{ID: "../x", Role: "r", Access: "read"}})
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if _, e := os.Stat(filepath.Join(root, "x.md")); e == nil {
		t.Fatal("fichier écrit")
	}
	if _, e := os.Stat(filepath.Join(root, "agents")); e == nil {
		t.Fatal("rien ne doit être écrit")
	}
}
