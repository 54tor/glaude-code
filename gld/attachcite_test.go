package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const (
	sessionA   = "aaaaaaaa-2222-3333-4444-555555555555"
	sessionB   = "bbbbbbbb-2222-3333-4444-555555555555"
	nomFichier = "20260101-000000-aabbccdd.png"
)

func ecrireTranscript(t *testing.T, chemin, contenu string) {
	t.Helper()
	if erreur := os.MkdirAll(filepath.Dir(chemin), 0o755); erreur != nil {
		t.Fatal(erreur)
	}
	if erreur := os.WriteFile(chemin, []byte(contenu), 0o600); erreur != nil {
		t.Fatal(erreur)
	}
}

func ligneCitant(nom string) string {
	return `{"type":"user","message":{"content":"voir @~/.claude/gld-web-images/` + nom + `"}}` + "\n"
}

func TestCitationsFindsNameInTranscript(t *testing.T) {
	use(t.TempDir())
	ecrireTranscript(t, filepath.Join(root, "p", sessionA+".jsonl"), ligneCitant(nomFichier))
	valeurObtenue := attachmentCitations()
	valeurAttendue := map[string][]string{nomFichier: {sessionA}}
	if !reflect.DeepEqual(valeurAttendue, valeurObtenue) {
		t.Fatalf("attendu %v, obtenu %v", valeurAttendue, valeurObtenue)
	}
}

func TestCitationsAttributesSubagentToParent(t *testing.T) {
	use(t.TempDir())
	ecrireTranscript(t, filepath.Join(root, "p", sessionA, "subagents", "agent-1.jsonl"), ligneCitant(nomFichier))
	valeurObtenue := attachmentCitations()
	if !reflect.DeepEqual(map[string][]string{nomFichier: {sessionA}}, valeurObtenue) {
		t.Fatalf("obtenu %v", valeurObtenue)
	}
}

func TestCitationsMultipleSessionsListsAll(t *testing.T) {
	use(t.TempDir())
	ecrireTranscript(t, filepath.Join(root, "p", sessionA+".jsonl"), ligneCitant(nomFichier))
	ecrireTranscript(t, filepath.Join(root, "p", sessionB+".jsonl"), ligneCitant(nomFichier))
	valeurObtenue := attachmentCitations()[nomFichier]
	sort.Strings(valeurObtenue)
	if !reflect.DeepEqual([]string{sessionA, sessionB}, valeurObtenue) {
		t.Fatalf("obtenu %v", valeurObtenue)
	}
}
