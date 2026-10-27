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
	mu       sync.Mutex
	panes    map[string]string
	idle     []string
	screens  map[string]string
	capErr   map[string]bool
	script   map[string][]string // captures successives d'un pane avant de retomber sur screens
	caps     int
	pastes   [][2]string
	keys     [][2]string
	closed   []string
	panesErr error
	unknown  bool
	live     map[string]bool
	leadIDs  []string
	order    []string
	busy     int32
	overlap  bool
}

func (f *fakeIO) Panes() (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "panes")
	return f.panes, f.panesErr
}
func (f *fakeIO) LeadState(id string) (bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "lead")
	f.leadIDs = append(f.leadIDs, id)
	return f.live[id], !f.unknown
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
	if len(v.Messages) != 2 {
		t.Fatalf("messages : %+v", v.Messages)
	}
	os.WriteFile(filepath.Join(root, "teams", trName, "inboxes", "ada.json"), []byte("[]"), 0o644)
	v, _ = teamView(root, trName)
	v, _ = teamView(root, trName)
	if len(v.Messages) != 2 {
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

func TestCloseFinishedTeamCleansUp(t *testing.T) {
	root, f := setup(t)
	setActive(t, root, "hugo", false)
	teamSend(root, trName, "max", "hugo", "direct")
	teamView(root, trName)
	f.panes = map[string]string{}
	if err := teamClose(root, trName); err != nil {
		t.Fatal(err)
	}
	if len(f.closed) != 0 {
		t.Fatalf("Close appelé : %v", f.closed)
	}
	if d, _ := journalRead(root, trName); len(d) != 0 {
		t.Fatalf("journal : %v", d)
	}
	if len(teamQueue[trName]) != 0 || len(teamLog[trName]) != 0 {
		t.Fatal("file ou journal mémoire non vidés")
	}
}

func TestCloseLiveTeamStillClosesFirst(t *testing.T) {
	root, f := setup(t)
	teamSend(root, trName, "max", "ada", "x")
	if err := teamClose(root, trName); err != nil || len(f.closed) != 1 {
		t.Fatalf("%v %v", err, f.closed)
	}
	if err := teamClose(root, "../x"); !isMsg(err, "srv.teams_run.nomInvalide") {
		t.Fatal(err)
	}
}

func TestLogKeepsIdleWithResult(t *testing.T) {
	root, _ := setup(t)
	teamView(root, trName)
	var got *InboxMsg
	for _, m := range teamLog[trName] {
		if m.Kind == "idle" {
			got = &m
		}
	}
	if got == nil || got.From != "ada" || !strings.Contains(got.Text, "/tmp/gld-spike-perm.txt") {
		t.Fatalf("%+v", teamLog[trName])
	}
}

func TestLogDropsIdleWithoutResult(t *testing.T) {
	tf := TeamFiles{Name: "t-vide", Inbox: []InboxMsg{{ID: "a", Kind: "idle"}, {ID: "b", Kind: "idle", Text: "fait"}}}
	logInbox(tf)
	l := teamLog["t-vide"]
	if len(l) != 1 || l[0].ID != "b" {
		t.Fatalf("%+v", l)
	}
}

func TestTeamSendReportsTmuxError(t *testing.T) {
	root, f := setup(t)
	f.panesErr = errors.New("tmux gelé")
	f.panes = nil
	if _, err := teamSend(root, trName, "max", "ada", "x"); !isMsg(err, "srv.teams_run.tmuxInutilisable") {
		t.Fatalf("send: %v", err)
	}
	if err := teamKey(root, trName, "ada", "1", ""); !isMsg(err, "srv.teams_run.tmuxInutilisable") {
		t.Fatalf("key: %v", err)
	}
	if _, err := teamScreen(root, trName, "ada"); !isMsg(err, "srv.teams_run.tmuxInutilisable") {
		t.Fatalf("screen: %v", err)
	}
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.tmuxInutilisable") {
		t.Fatalf("close: %v", err)
	}
	if v, err := teamView(root, trName); err != nil || v.Live {
		t.Fatalf("view: %v %v", v.Live, err)
	}
}

func TestTeamViewUsesRoster(t *testing.T) {
	root, _ := setup(t)
	if err := rosterWrite(root, trName, map[string]string{"ada": "backend", "hugo": "testeur"}); err != nil {
		t.Fatal(err)
	}
	v, err := teamView(root, trName)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]AgentView{}
	for _, a := range v.Agents {
		by[a.Name] = a
	}
	if a := by["ada"]; a.Display != "Ada" || a.Role != "Développement backend" || a.AgentID != "backend" {
		t.Errorf("ada : %+v", a)
	}
	if a := by["hugo"]; a.Display != "Hugo" || a.Role != "Testeur" || a.AgentID != "testeur" {
		t.Errorf("hugo : %+v", a)
	}
	if a := by["zoe"]; a.Display != "Zoe" || a.Role != "" {
		t.Errorf("zoe : %+v", a)
	}
	if a := by["team-lead"]; a.Display != "Chef de projet" || a.Role != "" || !a.Lead {
		t.Errorf("chef : %+v", a)
	}
}

func TestTeamViewWithoutRoster(t *testing.T) {
	root, _ := setup(t)
	v, err := teamView(root, trName)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range v.Agents {
		if !a.Lead && (a.Role != "" || a.Display != upFirst(a.Name)) {
			t.Errorf("%+v", a)
		}
	}
}

func TestTeamViewCorruptRoster(t *testing.T) {
	root, _ := setup(t)
	os.MkdirAll(filepath.Join(root, journalDir), 0o700)
	os.WriteFile(filepath.Join(root, journalDir, trName+".roster.json"), []byte("{pas du json"), 0o600)
	v, err := teamView(root, trName)
	if err != nil || len(v.Agents) == 0 {
		t.Fatal(v, err)
	}
}

func TestCloseDropsRoster(t *testing.T) {
	for _, live := range []bool{true, false} {
		root, f := setup(t)
		rosterWrite(root, trName, map[string]string{"ada": "backend"})
		if !live {
			f.panes = map[string]string{}
		}
		if err := teamClose(root, trName); err != nil {
			t.Fatal(live, err)
		}
		if m, _ := rosterRead(root, trName); len(m) != 0 {
			t.Errorf("live=%v roster : %v", live, m)
		}
	}
}

func TestUpFirstEmpty(t *testing.T) {
	if upFirst("") != "" || upFirst("ada") != "Ada" {
		t.Fatal("upFirst")
	}
}

func seedClose(t *testing.T) (string, *fakeIO) {
	t.Helper()
	root, f := setup(t)
	setActive(t, root, "hugo", false)
	teamSend(root, trName, "max", "hugo", "direct")
	teamView(root, trName)
	teamRunMu.Lock()
	teamQueue[trName] = []queuedMsg{{}}
	teamRunMu.Unlock()
	if len(teamLog[trName]) == 0 {
		t.Fatal("log non amorcé")
	}
	f.panes = map[string]string{}
	return root, f
}

func stateIntact(t *testing.T, root string, f *fakeIO) {
	t.Helper()
	if d, _ := journalRead(root, trName); len(d) == 0 {
		t.Fatal("journal supprimé")
	}
	if len(teamQueue[trName]) == 0 || len(teamLog[trName]) == 0 {
		t.Fatal("log ou file vidés")
	}
	if len(f.closed) != 0 {
		t.Fatalf("Close appelé : %v", f.closed)
	}
}

func stateCleaned(t *testing.T, root string, f *fakeIO) {
	t.Helper()
	if d, _ := journalRead(root, trName); len(d) != 0 || len(f.closed) != 0 {
		t.Fatal("nettoyage attendu")
	}
	if len(teamQueue[trName]) != 0 || len(teamLog[trName]) != 0 {
		t.Fatal("file ou log non vidés")
	}
}

var noServer = terr("srv.sendkeys.aucuneSessionNA")

func TestCloseAbortsWhenStateUnknown(t *testing.T) {
	root, f := seedClose(t)
	f.unknown = true
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.etatInconnu") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, f)
}

func TestCloseAbortsWhenLeadAliveButNoPane(t *testing.T) {
	root, f := seedClose(t)
	f.live = map[string]bool{trLead: true}
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.chefOuvertSansTerminal") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, f)
}

func TestCloseAbortsWhenLeadAliveAndTmuxUnreachable(t *testing.T) {
	root, f := seedClose(t)
	f.live, f.panes, f.panesErr = map[string]bool{trLead: true}, nil, noServer
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.chefOuvertSansTerminal") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, f)
}

func TestCloseCleansWhenLeadNotAlive(t *testing.T) {
	for name, set := range map[string]func(*fakeIO){
		"vide":     func(f *fakeIO) {},
		"sentinel": func(f *fakeIO) { f.panes, f.panesErr = nil, noServer },
	} {
		t.Run(name, func(t *testing.T) {
			root, f := seedClose(t)
			set(f)
			if err := teamClose(root, trName); err != nil {
				t.Fatal(err)
			}
			stateCleaned(t, root, f)
		})
	}
}

func TestCloseStillAbortsOnRealTmuxError(t *testing.T) {
	root, f := seedClose(t)
	f.panes, f.panesErr = nil, errors.New("tmux gelé")
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.tmuxInutilisable") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, f)
}

func TestCloseReadsLeadStateBeforePanes(t *testing.T) {
	root, f := seedClose(t)
	f.order = nil
	teamClose(root, trName)
	if len(f.order) < 2 || f.order[0] != "lead" || f.order[1] != "panes" {
		t.Fatalf("ordre : %v", f.order)
	}
}

func TestCloseAsksLeadStateForTheLeadSessionID(t *testing.T) {
	root, f := seedClose(t)
	tf, err := readTeam(root, trName)
	if err != nil || tf.LeadSessionID == "" {
		t.Fatal(err, tf.LeadSessionID)
	}
	teamClose(root, trName)
	if len(f.leadIDs) != 1 || f.leadIDs[0] != tf.LeadSessionID {
		t.Fatalf("ids : %v, attendu %q", f.leadIDs, tf.LeadSessionID)
	}
}
