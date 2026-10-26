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
	"syscall"
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
		if out[i].Status == "running" && !open {
			out[i].Status = "unknown"
		}
	}
	return out
}

func scanBgTasks(p string) []BgTask {
	f, err := os.Open(p)
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
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// bgOutput : les 64 derniers Kio de la sortie d'une tâche de ce transcript. L'identifiant doit être une tâche relevée dans le transcript
// et le chemin respecter la forme que Claude Code lui donne.
func bgOutput(p, id string) (string, bool) {
	for _, t := range scanBgTasks(p) {
		if t.ID != id || !bgOutputOK(t.Output) {
			continue
		}
		f, err := os.OpenFile(t.Output, os.O_RDONLY|syscall.O_NOFOLLOW, 0) // un lien posé entre la vérification et l'ouverture est refusé
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
