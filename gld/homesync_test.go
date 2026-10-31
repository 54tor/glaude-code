package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syncEnv(t *testing.T) (src, dst string) {
	cfg := t.TempDir()
	use(cfg)
	src, dst = t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "Pro", Dir: src}, {Name: "Perso", Dir: dst}}); err != nil {
		t.Fatal(err)
	}
	return
}

func syncItem(items []SyncItem, id string) (SyncItem, bool) {
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return SyncItem{}, false
}

func TestSyncPlan(t *testing.T) {
	src, dst := syncEnv(t)
	put(t, filepath.Join(src, "settings.json"), "{\n  \"model\": \"opus\",\n  \"theme\": \"dark\",\n  \"env\": {\n    \"X\": \"1\"\n  },\n  \"apiKeyHelper\": \"x\",\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ls:*)\",\n      \"Bash(git status:*)\"\n    ],\n    \"defaultMode\": \"auto\"\n  },\n  \"hooks\": {\n    \"Stop\": []\n  }\n}\n", old)
	put(t, filepath.Join(dst, "settings.json"), "{\n  \"theme\": \"dark\",\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ls:*)\"\n    ]\n  }\n}\n", old)
	put(t, filepath.Join(src, "CLAUDE.md"), "regles\n", old)
	put(t, filepath.Join(src, "rules", "a.md"), "a\n", old)
	put(t, filepath.Join(dst, "rules", "a.md"), "autre\n", old)
	put(t, filepath.Join(src, "skills", "deploy", "SKILL.md"), "x\n", old)

	items, err := syncPlan("Pro", "Perso")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"set:model": "new", "set:theme": "same", "set:hooks": "new", "perm:allow": "diff", "file:CLAUDE.md": "new", "file:rules/a.md": "diff", "skill:deploy": "new"}
	for id, st := range want {
		if it, ok := syncItem(items, id); !ok || it.Status != st {
			t.Errorf("%s : %+v", id, it)
		}
	}
	for _, no := range []string{"set:env", "set:apiKeyHelper", "set:permissions"} {
		if _, ok := syncItem(items, no); ok {
			t.Errorf("%s proposé à tort", no)
		}
	}
	if it, _ := syncItem(items, "set:hooks"); it.Risk != "exec" {
		t.Errorf("hooks sans avertissement : %+v", it)
	}
	if _, err := syncPlan("Pro", "Pro"); err == nil {
		t.Error("source = cible accepté")
	}
	if _, err := syncPlan("Pro", "Inconnu"); err == nil {
		t.Error("dossier inconnu accepté")
	}
}

func TestSyncApply(t *testing.T) {
	src, dst := syncEnv(t)
	put(t, filepath.Join(src, "settings.json"), "{\n  \"model\": \"opus\",\n  \"theme\": \"light\",\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ls:*)\",\n      \"Bash(git status:*)\"\n    ]\n  }\n}\n", old)
	put(t, filepath.Join(dst, "settings.json"), "{\n  \"theme\": \"dark\",\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(cat:*)\"\n    ]\n  }\n}\n", old)
	put(t, filepath.Join(src, "rules", "a.md"), "nouvelle\n", old)
	put(t, filepath.Join(dst, "rules", "a.md"), "ancienne\n", old)
	put(t, filepath.Join(src, "skills", "deploy", "SKILL.md"), "skill\n", old)

	out, err := syncApply("Pro", "Perso", []SyncPick{
		{"set:model", "replace"}, {"perm:allow", "merge"}, {"file:rules/a.md", "replace"}, {"skill:deploy", "replace"},
		{"set:theme", "merge"},      // valeur simple déjà présente : « fusionner » n'existe pas, remplace
		{"set:env", "replace"},      // absent du plan : ignoré
		{"file:../evil", "replace"}, // idem
	})
	if err != nil {
		t.Fatal(err, out)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "settings.json"))
	text := string(data)
	for _, want := range []string{`"model": "opus"`, `"theme": "light"`, `"Bash(cat:*)"`, `"Bash(ls:*)"`, `"Bash(git status:*)"`} {
		if !strings.Contains(text, want) {
			t.Errorf("manque %s dans :\n%s", want, text)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "rules", "a.md")); string(data) != "nouvelle\n" {
		t.Errorf("règle non remplacée : %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "skills", "deploy", "SKILL.md")); string(data) != "skill\n" {
		t.Errorf("skill non copié : %q", data)
	}
	// le fichier remplacé est sauvegardé, et l'ancien contenu y est
	var found bool
	filepath.WalkDir(filepath.Join(dst, "gld-web-sync-backups"), func(path string, value os.DirEntry, err error) error {
		if err == nil && !value.IsDir() && filepath.Base(path) == "a.md" {
			data, _ := os.ReadFile(path)
			found = string(data) == "ancienne\n"
		}
		return nil
	})
	if !found {
		t.Error("règle remplacée non sauvegardée")
	}
	if _, err := os.Stat(filepath.Join(dst, "evil")); err == nil {
		t.Error("chemin hors plan écrit")
	}
	// seconde passe : tout est identique, rien à copier
	if items, _ := syncPlan("Pro", "Perso"); func() bool { it, _ := syncItem(items, "set:model"); return it.Status != "same" }() {
		t.Error("le plan ne voit pas la copie")
	}
}

// Fusion d'un objet : les clés de la source qui manquent sont ajoutées, celles de la cible gardent leur valeur.
func TestSyncMergeObjet(t *testing.T) {
	src, dst := syncEnv(t)
	put(t, filepath.Join(src, "settings.json"), "{\n  \"enabledPlugins\": {\n    \"a@m\": true,\n    \"b@m\": true\n  }\n}\n", old)
	put(t, filepath.Join(dst, "settings.json"), "{\n  \"enabledPlugins\": {\n    \"a@m\": false\n  }\n}\n", old)
	if _, err := syncApply("Pro", "Perso", []SyncPick{{"set:enabledPlugins", "merge"}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "settings.json"))
	if text := string(data); !strings.Contains(text, `"a@m": false`) || !strings.Contains(text, `"b@m": true`) {
		t.Fatalf("%s", text)
	}
}

// Un settings.json de cible mis en forme autrement n'est pas réécrit.
func TestSyncRefuseFormatParticulier(t *testing.T) {
	src, dst := syncEnv(t)
	put(t, filepath.Join(src, "settings.json"), "{\n  \"model\": \"opus\"\n}\n", old)
	put(t, filepath.Join(dst, "settings.json"), `{"theme":"dark"}`+"\n", old)
	if _, err := syncApply("Pro", "Perso", []SyncPick{{"set:model", "replace"}}); err == nil {
		t.Fatal("fichier compact réécrit")
	}
}

// Remplacer un skill rend la cible identique à la source : un fichier absent de la source part dans la sauvegarde.
func TestSyncSkillRemplace(t *testing.T) {
	src, dst := syncEnv(t)
	put(t, filepath.Join(src, "skills", "deploy", "SKILL.md"), "neuf\n", old)
	put(t, filepath.Join(dst, "skills", "deploy", "SKILL.md"), "ancien\n", old)
	put(t, filepath.Join(dst, "skills", "deploy", "notes.md"), "en trop\n", old)
	if _, err := syncApply("Pro", "Perso", []SyncPick{{"skill:deploy", "replace"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "skills", "deploy", "notes.md")); err == nil {
		t.Error("fichier absent de la source resté dans la cible")
	}
	saved, _ := filepath.Glob(filepath.Join(dst, "gld-web-sync-backups", "*", "skills", "deploy", "notes.md"))
	if len(saved) != 1 {
		t.Error("fichier retiré non sauvegardé")
	}
	if items, _ := syncPlan("Pro", "Perso"); func() bool { it, _ := syncItem(items, "skill:deploy"); return it.Status != "same" }() {
		t.Error("skill encore différent après remplacement")
	}
}

// Égalité d'ensembles : l'ordre et les doublons ne comptent pas, mais un élément d'un côté absent de l'autre, si.
func TestSameSet(t *testing.T) {
	for _, record := range []struct {
		a, b []string
		want bool
	}{
		{[]string{"x", "y"}, []string{"y", "x"}, true},
		{[]string{"x", "y"}, []string{"x", "x"}, false},
		{[]string{"x", "x"}, []string{"x", "y"}, false},
		{[]string{"x", "x"}, []string{"x"}, true},
		{nil, []string{}, true},
		{[]string{"x"}, nil, false},
	} {
		if got := sameSet(record.a, record.b); got != record.want {
			t.Errorf("sameSet(%q, %q) = %v", record.a, record.b, got)
		}
	}
}

func TestSyncSkillGardeLeBitExecutable(t *testing.T) {
	src, dst := syncEnv(t)
	put(t, filepath.Join(src, "skills", "run", "SKILL.md"), "x\n", old)
	put(t, filepath.Join(src, "skills", "run", "scripts", "go.sh"), "echo\n", old)
	os.Chmod(filepath.Join(src, "skills", "run", "scripts", "go.sh"), 0o755)
	if _, err := syncApply("Pro", "Perso", []SyncPick{{"skill:run", "replace"}}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dst, "skills", "run", "scripts", "go.sh")); err != nil || st.Mode()&0o100 == 0 {
		t.Fatalf("script synchronisé sans bit exécutable : %v %v", st, err)
	}
	if st, _ := os.Stat(filepath.Join(dst, "skills", "run", "SKILL.md")); st.Mode()&0o111 != 0 {
		t.Fatal("SKILL.md rendu exécutable")
	}
}

func TestSyncDeuxFoisLaMemeSeconde(t *testing.T) {
	src, dst := syncEnv(t)
	put(t, filepath.Join(src, "skills", "d", "SKILL.md"), "v1\n", old)
	put(t, filepath.Join(dst, "skills", "d", "SKILL.md"), "original\n", old)
	for index := 0; index < 2; index++ {
		if _, err := syncApply("Pro", "Perso", []SyncPick{{"skill:d", "replace"}}); err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(src, "skills", "d", "SKILL.md"), "v2\n", old)
	}
	saved, _ := filepath.Glob(filepath.Join(dst, "gld-web-sync-backups", "*", "skills", "d", "SKILL.md"))
	found := false
	for _, path := range saved {
		data, _ := os.ReadFile(path)
		found = found || string(data) == "original\n"
	}
	if len(saved) != 2 || !found {
		t.Fatalf("sauvegardes : %v, original gardé : %v", saved, found)
	}
}

func TestSyncPlanDirsSkipPlugin(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	put(t, filepath.Join(src, "settings.json"), "{\n  \"enabledPlugins\": {\n    \"a@m\": true\n  },\n  \"extraKnownMarketplaces\": {\n    \"m\": {}\n  },\n  \"theme\": \"light\"\n}\n", old)
	items, err := syncPlanDirs(src, dst, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := syncItem(items, "set:theme"); !ok {
		t.Error("set:theme attendu")
	}
	for _, id := range []string{"set:enabledPlugins", "set:extraKnownMarketplaces"} {
		if _, ok := syncItem(items, id); ok {
			t.Errorf("%s ne doit pas être planifié", id)
		}
	}
	items, err = syncPlanDirs(src, dst, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"set:theme", "set:enabledPlugins", "set:extraKnownMarketplaces"} {
		if _, ok := syncItem(items, id); !ok {
			t.Errorf("%s attendu sans skipPlugin", id)
		}
	}
}

func TestSyncApplyDirsRefusesSkippedKey(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	put(t, filepath.Join(src, "settings.json"), "{\n  \"enabledPlugins\": {\n    \"a@m\": true\n  }\n}\n", old)
	if _, err := syncApplyDirs(src, dst, []SyncPick{{"set:enabledPlugins", "replace"}}, true); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dst, "settings.json")); err == nil && strings.Contains(string(data), "enabledPlugins") {
		t.Errorf("clé écrite malgré skipPlugin: %s", data)
	}
}
