package main

import (
	"archive/zip"
	"bytes"
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

	// Groupes : enregistrés puis relus ; même nom permis dans deux projets, pas dans un seul ; ids invalides refusés.
	store := Store{Groups: []VGroup{{Name: " Infra  prod ", Project: "/p", IDs: []string{id1}, Fav: true}, {Name: "Infra prod", Project: "/q"}}, Favs: []string{id1}}
	if err := saveStore(store); err != nil {
		t.Fatal(err)
	}
	if g := loadStore(); len(g.Groups) != 2 || g.Groups[0].Name != "Infra prod" || !g.Groups[0].Fav || g.Favs[0] != id1 {
		t.Fatalf("groupes = %+v", g)
	}
	bad := []Store{
		{Groups: []VGroup{{Name: "a", Project: "/p"}, {Name: "a", Project: "/p"}}, Favs: []string{}},
		{Groups: []VGroup{{Name: "a", Project: "/p", IDs: []string{"../x"}}}, Favs: []string{}},
		{Groups: []VGroup{{Name: "a"}}, Favs: []string{}},
		{Groups: []VGroup{}, Favs: []string{"x"}},
	}
	for _, b := range bad {
		if saveStore(b) == nil {
			t.Fatalf("accepté : %+v", b)
		}
	}
	// Ancien format (liste sans projet) : chaque groupe est rattaché au dossier de travail de ses sessions.
	put(t, groupsFile(), `[{"name":"Vieux","ids":["`+id1+`","`+id3+`"]}]`, old)
	if g := loadStore(); len(g.Groups) != 1 || g.Groups[0].Project != "/p" || len(g.Groups[0].IDs) != 1 {
		t.Fatalf("migration = %+v", g)
	}
}

func TestCommands(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".claude")
	use(cfg)
	proj := filepath.Join(home, "p")
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-h", id2+".jsonl"), `{"cwd":"`+home+`"}`+"\n", old) // projet « ~ »
	put(t, filepath.Join(cfg, "commands", "revue.md"), "---\ndescription: Revue de code\n---\nFais une revue de $ARGUMENTS", old)
	put(t, filepath.Join(proj, ".claude", "commands", "go", "tests.md"), "Écris les tests", old)
	put(t, filepath.Join(cfg, "commands", ".cache", "x.md"), "caché", old)
	put(t, filepath.Join(cfg, "commands", "notes.txt"), "pas une commande", old)

	l, scopes := listSnippets()
	if len(l) != 2 || l[0].Key != "u/revue.md" || l[0].Command != "/revue" || l[0].Description != "Revue de code" ||
		l[1].Key != "p/-p/go/tests.md" || l[1].Command != "/go:tests" || len(scopes) != 1 || scopes[0].Project != proj {
		t.Fatalf("liste : %+v %+v", l, scopes)
	}

	// Création, refus de recréer, modification (l'ancienne version à la corbeille), restauration, suppression.
	if err := saveSnippet("p/-p/deploy.md", "v1", true); err != nil {
		t.Fatal(err)
	}
	if saveSnippet("p/-p/deploy.md", "v1", true) == nil || saveSnippet("p/-p/deploy.md", "v2", false) != nil {
		t.Fatal("création / modification")
	}
	tr := trashList()
	if len(tr) != 1 || tr[0].Key != "s/p/-p/deploy.md" || tr[0].Reason != "remplacé" {
		t.Fatalf("corbeille : %+v", tr)
	}
	if _, err := trashRestore(tr[0].ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := readSnippet("p/-p/deploy.md"); s != "v1" {
		t.Fatal("restauration :", s)
	}
	if removeSnippet("p/-p/deploy.md") != nil {
		t.Fatal("suppression")
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "commands", "deploy.md")); err == nil {
		t.Fatal("toujours là")
	}

	// Export puis import dans les commandes personnelles : l'existant est gardé sans overwrite.
	z, n, err := exportSnippets("-p")
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	done, skipped, err := importSnippets("", []ImportFile{{"lot.zip", z}, {"revue.md", []byte("autre")}, {"nouveau.md", []byte("n")}}, false)
	if err != nil || len(done) != 2 || len(skipped) != 1 {
		t.Fatal(done, skipped, err)
	}
	if s, _ := readSnippet("u/go/tests.md"); s != "Écris les tests" {
		t.Fatal("import :", s)
	}
	if s, _ := readSnippet("u/revue.md"); !strings.Contains(s, "$ARGUMENTS") {
		t.Fatal("un snippet existant a été écrasé")
	}

	outside := filepath.Join(home, "dehors.md")
	for _, bad := range []string{"u/../dehors.md", "u/" + outside, "u/.cache/x.md", "u/notes.txt", "p/-h/x.md", "p/../x.md", "x/y.md"} {
		if saveSnippet(bad, "x", true) == nil {
			t.Fatal("clé acceptée : " + bad)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("écrit hors du dossier")
	}
	// Une archive ne peut rien écrire hors du dossier non plus.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../dehors.md")
	w.Write([]byte("x"))
	zw.Close()
	importSnippets("", []ImportFile{{"piege.zip", buf.Bytes()}}, true)
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("archive : écrit hors du dossier")
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
	// Sauvegardes automatiques : seule la dernière heure avant la plus récente reste ; les manuelles toutes.
	at := func(d time.Duration) string { return time.Now().Add(-d).Format(time.RFC3339Nano) }
	ss := []TabSession{{ID: id1}}
	l := pruneAuto([]TabSave{
		{Auto: true, At: at(10 * time.Hour), Sessions: ss}, // la plus récente, même vieille (nuit éteinte) : gardée
		{Auto: true, At: at(10*time.Hour + 30*time.Minute), Sessions: ss},
		{Name: "vieille manuelle", At: at(48 * time.Hour), Sessions: ss},
		{Auto: true, At: at(12 * time.Hour), Sessions: ss},
	})
	if len(l) != 3 || l[2].Name != "vieille manuelle" {
		t.Fatalf("pruneAuto : %+v", l)
	}
	open = []string{id1, id2}
	saveTabs("", true)
	all := loadTabs()
	if len(all) != 3 || !all[0].Auto || len(all[0].Sessions) != 2 {
		t.Fatalf("sauvegardes : %+v", all)
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

func TestPastedTitle(t *testing.T) {
	l := func(c string) *line {
		var x line
		x.Message.Content = []byte(c)
		return &x
	}
	if got := promptOf(l(`"le cdn ne démarre pas <pasted_content id=\"274a\">\nERR 42\n</pasted_content id=\"274a\"> merci"`)); got != "le cdn ne démarre pas [texte collé] merci" {
		t.Fatalf("%q", got)
	}
	if got := promptOf(l(`"\n<pasted_content id=\"1\">\nseul\n</pasted_content id=\"1\">\n"`)); got != "" {
		t.Fatalf("collage seul : %q", got)
	}
}

func TestResumePrompt(t *testing.T) {
	liveSessions = func() map[string]bool { return map[string]bool{id2: true} }
	if checkResumePrompt("", "") != nil || checkResumePrompt(id1, "continue") != nil {
		t.Fatal("message valide refusé")
	}
	for _, c := range [][2]string{{"", "sans session"}, {id2, "session ouverte"}, {id1, "nul\x00"}, {id1, strings.Repeat("x", 40<<10)}} {
		if checkResumePrompt(c[0], c[1]) == nil {
			t.Fatalf("accepté : %q", c[1][:min(len(c[1]), 20)])
		}
	}
}

func TestChat(t *testing.T) {
	a := chatArgs([]string{"--model", "x"}, id1, "acceptEdits", "--pas-une-option")
	if strings.Join(a, " ") != "--model x -p --resume "+id1+" --output-format stream-json --verbose --permission-mode acceptEdits -- --pas-une-option" {
		t.Fatal(a)
	}
	if _, _, err := claudeFlags("--ok; touch y"); err == nil {
		t.Fatal("option shell acceptée")
	}
	// Seules les options sans effet sur les permissions passent au chat.
	kept, ignored, _ := claudeFlags("--model=opus --dangerously-skip-permissions --permission-mode=bypassPermissions --add-dir=/x --effort=high")
	if strings.Join(kept, " ") != "--model=opus --effort=high" || len(ignored) != 3 {
		t.Fatal(kept, ignored)
	}
	out := `{"type":"system","subtype":"init"}
{"type":"assistant","message":{"content":[{"type":"text","text":"Bonjour"},{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"a.txt"}]}}
{"type":"result","subtype":"success","is_error":false,"total_cost_usd":0.01,"result":"Bonjour"}
`
	var evs []chatEvent
	if !relay(strings.NewReader(out), func(e chatEvent) { evs = append(evs, e) }) {
		t.Fatal("résultat non vu")
	}
	if len(evs) != 3 || evs[0].Msgs[0].T != "Bonjour" || evs[0].Msgs[1].R != "tool" || !strings.Contains(evs[1].Msgs[0].T, "a.txt") || !evs[2].Done || !evs[2].OK {
		t.Fatalf("%+v", evs)
	}
}

func TestMigrateData(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "cc-web-groups.json"), "ancien", old)
	put(t, filepath.Join(cfg, "cc-web.log"), "ancien", old)
	put(t, filepath.Join(cfg, "gld-web.log"), "nouveau", old)
	migrateData()
	if b, _ := os.ReadFile(filepath.Join(cfg, "gld-web-groups.json")); string(b) != "ancien" {
		t.Fatal("groupes non migrés")
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "gld-web.log")); string(b) != "nouveau" {
		t.Fatal("un fichier déjà au nouveau nom a été écrasé")
	}
}

func TestTrash(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "@RTK.md\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [A](a.md) — a\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "entrée a", old)

	// Supprimer puis remplacer : chaque version perdue part à la corbeille.
	if memRemove("g/CLAUDE.md") != nil || memSave("m/-p/a.md", "nouvelle a") != nil {
		t.Fatal("suppression ou enregistrement")
	}
	l := trashList()
	if len(l) != 2 || l[0].Key != "m/-p/a.md" || l[0].Reason != "remplacé" || l[1].Key != "g/CLAUDE.md" || l[1].Reason != "supprimé" {
		t.Fatalf("corbeille : %+v", l)
	}
	if _, err := trashRestore(l[1].ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := memRead("g/CLAUDE.md"); s != "@RTK.md\n" {
		t.Fatalf("restauré : %q", s)
	}
	if len(trashList()) != 1 {
		t.Fatal("l'élément restauré doit quitter la corbeille")
	}
	// Un identifiant inventé (chemin, etc.) n'est jamais suivi.
	if _, err := trashRestore("../../etc"); err == nil || trashPurge("../x") == nil {
		t.Fatal("identifiant hors corbeille accepté")
	}
	if trashPurge("") != nil || len(trashList()) != 0 {
		t.Fatal("vider la corbeille")
	}
	journal("test %d", 1)
	if b, _ := os.ReadFile(filepath.Join(cfg, "gld-web.log")); !strings.Contains(string(b), " test 1\n") {
		t.Fatalf("journal : %q", b)
	}
}

func TestMemoryDraft(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [A](a.md) — a", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "contenu A", old)
	var got, sys string
	memDraft = func(model, system, prompt string) (string, float64, error) {
		got, sys = prompt, system
		return "---\nname: b\n---\nB\n", 0.001, nil
	}

	text, _, err := memWriteDraft("haiku", "fusionne", "", []string{"m/-p/a.md"})
	if err != nil || !strings.Contains(got, "Consigne : fusionne") || !strings.Contains(got, "contenu A") || text == "" {
		t.Fatal(err, got)
	}
	for _, c := range [][2]string{{"gpt", "x"}, {"haiku", ""}} {
		if _, _, err := memWriteDraft(c[0], c[1], "", nil); err == nil {
			t.Fatal("accepté :", c)
		}
	}
	if _, _, err := memWriteDraft("haiku", "x", "", []string{"m/../a.md"}); err == nil {
		t.Fatal("référence invalide acceptée")
	}
	if sys != memSystem {
		t.Fatal("une entrée de mémoire se rédige au format mémoire")
	}
	// Conversion en CLAUDE.md global (absent ici) : format CLAUDE.md, fichier à créer.
	if _, _, err := memWriteDraft("haiku", "convertis", "g/CLAUDE.md", []string{"m/-p/a.md"}); err != nil || sys != claudeSystem || !strings.Contains(got, "n'existe pas encore") {
		t.Fatal(err, got)
	}
	// Nouvelle entrée : sa ligne s'ajoute à l'index, une seule fois.
	text = "---\nname: Entrée B\ndescription: ce que dit B\nmetadata:\n  type: project\n---\nB\n"
	if memSave("m/-p/b.md", text) != nil || memIndex("m/-p/b.md", text) != nil || memIndex("m/-p/b.md", text) != nil {
		t.Fatal("enregistrement")
	}
	if s, _ := memRead("m/-p/MEMORY.md"); s != "- [A](a.md) — a\n- [Entrée B](b.md) — ce que dit B\n" {
		t.Fatalf("index : %q", s)
	}
}

// Le projet « ~ » a pour .claude/CLAUDE.md le CLAUDE.md global : il ne doit apparaître (et se supprimer) qu'une fois.
func TestGlobalClaudeNotAProjectFile(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".claude")
	use(cfg)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "@RTK.md\n", old)
	put(t, filepath.Join(cfg, "projects", "-h", id1+".jsonl"), `{"cwd":"`+home+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-h", "memory", "a.md"), "a", old)
	g, ps, _ := memList()
	if len(g) != 1 || len(ps) != 1 {
		t.Fatalf("%+v %+v", g, ps)
	}
	for _, f := range ps[0].Files {
		if strings.HasSuffix(f.Key, "CLAUDE.md") {
			t.Fatal("CLAUDE.md global listé comme fichier du projet :", f.Key)
		}
	}
	if memRemove("p/-h/.claude/CLAUDE.md") == nil {
		t.Fatal("suppression du CLAUDE.md global via le projet acceptée")
	}
	if s, _ := memRead("g/CLAUDE.md"); s != "@RTK.md\n" {
		t.Fatal("CLAUDE.md global touché")
	}
}
