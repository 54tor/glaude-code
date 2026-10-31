package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestAgentFileRoundTrip(t *testing.T) {
	agent := Agent{ID: "rex", Name: "Rex", Role: "Revue: stricte", Access: "write", Prompt: "Tu relis.", Model: "opus"}
	text, body, ok := frontMatter(string(agentFile(agent)))
	if !ok {
		t.Fatal("pas de front matter")
	}
	var lookup map[string]any
	if err := yaml.Unmarshal([]byte(text), &lookup); err != nil {
		t.Fatal(err)
	}
	if lookup["name"] != "rex" || lookup["description"] != "Revue: stricte" || lookup["model"] != "opus" {
		t.Fatalf("front matter: %v", lookup)
	}
	if _, has := lookup["tools"]; has {
		t.Fatal("write ne doit pas limiter les outils")
	}
	if !strings.HasPrefix(body, "<!-- gld:managed -->\n\nTu relis.") {
		t.Fatalf("corps: %q", body)
	}
}

func TestAgentFileReadOnlyTools(t *testing.T) {
	text, _, _ := frontMatter(string(agentFile(Agent{ID: "lio", Role: "r", Access: "read", Prompt: "p"})))
	var lookup map[string]any
	if err := yaml.Unmarshal([]byte(text), &lookup); err != nil {
		t.Fatal(err)
	}
	tools, _ := lookup["tools"].(string)
	if tools == "" {
		t.Fatal("tools manquant")
	}
	for _, bad := range []string{"Edit", "Write", "Bash", "NotebookEdit", "Web"} {
		if strings.Contains(tools, bad) {
			t.Fatalf("%s dans %q", bad, tools)
		}
	}
	if _, has := lookup["model"]; has {
		t.Fatal("model vide doit être omis")
	}
}

func TestWriteAgentsConflict(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "agents"), 0o755)
	path := filepath.Join(root, "agents", "rex.md")
	os.WriteFile(path, []byte("mien"), 0o644)
	values, localValues, err := writeAgents(root, []Agent{{ID: "rex", Role: "r", Access: "read"}, {ID: "zed", Role: "r", Access: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(localValues) != 1 || localValues[0] != "rex" || len(values) != 1 || values[0] != "zed" {
		t.Fatalf("w=%v c=%v", values, localValues)
	}
	if data, _ := os.ReadFile(path); string(data) != "mien" {
		t.Fatalf("fichier modifié: %q", data)
	}
}

func TestWriteAgentsRewritesManaged(t *testing.T) {
	root := t.TempDir()
	if _, _, err := writeAgents(root, []Agent{{ID: "rex", Role: "v1", Access: "write", Prompt: "a"}}); err != nil {
		t.Fatal(err)
	}
	values, localValues, err := writeAgents(root, []Agent{{ID: "rex", Role: "v2", Access: "write", Prompt: "b"}})
	if err != nil || len(localValues) != 0 || len(values) != 1 {
		t.Fatalf("w=%v c=%v err=%v", values, localValues, err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "agents", "rex.md"))
	if !strings.Contains(string(data), "v2") || strings.Contains(string(data), "v1") {
		t.Fatalf("pas remplacé: %s", data)
	}
}

func TestWriteAgentsRejectsBadID(t *testing.T) {
	root := t.TempDir()
	_, _, err := writeAgents(root, []Agent{{ID: "../x", Role: "r", Access: "read"}})
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if _, err := os.Stat(filepath.Join(root, "x.md")); err == nil {
		t.Fatal("fichier écrit")
	}
	if _, err := os.Stat(filepath.Join(root, "agents")); err == nil {
		t.Fatal("rien ne doit être écrit")
	}
}
