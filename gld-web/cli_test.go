package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cliEnv : une config Claude vide, un projet qui existe et le dossier de ses transcripts.
func cliEnv(t *testing.T) (cfg, proj, dir string) {
	cfg, proj = t.TempDir(), t.TempDir()
	use(cfg)
	t.Setenv("GLD_MATIN_PAUSE", "0")
	t.Setenv("GLD_MATIN_ARGS", "")
	t.Setenv("GLD_MATIN_EXCLURE", "")
	t.Setenv("GLD_MATIN_TERM", "")
	return cfg, proj, filepath.Join(cfg, "projects", nonAlnum.ReplaceAllString(proj, "-"))
}

func session(t *testing.T, dir, id, title, prompt string, age time.Duration) {
	var b strings.Builder
	if title != "" {
		b.WriteString(`{"type":"custom-title","customTitle":"` + title + `","sessionId":"` + id + `"}` + "\n")
	}
	b.WriteString(`{"type":"user","cwd":"/p","message":{"content":"` + prompt + `"}}` + "\n")
	put(t, filepath.Join(dir, id+".jsonl"), b.String(), age)
}

func run(t *testing.T, fn cliFunc, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := fn(context.Background(), args, &out)
	return out.String(), err
}

func contains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("manque %q dans :\n%s", w, got)
		}
	}
}

func TestDispatch(t *testing.T) {
	for _, argv := range [][]string{{"gld-web"}, {"gld-web", "inconnu"}, {"/bin/gld-web.exe", "--listen", "x"}} {
		if _, ok := dispatchCLI(argv); ok {
			t.Errorf("%v ne doit pas être une commande", argv)
		}
	}
	for argv, want := range map[string]int{"gld-matin --bogus": 1, "gld-web matin --bogus": 1, "/usr/bin/gld-matin.exe -h": 0} {
		if code, ok := dispatchCLI(strings.Fields(argv)); !ok || code != want {
			t.Errorf("%q : code %d (ok=%v), attendu %d", argv, code, ok, want)
		}
	}
}

func TestMatin(t *testing.T) {
	_, proj, dir := cliEnv(t)
	session(t, dir, id1, "Alpha", "a", time.Hour)
	session(t, dir, id2, "", "Déploie caddy", 2*time.Hour)
	session(t, dir, id3, "Alpha", "ancienne", 3*time.Hour) // homonyme plus ancien
	session(t, dir, "44444444-4444-4444-4444-444444444444", "Vieille", "v", 30*24*time.Hour)
	live4 := "55555555-5555-5555-5555-555555555555"
	session(t, dir, live4, "Ouverte", "o", time.Hour)
	liveSessions = func() map[string]bool { return map[string]bool{live4: true} }

	type tab struct {
		launcher, title string
		spec            tabSpec
	}
	var tabs []tab
	real := openTab
	openTab = func(l, title string, s tabSpec) error { tabs = append(tabs, tab{l, title, s}); return nil }
	t.Cleanup(func() { openTab = real })

	out, err := run(t, cliMatin, "-n", "--min", "0", proj)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, out, "ouvrirait", "Alpha", "~ Déploie caddy", "déjà ouverte", "2 sujet(s) seraient rouverts")
	if strings.Contains(out, "Vieille") || strings.Contains(out, id3[:8]) || len(tabs) != 0 {
		t.Errorf("une session trop vieille, un homonyme ou un dry-run a ouvert quelque chose :\n%s", out)
	}

	out, err = run(t, cliMatin, "--term", "tmux", "--min", "0", "--args", "--model=opus", "--nommees", proj)
	if err != nil || len(tabs) != 1 {
		t.Fatalf("%v %d onglets\n%s", err, len(tabs), out)
	}
	want := tab{"tmux", "Alpha", tabSpec{Cwd: proj, Args: []string{"--model=opus", "--resume", id1}}}
	if got := tabs[0]; got.launcher != want.launcher || got.title != want.title || got.spec.Cwd != want.spec.Cwd || strings.Join(got.spec.Args, " ") != strings.Join(want.spec.Args, " ") {
		t.Errorf("onglet %+v, attendu %+v", got, want)
	}

	// Une seule session, même vieille, avec un message.
	tabs = nil
	old4 := "44444444-4444-4444-4444-444444444444"
	if _, err = run(t, cliMatin, "--term", "tmux", "--id", old4, "--prompt", "suite; avec 'quotes'", proj); err != nil || len(tabs) != 1 ||
		strings.Join(tabs[0].spec.Args, "|") != "--resume|"+old4+"|--|suite; avec 'quotes'" {
		t.Fatalf("%v %+v", err, tabs)
	}

	// Nouvelle session : le prompt passe tel quel, sans shell.
	tabs = nil
	out, err = run(t, cliMatin, "--term", "kitty", "--prompt", "fusionne", proj)
	if err != nil || len(tabs) != 1 || tabs[0].title != "claude" || strings.Join(tabs[0].spec.Args, "|") != "--|fusionne" {
		t.Fatalf("%v %+v\n%s", err, tabs, out)
	}
	contains(t, out, "1 onglet lancé : nouvelle session dans "+proj)
	if out, _ = run(t, cliMatin, "-n", "--prompt", "x", proj); !strings.Contains(out, "ouvrirait une nouvelle session") {
		t.Error(out)
	}

	// Erreurs.
	if _, err = run(t, cliMatin, "--term", "tmux", filepath.Join(proj, "absent")); err == nil {
		t.Error("projet absent accepté")
	}
	if _, err = run(t, cliMatin, "--bogus"); err == nil {
		t.Error("option inconnue acceptée")
	}
	if out, err = run(t, cliMatin, "--term", "list"); err != nil || !strings.Contains(out, "lanceurs disponibles") {
		t.Error(out, err)
	}
	t.Setenv("GLD_MATIN_EXCLURE", "Alpha")
	tabs = nil
	run(t, cliMatin, "--term", "tmux", "--min", "0", proj)
	for _, x := range tabs {
		if x.title == "Alpha" {
			t.Error("titre exclu rouvert")
		}
	}
}

func TestMatinResume(t *testing.T) {
	cfg, proj, dir := cliEnv(t)
	session(t, dir, id1, "Alpha", "a", time.Hour)
	session(t, dir, id2, "Bêta", "b", time.Hour)
	b, _ := json.Marshal([]TabSave{
		{Name: "", Auto: true, At: "2026-10-21T09:00:00.123Z", Sessions: []TabSession{{ID: id1, Project: proj}}},
		{Name: "soir", At: "2026-10-20T18:00:00Z", Sessions: []TabSession{{ID: id2, Project: proj}}},
	})
	os.WriteFile(filepath.Join(cfg, "gld-web-tabs.json"), b, 0o644)
	var ids []string
	real := openTab
	openTab = func(l, title string, s tabSpec) error { ids = append(ids, s.Args[len(s.Args)-1]); return nil }
	t.Cleanup(func() { openTab = real })

	out, err := run(t, cliMatin, "--term", "tmux", "--resume")
	if err != nil || strings.Join(ids, ",") != id1 {
		t.Fatalf("%v %v\n%s", err, ids, out)
	}
	contains(t, out, "sauvegarde : automatique du 2026-10-21 09:00, 1 session(s)")
	ids = nil
	if _, err = run(t, cliMatin, "--term", "tmux", "--resume", "soir"); err != nil || strings.Join(ids, ",") != id2 {
		t.Fatalf("%v %v", err, ids)
	}
	if _, err = run(t, cliMatin, "--term", "tmux", "--resume", "inconnue"); err == nil {
		t.Error("sauvegarde inconnue acceptée")
	}
}

func TestLauncherCmd(t *testing.T) {
	argv := []string{"/b/gld-web", "_run", "/tmp/x it's.json"}
	cases := map[string]string{
		"tmux":           `tmux new-window -n T '/b/gld-web' '_run' '/tmp/x it'\''s.json'`,
		"kitty":          "kitty @ launch --type=tab --tab-title T /b/gld-web _run /tmp/x it's.json",
		"wezterm":        "wezterm cli spawn -- /b/gld-web _run /tmp/x it's.json",
		"gnome-terminal": "gnome-terminal --tab --title=T -- /b/gld-web _run /tmp/x it's.json",
		"konsole":        "konsole --new-tab -p tabtitle=T -e /b/gld-web _run /tmp/x it's.json",
		"terminator":     `terminator --new-tab -T T -e '/b/gld-web' '_run' '/tmp/x it'\''s.json'`,
	}
	for name, want := range cases {
		c, err := launcherCmd(name, "T", argv)
		if err != nil || strings.Join(c.Args, " ") != want {
			t.Errorf("%s : %v %q", name, err, strings.Join(c.Args, " "))
		}
	}
	if c, _ := launcherCmd("tmux", "x #(touch /tmp/pwn) #{a}", argv); c.Args[3] != "x ##(touch /tmp/pwn) ##{a}" {
		t.Errorf("tmux : titre non neutralisé : %q", c.Args[3])
	}
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	c, _ := launcherCmd("wt", "a;b\"c", argv)
	if got := strings.Join(c.Args, " "); !strings.HasPrefix(got, "wt.exe -w 0 new-tab --title a b c wsl.exe -d Ubuntu -- /b/gld-web _run") {
		t.Errorf("wt : %q", got)
	}
	if _, err := launcherCmd("inconnu", "T", argv); err == nil {
		t.Error("lanceur inconnu accepté")
	}
	if got := claudeTabArgs([]string{"-x"}, "", "p"); strings.Join(got, "|") != "-x|--|p" {
		t.Error(got)
	}
}

func TestRunTab(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	if code := runTab(missing); code != 1 { // stdin vide : « Entrée pour fermer » rend aussitôt la main
		t.Errorf("fichier absent : code %d", code)
	}
	f := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(f, []byte(`{"cwd":"/nexiste/pas","args":[]}`), 0o600)
	if code := runTab(f); code != 1 || exists(f) {
		t.Errorf("dossier absent : code %d, fichier gardé=%v", code, exists(f))
	}
}

func TestHelpEtErreurs(t *testing.T) {
	for name, fn := range cliCommands {
		out, err := run(t, fn, "--help")
		if !errors.Is(err, errHelp) || !strings.Contains(out, "gld-"+name) {
			t.Errorf("%s --help : %v\n%s", name, err, out)
		}
		if _, err := run(t, fn, "--nimporte"); err == nil || !strings.Contains(err.Error(), "option inconnue") {
			t.Errorf("%s : %v", name, err)
		}
	}
	cliEnv(t)
	if _, err := resolveProject(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("projet absent")
	}
	if _, err := resolveProject(t.TempDir()); err == nil || !strings.Contains(err.Error(), "aucune session Claude") {
		t.Error("projet sans session :", err)
	}
}

func TestAliasDeProjet(t *testing.T) {
	cfg, proj, dir := cliEnv(t)
	canon := filepath.Join(cfg, "projects", "-canon")
	session(t, canon, id1, "Alpha", "x", time.Hour)
	put(t, filepath.Join(dir, ".session-aliases"), canon+"\n", time.Hour)
	p, err := resolveProject(proj)
	if err != nil || p.Transcripts != canon || p.Alias != canon {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestLanceurTerminatorTmux(t *testing.T) {
	c, err := launcherCmd("terminator-tmux", "Mon #(x) titre", []string{"/bin/gld", "_run", "/tmp/a b.json"})
	if err != nil {
		t.Fatal(err)
	}
	e := c.Args[len(c.Args)-1]
	want := `tmux new-session -n 'Mon ##(x) titre' ''\''/bin/gld'\'' '\''_run'\'' '\''/tmp/a b.json'\'''`
	if e != want {
		t.Errorf("\n%s\n%s", e, want)
	}
}
