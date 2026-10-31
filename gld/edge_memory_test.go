package main

// Tests issus des tests de mutation (gremlins) : chacun fige une limite ou une branche qu'aucun autre test ne vérifiait.

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLimitesSnippets(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".claude")
	use(cfg)
	snipDir = ""
	pile := strings.Repeat("a", snippetMax)
	put(t, filepath.Join(cfg, "commands", "max.md"), pile, old)
	put(t, filepath.Join(cfg, "commands", "trop.md"), pile+"a", old)
	snippets, _ := listSnippets()
	if len(snippets) != 1 || snippets[0].Key != "u/max.md" || snippets[0].KB != snippetMax/1024 || !near(snippets[0].Mtime, mtimeOf(t, filepath.Join(cfg, "commands", "max.md"))) {
		t.Fatalf("liste : %+v", snippets)
	}
	if text, err := readSnippet("u/max.md"); err != nil || len(text) != snippetMax {
		t.Fatal("lecture à la limite :", err)
	}
	if _, err := readSnippet("u/trop.md"); err == nil {
		t.Fatal("fichier trop gros lu")
	}
	if saveSnippet("u/n.md", pile, true) != nil || saveSnippet("u/n2.md", pile+"a", true) == nil {
		t.Fatal("enregistrement à la limite")
	}
	if saveSnippet("u/max.md", "v2", false) != nil {
		t.Fatal("modification")
	}
	if text, _ := readSnippet("u/max.md"); text != "v2" {
		t.Fatal("modification non écrite :", len(text))
	}
	os.MkdirAll(filepath.Join(cfg, "commands", "dossier.md"), 0o755)
	if saveSnippet("u/dossier.md", "x", false) == nil {
		t.Fatal("écriture sur un dossier acceptée")
	}

	// Deux projets : les scopes sont triés.
	for index, dir := range []string{"-b", "-a"} {
		proj := filepath.Join(home, "proj"+dir)
		os.MkdirAll(proj, 0o755)
		put(t, filepath.Join(cfg, "projects", dir, fmt.Sprintf("%08d-0000-0000-0000-000000000000.jsonl", index)), `{"cwd":"`+proj+`"}`+"\n", old)
	}
	if _, sc := listSnippets(); len(sc) != 2 || sc[0].Project > sc[1].Project {
		t.Fatalf("scopes : %+v", sc)
	}

	// Import : une entrée d'archive à la limite passe entière, au-delà elle est ignorée ; scope de projet.
	zipOf := func(files map[string]string) []byte {
		var buffer bytes.Buffer
		zw := zip.NewWriter(&buffer)
		for name, content := range files {
			writer, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
			writer.Write([]byte(content))
		}
		zw.Close()
		return buffer.Bytes()
	}
	done, _, err := importSnippets("-a", []ImportFile{{"l.zip", zipOf(map[string]string{"z.md": pile, "zz.md": pile + "a"})}}, false)
	if err != nil || len(done) != 1 || done[0] != "z.md" {
		t.Fatal(done, err)
	}
	if text, err := readSnippet("p/-a/z.md"); err != nil || len(text) != snippetMax {
		t.Fatal("import dans le projet :", err)
	}
	if _, _, err := importSnippets("", []ImportFile{{"x.zip", []byte("pas une archive")}}, false); err == nil || !strings.Contains(err.Error(), "x.zip : archive illisible") {
		t.Fatal(err)
	}
	abime := bytes.Replace(zipOf(map[string]string{"c.md": "contenu-sain"}), []byte("contenu-sain"), []byte("contenu-abim"), 1)
	if _, _, err := importSnippets("", []ImportFile{{"c.zip", abime}}, false); err == nil || !strings.HasPrefix(err.Error(), "c.md : ") {
		t.Fatal("somme de contrôle :", err)
	}
}

func TestLimitesMemoire(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	base := t.TempDir()
	pa, pb := filepath.Join(base, "a"), filepath.Join(base, "b")
	os.MkdirAll(pa, 0o755)
	os.MkdirAll(pb, 0o755)
	pp := filepath.Join(cfg, "projects")
	put(t, filepath.Join(pp, "-b", id1+".jsonl"), `{"cwd":"`+pb+`"}`+"\n", 2*time.Hour)
	put(t, filepath.Join(pp, "-b", id2+".jsonl"), "", time.Hour) // plus récente mais vide : ignorée
	put(t, filepath.Join(pp, "-a", id3+".jsonl"), `{"cwd":"`+pa+`"}`+"\n", old)
	put(t, filepath.Join(pp, "-vide", "44444444-4444-4444-4444-444444444444.jsonl"), `{"cwd":"`+base+`"}`+"\n", old) // projet sans mémoire
	put(t, filepath.Join(pp, "-b", "memory", "x.md"), strings.Repeat("a", 3000), old)
	put(t, filepath.Join(pp, "-a", "memory", "y.md"), "y", old)
	put(t, filepath.Join(pp, "-zz-introuvable", "memory", "z.md"), "z", old)
	if projectCwd("-b") != pb {
		t.Fatal("projectCwd :", projectCwd("-b"))
	}

	_, ps, ts := memList()
	if len(ps) != 3 || ps[0].Project != pa || ps[1].Project != pb || ps[2].Project != "/zz/introuvable" || !ps[2].Missing {
		t.Fatalf("projets : %+v", ps)
	}
	if memFile := ps[1].Files[0]; memFile.KB != 2 || !near(memFile.Mtime, mtimeOf(t, memFile.Path)) {
		t.Fatalf("fichier : %+v", memFile)
	}
	for index := 1; index < len(ts); index++ {
		if ts[index-1].Project > ts[index].Project {
			t.Fatalf("cibles non triées : %+v", ts)
		}
	}

	pile := strings.Repeat("a", memMax)
	if memSave("m/-a/max.md", pile) != nil || memSave("m/-a/trop.md", pile+"a") == nil {
		t.Fatal("écriture à la limite")
	}
	if text, err := memRead("m/-a/max.md"); err != nil || len(text) != memMax {
		t.Fatal("lecture à la limite :", err)
	}
	put(t, filepath.Join(pp, "-a", "memory", "trop.md"), pile+"a", old)
	if _, err := memRead("m/-a/trop.md"); err == nil {
		t.Fatal("fichier trop gros lu")
	}
	if memSave("p/-a/.claude/CLAUDE.md", "dans .claude") != nil {
		t.Fatal("sous-dossier .claude non créé")
	}
	os.MkdirAll(filepath.Join(pp, "-a", "memory", "dossier.md"), 0o755)
	if memSave("m/-a/dossier.md", "x") == nil {
		t.Fatal("écriture sur un dossier acceptée")
	}

	// Fusion : de 2 à mergeMax fichiers, tous présents ; le dossier de lancement suit une clé de projet.
	var keys []string
	for index := range mergeMax + 1 {
		key := fmt.Sprintf("m/-a/f%02d.md", index)
		put(t, filepath.Join(pp, "-a", "memory", filepath.Base(key)), "f", old)
		keys = append(keys, key)
	}
	if _, _, err := mergePrompt(keys[:mergeMax], ""); err != nil {
		t.Fatal("mergeMax fichiers refusés :", err)
	}
	if _, _, err := mergePrompt(keys, ""); err == nil {
		t.Fatal("trop de fichiers acceptés")
	}
	if _, _, err := mergePrompt([]string{"m/-a/y.md", "m/-a/absent.md"}, ""); err == nil || !strings.Contains(err.Error(), "introuvable : "+filepath.Join(pp, "-a", "memory", "absent.md")) {
		t.Fatal(err)
	}
	put(t, filepath.Join(cfg, "CLAUDE.md"), "g", old)
	if _, cwd, err := mergePrompt([]string{"g/CLAUDE.md", "p/-a/.claude/CLAUDE.md"}, ""); err != nil || cwd != pa {
		t.Fatal("dossier de fusion :", cwd, err)
	}

	// Brouillons : bornes de la consigne, du texte et des références.
	memDraft = func(model, system, prompt string) (string, float64, error) { return "ok\n", 0, nil }
	for _, values := range [][2]string{{"", ""}, {strings.Repeat("c", 8<<10+1), ""}, {"", strings.Repeat("t", snippetMax+1)}} {
		if _, _, err := snippetDraft("haiku", values[0], values[1]); err == nil {
			t.Fatalf("mise en forme acceptée : %d/%d", len(values[0]), len(values[1]))
		}
	}
	if _, _, err := snippetDraft("haiku", strings.Repeat("c", 8<<10), strings.Repeat("t", snippetMax)); err != nil {
		t.Fatal("mise en forme à la limite :", err)
	}
	if _, err := memWritePrompt(strings.Repeat("c", 8<<10), "", keys[:20]); err != nil {
		t.Fatal("brouillon à la limite :", err)
	}
	if _, err := memWritePrompt(strings.Repeat("c", 8<<10+1), "", nil); err == nil {
		t.Fatal("consigne trop longue acceptée")
	}
	if _, err := memWritePrompt("c", "", keys); err == nil {
		t.Fatal("21 références acceptées")
	}
	if _, err := memWritePrompt("c", "m/-a/trop.md", nil); err == nil {
		t.Fatal("fichier illisible cité")
	}
}

func TestBrouillonClaude(t *testing.T) {
	web(t)
	t.Setenv("FAKE_DRAFT", "pas du json")
	if _, _, err := memDraft("haiku", "s", "p"); err == nil || err.Error() != "réponse illisible" {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DRAFT", `{"is_error":true,"result":"quota atteint","total_cost_usd":0.5}`)
	if _, cost, err := memDraft("haiku", "s", "p"); err == nil || err.Error() != "quota atteint" || cost != 0.5 {
		t.Fatal(cost, err)
	}
	os.Remove(filepath.Join(os.Getenv("HOME"), ".local", "bin", "claude"))
	if _, _, err := memDraft("haiku", "s", "p"); err == nil || err.Error() == "réponse illisible" {
		t.Fatal("claude absent :", err)
	}
}

func TestLimitesOngletsCorbeille(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"/p"}`+"\n", old)
	openSessions = func() []string { return []string{id1} }
	if _, _, err := saveTabs(strings.Repeat("é", 80), false); err != nil {
		t.Fatal("nom à la limite :", err)
	}
	if _, _, err := saveTabs(strings.Repeat("é", 81), false); err == nil {
		t.Fatal("nom trop long accepté")
	}
	now := time.Now()
	at := func(duration time.Duration) string { return now.Add(-duration).Format(time.RFC3339Nano) }
	ss := []TabSession{{ID: id1}}
	if tabSaves := pruneAuto([]TabSave{{Auto: true, At: at(0), Sessions: ss}, {Auto: true, At: at(keepAuto), Sessions: ss}}); len(tabSaves) != 2 {
		t.Fatal("sauvegarde à une heure pile retirée")
	}

	if err := saveTrash("g/CLAUDE.md", "/x", "supprimé", strings.Repeat("a", 3000)); err != nil {
		t.Fatal(err)
	}
	if trasheds := trashList(); len(trasheds) != 1 || trasheds[0].KB != 2 || !near(trasheds[0].At, float64(time.Now().UnixNano())/1e9) {
		t.Fatalf("corbeille : %+v", trasheds)
	}
}

// main : démarre, sert sur 127.0.0.1 et localhost, et s'arrête proprement sur interruption.
// Dans un sous-processus (le binaire de test relancé) : main laisse tourner la sauvegarde des onglets.

// Une clé de mémoire ne crée pas de dossier de projet : « . », un nom encodé ou un projet absent sont refusés.
func TestMemResolveProjetExistant(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	os.MkdirAll(filepath.Join(cfg, "projects", "-p"), 0o755)
	if _, _, err := memResolve("m/-p/MEMORY.md"); err != nil {
		t.Fatal("projet existant refusé :", err)
	}
	for _, key := range []string{"m/./x.md", "m/%2e%2e/x.md", "m/．．/x.md", "m/-absent/x.md"} {
		if memSave(key, "x") == nil {
			t.Errorf("%s accepté", key)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(cfg, "projects"))
	if len(ents) != 1 {
		t.Fatalf("dossiers créés dans projects/ : %v", ents)
	}
}

func TestSaveSnippetGardeUnFichierIllisible(t *testing.T) {
	cfg := t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	snipDir = ""
	path := filepath.Join(cfg, "commands", "x.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("\xff\xfe"), 0o644)
	if err := saveSnippet("u/x.md", "nouveau", false); err == nil {
		t.Fatal("snippet illisible écrasé")
	}
	if data, _ := os.ReadFile(path); string(data) != "\xff\xfe" {
		t.Fatalf("contenu perdu : %q", data)
	}
}
