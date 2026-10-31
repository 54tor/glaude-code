package main

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchRenameGroups(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	put(t, path, `{"type":"user","cwd":"/p","message":{"content":"Déploie Caddy en prod"}}`+"\n"+
		`{"type":"assistant","message":{"content":[{"type":"text","text":"caddy redémarré"},{"type":"tool_use","name":"Bash","input":{"command":"caddy reload"}}]}}`, old)

	// Recherche : insensible à la casse, dans les messages seulement (pas l'appel d'outil).
	hits := search("CADDY")
	if len(hits) != 1 || hits[0].ID != id1 || hits[0].N != 2 || hits[0].Snip != "Déploie Caddy en prod" {
		t.Fatalf("search = %+v", hits)
	}
	if len(search("reload")) != 0 {
		t.Fatal("les entrées d'outils ne doivent pas être cherchées")
	}

	// Renommage : la dernière ligne custom-title fait foi, la date du fichier ne bouge pas.
	before, _ := os.Stat(path)
	if hasTitle(path) {
		t.Fatal("titre détecté avant tout renommage")
	}
	if err := setTitle(path, id1, "Caddy prod"); err != nil {
		t.Fatal(err)
	}
	if !hasTitle(path) {
		t.Fatal("titre posé non détecté")
	}
	st, _ := os.Stat(path)
	if session := info(path, st); session.Title != "Caddy prod" || !session.Named {
		t.Fatalf("titre = %q", session.Title)
	}
	if !st.ModTime().Equal(before.ModTime()) || time.Since(st.ModTime()) < old/2 {
		t.Fatal("la date de modification a changé")
	}
	if len(conv(path, false)) != 2 {
		t.Fatal("la ligne de titre ne doit pas apparaître dans la conversation")
	}

	// Groupes : enregistrés puis relus ; même nom permis dans deux projets, pas dans un seul ; ids invalides refusés.
	store := Store{Groups: []VGroup{{Name: " Infra  prod ", Project: "/p", IDs: []string{id1}, Fav: true}, {Name: "Infra prod", Project: "/q"}}, Favs: []string{id1}}
	if err := saveStore(store); err != nil {
		t.Fatal(err)
	}
	if store := loadStore(); len(store.Groups) != 2 || store.Groups[0].Name != "Infra prod" || !store.Groups[0].Fav || store.Favs[0] != id1 {
		t.Fatalf("groupes = %+v", store)
	}
	bad := []Store{
		{Groups: []VGroup{{Name: "a", Project: "/p"}, {Name: "a", Project: "/p"}}, Favs: []string{}},
		{Groups: []VGroup{{Name: "a", Project: "/p", IDs: []string{"../x"}}}, Favs: []string{}},
		{Groups: []VGroup{{Name: "a"}}, Favs: []string{}},
		{Groups: []VGroup{}, Favs: []string{"x"}},
	}
	for _, store := range bad {
		if saveStore(store) == nil {
			t.Fatalf("accepté : %+v", store)
		}
	}
	// Ancien format (liste sans projet) : chaque groupe est rattaché au dossier de travail de ses sessions.
	put(t, groupsFile(), `[{"name":"Vieux","ids":["`+id1+`","`+id3+`"]}]`, old)
	if store := loadStore(); len(store.Groups) != 1 || store.Groups[0].Project != "/p" || len(store.Groups[0].IDs) != 1 {
		t.Fatalf("migration = %+v", store)
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

	snippets, scopes := listSnippets()
	if len(snippets) != 2 || snippets[0].Key != "u/revue.md" || snippets[0].Command != "/revue" || snippets[0].Description != "Revue de code" ||
		snippets[1].Key != "p/-p/go/tests.md" || snippets[1].Command != "/go:tests" || len(scopes) != 1 || scopes[0].Project != proj {
		t.Fatalf("liste : %+v %+v", snippets, scopes)
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
	if text, _ := readSnippet("p/-p/deploy.md"); text != "v1" {
		t.Fatal("restauration :", text)
	}
	if removeSnippet("p/-p/deploy.md") != nil {
		t.Fatal("suppression")
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "commands", "deploy.md")); err == nil {
		t.Fatal("toujours là")
	}

	// Export puis import dans les commandes personnelles : l'existant est gardé sans overwrite.
	data, count, err := exportSnippets("-p")
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	done, skipped, err := importSnippets("", []ImportFile{{"lot.zip", data}, {"revue.md", []byte("autre")}, {"nouveau.md", []byte("n")}}, false)
	if err != nil || len(done) != 2 || len(skipped) != 1 {
		t.Fatal(done, skipped, err)
	}
	if text, _ := readSnippet("u/go/tests.md"); text != "Écris les tests" {
		t.Fatal("import :", text)
	}
	if text, _ := readSnippet("u/revue.md"); !strings.Contains(text, "$ARGUMENTS") {
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
	writer, _ := zw.Create("../../dehors.md")
	writer.Write([]byte("x"))
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
	cfg := t.TempDir()
	use(cfg)
	path := filepath.Join(cfg, "projects", "-h", "s.jsonl")
	put(t, path, `{"cwd":"/h","x":1}`+"\n"+`{"cwd":"/h/.local/bin"}`+"\n"+`{"cwd":"/h/P/tb"}`+"\n", old)
	cwdRewrite, err := prepareCwd(path, "/h", "/h/P/tb", false)
	if err == nil {
		err = cwdRewrite.commit()
	}
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if want := `{"cwd":"/h/P/tb","x":1}` + "\n" + `{"cwd":"/h/.local/bin"}` + "\n" + `{"cwd":"/h/P/tb"}` + "\n"; string(data) != want {
		t.Fatalf("transcript :\n%s", data)
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
	put(t, filepath.Join(proj, "AGENTS.md"), "agents", old)
	for _, name := range []string{"CONVENTIONS.md", "PLAN.md", "TOOLING.md", "README.md"} {
		put(t, filepath.Join(proj, name), "@quelquun et @fichier-inexistant.md\n", old)
	}

	memFiles, ps, _ := memList()
	if len(memFiles) != 2 || len(ps) != 1 || len(ps[0].Files) != 8 || ps[0].Project != proj {
		t.Fatalf("liste : %+v %+v", memFiles, ps)
	}
	if text, err := memRead("p/-p/CLAUDE.md"); err != nil || text != "projet" {
		t.Fatal(text, err)
	}
	if text, err := memRead("p/-p/AGENTS.md"); err != nil || text != "agents" {
		t.Fatal(text, err)
	}
	if err := memSave("g/rules/r.md", "nouvelle"); err != nil {
		t.Fatal(err)
	}
	if text, _ := memRead("g/rules/r.md"); text != "nouvelle" {
		t.Fatal("enregistrement :", text)
	}
	// Supprimer une entrée retire aussi sa ligne d'index.
	if err := memRemove("m/-p/a.md"); err != nil {
		t.Fatal(err)
	}
	if text, _ := memRead("m/-p/MEMORY.md"); text != "- [B](b.md) — b\n" {
		t.Fatalf("index : %q", text)
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
	if tabSave, saved, _ := saveTabs("  avant  reboot ", false); !saved || tabSave.Name != "avant reboot" || tabSave.Sessions[0].Project != "/p" || tabSave.Sessions[0].Title != "un" {
		t.Fatalf("sauvegarde manuelle : %+v", tabSave)
	}
	// Sauvegardes automatiques : seule la dernière heure avant la plus récente reste ; les manuelles toutes.
	at := func(duration time.Duration) string { return time.Now().Add(-duration).Format(time.RFC3339Nano) }
	ss := []TabSession{{ID: id1}}
	tabSaves := pruneAuto([]TabSave{
		{Auto: true, At: at(10 * time.Hour), Sessions: ss}, // la plus récente, même vieille (nuit éteinte) : gardée
		{Auto: true, At: at(10*time.Hour + 30*time.Minute), Sessions: ss},
		{Name: "vieille manuelle", At: at(48 * time.Hour), Sessions: ss},
		{Auto: true, At: at(12 * time.Hour), Sessions: ss},
	})
	if len(tabSaves) != 3 || tabSaves[2].Name != "vieille manuelle" {
		t.Fatalf("pruneAuto : %+v", tabSaves)
	}
	review := "00000000-0000-4000-8000-0000000000aa"
	put(t, filepath.Join(cfg, "projects", "-p", review+".jsonl"), `{"cwd":"/p","type":"user","message":{"content":"Perform a read-only security review of this codebase. Do not edit."}}`+"\n", old)
	if tabSessions := func() []TabSession {
		open = []string{review}
		defer func() { open = []string{id1} }()
		return liveTabs()
	}(); len(tabSessions) != 0 {
		t.Fatalf("revue de sécurité sauvegardée pour la restauration : %+v", tabSessions)
	}
	open = []string{id1, id2}
	saveTabs("", true)
	all, _ := loadTabs()
	if len(all) != 3 || !all[0].Auto || len(all[0].Sessions) != 2 {
		t.Fatalf("sauvegardes : %+v", all)
	}
	open = nil
	if _, _, err := saveTabs("", true); err == nil {
		t.Fatal("aucune session ouverte : rien ne doit être écrasé")
	}
	if err := removeTabs(all[0].At); err != nil {
		t.Fatal("suppression :", err)
	}
	if rest, _ := loadTabs(); len(rest) != len(all)-1 {
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
	path, cwd, err := mergePrompt([]string{"m/-p/a.md", "m/-p/b.md"}, "")
	if err != nil || cwd != proj || !strings.Contains(path, filepath.Join(cfg, "projects", "-p", "memory", "b.md")) {
		t.Fatal(path, cwd, err)
	}
	argument, body := filepath.Join(cfg, "projects", "-p", "memory", "a.md"), filepath.Join(cfg, "projects", "-p", "memory", "b.md")
	if !strings.Contains(path, "dans "+argument+",") {
		t.Fatal("sortie par défaut : le premier fichier", path)
	}
	if path, _, err = mergePrompt([]string{"m/-p/a.md", "m/-p/b.md"}, "m/-p/b.md"); err != nil || !strings.Contains(path, "dans "+body+",") {
		t.Fatal("sortie choisie", path, err)
	}
	if path, _, err = mergePrompt([]string{"m/-p/a.md", "m/-p/b.md"}, "p/-p/AGENTS.md"); err != nil || !strings.Contains(path, "dans "+filepath.Join(proj, "AGENTS.md")+" (") {
		t.Fatal("sortie AGENTS.md", path, err)
	}
	if _, _, err = mergePrompt([]string{"m/-p/a.md", "m/-p/b.md"}, "m/-p/c.md"); err == nil {
		t.Fatal("sortie hors sélection acceptée")
	}
	if _, _, err := mergePrompt([]string{"m/-p/a.md"}, ""); err == nil {
		t.Fatal("un seul fichier accepté")
	}
	if _, _, err := mergePrompt([]string{"m/-p/a.md", "m/-p/../x.md"}, ""); err == nil {
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

	if dst, err := memTransfer("m/-a/x.md", "-b", false); err != nil || dst != "m/-b/x.md" {
		t.Fatal(dst, err)
	}
	if text, _ := memRead("m/-b/x.md"); text != "contenu x" {
		t.Fatal("contenu :", text)
	}
	if text, _ := memRead("m/-a/MEMORY.md"); text != "- [Y](y.md) — y\n" {
		t.Fatalf("index source : %q", text)
	}
	if text, _ := memRead("m/-b/MEMORY.md"); text != "- [Z](z.md) — z\n- [X](x.md) — accroche\n" {
		t.Fatalf("index destination : %q", text)
	}
	put(t, filepath.Join(cfg, "projects", "-a", "memory", "y.md"), "y", old)
	if _, err := memTransfer("m/-a/y.md", "-b", false); err == nil {
		t.Fatal("écrasement accepté")
	}
	if _, err := memTransfer("m/-b/x.md", "global", false); err != nil {
		t.Fatal(err)
	}
	if text, _ := memRead("g/rules/x.md"); text != "contenu x" {
		t.Fatal("vers les règles :", text)
	}
	for _, bad := range [][2]string{{"m/-a/y.md", "../x"}, {"m/-a/y.md", "-inexistant"}, {"g/CLAUDE.md", "-b"}} {
		if _, err := memTransfer(bad[0], bad[1], false); err == nil {
			t.Fatal("déplacement accepté :", bad)
		}
	}
}

func TestPastedTitle(t *testing.T) {
	callback := func(content string) *line {
		var localLine line
		localLine.Message.Content = []byte(content)
		return &localLine
	}
	if got := promptOf(callback(`"le cdn ne démarre pas <pasted_content id=\"274a\">\nERR 42\n</pasted_content id=\"274a\"> merci"`)); got != "le cdn ne démarre pas [texte collé] merci" {
		t.Fatalf("%q", got)
	}
	if got := promptOf(callback(`"\n<pasted_content id=\"1\">\nseul\n</pasted_content id=\"1\">\n"`)); got != "" {
		t.Fatalf("collage seul : %q", got)
	}
}

func TestResumePrompt(t *testing.T) {
	liveSessions = func() map[string]bool { return map[string]bool{id2: true} }
	if checkResumePrompt("", "") != nil || checkResumePrompt(id1, "continue") != nil {
		t.Fatal("message valide refusé")
	}
	for _, values := range [][2]string{{"", "sans session"}, {id2, "session ouverte"}, {id1, "nul\x00"}, {id1, strings.Repeat("x", 40<<10)}} {
		if checkResumePrompt(values[0], values[1]) == nil {
			t.Fatalf("accepté : %q", values[1][:min(len(values[1]), 20)])
		}
	}
	if !viaCmdExe(`C:\npm\claude.CMD`) || !viaCmdExe("claude.bat") || viaCmdExe(`C:\u\.local\bin\claude.exe`) {
		t.Fatal("viaCmdExe")
	}
}

func TestChat(t *testing.T) {
	values := chatArgs([]string{"--model", "x"}, id1, "acceptEdits", "--pas-une-option")
	if strings.Join(values, " ") != "--model x -p --resume "+id1+" --output-format stream-json --verbose --permission-mode acceptEdits -- --pas-une-option" {
		t.Fatal(values)
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
	if !relay(strings.NewReader(out), func(localChatEvent chatEvent) { evs = append(evs, localChatEvent) }) {
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
	if data, _ := os.ReadFile(filepath.Join(cfg, "gld-web-groups.json")); string(data) != "ancien" {
		t.Fatal("groupes non migrés")
	}
	if data, _ := os.ReadFile(filepath.Join(cfg, "gld-web.log")); string(data) != "nouveau" {
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
	trasheds := trashList()
	if len(trasheds) != 2 || trasheds[0].Key != "m/-p/a.md" || trasheds[0].Reason != "remplacé" || trasheds[1].Key != "g/CLAUDE.md" || trasheds[1].Reason != "supprimé" {
		t.Fatalf("corbeille : %+v", trasheds)
	}
	if _, err := trashRestore(trasheds[1].ID); err != nil {
		t.Fatal(err)
	}
	if text, _ := memRead("g/CLAUDE.md"); text != "@RTK.md\n" {
		t.Fatalf("restauré : %q", text)
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
	if data, _ := os.ReadFile(filepath.Join(cfg, "gld-web.log")); !strings.Contains(string(data), " test 1\n") {
		t.Fatalf("journal : %q", data)
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
	for _, values := range [][2]string{{"gpt", "x"}, {"haiku", ""}} {
		if _, _, err := memWriteDraft(values[0], values[1], "", nil); err == nil {
			t.Fatal("accepté :", values)
		}
	}
	if _, _, err := memWriteDraft("haiku", "x", "", []string{"m/../a.md"}); err == nil {
		t.Fatal("référence invalide acceptée")
	}
	if sys != memSystem() {
		t.Fatal("une entrée de mémoire se rédige au format mémoire")
	}
	// Conversion en CLAUDE.md global (absent ici) : format CLAUDE.md, fichier à créer.
	if _, _, err := memWriteDraft("haiku", "convertis", "g/CLAUDE.md", []string{"m/-p/a.md"}); err != nil || sys != claudeSystem() || !strings.Contains(got, "n'existe pas encore") {
		t.Fatal(err, got)
	}
	// Mise en forme d'une commande : format commande, texte transmis, consigne par défaut.
	if _, _, err := snippetDraft("haiku", "", "fais une revue de code"); err != nil || sys != cmdSystem() || !strings.Contains(got, "fais une revue de code") || !strings.Contains(got, "Mets ce texte en forme") {
		t.Fatal(err, got)
	}
	if _, _, err := snippetDraft("haiku", "", " "); err == nil {
		t.Fatal("rien à mettre en forme : accepté")
	}
	// Nouvelle entrée : sa ligne s'ajoute à l'index, une seule fois.
	text = "---\nname: Entrée B\ndescription: ce que dit B\nmetadata:\n  type: project\n---\nB\n"
	if memSave("m/-p/b.md", text) != nil || memIndex("m/-p/b.md", text) != nil || memIndex("m/-p/b.md", text) != nil {
		t.Fatal("enregistrement")
	}
	if text, _ := memRead("m/-p/MEMORY.md"); text != "- [A](a.md) — a\n- [Entrée B](b.md) — ce que dit B\n" {
		t.Fatalf("index : %q", text)
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
	memFiles, ps, _ := memList()
	if len(memFiles) != 1 || len(ps) != 1 {
		t.Fatalf("%+v %+v", memFiles, ps)
	}
	for _, memFile := range ps[0].Files {
		if strings.HasSuffix(memFile.Key, "CLAUDE.md") {
			t.Fatal("CLAUDE.md global listé comme fichier du projet :", memFile.Key)
		}
	}
	if memRemove("p/-h/.claude/CLAUDE.md") == nil {
		t.Fatal("suppression du CLAUDE.md global via le projet acceptée")
	}
	if text, _ := memRead("g/CLAUDE.md"); text != "@RTK.md\n" {
		t.Fatal("CLAUDE.md global touché")
	}
}

// Une aide qui échoue rapporte la dernière ligne de sa sortie d'erreur, pas seulement « exit status 1 ».
func TestHelperOutputStderr(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "echo détail >&2; echo 'Not logged in' >&2; exit 1")
	_, err := helperOutput(context.Background(), cmd)
	if err == nil || !strings.HasSuffix(err.Error(), "exit status 1 : Not logged in") {
		t.Fatalf("erreur : %v", err)
	}
	if _, err := helperOutput(context.Background(), exec.Command("sh", "-c", "exit 2")); err == nil || err.Error() != "exit status 2" {
		t.Fatalf("sans stderr : %v", err)
	}
}

func TestTextCacheBornee(t *testing.T) {
	textCache.Clear()
	textBytes.Store(textCacheMax)
	path := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(path, []byte("{\"type\":\"user\",\"message\":{\"content\":\"bonjour\"}}\n"), 0o600)
	if got := messageTexts(path); len(got) != 1 {
		t.Fatalf("textes : %v", got)
	}
	if count := textBytes.Load(); count > textCacheMax/2 {
		t.Fatalf("le cache plein n'a pas été vidé : %d", count)
	}
}

func TestReadTabsDropsAutoReviews(t *testing.T) {
	data := []byte(`[{"at":"1","sessions":[{"id":"a","title":"Perform a read-only security review of this codebase. x"},{"id":"b","title":"travail"}]},
	{"at":"2","sessions":[{"id":"c","title":"You previously flagged these candidate vulnerabilities: ["}]}]`)
	got, err := readTabs(data)
	if err != nil || len(got) != 1 || len(got[0].Sessions) != 1 || got[0].Sessions[0].ID != "b" {
		t.Fatalf("readTabs = %+v, %v", got, err)
	}
}

func TestSnippetMergePrompt(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	snipDir = filepath.Join(cfg, "commands")
	t.Cleanup(func() { snipDir = "" })
	put(t, filepath.Join(cfg, "commands", "a.md"), "---\ndescription: A\n---\na", old)
	put(t, filepath.Join(cfg, "commands", "b.md"), "b", old)
	path, cwd, err := snippetMergePrompt([]string{"u/a.md", "u/b.md"}, "u/b.md")
	body := filepath.Join(cfg, "commands", "b.md")
	if err != nil || cwd != cfg || !strings.Contains(path, "- "+filepath.Join(cfg, "commands", "a.md")) || !strings.Contains(path, "dans "+body+",") {
		t.Fatal(path, cwd, err)
	}
	for _, record := range []struct {
		keys []string
		out  string
	}{{[]string{"u/a.md"}, ""}, {[]string{"u/a.md", "u/b.md"}, "u/c.md"}, {[]string{"u/a.md", "u/absent.md"}, ""}} {
		if _, _, err := snippetMergePrompt(record.keys, record.out); err == nil {
			t.Error("refus attendu :", record)
		}
	}
}
