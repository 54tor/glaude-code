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
	v, err := parseOrdered([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got := formatOrdered(v); got != doc {
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
	v, _ := parseOrdered([]byte(`{"env":{"API_KEY":"abc","PATH":"/bin"},"headers":{"Authorization":"Bearer x"},"n":["token"]}`))
	out := formatOrdered(maskSecrets(v, ""))
	if strings.Contains(out, "abc") || strings.Contains(out, "Bearer") || !strings.Contains(out, "/bin") {
		t.Fatal(out)
	}
}

// Le vrai fichier de l'utilisateur (s'il existe) doit survivre à l'aller-retour : sinon une écriture le reformaterait.
func TestFichierUtilisateurAllerRetour(t *testing.T) {
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(home + "/.claude/settings.json")
	if err != nil {
		t.Skip("pas de settings.json")
	}
	v, err := parseOrdered(b)
	if err != nil {
		t.Skip("JSON invalide")
	}
	if got := formatOrdered(v); got != strings.TrimRight(string(b), "\n") {
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
	for _, i := range configHealth(files, raw) {
		kinds = append(kinds, i.Kind+":"+i.Msg)
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
	if h := hooksOf(raw); len(h) != 1 || h[0].Event != "Stop" || h[0].Missing == "" {
		t.Fatalf("%+v", h)
	}
}

func TestPrecedence(t *testing.T) {
	u, _ := parseOrdered([]byte(`{"model":"opus","permissions":{"allow":["A"]},"env":{"X":"1"}}`))
	p, _ := parseOrdered([]byte(`{"model":"sonnet","permissions":{"allow":["B"]}}`))
	l, _ := parseOrdered([]byte(`{"model":"haiku"}`))
	rows := effective(map[string]any{"u": u, "p/-a": p, "l/-a": l}, "-a")
	by := map[string]EffRow{}
	for _, r := range rows {
		by[r.Path] = r
	}
	if m := by["model"]; m.Winner != "l" || m.Values["u"] != "opus" || m.Values["p"] != "sonnet" {
		t.Fatalf("model : %+v", m)
	}
	if a := by["permissions.allow[]"]; !a.Merged || a.Values["u"] != "A" || a.Values["p"] != "B" {
		t.Fatalf("liste : %+v", a)
	}
	if e := by["env.X"]; e.Winner != "u" {
		t.Fatalf("env : %+v", e)
	}
	if _, ok := effective(map[string]any{"u": u}, "")[0], true; !ok {
		t.Fatal()
	}
}
