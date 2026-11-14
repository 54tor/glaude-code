package main

import (
	"strings"
	"testing"
)

func TestMcpMask(t *testing.T) {
	if got := mcpMaskURL("https://u:p@h.example/x?key=SECRET", false); strings.Contains(got, "SECRET") || strings.Contains(got, "u:p") {
		t.Errorf("url : %s", got)
	}
	got := mcpMaskArgs([]string{"-y", "srv", "--api-key", "SECRET", "--token=TOP", "--port", "80"})
	if strings.Contains(got, "SECRET") || strings.Contains(got, "TOP") || !strings.Contains(got, "--port 80") {
		t.Errorf("args : %s", got)
	}
}

func TestMcpStatusPasseParLeCacheDesProjets(t *testing.T) {
	originalMcpStates := projectMcpStates
	defer func() { projectMcpStates = originalMcpStates }()
	nombreDeLancements := 0
	projectMcpStates = func(string) ([]McpState, error) {
		nombreDeLancements++
		return []McpState{{Name: "serveur-a", Status: "✔ Connected", OK: true}}, nil
	}

	statutObtenu, err := mcpStatus()

	if err != nil || statutObtenu["serveur-a"] != "✔ Connected" || nombreDeLancements != 1 {
		t.Errorf("statut=%v err=%v lancements=%d", statutObtenu, err, nombreDeLancements)
	}
}
