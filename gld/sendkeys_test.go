package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sid = "11111111-2222-3333-4444-555555555555"

func fakeTmux(t *testing.T, panes string) *[][]string {
	t.Helper()
	var calls [][]string
	oldRun, oldPid, oldPar := tmuxRun, sessionPIDs, parentOf
	t.Cleanup(func() { tmuxRun, sessionPIDs, parentOf = oldRun, oldPid, oldPar })
	tmuxRun = func(stdin string, args ...string) (string, error) {
		calls = append(calls, append([]string{stdin}, args...))
		if args[0] == "list-panes" {
			return panes, nil
		}
		return "", nil
	}
	sessionPIDs = func() map[string]int { return map[string]int{sid: 300, "autre": 900} }
	parentOf = func(pid int) int { return map[int]int{300: 200, 200: 100, 900: 1}[pid] } // claude -> shell -> processus du panneau
	return &calls
}

func TestEnvoiTmux(t *testing.T) {
	calls := fakeTmux(t, "%3 100\n%4 555\n")
	ids, err := sendable()
	if err != nil || ids[sid] != "%3" || len(ids) != 1 {
		t.Fatalf("sessions joignables : %v %v", ids, err)
	}
	if err := sendToSession(sid, "ligne 1\nligne 2"); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, values := range (*calls)[1:] { // le premier appel est la liste des panneaux
		seq = append(seq, values[1])
	}
	if got := strings.Join(seq, " "); got != "list-panes capture-pane load-buffer paste-buffer send-keys" && got != "capture-pane load-buffer paste-buffer send-keys" {
		t.Fatalf("séquence : %v", seq)
	}
	last := (*calls)[len(*calls)-3:]
	if last[0][0] != "ligne 1\nligne 2" || last[1][len(last[1])-1] != "%3" || last[1][2] != "-p" || last[2][len(last[2])-1] != "Enter" {
		t.Fatalf("appels : %v", last)
	}
	if err := sendToSession(sid, "encore"); err != nil {
		t.Fatal(err)
	}
	if body := (*calls)[len(*calls)-3][3]; body == last[0][3] { // deux envois simultanés ne doivent pas partager un tampon
		t.Fatalf("tampon réutilisé : %s", body)
	}
}

func TestEnvoiMentionFinale(t *testing.T) {
	calls := fakeTmux(t, "%3 100\n")
	for in, want := range map[string]string{"voir\n@/x/a.png": "voir\n@/x/a.png ", "@/x/a.png": "@/x/a.png ", "écrit à @moi ok": "écrit à @moi ok"} {
		if err := sendToSession(sid, in); err != nil {
			t.Fatal(err)
		}
		if got := (*calls)[len(*calls)-3][0]; got != want {
			t.Fatalf("%q envoyé comme %q, attendu %q", in, got, want)
		}
	}
}

func TestEnvoiRefuse(t *testing.T) {
	fakeTmux(t, "%4 555\n") // la session n'est dans aucun panneau
	if err := sendToSession(sid, "salut"); err == nil || !strings.Contains(err.Error(), "pas dans tmux") {
		t.Fatalf("hors tmux : %v", err)
	}
	fakeTmux(t, "%3 100\n")
	for _, txt := range []string{"", "  \n", "a\x1b[201~rm -rf", "a\x00b", strings.Repeat("x", sendMax+1), "a\x7f"} {
		if err := sendToSession(sid, txt); err == nil {
			t.Errorf("texte accepté : %q", txt)
		}
	}
	if err := sendToSession("../x", "salut"); err == nil {
		t.Error("identifiant invalide accepté")
	}
	if err := sendToSession("99999999-2222-3333-4444-555555555555", "salut"); err == nil {
		t.Error("session inconnue acceptée")
	}
}

// Essai réel avec tmux (ignoré s'il est absent) : un panneau qui écrit son entrée dans un fichier ; la session « claude » est
// simulée par un processus fils du panneau.
func TestEnvoiTmuxReel(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	dir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	out := filepath.Join(dir, "recu.txt")
	if data, err := exec.Command("tmux", "new-session", "-d", "-s", "t", "-x", "200", "-y", "50", "sh -c 'cat > "+out+"'").CombinedOutput(); err != nil {
		t.Skip("tmux inutilisable ici : ", string(data))
	}
	defer exec.Command("tmux", "kill-server").Run()
	panes, err := tmuxPanes()
	if err != nil || len(panes) != 1 {
		t.Fatal(panes, err)
	}
	var panePid int
	for port := range panes {
		panePid = port
	}
	oldPid := sessionPIDs
	defer func() { sessionPIDs = oldPid }()
	sessionPIDs = func() map[string]int { return map[string]int{sid: panePid} }
	if err := sendToSession(sid, "bonjour\nsuite avec accents é"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	localData, _ := os.ReadFile(out)
	if !strings.Contains(string(localData), "bonjour") || !strings.Contains(string(localData), "suite avec accents é") {
		t.Fatalf("reçu : %q", localData)
	}
}

func TestFermerSession(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer cmd.Process.Kill()
	old := sessionPIDs
	defer func() { sessionPIDs = old }()
	sessionPIDs = func() map[string]int { return map[string]int{sid: cmd.Process.Pid} }
	if err := closeSession(sid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("le processus a fini normalement : il aurait dû être interrompu")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("processus encore vivant")
	}
	if closeSession("99999999-2222-3333-4444-555555555555") == nil || closeSession("../x") == nil {
		t.Fatal("session inconnue ou invalide acceptée")
	}
}

func TestEcranEtTouches(t *testing.T) {
	calls := fakeTmux(t, "%3 100\n")
	oldRun := tmuxRun
	tmuxRun = func(stdin string, args ...string) (string, error) {
		if args[0] == "capture-pane" {
			return "Do you want to proceed?   \n❯ 1. Yes\n  2. No\n\n\n", nil
		}
		return oldRun(stdin, args...)
	}
	if text, err := sessionScreen(sid); err != nil || text != "Do you want to proceed?\n❯ 1. Yes\n  2. No" {
		t.Fatalf("écran : %q %v", text, err)
	}
	if err := sendKey(sid, "2"); err != nil {
		t.Fatal(err)
	}
	last := (*calls)[len(*calls)-1]
	if last[1] != "send-keys" || last[len(last)-1] != "2" || last[len(last)-2] != "%3" {
		t.Fatalf("touche : %v", last)
	}
	if err := sendKey(sid, "CtrlB"); err != nil {
		t.Fatal(err)
	}
	last = (*calls)[len(*calls)-1]
	if last[len(last)-1] != "C-b" {
		t.Fatalf("touche : %v", last)
	}
	for _, key := range []string{"rm -rf", "C-d", "a", "Enter; ls", ""} {
		if sendKey(sid, key) == nil {
			t.Errorf("touche acceptée : %q", key)
		}
	}
	if sendKey("../x", "Enter") == nil {
		t.Error("session invalide acceptée")
	}
}

func TestCloseBackgroundSession(t *testing.T) {
	const sid = "28535867-7765-4b88-867d-ee7dade64898"
	oldPid, oldBg, oldStop := sessionPIDs, backgroundSessions, stopBackground
	t.Cleanup(func() { sessionPIDs, backgroundSessions, stopBackground = oldPid, oldBg, oldStop })
	sessionPIDs = func() map[string]int { return map[string]int{} }
	backgroundSessions = func() map[string]string { return map[string]string{sid: "28535867"} }
	var got string
	stopBackground = func(short string) error { got = short; return nil }
	if err := closeSession(sid); err != nil || got != "28535867" {
		t.Fatalf("err=%v short=%q", err, got)
	}
	if err := closeSession("11111111-1111-1111-1111-111111111111"); err == nil {
		t.Fatal("session inconnue acceptée")
	}
}

func TestEnvoiCommandeVideLaLigne(t *testing.T) {
	calls := fakeTmux(t, "%3 100\n")
	if err := sendToSession(sid, "/model opus"); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, values := range (*calls)[1:] {
		seq = append(seq, strings.Join(values[1:2], "")+":"+values[len(values)-1])
	}
	if got := strings.Join(seq, " "); !strings.Contains(got, "send-keys:C-u load-buffer") {
		t.Fatalf("la ligne n'est pas vidée avant la commande : %s", got)
	}
}

func TestBtwOpen(t *testing.T) {
	if !btwOpen("  /btw q\n\n  réponse\n\n  ↑/↓ to scroll · c to copy · f to fork · Esc to close\n") {
		t.Fatal("surcouche non détectée")
	}
	if btwOpen("❯ \n  ⏵⏵ auto mode on\n") {
		t.Fatal("faux positif")
	}
}

// installerFauxExecutableQuiAfficheEnv place en tête du PATH un faux exécutable `nom` qui imprime son environnement :
// on vérifie ainsi ce que gld transmet réellement au processus lancé, sans dépendre du vrai binaire.
func installerFauxExecutableQuiAfficheEnv(t *testing.T, nom string) {
	t.Helper()
	if _, shErr := exec.LookPath("sh"); shErr != nil {
		t.Skip("sh introuvable")
	}
	dossierFaux := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(dossierFaux, nom), []byte("#!/bin/sh\nenv\n"), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", dossierFaux+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// La clé maîtresse fournie par GLD_AUTH_KEY ne doit jamais atteindre tmux : un serveur tmux démarré par gld la copierait
// dans son environnement global, lisible depuis chaque panneau.
func TestTmuxRunNeTransmetPasLaCleMaitresse(t *testing.T) {
	installerFauxExecutableQuiAfficheEnv(t, "tmux")
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
	environnementObtenu, runErr := tmuxRun("")
	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.Contains(environnementObtenu, "GLD_AUTH_KEY=") {
		t.Fatal("GLD_AUTH_KEY transmise à tmux")
	}
	if !strings.Contains(environnementObtenu, "PATH=") {
		t.Fatal("environnement vide : le faux tmux n'a pas tourné")
	}
}

// Même exigence pour git, lancé par les routes d'état git (mem_read).
func TestGitOut1NeTransmetPasLaCleMaitresse(t *testing.T) {
	installerFauxExecutableQuiAfficheEnv(t, "git")
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
	environnementObtenu, runErr := gitOut1(t.TempDir(), "status")
	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.Contains(environnementObtenu, "GLD_AUTH_KEY=") || !strings.Contains(environnementObtenu, "PATH=") {
		t.Fatalf("environnement transmis à git incorrect")
	}
}

// L'émulateur lancé par openTab (ici un faux « terminator ») ne reçoit pas la clé maîtresse : il n'est pas marqué
// non-dumpable et démarrerait, avec un lanceur « +tmux », un serveur tmux qui la garderait.
func TestOpenTabNeTransmetPasLaCleMaitresse(t *testing.T) {
	if _, shErr := exec.LookPath("sh"); shErr != nil {
		t.Skip("sh introuvable")
	}
	dossierFaux := t.TempDir()
	fichierEnvironnement := filepath.Join(dossierFaux, "env.txt")
	script := "#!/bin/sh\nenv > \"" + fichierEnvironnement + ".tmp\" && mv \"" + fichierEnvironnement + ".tmp\" \"" + fichierEnvironnement + "\"\n"
	if writeErr := os.WriteFile(filepath.Join(dossierFaux, "terminator"), []byte(script), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", dossierFaux+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
	t.Setenv("TMPDIR", dossierFaux) // le fichier de spécification n'est relu par aucun « gld _run » ici : qu'il reste dans le dossier du test
	if openErr := openTab("terminator", "titre", tabSpec{Cwd: dossierFaux, Args: []string{"claude"}}); openErr != nil {
		t.Fatal(openErr)
	}
	echeance := time.Now().Add(5 * time.Second)
	var environnementObtenu []byte
	for time.Now().Before(echeance) {
		if contenu, readErr := os.ReadFile(fichierEnvironnement); readErr == nil {
			environnementObtenu = contenu
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(string(environnementObtenu), "PATH=") {
		t.Fatal("le faux émulateur n'a pas tourné")
	}
	if strings.Contains(string(environnementObtenu), "GLD_AUTH_KEY=") {
		t.Fatal("GLD_AUTH_KEY transmise à l'émulateur")
	}
}
