package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// withSettings pointe cfgDir vers un dossier temporaire et y écrit settings.json (omis si content est vide).
func withSettings(t *testing.T, content string) string {
	t.Helper()
	cfg := t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	root = filepath.Join(cfg, "projects")
	if content != "" {
		if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

// noOverride suspend la langue forcée par i18n_init_test.go.
func noOverride(t *testing.T) {
	t.Helper()
	old := langOverride
	langOverride = ""
	t.Cleanup(func() { langOverride = old })
}

func TestNormalizeLang(t *testing.T) {
	for in, want := range map[string]string{
		"fr": "fr", "FR-fr": "fr", "French": "fr", "Français": "fr",
		"en-US": "en", "English": "en", "en": "en",
		"Spanish": "other", "日本語": "other",
		"": "", "  ": "",
	} {
		if got := normalizeLang(in); got != want {
			t.Errorf("normalizeLang(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestUserLanguageRaw(t *testing.T) {
	noOverride(t)
	for name, c := range map[string]struct{ content, want string }{
		"absent":        {"", ""},
		"json invalide": {"{pas du json", ""},
		"non texte":     {`{"language":3}`, ""},
		"anglais":       {`{"language":"English"}`, "English"},
	} {
		withSettings(t, c.content)
		if got := userLanguageRaw(); got != c.want {
			t.Errorf("%s : %q, attendu %q", name, got, c.want)
		}
	}
	cfg := withSettings(t, `{"language":"French"}`)
	if userLanguageRaw() != "French" {
		t.Fatal("première lecture")
	}
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"language":"Spanish!"}`), 0o644)
	if got := userLanguageRaw(); got != "Spanish!" {
		t.Fatalf("après réécriture : %q", got)
	}
}

func TestUiLang(t *testing.T) {
	noOverride(t)
	for content, want := range map[string]string{
		`{"language":"Spanish"}`: "en",
		`{"language":"French"}`:  "fr",
		`{"language":"en"}`:      "en",
		`{}`:                     "fr",
	} {
		withSettings(t, content)
		if got := uiLang(); got != want {
			t.Errorf("%s : %q, attendu %q", content, got, want)
		}
	}
}

func TestCliLang(t *testing.T) {
	noOverride(t)
	withSettings(t, `{}`)
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := cliLang(); got != "en" {
		t.Fatalf("LANG anglais : %q", got)
	}
	withSettings(t, `{"language":"French"}`)
	if got := cliLang(); got != "fr" {
		t.Fatalf("le réglage prime sur LANG : %q", got)
	}
	withSettings(t, `{}`)
	t.Setenv("LANG", "C")
	if got := cliLang(); got != "fr" {
		t.Fatalf("LANG=C : %q", got)
	}
}

func TestReplyLang(t *testing.T) {
	noOverride(t)
	withSettings(t, `{"language":"Spanish"}`)
	if got := replyLang(); got != "Spanish" {
		t.Fatalf("%q", got)
	}
	withSettings(t, `{}`)
	if got := replyLang(); got != "fr" {
		t.Fatalf("%q", got)
	}
}

func TestTrPlural(t *testing.T) {
	for _, c := range []struct {
		lang string
		n    int
		want string
	}{
		{"fr", 0, "0 élément"}, {"fr", 1, "1 élément"}, {"fr", 2, "2 éléments"},
		{"en", 0, "0 items"}, {"en", 1, "1 item"}, {"en", 2, "2 items"},
	} {
		if got := tr(c.lang, "test.items", map[string]any{"Count": c.n}); got != c.want {
			t.Errorf("%s %d : %q, attendu %q", c.lang, c.n, got, c.want)
		}
	}
	if got := tr("en", "inconnu.id", nil); got != "inconnu.id" {
		t.Fatalf("identifiant inconnu : %q", got)
	}
}

func TestCatalogSymmetry(t *testing.T) {
	fr, en := trKeys("fr"), trKeys("en")
	if len(fr) == 0 || !reflect.DeepEqual(fr, en) {
		t.Fatalf("clés différentes\nfr : %v\nen : %v", fr, en)
	}
	for _, l := range []string{"fr", "en"} {
		for _, k := range trKeys(l) {
			if tr(l, k, map[string]any{"Count": 2}) == "" {
				t.Errorf("%s : %s vide", l, k)
			}
		}
	}
}

func TestApiLang(t *testing.T) {
	noOverride(t)
	h, _, _ := web(t)
	withSettings(t, `{"language":"Spanish"}`)
	if got := status(t, h, "GET", "/api", "", 200)["lang"]; got != "other" {
		t.Fatalf("lang = %v", got)
	}
}

func setLangT(t *testing.T, l string) {
	t.Helper()
	old := langOverride
	langOverride = l
	t.Cleanup(func() { langOverride = old })
}

func TestErrOrLang(t *testing.T) {
	setLangT(t, "en")
	if got := errOr(errors.New("x")); got != "failed: x" {
		t.Fatalf("en : %q", got)
	}
	if errOr(nil) != "ok" {
		t.Fatal("ok")
	}
	setLangT(t, "fr")
	if got := errOr(errors.New("x")); got != "échec : x" {
		t.Fatalf("fr : %q", got)
	}
}

func TestRmSummaryLang(t *testing.T) {
	for _, c := range []struct {
		lang      string
		n, spared int
		want      string
	}{
		{"fr", 0, 0, "Aucune session supprimée"},
		{"fr", 1, 0, "1 session supprimée"},
		{"fr", 2, 1, "2 sessions supprimées ; 1 session ouverte épargnée"},
		{"fr", 1, 2, "1 session supprimée ; 2 sessions ouvertes épargnées"},
		{"en", 0, 0, "No session deleted"},
		{"en", 1, 0, "1 session deleted"},
		{"en", 2, 1, "2 sessions deleted; 1 open session spared"},
		{"en", 1, 2, "1 session deleted; 2 open sessions spared"},
	} {
		setLangT(t, c.lang)
		if got := rmSummary(c.n, c.spared); got != c.want {
			t.Errorf("%s %d/%d : %q, attendu %q", c.lang, c.n, c.spared, got, c.want)
		}
	}
}

func TestTerrFollowsLangAndWraps(t *testing.T) {
	base := errors.New("boom")
	e := terr("srv.sendkeys.fermetureImpossible", "W1", base)
	setLangT(t, "fr")
	if e.Error() != "fermeture impossible : boom" {
		t.Fatalf("fr : %q", e.Error())
	}
	setLangT(t, "en")
	if e.Error() != "cannot close: boom" {
		t.Fatalf("en : %q", e.Error())
	}
	if !errors.Is(e, base) {
		t.Fatal("l'erreur enveloppée doit rester trouvable")
	}
}

func TestImportSnippetsSkipsExistingInAnyLang(t *testing.T) {
	use(t.TempDir())
	for _, lang := range []string{"fr", "en"} {
		setLangT(t, lang)
		name := "doublon-" + lang + ".md"
		files := []ImportFile{{name, []byte("contenu")}}
		if done, _, err := importSnippets("", files, false); err != nil || len(done) != 1 {
			t.Fatalf("%s : premier import %v %v", lang, done, err)
		}
		if done, skipped, err := importSnippets("", files, false); err != nil || len(done) != 0 || len(skipped) != 1 {
			t.Fatalf("%s : second import %v %v %v", lang, done, skipped, err)
		}
	}
}

func TestMemorySkillMessagesLang(t *testing.T) {
	for lang, want := range map[string]string{"fr": "skill invalide", "en": "invalid skill"} {
		setLangT(t, lang)
		if got := tr(lang, "srv.skills.skillInvalide", nil); got != want {
			t.Errorf("%s : %q", lang, got)
		}
		if got := L("srv.memmeta.indexTooLong", "A1", 250, "A2", 200); !strings.Contains(got, "250") || !strings.Contains(got, "200") {
			t.Errorf("%s : %q", lang, got)
		}
	}
	setLangT(t, "en")
	if got := L("srv.memmeta.stale"); got != "not modified for over six months" {
		t.Fatal(got)
	}
}

func TestActivityLocalizedAndFlagged(t *testing.T) {
	for lang, want := range map[string]string{"fr": "réfléchit au résultat", "en": "is thinking about the result"} {
		setLangT(t, lang)
		if got := localizeActivity("réfléchit au résultat"); got != want {
			t.Errorf("%s : %q", lang, got)
		}
	}
	setLangT(t, "en")
	if got := localizeActivity("exécute Bash : ls"); got != "runs Bash : ls" {
		t.Fatal(got)
	}
	if !isGenericActivity("travaille") || isGenericActivity("attend votre message") || isGenericActivity("exécute Bash") {
		t.Fatal("états génériques")
	}
	if got := localizeActivity("libre"); got != "libre" {
		t.Fatal("un texte inconnu reste tel quel")
	}
}

func TestConfigMessagesLang(t *testing.T) {
	setLangT(t, "en")
	if got := L("srv.configcheck.dupRule", "A1", "allow", "A2", "Bash(ls)"); got != "duplicate rule in allow: Bash(ls)" {
		t.Fatal(got)
	}
	if got := summarize(&omap{keys: []string{"a", "b"}}); got != "{2 key(s)}" {
		t.Fatal(got)
	}
	setLangT(t, "fr")
	if got := summarize(&omap{keys: []string{"a", "b"}}); got != "{2 clé(s)}" {
		t.Fatal(got)
	}
}

func TestMatinOutputLang(t *testing.T) {
	use(t.TempDir())
	setLangT(t, "en")
	out, err := run(t, cliMatin, "-h")
	if err != nil && !errors.Is(err, errHelp) {
		t.Fatal(err)
	}
	if !strings.Contains(out, "reopens each Claude topic") || strings.Contains(out, "rouvre chaque sujet") {
		t.Fatalf("aide en anglais : %q", out[:80])
	}
	if _, err := run(t, cliMatin, "--nimporte"); err == nil || err.Error() != "unknown option: --nimporte" {
		t.Fatalf("option inconnue en anglais : %v", err)
	}
	if out, _ := run(t, cliMatin, "--term", "list"); !strings.Contains(out, "launchers available here:") {
		t.Fatalf("lanceurs : %q", out)
	}
	setLangT(t, "fr")
	if out, _ := run(t, cliMatin, "-h"); !strings.Contains(out, "rouvre chaque sujet") {
		t.Fatalf("aide en français : %q", out[:80])
	}
	if _, err := run(t, cliMatin, "--nimporte"); err == nil || err.Error() != "option inconnue : --nimporte" {
		t.Fatalf("option inconnue en français : %v", err)
	}
}

func TestInvalidRequestLang(t *testing.T) {
	h, _, _ := web(t)
	for lang, want := range map[string]string{"fr": "requête invalide", "en": "invalid request"} {
		setLangT(t, lang)
		if got := status(t, h, "POST", "/rm", "pas du json", 400)["out"]; got != want {
			t.Errorf("%s : %v", lang, got)
		}
	}
}
