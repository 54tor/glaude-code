package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchRenameGroups(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	put(t, p, `{"type":"user","cwd":"/p","message":{"content":"Déploie Caddy en prod"}}`+"\n"+
		`{"type":"assistant","message":{"content":[{"type":"text","text":"caddy redémarré"},{"type":"tool_use","name":"Bash","input":{"command":"caddy reload"}}]}}`, old)

	// Recherche : insensible à la casse, dans les messages seulement (pas l'appel d'outil).
	h := search("CADDY")
	if len(h) != 1 || h[0].ID != id1 || h[0].N != 2 || h[0].Snip != "Déploie Caddy en prod" {
		t.Fatalf("search = %+v", h)
	}
	if len(search("reload")) != 0 {
		t.Fatal("les entrées d'outils ne doivent pas être cherchées")
	}

	// Renommage : la dernière ligne custom-title fait foi, la date du fichier ne bouge pas.
	before, _ := os.Stat(p)
	if err := setTitle(p, id1, "Caddy prod"); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if s := info(p, st); s.Title != "Caddy prod" || !s.Named {
		t.Fatalf("titre = %q", s.Title)
	}
	if !st.ModTime().Equal(before.ModTime()) || time.Since(st.ModTime()) < old/2 {
		t.Fatal("la date de modification a changé")
	}
	if len(conv(p, false)) != 2 {
		t.Fatal("la ligne de titre ne doit pas apparaître dans la conversation")
	}

	// Groupes : enregistrés puis relus ; noms en double et ids invalides refusés.
	if err := saveGroups([]VGroup{{Name: " Infra  prod ", IDs: []string{id1}}}); err != nil {
		t.Fatal(err)
	}
	if g := loadGroups(); len(g) != 1 || g[0].Name != "Infra prod" || g[0].IDs[0] != id1 {
		t.Fatalf("groupes = %+v", g)
	}
	if saveGroups([]VGroup{{Name: "a"}, {Name: "a"}}) == nil || saveGroups([]VGroup{{Name: "a", IDs: []string{"../x"}}}) == nil {
		t.Fatal("validation des groupes")
	}
}

func TestSnippets(t *testing.T) {
	d := t.TempDir()
	put(t, filepath.Join(d, "revue.md"), "Fais une revue", 0)
	put(t, filepath.Join(d, "go", "tests.md"), "Écris les tests", 0)
	put(t, filepath.Join(d, ".git", "HEAD"), "x", 0)
	put(t, filepath.Join(t.TempDir(), "secret"), "s", 0)
	if l := snippets(d); len(l) != 2 || l[0].Path != "go/tests.md" || l[1].Path != "revue.md" {
		t.Fatalf("snippets = %+v", l)
	}
	if s, err := readSnippet(d, "go/tests.md"); err != nil || s != "Écris les tests" {
		t.Fatal(s, err)
	}
	for _, bad := range []string{"../secret", "/etc/passwd", "go/../../x"} {
		if _, err := readSnippet(d, bad); err == nil {
			t.Fatal("chemin accepté : " + bad)
		}
	}
}

// Déplacer une session ne touche qu'à son cwd ; ses sous-dossiers ne suivent que si l'ancien dossier a disparu.
func TestRewriteCwd(t *testing.T) {
	if got := remapCwd("/h/.local/bin", "/h", "/h/P/tb", false); got != "/h/.local/bin" {
		t.Fatal("sous-dossier réel réécrit :", got)
	}
	if got := remapCwd("/h/P/tb/x", "/h", "/h/P/tb", true); got != "/h/P/tb/x" {
		t.Fatal("chemin déjà dans la cible réécrit :", got)
	}
	if got := remapCwd("/old/src", "/old", "/new", true); got != "/new/src" {
		t.Fatal("projet renommé :", got)
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	put(t, p, `{"cwd":"/h","x":1}`+"\n"+`{"cwd":"/h/.local/bin"}`+"\n"+`{"cwd":"/h/P/tb"}`+"\n", old)
	if err := rewriteCwd(p, "/h", "/h/P/tb", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if want := `{"cwd":"/h/P/tb","x":1}` + "\n" + `{"cwd":"/h/.local/bin"}` + "\n" + `{"cwd":"/h/P/tb"}` + "\n"; string(b) != want {
		t.Fatalf("transcript :\n%s", b)
	}
}

func TestMemory(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "CLAUDE.md"), "global", old)
	put(t, filepath.Join(cfg, "rules", "r.md"), "règle", old)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [A](a.md) — a\n- [B](b.md) — b\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "entrée a", old)
	put(t, filepath.Join(proj, "CLAUDE.md"), "projet", old)

	g, ps, _ := memList()
	if len(g) != 2 || len(ps) != 1 || len(ps[0].Files) != 3 || ps[0].Project != proj {
		t.Fatalf("liste : %+v %+v", g, ps)
	}
	if s, err := memRead("p/-p/CLAUDE.md"); err != nil || s != "projet" {
		t.Fatal(s, err)
	}
	if err := memSave("g/rules/r.md", "nouvelle"); err != nil {
		t.Fatal(err)
	}
	if s, _ := memRead("g/rules/r.md"); s != "nouvelle" {
		t.Fatal("enregistrement :", s)
	}
	// Supprimer une entrée retire aussi sa ligne d'index.
	if err := memRemove("m/-p/a.md"); err != nil {
		t.Fatal(err)
	}
	if s, _ := memRead("m/-p/MEMORY.md"); s != "- [B](b.md) — b\n" {
		t.Fatalf("index : %q", s)
	}
	for _, bad := range []string{"g/../x.md", "g/rules/../../x.md", "m/../memory/a.md", "m/-p/../a.md", "p/-p/secret.txt", "p/-p/../CLAUDE.md", "g/settings.json"} {
		if _, err := memRead(bad); err == nil {
			t.Fatal("clé acceptée : " + bad)
		}
		if memSave(bad, "x") == nil {
			t.Fatal("écriture acceptée : " + bad)
		}
	}
}

func TestTabs(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"/p","type":"user","message":{"content":"un"}}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"cwd":"/p","type":"user","message":{"content":"deux"}}`+"\n", old)
	open := []string{id1}
	openSessions = func() []string { return open }

	if _, saved, err := saveTabs("", true); err != nil || !saved {
		t.Fatal(saved, err)
	}
	if _, saved, _ := saveTabs("", true); saved {
		t.Fatal("sauvegarde automatique identique : elle doit être sautée")
	}
	if s, saved, _ := saveTabs("  avant  reboot ", false); !saved || s.Name != "avant reboot" || s.Sessions[0].Project != "/p" || s.Sessions[0].Title != "un" {
		t.Fatalf("sauvegarde manuelle : %+v", s)
	}
	// Seules les keepAuto sauvegardes automatiques les plus récentes restent ; la manuelle est gardée.
	for i := 0; i < keepAuto+3; i++ {
		if i%2 == 0 {
			open = []string{id1, id2}
		} else {
			open = []string{id2}
		}
		saveTabs("", true)
	}
	all := loadTabs()
	autos, manual := 0, 0
	for _, s := range all {
		if s.Auto {
			autos++
		} else {
			manual++
		}
	}
	if autos != keepAuto || manual != 1 {
		t.Fatalf("%d auto, %d manuelles", autos, manual)
	}
	open = nil
	if _, _, err := saveTabs("", true); err == nil {
		t.Fatal("aucune session ouverte : rien ne doit être écrasé")
	}
	if removeTabs(all[0].At) != nil || len(loadTabs()) != len(all)-1 {
		t.Fatal("suppression")
	}
}

func TestMergePrompt(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "a", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "b.md"), "b", old)
	p, cwd, err := mergePrompt([]string{"m/-p/a.md", "m/-p/b.md"})
	if err != nil || cwd != proj || !strings.Contains(p, filepath.Join(cfg, "projects", "-p", "memory", "b.md")) {
		t.Fatal(p, cwd, err)
	}
	if _, _, err := mergePrompt([]string{"m/-p/a.md"}); err == nil {
		t.Fatal("un seul fichier accepté")
	}
	if _, _, err := mergePrompt([]string{"m/-p/a.md", "m/-p/../x.md"}); err == nil {
		t.Fatal("clé invalide acceptée")
	}
}

func TestMemoryMove(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "projects", "-a", "memory", "MEMORY.md"), "- [X](x.md) — accroche\n- [Y](y.md) — y\n", old)
	put(t, filepath.Join(cfg, "projects", "-a", "memory", "x.md"), "contenu x", old)
	put(t, filepath.Join(cfg, "projects", "-b", "memory", "MEMORY.md"), "- [Z](z.md) — z", old)
	put(t, filepath.Join(cfg, "projects", "-b", "memory", "y.md"), "autre y", old)

	if dst, err := memMove("m/-a/x.md", "-b"); err != nil || dst != "m/-b/x.md" {
		t.Fatal(dst, err)
	}
	if s, _ := memRead("m/-b/x.md"); s != "contenu x" {
		t.Fatal("contenu :", s)
	}
	if s, _ := memRead("m/-a/MEMORY.md"); s != "- [Y](y.md) — y\n" {
		t.Fatalf("index source : %q", s)
	}
	if s, _ := memRead("m/-b/MEMORY.md"); s != "- [Z](z.md) — z\n- [X](x.md) — accroche\n" {
		t.Fatalf("index destination : %q", s)
	}
	put(t, filepath.Join(cfg, "projects", "-a", "memory", "y.md"), "y", old)
	if _, err := memMove("m/-a/y.md", "-b"); err == nil {
		t.Fatal("écrasement accepté")
	}
	if _, err := memMove("m/-b/x.md", "global"); err != nil {
		t.Fatal(err)
	}
	if s, _ := memRead("g/rules/x.md"); s != "contenu x" {
		t.Fatal("vers les règles :", s)
	}
	for _, bad := range [][2]string{{"m/-a/y.md", "../x"}, {"m/-a/y.md", "-inexistant"}, {"g/CLAUDE.md", "-b"}} {
		if _, err := memMove(bad[0], bad[1]); err == nil {
			t.Fatal("déplacement accepté :", bad)
		}
	}
}
