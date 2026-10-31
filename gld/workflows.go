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
	// Plafonds de lecture d'une fiche d'agent et du journal d'un run.
	wfMaxAgentMeta = 1 << 20
	wfMaxJournal   = 4 << 20
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

// wfRead : un fichier ordinaire de l'utilisateur de gld, ouvert sans suivre de lien, lu jusqu'à maxBytes octets.
func wfRead(path string, maxBytes int64) ([]byte, os.FileInfo, bool) {
	fileInfo, err := os.Lstat(path)
	if err != nil || !fileInfo.Mode().IsRegular() || !ownedByMe(fileInfo) || fileInfo.Size() > maxBytes {
		return nil, nil, false
	}
	file, err := os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return nil, nil, false
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBytes))
	return content, fileInfo, err == nil
}

// wfDirOK : un vrai dossier (pas un lien) qui appartient à l'utilisateur de gld.
func wfDirOK(path string) bool {
	dirInfo, err := os.Lstat(path)
	return err == nil && dirInfo.IsDir() && ownedByMe(dirInfo)
}

func wfNames(dir string) []string {
	if !wfDirOK(dir) {
		return nil
	}
	entries, _ := os.ReadDir(dir)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// sessionWorkflows : les runs les plus récents d'abord, plafonnés.
func sessionWorkflows(transcriptPath string) []Workflow {
	base := strings.TrimSuffix(transcriptPath, ".jsonl")
	if !wfDirOK(base) {
		return []Workflow{}
	}
	type runCandidate struct {
		id string
		at time.Time
	}
	seen := map[string]time.Time{}
	for _, entryName := range wfNames(filepath.Join(base, "workflows")) {
		id := strings.TrimSuffix(entryName, ".json")
		if id == entryName || !wfRunRe.MatchString(id) {
			continue
		}
		if fileInfo, err := os.Lstat(filepath.Join(base, "workflows", entryName)); err == nil {
			seen[id] = fileInfo.ModTime()
		}
	}
	for _, entryName := range wfNames(filepath.Join(base, "subagents", "workflows")) {
		if _, alreadySeen := seen[entryName]; alreadySeen || !wfRunRe.MatchString(entryName) {
			continue
		}
		if fileInfo, err := os.Lstat(filepath.Join(base, "subagents", "workflows", entryName)); err == nil {
			seen[entryName] = fileInfo.ModTime()
		}
	}
	candidates := make([]runCandidate, 0, len(seen))
	for id, modTime := range seen {
		candidates = append(candidates, runCandidate{id, modTime})
	}
	sort.Slice(candidates, func(leftIndex, rightIndex int) bool { return candidates[leftIndex].at.After(candidates[rightIndex].at) })
	if len(candidates) > wfMaxRuns {
		candidates = candidates[:wfMaxRuns]
	}
	workflows := make([]Workflow, 0, len(candidates))
	for _, candidate := range candidates {
		if workflow, loaded := loadWorkflow(base, candidate.id); loaded {
			workflows = append(workflows, workflow)
		}
	}
	sort.SliceStable(workflows, func(leftIndex, rightIndex int) bool {
		return workflows[leftIndex].StartTime > workflows[rightIndex].StartTime
	})
	return workflows
}

func loadWorkflow(base, run string) (Workflow, bool) {
	if !wfRunRe.MatchString(run) {
		return Workflow{}, false
	}
	content, _, found := wfRead(filepath.Join(base, "workflows", run+".json"), wfMaxFile)
	if !found {
		return runningWorkflow(base, run)
	}
	var finishedRun struct {
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
	if json.Unmarshal(content, &finishedRun) != nil {
		return Workflow{}, false
	}
	workflow := Workflow{RunID: run, TaskID: finishedRun.TaskID, Name: finishedRun.Name, Summary: finishedRun.Summary, Status: finishedRun.Status, StartTime: finishedRun.StartTime,
		DurationMs: finishedRun.DurationMs, TotalTokens: finishedRun.TotalTokens, TotalToolCalls: finishedRun.TotalToolCalls,
		Phases: append([]WfPhase{}, finishedRun.Phases...), Agents: []WfAgent{}, Logs: append([]string{}, finishedRun.Logs...)}
	for _, agentProgress := range finishedRun.Progress {
		if agentProgress.Type != "workflow_agent" || len(workflow.Agents) >= wfMaxAgents {
			continue
		}
		workflow.Agents = append(workflow.Agents, WfAgent{agentProgress.Label, agentProgress.PhaseTitle, agentProgress.State, agentProgress.Model, agentProgress.Tokens, agentProgress.ToolCalls, agentProgress.DurationMs, agentProgress.LastTool})
	}
	return workflow, true
}

// runningWorkflow : run sans wf json, reconstitué des agents ; « en cours » tant qu'un transcript d'agent a bougé récemment.
func runningWorkflow(base, run string) (Workflow, bool) {
	dir := filepath.Join(base, "subagents", "workflows", run)
	names := wfNames(dir)
	if len(names) == 0 {
		return Workflow{}, false
	}
	sort.Strings(names)
	workflow := Workflow{RunID: run, Status: "unknown", Phases: []WfPhase{}, Agents: []WfAgent{}, Logs: []string{}}
	phases := map[string]bool{}
	var first, last time.Time
	// Le journal du run dit quels agents ont rendu leur résultat ; l'âge du transcript ne sert que sans journal.
	finished, hasJournal := wfJournalDone(filepath.Join(dir, "journal.jsonl"))
	for _, fileName := range names {
		agentMatch := wfAgentRe.FindStringSubmatch(fileName)
		if agentMatch == nil || len(workflow.Agents) >= wfMaxAgents {
			continue
		}
		metaJSON, _, readable := wfRead(filepath.Join(dir, fileName), wfMaxAgentMeta)
		if !readable {
			continue
		}
		var meta struct {
			Description string `json:"description"`
			Phase       string `json:"workflowPhase"`
		}
		if json.Unmarshal(metaJSON, &meta) != nil {
			continue
		}
		agent := WfAgent{Label: meta.Description, Phase: meta.Phase, State: "done"}
		if transcriptInfo, err := os.Lstat(filepath.Join(dir, "agent-"+agentMatch[1]+".jsonl")); err == nil && transcriptInfo.Mode().IsRegular() {
			modTime := transcriptInfo.ModTime()
			if first.IsZero() || modTime.Before(first) {
				first = modTime
			}
			if modTime.After(last) {
				last = modTime
			}
			if hasJournal {
				if !finished[agentMatch[1]] {
					agent.State = "running"
				}
			} else if time.Since(modTime) < wfRunning {
				agent.State = "running"
			}
			if agent.State == "running" {
				workflow.Status = "running"
			}
		}
		if meta.Phase != "" && !phases[meta.Phase] {
			phases[meta.Phase] = true
			workflow.Phases = append(workflow.Phases, WfPhase{Title: meta.Phase})
		}
		workflow.Agents = append(workflow.Agents, agent)
	}
	if len(workflow.Agents) == 0 {
		return Workflow{}, false
	}
	workflow.StartTime = first.UnixMilli()
	if !first.IsZero() {
		workflow.DurationMs = last.Sub(first).Milliseconds()
	}
	return workflow, true
}

// wfJournalDone : ids des agents dont le journal du run porte une ligne « result ». ok faux : journal absent ou illisible.
func wfJournalDone(path string) (map[string]bool, bool) {
	journalContent, _, readable := wfRead(path, wfMaxJournal)
	if !readable {
		return nil, false
	}
	done := map[string]bool{}
	for _, journalLine := range strings.Split(string(journalContent), "\n") {
		var entry struct {
			Type    string `json:"type"`
			AgentID string `json:"agentId"`
		}
		if json.Unmarshal([]byte(journalLine), &entry) == nil && entry.Type == "result" && entry.AgentID != "" {
			done[entry.AgentID] = true
		}
	}
	return done, true
}
