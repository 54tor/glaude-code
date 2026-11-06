//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Le contexte du tour annulé (délai, droit retiré) doit tuer aussi les petits-enfants de claude (une commande Bash lancée en
// tâche de fond), pas seulement claude.
func TestRunInOwnGroupTueLesPetitsEnfantsALAnnulation(t *testing.T) {
	fichierDuPID := filepath.Join(t.TempDir(), "pid")
	contexteDuTour, annuler := context.WithCancel(context.Background())
	commande := exec.CommandContext(contexteDuTour, "sh", "-c", "sleep 30 & echo $! > "+fichierDuPID+"; wait")
	runInOwnGroup(commande)
	if startErr := commande.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	var pidDuPetitEnfant int
	for echeance := time.Now().Add(5 * time.Second); time.Now().Before(echeance); time.Sleep(10 * time.Millisecond) {
		if contenu, readErr := os.ReadFile(fichierDuPID); readErr == nil && strings.TrimSpace(string(contenu)) != "" {
			pidDuPetitEnfant, _ = strconv.Atoi(strings.TrimSpace(string(contenu)))
			break
		}
	}
	if pidDuPetitEnfant == 0 {
		t.Fatal("le petit-enfant n'a pas démarré")
	}
	annuler()
	commande.Wait()
	for echeance := time.Now().Add(2 * time.Second); time.Now().Before(echeance); time.Sleep(10 * time.Millisecond) {
		if syscall.Kill(pidDuPetitEnfant, 0) != nil {
			return
		}
	}
	syscall.Kill(pidDuPetitEnfant, syscall.SIGKILL)
	t.Fatal("le petit-enfant a survécu à l'annulation du tour")
}
