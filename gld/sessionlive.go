package main

// État en direct d'une session ouverte : ce que Claude fait (réfléchit, exécute un outil, attend), la taille du contexte, les
// sous-agents. Tout vient de fichiers locaux : ~/.claude/sessions/<pid>.json (statut tenu à jour par Claude Code), la fin du
// transcript et les fichiers du dossier subagents/. Rien n'est envoyé à Claude.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	Open     bool      `json:"open"`
	Status   string    `json:"status"`   // busy, idle, "" (inconnu)
	Activity string    `json:"activity"` // en une phrase
	Thinking bool      `json:"thinking"` // Claude travaille (réflexion ou outil)
	Since    float64   `json:"since"`    // dernier événement du transcript (secondes epoch)
	Context  int       `json:"context"`  // tokens du dernier appel : entrée + cache lu + cache écrit
	Out      int       `json:"out"`      // tokens produits au dernier tour
	Model    string    `json:"model"`
	Subs     []SubLive `json:"subs"`
	Verbs    []string  `json:"verbs"` // verbes affichés à tour de rôle pendant la réflexion
	Queue    []Queued  `json:"queue"` // messages envoyés pendant que Claude travaillait, pas encore lus par la session
}

type Queued struct {
	T  string `json:"t"`
	Ts string `json:"ts"`
}

// pendingQueue : les messages mis en file d'attente par Claude Code (« queue-operation » du transcript) et pas encore pris
// en charge. « dequeue » retire le plus ancien ; « remove » (l'utilisateur reprend son message) retire celui du même texte.
func pendingQueue(path string) []Queued {
	var q []Queued
	for _, l := range tailLines(path, 512<<10) {
		if !bytes.Contains(l, []byte(`"queue-operation"`)) {
			continue
		}
		var o struct {
			Type, Operation, Timestamp, Content string
		}
		if json.Unmarshal(l, &o) != nil || o.Type != "queue-operation" {
			continue
		}
		switch o.Operation {
		case "enqueue":
			q = append(q, Queued{T: o.Content, Ts: o.Timestamp})
		case "dequeue":
			if len(q) > 0 {
				q = q[1:]
			}
		case "remove":
			for i := len(q) - 1; i >= 0; i-- {
				if o.Content == "" || q[i].T == o.Content {
					q = append(q[:i], q[i+1:]...)
					break
				}
			}
		}
	}
	out := []Queued{}
	for _, m := range q {
		if t := strings.TrimSpace(m.T); t != "" && !strings.HasPrefix(t, "<task-notification") { // les notifications de tâche ne sont pas des messages
			out = append(out, Queued{cut(t, 400), m.Ts})
		}
	}
	return out
}

var thinkingVerbs = []string{"réfléchit", "cogite", "médite", "mijote", "rumine", "planche", "phosphore", "gamberge", "se concentre", "décortique", "jongle avec les idées", "pèse le pour et le contre", "fait chauffer les neurones", "creuse la question", "tisse sa réponse", "cherche le bon angle", "démêle l'écheveau", "met de l'ordre dans ses idées", "ourdit un plan", "contemple le problème"}

// spinnerVerbs : la liste par défaut, ou celle du réglage « spinnerVerbs » de Claude Code (mode « replace » : seulement la vôtre ;
// « append » : à la suite de la liste par défaut).
func spinnerVerbs() []string {
	var d struct {
		SpinnerVerbs struct {
			Mode  string   `json:"mode"`
			Verbs []string `json:"verbs"`
		} `json:"spinnerVerbs"`
	}
	if b, err := os.ReadFile(filepath.Join(cfgDir(), "settings.json")); err != nil || json.Unmarshal(b, &d) != nil || len(d.SpinnerVerbs.Verbs) == 0 {
		return thinkingVerbs
	}
	var mine []string
	for _, v := range d.SpinnerVerbs.Verbs {
		if v = strings.TrimSpace(v); v != "" && len(v) <= 60 {
			mine = append(mine, v)
		}
	}
	if len(mine) == 0 {
		return thinkingVerbs
	}
	if d.SpinnerVerbs.Mode == "replace" {
		return mine
	}
	return append(append([]string{}, thinkingVerbs...), mine...)
}

// sessionStatus : le statut d'une session ouverte d'après ~/.claude/sessions/*.json (processus encore vivant).
func sessionStatus(id string) (status string, open bool) {
	files, _ := filepath.Glob(filepath.Join(cfgDir(), "sessions", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			Pid       int    `json:"pid"`
			SessionId string `json:"sessionId"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(b, &s) != nil || s.SessionId != id || s.Pid <= 0 {
			continue
		}
		if _, err := os.Stat("/proc/" + strconv.Itoa(s.Pid)); err != nil && pidProbe {
			continue
		}
		return s.Status, true
	}
	return "", false
}

// pidProbe : sous Linux on vérifie que le processus vit (/proc) ; ailleurs on s'en remet au fichier (remplaçable dans les tests).
var pidProbe = func() bool { _, err := os.Stat("/proc/self"); return err == nil }()

type usage struct {
	In, Out, CacheRead, CacheWrite int
}

func (u usage) context() int { return u.In + u.CacheRead + u.CacheWrite }

// toolBrief : de quoi reconnaître un appel d'outil en quelques mots (commande, fichier, motif, description).
func toolBrief(name string, in map[string]any) string {
	for _, k := range []string{"description", "command", "file_path", "path", "pattern", "url", "query", "prompt", "subject"} {
		if s, ok := in[k].(string); ok && strings.TrimSpace(s) != "" {
			if k == "command" { // un script multiligne (heredoc) : sa première ligne dit déjà de quoi il s'agit
				s = strings.TrimSpace(s)
				s, _, _ = strings.Cut(s, "\n")
			}
			return name + " : " + cut(strings.Join(strings.Fields(s), " "), 90)
		}
	}
	return name
}

// readActivity lit la fin d'un transcript : ce que Claude fait d'après le dernier événement utile.
func readActivity(path string) (activity string, thinking bool, since float64, u usage, model string) {
	lines := tailLines(path, 512<<10)
	pendingTool := map[string]string{} // tool_use_id -> résumé, tant qu'aucun résultat n'est arrivé
	var order []string
	for _, l := range lines {
		var o struct {
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
		if json.Unmarshal(l, &o) != nil || (o.Type != "assistant" && o.Type != "user") {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, o.Timestamp); err == nil {
			since = float64(t.UnixNano()) / 1e9
		}
		var blocks []struct {
			Type      string         `json:"type"`
			ID        string         `json:"id"`
			Name      string         `json:"name"`
			Input     map[string]any `json:"input"`
			ToolUseID string         `json:"tool_use_id"`
			Text      string         `json:"text"`
		}
		json.Unmarshal(o.Message.Content, &blocks) // une chaîne simple donne une liste vide : un message de l'utilisateur
		switch o.Type {
		case "assistant":
			if us := o.Message.Usage; us != nil {
				u = usage{us.In, us.Out, us.CRead, us.CWr}
			}
			if o.Message.Model != "" && !strings.HasPrefix(o.Message.Model, "<") {
				model = o.Message.Model
			}
			last := ""
			for _, b := range blocks {
				switch b.Type {
				case "tool_use":
					pendingTool[b.ID] = toolBrief(b.Name, b.Input)
					order = append(order, b.ID)
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
			case last == "text" && o.Message.StopReason == "end_turn":
				activity, thinking = "a terminé sa réponse", false
			default:
				activity, thinking = "réfléchit", true
			}
		case "user":
			isResult := false
			for _, b := range blocks {
				if b.Type == "tool_result" {
					delete(pendingTool, b.ToolUseID)
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
	for i := len(order) - 1; i >= 0; i-- { // l'appel d'outil le plus récent encore sans résultat
		if s, ok := pendingTool[order[i]]; ok {
			return "exécute " + s, true, since, u, model
		}
	}
	return activity, thinking, since, u, model
}

func sessionLive(dir, id string) LiveState {
	st := LiveState{Subs: []SubLive{}, Verbs: spinnerVerbs(), Queue: []Queued{}}
	st.Status, st.Open = sessionStatus(id)
	p, ok := transcript(dir, id)
	if !ok {
		return st
	}
	st.Activity, st.Thinking, st.Since, _, st.Model = "", false, 0, usage{}, ""
	var u usage
	st.Activity, st.Thinking, st.Since, u, st.Model = readActivity(p)
	st.Context, st.Out = u.context(), u.Out
	if st.Open && st.Status == "idle" {
		st.Activity, st.Thinking = "attend votre message", false
	} else if st.Open && st.Status == "busy" {
		st.Thinking = true
		if st.Activity == "" || st.Activity == "a terminé sa réponse" {
			st.Activity = "travaille"
		}
	}
	if !st.Open {
		st.Thinking = false
	}
	if st.Open {
		st.Queue = pendingQueue(p)
	}
	st.Subs = subLives(filepath.Join(filepath.Dir(p), id, "subagents"))
	return st
}

// subLives : les sous-agents d'une session, les plus récents d'abord (60 au plus).
func subLives(dir string) []SubLive {
	out := []SubLive{}
	metas, _ := filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
	for _, m := range metas {
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), "agent-"), ".meta.json")
		var meta struct {
			AgentType   string `json:"agentType"`
			Description string `json:"description"`
			Finished    bool   `json:"finished"`
		}
		if b, err := os.ReadFile(m); err != nil || json.Unmarshal(b, &meta) != nil {
			continue
		}
		tp := filepath.Join(dir, "agent-"+id+".jsonl")
		fi, err := os.Stat(tp)
		if err != nil {
			continue
		}
		act, thinking, _, u, _ := readActivity(tp)
		s := SubLive{ID: id, Desc: meta.Description, Type: meta.AgentType, Activity: act, Context: u.context(), Out: u.Out, Mtime: float64(fi.ModTime().UnixNano()) / 1e9}
		// Un sous-agent « fini » d'après son fichier meta ne travaille plus ; sans cette marque, il travaille s'il a écrit récemment.
		s.Running = !meta.Finished && thinking && time.Since(fi.ModTime()) < 10*time.Minute
		if !s.Running && s.Activity == "" {
			s.Activity = "terminé"
		}
		if !s.Running && meta.Finished {
			s.Activity = "terminé"
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Running != out[j].Running {
			return out[i].Running
		}
		return out[i].Mtime > out[j].Mtime
	})
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}
