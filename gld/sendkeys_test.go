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
	for _, c := range (*calls)[1:] { // le premier appel est la liste des panneaux
		seq = append(seq, c[1])
	}
	if strings.Join(seq, " ") != "list-panes load-buffer paste-buffer send-keys" && strings.Join(seq, " ") != "load-buffer paste-buffer send-keys" {
		t.Fatalf("séquence : %v", seq)
	}
	last := (*calls)[len(*calls)-3:]
	if last[0][0] != "ligne 1\nligne 2" || last[1][len(last[1])-1] != "%3" || last[1][2] != "-p" || last[2][len(last[2])-1] != "Enter" {
		t.Fatalf("appels : %v", last)
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
	if b, err := exec.Command("tmux", "new-session", "-d", "-s", "t", "-x", "200", "-y", "50", "sh -c 'cat > "+out+"'").CombinedOutput(); err != nil {
		t.Skip("tmux inutilisable ici : ", string(b))
	}
	defer exec.Command("tmux", "kill-server").Run()
	panes, err := tmuxPanes()
	if err != nil || len(panes) != 1 {
		t.Fatal(panes, err)
	}
	var panePid int
	for p := range panes {
		panePid = p
	}
	oldPid := sessionPIDs
	defer func() { sessionPIDs = oldPid }()
	sessionPIDs = func() map[string]int { return map[string]int{sid: panePid} }
	if err := sendToSession(sid, "bonjour\nsuite avec accents é"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "bonjour") || !strings.Contains(string(b), "suite avec accents é") {
		t.Fatalf("reçu : %q", b)
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
	if s, err := sessionScreen(sid); err != nil || s != "Do you want to proceed?\n❯ 1. Yes\n  2. No" {
		t.Fatalf("écran : %q %v", s, err)
	}
	if err := sendKey(sid, "2"); err != nil {
		t.Fatal(err)
	}
	last := (*calls)[len(*calls)-1]
	if last[1] != "send-keys" || last[len(last)-1] != "2" || last[len(last)-2] != "%3" {
		t.Fatalf("touche : %v", last)
	}
	for _, k := range []string{"rm -rf", "C-d", "a", "Enter; ls", ""} {
		if sendKey(sid, k) == nil {
			t.Errorf("touche acceptée : %q", k)
		}
	}
	if sendKey("../x", "Enter") == nil {
		t.Error("session invalide acceptée")
	}
}
