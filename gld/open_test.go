package main

import (
	"strings"
	"testing"
)

func TestSafeTitle(t *testing.T) {
	// Aucun métacaractère de cmd.exe (& | < > ^ %), de wt.exe (;) ni de guillemet ne doit survivre.
	hostile := `x&calc | whoami ^ %PATH% > f < g ; "h" ` + "\x00\n\t"
	got := safeTitle(hostile)
	if strings.ContainsAny(got, `&|<>^%;"`) || unsafeTitle.MatchString(got) {
		t.Errorf("caractère interdit dans %q", got)
	}
	if !strings.Contains(got, "calc") || !strings.Contains(got, "whoami") {
		t.Errorf("le texte utile doit rester : %q", got)
	}
	if got := safeTitle("Réunion équipe_v2.1-final"); got != "Réunion équipe_v2.1-final" {
		t.Errorf("un titre sain doit rester intact : %q", got)
	}
	if count := len([]rune(safeTitle(strings.Repeat("é", 200)))); count != 40 {
		t.Errorf("titre limité à 40 caractères, obtenu %d", count)
	}
}

// Le titre d'une session (donnée non fiable) ne doit pas ajouter de propriété de profil Konsole (« ; » sépare les propriétés de -p).
func TestLauncherCmdAssainitLeTitrePourKonsole(t *testing.T) {
	commandeObtenue, launchErr := launcherCmd("konsole", "projet;Command=/bin/sh", []string{"claude"})
	if launchErr != nil {
		t.Fatal(launchErr)
	}
	for _, argumentCourant := range commandeObtenue.Args {
		if strings.HasPrefix(argumentCourant, "tabtitle=") && strings.ContainsAny(strings.TrimPrefix(argumentCourant, "tabtitle="), ";=") {
			t.Fatalf("titre non assaini : %q", argumentCourant)
		}
	}
}
