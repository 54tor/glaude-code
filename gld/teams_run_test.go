package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	trName  = "session-9d087000"
	trLead  = "9d087000-13eb-4519-9823-771e3a943e4c"
	trLPane = "%66"
)

type fakeIO struct {
	mu      sync.Mutex
	panes   map[string]string
	idle    []string
	screens map[string]string
	capErr  map[string]bool
	script  map[string][]string // captures successives d'un pane avant de retomber sur screens
	caps    int
	pastes  [][2]string
	keys    [][2]string
	closed  []string
	busy    int32
	overlap bool
}

func (f *fakeIO) Panes() (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.panes, nil
}
func (f *fakeIO) Idle() []string { f.mu.Lock(); defer f.mu.Unlock(); return f.idle }
func (f *fakeIO) Capture(p string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.caps++
	if q := f.script[p]; len(q) > 0 {
		f.script[p] = q[1:]
		return q[0], nil
	}
	if f.capErr[p] {
		return "", errors.New("capture")
	}
	return f.screens[p], nil
}
func (f *fakeIO) Paste(p, t string) error {
	if atomic.AddInt32(&f.busy, 1) > 1 {
		f.overlap = true
	}
	time.Sleep(5 * time.Millisecond)
	atomic.AddInt32(&f.busy, -1)
	f.mu.Lock()
	f.pastes = append(f.pastes, [2]string{p, t})
	f.mu.Unlock()
	return nil
}
func (f *fakeIO) Key(p, k string) error {
	f.mu.Lock()
	f.keys = append(f.keys, [2]string{p, k})
	f.mu.Unlock()
	return nil
}
func (f *fakeIO) Close(id string) error {
	f.mu.Lock()
	f.closed = append(f.closed, id)
	f.mu.Unlock()
	return nil
}

// setup copie les fixtures réelles du spike dans un cfgRoot jetable.
func setup(t *testing.T) (string, *fakeIO) {
	t.Helper()
	root := t.TempDir()
	d := filepath.Join(root, "teams", trName)
	os.MkdirAll(filepath.Join(d, "inboxes"), 0o755)
	for src, dst := range map[string]string{"config.json": "config.json", "inbox-ada.json": "inboxes/ada.json", "inbox-team-lead.json": "inboxes/team-lead.json"} {
		b, err := os.ReadFile(filepath.Join("testdata", "teams", src))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, dst), b, 0o644)
	}
	f := &fakeIO{panes: map[string]string{trLead: trLPane}, idle: []string{trLead}, screens: map[string]string{}, capErr: map[string]bool{}}
	old := teamPanes
	teamPanes = f
	teamRunMu.Lock()
	teamLog, teamQueue = map[string][]InboxMsg{}, map[string][]queuedMsg{}
	teamRunMu.Unlock()
	t.Cleanup(func() { teamPanes = old })
	return root, f
}

func setActive(t *testing.T, root, mate string, active bool) {
	t.Helper()
	p := filepath.Join(root, "teams", trName, "config.json")
	b, _ := os.ReadFile(p)
	s := string(b)
	i := strings.Index(s, `"name": "`+mate+`"`)
	j := i + strings.Index(s[i:], `"isActive": `)
	k := j + len(`"isActive": `)
	e := k + strings.IndexAny(s[k:], ",\n}")
	v := "false"
	if active {
		v = "true"
	}
	os.WriteFile(p, []byte(s[:k]+v+s[e:]), 0o644)
}

func TestSendToIdleMate(t *testing.T) {
	root, f := setup(t)
	q, err := teamSend(root, trName, "max", "ada", "salut")
	if err != nil || q {
		t.Fatal(q, err)
	}
	if len(f.pastes) != 1 || f.pastes[0] != [2]string{"%67", "salut"} {
		t.Fatalf("collages : %v", f.pastes)
	}
	d, _ := journalRead(root, trName)
	if len(d) != 1 || d[0].Author != "max" || d[0].To != "ada" {
		t.Fatalf("journal : %v", d)
	}
}

func TestSendToBusyMateQueues(t *testing.T) {
	root, f := setup(t)
	setActive(t, root, "ada", true)
	q, err := teamSend(root, trName, "max", "ada", "salut")
	if err != nil || !q || len(f.pastes) != 0 {
		t.Fatal(q, err, f.pastes)
	}
	v, _ := teamView(root, trName)
	if v.Queued != 1 {
		t.Fatalf("Queued = %d", v.Queued)
	}
}

func TestQueueFlushesWhenIdle(t *testing.T) {
	root, f := setup(t)
	setActive(t, root, "ada", true)
	teamSend(root, trName, "max", "ada", "un")
	teamSend(root, trName, "max", "ada", "deux")
	setActive(t, root, "ada", false)
	v, _ := teamView(root, trName)
	if len(f.pastes) != 1 || f.pastes[0][1] != "un" || v.Queued != 1 {
		t.Fatalf("collages %v queued %d", f.pastes, v.Queued)
	}
	if d, _ := journalRead(root, trName); len(d) != 1 || d[0].Text != "un" {
		t.Fatalf("journal : %v", d)
	}
}

func TestSendRejectsControlChars(t *testing.T) {
	root, f := setup(t)
	if _, err := teamSend(root, trName, "max", "ada", "a\x1bb"); err == nil || len(f.pastes) != 0 {
		t.Fatal("caractère de contrôle accepté")
	}
}

func TestSendNeverAnswersPermissionPrompt(t *testing.T) {
	root, f := setup(t)
	b, _ := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	f.screens[trLPane] = string(b)
	q, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !q || len(f.pastes) != 0 {
		t.Fatal(q, err, f.pastes)
	}
}

func TestTeamKeyOnlyAllowedKeys(t *testing.T) {
	root, f := setup(t)
	f.screens[trLPane] = permFixture(t)
	if err := teamKey(root, trName, "team-lead", "rm -rf", ""); err == nil {
		t.Fatal("touche refusée attendue")
	}
	if err := teamKey(root, trName, "team-lead", "Enter", ""); err != nil || len(f.keys) != 1 {
		t.Fatal(err, f.keys)
	}
}

func TestTeamKeyTargetsLeadPane(t *testing.T) {
	root, f := setup(t)
	f.screens[trLPane] = permFixture(t)
	if err := teamKey(root, trName, "team-lead", "1", ""); err != nil || f.keys[0] != [2]string{trLPane, "1"} {
		t.Fatal(err, f.keys)
	}
}

func TestCloseDropsJournal(t *testing.T) {
	root, f := setup(t)
	setActive(t, root, "ada", true)
	teamSend(root, trName, "max", "ada", "en file")
	setActive(t, root, "hugo", false)
	teamSend(root, trName, "max", "hugo", "direct")
	teamView(root, trName)
	if err := teamClose(root, trName); err != nil {
		t.Fatal(err)
	}
	if len(f.closed) != 1 || f.closed[0] != trLead {
		t.Fatalf("closed : %v", f.closed)
	}
	if d, _ := journalRead(root, trName); len(d) != 0 {
		t.Fatalf("journal : %v", d)
	}
	if len(teamQueue[trName]) != 0 || len(teamLog[trName]) != 0 {
		t.Fatal("file ou journal mémoire non vidés")
	}
}

func TestTeamViewUnknownFormat(t *testing.T) {
	root, _ := setup(t)
	os.WriteFile(filepath.Join(root, "teams", trName, "config.json"), []byte("{pas du json"), 0o644)
	v, err := teamView(root, trName)
	if err != nil || len(v.Unknown) == 0 {
		t.Fatal(v.Unknown, err)
	}
}

func TestTeamViewPermissionDetected(t *testing.T) {
	root, f := setup(t)
	b, _ := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	f.screens[trLPane] = string(b)
	// yan n'est pas dans la config du spike : on l'y ajoute
	p := filepath.Join(root, "teams", trName, "config.json")
	c, _ := os.ReadFile(p)
	c = []byte(strings.Replace(string(c), `"members": [`, `"members": [{"agentId":"yan@x","name":"yan","tmuxPaneId":"%70","backendType":"tmux"},`, 1))
	os.WriteFile(p, c, 0o644)
	v, err := teamView(root, trName)
	if err != nil || v.Permission == "" || !strings.Contains(v.Permission, "from the yan agent") {
		t.Fatal(v.Permission, err)
	}
	for _, a := range v.Agents {
		if (a.Name == "yan" || a.Lead) && a.State != "waiting" {
			t.Fatalf("%s : %s", a.Name, a.State)
		}
	}
}

func TestTeamViewMessagesAccumulate(t *testing.T) {
	root, _ := setup(t)
	v, _ := teamView(root, trName)
	if len(v.Messages) != 1 || v.Messages[0].Kind == "idle" {
		t.Fatalf("messages : %+v", v.Messages)
	}
	os.WriteFile(filepath.Join(root, "teams", trName, "inboxes", "ada.json"), []byte("[]"), 0o644)
	v, _ = teamView(root, trName)
	v, _ = teamView(root, trName)
	if len(v.Messages) != 1 {
		t.Fatalf("après vidage : %+v", v.Messages)
	}
}

func TestTeamListSkipsStale(t *testing.T) {
	root, f := setup(t)
	if l := teamList(root); len(l) != 1 || l[0].Name != trName || l[0].Members < 3 {
		t.Fatalf("liste : %+v", l)
	}
	f.panes = map[string]string{}
	if l := teamList(root); len(l) != 0 {
		t.Fatalf("équipe périmée listée : %+v", l)
	}
}

func TestTeamBadName(t *testing.T) {
	root, f := setup(t)
	bad := "../x"
	if _, err := teamView(root, bad); err == nil {
		t.Error("teamView")
	}
	if _, err := teamSend(root, bad, "a", "b", "c"); err == nil {
		t.Error("teamSend")
	}
	if teamKey(root, bad, "a", "1", "") == nil {
		t.Error("teamKey")
	}
	if _, err := teamScreen(root, bad, "a"); err == nil {
		t.Error("teamScreen")
	}
	if teamClose(root, bad) == nil || len(f.closed) != 0 || len(f.pastes) != 0 || len(f.keys) != 0 {
		t.Error("teamClose")
	}
}

func TestSendQueueFull(t *testing.T) {
	root, _ := setup(t)
	setActive(t, root, "ada", true)
	for i := 0; i < teamQueueMax; i++ {
		if q, err := teamSend(root, trName, "max", "ada", "m"); err != nil || !q {
			t.Fatal(i, q, err)
		}
	}
	_, err := teamSend(root, trName, "max", "ada", "m")
	if err == nil || !isMsg(err, "srv.teams_run.fileOuverte") {
		t.Fatalf("file pleine : %v", err)
	}
}

func TestConcurrentSendsSerialised(t *testing.T) {
	root, f := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); teamSend(root, trName, "max", "hugo", "x") }()
	}
	wg.Wait()
	if f.overlap || len(f.pastes) != 8 {
		t.Fatalf("chevauchement %v, collages %d", f.overlap, len(f.pastes))
	}
}

const leadOwnPrompt = ` Bash command
   rm -rf build
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again
   3. No, and tell Claude what to do differently (esc)
`

func TestSendNeverAnswersLeadOwnPrompt(t *testing.T) {
	root, f := setup(t)
	f.screens[trLPane] = leadOwnPrompt
	q, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !q || len(f.pastes) != 0 {
		t.Fatal(q, err, f.pastes)
	}
	v, _ := teamView(root, trName)
	if v.Permission == "" || len(f.pastes) != 0 {
		t.Fatalf("permission %q, collages %v", v.Permission, f.pastes)
	}
}

func TestSendNeverAnswersLongTeammatePrompt(t *testing.T) {
	root, f := setup(t)
	b, _ := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	parts := strings.SplitN(string(b), "╌", 2)
	f.screens[trLPane] = parts[0] + strings.Repeat("   | ligne de diff\n", 40) + "╌" + parts[1]
	q, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !q || len(f.pastes) != 0 {
		t.Fatal(q, err, f.pastes)
	}
}

func TestPromptAppearsBetweenCheckAndPaste(t *testing.T) {
	root, f := setup(t)
	f.script = map[string][]string{trLPane: {"", leadOwnPrompt}}
	q, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !q || len(f.pastes) != 0 {
		t.Fatal(q, err, f.pastes)
	}
	if n := len(teamQueue[trName]); n != 1 {
		t.Fatalf("file = %d", n)
	}
}

func TestTeamKeyScreenNeedLiveTeam(t *testing.T) {
	root, f := setup(t)
	f.panes = map[string]string{}
	if teamKey(root, trName, "ada", "1", "") == nil || len(f.keys) != 0 {
		t.Error("teamKey sur une team périmée")
	}
	caps := f.caps
	if _, err := teamScreen(root, trName, "ada"); err == nil || f.caps != caps {
		t.Error("teamScreen sur une team périmée")
	}
}

func permFixture(t *testing.T) string {
	b, err := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTeamKeyRefusesWhenNoDialog(t *testing.T) {
	root, f := setup(t)
	if err := teamKey(root, trName, "team-lead", "1", ""); err == nil || len(f.keys) != 0 {
		t.Fatal(err, f.keys)
	}
}

func TestTeamKeyRefusesWhenDialogChanged(t *testing.T) {
	root, f := setup(t)
	f.screens[trLPane] = permFixture(t)
	if err := teamKey(root, trName, "team-lead", "1", "autre dialogue"); err == nil || len(f.keys) != 0 {
		t.Fatal(err, f.keys)
	}
}

func TestTeamKeyOKWhenExpectMatches(t *testing.T) {
	root, f := setup(t)
	f.screens[trLPane] = permFixture(t)
	_, txt := permText(f.screens[trLPane])
	if err := teamKey(root, trName, "team-lead", "1", "  "+txt+"\n"); err != nil || len(f.keys) != 1 {
		t.Fatal(err, f.keys)
	}
}
