package main

import (
	"os"
	"testing"
)

// Les tests changent le PATH et les faux claude d'un test à l'autre : aucun relevé de `claude agents` ne doit survivre entre eux.
func TestMain(m *testing.M) {
	agentsTTL = 0
	os.Setenv("GLD_AUTH_DIR", "/nonexistent/gld-auth-test") // jamais la base réelle de l'utilisateur
	stampTTL = 0
	os.Exit(m.Run())
}
