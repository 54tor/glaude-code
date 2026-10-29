package main

import (
	"encoding/json"
	"strconv"
	"testing"
)

// Un jumelage sur mon compte doit apparaître dans mes alertes (pour révocation depuis /v1/auth/sessions), jamais
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

	w := g.do("GET", "/v1/auth/alerts?since=0", c, "", nil)
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

// Le curseur suit les lignes, pas les secondes : un bannissement écrit dans la même seconde que le relevé précédent n'est pas perdu.
func TestAlertsMemeSeconde(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, _ := g.full(secret)
	type res struct {
		Cursor int64            `json:"cursor"`
		Bans   []map[string]any `json:"bans"`
	}
	get := func(after int64) res {
		var r res
		w := g.do("GET", "/v1/auth/alerts?after="+strconv.FormatInt(after, 10), c, "", nil)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil {
			t.Fatalf("alertes : %d %s", w.Code, w.Body)
		}
		return r
	}
	r := get(-1)
	g.s.eventIP(0, "lock", "ban", "198.51.100.1")
	r = get(r.Cursor)
	if len(r.Bans) != 1 {
		t.Fatalf("premier ban : %v", r.Bans)
	}
	g.s.eventIP(0, "lock", "ban", "198.51.100.2") // même seconde (horloge du banc figée)
	if r = get(r.Cursor); len(r.Bans) != 1 {
		t.Fatalf("second ban dans la même seconde perdu : %v", r.Bans)
	}
}
