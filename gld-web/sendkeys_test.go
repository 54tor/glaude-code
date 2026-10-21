package main

import (
	"strings"
	"testing"
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
