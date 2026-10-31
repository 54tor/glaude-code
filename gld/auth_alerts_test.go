package main

import (
	"encoding/json"
	"strconv"
	"testing"
)

// Un jumelage sur mon compte doit apparaître dans mes alertes (pour révocation depuis /v1/auth/sessions), jamais
// un jumelage sur un autre compte.
func TestAlertsPairing(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, _ := authRig.full(secret)
	bob, err := authRig.s.AddUser("bob", testPw, false)
	if err != nil {
		t.Fatal(err)
	}

	authRig.s.eventIP(authRig.uid, evLogin, "pair", "203.0.113.9")
	authRig.s.eventIP(bob, evLogin, "pair", "203.0.113.10")

	recorder := authRig.do("GET", "/v1/auth/alerts?since=0", content, "", nil)
	var record struct {
		Pairings []map[string]any `json:"pairings"`
	}
	if recorder.Code != 200 {
		t.Fatalf("alertes : %d %s", recorder.Code, recorder.Body)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Pairings) != 1 || record.Pairings[0]["ip"] != "203.0.113.9" {
		t.Fatalf("jumelages : %v", record.Pairings)
	}
}

// Le curseur suit les lignes, pas les secondes : un bannissement écrit dans la même seconde que le relevé précédent n'est pas perdu.
func TestAlertsMemeSeconde(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, _ := authRig.full(secret)
	type res struct {
		Cursor int64            `json:"cursor"`
		Bans   []map[string]any `json:"bans"`
	}
	get := func(after int64) res {
		var localRes res
		recorder := authRig.do("GET", "/v1/auth/alerts?after="+strconv.FormatInt(after, 10), content, "", nil)
		if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &localRes) != nil {
			t.Fatalf("alertes : %d %s", recorder.Code, recorder.Body)
		}
		return localRes
	}
	anotherRes := get(-1)
	authRig.s.eventIP(0, "lock", "ban", "198.51.100.1")
	anotherRes = get(anotherRes.Cursor)
	if len(anotherRes.Bans) != 1 {
		t.Fatalf("premier ban : %v", anotherRes.Bans)
	}
	authRig.s.eventIP(0, "lock", "ban", "198.51.100.2") // même seconde (horloge du banc figée)
	if anotherRes = get(anotherRes.Cursor); len(anotherRes.Bans) != 1 {
		t.Fatalf("second ban dans la même seconde perdu : %v", anotherRes.Bans)
	}
}
