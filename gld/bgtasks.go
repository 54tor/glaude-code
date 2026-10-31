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

const (
	jsonLineBufferSize = 1 << 20
	bgDescMaxLen       = 200
	bgSummaryMaxLen    = 300
	bgKilledTailBytes  = 32
	bgOutputTailBytes  = 64 << 10
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

func bgTag(text string) map[string]string {
	tags := map[string]string{}
	for _, match := range bgTagRe.FindAllStringSubmatch(text, -1) {
		tags[match[1]] = match[2]
	}
	return tags
}

// bgTasks : les tâches de fond du transcript transcriptPath, dans l'ordre de démarrage ; open dit si la session tourne encore.
func bgTasks(transcriptPath string, open bool) []BgTask {
	fileInfo, err := os.Stat(transcriptPath)
	if err != nil {
		return nil
	}
	bgCache.Lock()
	cached, found := bgCached[transcriptPath]
	bgCache.Unlock()
	isStale := !found || cached.size != fileInfo.Size() || cached.mtime != fileInfo.ModTime().UnixNano()
	if isStale {
		cached.size, cached.mtime, cached.tasks = fileInfo.Size(), fileInfo.ModTime().UnixNano(), scanBgTasks(transcriptPath)
		bgCache.Lock()
		bgCached[transcriptPath] = cached
		bgCache.Unlock()
	}
	tasks := make([]BgTask, len(cached.tasks))
	copy(tasks, cached.tasks)
	for taskIndex := range tasks {
		task := &tasks[taskIndex]
		if task.Status != "running" {
			continue
		}
		switch {
		case task.HasOut && bgKilled(task.Output):
			task.Status = "killed"
		case task.Tool == "Agent" && agentFinished(transcriptPath, task.ID):
			task.Status = "completed"
		case !open:
			task.Status = "unknown"
		}
	}
	return tasks
}

func scanBgTasks(transcriptPath string) []BgTask {
	transcriptFile, openErr := openTrusted(transcriptPath)
	if openErr != nil {
		return nil
	}
	defer transcriptFile.Close()
	type toolUse struct{ tool, desc string }
	toolUses := map[string]toolUse{} // tool_use_id → l'appel qui a lancé la tâche
	tasksByID := map[string]*BgTask{}
	var order []string
	reader := bufio.NewReaderSize(transcriptFile, jsonLineBufferSize)
	for {
		transcriptLine, readErr := reader.ReadBytes('\n')
		if len(transcriptLine) > 0 {
			switch {
			case bytes.Contains(transcriptLine, []byte(`"run_in_background"`)) && bytes.Contains(transcriptLine, []byte(`"tool_use"`)):
				var toolUseLine struct {
					Message struct {
						Content []struct {
							Type, ID, Name string
							Input          struct {
								Description, Command string
							}
						}
					}
				}
				if json.Unmarshal(transcriptLine, &toolUseLine) == nil {
					for _, contentBlock := range toolUseLine.Message.Content {
						if contentBlock.Type == "tool_use" {
							description := contentBlock.Input.Description
							if description == "" {
								description = cut(contentBlock.Input.Command, bgDescMaxLen)
							}
							toolUses[contentBlock.ID] = toolUse{contentBlock.Name, description}
						}
					}
				}
			case bytes.Contains(transcriptLine, []byte(`"async_launched"`)) && bytes.Contains(transcriptLine, []byte(`"agentId"`)):
				// Agent lancé en fond : son identifiant est la task-id de la notification de fin
				var agentLaunchLine struct {
					Timestamp string
					Result    struct{ Status, AgentId, Description string } `json:"toolUseResult"`
				}
				if json.Unmarshal(transcriptLine, &agentLaunchLine) != nil {
					break
				}
				agentID := agentLaunchLine.Result.AgentId
				isNewAgentLaunch := agentLaunchLine.Result.Status == "async_launched" && bgSafeID.MatchString(agentID) && tasksByID[agentID] == nil
				if isNewAgentLaunch {
					task := &BgTask{ID: agentID, Tool: "Agent", Desc: cut(agentLaunchLine.Result.Description, bgDescMaxLen), Status: "running", Start: agentLaunchLine.Timestamp}
					tasksByID[task.ID] = task
					order = append(order, task.ID)
				}
			case bytes.Contains(transcriptLine, []byte(`"backgroundTaskId"`)):
				var taskStartLine struct {
					Timestamp string
					Message   struct {
						Content []struct {
							ToolUseID string `json:"tool_use_id"`
						}
					}
					Result struct{ BackgroundTaskId string } `json:"toolUseResult"`
				}
				if json.Unmarshal(transcriptLine, &taskStartLine) != nil {
					break
				}
				backgroundTaskID := taskStartLine.Result.BackgroundTaskId
				isNewTask := backgroundTaskID != "" && tasksByID[backgroundTaskID] == nil
				if isNewTask {
					task := &BgTask{ID: backgroundTaskID, Status: "running", Start: taskStartLine.Timestamp}
					if len(taskStartLine.Message.Content) > 0 {
						launchingUse := toolUses[taskStartLine.Message.Content[0].ToolUseID]
						task.Tool, task.Desc = launchingUse.tool, launchingUse.desc
					}
					tasksByID[task.ID] = task
					order = append(order, task.ID)
				}
			case bytes.Contains(transcriptLine, []byte(`Successfully stopped task: `)) && bytes.Contains(transcriptLine, []byte(`"tool_result"`)):
				// TaskStop ne met aucune <task-notification> en file : son résultat est la seule trace de la fin
				var stopLine struct{ Timestamp string }
				if json.Unmarshal(transcriptLine, &stopLine) != nil {
					break
				}
				if stoppedMatch := bgStoppedRe.FindSubmatch(transcriptLine); stoppedMatch != nil {
					if task := tasksByID[string(stoppedMatch[1])]; task != nil && task.Status == "running" {
						task.Status, task.End = "killed", stopLine.Timestamp
					}
				}
			case bytes.Contains(transcriptLine, []byte(`"queue-operation"`)) && bytes.Contains(transcriptLine, []byte(`<task-id>`)):
				var queueLine struct {
					Operation, Timestamp, Content string
				}
				if json.Unmarshal(transcriptLine, &queueLine) != nil || queueLine.Operation != "enqueue" {
					break
				}
				tags := bgTag(queueLine.Content)
				taskID := tags["task-id"]
				if taskID == "" || !bgSafeID.MatchString(taskID) {
					break
				}
				task := tasksByID[taskID]
				if task == nil { // lancée autrement (Monitor, workflow…) : on ne connaît que sa fin
					task = &BgTask{ID: taskID, Desc: tags["summary"]}
					tasksByID[taskID] = task
					order = append(order, taskID)
				}
				task.Status, task.Summary, task.End = tags["status"], cut(tags["summary"], bgSummaryMaxLen), queueLine.Timestamp
				if task.Desc == "" {
					task.Desc = task.Summary
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return nil
			}
			break
		}
	}
	tasks := make([]BgTask, 0, len(order))
	for _, taskID := range order {
		task := tasksByID[taskID]
		task.Output = bgOutputPath(transcriptPath, taskID) // jamais le chemin écrit dans le transcript : il n'est pas de confiance
		task.HasOut = bgOutputOK(task.Output)
		tasks = append(tasks, *task)
	}
	return tasks
}

var bgStoppedRe = regexp.MustCompile(`Successfully stopped task: ([A-Za-z0-9]{1,40})\b`)

var bgSafeID = regexp.MustCompile(`^[A-Za-z0-9]{1,40}$`)

// bgOutputPath : l'emplacement habituel de la sortie d'une tâche, d'après le transcript (<tmp>/claude-<uid>/<dossier>/<session>/tasks).
func bgOutputPath(transcript, id string) string {
	sessionID := strings.TrimSuffix(filepath.Base(transcript), ".jsonl")
	projectDir := filepath.Base(filepath.Dir(transcript))
	return filepath.Join(os.TempDir(), "claude-"+strconv.Itoa(os.Getuid()), projectDir, sessionID, "tasks", id+".output")
}

// bgOutputOK : un fichier ordinaire (ni lien symbolique, ni répertoire) qui appartient à l'utilisateur de gld, au chemin que Claude Code lui donne.
func bgOutputOK(path string) bool {
	if filepath.Clean(path) != path || !bgOutRe.MatchString(path) {
		return false
	}
	fileInfo, err := os.Lstat(path)
	if err != nil || !fileInfo.Mode().IsRegular() {
		return false
	}
	return ownedByMe(fileInfo)
}

// bgKilled : Claude Code termine par « [killed] » la sortie d'une commande de fond tuée, même sans <task-notification> (kill du processus).
// Les agents n'ont pas ce marqueur : leur sortie est leur transcript.
func bgKilled(path string) bool {
	outputFile, err := os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return false
	}
	defer outputFile.Close()
	fileInfo, err := outputFile.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() {
		return false
	}
	tail := make([]byte, min(fileInfo.Size(), bgKilledTailBytes))
	readCount, _ := outputFile.ReadAt(tail, fileInfo.Size()-int64(len(tail)))
	return bytes.HasSuffix(bytes.TrimSpace(tail[:readCount]), []byte("[killed]"))
}

// agentFinished : Claude Code pose <session>/subagents/agent-<id>.meta.json avec finished à vrai quand un sous-agent se termine,
// même si la <task-notification> n'a jamais été mise en file (session fermée entre-temps).
func agentFinished(transcript, id string) bool {
	metaContent, err := readTrusted(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "agent-"+id+".meta.json"))
	if err != nil {
		return false
	}
	var meta struct{ Finished bool }
	return json.Unmarshal(metaContent, &meta) == nil && meta.Finished
}

// bgOutput : les 64 derniers Kio de la sortie d'une tâche de ce transcript. L'identifiant doit être une tâche relevée dans le transcript
// et le chemin respecter la forme que Claude Code lui donne.
func bgOutput(transcriptPath, id string) (string, bool) {
	for _, task := range scanBgTasks(transcriptPath) {
		if task.ID != id || !bgOutputOK(task.Output) {
			continue
		}
		outputFile, err := os.OpenFile(task.Output, os.O_RDONLY|oNoFollow, 0) // un lien posé entre la vérification et l'ouverture est refusé
		if err != nil {
			return "", false
		}
		defer outputFile.Close()
		fileInfo, err := outputFile.Stat()
		if err != nil || !fileInfo.Mode().IsRegular() {
			return "", false
		}
		offset := max(0, fileInfo.Size()-bgOutputTailBytes)
		content := make([]byte, fileInfo.Size()-offset)
		readCount, _ := outputFile.ReadAt(content, offset)
		return string(content[:readCount]), true
	}
	return "", false
}
