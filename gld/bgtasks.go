package main

// Tâches de fond d'une session (commande Bash lancée avec run_in_background, Monitor, workflows…) relues dans son transcript :
// le démarrage est le résultat d'outil qui porte backgroundTaskId, la fin une <task-notification> mise en file par Claude Code.
// La sortie de chaque tâche est un fichier /tmp/claude-<uid>/…/tasks/<id>.output que Claude Code écrit lui-même.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type BgTask struct {
	ID      string `json:"id"`
	Tool    string `json:"tool,omitempty"`
	Desc    string `json:"desc"`
	Status  string `json:"status"` // running, completed, failed, killed… (celui de la notification), unknown : session fermée sans fin relevée
	Summary string `json:"summary,omitempty"`
	Start   string `json:"start,omitempty"`
	End     string `json:"end,omitempty"`
	Output  string `json:"-"` // chemin du fichier de sortie, jamais envoyé tel quel
	HasOut  bool   `json:"hasOutput"`
}

var (
	bgOutRe  = regexp.MustCompile(`^/tmp/claude-\d+/[^/]+/[0-9a-fA-F-]{36}/tasks/[A-Za-z0-9]+\.output$`)
	bgTagRe  = regexp.MustCompile(`<(task-id|tool-use-id|output-file|status|summary)>([^<]*)</`)
	bgCache  sync.Mutex
	bgCached = map[string]struct {
		size, mtime int64
		tasks       []BgTask
	}{}
)

func bgTag(s string) map[string]string {
	m := map[string]string{}
	for _, x := range bgTagRe.FindAllStringSubmatch(s, -1) {
		m[x[1]] = x[2]
	}
	return m
}

// bgTasks : les tâches de fond du transcript p, dans l'ordre de démarrage ; open dit si la session tourne encore.
func bgTasks(p string, open bool) []BgTask {
	fi, err := os.Stat(p)
	if err != nil {
		return nil
	}
	bgCache.Lock()
	c, ok := bgCached[p]
	bgCache.Unlock()
	if !ok || c.size != fi.Size() || c.mtime != fi.ModTime().UnixNano() {
		c.size, c.mtime, c.tasks = fi.Size(), fi.ModTime().UnixNano(), scanBgTasks(p)
		bgCache.Lock()
		bgCached[p] = c
		bgCache.Unlock()
	}
	out := make([]BgTask, len(c.tasks))
	copy(out, c.tasks)
	for i := range out {
		if out[i].Status == "running" && out[i].HasOut && bgKilled(out[i].Output) {
			out[i].Status = "killed"
		} else if out[i].Status == "running" && out[i].Tool == "Agent" && agentFinished(p, out[i].ID) {
			out[i].Status = "completed"
		} else if out[i].Status == "running" && !open {
			out[i].Status = "unknown"
		}
	}
	return out
}

func scanBgTasks(p string) []BgTask {
	f, err := openTrusted(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	type use struct{ tool, desc string }
	uses := map[string]use{} // tool_use_id → l'appel qui a lancé la tâche
	byID := map[string]*BgTask{}
	var order []string
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		l, err := r.ReadBytes('\n')
		if len(l) > 0 {
			switch {
			case bytes.Contains(l, []byte(`"run_in_background"`)) && bytes.Contains(l, []byte(`"tool_use"`)):
				var o struct {
					Message struct {
						Content []struct {
							Type, ID, Name string
							Input          struct {
								Description, Command string
							}
						}
					}
				}
				if json.Unmarshal(l, &o) == nil {
					for _, b := range o.Message.Content {
						if b.Type == "tool_use" {
							d := b.Input.Description
							if d == "" {
								d = cut(b.Input.Command, 200)
							}
							uses[b.ID] = use{b.Name, d}
						}
					}
				}
			case bytes.Contains(l, []byte(`"async_launched"`)) && bytes.Contains(l, []byte(`"agentId"`)):
				// Agent lancé en fond : son identifiant est la task-id de la notification de fin
				var o struct {
					Timestamp string
					Result    struct{ Status, AgentId, Description string } `json:"toolUseResult"`
				}
				if json.Unmarshal(l, &o) == nil && o.Result.Status == "async_launched" && bgSafeID.MatchString(o.Result.AgentId) && byID[o.Result.AgentId] == nil {
					t := &BgTask{ID: o.Result.AgentId, Tool: "Agent", Desc: cut(o.Result.Description, 200), Status: "running", Start: o.Timestamp}
					byID[t.ID] = t
					order = append(order, t.ID)
				}
			case bytes.Contains(l, []byte(`"backgroundTaskId"`)):
				var o struct {
					Timestamp string
					Message   struct {
						Content []struct {
							ToolUseID string `json:"tool_use_id"`
						}
					}
					Result struct{ BackgroundTaskId string } `json:"toolUseResult"`
				}
				if json.Unmarshal(l, &o) == nil && o.Result.BackgroundTaskId != "" && byID[o.Result.BackgroundTaskId] == nil {
					t := &BgTask{ID: o.Result.BackgroundTaskId, Status: "running", Start: o.Timestamp}
					if len(o.Message.Content) > 0 {
						u := uses[o.Message.Content[0].ToolUseID]
						t.Tool, t.Desc = u.tool, u.desc
					}
					byID[t.ID] = t
					order = append(order, t.ID)
				}
			case bytes.Contains(l, []byte(`Successfully stopped task: `)) && bytes.Contains(l, []byte(`"tool_result"`)):
				// TaskStop ne met aucune <task-notification> en file : son résultat est la seule trace de la fin
				var o struct{ Timestamp string }
				if json.Unmarshal(l, &o) != nil {
					break
				}
				if m := bgStoppedRe.FindSubmatch(l); m != nil {
					if t := byID[string(m[1])]; t != nil && t.Status == "running" {
						t.Status, t.End = "killed", o.Timestamp
					}
				}
			case bytes.Contains(l, []byte(`"queue-operation"`)) && bytes.Contains(l, []byte(`<task-id>`)):
				var o struct {
					Operation, Timestamp, Content string
				}
				if json.Unmarshal(l, &o) != nil || o.Operation != "enqueue" {
					break
				}
				m := bgTag(o.Content)
				id := m["task-id"]
				if id == "" || !bgSafeID.MatchString(id) {
					break
				}
				t := byID[id]
				if t == nil { // lancée autrement (Monitor, workflow…) : on ne connaît que sa fin
					t = &BgTask{ID: id, Desc: m["summary"]}
					byID[id] = t
					order = append(order, id)
				}
				t.Status, t.Summary, t.End = m["status"], cut(m["summary"], 300), o.Timestamp
				if t.Desc == "" {
					t.Desc = t.Summary
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				return nil
			}
			break
		}
	}
	out := make([]BgTask, 0, len(order))
	for _, id := range order {
		t := byID[id]
		t.Output = bgOutputPath(p, id) // jamais le chemin écrit dans le transcript : il n'est pas de confiance
		t.HasOut = bgOutputOK(t.Output)
		out = append(out, *t)
	}
	return out
}

var bgStoppedRe = regexp.MustCompile(`Successfully stopped task: ([A-Za-z0-9]{1,40})\b`)

var bgSafeID = regexp.MustCompile(`^[A-Za-z0-9]{1,40}$`)

// bgOutputPath : l'emplacement habituel de la sortie d'une tâche, d'après le transcript (<tmp>/claude-<uid>/<dossier>/<session>/tasks).
func bgOutputPath(transcript, id string) string {
	sess := strings.TrimSuffix(filepath.Base(transcript), ".jsonl")
	dir := filepath.Base(filepath.Dir(transcript))
	return filepath.Join(os.TempDir(), "claude-"+strconv.Itoa(os.Getuid()), dir, sess, "tasks", id+".output")
}

// bgOutputOK : un fichier ordinaire (ni lien symbolique, ni répertoire) qui appartient à l'utilisateur de gld, au chemin que Claude Code lui donne.
func bgOutputOK(path string) bool {
	if filepath.Clean(path) != path || !bgOutRe.MatchString(path) {
		return false
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	return ownedByMe(fi)
}

// bgKilled : Claude Code termine par « [killed] » la sortie d'une commande de fond tuée, même sans <task-notification> (kill du processus).
// Les agents n'ont pas ce marqueur : leur sortie est leur transcript.
func bgKilled(path string) bool {
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	b := make([]byte, min(fi.Size(), 32))
	n, _ := f.ReadAt(b, fi.Size()-int64(len(b)))
	return bytes.HasSuffix(bytes.TrimSpace(b[:n]), []byte("[killed]"))
}

// agentFinished : Claude Code pose <session>/subagents/agent-<id>.meta.json avec finished à vrai quand un sous-agent se termine,
// même si la <task-notification> n'a jamais été mise en file (session fermée entre-temps).
func agentFinished(transcript, id string) bool {
	b, err := readTrusted(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "agent-"+id+".meta.json"))
	if err != nil {
		return false
	}
	var m struct{ Finished bool }
	return json.Unmarshal(b, &m) == nil && m.Finished
}

// bgOutput : les 64 derniers Kio de la sortie d'une tâche de ce transcript. L'identifiant doit être une tâche relevée dans le transcript
// et le chemin respecter la forme que Claude Code lui donne.
func bgOutput(p, id string) (string, bool) {
	for _, t := range scanBgTasks(p) {
		if t.ID != id || !bgOutputOK(t.Output) {
			continue
		}
		f, err := os.OpenFile(t.Output, os.O_RDONLY|oNoFollow, 0) // un lien posé entre la vérification et l'ouverture est refusé
		if err != nil {
			return "", false
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil || !fi.Mode().IsRegular() {
			return "", false
		}
		off := max(0, fi.Size()-64<<10)
		b := make([]byte, fi.Size()-off)
		n, _ := f.ReadAt(b, off)
		return string(b[:n]), true
	}
	return "", false
}
