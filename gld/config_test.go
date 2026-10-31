package main

import (
	"os"
	"strings"
	"testing"
)

// Écrire un fichier lu ne change pas un octet : ordre des clés, indentation, accents, nombres, vides.
func TestJSONOrdonneAllerRetour(t *testing.T) {
	doc := `{
  "z": 1,
  "a": {
    "b": [],
    "c": {},
    "d": [
      "é < > & \"x\"",
      3650000,
      1.5,
      true,
      null
    ]
  },
  "m": "Français"
}`
	value, err := parseOrdered([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got := formatOrdered(value); got != doc {
		t.Fatalf("aller-retour différent :\n%s", got)
	}
	if _, err := parseOrdered([]byte(`{"a":1} x`)); err == nil {
		t.Fatal("texte après le document accepté")
	}
	if _, err := parseOrdered([]byte(`{"a":`)); err == nil {
		t.Fatal("JSON tronqué accepté")
	}
}

func TestMasquage(t *testing.T) {
	value, _ := parseOrdered([]byte(`{"env":{"API_KEY":"abc","PATH":"/bin"},"headers":{"Authorization":"Bearer x"},"n":["token"]}`))
	out := formatOrdered(maskSecrets(value, ""))
	if strings.Contains(out, "abc") || strings.Contains(out, "Bearer") || !strings.Contains(out, "/bin") {
		t.Fatal(out)
	}
}

// Le vrai fichier de l'utilisateur (s'il existe) doit survivre à l'aller-retour : sinon une écriture le reformaterait.
func TestFichierUtilisateurAllerRetour(t *testing.T) {
	if os.Getenv("GLD_TEST_REAL_HOME") == "" {
		t.Skip("lit le vrai ~/.claude/settings.json : GLD_TEST_REAL_HOME=1 pour l'exercer")
	}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(home + "/.claude/settings.json")
	if err != nil {
		t.Skip("pas de settings.json")
	}
	value, err := parseOrdered(data)
	if err != nil {
		t.Skip("JSON invalide")
	}
	if got := formatOrdered(value); got != strings.TrimRight(string(data), "\n") {
		t.Fatal("le fichier serait reformaté à l'écriture")
	}
}

func TestHookCommandesManquantes(t *testing.T) {
	home, _ := os.UserHomeDir()
	ok := home + "/.local/bin" // n'importe quel dossier qui existe
	if _, err := os.Stat(ok); err != nil {
		ok = home
	}
	for cmd, want := range map[string]string{
		"python3 $HOME/inexistant-xyz/guard.py":                                       home + "/inexistant-xyz/guard.py",
		"[ -x \"$HOME/nulle-part-xyz\" ] && \"$HOME/nulle-part-xyz\" _loaded; exit 0": home + "/nulle-part-xyz",
		"programme-introuvable-xyz --option":                                          "programme-introuvable-xyz",
		"echo ok && ls " + ok:                                                         "",
		"ls -la \"$VARIABLE_INCONNUE/x\"":                                             "",
		"curl https://exemple.org/x.sh":                                               "",
		"true; exit 0":                                                                "",
		"./scripts/inexistant-xyz.sh":                                                 "", // relatif au dossier du projet, inconnu ici
	} {
		if got := missingPart(cmd); got != want {
			t.Errorf("missingPart(%q) = %q ; attendu %q", cmd, got, want)
		}
	}
}

func TestSanteConfig(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, cfg+"/settings.json", `{
  "permissions": {"allow": ["Bash(ls:*)", "Bash(ls:*)", "Bash", "Edit(x)"], "deny": ["Edit(x)"]},
  "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "programme-introuvable-xyz"}]}]},
  "enabledPlugins": {"inconnu@nulle-part": true, "desactive@nulle-part": false}
}`, old)
	put(t, cfg+"/plugins/installed_plugins.json", `{"version":2,"plugins":{"autre@x":[]}}`, old)
	files, raw := allConfigs()
	if len(files) != 1 || files[0].ID != "u" || files[0].Error != "" {
		t.Fatalf("%+v", files)
	}
	var kinds []string
	for _, cfgIssue := range configHealth(files, raw) {
		kinds = append(kinds, cfgIssue.Kind+":"+cfgIssue.Msg)
	}
	all := strings.Join(kinds, "\n")
	for _, want := range []string{"en double dans allow : Bash(ls:*)", "trop large dans allow : « Bash »", "à la fois autorisée et refusée", "hook Stop", "plugin activé mais pas installé : inconnu@nulle-part"} {
		if !strings.Contains(all, want) {
			t.Errorf("manque %q dans :\n%s", want, all)
		}
	}
	if strings.Contains(all, "desactive@") {
		t.Error("plugin désactivé signalé")
	}
	if hookInfos := hooksOf(raw); len(hookInfos) != 1 || hookInfos[0].Event != "Stop" || hookInfos[0].Missing == "" {
		t.Fatalf("%+v", hookInfos)
	}
}

func TestPrecedence(t *testing.T) {
	value, _ := parseOrdered([]byte(`{"model":"opus","permissions":{"allow":["A"]},"env":{"X":"1"}}`))
	localValue, _ := parseOrdered([]byte(`{"model":"sonnet","permissions":{"allow":["B"]}}`))
	anotherValue, _ := parseOrdered([]byte(`{"model":"haiku"}`))
	rows := effective(map[string]any{"u": value, "p/-a": localValue, "l/-a": anotherValue}, "-a")
	by := map[string]EffRow{}
	for _, effRow := range rows {
		by[effRow.Path] = effRow
	}
	if effRow := by["model"]; effRow.Winner != "l" || effRow.Values["u"] != "opus" || effRow.Values["p"] != "sonnet" {
		t.Fatalf("model : %+v", effRow)
	}
	if effRow := by["permissions.allow[]"]; !effRow.Merged || effRow.Values["u"] != "A" || effRow.Values["p"] != "B" {
		t.Fatalf("liste : %+v", effRow)
	}
	if effRow := by["env.X"]; effRow.Winner != "u" {
		t.Fatalf("env : %+v", effRow)
	}
	if _, ok := effective(map[string]any{"u": value}, "")[0], true; !ok {
		t.Fatal()
	}
}
