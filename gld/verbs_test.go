package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// Effectif attendu par langue : 124 verbes d'origine, puis +500 (français), puis +500 (anglais).
var wantVerbs = map[string]int{"fr": 124, "en": 124}

var forbiddenVerbWords = regexp.MustCompile(`(?i)\b(heures?|sommeil|fatigue|dormir|bonne nuit|sleep|tired|bedtime|o'clock)\b`)

func TestVerbsQuality(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		verbs := defaultVerbs(lang)
		if len(verbs) != wantVerbs[lang] {
			t.Errorf("%s : %d verbes, attendu %d", lang, len(verbs), wantVerbs[lang])
		}
		seen := map[string]bool{}
		for _, v := range verbs {
			k := strings.ToLower(v)
			switch {
			case v == "" || v != strings.TrimSpace(v):
				t.Errorf("%s : verbe vide ou avec espace en bord : %q", lang, v)
			case utf8.RuneCountInString(v) > 60:
				t.Errorf("%s : trop long (%d) : %q", lang, utf8.RuneCountInString(v), v)
			case strings.HasSuffix(v, "."):
				t.Errorf("%s : point final : %q", lang, v)
			case seen[k]:
				t.Errorf("%s : doublon : %q", lang, v)
			case forbiddenVerbWords.MatchString(v):
				t.Errorf("%s : mot interdit : %q", lang, v)
			}
			seen[k] = true
		}
	}
	if wantVerbs["fr"] == wantVerbs["en"] && len(defaultVerbs("fr")) != len(defaultVerbs("en")) {
		t.Error("les deux langues doivent avoir le même effectif")
	}
}

func TestSpinnerVerbsLang(t *testing.T) {
	fr, en := spinnerVerbs("fr"), spinnerVerbs("en")
	if len(fr) == 0 || len(en) == 0 || fr[0] == en[0] {
		t.Fatalf("listes : %d, %d, %q / %q", len(fr), len(en), fr[0], en[0])
	}
	for _, lang := range []string{"fr", "en"} {
		cfg := t.TempDir()
		old := root
		t.Cleanup(func() { root = old })
		root = filepath.Join(cfg, "projects")
		base := len(defaultVerbs(lang))
		os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"spinnerVerbs":{"mode":"append","verbs":["à moi"," ","x"]}}`), 0o644)
		if got := spinnerVerbs(lang); len(got) != base+2 || got[len(got)-2] != "à moi" {
			t.Errorf("%s append : %d verbes", lang, len(got))
		}
		os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"spinnerVerbs":{"mode":"replace","verbs":["seul"]}}`), 0o644)
		if got := spinnerVerbs(lang); len(got) != 1 || got[0] != "seul" {
			t.Errorf("%s replace : %v", lang, got)
		}
	}
	var raw []string
	b, _ := localesFS.ReadFile("locales/verbs.en.json")
	if json.Unmarshal(b, &raw) != nil {
		t.Fatal("verbs.en.json illisible")
	}
}
