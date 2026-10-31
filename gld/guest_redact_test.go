package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPathRedactorMasqueLHote(t *testing.T) {
	redactor := newPathRedactor("/home/bobby", "/home/bobby/Projets/app", "guest-ab12", "Jean Dupont", "jean.dupont@example.com")
	testCases := map[string]string{
		`{"cwd":"/home/bobby/Projets/app/src/a.go"}`:          `{"cwd":"src/a.go"}`,
		`cd /home/bobby/Projets/app && ls`:                    `cd . && ls`,
		`cat /home/bobby/.claude/settings.json`:               `cat …/settings.json`,
		`/tmp/claude-1000/-home-bobby-Projets/s/scratchpad/x`: `…/x`,
		`ls -l /usr/bin/git:12`:                               `ls -l …/git:12`,
		"ligne\n/etc/passwd fin":                              "ligne\n…/passwd fin",
		`-home-bobby-Projets-app`:                             `-home-guest-ab12-Projets-app`,
		`whoami -> bobby, bobby@example.com, bobbyx, /v1/a/b`: `whoami -> guest-ab12, guest-ab12@example.com, bobbyx, /v1/a/b`,
		`https://example.com/tmp/page`:                        `https://example.com/tmp/page`,
		`/home/bobbyx/Projets/app/a.go`:                       `…/a.go`,
		`/data/backups/db.sql`:                                `…/db.sql`,
		`auteur Jean Dupont <jean.dupont@example.com>`:        `auteur guest-ab12 <guest-ab12>`,
	}
	for input, expected := range testCases {
		if obtained := string(redactor.apply([]byte(input))); obtained != expected {
			t.Errorf("%q : obtenu %q, attendu %q", input, obtained, expected)
		}
	}
}

func TestPathRedactorSansHomeNeFaitRien(t *testing.T) {
	for _, home := range []string{"", "/"} {
		if newPathRedactor(home, "", "guest-ab12") != nil {
			t.Errorf("home %q : un masqueur a été créé", home)
		}
	}
}

func TestRedactWriterRetireLaLongueurEtMasque(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Length", "99")
	writer := guestRedactedWriter(recorder, newPathRedactor("/home/bob", "/home/bob/p", "guest-ab12"))
	writer.WriteHeader(200)
	count, err := writer.Write([]byte(`{"p":"/home/bob/p/x"}`))
	if err != nil || count != len(`{"p":"/home/bob/p/x"}`) {
		t.Fatalf("écriture : %d %v", count, err)
	}
	if body := recorder.Body.String(); body != `{"p":"x"}` {
		t.Errorf("corps : %q", body)
	}
	if recorder.Header().Get("Content-Length") != "" {
		t.Error("Content-Length conservé")
	}
}

func TestGuestLiveBodyRetireLesLignesDeCommande(t *testing.T) {
	body := []byte(`{"activity":"exécute Bash : sed -n 1,5p gld/x.go","tool":"Bash : sed -n 1,5p gld/x.go","exec":true,"context":123,` +
		`"subs":[{"id":"a","activity":"exécute Read : /home/bob/secret.txt"},{"id":"b","activity":"réfléchit"}]}`)
	expected := `{"activity":"exécute Bash","context":123,"exec":true,"subs":[{"activity":"exécute Read","id":"a"},{"activity":"réfléchit","id":"b"}],"tool":"Bash"}`
	if obtained := string(guestLiveBody(body)); obtained != expected {
		t.Errorf("obtenu %s", obtained)
	}
	if obtained := string(guestLiveBody([]byte("pas du json"))); obtained != "pas du json" {
		t.Errorf("corps illisible modifié : %q", obtained)
	}
}

func TestGuestAliasStableEtSansLeCompte(t *testing.T) {
	if guestAlias("inv-1") != guestAlias("inv-1") || guestAlias("inv-1") == guestAlias("inv-2") {
		t.Error("alias instable ou identique entre invitations")
	}
	if alias := guestAlias("inv-1"); len(alias) != len(aliasPrefix)+aliasHexLength {
		t.Errorf("alias : %q", alias)
	}
}

func TestExpandIdentityTerms(t *testing.T) {
	obtained := expandIdentityTerms([]string{"Jean Dupont", "jean.dupont@example.com", "ab"})
	expected := []string{"jean.dupont@example.com", "Jean Dupont", "jean.dupont", "Dupont", "Jean"}
	if strings.Join(obtained, "|") != strings.Join(expected, "|") {
		t.Errorf("obtenu %v, attendu %v", obtained, expected)
	}
}
