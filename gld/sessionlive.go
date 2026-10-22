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
	Said        string    `json:"said,omitempty"` // session en attente : dernier paragraphe de la réponse de Claude (souvent la question posée)
	Open        bool      `json:"open"`
	Status      string    `json:"status"`   // busy, idle, "" (inconnu)
	Activity    string    `json:"activity"` // en une phrase
	Thinking    bool      `json:"thinking"` // Claude travaille (réflexion ou outil)
	Since       float64   `json:"since"`    // dernier événement du transcript (secondes epoch)
	Context     int       `json:"context"`  // tokens du dernier appel : entrée + cache lu + cache écrit
	Out         int       `json:"out"`      // tokens produits au dernier tour
	Model       string    `json:"model"`
	Tool        string    `json:"tool"`        // dernier outil appelé (« Bash : go test »)
	ToolRunning bool      `json:"toolRunning"` // son résultat n'est pas encore arrivé
	Subs        []SubLive `json:"subs"`
	Verbs       []string  `json:"verbs"` // verbes affichés à tour de rôle pendant la réflexion
	Queue       []Queued  `json:"queue"` // messages envoyés pendant que Claude travaillait, pas encore lus par la session
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

var thinkingVerbs = []string{"réfléchit", "cogite", "médite", "mijote", "rumine", "planche", "phosphore", "gamberge", "se concentre", "décortique", "jongle avec les idées", "pèse le pour et le contre", "fait chauffer les neurones", "creuse la question", "tisse sa réponse", "cherche le bon angle", "démêle l'écheveau", "met de l'ordre dans ses idées", "ourdit un plan", "contemple le problème",
	// drôles et improbables
	"négocie avec ses neurones", "consulte l'oracle des octets", "dresse les hamsters du serveur", "démonte le problème à la petite cuillère",
	"réveille le gobelin du cache", "fait sa gymnastique mentale", "attend que le café fasse effet", "plaide sa cause auprès du compilateur",
	"compte les moutons électriques", "épluche les octets", "fait mariner la question", "bat le rappel des idées",
	"cherche ses lunettes dans le contexte", "jongle avec trois tokens", "écoute le murmure des pointeurs", "dépoussière sa boule de cristal",
	"fait un câlin au compilateur", "remonte ses manches virtuelles", "invoque l'esprit du grep", "danse la gigue des octets",
	"tricote une réponse", "mouline à plein régime", "fait la sieste active", "confie le problème à un pigeon voyageur",
	"réchauffe ses circuits au bain-marie", "chasse le bug sauvage", "interroge les astres du code", "ouvre un œil sur la RAM",
	"fait claquer ses transistors", "gratte la surface du problème", "déplie sa carte du royaume", "taille une plume numérique",
	"marmonne en binaire", "mâchouille le prompt", "convoque le conseil des sages", "teste sa théorie sur un canard en plastique",
	"affûte son meilleur argument", "fait le tour du propriétaire", "secoue le sac à idées", "souffle sur les braises de l'inspiration",
	"s'étire les neurones", "range ses pensées par ordre alphabétique", "triture le problème", "fait bouillir la marmite à tokens",
	"navigue à vue dans le brouillard", "ausculte le code", "fait passer le message de bouche à oreille", "cherche une aiguille dans la botte de foin",
	"lustre ses algorithmes", "tire les ficelles", "lit dans le marc de café", "fouille dans le grenier de la mémoire",
	"prend son élan", "dialogue avec le rubber duck", "remue le cambouis", "mène l'enquête en imperméable",
	"colle des post-it mentaux", "affronte le dragon des dépendances", "improvise un solo de clavier", "plie le problème en origami",
	"fait des étincelles", "tâtonne dans le noir avec panache", "pousse un grand soupir numérique", "met son chapeau de détective",
	"réchauffe la soupe de bits", "pétrit la pâte à réponses", "se gratte la tête avec élégance", "tisse sa toile de raisonnement",
	"sonde les abysses du dépôt", "fait du lèche-vitrine dans les fichiers", "affine sa stratégie de ninja", "mitonne un petit plat de logique",
	"pèse ses mots au microgramme", "compte jusqu'à dix en hexadécimal", "ressort la boîte à outils", "verse un peu d'huile dans les rouages",
	"gonfle ses voiles", "joue à cache-cache avec le bug", "s'entraîne devant le miroir", "cuisine à feu doux",
	"déchiffre les hiéroglyphes du legacy", "fait des claquettes sur le clavier", "visse et dévisse ses hypothèses", "plonge en apnée dans le code",
	"cherche la clé sous le paillasson", "organise un brainstorm avec lui-même", "enfile ses bottes de sept lieues", "polit le cristal de la logique",
	"mélange les cartes", "met le nez dans les logs", "fait tourner la roue du hamster", "dépiste l'erreur à la trace",
	"échafaude une théorie bancale mais courageuse", "ratisse large", "sort la loupe de Sherlock", "peaufine sa botte secrète",
	"dompte le chaos", "carbure au café imaginaire", "fait chanter les variables", "avale un dictionnaire de travers",
	"reprend son souffle au bord du stack", "s'en remet au hasard bien organisé", "attrape les idées au vol", "fait du rangement dans ses pointeurs",
}

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

// toolFull : l'appel d'outil en entier (commande complète, plusieurs lignes comprises), pour l'affichage du dernier outil.
func toolFull(name string, in map[string]any) string {
	for _, k := range []string{"command", "description", "file_path", "path", "pattern", "url", "query", "prompt", "subject"} {
		if s, ok := in[k].(string); ok && strings.TrimSpace(s) != "" {
			return name + " : " + cut(strings.TrimSpace(s), 4000)
		}
	}
	return name
}

// readActivity lit la fin d'un transcript : ce que Claude fait d'après le dernier événement utile.
func readActivity(path string) (activity string, thinking bool, since float64, u usage, model string, tool string, toolRunning bool) {
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
					tool, toolRunning = toolFull(b.Name, b.Input), true
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
					if len(order) > 0 && order[len(order)-1] == b.ToolUseID {
						toolRunning = false
					}
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
			return "exécute " + s, true, since, u, model, tool, toolRunning
		}
	}
	return activity, thinking, since, u, model, tool, toolRunning
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
	st.Activity, st.Thinking, st.Since, u, st.Model, st.Tool, st.ToolRunning = readActivity(p)
	st.Context, st.Out = u.context(), u.Out
	if st.Open && st.Status == "idle" {
		st.Activity, st.Thinking = "attend votre message", false
		st.Said = lastSaid(p)
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
		act, thinking, _, u, _, _, _ := readActivity(tp)
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

// lastSaid : le dernier paragraphe du dernier texte écrit par Claude (200 caractères au plus), "" s'il n'y en a pas.
func lastSaid(path string) string {
	lines := tailLines(path, 256<<10)
	for i := len(lines) - 1; i >= 0; i-- {
		var o struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &o) != nil || o.Type != "assistant" {
			continue
		}
		var blocks []struct{ Type, Text string }
		if json.Unmarshal(o.Message.Content, &blocks) != nil {
			continue
		}
		for j := len(blocks) - 1; j >= 0; j-- {
			if t := strings.TrimSpace(blocks[j].Text); blocks[j].Type == "text" && t != "" {
				paras := strings.Split(t, "\n\n")
				return cut(spaceRe.ReplaceAllString(strings.TrimSpace(paras[len(paras)-1]), " "), 200)
			}
		}
		return "" // le dernier message de Claude n'a pas de texte (outil seul)
	}
	return ""
}
