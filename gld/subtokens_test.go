package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubagentTokensAreAddedToSession(testing *testing.T) {
	dossierSession := testing.TempDir()
	cheminTranscript := filepath.Join(dossierSession, "abc.jsonl")
	ligneUsage := func(identifiant string, sortie string) string {
		return `{"type":"assistant","message":{"id":"` + identifiant + `","usage":{"input_tokens":1,"output_tokens":` + sortie + `,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	}
	dossierSousAgents := filepath.Join(dossierSession, "abc", "subagents")
	if err := os.MkdirAll(dossierSousAgents, 0o755); err != nil {
		testing.Fatal(err)
	}
	os.WriteFile(cheminTranscript, []byte(ligneUsage("m1", "10")), 0o600)
	os.WriteFile(filepath.Join(dossierSousAgents, "agent-x.jsonl"), []byte(ligneUsage("s1", "5")), 0o600)
	informationsFichier, _ := os.Stat(cheminTranscript)
	sessionObtenue := info(cheminTranscript, informationsFichier)
	if sessionObtenue.Tok.Out != 15 || sessionObtenue.Sub.Out != 5 {
		testing.Fatalf("attendu Tok.Out=15 Sub.Out=5, obtenu %+v %+v", sessionObtenue.Tok, sessionObtenue.Sub)
	}
}
