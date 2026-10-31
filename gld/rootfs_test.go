package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Un lien posé par la cible, en dernier segment ou au milieu du chemin, ne fait jamais écrire ni effacer hors de la base.
func TestConfineRefuseLesLiensSortants(t *testing.T) {
	base, out := t.TempDir(), t.TempDir()
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(base, "lien"))
	os.Symlink(out, filepath.Join(base, "dossier"))
	checks := map[string]error{
		"WriteFile lien":    rWriteFile(base, filepath.Join(base, "lien"), []byte("x"), 0o600),
		"WriteFile dossier": rWriteFile(base, filepath.Join(base, "dossier", "victime"), []byte("x"), 0o600),
		"RemoveAll dossier": rRemoveAll(base, filepath.Join(base, "dossier", "victime")),
		"Rename dossier":    rRename(base, filepath.Join(base, "dossier", "victime"), filepath.Join(base, "vole")),
		"MkdirAll dossier":  rMkdirAll(base, filepath.Join(base, "dossier", "sous"), 0o700),
		"hors de la base":   rWriteFile(base, victim, []byte("x"), 0o600),
	}
	if file, err := rOpenFile(base, filepath.Join(base, "lien"), os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		file.Close()
		t.Error("OpenFile a suivi le lien")
	}
	for what, err := range checks {
		if err == nil {
			t.Errorf("%s accepté", what)
		}
	}
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("fichier hors de la base modifié : %q", data)
	}
	if _, err := os.Stat(filepath.Join(out, "sous")); err == nil {
		t.Fatal("dossier créé hors de la base")
	}
	if err := rWriteFile(base, filepath.Join(base, "a", "b"), []byte("ok"), 0o600); err == nil {
		t.Fatal("dossier parent absent : WriteFile aurait dû échouer")
	}
	if err := rMkdirAll(base, filepath.Join(base, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := rWriteFile(base, filepath.Join(base, "a", "b"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTrashNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(cfg, "gld-web.log"))
	journal("essai")
	os.Mkdir(filepath.Join(out, "x"), 0o700)
	os.Symlink(out, filepath.Join(cfg, "gld-web-trash"))
	trashPurge("")
	os.RemoveAll(filepath.Join(cfg, "gld-web-trash")) // le lien seul, jamais sa cible
	rRemoveAll(cfg, filepath.Join(cfg, "gld-web-trash", "x"))
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("journal écrit à travers un lien : %q", data)
	}
	if _, err := os.Stat(filepath.Join(out, "x")); err != nil {
		t.Fatal("corbeille vidée à travers un lien")
	}
}

func TestWriteConfigNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	os.Symlink(out, filepath.Join(cfg, "gld-web-config-backups"))
	if err := saveBackup("u", []byte("x")); err == nil {
		t.Error("sauvegarde écrite à travers un lien")
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("fichiers écrits hors de la config : %v", ents)
	}
	path := filepath.Join(cfg, "settings.json")
	if err := writeConfig(path, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	os.Chmod(path, 0o644)
	writeConfig(path, []byte("{ }\n"))
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Fatalf("mode non conservé : %v", st.Mode())
	}
	if values, _ := filepath.Glob(filepath.Join(cfg, ".gld-web-*.tmp")); len(values) != 0 {
		t.Fatalf("temporaires restés : %v", values)
	}
}

func TestFichiersDeDonneesNeSuiventPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	for _, filePath := range []string{"gld-web-notes.json.tmp", "gld-web-tabs.json.tmp", "gld-web-groups.json.tmp"} {
		os.Symlink(victim, filepath.Join(cfg, filePath))
	}
	if err := notesSave(map[string]string{"p/x": "y"}); err != nil {
		t.Fatal(err)
	}
	writeTabs(nil)
	os.MkdirAll(filepath.Join(out, "-p"), 0o700)
	os.WriteFile(filepath.Join(out, "-p", id1+".jsonl"), []byte("{}\n"), 0o600)
	os.MkdirAll(filepath.Join(cfg, "projects"), 0o700)
	os.Symlink(filepath.Join(out, "-p"), filepath.Join(cfg, "projects", "-p"))
	if err := setTitle(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), id1, "t"); err == nil {
		t.Error("titre écrit à travers un lien")
	}
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("fichier hors de la config écrasé : %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(out, "-p", id1+".jsonl")); string(data) != "{}\n" {
		t.Fatalf("transcript hors de la config modifié : %q", data)
	}
}

func TestCorbeilleSessionsNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{}\n"), 0o600)
	os.Symlink(out, filepath.Join(cfg, "gld-web-trash")) // la corbeille pointe hors de la config
	if count, _, _ := trashSessions([]item{{"-p", id1}}); count != 0 {
		t.Fatal("session déplacée à travers un lien")
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("écrit hors de la config : %v", ents)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("transcript perdu")
	}
	victim := filepath.Join(out, "history")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(cfg, "history.jsonl"))
	pruneHistory([]string{id1})
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("history réécrit à travers un lien : %q", data)
	}
}

func TestReassignNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	encSrc := nonAlnum.ReplaceAllString(src, "-")
	jl := filepath.Join(cfg, "projects", encSrc, id1+".jsonl")
	os.MkdirAll(filepath.Dir(jl), 0o755)
	os.WriteFile(jl, []byte(`{"cwd":"`+src+`"}`+"\n"), 0o600)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, jl+".tmp")
	if count, err := reassign([]item{{encSrc, id1}}, dst); count != 1 || err != nil {
		t.Fatal(count, err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("réécriture du cwd à travers un lien : %q", data)
	}
}

func TestImagesEtSkillsNeSuiventPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	os.Symlink(out, filepath.Join(cfg, "gld-web-images"))
	png := []byte("\x89PNG\r\n\x1a\n0000")
	if _, err := saveImage(png, ""); err == nil {
		t.Error("image écrite à travers un lien")
	}
	os.Symlink(out, filepath.Join(cfg, "skills"))
	if err := saveSkill("u/neuf", "---\nname: neuf\ndescription: d\n---\n", true); err == nil {
		t.Error("skill créé à travers un lien")
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("écrit hors de la config : %v", ents)
	}
}

func TestWalkRootSuitUnLienHorsRunAs(t *testing.T) {
	real, links := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(real, "revue.md"), []byte("---\ndescription: d\n---\n"), 0o644)
	os.Symlink(real, filepath.Join(links, "commands"))
	got := walkCommands(filepath.Join(links, "commands"), "", "", nil)
	if len(got) != 1 || got[0].Path != "revue.md" {
		t.Fatalf("commandes d'un dossier lié : %+v", got)
	}
}

func TestQueueNeSuitPasLesLiens(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	queueStore, err := openQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(dir, "queue.json"))
	if _, err := queueStore.Add("s", "host", "", "x"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("fichier hors de la base écrasé : %q", data)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "queue.json")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("queue.json reste un lien : %v", err)
	}
}

func TestSharesNeSuitPasLesLiens(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	shareStore, err := openShares(dir)
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(dir, "shares.json"))
	shareStore.CreateInvite("s", "d", "", "") // erreur attendue : lien refusé, sans écriture hors base
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("fichier hors de la base écrasé : %q", data)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "shares.json")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("shares.json reste un lien : %v", err)
	}
}

// Équipes : un dossier agents/ remplacé par un lien sortant ne fait rien écrire ailleurs ; un fichier d'agent lié reste intact.
func TestAgentsNeSuiventPasLesLiens(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	os.Symlink(out, filepath.Join(root, "agents"))
	if _, _, err := writeAgents(root, []Agent{{ID: "rex", Role: "r", Access: "read"}}); err == nil {
		t.Error("dossier agents/ lié : écriture acceptée")
	}
	if es, _ := os.ReadDir(out); len(es) != 0 {
		t.Fatalf("écrit hors de la racine : %v", es)
	}
	root2 := t.TempDir()
	os.Mkdir(filepath.Join(root2, "agents"), 0o755)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(root2, "agents", "rex.md"))
	writeAgents(root2, []Agent{{ID: "rex", Role: "r", Access: "read"}})
	if data, _ := os.ReadFile(victim); string(data) != "intact" {
		t.Fatalf("fichier lié modifié : %q", data)
	}
}

func TestJournalChargementsNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, loadedFile())
	appendLoaded([]byte(`{"at":1,"session":"s","file":"/f"}` + "\n"))
	if content, _ := os.ReadFile(victim); string(content) != "intact" {
		t.Fatalf("fichier hors de la config modifié : %q", content)
	}
}
