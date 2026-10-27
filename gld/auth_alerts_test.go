package main

import (
	"encoding/json"
	"testing"
)

// Un jumelage sur mon compte doit apparaître dans mes alertes (pour révocation depuis /auth/sessions), jamais
// un jumelage sur un autre compte.
func TestAlertsPairing(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, _ := g.full(secret)
	bob, err := g.s.AddUser("bob", testPw, false)
	if err != nil {
		t.Fatal(err)
	}

	g.s.eventIP(g.uid, evLogin, "pair", "203.0.113.9")
	g.s.eventIP(bob, evLogin, "pair", "203.0.113.10")

	w := g.do("GET", "/auth/alerts?since=0", c, "", nil)
	var b struct {
		Pairings []map[string]any `json:"pairings"`
	}
	if w.Code != 200 {
		t.Fatalf("alertes : %d %s", w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Pairings) != 1 || b.Pairings[0]["ip"] != "203.0.113.9" {
		t.Fatalf("jumelages : %v", b.Pairings)
	}
}
