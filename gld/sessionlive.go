package main

// État en direct d'une session ouverte : ce que Claude fait (réfléchit, exécute un outil, attend), la taille du contexte, les
// sous-agents. Tout vient de fichiers locaux : ~/.claude/sessions/<pid>.json (statut tenu à jour par Claude Code), la fin du
// transcript et les fichiers du dossier subagents/. Rien n'est envoyé à Claude.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SubLive struct {
	ID       string  `json:"id"`
	Desc     string  `json:"desc"`
	Type     string  `json:"type"`
	Running  bool    `json:"running"`
	Activity string  `json:"activity"`
	Context  int     `json:"context"` // taille du contexte à son dernier tour
	Out      int     `json:"out"`     // tokens produits en tout
	Mtime    float64 `json:"mtime"`
}

type LiveState struct {
	Said        string    `json:"said,omitempty"` // session en attente : dernier paragraphe de la réponse de Claude (souvent la question posée)
	Open        bool      `json:"open"`
	Status      string    `json:"status"`   // busy, idle, "" (inconnu)
	Activity    string    `json:"activity"` // en une phrase
	Thinking    bool      `json:"thinking"` // Claude travaille (réflexion ou outil)
	Generic     bool      `json:"generic"`  // l'activité est un état générique (réflexion, travail) : l'interface affiche un verbe à la place
	Exec        bool      `json:"exec"`     // l'activité est l'exécution d'un outil
	Since       float64   `json:"since"`    // dernier événement du transcript (secondes epoch)
	Context     int       `json:"context"`  // tokens du dernier appel : entrée + cache lu + cache écrit
	Out         int       `json:"out"`      // tokens produits au dernier tour
	Model       string    `json:"model"`
	Tool        string    `json:"tool"`        // dernier outil appelé (« Bash : go test »)
	ToolRunning bool      `json:"toolRunning"` // son résultat n'est pas encore arrivé
	Subs        []SubLive `json:"subs"`
	Tasks       []BgTask  `json:"tasks"`
	Verbs       []string  `json:"verbs"`   // verbes affichés à tour de rôle pendant la réflexion
	Queue       []Queued  `json:"queue"`   // messages envoyés pendant que Claude travaillait, pas encore lus par la session
	Pending     []QItem   `json:"pending"` // file serveur : messages en attente que la session soit libre
}

const (
	teamShortIDLength       = 8
	queuedMessageMaxLength  = 400
	serviceMessageMaxLength = 64000
	quickPickupThreshold    = 5 * time.Millisecond
	servicePickupThreshold  = time.Second
	spinnerVerbMaxLength    = 60
	toolBriefMaxLength      = 90
	toolFullMaxLength       = 4000
	activityTailBytes       = 512 << 10
	lastSaidTailBytes       = 256 << 10
	lastSaidMaxLength       = 200
	subagentRecentWindow    = 10 * time.Minute
	subagentListMax         = 60
)

type Queued struct {
	T  string `json:"t"`
	Ts string `json:"ts"`
}

// pendingQueue : les messages mis en file d'attente par Claude Code (« queue-operation » du transcript) et pas encore pris
// en charge. « dequeue » retire le plus ancien ; « remove » (l'utilisateur reprend son message) retire celui du même texte.
// La file se reconstruit depuis le début du transcript (un enqueue ancien, hors d'une fenêtre de fin, ferait retirer au dequeue
// suivant le mauvais message) ; seules les lignes ajoutées depuis le passage précédent sont lues.
// ponytail: un état par transcript relevé, jamais oublié ; borner si le nombre de sessions suivies devient grand.
type pqState struct {
	off int64
	q   []Queued
}

var (
	pqMu   sync.Mutex
	pqSeen = map[string]pqState{}
)

func pendingQueue(path string) []Queued {
	pqMu.Lock()
	defer pqMu.Unlock()
	transcriptHandle, err := openTrusted(path)
	if err != nil {
		delete(pqSeen, path)
		return []Queued{}
	}
	defer transcriptHandle.Close()
	queueState := pqSeen[path]
	if fileInfo, statErr := transcriptHandle.Stat(); statErr != nil || fileInfo.Size() < queueState.off { // raccourci : relu en entier
		queueState = pqState{}
	}
	if _, seekErr := transcriptHandle.Seek(queueState.off, io.SeekStart); seekErr != nil {
		return []Queued{}
	}
	reader := bufio.NewReaderSize(transcriptHandle, jsonLineBufferSize)
	for {
		transcriptLine, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			break // ligne incomplète : relue au prochain passage
		}
		queueState.off += int64(len(transcriptLine))
		if bytes.Contains(transcriptLine, []byte(`"queue-operation"`)) {
			queueState.q = applyQueueOp(queueState.q, transcriptLine)
		}
	}
	pqSeen[path] = queueState
	visible := []Queued{}
	for _, queued := range queueState.q {
		text := strings.TrimSpace(queued.T)
		isTaskNotification := strings.HasPrefix(text, "<task-notification") // les notifications de tâche ne sont pas des messages
		if text == "" || isTaskNotification {
			continue
		}
		// Un message balisé (agent, session) garde sa balise de fermeture : l'UI en fait une carte. Plafond large : relevé toutes les 2 s.
		if serviceMsg.MatchString(text) {
			text = cut(text, serviceMessageMaxLength)
		} else {
			text = cut(text, queuedMessageMaxLength)
		}
		visible = append(visible, Queued{text, queued.Ts})
	}
	return visible
}

// serviceMsg : messages de service d'agents (balises connues du transcript), à ne pas confondre avec un prompt qui commencerait par « < ».
var serviceMsg = regexp.MustCompile(`^<(?:agent-message|cross-session-message|teammate-message)[\s>]`)

func parseTs(timestamp string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, timestamp)
	return parsed
}

func applyQueueOp(queue []Queued, transcriptLine []byte) []Queued {
	var operation struct {
		Type, Operation, Timestamp, Content string
	}
	if json.Unmarshal(transcriptLine, &operation) != nil || operation.Type != "queue-operation" {
		return queue
	}
	switch operation.Operation {
	case "enqueue":
		queue = append(queue, Queued{T: operation.Content, Ts: operation.Timestamp})
	case "dequeue":
		if queueLength := len(queue); queueLength > 0 {
			// Un dequeue collé à son enqueue (< 5 ms) retire celui-ci : Claude Code prend le message aussitôt, sans file. Retirer le plus
			// ancien laissait un résidu, puis chaque paire suivante retirait ce résidu et laissait le dernier prompt affiché à jamais.
			// Un message de service (hand-back de sous-agent) est repris 12 à 14 ms après son enqueue : seuil à la seconde pour lui.
			enqueuedAt, dequeuedAt := parseTs(queue[queueLength-1].Ts), parseTs(operation.Timestamp)
			pickupThreshold := quickPickupThreshold
			if serviceMsg.MatchString(strings.TrimSpace(queue[queueLength-1].T)) {
				pickupThreshold = servicePickupThreshold
			}
			bothTimestampsKnown := !enqueuedAt.IsZero() && !dequeuedAt.IsZero()
			pickupDelay := dequeuedAt.Sub(enqueuedAt)
			isImmediatePickup := bothTimestampsKnown && pickupDelay >= 0 && pickupDelay < pickupThreshold
			if isImmediatePickup {
				return queue[:queueLength-1]
			}
			queue = queue[1:]
		}
	case "remove":
		for queueIndex := len(queue) - 1; queueIndex >= 0; queueIndex-- {
			if operation.Content == "" || queue[queueIndex].T == operation.Content {
				queue = append(queue[:queueIndex], queue[queueIndex+1:]...)
				break
			}
		}
	}
	return queue
}

// Les états d'activité restent en français dans le code (comparaisons, tests) ; localizeActivity les traduit à la sortie.
var activityIDs = map[string]string{
	"a terminé sa réponse":           "srv.live.finished",
	"réfléchit":                      "srv.live.thinking",
	"réfléchit au résultat":          "srv.live.result",
	"lit votre message et réfléchit": "srv.live.reading",
	"attend votre message":           "srv.live.waiting",
	"travaille":                      "srv.live.working",
	"terminé":                        "srv.live.done",
}

func isGenericActivity(activity string) bool {
	switch activity {
	case "", "réfléchit", "travaille", "réfléchit au résultat", "lit votre message et réfléchit":
		return true
	}
	return false
}

func localizeActivity(activity string) string {
	if messageID, isKnown := activityIDs[activity]; isKnown {
		return L(messageID)
	}
	if toolText, isExecution := strings.CutPrefix(activity, "exécute "); isExecution {
		return L("srv.live.exec", "A1", toolText)
	}
	return activity
}

// spinnerVerbs : la liste par défaut, ou celle du réglage « spinnerVerbs » de Claude Code (mode « replace » : seulement la vôtre ;
// « append » : à la suite de la liste par défaut).
// defaultVerbs : la liste embarquée de la langue (locales/verbs.<langue>.json).
func defaultVerbs(lang string) []string {
	verbsJSON, err := localesFS.ReadFile("locales/verbs." + lang + ".json")
	var verbs []string
	if err != nil || json.Unmarshal(verbsJSON, &verbs) != nil {
		return nil
	}
	return verbs
}

// verbsCache : GET /v1/sessions/{id}/live est relu toutes les 2 s par le navigateur ; la liste ne change qu'avec settings.json ou la langue.
var verbsCache struct {
	sync.Mutex
	key   string
	verbs []string
}

func spinnerVerbs(lang string) []string {
	settings := filepath.Join(cfgDir(), "settings.json")
	key := lang + "\x00" + settings
	if settingsInfo, err := os.Stat(settings); err == nil {
		key += "\x00" + strconv.FormatInt(settingsInfo.ModTime().UnixNano(), 10) + "\x00" + strconv.FormatInt(settingsInfo.Size(), 10)
	}
	verbsCache.Lock()
	defer verbsCache.Unlock()
	if verbsCache.key != key {
		verbsCache.key, verbsCache.verbs = key, readSpinnerVerbs(lang, settings)
	}
	return verbsCache.verbs
}

func readSpinnerVerbs(lang, settings string) []string {
	thinkingVerbs := defaultVerbs(lang)
	var settingsFile struct {
		SpinnerVerbs struct {
			Mode  string   `json:"mode"`
			Verbs []string `json:"verbs"`
		} `json:"spinnerVerbs"`
	}
	settingsContent, err := readTrusted(settings)
	if err != nil || json.Unmarshal(settingsContent, &settingsFile) != nil || len(settingsFile.SpinnerVerbs.Verbs) == 0 {
		return thinkingVerbs
	}
	var mine []string
	for _, verb := range settingsFile.SpinnerVerbs.Verbs {
		if verb = strings.TrimSpace(verb); verb != "" && len(verb) <= spinnerVerbMaxLength {
			mine = append(mine, verb)
		}
	}
	if len(mine) == 0 {
		return thinkingVerbs
	}
	if settingsFile.SpinnerVerbs.Mode == "replace" {
		return mine
	}
	return append(append([]string{}, thinkingVerbs...), mine...)
}

// sessionStatus : le statut d'une session ouverte d'après ~/.claude/sessions/*.json (processus encore vivant).
func sessionStatus(id string) (status string, isOpen bool) {
	files, _ := filepath.Glob(filepath.Join(cfgDir(), "sessions", "*.json"))
	for _, sessionFile := range files {
		content, err := readTrusted(sessionFile)
		if err != nil {
			continue
		}
		var sessionRecord struct {
			Pid       int    `json:"pid"`
			SessionId string `json:"sessionId"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(content, &sessionRecord) != nil || sessionRecord.SessionId != id || sessionRecord.Pid <= 0 {
			continue
		}
		if _, statErr := os.Stat("/proc/" + strconv.Itoa(sessionRecord.Pid)); statErr != nil && pidProbe {
			continue
		}
		return sessionRecord.Status, true
	}
	return "", false
}

// rcSessions : ids des sessions ouvertes reliées à claude.ai (remote control), d'après bridgeSessionId dans ~/.claude/sessions/*.json.
func rcSessions() []string {
	sessionIDs := []string{}
	files, _ := filepath.Glob(filepath.Join(cfgDir(), "sessions", "*.json"))
	for _, sessionFile := range files {
		content, err := readTrusted(sessionFile)
		if err != nil {
			continue
		}
		var sessionRecord struct {
			Pid       int    `json:"pid"`
			SessionId string `json:"sessionId"`
			Bridge    string `json:"bridgeSessionId"`
		}
		if json.Unmarshal(content, &sessionRecord) != nil || sessionRecord.SessionId == "" || sessionRecord.Bridge == "" || sessionRecord.Pid <= 0 {
			continue
		}
		if _, statErr := os.Stat("/proc/" + strconv.Itoa(sessionRecord.Pid)); statErr != nil && pidProbe {
			continue
		}
		sessionIDs = append(sessionIDs, sessionRecord.SessionId)
	}
	return sessionIDs
}

// pidProbe : sous Linux on vérifie que le processus vit (/proc) ; ailleurs on s'en remet au fichier (remplaçable dans les tests).
var pidProbe = func() bool { _, err := os.Stat("/proc/self"); return err == nil }()

type usage struct {
	In, Out, CacheRead, CacheWrite int
}

func (counts usage) context() int { return counts.In + counts.CacheRead + counts.CacheWrite }

// toolBrief : de quoi reconnaître un appel d'outil en quelques mots (commande, fichier, motif, description).
func toolBrief(name string, input map[string]any) string {
	for _, key := range []string{"description", "command", "file_path", "path", "pattern", "url", "query", "prompt", "subject"} {
		if value, isText := input[key].(string); isText && strings.TrimSpace(value) != "" {
			if key == "command" { // un script multiligne (heredoc) : sa première ligne dit déjà de quoi il s'agit
				value = strings.TrimSpace(value)
				value, _, _ = strings.Cut(value, "\n")
			}
			return name + " : " + cut(strings.Join(strings.Fields(value), " "), toolBriefMaxLength)
		}
	}
	return name
}

// toolFull : l'appel d'outil en entier (commande complète, plusieurs lignes comprises), pour l'affichage du dernier outil.
func toolFull(name string, input map[string]any) string {
	for _, key := range []string{"command", "description", "file_path", "path", "pattern", "url", "query", "prompt", "subject"} {
		if value, isText := input[key].(string); isText && strings.TrimSpace(value) != "" {
			return name + " : " + cut(strings.TrimSpace(value), toolFullMaxLength)
		}
	}
	return name
}

// readActivity lit la fin d'un transcript : ce que Claude fait d'après le dernier événement utile.
func readActivity(path string) (activity string, thinking bool, since float64, tokenUsage usage, model string, tool string, toolRunning bool) {
	lines := tailLines(path, activityTailBytes)
	pendingTool := map[string]string{} // tool_use_id -> résumé, tant qu'aucun résultat n'est arrivé
	var order []string
	for _, transcriptLine := range lines {
		var event struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Model      string          `json:"model"`
				StopReason string          `json:"stop_reason"`
				Content    json.RawMessage `json:"content"`
				Usage      *struct {
					In    int `json:"input_tokens"`
					Out   int `json:"output_tokens"`
					CRead int `json:"cache_read_input_tokens"`
					CWr   int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(transcriptLine, &event) != nil || (event.Type != "assistant" && event.Type != "user") {
			continue
		}
		if eventTime, err := time.Parse(time.RFC3339Nano, event.Timestamp); err == nil {
			since = float64(eventTime.UnixNano()) / nanosecondsPerSecond
		}
		var blocks []struct {
			Type      string         `json:"type"`
			ID        string         `json:"id"`
			Name      string         `json:"name"`
			Input     map[string]any `json:"input"`
			ToolUseID string         `json:"tool_use_id"`
			Text      string         `json:"text"`
		}
		json.Unmarshal(event.Message.Content, &blocks) // une chaîne simple donne une liste vide : un message de l'utilisateur
		switch event.Type {
		case "assistant":
			if eventUsage := event.Message.Usage; eventUsage != nil {
				tokenUsage = usage{eventUsage.In, eventUsage.Out, eventUsage.CRead, eventUsage.CWr}
			}
			if event.Message.Model != "" && !strings.HasPrefix(event.Message.Model, "<") {
				model = event.Message.Model
			}
			last := ""
			for _, block := range blocks {
				switch block.Type {
				case "tool_use":
					pendingTool[block.ID] = toolBrief(block.Name, block.Input)
					tool, toolRunning = toolFull(block.Name, block.Input), true
					order = append(order, block.ID)
					last = "tool"
				case "thinking":
					last = "thinking"
				case "text":
					last = "text"
				}
			}
			switch {
			case last == "tool":
				thinking = true
			case last == "text" && event.Message.StopReason == "end_turn":
				activity, thinking = "a terminé sa réponse", false
			default:
				activity, thinking = "réfléchit", true
			}
		case "user":
			isResult := false
			for _, block := range blocks {
				if block.Type == "tool_result" {
					if len(order) > 0 && order[len(order)-1] == block.ToolUseID {
						toolRunning = false
					}
					delete(pendingTool, block.ToolUseID)
					isResult = true
				}
			}
			if isResult {
				activity, thinking = "réfléchit au résultat", true
			} else {
				activity, thinking = "lit votre message et réfléchit", true
			}
		}
	}
	for orderIndex := len(order) - 1; orderIndex >= 0; orderIndex-- { // l'appel d'outil le plus récent encore sans résultat
		if summary, isPending := pendingTool[order[orderIndex]]; isPending {
			return "exécute " + summary, true, since, tokenUsage, model, tool, toolRunning
		}
	}
	return activity, thinking, since, tokenUsage, model, tool, toolRunning
}

func sessionLive(dir, sessionID string) LiveState {
	state := LiveState{Subs: []SubLive{}, Tasks: []BgTask{}, Verbs: spinnerVerbs(uiLang()), Queue: []Queued{}, Pending: []QItem{}}
	if queueSt != nil {
		state.Pending = queueSt.List(sessionID)
	}
	state.Status, state.Open = sessionStatus(sessionID)
	transcriptPath, found := transcript(dir, sessionID)
	if !found {
		return state
	}
	var tokenUsage usage
	state.Activity, state.Thinking, state.Since, tokenUsage, state.Model, state.Tool, state.ToolRunning = readActivity(transcriptPath)
	state.Context, state.Out = tokenUsage.context(), tokenUsage.Out
	if state.Open && state.Status == "idle" {
		state.Activity, state.Thinking = "attend votre message", false
		state.Said = lastSaid(transcriptPath)
	} else if state.Open && state.Status == "busy" {
		state.Thinking = true
		if state.Activity == "" || state.Activity == "a terminé sa réponse" {
			state.Activity = "travaille"
		}
	}
	if !state.Open {
		state.Thinking = false
	}
	state.Generic, state.Exec = isGenericActivity(state.Activity), strings.HasPrefix(state.Activity, "exécute ")
	state.Activity = localizeActivity(state.Activity)
	if state.Open {
		state.Queue = pendingQueue(transcriptPath)
	}
	state.Subs = append(subLives(filepath.Join(filepath.Dir(transcriptPath), sessionID, "subagents")), teammateLives(sessionID)...)
	if tasks := bgTasks(transcriptPath, state.Open); tasks != nil {
		state.Tasks = tasks
	}
	return state
}

// subLives : les sous-agents d'une session, les plus récents d'abord (60 au plus).
func subLives(dir string) []SubLive {
	subagents := []SubLive{}
	metas, _ := filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
	for _, metaPath := range metas {
		agentID := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(metaPath), "agent-"), ".meta.json")
		var meta struct {
			AgentType   string `json:"agentType"`
			Description string `json:"description"`
			Finished    bool   `json:"finished"`
		}
		if metaContent, readErr := readTrusted(metaPath); readErr != nil || json.Unmarshal(metaContent, &meta) != nil {
			continue
		}
		transcriptPath := filepath.Join(dir, "agent-"+agentID+".jsonl")
		fileInfo, err := os.Stat(transcriptPath)
		if err != nil {
			continue
		}
		activity, thinking, _, tokenUsage, _, _, _ := readActivity(transcriptPath)
		subagent := SubLive{ID: agentID, Desc: meta.Description, Type: meta.AgentType, Activity: activity, Context: tokenUsage.context(), Out: tokenUsage.Out, Mtime: float64(fileInfo.ModTime().UnixNano()) / nanosecondsPerSecond}
		// Un sous-agent « fini » d'après son fichier meta ne travaille plus ; sans cette marque, il travaille s'il a écrit récemment.
		subagent.Running = !meta.Finished && thinking && time.Since(fileInfo.ModTime()) < subagentRecentWindow
		if !subagent.Running && subagent.Activity == "" {
			subagent.Activity = "terminé"
		}
		if !subagent.Running && meta.Finished {
			subagent.Activity = "terminé"
		}
		subagent.Activity = localizeActivity(subagent.Activity)
		subagents = append(subagents, subagent)
	}
	sort.Slice(subagents, func(left, right int) bool {
		if subagents[left].Running != subagents[right].Running {
			return subagents[left].Running
		}
		return subagents[left].Mtime > subagents[right].Mtime
	})
	if len(subagents) > subagentListMax {
		subagents = subagents[:subagentListMax]
	}
	return subagents
}

// teammateLives : les coéquipiers in-process de l'équipe « session-<8 premiers caractères> » (hors chef), sans transcript lisible.
func teammateLives(sessionID string) []SubLive {
	if len(sessionID) < teamShortIDLength {
		return nil
	}
	configPath := filepath.Join(cfgDir(), "teams", "session-"+sessionID[:teamShortIDLength], "config.json")
	configContent, readErr := readTrusted(configPath)
	if readErr != nil {
		return nil
	}
	var team struct {
		Members []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"members"`
	}
	if json.Unmarshal(configContent, &team) != nil {
		return nil
	}
	teammates := []SubLive{}
	for _, member := range team.Members {
		if member.Name == "" || member.Name == "team-lead" {
			continue
		}
		teammates = append(teammates, SubLive{ID: "team-" + member.Name, Desc: member.Name, Type: member.Model, Activity: localizeActivity("terminé")})
	}
	return teammates
}

// lastSaid : le dernier paragraphe du dernier texte écrit par Claude (200 caractères au plus), "" s'il n'y en a pas.
func lastSaid(path string) string {
	lines := tailLines(path, lastSaidTailBytes)
	for lineIndex := len(lines) - 1; lineIndex >= 0; lineIndex-- {
		var event struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[lineIndex], &event) != nil || event.Type != "assistant" {
			continue
		}
		var blocks []struct{ Type, Text string }
		if json.Unmarshal(event.Message.Content, &blocks) != nil {
			continue
		}
		for blockIndex := len(blocks) - 1; blockIndex >= 0; blockIndex-- {
			if text := strings.TrimSpace(blocks[blockIndex].Text); blocks[blockIndex].Type == "text" && text != "" {
				paras := strings.Split(text, "\n\n")
				return cut(spaceRe.ReplaceAllString(strings.TrimSpace(paras[len(paras)-1]), " "), lastSaidMaxLength)
			}
		}
		return "" // le dernier message de Claude n'a pas de texte (outil seul)
	}
	return ""
}
