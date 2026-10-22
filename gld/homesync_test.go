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
	if err := saveHomes([]Home{{"Pro", src}, {"Perso", dst}}); err != nil {
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
	b, _ := os.ReadFile(filepath.Join(dst, "settings.json"))
	s := string(b)
	for _, want := range []string{`"model": "opus"`, `"theme": "light"`, `"Bash(cat:*)"`, `"Bash(ls:*)"`, `"Bash(git status:*)"`} {
		if !strings.Contains(s, want) {
			t.Errorf("manque %s dans :\n%s", want, s)
		}
	}
	if c, _ := os.ReadFile(filepath.Join(dst, "rules", "a.md")); string(c) != "nouvelle\n" {
		t.Errorf("règle non remplacée : %q", c)
	}
	if c, _ := os.ReadFile(filepath.Join(dst, "skills", "deploy", "SKILL.md")); string(c) != "skill\n" {
		t.Errorf("skill non copié : %q", c)
	}
	// le fichier remplacé est sauvegardé, et l'ancien contenu y est
	var found bool
	filepath.WalkDir(filepath.Join(dst, "gld-web-sync-backups"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(p) == "a.md" {
			c, _ := os.ReadFile(p)
			found = string(c) == "ancienne\n"
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
	b, _ := os.ReadFile(filepath.Join(dst, "settings.json"))
	if s := string(b); !strings.Contains(s, `"a@m": false`) || !strings.Contains(s, `"b@m": true`) {
		t.Fatalf("%s", s)
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
	for _, c := range []struct {
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
		if got := sameSet(c.a, c.b); got != c.want {
			t.Errorf("sameSet(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
