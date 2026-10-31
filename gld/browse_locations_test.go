package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowseLocations(t *testing.T) {
	allowedDir := t.TempDir()
	otherDir := filepath.Join(t.TempDir(), "other")
	if err := os.Mkdir(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setBrowseLocations("") })

	setBrowseLocations("")
	if !browseAllowed(otherDir) {
		t.Fatal("sans -locations tout doit être permis")
	}

	setBrowseLocations(allowedDir)
	if !browseAllowed(filepath.Join(allowedDir, "sub")) {
		t.Fatal("un sous-chemin d'une racine doit être permis")
	}
	if !browseAllowed(os.TempDir()) {
		t.Fatal("le dossier temporaire reste accessible")
	}
	if !browseIsRoot(allowedDir) {
		t.Fatal("la racine doit être reconnue")
	}
	if browseAllowed("/etc") {
		t.Fatal("/etc ne doit pas être permis")
	}
}
