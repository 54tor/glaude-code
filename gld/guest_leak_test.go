package main

import (
	"os"
	"strings"
	"testing"
)

const (
	leakHome       = "/home/bobby"
	leakProjectDir = "/home/bobby/Projets/app"
)

var leakedHostStrings = []string{"bobby", "Jean Dupont", "jean.dupont", "/home/", "Projets/app", "/data/backups"}

func TestGuestNeVoitAucuneIdentiteNiChemin(t *testing.T) {
	t.Setenv("HOME", leakHome)
	previousLoader := identityLoadTerms
	identityLoadTerms = func() []string {
		return expandIdentityTerms([]string{"bobby", "Jean Dupont", "jean.dupont@example.com"})
	}
	identityMu.Lock()
	identityCached = nil
	identityMu.Unlock()
	t.Cleanup(func() {
		identityLoadTerms = previousLoader
		identityMu.Lock()
		identityCached = nil
		identityMu.Unlock()
	})

	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	transcriptPath, found := transcript("-p", id1)
	if !found {
		t.Fatal("transcript introuvable")
	}
	hostLine := `{"type":"user","cwd":"` + leakProjectDir + `","timestamp":"2026-01-01T00:00:01Z","message":{"content":"` +
		`lis ` + leakProjectDir + `/src/a.go et /data/backups/db.sql ; whoami -> bobby ; auteur Jean Dupont jean.dupont@example.com"}}` + "\n"
	transcriptFile, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	transcriptFile.WriteString(hostLine)
	transcriptFile.Close()

	for _, requestPath := range []string{
		"/v1/sessions/" + id1 + "/conversation",
		"/v1/sessions/" + id1 + "/live",
		"/v1/guest/me",
	} {
		recorder := gReq{method: "GET", path: requestPath, cookie: content}.do(handler)
		if recorder.Code != 200 {
			t.Fatalf("%s : %d", requestPath, recorder.Code)
		}
		for _, leaked := range leakedHostStrings {
			if strings.Contains(strings.ToLower(recorder.Body.String()), strings.ToLower(leaked)) {
				t.Errorf("%s : %q visible pour un invité : %s", requestPath, leaked, recorder.Body.String())
			}
		}
	}
	conversation := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}.do(handler).Body.String()
	for _, expectedPart := range []string{"…/a.go", "…/db.sql"} {
		if !strings.Contains(conversation, expectedPart) {
			t.Errorf("conversation : %q attendu absent de %s", expectedPart, conversation)
		}
	}
}
