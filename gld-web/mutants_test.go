package main

// Tests issus des tests de mutation (gremlins) : chacun fige une limite ou une branche qu'aucun autre test ne vérifiait.

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func near(a, b float64) bool { return math.Abs(a-b) < 2 }

func mtimeOf(t *testing.T, p string) float64 {
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return float64(st.ModTime().UnixNano()) / 1e9
}

func TestEnvEtTexte(t *testing.T) {
	t.Setenv("GLD_TEST_X", "v")
	if env("GLD_TEST_X", "d") != "v" || env("GLD_TEST_ABSENT", "d") != "d" {
		t.Fatal("env")
	}
	var l line
	l.Message.Content = []byte(`[{"type":"text","text":"bonjour"},{"type":"text","text":"toi"}]`)
	if got := promptOf(&l); got != "bonjour toi" {
		t.Fatalf("prompt en blocs : %q", got)
	}
	p := filepath.Join(t.TempDir(), "x")
	for _, c := range []string{"a\nb\n", "a\nb"} {
		os.WriteFile(p, []byte(c), 0o644)
		n := 0
		scan(p, func([]byte) { n++ })
		if n != 2 {
			t.Fatalf("scan %q : %d lignes", c, n)
		}
	}
	if toolArg([]byte(`{"file_path":"","pattern":"x"}`)) != "x" || toolArg([]byte(`{"command":"ls"}`)) != "ls" || toolArg([]byte(`[1]`)) != "[1]" {
		t.Fatal("toolArg")
	}
	if flat([]byte(`[{"text":"a"},{"text":"b"}]`)) != "a\nb" || flat([]byte(`"s"`)) != "s" {
		t.Fatal("flat")
	}
	// Une ligne sans message lisible n'émet rien.
	var n int
	relay(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"x"}]}}`+"\n"), func(chatEvent) { n++ })
	if n != 0 {
		t.Fatal("événement vide émis")
	}
	// Extrait : 60 caractères avant, 100 après la longueur cherchée, marqués « … » s'ils sont coupés.
	_, s := snippet(strings.Repeat("x", 100)+"cible"+strings.Repeat("y", 200), "cible")
	if s != "…"+strings.Repeat("x", 60)+"cible"+strings.Repeat("y", 100)+"…" {
		t.Fatalf("extrait : %q", s)
	}
}

func TestMemeDossier(t *testing.T) {
	d := t.TempDir()
	lien := filepath.Join(t.TempDir(), "lien")
	os.Symlink(d, lien)
	if !sameDir(d, lien) || sameDir(d, filepath.Join(d, "absent")) || sameDir(filepath.Join(d, "absent"), d) {
		t.Fatal("sameDir")
	}
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "g", old)
	ailleurs := filepath.Join(t.TempDir(), "CLAUDE.md")
	os.Symlink(filepath.Join(cfg, "CLAUDE.md"), ailleurs)
	autre := filepath.Join(t.TempDir(), "CLAUDE.md")
	put(t, autre, "g", old)
	if !globalClaude(ailleurs) || globalClaude(autre) || globalClaude(filepath.Join(cfg, "absent", "CLAUDE.md")) {
		t.Fatal("globalClaude")
	}
}

func TestLimitesSnippets(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".claude")
	use(cfg)
	snipDir = ""
	pile := strings.Repeat("a", snippetMax)
	put(t, filepath.Join(cfg, "commands", "max.md"), pile, old)
	put(t, filepath.Join(cfg, "commands", "trop.md"), pile+"a", old)
	l, _ := listSnippets()
	if len(l) != 1 || l[0].Key != "u/max.md" || l[0].KB != snippetMax/1024 || !near(l[0].Mtime, mtimeOf(t, filepath.Join(cfg, "commands", "max.md"))) {
		t.Fatalf("liste : %+v", l)
	}
	if s, err := readSnippet("u/max.md"); err != nil || len(s) != snippetMax {
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
	if s, _ := readSnippet("u/max.md"); s != "v2" {
		t.Fatal("modification non écrite :", len(s))
	}
	os.MkdirAll(filepath.Join(cfg, "commands", "dossier.md"), 0o755)
	if saveSnippet("u/dossier.md", "x", false) == nil {
		t.Fatal("écriture sur un dossier acceptée")
	}

	// Deux projets : les scopes sont triés.
	for i, d := range []string{"-b", "-a"} {
		proj := filepath.Join(home, "proj"+d)
		os.MkdirAll(proj, 0o755)
		put(t, filepath.Join(cfg, "projects", d, fmt.Sprintf("%08d-0000-0000-0000-000000000000.jsonl", i)), `{"cwd":"`+proj+`"}`+"\n", old)
	}
	if _, sc := listSnippets(); len(sc) != 2 || sc[0].Project > sc[1].Project {
		t.Fatalf("scopes : %+v", sc)
	}

	// Import : une entrée d'archive à la limite passe entière, au-delà elle est ignorée ; scope de projet.
	zipOf := func(files map[string]string) []byte {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		for n, c := range files {
			w, _ := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
			w.Write([]byte(c))
		}
		zw.Close()
		return b.Bytes()
	}
	done, _, err := importSnippets("-a", []ImportFile{{"l.zip", zipOf(map[string]string{"z.md": pile, "zz.md": pile + "a"})}}, false)
	if err != nil || len(done) != 1 || done[0] != "z.md" {
		t.Fatal(done, err)
	}
	if s, err := readSnippet("p/-a/z.md"); err != nil || len(s) != snippetMax {
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

func TestRechercheEtCache(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	put(t, p, `{"type":"user","message":{"content":"alpha"}}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"type":"user","message":{"content":"rien"}}`+"\n", old)
	if h := search("alpha"); len(h) != 1 {
		t.Fatalf("recherche : %+v", h)
	}
	// Même date, autre taille : le cache ne doit pas resservir l'ancien texte.
	put(t, p, `{"type":"user","message":{"content":"beta, plus long"}}`+"\n", old)
	st, _ := os.Stat(p)
	os.Chtimes(p, st.ModTime(), st.ModTime())
	if h := search("beta"); len(h) != 1 {
		t.Fatal("texte périmé servi par le cache")
	}
	nom := strings.Repeat("é", 80)
	if saveStore(Store{Groups: []VGroup{{Name: nom, Project: "/p"}}, Favs: []string{}}) != nil ||
		saveStore(Store{Groups: []VGroup{{Name: nom + "é", Project: "/p"}}, Favs: []string{}}) == nil {
		t.Fatal("nom de groupe à la limite")
	}
}

func TestSessionsEtDetail(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	base := t.TempDir()
	proj, proj2 := filepath.Join(base, "un"), filepath.Join(base, "deux")
	cache := filepath.Join(base, "mon projet.v2", "sous")
	for _, d := range []string{proj, proj2, cache} {
		os.MkdirAll(d, 0o755)
	}
	pp := filepath.Join(cfg, "projects")
	put(t, filepath.Join(pp, "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n"+strings.Repeat(" ", 3000)+"\n", 3*time.Hour)
	put(t, filepath.Join(pp, "-p", id2+".jsonl"), `{"type":"user","message":{"content":"sans cwd"}}`+"\n", time.Hour) // rangée avec sa voisine
	put(t, filepath.Join(pp, "-p", id3+".jsonl"), `{"cwd":"`+proj2+`"}`+"\n", 2*time.Hour)
	enc := nonAlnum.ReplaceAllString(cache, "-")
	put(t, filepath.Join(pp, enc, "44444444-4444-4444-4444-444444444444.jsonl"), `{"type":"user","message":{"content":"x"}}`+"\n", old)
	put(t, filepath.Join(pp, "-q", "55555555-5555-5555-5555-555555555555.jsonl"), "", old) // vide : ignorée

	gs := sessions()
	if len(gs) != 3 || gs[0].Project > gs[1].Project || gs[1].Project > gs[2].Project {
		t.Fatalf("groupes : %+v", gs)
	}
	by := map[string]Group{}
	for _, g := range gs {
		by[g.Project] = g
	}
	g := by[proj]
	if len(g.Sessions) != 2 || g.Sessions[0].ID != id2 || g.Sessions[1].ID != id1 || len(by[proj2].Sessions) != 1 || len(by[cache].Sessions) != 1 {
		t.Fatalf("regroupement : %+v", gs)
	}
	s := g.Sessions[1]
	if s.KB != 2 || !near(s.Mtime, mtimeOf(t, filepath.Join(pp, "-p", id1+".jsonl"))) {
		t.Fatalf("taille et date : %+v", s)
	}
	if decodeDir("-nexiste-pas-du-tout") != "" {
		t.Fatal("dossier inventé décodé")
	}

	// Détail : premières et dernières dates, prompts (3 derniers), modèles distincts triés, sous-agents.
	var b strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, `{"type":"user","timestamp":"t%d","message":{"content":"p%d"}}`+"\n", i, i)
	}
	b.WriteString(`{"type":"user","message":{"content":[{"type":"tool_result","content":"r"}]}}` + "\n")
	b.WriteString(`{"type":"assistant","message":{"model":"b"}}` + "\n" + `{"type":"assistant","message":{"model":"a"}}` + "\n" + `{"type":"assistant","message":{"model":"a"}}` + "\n")
	dp := filepath.Join(pp, "-p", id1+".jsonl")
	put(t, dp, b.String(), old)
	put(t, filepath.Join(pp, "-p", id1, "subagents", "a.jsonl"), "x", old)
	put(t, filepath.Join(pp, "-p", id1, "subagents", "b.jsonl"), "x", old)
	put(t, filepath.Join(pp, "-p", id1, "subagents", "c.txt"), "x", old)
	d := detail(dp)
	if d.T0 != "t1" || d.T1 != "t5" || d.N != 5 || strings.Join(d.Prompts, ",") != "p3,p4,p5" || strings.Join(d.Models, ",") != "a,b" || d.Subagents != 2 {
		t.Fatalf("détail : %+v", d)
	}
}

func TestReassignation(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	encSrc, encDst := nonAlnum.ReplaceAllString(src, "-"), nonAlnum.ReplaceAllString(dst, "-")
	jl := filepath.Join(cfg, "projects", encSrc, id1+".jsonl")
	put(t, jl, `{"cwd":"`+src+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", encSrc, id1, "subagents", "a.jsonl"), `{"cwd":"`+src+`"}`+"\n", old)
	before := mtimeOf(t, jl)

	liveSessions = func() map[string]bool { return map[string]bool{id1: true} }
	if _, err := reassign([]item{{encSrc, id1}}, dst); err == nil || !strings.Contains(err.Error(), "session vivante, ferme-la d'abord : 11111111") {
		t.Fatal(err)
	}
	use(cfg)
	if _, err := reassign([]item{{encSrc, id1}}, src); err == nil || !strings.Contains(err.Error(), "déjà dans ce dossier : 11111111") {
		t.Fatal(err)
	}
	put(t, filepath.Join(cfg, "projects", encDst, id1+".jsonl"), "{}\n", old)
	if _, err := reassign([]item{{encSrc, id1}}, dst); err == nil || !strings.Contains(err.Error(), "existe déjà dans la cible : "+id1+".jsonl") {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(cfg, "projects", encDst, id1+".jsonl"))
	if n, err := reassign([]item{{encSrc, id1}}, dst); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	moved := filepath.Join(cfg, "projects", encDst, id1+".jsonl")
	b, _ := os.ReadFile(moved)
	sub, _ := os.ReadFile(filepath.Join(cfg, "projects", encDst, id1, "subagents", "a.jsonl"))
	if !strings.Contains(string(b), `"cwd":"`+dst+`"`) || !strings.Contains(string(sub), `"cwd":"`+dst+`"`) {
		t.Fatalf("cwd non réécrit :\n%s\n%s", b, sub)
	}
	if !near(mtimeOf(t, moved), before) {
		t.Fatal("la date du transcript a changé")
	}
}

func TestOuvertsEtRecents(t *testing.T) {
	h, cfg, _ := web(t)
	t.Setenv("FAKE_AGENTS", `[{"sessionId":"a","status":"busy","state":"blocked"},{"sessionId":"b","status":"busy","state":"running"},{"sessionId":"c","status":"idle"}]`)
	if w := waiting(); strings.Join(w, ",") != "a,c" {
		t.Fatalf("en attente : %v", w)
	}
	status(t, h, "GET", "/ls?path=::drives", "", 400) // la liste des lecteurs n'existe que sous Windows

	// Sessions à rouvrir : récentes (2 jours < 7), assez longues (10 octets, limite comprise), pas ouvertes, récentes d'abord.
	dir := filepath.Join(cfg, "projects", "-r")
	titled := func(s string) string { return `{"type":"user","cwd":"/r","message":{"content":"` + s + `"}}` + "\n" }
	put(t, filepath.Join(dir, id1+".jsonl"), titled("un"), 48*time.Hour)
	put(t, filepath.Join(dir, id2+".jsonl"), titled("deux"), time.Hour)
	put(t, filepath.Join(dir, id3+".jsonl"), "123456789\n", time.Hour) // exactement 10 octets
	put(t, filepath.Join(dir, "44444444-4444-4444-4444-444444444444.jsonl"), "court\n", time.Hour)
	put(t, filepath.Join(dir, "55555555-5555-5555-5555-555555555555.jsonl"), titled("vieille"), 8*24*time.Hour)
	put(t, filepath.Join(dir, "66666666-6666-6666-6666-666666666666.jsonl"), titled("ouverte"), time.Hour)
	liveSessions = func() map[string]bool { return map[string]bool{"66666666-6666-6666-6666-666666666666": true} }
	var ids []string
	for _, s := range recentSessions(dir, 7, 10) {
		ids = append(ids, s.ID[:1])
	}
	if strings.Join(ids, "") != "231" && strings.Join(ids, "") != "321" {
		t.Fatalf("récentes : %v", ids)
	}
	if ids[2] != "1" {
		t.Fatal("tri : la plus ancienne en dernier")
	}
}

func TestLimitesHTTP(t *testing.T) {
	h, cfg, _ := web(t)
	pile := strings.Repeat("a", snippetMax)
	ok(t, h, "/snippet-save", `{"key":"u/max.md","text":"`+pile+`"}`, true, "Snippet enregistré")
	t.Setenv("FAKE_DRAFT", "stdin") // le prompt arrive sur stdin, quelle que soit sa taille
	if m := ok(t, h, "/snippet-draft", `{"model":"haiku","instruction":"x","text":"`+pile+`"}`, true, "Mise en forme prête"); m["text"] != "Consigne : x\n" {
		t.Fatalf("prompt reçu : %v", m["text"])
	}
	ok(t, h, "/memory-save", `{"key":"m/-p/max.md","text":"`+strings.Repeat("a", memMax)+`"}`, true, "Enregistré")

	// Le compte rendu de suppression est joint au message.
	put(t, filepath.Join(cfg, "history.jsonl"), `{"sessionId":"`+id1+`"}`+"\n", old)
	ok(t, h, "/rm", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, true, "1 supprimée(s), 0 vivante(s) épargnée(s)\\n  history.jsonl   : 1 ligne(s)")

	// Réponse du chat poussée au fil de l'eau.
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"cwd":"`+filepath.Join(cfg, "..", "p")+`"}`+"\n", old)
	t.Setenv("FAKE_CHAT", `{"type":"result","subtype":"success"}`)
	r := httptest.NewRequest("POST", "/chat", strings.NewReader(`{"dir":"-p","id":"`+id2+`","prompt":"x"}`))
	r.Host = testHost
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !w.Flushed || !strings.Contains(w.Body.String(), `"done":true`) {
		t.Fatalf("chat non poussé : %v %s", w.Flushed, w.Body)
	}
}

func TestArretSansSignal(t *testing.T) {
	h, _, _ := web(t)
	// Un processus qui ignore l'interruption n'est pas tué pour autant : « Arrêter » demande, ne force pas.
	cmd := exec.Command("/bin/sh", "-c", `trap "" INT; /bin/sleep 30`)
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer cmd.Process.Kill()
	chatMu.Lock()
	chatRun[id1] = cmd
	chatMu.Unlock()
	defer func() { chatMu.Lock(); delete(chatRun, id1); chatMu.Unlock() }()
	time.Sleep(100 * time.Millisecond) // le temps que trap soit posé
	ok(t, h, "/chat-stop", `{"id":"`+id1+`"}`, true, "Arrêt demandé")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Fatal("processus tué au lieu d'être interrompu")
	case <-time.After(300 * time.Millisecond):
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
	if f := ps[1].Files[0]; f.KB != 2 || !near(f.Mtime, mtimeOf(t, f.Path)) {
		t.Fatalf("fichier : %+v", f)
	}
	for i := 1; i < len(ts); i++ {
		if ts[i-1].Project > ts[i].Project {
			t.Fatalf("cibles non triées : %+v", ts)
		}
	}

	pile := strings.Repeat("a", memMax)
	if memSave("m/-a/max.md", pile) != nil || memSave("m/-a/trop.md", pile+"a") == nil {
		t.Fatal("écriture à la limite")
	}
	if s, err := memRead("m/-a/max.md"); err != nil || len(s) != memMax {
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

	// Fusion : de 2 à 20 fichiers, tous présents ; le dossier de lancement suit une clé de projet.
	var keys []string
	for i := range 21 {
		k := fmt.Sprintf("m/-a/f%02d.md", i)
		put(t, filepath.Join(pp, "-a", "memory", filepath.Base(k)), "f", old)
		keys = append(keys, k)
	}
	if _, _, err := mergePrompt(keys[:20]); err != nil {
		t.Fatal("20 fichiers refusés :", err)
	}
	if _, _, err := mergePrompt(keys); err == nil {
		t.Fatal("21 fichiers acceptés")
	}
	if _, _, err := mergePrompt([]string{"m/-a/y.md", "m/-a/absent.md"}); err == nil || !strings.Contains(err.Error(), "introuvable : "+filepath.Join(pp, "-a", "memory", "absent.md")) {
		t.Fatal(err)
	}
	put(t, filepath.Join(cfg, "CLAUDE.md"), "g", old)
	if _, cwd, err := mergePrompt([]string{"g/CLAUDE.md", "p/-a/.claude/CLAUDE.md"}); err != nil || cwd != pa {
		t.Fatal("dossier de fusion :", cwd, err)
	}

	// Brouillons : bornes de la consigne, du texte et des références.
	memDraft = func(model, system, prompt string) (string, float64, error) { return "ok\n", 0, nil }
	for _, c := range [][2]string{{"", ""}, {strings.Repeat("c", 8<<10+1), ""}, {"", strings.Repeat("t", snippetMax+1)}} {
		if _, _, err := snippetDraft("haiku", c[0], c[1]); err == nil {
			t.Fatalf("mise en forme acceptée : %d/%d", len(c[0]), len(c[1]))
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

func TestPurgeRestes(t *testing.T) {
	t.Setenv("GLD_GRACE", "")
	for v, want := range map[string]time.Duration{"": time.Hour, "0": 0, "5": 5 * time.Minute, "-5": time.Hour, "x": time.Hour} {
		t.Setenv("GLD_GRACE", v)
		if g := grace(); g != want {
			t.Fatalf("GLD_GRACE=%q : %v", v, g)
		}
	}
	if h := lineHashes([]byte(`{"pastedContents":[{"contentHash":"x"},{"contentHash":""}]}`)); len(h) != 1 || h[0] != "x" {
		t.Fatal("collages en liste :", h)
	}
	if lineHashes([]byte(`{"pastedContents":"?"}`)) != nil {
		t.Fatal("collages illisibles")
	}

	cfg := t.TempDir()
	use(cfg)
	pp := filepath.Join(cfg, "projects", "-p")
	hist := filepath.Join(cfg, "history.jsonl")
	ss := filepath.Join(cfg, "shell-snapshots")
	// id1 : deux instantanés sur une ligne, orphelins ; id2 : un collage seul ; id3 : un instantané seul.
	put(t, filepath.Join(pp, id1+".jsonl"), `{"m":"snapshot-bash-444-d.sh snapshot-bash-555-e.sh snapshot-bash-666-f.sh"}`+"\n", old)
	put(t, filepath.Join(pp, id2+".jsonl"), "{}\n", old)
	put(t, filepath.Join(pp, id3+".jsonl"), `{"m":"snapshot-bash-888-h.sh"}`+"\n", old)
	put(t, filepath.Join(pp, "77777777-7777-7777-7777-777777777777.jsonl"), `{"m":"snapshot-bash-666-f.sh snapshot-bash-999-i.sh"}`+"\n", old) // gardée
	put(t, hist, `{"sessionId":"`+id2+`","pastedContents":{"1":{"contentHash":"dddddddddddddddd"}}}`+"\n", old)
	os.Chmod(hist, 0o644)
	for _, s := range []string{"444-d", "555-e", "666-f", "888-h", "999-i"} {
		put(t, filepath.Join(ss, "snapshot-bash-"+s+".sh"), "s", old)
	}
	put(t, filepath.Join(cfg, "paste-cache", "dddddddddddddddd.txt"), "c", old)

	if _, _, notes, err := purgeSessions([]item{{"-p", id1}}); err != nil || len(notes) != 1 || notes[0] != "  shell-snapshots : 2 instantané(s) effacé(s)" {
		t.Fatal(notes, err)
	}
	if exists(filepath.Join(ss, "snapshot-bash-444-d.sh")) || exists(filepath.Join(ss, "snapshot-bash-555-e.sh")) || !exists(filepath.Join(ss, "snapshot-bash-666-f.sh")) {
		t.Fatal("instantanés d'une même ligne")
	}
	// Sans ligne d'historique ni reste : history.jsonl n'est pas réécrit, aucun compte rendu.
	h0 := mtimeOf(t, hist)
	if _, _, notes, _ := purgeSessions([]item{{"-p", id3}}); len(notes) != 1 || !strings.Contains(notes[0], "1 instantané") {
		t.Fatal("instantané seul :", notes)
	}
	if mtimeOf(t, hist) != h0 {
		t.Fatal("history.jsonl réécrit sans raison")
	}
	if _, _, notes, _ := purgeSessions([]item{{"-p", id2}}); len(notes) != 2 || !strings.Contains(notes[1], "1 collage") {
		t.Fatal("collage seul :", notes)
	}
	if st, _ := os.Stat(hist); st.Mode().Perm() != 0o644 {
		t.Fatal("droits de history.jsonl perdus :", st.Mode())
	}
	if exists(filepath.Join(cfg, "paste-cache", "dddddddddddddddd.txt")) {
		t.Fatal("collage orphelin gardé")
	}
	put(t, filepath.Join(pp, "88888888-8888-8888-8888-888888888888.jsonl"), "{}\n", old)
	if _, _, notes, _ := purgeSessions([]item{{"-p", "88888888-8888-8888-8888-888888888888"}}); len(notes) != 0 {
		t.Fatal("compte rendu sans rien d'effacé :", notes)
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
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339Nano) }
	ss := []TabSession{{ID: id1}}
	if l := pruneAuto([]TabSave{{Auto: true, At: at(0), Sessions: ss}, {Auto: true, At: at(keepAuto), Sessions: ss}}); len(l) != 2 {
		t.Fatal("sauvegarde à une heure pile retirée")
	}

	if err := saveTrash("g/CLAUDE.md", "/x", "supprimé", strings.Repeat("a", 3000)); err != nil {
		t.Fatal(err)
	}
	if l := trashList(); len(l) != 1 || l[0].KB != 2 || !near(l[0].At, float64(time.Now().UnixNano())/1e9) {
		t.Fatalf("corbeille : %+v", l)
	}
}

// main : démarre, sert sur 127.0.0.1 et localhost, et s'arrête proprement sur interruption.
// Dans un sous-processus (le binaire de test relancé) : main laisse tourner la sauvegarde des onglets.
func TestMainServeur(t *testing.T) {
	if os.Getenv("GLD_TEST_MAIN") == "1" {
		main()
		return
	}
	home := t.TempDir()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	p := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainServeur$")
	cmd.Env = append(os.Environ(), "GLD_TEST_MAIN=1", "GLD_WEB_PORT="+p, "HOME="+home, "PATH="+t.TempDir(),
		"CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var resp *http.Response
	for range 300 {
		if resp, err = http.Get("http://127.0.0.1:" + p + "/api"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("serveur :", err, out.String())
	}
	b := new(bytes.Buffer)
	b.ReadFrom(resp.Body)
	resp.Body.Close()
	if !strings.Contains(b.String(), `"root":"`+filepath.Join(home, ".claude", "projects")+`"`) {
		t.Fatal("root :", b)
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+p+"/api", nil)
	req.Host = "localhost:" + p
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatal("localhost refusé :", err)
	}
	cmd.Process.Signal(syscall.SIGINT)
	if err := cmd.Wait(); err != nil || !strings.Contains(out.String(), "arrêt.") {
		t.Fatalf("arrêt sur interruption : %v\n%s", err, out.String())
	}
}
