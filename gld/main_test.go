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
	qd, _ := os.MkdirTemp("", "gld-queue-test")
	queueDir = func() string { return qd }
	sd, _ := os.MkdirTemp("", "gld-share-test")
	shareDir = func() string { return sd }
	// L'environnement du poste de développement ne doit pas changer l'adresse du QR ni la confiance aux proxys.
	os.Unsetenv("GLD_PUBLIC_URL")
	os.Unsetenv("GLD_TRUSTED_PROXIES")
	os.Exit(m.Run())
}
