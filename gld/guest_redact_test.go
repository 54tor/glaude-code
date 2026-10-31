package main

import (
	"net/http/httptest"
	"testing"
)

func TestPathRedactorMasqueLesFormesDeChemin(t *testing.T) {
	redactor := newPathRedactor("/home/bob")
	testCases := map[string]string{
		`{"cwd":"/home/bob/proj/a.go"}`:                  `{"cwd":"~/proj/a.go"}`,
		`/tmp/claude-1000/-home-bob-proj/s/scratchpad/x`: `/tmp/claude-1000/-home-user-proj/s/scratchpad/x`,
		`dir=-home-bob`: `dir=-home-user`,
		`bob écrit à bob@example.com, /home/bobby/x`: `bob écrit à bob@example.com, /home/bobby/x`,
		`-home-bobby-proj`: `-home-bobby-proj`,
	}
	for input, expected := range testCases {
		if obtained := string(redactor.apply([]byte(input))); obtained != expected {
			t.Errorf("%q : obtenu %q, attendu %q", input, obtained, expected)
		}
	}
}

func TestPathRedactorSansHomeNeFaitRien(t *testing.T) {
	for _, home := range []string{"", "/"} {
		if newPathRedactor(home) != nil {
			t.Errorf("home %q : un masqueur a été créé", home)
		}
	}
}

func TestRedactWriterRetireLaLongueurEtMasque(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Length", "99")
	writer := guestRedactedWriter(recorder, newPathRedactor("/home/bob"))
	writer.WriteHeader(200)
	count, err := writer.Write([]byte(`{"p":"/home/bob/x"}`))
	if err != nil || count != len(`{"p":"/home/bob/x"}`) {
		t.Fatalf("écriture : %d %v", count, err)
	}
	if body := recorder.Body.String(); body != `{"p":"~/x"}` {
		t.Errorf("corps : %q", body)
	}
	if recorder.Header().Get("Content-Length") != "" {
		t.Error("Content-Length conservé")
	}
}
