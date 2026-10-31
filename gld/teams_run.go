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
func (realPanes) LeadState(sessionID string) (isAlive, known bool) {
	forgetAgents()
	aliveSessions := alive()
	return aliveSessions[sessionID], aliveSessions != nil
}
func (realPanes) Idle() []string                      { return waiting() }
func (realPanes) Capture(pane string) (string, error) { return capturePane(pane) }
func (realPanes) Paste(pane, text string) error       { return sendToPane(pane, text) }
func (realPanes) Close(sessionID string) error        { return closeSession(sessionID) }
func (realPanes) Key(pane, key string) error {
	tmuxKey, isAllowed := allowedKeys[key]
	if !isAllowed {
		return terr("srv.sendkeys.toucheNonPermise")
	}
	_, err := tmuxRun("", "send-keys", "-t", pane, tmuxKey)
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

	permScanLines   = 80  // lignes de fin d'écran examinées pour trouver une demande de permission
	permTailLines   = 15  // lignes gardées quand le titre « · from the X agent » n'est pas visible
	teamScreenLines = 200 // lignes rendues par teamScreen
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
	teamFiles, err := readTeam(cfgRoot, name)
	if err != nil {
		return teamCtx{}, err
	}
	panes, panesErr := teamPanes.Panes()
	logInbox(teamFiles)
	return teamCtx{tf: teamFiles, leadPane: panes[teamFiles.LeadSessionID], panesErr: panesErr}, nil
}

// needLive : erreur si la team n'est pas pilotable ; tmux injoignable se distingue d'une team terminée.
func (teamContext teamCtx) needLive() error {
	if teamContext.panesErr != nil {
		return terr("srv.teams_run.tmuxInutilisable", "W1", teamContext.panesErr)
	}
	if teamContext.leadPane == "" {
		return terr("srv.teams_run.equipeInactive")
	}
	return nil
}

// logInbox garde les messages vus : Claude Code retire les messages lus de ses boîtes.
func logInbox(teamFiles TeamFiles) {
	teamRunMu.Lock()
	defer teamRunMu.Unlock()
	logged := teamLog[teamFiles.Name]
	seen := map[string]bool{}
	for _, message := range logged {
		seen[message.ID] = true
	}
	for _, message := range teamFiles.Inbox {
		if (message.Kind == "idle" && message.Text == "") || message.ID == "" || seen[message.ID] {
			continue
		}
		seen[message.ID] = true
		logged = append(logged, message)
	}
	if len(logged) > teamLogMax {
		logged = logged[len(logged)-teamLogMax:]
	}
	teamLog[teamFiles.Name] = logged
}

// target : le pane d'un agent (le chef passe par le pane de sa session) ; found faux si l'agent n'est pas dans la team.
func (teamContext teamCtx) target(agent string) (pane string, found bool) {
	for _, member := range teamContext.tf.Members {
		if member.Name == agent {
			if member.Lead {
				return teamContext.leadPane, true
			}
			return member.Pane, true
		}
	}
	return "", false
}

// permission : l'écran du chef attend une réponse (screenAsks attrape aussi sa propre demande et les longues demandes) ; agent
// nomme le coéquipier demandeur quand le titre « · from the X agent » est visible. captured faux : écran illisible.
func (teamContext teamCtx) permission() (agent, text string, truncated, asking, captured bool) {
	if teamContext.leadPane == "" {
		return "", "", false, false, false
	}
	screen, err := teamPanes.Capture(teamContext.leadPane)
	if err != nil {
		return "", "", false, false, false
	}
	if !screenAsks(screen) {
		return "", "", false, false, true
	}
	agent, text, truncated = permText(screen)
	return agent, text, truncated, true, true
}

// permText : le texte de la demande visible à l'écran (même coupe que la vue), et le coéquipier demandeur si le titre est lisible.
// truncated : des lignes ou des octets de la demande n'ont pas été gardés ; l'humain ne l'a pas vue en entier.
func permText(screen string) (agent, text string, truncated bool) {
	lines := strings.Split(strings.TrimRight(screen, " \n"), "\n")
	if len(lines) > permScanLines {
		lines = lines[len(lines)-permScanLines:]
	}
	from := len(lines) - permTailLines
	if from < 0 {
		from = 0
	}
	titled := false
	for lineIndex := len(lines) - 1; lineIndex >= 0; lineIndex-- {
		if match := permTitleRe.FindStringSubmatch(lines[lineIndex]); match != nil {
			agent, from, titled = match[1], lineIndex, true
			break
		}
	}
	truncated = !titled && strings.TrimSpace(strings.Join(lines[:from], "")) != ""
	text = strings.TrimRight(strings.Join(lines[from:], "\n"), " \n")
	if len(text) > permCut {
		truncated = true
		text = text[:permCut]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return agent, text, truncated
}

// states : état de chaque agent par nom, et le texte d'une demande de permission en attente.
func (teamContext teamCtx) states() (map[string]string, string, bool) {
	states := map[string]string{}
	askingAgent, permissionText, truncated, asking, captured := teamContext.permission()
	idle := false
	for _, sessionID := range teamPanes.Idle() {
		if sessionID == teamContext.tf.LeadSessionID {
			idle = true
		}
	}
	for _, member := range teamContext.tf.Members {
		switch {
		case member.Lead && teamContext.leadPane == "":
			states[member.Name] = "unknown"
		case member.Lead && !captured:
			states[member.Name] = "unknown"
		case member.Lead && asking:
			states[member.Name] = "waiting"
		case member.Lead && idle:
			states[member.Name] = "idle"
		case member.Lead:
			states[member.Name] = "working"
		case member.Pane == "":
			states[member.Name] = "unknown"
		case member.Name == askingAgent:
			states[member.Name] = "waiting"
		case member.Active:
			states[member.Name] = "working"
		default:
			states[member.Name] = "idle"
		}
	}
	return states, permissionText, truncated
}

func paneMu(pane string) *sync.Mutex {
	stored, _ := paneMus.LoadOrStore(pane, &sync.Mutex{})
	return stored.(*sync.Mutex)
}

var errAsking = terr("srv.teams_run.dialogueEnCours")

// deliver recontrôle l'écran du pane sous son verrou, juste avant le collage : un dialogue apparu depuis le relevé d'état serait
// validé par l'Entrée du collage. errAsking : rien n'est parti, l'appelant met en file.
func deliver(cfgRoot, team, pane string, message queuedMsg) error {
	paneLock := paneMu(pane)
	paneLock.Lock()
	screen, captureErr := teamPanes.Capture(pane)
	if captureErr != nil || screenAsks(screen) {
		paneLock.Unlock()
		return errAsking
	}
	err := teamPanes.Paste(pane, message.Text)
	paneLock.Unlock()
	if err != nil {
		return terr("srv.teams_run.envoiImpossible", "W1", err)
	}
	_ = journalAppend(cfgRoot, team, DirectMsg{Author: message.Author, To: message.To, Text: message.Text}) // journal plein : le message est parti quand même
	return nil
}

// flush envoie, dans l'ordre, le premier message en file de chaque agent redevenu disponible.
func (teamContext teamCtx) flush(cfgRoot string, states map[string]string) {
	teamRunMu.Lock()
	defer teamRunMu.Unlock()
	queue := teamQueue[teamContext.tf.Name]
	if len(queue) == 0 {
		return
	}
	first := map[string]bool{}
	var keep []queuedMsg
	for _, message := range queue {
		if first[message.To] {
			keep = append(keep, message)
			continue
		}
		first[message.To] = true
		pane, found := teamContext.target(message.To)
		if found && pane != "" && states[message.To] == "idle" && deliver(cfgRoot, teamContext.tf.Name, pane, message) == nil {
			continue
		}
		keep = append(keep, message)
	}
	teamQueue[teamContext.tf.Name] = keep
}

// identify : prénom d'affichage, identifiant et libellé de rôle d'un membre, d'après le roster (prénom -> slug). Le nom brut reste la clé des envois.
func identify(catalog Catalog, roster map[string]string, member TeamMember) (display, agentID, role string) {
	byID := func(wantedID string) (Agent, bool) {
		for _, agent := range catalog.Agents {
			if agent.ID == wantedID {
				return agent, true
			}
		}
		return Agent{}, false
	}
	if member.Lead {
		if lead, found := byID("chef"); found {
			return lead.Name, member.AgentID, ""
		}
		return upFirst(member.Name), member.AgentID, ""
	}
	slug, known := roster[member.Name]
	if !known {
		return upFirst(member.Name), member.AgentID, ""
	}
	roleAgent, _ := byID(slug)
	return upFirst(member.Name), slug, roleAgent.Name
}

func upFirst(text string) string {
	if text == "" {
		return ""
	}
	firstRune, firstRuneSize := utf8.DecodeRuneInString(text)
	return string(unicode.ToUpper(firstRune)) + text[firstRuneSize:]
}

func teamView(cfgRoot, name string) (TeamView, error) {
	teamContext, err := teamLoad(cfgRoot, name)
	if err != nil {
		return TeamView{}, err
	}
	states, permission, truncated := teamContext.states()
	teamContext.flush(cfgRoot, states)
	catalog, _ := loadCatalog(cfgRoot)
	roster, _ := rosterRead(cfgRoot, name)
	view := TeamView{Name: name, LeadSessionID: teamContext.tf.LeadSessionID, Live: teamContext.leadPane != "", Tasks: teamContext.tf.Tasks,
		Permission: permission, PermissionTruncated: truncated, Unknown: teamContext.tf.Unknown}
	for _, member := range teamContext.tf.Members {
		pane := member.Pane
		if member.Lead {
			pane = teamContext.leadPane
		}
		display, agentID, role := identify(catalog, roster, member)
		view.Agents = append(view.Agents, AgentView{Name: member.Name, Display: display, AgentID: agentID, Role: role, Pane: pane, State: states[member.Name], Lead: member.Lead})
	}
	teamRunMu.Lock()
	view.Messages = append([]InboxMsg(nil), teamLog[name]...)
	view.Queued = len(teamQueue[name])
	teamRunMu.Unlock()
	view.Direct, _ = journalRead(cfgRoot, name)
	return view, nil
}

func teamList(cfgRoot string) []TeamBrief {
	panes, _ := teamPanes.Panes()
	briefs := []TeamBrief{}
	for _, teamName := range listTeams(cfgRoot) {
		teamFiles, err := readTeam(cfgRoot, teamName)
		if err != nil || panes[teamFiles.LeadSessionID] == "" {
			continue
		}
		briefs = append(briefs, TeamBrief{Name: teamName, LeadSessionID: teamFiles.LeadSessionID, Live: true, Members: len(teamFiles.Members)})
	}
	return briefs
}

func teamSend(cfgRoot, name, author, to, text string) (bool, error) {
	if !teamNameRe.MatchString(name) {
		return false, terr("srv.teams_run.nomInvalide")
	}
	if checkErr := checkSendText(text); checkErr != nil {
		return false, checkErr
	}
	teamContext, err := teamLoad(cfgRoot, name)
	if err != nil {
		return false, err
	}
	if liveErr := teamContext.needLive(); liveErr != nil {
		return false, liveErr
	}
	pane, found := teamContext.target(to)
	if !found {
		return false, terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return false, terr("srv.teams_run.paneAbsent")
	}
	states, _, _ := teamContext.states()
	message := queuedMsg{author, to, text}
	teamRunMu.Lock()
	pending := false
	for _, queued := range teamQueue[name] {
		pending = pending || queued.To == to // l'ordre d'envoi est conservé
	}
	teamRunMu.Unlock()
	if states[to] == "idle" && !pending {
		if deliverErr := deliver(cfgRoot, name, pane, message); deliverErr != errAsking {
			return false, deliverErr
		}
	}
	teamRunMu.Lock()
	defer teamRunMu.Unlock()
	if len(teamQueue[name]) >= teamQueueMax {
		return false, terr("srv.teams_run.fileOuverte")
	}
	teamQueue[name] = append(teamQueue[name], message)
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
	if _, isAllowed := allowedKeys[key]; !isAllowed {
		return terr("srv.sendkeys.toucheNonPermise")
	}
	teamContext, err := teamLoad(cfgRoot, name)
	if err != nil {
		return err
	}
	if liveErr := teamContext.needLive(); liveErr != nil {
		return liveErr
	}
	pane, found := teamContext.target(agent)
	if !found {
		return terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return terr("srv.teams_run.paneAbsent")
	}
	paneLock := paneMu(pane)
	paneLock.Lock()
	defer paneLock.Unlock()
	screen, captureErr := teamPanes.Capture(pane)
	if captureErr != nil || !screenAsks(screen) {
		return terr("srv.teams_run.aucunDialogue")
	}
	_, fresh, truncated := permText(screen)
	if truncated && !declineKeys[key] {
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
	teamContext, err := teamLoad(cfgRoot, name)
	if err != nil {
		return "", err
	}
	if liveErr := teamContext.needLive(); liveErr != nil {
		return "", liveErr
	}
	pane, found := teamContext.target(agent)
	if !found {
		return "", terr("srv.teams_run.agentInconnu")
	}
	if pane == "" {
		return "", terr("srv.teams_run.paneAbsent")
	}
	screen, err := teamPanes.Capture(pane)
	if err != nil {
		return "", terr("srv.teams_run.captureImpossible", "W1", err)
	}
	lines := strings.Split(strings.TrimRight(screen, " \n"), "\n")
	if len(lines) > teamScreenLines {
		lines = lines[len(lines)-teamScreenLines:]
	}
	return strings.Join(lines, "\n"), nil
}

func teamClose(cfgRoot, name string) error {
	if !teamNameRe.MatchString(name) {
		return terr("srv.teams_run.nomInvalide")
	}
	teamFiles, err := readTeam(cfgRoot, name)
	if err != nil {
		return err
	}
	// un seul relevé frais décide ; Panes() le réutilise (cache de 1,5 s). Illisible : on ne touche à rien.
	leadAlive, known := teamPanes.LeadState(teamFiles.LeadSessionID)
	if !known {
		return terr("srv.teams_run.etatInconnu")
	}
	panes, panesErr := teamPanes.Panes()
	// serveur tmux disparu : plus aucun panneau
	if panesErr != nil && !isMsg(panesErr, "srv.sendkeys.aucuneSessionNA") {
		return terr("srv.teams_run.tmuxInutilisable", "W1", panesErr)
	}
	if panes[teamFiles.LeadSessionID] != "" {
		if closeErr := teamPanes.Close(teamFiles.LeadSessionID); closeErr != nil {
			return terr("srv.teams_run.fermetureImpossible", "W1", closeErr)
		}
	} else if leadAlive {
		return terr("srv.teams_run.chefOuvertSansTerminal")
	}
	// team terminée (le chef a quitté) : plus de session à fermer, on nettoie quand même
	if journalErr := journalDrop(cfgRoot, name); journalErr != nil {
		return journalErr
	}
	if rosterErr := rosterDrop(cfgRoot, name); rosterErr != nil {
		return rosterErr
	}
	teamRunMu.Lock()
	delete(teamLog, name)
	delete(teamQueue, name)
	teamRunMu.Unlock()
	return nil
}
