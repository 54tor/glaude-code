package main

import "testing"

func TestScreenAsks(t *testing.T) {
	for _, c := range []struct {
		name, scr string
		want      bool
	}{
		{"question à choix", "Quel périmètre ?\n\n❯ 1. Tout\n  2. Le noyau\n  3. Type something.\n────\n  4. Chat about this\n\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel", true},
		{"autorisation", " Ask rule Bash(rm *) overrides auto mode\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend", true},
		{"liste numérotée dans le texte", "Voici le plan :\n1. Lire\n2. Écrire\n\n❯ \n ⏵⏵ auto mode on (shift+tab to cycle)", false},
		{"en réflexion", "Do you want to proceed?\n1. a\n2. b\n✻ Réfléchit… (esc to interrupt)", false},
		{"écran vide", "", false},
	} {
		if got := screenAsks(c.scr); got != c.want {
			t.Errorf("%s : %v, attendu %v", c.name, got, c.want)
		}
	}
}
