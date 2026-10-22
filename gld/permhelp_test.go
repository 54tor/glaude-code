package main

import "testing"

func TestPermHelp(t *testing.T) {
	old := permHelpRun
	defer func() { permHelpRun = old }()
	calls := 0
	permHelpRun = func(p string) (string, error) { calls++; return "  Autorise : x\n", nil }
	clear(permHelpCache)
	for i := 0; i < 2; i++ {
		if got, err := permHelp("allow", "Bash(git status:*)"); err != nil || got != "Autorise : x" {
			t.Fatalf("%q %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("explication non gardée : %d appels", calls)
	}
	for _, bad := range []string{"", "  ", "a\nb"} {
		if _, err := permHelp("allow", bad); err == nil {
			t.Errorf("règle invalide acceptée : %q", bad)
		}
	}
}
