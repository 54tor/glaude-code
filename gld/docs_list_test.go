package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDocsDeduplicatesAndOrdersMostRecentFirst(t *testing.T) {
	directory := t.TempDir()
	firstDocument := filepath.Join(directory, "premier.md")
	secondDocument := filepath.Join(directory, "second.md")
	os.WriteFile(firstDocument, []byte("abc"), 0o600)
	os.WriteFile(secondDocument, []byte("x"), 0o600)
	transcriptPath := filepath.Join(directory, "transcript.jsonl")
	transcriptContent := writeToolUseLine(firstDocument) + writeToolUseLine(secondDocument) + writeToolUseLine(firstDocument)
	os.WriteFile(transcriptPath, []byte(transcriptContent), 0o600)

	entries, listErr := listDocs(transcriptPath)

	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(entries) != 2 || entries[0].Path != firstDocument || entries[1].Path != secondDocument {
		t.Fatalf("ordre ou doublons inattendus : %+v", entries)
	}
	if entries[0].Size != 3 || entries[0].Name != "premier.md" {
		t.Fatalf("taille ou nom inattendus : %+v", entries[0])
	}
}

func TestListDocsFlagsMissingFilesAndIgnoresNonDisplayableOnes(t *testing.T) {
	directory := t.TempDir()
	missingDocument := filepath.Join(directory, "disparu.md")
	codeFile := filepath.Join(directory, "main.go")
	transcriptPath := filepath.Join(directory, "transcript.jsonl")
	os.WriteFile(transcriptPath, []byte(writeToolUseLine(missingDocument)+writeToolUseLine(codeFile)), 0o600)

	entries, _ := listDocs(transcriptPath)

	if len(entries) != 1 || !entries[0].Missing {
		t.Fatalf("un fichier disparu doit être listé comme absent, le code ignoré : %+v", entries)
	}
}

func TestDocumentsRouteListsOnlyTranscriptDocs(t *testing.T) {
	handler, cfg, _ := web(t)
	directory := t.TempDir()
	writtenDocument := filepath.Join(directory, "rapport.md")
	os.WriteFile(writtenDocument, []byte("# titre"), 0o600)
	os.WriteFile(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), []byte(writeToolUseLine(writtenDocument)), 0o600)

	request := httptest.NewRequest("GET", "/v1/sessions/"+id1+"/conversation/documents?dir=-p", nil)
	request.Host = testHost
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name":"rapport.md"`) {
		t.Fatalf("liste inattendue : %d %s", recorder.Code, recorder.Body)
	}
}
