package main

import "testing"

func TestPersistedTokenSurvivesRestart(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	premierDemarrage := persistedToken()
	secondDemarrage := persistedToken()
	if premierDemarrage != secondDemarrage {
		t.Fatalf("le jeton généré doit survivre au redémarrage : %q != %q", premierDemarrage, secondDemarrage)
	}
	if checkToken(premierDemarrage) != nil {
		t.Fatalf("jeton trop court : %q", premierDemarrage)
	}
}
