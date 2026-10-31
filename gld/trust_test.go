package main

import (
	"strings"
	"testing"
)

func TestTrustPanes(t *testing.T) {
	old := tmuxRun
	defer func() { tmuxRun = old }()
	tmuxRun = func(_ string, values ...string) (string, error) {
		switch values[0] {
		case "list-panes":
			return "%1\tclaude\t/home/u/neuf\n%2\tclaude\t/home/u/autre\n%3\tclaude\trelatif\n%4\tless\t/home/u/code\n", nil
		case "capture-pane":
			if values[3] == "%1" || values[3] == "%3" || values[3] == "%4" {
				return " Do you trust the files in this folder?\n 1. Yes, proceed\n 2. No, exit\n", nil
			}
			return "$ ls\n", nil
		}
		return "", nil
	}
	ps := trustPanes()
	if len(ps) != 1 || ps[0].dir != "/home/u/neuf" || ps[0].pane != "%1" {
		t.Fatalf("%+v", ps)
	}
}

func TestTrustKeys(t *testing.T) {
	neuf := " Quick safety check: Is this a project you created or one you trust?\n ❯ No, exit\n   Yes, I trust this folder\n Enter to confirm · Esc to cancel"
	ancien := " Do you trust the files in this folder?\n ❯ 1. Yes, proceed\n   2. No, exit\n"
	// Des lignes « Yes… / No… » ailleurs sur l'écran (avant le dialogue) ne comptent pas.
	bruit := "Yes, we should deploy\nNo, wait\n" + neuf
	// Trois réponses, ou deux « Yes » : ambigu, rien n'est envoyé.
	trois := " Is this a project you created or one you trust?\n ❯ No, exit\n   Yes, I trust this folder\n   Yes, always\n"
	for _, record := range []struct {
		scr  string
		yes  bool
		want string
		nil_ bool
	}{
		{neuf, true, "Down", false},
		{neuf, false, "", false},
		{ancien, true, "", false},
		{ancien, false, "Down", false},
		{bruit, true, "Down", false},
		{trois, true, "", true},
		{"rien à voir", true, "", true},
	} {
		keys, valid := trustKeys(record.scr, record.yes)
		if valid == record.nil_ || strings.Join(keys, " ") != record.want {
			t.Errorf("yes=%v : %q (valide=%v), attendu %q (ambigu=%v)", record.yes, keys, valid, record.want, record.nil_)
		}
	}
	if trustOnWant(neuf, true) || !trustOnWant(neuf, false) || !trustOnWant(ancien, true) {
		t.Error("position du curseur mal lue")
	}
}
