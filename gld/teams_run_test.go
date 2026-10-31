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

func (localFakeIO *fakeIO) Panes() (map[string]string, error) {
	localFakeIO.mu.Lock()
	defer localFakeIO.mu.Unlock()
	localFakeIO.order = append(localFakeIO.order, "panes")
	return localFakeIO.panes, localFakeIO.panesErr
}
func (localFakeIO *fakeIO) LeadState(id string) (bool, bool) {
	localFakeIO.mu.Lock()
	defer localFakeIO.mu.Unlock()
	localFakeIO.order = append(localFakeIO.order, "lead")
	localFakeIO.leadIDs = append(localFakeIO.leadIDs, id)
	return localFakeIO.live[id], !localFakeIO.unknown
}
func (localFakeIO *fakeIO) Idle() []string {
	localFakeIO.mu.Lock()
	defer localFakeIO.mu.Unlock()
	return localFakeIO.idle
}
func (localFakeIO *fakeIO) Capture(path string) (string, error) {
	localFakeIO.mu.Lock()
	defer localFakeIO.mu.Unlock()
	localFakeIO.caps++
	if values := localFakeIO.script[path]; len(values) > 0 {
		localFakeIO.script[path] = values[1:]
		return values[0], nil
	}
	if localFakeIO.capErr[path] {
		return "", errors.New("capture")
	}
	return localFakeIO.screens[path], nil
}
func (localFakeIO *fakeIO) Paste(path, title string) error {
	if atomic.AddInt32(&localFakeIO.busy, 1) > 1 {
		localFakeIO.overlap = true
	}
	time.Sleep(5 * time.Millisecond)
	atomic.AddInt32(&localFakeIO.busy, -1)
	localFakeIO.mu.Lock()
	localFakeIO.pastes = append(localFakeIO.pastes, [2]string{path, title})
	localFakeIO.mu.Unlock()
	return nil
}
func (localFakeIO *fakeIO) Key(path, key string) error {
	localFakeIO.mu.Lock()
	localFakeIO.keys = append(localFakeIO.keys, [2]string{path, key})
	localFakeIO.mu.Unlock()
	return nil
}
func (localFakeIO *fakeIO) Close(id string) error {
	localFakeIO.mu.Lock()
	localFakeIO.closed = append(localFakeIO.closed, id)
	localFakeIO.mu.Unlock()
	return nil
}

// setup copie les fixtures réelles du spike dans un cfgRoot jetable.
func setup(t *testing.T) (string, *fakeIO) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "teams", trName)
	os.MkdirAll(filepath.Join(dir, "inboxes"), 0o755)
	for src, dst := range map[string]string{"config.json": "config.json", "inbox-ada.json": "inboxes/ada.json", "inbox-team-lead.json": "inboxes/team-lead.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "teams", src))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, dst), data, 0o644)
	}
	localFakeIO := &fakeIO{panes: map[string]string{trLead: trLPane}, idle: []string{trLead}, screens: map[string]string{}, capErr: map[string]bool{}}
	old := teamPanes
	teamPanes = localFakeIO
	teamRunMu.Lock()
	teamLog, teamQueue = map[string][]InboxMsg{}, map[string][]queuedMsg{}
	teamRunMu.Unlock()
	t.Cleanup(func() { teamPanes = old })
	return root, localFakeIO
}

func setActive(t *testing.T, root, mate string, active bool) {
	t.Helper()
	path := filepath.Join(root, "teams", trName, "config.json")
	data, _ := os.ReadFile(path)
	text := string(data)
	index := strings.Index(text, `"name": "`+mate+`"`)
	innerIndex := index + strings.Index(text[index:], `"isActive": `)
	thirdIndex := innerIndex + len(`"isActive": `)
	number := thirdIndex + strings.IndexAny(text[thirdIndex:], ",\n}")
	value := "false"
	if active {
		value = "true"
	}
	os.WriteFile(path, []byte(text[:thirdIndex]+value+text[number:]), 0o644)
}

func TestSendToIdleMate(t *testing.T) {
	root, fakeIO := setup(t)
	flag, err := teamSend(root, trName, "max", "ada", "salut")
	if err != nil || flag {
		t.Fatal(flag, err)
	}
	if len(fakeIO.pastes) != 1 || fakeIO.pastes[0] != [2]string{"%67", "salut"} {
		t.Fatalf("collages : %v", fakeIO.pastes)
	}
	directMsgs, _ := journalRead(root, trName)
	if len(directMsgs) != 1 || directMsgs[0].Author != "max" || directMsgs[0].To != "ada" {
		t.Fatalf("journal : %v", directMsgs)
	}
}

func TestSendToBusyMateQueues(t *testing.T) {
	root, fakeIO := setup(t)
	setActive(t, root, "ada", true)
	flag, err := teamSend(root, trName, "max", "ada", "salut")
	if err != nil || !flag || len(fakeIO.pastes) != 0 {
		t.Fatal(flag, err, fakeIO.pastes)
	}
	localTeamView, _ := teamView(root, trName)
	if localTeamView.Queued != 1 {
		t.Fatalf("Queued = %d", localTeamView.Queued)
	}
}

func TestQueueFlushesWhenIdle(t *testing.T) {
	root, fakeIO := setup(t)
	setActive(t, root, "ada", true)
	teamSend(root, trName, "max", "ada", "un")
	teamSend(root, trName, "max", "ada", "deux")
	setActive(t, root, "ada", false)
	localTeamView, _ := teamView(root, trName)
	if len(fakeIO.pastes) != 1 || fakeIO.pastes[0][1] != "un" || localTeamView.Queued != 1 {
		t.Fatalf("collages %v queued %d", fakeIO.pastes, localTeamView.Queued)
	}
	if directMsgs, _ := journalRead(root, trName); len(directMsgs) != 1 || directMsgs[0].Text != "un" {
		t.Fatalf("journal : %v", directMsgs)
	}
}

func TestSendRejectsControlChars(t *testing.T) {
	root, fakeIO := setup(t)
	if _, err := teamSend(root, trName, "max", "ada", "a\x1bb"); err == nil || len(fakeIO.pastes) != 0 {
		t.Fatal("caractère de contrôle accepté")
	}
}

func TestSendNeverAnswersPermissionPrompt(t *testing.T) {
	root, fakeIO := setup(t)
	data, _ := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	fakeIO.screens[trLPane] = string(data)
	flag, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !flag || len(fakeIO.pastes) != 0 {
		t.Fatal(flag, err, fakeIO.pastes)
	}
}

func TestTeamKeyOnlyAllowedKeys(t *testing.T) {
	root, fakeIO := setup(t)
	fakeIO.screens[trLPane] = permFixture(t)
	if err := teamKey(root, trName, "team-lead", "rm -rf", ""); err == nil {
		t.Fatal("touche refusée attendue")
	}
	if err := teamKey(root, trName, "team-lead", "Enter", ""); err != nil || len(fakeIO.keys) != 1 {
		t.Fatal(err, fakeIO.keys)
	}
}

func TestTeamKeyTargetsLeadPane(t *testing.T) {
	root, fakeIO := setup(t)
	fakeIO.screens[trLPane] = permFixture(t)
	if err := teamKey(root, trName, "team-lead", "1", ""); err != nil || fakeIO.keys[0] != [2]string{trLPane, "1"} {
		t.Fatal(err, fakeIO.keys)
	}
}

func TestCloseDropsJournal(t *testing.T) {
	root, fakeIO := setup(t)
	setActive(t, root, "ada", true)
	teamSend(root, trName, "max", "ada", "en file")
	setActive(t, root, "hugo", false)
	teamSend(root, trName, "max", "hugo", "direct")
	teamView(root, trName)
	if err := teamClose(root, trName); err != nil {
		t.Fatal(err)
	}
	if len(fakeIO.closed) != 1 || fakeIO.closed[0] != trLead {
		t.Fatalf("closed : %v", fakeIO.closed)
	}
	if directMsgs, _ := journalRead(root, trName); len(directMsgs) != 0 {
		t.Fatalf("journal : %v", directMsgs)
	}
	if len(teamQueue[trName]) != 0 || len(teamLog[trName]) != 0 {
		t.Fatal("file ou journal mémoire non vidés")
	}
}

func TestTeamViewUnknownFormat(t *testing.T) {
	root, _ := setup(t)
	os.WriteFile(filepath.Join(root, "teams", trName, "config.json"), []byte("{pas du json"), 0o644)
	localTeamView, err := teamView(root, trName)
	if err != nil || len(localTeamView.Unknown) == 0 {
		t.Fatal(localTeamView.Unknown, err)
	}
}

func TestTeamViewPermissionDetected(t *testing.T) {
	root, fakeIO := setup(t)
	data, _ := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	fakeIO.screens[trLPane] = string(data)
	// yan n'est pas dans la config du spike : on l'y ajoute
	path := filepath.Join(root, "teams", trName, "config.json")
	localData, _ := os.ReadFile(path)
	localData = []byte(strings.Replace(string(localData), `"members": [`, `"members": [{"agentId":"yan@x","name":"yan","tmuxPaneId":"%70","backendType":"tmux"},`, 1))
	os.WriteFile(path, localData, 0o644)
	localTeamView, err := teamView(root, trName)
	if err != nil || localTeamView.Permission == "" || !strings.Contains(localTeamView.Permission, "from the yan agent") {
		t.Fatal(localTeamView.Permission, err)
	}
	for _, agentView := range localTeamView.Agents {
		if (agentView.Name == "yan" || agentView.Lead) && agentView.State != "waiting" {
			t.Fatalf("%s : %s", agentView.Name, agentView.State)
		}
	}
}

func TestTeamViewMessagesAccumulate(t *testing.T) {
	root, _ := setup(t)
	localTeamView, _ := teamView(root, trName)
	if len(localTeamView.Messages) != 2 {
		t.Fatalf("messages : %+v", localTeamView.Messages)
	}
	os.WriteFile(filepath.Join(root, "teams", trName, "inboxes", "ada.json"), []byte("[]"), 0o644)
	localTeamView, _ = teamView(root, trName)
	localTeamView, _ = teamView(root, trName)
	if len(localTeamView.Messages) != 2 {
		t.Fatalf("après vidage : %+v", localTeamView.Messages)
	}
}

func TestTeamListSkipsStale(t *testing.T) {
	root, fakeIO := setup(t)
	if teamBriefs := teamList(root); len(teamBriefs) != 1 || teamBriefs[0].Name != trName || teamBriefs[0].Members < 3 {
		t.Fatalf("liste : %+v", teamBriefs)
	}
	fakeIO.panes = map[string]string{}
	if teamBriefs := teamList(root); len(teamBriefs) != 0 {
		t.Fatalf("équipe périmée listée : %+v", teamBriefs)
	}
}

func TestTeamBadName(t *testing.T) {
	root, fakeIO := setup(t)
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
	if teamClose(root, bad) == nil || len(fakeIO.closed) != 0 || len(fakeIO.pastes) != 0 || len(fakeIO.keys) != 0 {
		t.Error("teamClose")
	}
}

func TestSendQueueFull(t *testing.T) {
	root, _ := setup(t)
	setActive(t, root, "ada", true)
	for index := 0; index < teamQueueMax; index++ {
		if flag, err := teamSend(root, trName, "max", "ada", "m"); err != nil || !flag {
			t.Fatal(index, flag, err)
		}
	}
	_, err := teamSend(root, trName, "max", "ada", "m")
	if err == nil || !isMsg(err, "srv.teams_run.fileOuverte") {
		t.Fatalf("file pleine : %v", err)
	}
}

func TestConcurrentSendsSerialised(t *testing.T) {
	root, fakeIO := setup(t)
	var wg sync.WaitGroup
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() { defer wg.Done(); teamSend(root, trName, "max", "hugo", "x") }()
	}
	wg.Wait()
	if fakeIO.overlap || len(fakeIO.pastes) != 8 {
		t.Fatalf("chevauchement %v, collages %d", fakeIO.overlap, len(fakeIO.pastes))
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
	root, fakeIO := setup(t)
	fakeIO.screens[trLPane] = leadOwnPrompt
	flag, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !flag || len(fakeIO.pastes) != 0 {
		t.Fatal(flag, err, fakeIO.pastes)
	}
	localTeamView, _ := teamView(root, trName)
	if localTeamView.Permission == "" || len(fakeIO.pastes) != 0 {
		t.Fatalf("permission %q, collages %v", localTeamView.Permission, fakeIO.pastes)
	}
}

func TestSendNeverAnswersLongTeammatePrompt(t *testing.T) {
	root, fakeIO := setup(t)
	data, _ := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	parts := strings.SplitN(string(data), "╌", 2)
	fakeIO.screens[trLPane] = parts[0] + strings.Repeat("   | ligne de diff\n", 40) + "╌" + parts[1]
	flag, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !flag || len(fakeIO.pastes) != 0 {
		t.Fatal(flag, err, fakeIO.pastes)
	}
}

func TestPromptAppearsBetweenCheckAndPaste(t *testing.T) {
	root, fakeIO := setup(t)
	fakeIO.script = map[string][]string{trLPane: {"", leadOwnPrompt}}
	flag, err := teamSend(root, trName, "max", "team-lead", "oui")
	if err != nil || !flag || len(fakeIO.pastes) != 0 {
		t.Fatal(flag, err, fakeIO.pastes)
	}
	if count := len(teamQueue[trName]); count != 1 {
		t.Fatalf("file = %d", count)
	}
}

func TestTeamKeyScreenNeedLiveTeam(t *testing.T) {
	root, fakeIO := setup(t)
	fakeIO.panes = map[string]string{}
	if teamKey(root, trName, "ada", "1", "") == nil || len(fakeIO.keys) != 0 {
		t.Error("teamKey sur une team périmée")
	}
	caps := fakeIO.caps
	if _, err := teamScreen(root, trName, "ada"); err == nil || fakeIO.caps != caps {
		t.Error("teamScreen sur une team périmée")
	}
}

func permFixture(t *testing.T) string {
	data, err := os.ReadFile("testdata/teams/permission-prompt-lead-pane.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTeamKeyRefusesWhenNoDialog(t *testing.T) {
	root, fakeIO := setup(t)
	if err := teamKey(root, trName, "team-lead", "1", ""); err == nil || len(fakeIO.keys) != 0 {
		t.Fatal(err, fakeIO.keys)
	}
}

func TestTeamKeyRefusesWhenDialogChanged(t *testing.T) {
	root, fakeIO := setup(t)
	fakeIO.screens[trLPane] = permFixture(t)
	if err := teamKey(root, trName, "team-lead", "1", "autre dialogue"); err == nil || len(fakeIO.keys) != 0 {
		t.Fatal(err, fakeIO.keys)
	}
}

func TestTeamKeyOKWhenExpectMatches(t *testing.T) {
	root, fakeIO := setup(t)
	fakeIO.screens[trLPane] = permFixture(t)
	_, txt, _ := permText(fakeIO.screens[trLPane])
	if err := teamKey(root, trName, "team-lead", "1", "  "+txt+"\n"); err != nil || len(fakeIO.keys) != 1 {
		t.Fatal(err, fakeIO.keys)
	}
}

func TestCloseFinishedTeamCleansUp(t *testing.T) {
	root, fakeIO := setup(t)
	setActive(t, root, "hugo", false)
	teamSend(root, trName, "max", "hugo", "direct")
	teamView(root, trName)
	fakeIO.panes = map[string]string{}
	if err := teamClose(root, trName); err != nil {
		t.Fatal(err)
	}
	if len(fakeIO.closed) != 0 {
		t.Fatalf("Close appelé : %v", fakeIO.closed)
	}
	if directMsgs, _ := journalRead(root, trName); len(directMsgs) != 0 {
		t.Fatalf("journal : %v", directMsgs)
	}
	if len(teamQueue[trName]) != 0 || len(teamLog[trName]) != 0 {
		t.Fatal("file ou journal mémoire non vidés")
	}
}

func TestCloseLiveTeamStillClosesFirst(t *testing.T) {
	root, fakeIO := setup(t)
	teamSend(root, trName, "max", "ada", "x")
	if err := teamClose(root, trName); err != nil || len(fakeIO.closed) != 1 {
		t.Fatalf("%v %v", err, fakeIO.closed)
	}
	if err := teamClose(root, "../x"); !isMsg(err, "srv.teams_run.nomInvalide") {
		t.Fatal(err)
	}
}

func TestLogKeepsIdleWithResult(t *testing.T) {
	root, _ := setup(t)
	teamView(root, trName)
	var got *InboxMsg
	for _, inboxMsg := range teamLog[trName] {
		if inboxMsg.Kind == "idle" {
			got = &inboxMsg
		}
	}
	if got == nil || got.From != "ada" || !strings.Contains(got.Text, "/tmp/gld-spike-perm.txt") {
		t.Fatalf("%+v", teamLog[trName])
	}
}

func TestLogDropsIdleWithoutResult(t *testing.T) {
	tf := TeamFiles{Name: "t-vide", Inbox: []InboxMsg{{ID: "a", Kind: "idle"}, {ID: "b", Kind: "idle", Text: "fait"}}}
	logInbox(tf)
	inboxMsgs := teamLog["t-vide"]
	if len(inboxMsgs) != 1 || inboxMsgs[0].ID != "b" {
		t.Fatalf("%+v", inboxMsgs)
	}
}

func TestTeamSendReportsTmuxError(t *testing.T) {
	root, fakeIO := setup(t)
	fakeIO.panesErr = errors.New("tmux gelé")
	fakeIO.panes = nil
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
	if localTeamView, err := teamView(root, trName); err != nil || localTeamView.Live {
		t.Fatalf("view: %v %v", localTeamView.Live, err)
	}
}

func TestTeamViewUsesRoster(t *testing.T) {
	root, _ := setup(t)
	if err := rosterWrite(root, trName, map[string]string{"ada": "backend", "hugo": "testeur"}); err != nil {
		t.Fatal(err)
	}
	localTeamView, err := teamView(root, trName)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]AgentView{}
	for _, agentView := range localTeamView.Agents {
		by[agentView.Name] = agentView
	}
	if agentView := by["ada"]; agentView.Display != "Ada" || agentView.Role != "Développement backend" || agentView.AgentID != "backend" {
		t.Errorf("ada : %+v", agentView)
	}
	if agentView := by["hugo"]; agentView.Display != "Hugo" || agentView.Role != "Testeur" || agentView.AgentID != "testeur" {
		t.Errorf("hugo : %+v", agentView)
	}
	if agentView := by["zoe"]; agentView.Display != "Zoe" || agentView.Role != "" {
		t.Errorf("zoe : %+v", agentView)
	}
	if agentView := by["team-lead"]; agentView.Display != "Chef de projet" || agentView.Role != "" || !agentView.Lead {
		t.Errorf("chef : %+v", agentView)
	}
}

func TestTeamViewWithoutRoster(t *testing.T) {
	root, _ := setup(t)
	localTeamView, err := teamView(root, trName)
	if err != nil {
		t.Fatal(err)
	}
	for _, agentView := range localTeamView.Agents {
		if !agentView.Lead && (agentView.Role != "" || agentView.Display != upFirst(agentView.Name)) {
			t.Errorf("%+v", agentView)
		}
	}
}

func TestTeamViewCorruptRoster(t *testing.T) {
	root, _ := setup(t)
	os.MkdirAll(filepath.Join(root, journalDir), 0o700)
	os.WriteFile(filepath.Join(root, journalDir, trName+".roster.json"), []byte("{pas du json"), 0o600)
	localTeamView, err := teamView(root, trName)
	if err != nil || len(localTeamView.Agents) == 0 {
		t.Fatal(localTeamView, err)
	}
}

func TestCloseDropsRoster(t *testing.T) {
	for _, live := range []bool{true, false} {
		root, fakeIO := setup(t)
		rosterWrite(root, trName, map[string]string{"ada": "backend"})
		if !live {
			fakeIO.panes = map[string]string{}
		}
		if err := teamClose(root, trName); err != nil {
			t.Fatal(live, err)
		}
		if lookup, _ := rosterRead(root, trName); len(lookup) != 0 {
			t.Errorf("live=%v roster : %v", live, lookup)
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
	root, localFakeIO := setup(t)
	setActive(t, root, "hugo", false)
	teamSend(root, trName, "max", "hugo", "direct")
	teamView(root, trName)
	teamRunMu.Lock()
	teamQueue[trName] = []queuedMsg{{}}
	teamRunMu.Unlock()
	if len(teamLog[trName]) == 0 {
		t.Fatal("log non amorcé")
	}
	localFakeIO.panes = map[string]string{}
	return root, localFakeIO
}

func stateIntact(t *testing.T, root string, localFakeIO *fakeIO) {
	t.Helper()
	if directMsgs, _ := journalRead(root, trName); len(directMsgs) == 0 {
		t.Fatal("journal supprimé")
	}
	if len(teamQueue[trName]) == 0 || len(teamLog[trName]) == 0 {
		t.Fatal("log ou file vidés")
	}
	if len(localFakeIO.closed) != 0 {
		t.Fatalf("Close appelé : %v", localFakeIO.closed)
	}
}

func stateCleaned(t *testing.T, root string, localFakeIO *fakeIO) {
	t.Helper()
	if directMsgs, _ := journalRead(root, trName); len(directMsgs) != 0 || len(localFakeIO.closed) != 0 {
		t.Fatal("nettoyage attendu")
	}
	if len(teamQueue[trName]) != 0 || len(teamLog[trName]) != 0 {
		t.Fatal("file ou log non vidés")
	}
}

var noServer = terr("srv.sendkeys.aucuneSessionNA")

func TestCloseAbortsWhenStateUnknown(t *testing.T) {
	root, fakeIO := seedClose(t)
	fakeIO.unknown = true
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.etatInconnu") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, fakeIO)
}

func TestCloseAbortsWhenLeadAliveButNoPane(t *testing.T) {
	root, fakeIO := seedClose(t)
	fakeIO.live = map[string]bool{trLead: true}
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.chefOuvertSansTerminal") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, fakeIO)
}

func TestCloseAbortsWhenLeadAliveAndTmuxUnreachable(t *testing.T) {
	root, fakeIO := seedClose(t)
	fakeIO.live, fakeIO.panes, fakeIO.panesErr = map[string]bool{trLead: true}, nil, noServer
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.chefOuvertSansTerminal") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, fakeIO)
}

func TestCloseCleansWhenLeadNotAlive(t *testing.T) {
	for name, set := range map[string]func(*fakeIO){
		"vide":     func(localFakeIO *fakeIO) {},
		"sentinel": func(localFakeIO *fakeIO) { localFakeIO.panes, localFakeIO.panesErr = nil, noServer },
	} {
		t.Run(name, func(t *testing.T) {
			root, fakeIO := seedClose(t)
			set(fakeIO)
			if err := teamClose(root, trName); err != nil {
				t.Fatal(err)
			}
			stateCleaned(t, root, fakeIO)
		})
	}
}

func TestCloseStillAbortsOnRealTmuxError(t *testing.T) {
	root, fakeIO := seedClose(t)
	fakeIO.panes, fakeIO.panesErr = nil, errors.New("tmux gelé")
	if err := teamClose(root, trName); !isMsg(err, "srv.teams_run.tmuxInutilisable") {
		t.Fatalf("err : %v", err)
	}
	stateIntact(t, root, fakeIO)
}

func TestCloseReadsLeadStateBeforePanes(t *testing.T) {
	root, fakeIO := seedClose(t)
	fakeIO.order = nil
	teamClose(root, trName)
	if len(fakeIO.order) < 2 || fakeIO.order[0] != "lead" || fakeIO.order[1] != "panes" {
		t.Fatalf("ordre : %v", fakeIO.order)
	}
}

func TestCloseAsksLeadStateForTheLeadSessionID(t *testing.T) {
	root, fakeIO := seedClose(t)
	tf, err := readTeam(root, trName)
	if err != nil || tf.LeadSessionID == "" {
		t.Fatal(err, tf.LeadSessionID)
	}
	teamClose(root, trName)
	if len(fakeIO.leadIDs) != 1 || fakeIO.leadIDs[0] != tf.LeadSessionID {
		t.Fatalf("ids : %v, attendu %q", fakeIO.leadIDs, tf.LeadSessionID)
	}
}

func TestPermTextTruncated(t *testing.T) {
	if _, _, tr := permText(permFixture(t)); tr {
		t.Fatal("fixture should fit")
	}
	if _, _, tr := permText("x · from the yan agent\n" + strings.Repeat("a", permCut+10)); !tr {
		t.Fatal("byte cut must be flagged")
	}
	if _, _, tr := permText(strings.Repeat("echo line\n", 40)); !tr {
		t.Fatal("line cut must be flagged")
	}
	if _, _, tr := permText(strings.Repeat("echo line\n", 10)); tr {
		t.Fatal("short screen is not truncated")
	}
}

func TestTeamKeyRefusesApprovalWhenTruncated(t *testing.T) {
	root, fakeIO := setup(t)
	scr := permFixture(t)
	fakeIO.screens[trLPane] = strings.Replace(scr, "Fetch · from the yan agent", strings.Repeat("a", permCut+10), 1)
	if _, _, tr := permText(fakeIO.screens[trLPane]); !tr {
		t.Fatal("setup: screen should be truncated")
	}
	if err := teamKey(root, trName, "team-lead", "1", ""); err == nil || len(fakeIO.keys) != 0 {
		t.Fatal(err, fakeIO.keys)
	}
	if err := teamKey(root, trName, "team-lead", "Escape", ""); err != nil || len(fakeIO.keys) != 1 {
		t.Fatal(err, fakeIO.keys)
	}
}
