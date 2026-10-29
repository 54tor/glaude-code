package main

// Progression des workflows Claude Code (outil Workflow) d'une session, relue à côté de son transcript :
// <session>/workflows/wf_<run>.json (écrit à la fin du run) et <session>/subagents/workflows/wf_<run>/agent-*.meta.json (pendant).

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	wfMaxRuns   = 20
	wfMaxAgents = 200
	wfMaxFile   = 8 << 20
	wfRunning   = 2 * time.Minute
)

var (
	wfRunRe   = regexp.MustCompile(`^wf_[a-z0-9-]+$`)
	wfAgentRe = regexp.MustCompile(`^agent-([A-Za-z0-9_-]{1,64})\.meta\.json$`)
)

type WfPhase struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type WfAgent struct {
	Label      string `json:"label"`
	Phase      string `json:"phase"`
	State      string `json:"state"`
	Model      string `json:"model"`
	Tokens     int64  `json:"tokens"`
	ToolCalls  int    `json:"toolCalls"`
	DurationMs int64  `json:"durationMs"`
	LastTool   string `json:"lastTool"`
}

type Workflow struct {
	RunID          string    `json:"runId"`
	TaskID         string    `json:"taskId"`
	Name           string    `json:"name"`
	Summary        string    `json:"summary"`
	Status         string    `json:"status"`
	StartTime      int64     `json:"startTime"`
	DurationMs     int64     `json:"durationMs"`
	TotalTokens    int64     `json:"totalTokens"`
	TotalToolCalls int       `json:"totalToolCalls"`
	Phases         []WfPhase `json:"phases"`
	Agents         []WfAgent `json:"agents"`
	Logs           []string  `json:"logs"`
}

// wfRead : un fichier ordinaire de l'utilisateur de gld, ouvert sans suivre de lien, lu jusqu'à max octets.
func wfRead(path string, max int64) ([]byte, os.FileInfo, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || !ownedByMe(fi) || fi.Size() > max {
		return nil, nil, false
	}
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return nil, nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max))
	return b, fi, err == nil
}

// wfDirOK : un vrai dossier (pas un lien) qui appartient à l'utilisateur de gld.
func wfDirOK(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir() && ownedByMe(fi)
}

func wfNames(dir string) []string {
	if !wfDirOK(dir) {
		return nil
	}
	es, _ := os.ReadDir(dir)
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// sessionWorkflows : les runs les plus récents d'abord, plafonnés.
func sessionWorkflows(p string) []Workflow {
	base := strings.TrimSuffix(p, ".jsonl")
	if !wfDirOK(base) {
		return []Workflow{}
	}
	type cand struct {
		id string
		at time.Time
	}
	seen := map[string]time.Time{}
	for _, n := range wfNames(filepath.Join(base, "workflows")) {
		id := strings.TrimSuffix(n, ".json")
		if id == n || !wfRunRe.MatchString(id) {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(base, "workflows", n)); err == nil {
			seen[id] = fi.ModTime()
		}
	}
	for _, n := range wfNames(filepath.Join(base, "subagents", "workflows")) {
		if _, ok := seen[n]; ok || !wfRunRe.MatchString(n) {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(base, "subagents", "workflows", n)); err == nil {
			seen[n] = fi.ModTime()
		}
	}
	cs := make([]cand, 0, len(seen))
	for id, at := range seen {
		cs = append(cs, cand{id, at})
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].at.After(cs[j].at) })
	if len(cs) > wfMaxRuns {
		cs = cs[:wfMaxRuns]
	}
	out := make([]Workflow, 0, len(cs))
	for _, c := range cs {
		if w, ok := loadWorkflow(base, c.id); ok {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartTime > out[j].StartTime })
	return out
}

func loadWorkflow(base, run string) (Workflow, bool) {
	if !wfRunRe.MatchString(run) {
		return Workflow{}, false
	}
	if b, _, ok := wfRead(filepath.Join(base, "workflows", run+".json"), wfMaxFile); ok {
		var raw struct {
			RunID          string    `json:"runId"`
			TaskID         string    `json:"taskId"`
			Name           string    `json:"workflowName"`
			Summary        string    `json:"summary"`
			Status         string    `json:"status"`
			StartTime      int64     `json:"startTime"`
			DurationMs     int64     `json:"durationMs"`
			TotalTokens    int64     `json:"totalTokens"`
			TotalToolCalls int       `json:"totalToolCalls"`
			Phases         []WfPhase `json:"phases"`
			Logs           []string  `json:"logs"`
			Progress       []struct {
				Type       string `json:"type"`
				Label      string `json:"label"`
				PhaseTitle string `json:"phaseTitle"`
				Model      string `json:"model"`
				State      string `json:"state"`
				LastTool   string `json:"lastToolName"`
				Tokens     int64  `json:"tokens"`
				ToolCalls  int    `json:"toolCalls"`
				DurationMs int64  `json:"durationMs"`
			} `json:"workflowProgress"`
		}
		if json.Unmarshal(b, &raw) != nil {
			return Workflow{}, false
		}
		w := Workflow{RunID: run, TaskID: raw.TaskID, Name: raw.Name, Summary: raw.Summary, Status: raw.Status, StartTime: raw.StartTime,
			DurationMs: raw.DurationMs, TotalTokens: raw.TotalTokens, TotalToolCalls: raw.TotalToolCalls,
			Phases: append([]WfPhase{}, raw.Phases...), Agents: []WfAgent{}, Logs: append([]string{}, raw.Logs...)}
		for _, a := range raw.Progress {
			if a.Type != "workflow_agent" || len(w.Agents) >= wfMaxAgents {
				continue
			}
			w.Agents = append(w.Agents, WfAgent{a.Label, a.PhaseTitle, a.State, a.Model, a.Tokens, a.ToolCalls, a.DurationMs, a.LastTool})
		}
		return w, true
	}
	return runningWorkflow(base, run)
}

// runningWorkflow : run sans wf json, reconstitué des agents ; « en cours » tant qu'un transcript d'agent a bougé récemment.
func runningWorkflow(base, run string) (Workflow, bool) {
	dir := filepath.Join(base, "subagents", "workflows", run)
	names := wfNames(dir)
	if len(names) == 0 {
		return Workflow{}, false
	}
	sort.Strings(names)
	w := Workflow{RunID: run, Status: "unknown", Phases: []WfPhase{}, Agents: []WfAgent{}, Logs: []string{}}
	phases := map[string]bool{}
	var first, last time.Time
	// Le journal du run dit quels agents ont rendu leur résultat ; l'âge du transcript ne sert que sans journal.
	finished, hasJournal := wfJournalDone(filepath.Join(dir, "journal.jsonl"))
	for _, n := range names {
		m := wfAgentRe.FindStringSubmatch(n)
		if m == nil || len(w.Agents) >= wfMaxAgents {
			continue
		}
		b, _, ok := wfRead(filepath.Join(dir, n), 1<<20)
		if !ok {
			continue
		}
		var meta struct {
			Description string `json:"description"`
			Phase       string `json:"workflowPhase"`
		}
		if json.Unmarshal(b, &meta) != nil {
			continue
		}
		a := WfAgent{Label: meta.Description, Phase: meta.Phase, State: "done"}
		if fi, err := os.Lstat(filepath.Join(dir, "agent-"+m[1]+".jsonl")); err == nil && fi.Mode().IsRegular() {
			mt := fi.ModTime()
			if first.IsZero() || mt.Before(first) {
				first = mt
			}
			if mt.After(last) {
				last = mt
			}
			if hasJournal {
				if !finished[m[1]] {
					a.State = "running"
				}
			} else if time.Since(mt) < wfRunning {
				a.State = "running"
			}
			if a.State == "running" {
				w.Status = "running"
			}
		}
		if meta.Phase != "" && !phases[meta.Phase] {
			phases[meta.Phase] = true
			w.Phases = append(w.Phases, WfPhase{Title: meta.Phase})
		}
		w.Agents = append(w.Agents, a)
	}
	if len(w.Agents) == 0 {
		return Workflow{}, false
	}
	w.StartTime = first.UnixMilli()
	if !first.IsZero() {
		w.DurationMs = last.Sub(first).Milliseconds()
	}
	return w, true
}

// wfJournalDone : ids des agents dont le journal du run porte une ligne « result ». ok faux : journal absent ou illisible.
func wfJournalDone(path string) (map[string]bool, bool) {
	b, _, ok := wfRead(path, 4<<20)
	if !ok {
		return nil, false
	}
	done := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		var e struct {
			Type    string `json:"type"`
			AgentID string `json:"agentId"`
		}
		if json.Unmarshal([]byte(l), &e) == nil && e.Type == "result" && e.AgentID != "" {
			done[e.AgentID] = true
		}
	}
	return done, true
}
