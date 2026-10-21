package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEcritureConfig(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "settings.json")
	orig := "{\n  \"model\": \"opus\",\n  \"hooks\": {\n    \"Stop\": []\n  },\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ls:*)\"\n    ]\n  },\n  \"theme\": \"dark\"\n}\n"
	put(t, p, orig, old)

	do := func(e CfgEdit) (string, error) {
		e.File = "u"
		_, after, err := configEdit(e)
		return after, err
	}
	// aperçu : rien n'est écrit
	after, err := do(CfgEdit{Op: "set", Path: []string{"model"}, Value: "sonnet", Dry: true})
	if err != nil || !strings.Contains(after, `"model": "sonnet"`) {
		t.Fatal(after, err)
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatal("l'aperçu a écrit")
	}
	// écriture : seule la ligne concernée change, le reste (ordre, hooks) est intact
	after, err = do(CfgEdit{Op: "set", Path: []string{"model"}, Value: "sonnet"})
	if err != nil || after != strings.Replace(orig, `"opus"`, `"sonnet"`, 1) {
		t.Fatalf("%q %v", after, err)
	}
	if b, _ := os.ReadFile(p); string(b) != after {
		t.Fatal("fichier non écrit")
	}
	if bs := listBackups(); len(bs) != 1 || bs[0].File != "u" {
		t.Fatalf("sauvegardes : %+v", bs)
	}
	// permissions : ajout, doublon refusé, retrait ; une liste vidée disparaît avec son conteneur
	if after, err = do(CfgEdit{Op: "add", Path: []string{"permissions", "allow"}, Value: "Bash(git status:*)"}); err != nil || !strings.Contains(after, "Bash(git status:*)") {
		t.Fatal(after, err)
	}
	if _, err = do(CfgEdit{Op: "add", Path: []string{"permissions", "allow"}, Value: "Bash(ls:*)"}); err == nil {
		t.Fatal("doublon accepté")
	}
	do(CfgEdit{Op: "remove", Path: []string{"permissions", "allow"}, Value: "Bash(git status:*)"})
	if after, err = do(CfgEdit{Op: "remove", Path: []string{"permissions", "allow"}, Value: "Bash(ls:*)"}); err != nil || strings.Contains(after, "permissions") {
		t.Fatal(after, err)
	}
	// variables d'environnement et plugins : créés puis retirés, conteneur compris
	do(CfgEdit{Op: "set", Path: []string{"env", "MA_VAR"}, Value: "1"})
	do(CfgEdit{Op: "set", Path: []string{"enabledPlugins", "a@b"}, Value: true})
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"MA_VAR": "1"`) || !strings.Contains(string(b), `"a@b": true`) {
		t.Fatal(string(b))
	}
	do(CfgEdit{Op: "delete", Path: []string{"env", "MA_VAR"}})
	if after, _ = do(CfgEdit{Op: "delete", Path: []string{"enabledPlugins", "a@b"}}); strings.Contains(after, "env") || strings.Contains(after, "enabledPlugins") {
		t.Fatal(after)
	}
	// nombres et booléens typés
	if _, err = do(CfgEdit{Op: "set", Path: []string{"cleanupPeriodDays"}, Value: float64(30)}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"cleanupPeriodDays": 30`) {
		t.Fatal(string(b))
	}
	// refus : types faux, chemins hors liste, projet inconnu, nom de variable invalide
	for _, e := range []CfgEdit{
		{Op: "set", Path: []string{"autoCompactEnabled"}, Value: "oui"},
		{Op: "set", Path: []string{"cleanupPeriodDays"}, Value: float64(-1)},
		{Op: "set", Path: []string{"hooks", "Stop"}, Value: "x"},
		{Op: "set", Path: []string{"mcpServers", "x"}, Value: "x"},
		{Op: "set", Path: []string{"apiKeyHelper"}, Value: "x"},
		{Op: "set", Path: []string{"env", "A B"}, Value: "x"},
		{Op: "set", Path: []string{"env", "A"}, Value: "ligne1\nligne2"},
		{Op: "add", Path: []string{"permissions", "allow"}, Value: ""},
		{Op: "set", Path: []string{"enabledPlugins", "sans-arobase"}, Value: true},
	} {
		if _, err := do(e); err == nil {
			t.Errorf("modification acceptée : %+v", e)
		}
	}
	if _, _, err := configEdit(CfgEdit{File: "p/-inconnu", Op: "set", Path: []string{"model"}, Value: "x"}); err == nil {
		t.Fatal("projet inconnu accepté")
	}
	// restauration : l'état d'avant revient, et l'état remplacé est sauvegardé
	first := listBackups()[len(listBackups())-1]
	if err := restoreBackup(first.Name); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatalf("restauration : %q", b)
	}
	for _, bad := range []string{"../x.json", "20200101-000000.000000__u.json.sh", "n'importe quoi"} {
		if restoreBackup(bad) == nil {
			t.Errorf("sauvegarde invalide acceptée : %q", bad)
		}
	}
}

// Un fichier dont la mise en forme n'est pas celle de Claude Code n'est pas réécrit.
func TestEcritureRefuseFormatParticulier(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "settings.json"), `{"model":"opus"}`+"\n", old)
	if _, _, err := configEdit(CfgEdit{File: "u", Op: "set", Path: []string{"model"}, Value: "x"}); err == nil {
		t.Fatal("fichier compact réécrit")
	}
}

// Un fichier absent est créé, avec ses dossiers.
func TestEcritureCreeLeFichier(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	if _, after, err := configEdit(CfgEdit{File: "l/-p", Op: "set", Path: []string{"theme"}, Value: "dark"}); err != nil || after != "{\n  \"theme\": \"dark\"\n}\n" {
		t.Fatalf("%q %v", after, err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSuggestionsPermissions(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	line := func(cmd string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"` + cmd + `"}}]}}` + "\n"
	}
	var b strings.Builder
	for i := 0; i < 4; i++ {
		b.WriteString(line("git status") + line("ls -la /tmp") + line("rm -rf x") + line("go test ./...") + line("cat a | grep b"))
		b.WriteString(line("git log --oneline") + line("sed -n 1,5p f") + line("find . -name x") + line("env A=1 ls") + line("git -c core.pager=x log") + line("go build ./..."))
	}
	b.WriteString(line("git status") + line("make build"))
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), b.String(), old)
	got := map[string]PermSuggest{}
	for _, s := range permSuggestions([]string{"Bash(go test:*)"}) {
		got[s.Rule] = s
	}
	if s := got["Bash(git status:*)"]; s.Count != 5 || !s.ReadOnly {
		t.Fatalf("git status : %+v", got)
	}
	if s := got["Bash(ls:*)"]; s.Count != 4 || !s.ReadOnly {
		t.Fatalf("ls : %+v", got)
	}
	// « git log » peut lancer un programme (--ext-diff, --textconv) : proposée, mais pas « lecture seule »
	if s := got["Bash(git log:*)"]; s.Count != 4 || s.ReadOnly {
		t.Fatalf("git log : %+v", s)
	}
	// « go build » exécute du code (cgo, générateurs) : proposée, mais pas « lecture seule »
	if s := got["Bash(go build:*)"]; s.Count != 4 || s.ReadOnly {
		t.Fatalf("go build : %+v", s)
	}
	for _, no := range []string{"Bash(rm:*)", "Bash(go test:*)", "Bash(cat:*)", "Bash(make build:*)", "Bash(sed:*)", "Bash(find:*)", "Bash(env:*)", "Bash(git:*)"} {
		if _, ok := got[no]; ok {
			t.Errorf("règle proposée à tort : %s", no)
		}
	}
}

// Un fichier de réglages (ou son dossier) qui est un lien symbolique n'est jamais écrit.
func TestEcritureRefuseLesLiens(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	cible := filepath.Join(t.TempDir(), "cible.json")
	put(t, cible, "{\n  \"model\": \"opus\"\n}\n", old)
	if err := os.Symlink(cible, filepath.Join(cfg, "settings.json")); err != nil {
		t.Skip("liens symboliques indisponibles")
	}
	if _, _, err := configEdit(CfgEdit{File: "u", Op: "set", Path: []string{"model"}, Value: "x"}); err == nil || !strings.Contains(err.Error(), "lien symbolique") {
		t.Fatal("écriture à travers un lien :", err)
	}
	if b, _ := os.ReadFile(cible); !strings.Contains(string(b), "opus") {
		t.Fatal("la cible du lien a été modifiée")
	}
	// un fichier temporaire prévisible ne peut plus détourner l'écriture
	cfg2 := t.TempDir()
	use(cfg2)
	put(t, filepath.Join(cfg2, "settings.json"), "{\n  \"model\": \"opus\"\n}\n", old)
	os.Symlink(cible, filepath.Join(cfg2, "settings.json.gld-web.tmp"))
	if _, _, err := configEdit(CfgEdit{File: "u", Op: "set", Path: []string{"model"}, Value: "sonnet"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(cible); !strings.Contains(string(b), "opus") {
		t.Fatal("la cible d'un faux fichier temporaire a été écrasée")
	}
}

// Les appels à claude sans projet se font dans un dossier neuf et privé, pas dans /tmp.
func TestDossierDeTravailPrive(t *testing.T) {
	d := workDir()
	if d == os.TempDir() || d == "" {
		t.Fatal("dossier partagé :", d)
	}
	if st, err := os.Stat(d); err != nil || st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("dossier non privé : %v %v", st, err)
	}
}

func TestEditionJSONBrut(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "settings.json")
	orig := "{\n  \"model\": \"opus\",\n  \"env\": {\n    \"API_KEY\": \"secret-xyz\",\n    \"X\": \"1\"\n  }\n}\n"
	put(t, p, orig, old)

	text, err := configRawRead("u")
	if err != nil || strings.Contains(text, "secret-xyz") || !strings.Contains(text, maskText) {
		t.Fatalf("lecture : %q %v", text, err)
	}
	// modification en laissant le masque : le secret d'origine est conservé
	edited := strings.Replace(text, `"opus"`, `"sonnet"`, 1)
	before, after, _, err := configRawEdit("u", edited, true)
	if err != nil || before == after || strings.Contains(after, "secret-xyz") {
		t.Fatal(before, after, err)
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatal("l'aperçu a écrit")
	}
	if _, _, _, err := configRawEdit("u", edited, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"sonnet"`) || !strings.Contains(string(b), "secret-xyz") || strings.Contains(string(b), maskText) {
		t.Fatalf("secret perdu ou masque écrit : %s", b)
	}
	if len(listBackups()) != 1 {
		t.Fatal("pas de sauvegarde")
	}
	// erreurs : syntaxe (avec position), racine non objet, masque sans original, texte vide
	_, _, _, err = configRawEdit("u", "{\n  \"a\": 1,\n  \"b\": \n}", true)
	if err == nil || !strings.Contains(err.Error(), "ligne 4") {
		t.Fatalf("position de l'erreur : %v", err)
	}
	for _, bad := range []string{`[1,2]`, `{"nouveau": "` + maskText + `"}`, ``, `{"a":1} x`} {
		if _, _, _, err := configRawEdit("u", bad, true); err == nil {
			t.Errorf("texte accepté : %q", bad)
		}
	}
	// avertissements de santé sur le nouveau contenu
	_, _, warns, err := configRawEdit("u", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"programme-introuvable-xyz"}]}]}}`, true)
	if err != nil || len(warns) == 0 || warns[0].Kind != "hook" {
		t.Fatalf("avertissements : %+v %v", warns, err)
	}
	// un fichier absent peut être créé
	cfg2 := t.TempDir()
	use(cfg2)
	if _, _, _, err := configRawEdit("u", `{"theme":"dark"}`, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg2, "settings.json")); string(b) != "{\n  \"theme\": \"dark\"\n}\n" {
		t.Fatalf("%q", b)
	}
}

func TestImportPermissions(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "settings.json")
	orig := "{\n  \"model\": \"opus\",\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ls:*)\"\n    ],\n    \"defaultMode\": \"auto\"\n  }\n}\n"
	put(t, p, orig, old)
	frag := `{"permissions":{"allow":["Bash(ls:*)","Bash(git status:*)"," Bash(git status:*) "],"deny":["Bash(rm:*)"],"defaultMode":"bypassPermissions"}}`

	// fusion : ce qui manque est ajouté, le reste (autre clé, defaultMode) ne bouge pas, defaultMode importé est ignoré
	_, after, _, sum, err := permImport("u", frag, "merge", true)
	if err != nil || sum != "2 règle(s) ajoutée(s), 0 retirée(s)" {
		t.Fatal(sum, err)
	}
	for _, want := range []string{`"Bash(git status:*)"`, `"deny"`, `"Bash(rm:*)"`, `"defaultMode": "auto"`, `"model": "opus"`} {
		if !strings.Contains(after, want) {
			t.Errorf("manque %s dans :\n%s", want, after)
		}
	}
	if strings.Contains(after, "bypassPermissions") {
		t.Fatal("defaultMode importé")
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatal("l'aperçu a écrit")
	}
	if _, _, _, _, err := permImport("u", frag, "merge", false); err != nil {
		t.Fatal(err)
	}
	if len(listBackups()) != 1 {
		t.Fatal("pas de sauvegarde")
	}
	// remplacement : les listes présentes dans le fragment sont remplacées, les autres gardées
	_, _, _, sum, err = permImport("u", `{"allow":["Bash(cat:*)"]}`, "replace", false)
	if err != nil || sum != "1 règle(s) ajoutée(s), 2 retirée(s)" {
		t.Fatal(sum, err)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "git status") || !strings.Contains(string(b), "Bash(cat:*)") || !strings.Contains(string(b), "Bash(rm:*)") {
		t.Fatalf("remplacement :\n%s", b)
	}
	// une liste importée vide retire la catégorie ; un avertissement de santé accompagne une règle trop large
	_, _, warns, _, err := permImport("u", `{"allow":["Bash"],"deny":[]}`, "replace", true)
	if err != nil || len(warns) == 0 {
		t.Fatalf("avertissement attendu : %+v %v", warns, err)
	}
	// refus
	for _, bad := range []string{``, `[1]`, `{"permissions":1}`, `{"autre":1}`, `{"allow":"x"}`, `{"allow":[1]}`, `{"allow":["a\nb"]}`, `{"allow":[""]}`, `pas du json`} {
		if _, _, _, _, err := permImport("u", bad, "merge", true); err == nil {
			t.Errorf("import accepté : %q", bad)
		}
	}
	if _, _, _, _, err := permImport("u", frag, "autre", true); err == nil {
		t.Fatal("mode inconnu accepté")
	}
	if _, _, _, _, err := permImport("p/-inconnu", frag, "merge", true); err == nil {
		t.Fatal("projet inconnu accepté")
	}
}
