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
	if n := len([]rune(safeTitle(strings.Repeat("é", 200)))); n != 40 {
		t.Errorf("titre limité à 40 caractères, obtenu %d", n)
	}
}
