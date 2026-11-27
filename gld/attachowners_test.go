package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const sessionIDTest = "11111111-2222-3333-4444-555555555555"

func TestSaveOwnersRoundTrip(t *testing.T) {
	use(t.TempDir())
	valeurAttendue := map[string]string{"a.png": sessionIDTest}
	if erreur := saveOwners(valeurAttendue); erreur != nil {
		t.Fatal(erreur)
	}
	valeurObtenue, erreur := loadOwners()
	if erreur != nil || !reflect.DeepEqual(valeurAttendue, valeurObtenue) {
		t.Fatalf("attendu %v, obtenu %v (%v)", valeurAttendue, valeurObtenue, erreur)
	}
}

func TestLoadOwnersCorruptReturnsErrorAndKeepsFile(t *testing.T) {
	use(t.TempDir())
	os.MkdirAll(imagesDir(), 0o700)
	chemin := filepath.Join(imagesDir(), ownersFile)
	os.WriteFile(chemin, []byte("{"), 0o600)
	if _, erreur := loadOwners(); erreur != errOwnersCorrupt {
		t.Fatalf("attendu errOwnersCorrupt, obtenu %v", erreur)
	}
	if contenu, _ := os.ReadFile(chemin); string(contenu) != "{" {
		t.Fatalf("fichier modifié : %q", contenu)
	}
}

func TestDropSessionFilesRemovesIndexedFile(t *testing.T) {
	use(t.TempDir())
	os.MkdirAll(imagesDir(), 0o700)
	fichier := filepath.Join(imagesDir(), "a.png")
	os.WriteFile(fichier, []byte("x"), 0o600)
	saveOwners(map[string]string{"a.png": sessionIDTest, "b.png": "autre"})
	dropSessionFiles([]string{sessionIDTest})
	if _, erreur := os.Stat(fichier); !os.IsNotExist(erreur) {
		t.Fatal("fichier indexé gardé")
	}
	valeurObtenue, _ := loadOwners()
	if _, present := valeurObtenue["a.png"]; present || valeurObtenue["b.png"] != "autre" {
		t.Fatalf("index après suppression : %v", valeurObtenue)
	}
}

func TestSaveOwnersRefusesSymlinkedImagesDir(t *testing.T) {
	configuration := t.TempDir()
	use(configuration)
	dehors := t.TempDir()
	os.Symlink(dehors, imagesDir())
	if erreur := saveOwners(map[string]string{"a.png": sessionIDTest}); erreur == nil {
		if _, statErreur := os.Stat(filepath.Join(dehors, ownersFile)); statErreur == nil {
			t.Fatal("écriture hors du dossier de confiance")
		}
	}
}

func TestDropSessionFilesKeepsFileCitedByAnotherSession(t *testing.T) {
	use(t.TempDir())
	os.MkdirAll(imagesDir(), 0o700)
	fichier := filepath.Join(imagesDir(), nomFichier)
	os.WriteFile(fichier, []byte("x"), 0o600)
	saveOwners(map[string]string{nomFichier: sessionA})
	ecrireTranscript(t, filepath.Join(root, "p", sessionB+".jsonl"), ligneCitant(nomFichier))
	dropSessionFiles([]string{sessionA})
	if _, erreur := os.Stat(fichier); erreur != nil {
		t.Fatal("fichier cité par une autre session supprimé")
	}
	valeurObtenue, _ := loadOwners()
	if valeurObtenue[nomFichier] != sessionB {
		t.Fatalf("attendu %s, obtenu %v", sessionB, valeurObtenue)
	}
}
