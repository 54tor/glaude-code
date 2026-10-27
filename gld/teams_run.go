package main

// État d'une team vivante, messages directs, touches et fermeture. Les terminaux passent par paneIO pour être simulés dans les tests.

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

type paneIO interface {
	Panes() (map[string]string, error) // id de session -> pane
	Idle() []string
	Capture(pane string) (string, error)
	Paste(pane, text string) error
	Key(pane, key string) error
	Close(sessionID string) error
}

type realPanes struct{}

func (realPanes) Panes() (map[string]string, error)   { return sendable() }
func (realPanes) Idle() []string                      { return waiting() }
func (realPanes) Capture(pane string) (string, error) { return capturePane(pane) }
func (realPanes) Paste(pane, text string) error       { return sendToPane(pane, text) }
func (realPanes) Close(id string) error               { return closeSession(id) }
func (realPanes) Key(pane, key string) error {
	k, ok := allowedKeys[key]
	if !ok {
		return terr("srv.sendkeys.toucheNonPermise")
	}
	_, err := tmuxRun("", "send-keys", "-t", pane, k)
	return err
}

var teamPanes paneIO = realPanes{}

type AgentView struct {
	Name, AgentID, Role, Pane, State string
	Lead                             bool
}

type TeamView struct {
	Name, LeadSessionID string
	Live                bool
	Agents              []AgentView
	Tasks               []TeamTask
	Messages            []InboxMsg
	Direct              []DirectMsg
	Queued              int
	Permission          string
	Unknown             []string
}

type TeamBrief struct {
	Name, LeadSessionID string
	Live                bool
	Members             int
}

const (
	teamLogMax   = 500
	teamQueueMax = 20
	permCut      = 2000
)

type queuedMsg struct{ Author, To, Text string }

var (
	teamRunMu sync.Mutex // protège teamLog et teamQueue ; tenu pendant un flush (ponytail : verrou global, volume faible)
	teamLog   = map[string][]InboxMsg{}
	teamQueue = map[string][]queuedMsg{}
	paneMus   sync.Map // pane -> *sync.Mutex : deux collages sur un pane ne s'entrelacent pas
)

var (
	permTitleRe = regexp.MustCompile(`· from the (\S+) agent`)
	permOptRe   = regexp.MustCompile(`^\s*(❯\s*)?1\. `)
)

type teamCtx struct {
	tf       TeamFiles
	leadPane string
}

func teamLoad(cfgRoot, name string) (teamCtx, error) {
	if !teamNameRe.MatchString(name) {
		return teamCtx{}, terr("srv.teams_run.nomInvalide")
	}
	tf, err := readTeam(cfgRoot, name)
	if err != nil {
		return teamCtx{}, err
	}
	panes, _ := teamPanes.Panes()
	logInbox(tf)
	return teamCtx{tf: tf, leadPane: panes[tf.LeadSessionID]}, nil
}

// logInbox garde les messages vus : Claude Code retire les messages lus de ses boîtes.
func logInbox(tf TeamFiles) {
	teamRunMu.Lock()
	defer teamRunMu.Unlock()
	l := teamLog[tf.Name]
	seen := map[string]bool{}
	for _, m := range l {
		seen[m.ID] = true
	}
	for _, m := range tf.Inbox {
		if m.Kind == "idle" || m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		l = append(l, m)
	}
	if len(l) > teamLogMax {
		l = l[len(l)-teamLogMax:]
	}
	teamLog[tf.Name] = l
}

// target : le pane d'un agent (le chef passe par le pane de sa session) ; ok faux si l'agent n'est pas dans la team.
func (c teamCtx) target(agent string) (pane string, ok bool) {
	for _, m := range c.tf.Members {
		if m.Name == agent {
			if m.Lead {
				return c.leadPane, true
			}
			return m.Pane, true
		}
	}
	return "", false
}

func (c teamCtx) leadName() string {
	for _, m := range c.tf.Members {
		if m.Lead {
			return m.Name
		}
	}
	return ""
}

// permission : (agent demandeur, texte) quand le pane du chef affiche une demande d'un coéquipier, sinon ("", "").
func (c teamCtx) permission() (agent, text string, captured bool) {
	if c.leadPane == "" {
		return "", "", false
	}
	scr, err := teamPanes.Capture(c.leadPane)
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(scr, "\n")
	// seules les dernières lignes comptent : une demande déjà traitée reste dans l'historique capturé
	n, start := 0, 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			n++
		}
		if n == 20 {
			start = i
			break
		}
	}
	lines = lines[start:]
	for i := len(lines) - 1; i >= 0; i-- {
		m := permTitleRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		rest := lines[i:]
		for _, l := range rest[1:] {
			if permOptRe.MatchString(l) {
				t := strings.TrimRight(strings.Join(rest, "\n"), " \n")
				if len(t) > permCut {
					t = t[:permCut]
					for !utf8.ValidString(t) {
						t = t[:len(t)-1]
					}
				}
				return m[1], t, true
			}
		}
		break
	}
	return "", "", true
}

// states : état de chaque agent par nom, et le texte d'une demande de permission en attente.
func (c teamCtx) states() (map[string]string, string) {
	st := map[string]string{}
	who, ptxt, captured := c.permission()
	idle := false
	for _, id := range teamPanes.Idle() {
		if id == c.tf.LeadSessionID {
			idle = true
		}
	}
	for _, m := range c.tf.Members {
		switch {
		case m.Lead && c.leadPane == "":
			st[m.Name] = "unknown"
		case m.Lead && !captured:
			st[m.Name] = "unknown"
		case m.Lead && who != "":
			st[m.Name] = "waiting"
		case m.Lead && idle:
			st[m.Name] = "idle"
		case m.Lead:
			st[m.Name] = "working"
		case m.Pane == "":
			st[m.Name] = "unknown"
		case m.Name == who:
			st[m.Name] = "waiting"
		case m.Active:
			st[m.Name] = "working"
		default:
			st[m.Name] = "idle"
		}
	}
	return st, ptxt
}

func paneMu(pane string) *sync.Mutex {
	mu, _ := paneMus.LoadOrStore(pane, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func deliver(cfgRoot, team, pane string, m queuedMsg) error {
	mu := paneMu(pane)
	mu.Lock()
	err := teamPanes.Paste(pane, m.Text)
	mu.Unlock()
	if err != nil {
		return terr("srv.teams_run.envoiImpossible", "W1", err)
	}
	_ = journalAppend(cfgRoot, team, DirectMsg{Author: m.Author, To: m.To, Text: m.Text}) // journal plein : le message est parti quand même
	return nil
}

// flush envoie, dans l'ordre, le premier message en file de chaque agent redevenu disponible.
func (c teamCtx) flush(cfgRoot string, st map[string]string) {
	teamRunMu.Lock()
	defer teamRunMu.Unlock()
	q := teamQueue[c.tf.Name]
	if len(q) == 0 {
		return
	}
	first := map[string]bool{}
	var keep []queuedMsg
	for _, m := range q {
		if first[m.To] {
			keep = append(keep, m)
			continue
		}
		first[m.To] = true
		pane, ok := c.target(m.To)
		if ok && pane != "" && st[m.To] == "idle" && deliver(cfgRoot, c.tf.Name, pane, m) == nil {
			continue
		}
		keep = append(keep, m)
	}
	teamQueue[c.tf.Name] = keep
}

func (c teamCtx) role(cat Catalog, m TeamMember) string {
	id, _, _ := strings.Cut(m.AgentID, "@")
	for _, a := range cat.Agents {
		if a.ID == id || a.ID == m.Name {
			return a.Role
		}
	}
	return ""
}

func teamView(cfgRoot, name string) (TeamView, error) {
	c, err := teamLoad(cfgRoot, name)
	if err != nil {
		return TeamView{}, err
	}
	st, perm := c.states()
	c.flush(cfgRoot, st)
	cat, _ := loadCatalog(cfgRoot)
	v := TeamView{Name: name, LeadSessionID: c.tf.LeadSessionID, Live: c.leadPane != "", Tasks: c.tf.Tasks,
		Permission: perm, Unknown: c.tf.Unknown}
	for _, m := range c.tf.Members {
		pane := m.Pane
		if m.Lead {
			pane = c.leadPane
		}
		v.Agents = append(v.Agents, AgentView{Name: m.Name, AgentID: m.AgentID, Role: c.role(cat, m), Pane: pane, State: st[m.Name], Lead: m.Lead})
	}
	teamRunMu.Lock()
	v.Messages = append([]InboxMsg(nil), teamLog[name]...)
	v.Queued = len(teamQueue[name])
	teamRunMu.Unlock()
	v.Direct, _ = journalRead(cfgRoot, name)
	return v, nil
}

func teamList(cfgRoot string) []TeamBrief {
	panes, _ := teamPanes.Panes()
	out := []TeamBrief{}
	for _, n := range listTeams(cfgRoot) {
		tf, err := readTeam(cfgRoot, n)
		if err != nil || panes[tf.LeadSessionID] == "" {
			continue
		}
		out = append(out, TeamBrief{Name: n, LeadSessionID: tf.LeadSessionID, Live: true, Members: len(tf.Members)})
	}
	return out
}

func teamSend(cfgRoot, name, author, to, text string) (bool, error) {
	if !teamNameRe.MatchString(name) {
		return false, terr("srv.teams_run.nomInvalide")
	}
	if err := checkSendText(text); err != nil {
		return false, err
	}
	c, err := teamLoad(cfgRoot, name)
	if err != nil {
		return false, err
	}
	if c.leadPane == "" {
		return false, terr("srv.teams_run.equipeInactive")
	}
	pane, ok := c.target(to)
	if !ok {
		return false, terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return false, terr("srv.teams_run.paneAbsent")
	}
	st, _ := c.states()
	m := queuedMsg{author, to, text}
	teamRunMu.Lock()
	pending := false
	for _, q := range teamQueue[name] {
		pending = pending || q.To == to // l'ordre d'envoi est conservé
	}
	if st[to] != "idle" || pending {
		defer teamRunMu.Unlock()
		if len(teamQueue[name]) >= teamQueueMax {
			return false, terr("srv.teams_run.fileOuverte")
		}
		teamQueue[name] = append(teamQueue[name], m)
		return true, nil
	}
	teamRunMu.Unlock()
	return false, deliver(cfgRoot, name, pane, m)
}

func teamKey(cfgRoot, name, agent, key string) error {
	if !teamNameRe.MatchString(name) {
		return terr("srv.teams_run.nomInvalide")
	}
	if _, ok := allowedKeys[key]; !ok {
		return terr("srv.sendkeys.toucheNonPermise")
	}
	c, err := teamLoad(cfgRoot, name)
	if err != nil {
		return err
	}
	pane, ok := c.target(agent)
	if !ok {
		return terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return terr("srv.teams_run.paneAbsent")
	}
	return teamPanes.Key(pane, key)
}

func teamScreen(cfgRoot, name, agent string) (string, error) {
	c, err := teamLoad(cfgRoot, name)
	if err != nil {
		return "", err
	}
	pane, ok := c.target(agent)
	if !ok {
		return "", terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return "", terr("srv.teams_run.paneAbsent")
	}
	scr, err := teamPanes.Capture(pane)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(scr, " \n"), "\n")
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	return strings.Join(lines, "\n"), nil
}

func teamClose(cfgRoot, name string) error {
	if !teamNameRe.MatchString(name) {
		return terr("srv.teams_run.nomInvalide")
	}
	tf, err := readTeam(cfgRoot, name)
	if err != nil {
		return err
	}
	if err := teamPanes.Close(tf.LeadSessionID); err != nil {
		return err
	}
	if err := journalDrop(cfgRoot, name); err != nil {
		return err
	}
	teamRunMu.Lock()
	delete(teamLog, name)
	delete(teamQueue, name)
	teamRunMu.Unlock()
	return nil
}
