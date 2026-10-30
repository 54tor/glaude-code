package main

// État d'une team vivante, messages directs, touches et fermeture. Les terminaux passent par paneIO pour être simulés dans les tests.

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type paneIO interface {
	Panes() (map[string]string, error)              // id de session -> pane
	LeadState(sessionID string) (alive, known bool) // relevé frais des sessions ouvertes ; known=false s'il est illisible
	Idle() []string
	Capture(pane string) (string, error)
	Paste(pane, text string) error
	Key(pane, key string) error
	Close(sessionID string) error
}

type realPanes struct{}

func (realPanes) Panes() (map[string]string, error) { return sendable() }
func (realPanes) LeadState(id string) (isAlive, known bool) {
	forgetAgents()
	m := alive()
	return m[id], m != nil
}
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
	Name, Display, AgentID, Role, Pane, State string
	Lead                                      bool
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
	PermissionTruncated bool
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
)

type teamCtx struct {
	tf       TeamFiles
	leadPane string
	panesErr error
}

func teamLoad(cfgRoot, name string) (teamCtx, error) {
	if !teamNameRe.MatchString(name) {
		return teamCtx{}, terr("srv.teams_run.nomInvalide")
	}
	tf, err := readTeam(cfgRoot, name)
	if err != nil {
		return teamCtx{}, err
	}
	panes, perr := teamPanes.Panes()
	logInbox(tf)
	return teamCtx{tf: tf, leadPane: panes[tf.LeadSessionID], panesErr: perr}, nil
}

// needLive : erreur si la team n'est pas pilotable ; tmux injoignable se distingue d'une team terminée.
func (c teamCtx) needLive() error {
	if c.panesErr != nil {
		return terr("srv.teams_run.tmuxInutilisable", "W1", c.panesErr)
	}
	if c.leadPane == "" {
		return terr("srv.teams_run.equipeInactive")
	}
	return nil
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
		if (m.Kind == "idle" && m.Text == "") || m.ID == "" || seen[m.ID] {
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

// permission : l'écran du chef attend une réponse (screenAsks attrape aussi sa propre demande et les longues demandes) ; agent
// nomme le coéquipier demandeur quand le titre « · from the X agent » est visible. captured faux : écran illisible.
func (c teamCtx) permission() (agent, text string, trunc, asking, captured bool) {
	if c.leadPane == "" {
		return "", "", false, false, false
	}
	scr, err := teamPanes.Capture(c.leadPane)
	if err != nil {
		return "", "", false, false, false
	}
	if !screenAsks(scr) {
		return "", "", false, false, true
	}
	agent, text, trunc = permText(scr)
	return agent, text, trunc, true, true
}

// permText : le texte de la demande visible à l'écran (même coupe que la vue), et le coéquipier demandeur si le titre est lisible.
// trunc : des lignes ou des octets de la demande n'ont pas été gardés ; l'humain ne l'a pas vue en entier.
func permText(scr string) (agent, t string, trunc bool) {
	lines := strings.Split(strings.TrimRight(scr, " \n"), "\n")
	if len(lines) > 80 {
		lines = lines[len(lines)-80:]
	}
	from := len(lines) - 15
	if from < 0 {
		from = 0
	}
	titled := false
	for i := len(lines) - 1; i >= 0; i-- {
		if m := permTitleRe.FindStringSubmatch(lines[i]); m != nil {
			agent, from, titled = m[1], i, true
			break
		}
	}
	trunc = !titled && strings.TrimSpace(strings.Join(lines[:from], "")) != ""
	t = strings.TrimRight(strings.Join(lines[from:], "\n"), " \n")
	if len(t) > permCut {
		trunc = true
		t = t[:permCut]
		for !utf8.ValidString(t) {
			t = t[:len(t)-1]
		}
	}
	return agent, t, trunc
}

// states : état de chaque agent par nom, et le texte d'une demande de permission en attente.
func (c teamCtx) states() (map[string]string, string, bool) {
	st := map[string]string{}
	who, ptxt, trunc, asking, captured := c.permission()
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
		case m.Lead && asking:
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
	return st, ptxt, trunc
}

func paneMu(pane string) *sync.Mutex {
	mu, _ := paneMus.LoadOrStore(pane, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

var errAsking = terr("srv.teams_run.dialogueEnCours")

// deliver recontrôle l'écran du pane sous son verrou, juste avant le collage : un dialogue apparu depuis le relevé d'état serait
// validé par l'Entrée du collage. errAsking : rien n'est parti, l'appelant met en file.
func deliver(cfgRoot, team, pane string, m queuedMsg) error {
	mu := paneMu(pane)
	mu.Lock()
	scr, cerr := teamPanes.Capture(pane)
	if cerr != nil || screenAsks(scr) {
		mu.Unlock()
		return errAsking
	}
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

// identify : prénom d'affichage, identifiant et libellé de rôle d'un membre, d'après le roster (prénom -> slug). Le nom brut reste la clé des envois.
func identify(cat Catalog, roster map[string]string, m TeamMember) (display, agentID, role string) {
	byID := func(id string) (Agent, bool) {
		for _, a := range cat.Agents {
			if a.ID == id {
				return a, true
			}
		}
		return Agent{}, false
	}
	if m.Lead {
		if a, ok := byID("chef"); ok {
			return a.Name, m.AgentID, ""
		}
		return upFirst(m.Name), m.AgentID, ""
	}
	slug, known := roster[m.Name]
	if !known {
		return upFirst(m.Name), m.AgentID, ""
	}
	a, _ := byID(slug)
	return upFirst(m.Name), slug, a.Name
}

func upFirst(s string) string {
	if s == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

func teamView(cfgRoot, name string) (TeamView, error) {
	c, err := teamLoad(cfgRoot, name)
	if err != nil {
		return TeamView{}, err
	}
	st, perm, trunc := c.states()
	c.flush(cfgRoot, st)
	cat, _ := loadCatalog(cfgRoot)
	roster, _ := rosterRead(cfgRoot, name)
	v := TeamView{Name: name, LeadSessionID: c.tf.LeadSessionID, Live: c.leadPane != "", Tasks: c.tf.Tasks,
		Permission: perm, PermissionTruncated: trunc, Unknown: c.tf.Unknown}
	for _, m := range c.tf.Members {
		pane := m.Pane
		if m.Lead {
			pane = c.leadPane
		}
		disp, aid, role := identify(cat, roster, m)
		v.Agents = append(v.Agents, AgentView{Name: m.Name, Display: disp, AgentID: aid, Role: role, Pane: pane, State: st[m.Name], Lead: m.Lead})
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
	if err := c.needLive(); err != nil {
		return false, err
	}
	pane, ok := c.target(to)
	if !ok {
		return false, terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return false, terr("srv.teams_run.paneAbsent")
	}
	st, _, _ := c.states()
	m := queuedMsg{author, to, text}
	teamRunMu.Lock()
	pending := false
	for _, q := range teamQueue[name] {
		pending = pending || q.To == to // l'ordre d'envoi est conservé
	}
	teamRunMu.Unlock()
	if st[to] == "idle" && !pending {
		if err := deliver(cfgRoot, name, pane, m); err != errAsking {
			return false, err
		}
	}
	teamRunMu.Lock()
	defer teamRunMu.Unlock()
	if len(teamQueue[name]) >= teamQueueMax {
		return false, terr("srv.teams_run.fileOuverte")
	}
	teamQueue[name] = append(teamQueue[name], m)
	return true, nil
}

// teamKey : expect est la demande que l'humain a vue (vide : pas de comparaison). Sous le verrou du pane, rien ne part sans
// dialogue en attente, ni si l'écran a changé depuis : une réponse ne valide jamais un dialogue non vu.
// declineKeys : les touches qui refusent ou interrompent ; seules permises quand la demande n'a pas été vue en entier.
var declineKeys = map[string]bool{"Escape": true, "n": true, "CtrlC": true}

func teamKey(cfgRoot, name, agent, key, expect string) error {
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
	if err := c.needLive(); err != nil {
		return err
	}
	pane, ok := c.target(agent)
	if !ok {
		return terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return terr("srv.teams_run.paneAbsent")
	}
	mu := paneMu(pane)
	mu.Lock()
	defer mu.Unlock()
	scr, cerr := teamPanes.Capture(pane)
	if cerr != nil || !screenAsks(scr) {
		return terr("srv.teams_run.aucunDialogue")
	}
	_, fresh, trunc := permText(scr)
	if trunc && !declineKeys[key] {
		return terr("srv.teams_run.dialogueTronque")
	}
	if expect != "" {
		if strings.TrimSpace(fresh) != strings.TrimSpace(expect) {
			return terr("srv.teams_run.dialogueChange")
		}
	}
	return teamPanes.Key(pane, key)
}

func teamScreen(cfgRoot, name, agent string) (string, error) {
	c, err := teamLoad(cfgRoot, name)
	if err != nil {
		return "", err
	}
	if err := c.needLive(); err != nil {
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
		return "", terr("srv.teams_run.captureImpossible", "W1", err)
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
	// un seul relevé frais décide ; Panes() le réutilise (cache de 1,5 s). Illisible : on ne touche à rien.
	leadAlive, known := teamPanes.LeadState(tf.LeadSessionID)
	if !known {
		return terr("srv.teams_run.etatInconnu")
	}
	panes, perr := teamPanes.Panes()
	// serveur tmux disparu : plus aucun panneau
	if perr != nil && !isMsg(perr, "srv.sendkeys.aucuneSessionNA") {
		return terr("srv.teams_run.tmuxInutilisable", "W1", perr)
	}
	if panes[tf.LeadSessionID] != "" {
		if err := teamPanes.Close(tf.LeadSessionID); err != nil {
			return terr("srv.teams_run.fermetureImpossible", "W1", err)
		}
	} else if leadAlive {
		return terr("srv.teams_run.chefOuvertSansTerminal")
	}
	// team terminée (le chef a quitté) : plus de session à fermer, on nettoie quand même
	if err := journalDrop(cfgRoot, name); err != nil {
		return err
	}
	if err := rosterDrop(cfgRoot, name); err != nil {
		return err
	}
	teamRunMu.Lock()
	delete(teamLog, name)
	delete(teamQueue, name)
	teamRunMu.Unlock()
	return nil
}
