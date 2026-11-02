package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFsUploadWritesBinaryAndRefusesOverwrite(t *testing.T) {
	handler, _, project := web(t)
	binaryContent := "\x00\x01\xffdata"
	code, _, raw := call(t, handler, "POST", fsq("/v1/fs/entries/upload", project, "blob.bin"), binaryContent)
	if code != 201 {
		t.Fatal(code, raw)
	}
	if readF(t, filepath.Join(project, "blob.bin")) != binaryContent {
		t.Fatal("contenu différent")
	}
	code, _, raw = call(t, handler, "POST", fsq("/v1/fs/entries/upload", project, "blob.bin"), "autre")
	if code != 409 {
		t.Fatal(code, raw)
	}
	if readF(t, filepath.Join(project, "blob.bin")) != binaryContent {
		t.Fatal("fichier écrasé")
	}
	code, _, raw = call(t, handler, "POST", fsq("/v1/fs/entries/upload", project, "absent/x.bin"), "x")
	if code != 404 {
		t.Fatal(code, raw)
	}
	entries, _ := os.ReadDir(project)
	if len(entries) != 1 {
		t.Fatal("résidu", entries)
	}
}

func TestFsDownloadServesAttachment(t *testing.T) {
	handler, _, project := web(t)
	binaryContent := "\x00\x01binaire"
	os.WriteFile(filepath.Join(project, "a b.bin"), []byte(binaryContent), 0o644)
	code, _, raw := call(t, handler, "GET", fsq("/v1/fs/entries/raw", project, "a b.bin"), "")
	if code != 200 || raw != binaryContent {
		t.Fatal(code, raw)
	}
	code, _, _ = call(t, handler, "GET", fsq("/v1/fs/entries/raw", project, "../x"), "")
	if code != 400 && code != 403 && code != 404 {
		t.Fatal(code)
	}
}
