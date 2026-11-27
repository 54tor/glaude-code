package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func deposerFichier(t *testing.T, nom string, age time.Duration) string {
	t.Helper()
	os.MkdirAll(imagesDir(), 0o700)
	chemin := filepath.Join(imagesDir(), nom)
	if erreur := os.WriteFile(chemin, []byte("x"), 0o600); erreur != nil {
		t.Fatal(erreur)
	}
	instant := time.Now().Add(-age)
	os.Chtimes(chemin, instant, instant)
	return chemin
}

func existe(chemin string) bool {
	_, erreur := os.Lstat(chemin)
	return erreur == nil
}

func TestSweepIndexesCitedUnprefixedFile(t *testing.T) {
	use(t.TempDir())
	chemin := deposerFichier(t, nomFichier, 2*time.Hour)
	ecrireTranscript(t, filepath.Join(root, "p", sessionA+".jsonl"), ligneCitant(nomFichier))
	indexes, _, _, erreur := sweepAttachments()
	valeurObtenue, _ := loadOwners()
	if erreur != nil || indexes != 1 || valeurObtenue[nomFichier] != sessionA || !existe(chemin) {
		t.Fatalf("indexés=%d index=%v err=%v", indexes, valeurObtenue, erreur)
	}
}

func TestSweepTrashesUncitedFileOlderThanGrace(t *testing.T) {
	use(t.TempDir())
	chemin := deposerFichier(t, nomFichier, 2*time.Hour)
	_, mis, _, erreur := sweepAttachments()
	if erreur != nil || mis != 1 || existe(chemin) || !existe(filepath.Join(orphanTrashDir(), nomFichier)) {
		t.Fatalf("mis à la corbeille=%d err=%v", mis, erreur)
	}
}

func TestSweepSkipsFileYoungerThanGrace(t *testing.T) {
	use(t.TempDir())
	chemin := deposerFichier(t, nomFichier, time.Minute)
	_, mis, _, _ := sweepAttachments()
	if mis != 0 || !existe(chemin) {
		t.Fatalf("fichier récent déplacé (%d)", mis)
	}
}

func TestSweepSkipsPrefixedFile(t *testing.T) {
	use(t.TempDir())
	chemin := deposerFichier(t, sessionA+"-"+nomFichier, 2*time.Hour)
	_, mis, _, _ := sweepAttachments()
	if mis != 0 || !existe(chemin) {
		t.Fatal("fichier préfixé déplacé")
	}
}

func TestSweepStopsOnCorruptIndex(t *testing.T) {
	use(t.TempDir())
	chemin := deposerFichier(t, nomFichier, 2*time.Hour)
	os.WriteFile(filepath.Join(imagesDir(), ownersFile), []byte("{"), 0o600)
	_, mis, _, erreur := sweepAttachments()
	if erreur != errOwnersCorrupt || mis != 0 || !existe(chemin) {
		t.Fatalf("err=%v mis=%d", erreur, mis)
	}
}

func TestSweepPurgesTrashedFileOlderThanKeep(t *testing.T) {
	use(t.TempDir())
	deposerFichier(t, nomFichier, 2*time.Hour)
	if erreur := trashOrphan(nomFichier); erreur != nil {
		t.Fatal(erreur)
	}
	ancien := strconv.FormatInt(time.Now().Add(-trashKeep-time.Hour).Unix(), 10)
	os.WriteFile(orphanStampPath(nomFichier), []byte(ancien), 0o600)
	_, _, purges, erreur := sweepAttachments()
	if erreur != nil || purges != 1 || existe(filepath.Join(orphanTrashDir(), nomFichier)) {
		t.Fatalf("purgés=%d err=%v", purges, erreur)
	}
}

func TestRestoreOrphanMovesFileBackAndSurvivesNextSweep(t *testing.T) {
	use(t.TempDir())
	chemin := deposerFichier(t, nomFichier, 2*time.Hour)
	trashOrphan(nomFichier)
	if erreur := restoreOrphan(nomFichier); erreur != nil || !existe(chemin) {
		t.Fatalf("restauration : %v", erreur)
	}
	_, mis, _, _ := sweepAttachments()
	if mis != 0 || !existe(chemin) {
		t.Fatal("fichier restauré remis à la corbeille")
	}
}

func TestOrphanNameRejectsTraversal(t *testing.T) {
	use(t.TempDir())
	for _, nom := range []string{"../x", ".trash/x", "a/b", ".cache", "", ownersFile} {
		if erreur := trashOrphan(nom); erreur != errOrphanName {
			t.Errorf("%q accepté (%v)", nom, erreur)
		}
	}
}

func TestListOrphansShowsActiveAndTrashed(t *testing.T) {
	use(t.TempDir())
	deposerFichier(t, "a.png", 2*time.Hour)
	deposerFichier(t, "b.png", 2*time.Hour)
	trashOrphan("b.png")
	valeurObtenue := listOrphans()
	if len(valeurObtenue) != 2 {
		t.Fatalf("obtenu %v", valeurObtenue)
	}
	corbeille := 0
	for _, fichier := range valeurObtenue {
		if fichier.Trashed {
			corbeille++
		}
	}
	if corbeille != 1 {
		t.Fatalf("attendu 1 en corbeille, obtenu %d", corbeille)
	}
}

func TestOrphanTrashRefusesSymlinkedTrashDir(t *testing.T) {
	use(t.TempDir())
	deposerFichier(t, nomFichier, 2*time.Hour)
	dehors := t.TempDir()
	os.Symlink(dehors, orphanTrashDir())
	trashOrphan(nomFichier)
	if existe(filepath.Join(dehors, nomFichier)) {
		t.Fatal("fichier déplacé hors du dossier de confiance")
	}
}
