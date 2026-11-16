package main

// État des sous-agents d'une session d'après les hooks de Claude Code (SubagentStart, SubagentStop, Stop, SessionStart,
// SessionEnd) : `gld _event` ajoute une ligne JSON par événement, et l'on rejoue le journal d'une session pour savoir
// si un sous-agent tourne. Signal publié par Claude Code, contrairement au texte de l'écran ou aux fichiers internes.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type stateEvent struct {
	At        int64  `json:"at"`
	Session   string `json:"session"`
	Event     string `json:"event"`
	Agent     string `json:"agent,omitempty"`
	Subagents int    `json:"subagents"` // sous-agents en cours d'après background_tasks ; -1 si l'événement ne le dit pas
}

const (
	stateEventsMax      = 2 << 20
	stateEventsKeep     = stateEventsMax / 2
	stateEventsInputMax = 1 << 18
	unknownSubCount     = -1
)

func eventsFile() string { return filepath.Join(cfgDir(), "gld-web-events.jsonl") }

// runningTaskKinds : les tâches de fond qui gardent une session occupée ; un shell de fond, lui, la laisse au repos.
var runningTaskKinds = map[string]bool{"subagent": true, "workflow": true, "teammate": true}

var finishedTaskStatuses = map[string]bool{"completed": true, "failed": true, "killed": true, "stopped": true, "cancelled": true}

// runEvent : l'entrée du hook (JSON sur stdin). Jamais d'échec visible : un hook ne doit pas gêner une session.
func runEvent(input io.Reader) int {
	var hookInput struct {
		Session         string `json:"session_id"`
		Event           string `json:"hook_event_name"`
		Agent           string `json:"agent_id"`
		BackgroundTasks *[]struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"background_tasks"`
	}
	if json.NewDecoder(io.LimitReader(input, stateEventsInputMax)).Decode(&hookInput) != nil || hookInput.Session == "" || hookInput.Event == "" {
		return 0
	}
	event := stateEvent{At: time.Now().Unix(), Session: hookInput.Session, Event: hookInput.Event, Agent: hookInput.Agent, Subagents: unknownSubCount}
	if hookInput.BackgroundTasks != nil {
		event.Subagents = 0
		for _, task := range *hookInput.BackgroundTasks {
			if runningTaskKinds[task.Type] && !finishedTaskStatuses[task.Status] {
				event.Subagents++
			}
		}
	}
	line, _ := json.Marshal(event)
	trimLogTail(eventsFile(), stateEventsMax, stateEventsKeep)
	file, err := rOpenFile(cfgDir(), eventsFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0
	}
	defer file.Close()
	file.Write(append(line, '\n'))
	return 0
}

var (
	eventsMu    sync.Mutex
	eventsCache struct {
		size  int64
		mtime time.Time
		all   []stateEvent
	}
)

func readStateEvents() []stateEvent {
	eventsMu.Lock()
	defer eventsMu.Unlock()
	file, err := openTrusted(eventsFile())
	if err != nil {
		return nil
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() {
		return nil
	}
	if eventsCache.size == fileInfo.Size() && eventsCache.mtime.Equal(fileInfo.ModTime()) {
		return eventsCache.all
	}
	content, err := io.ReadAll(io.LimitReader(file, stateEventsMax+1))
	if err != nil {
		return nil
	}
	var all []stateEvent
	for _, rawLine := range splitLines(content) {
		var event stateEvent
		if json.Unmarshal(rawLine, &event) == nil && event.Session != "" {
			all = append(all, event)
		}
	}
	eventsCache.size, eventsCache.mtime, eventsCache.all = fileInfo.Size(), fileInfo.ModTime(), all
	return all
}

// subagentsFromEvents rejoue les événements d'une session : known faux si aucun hook ne l'a vue (session ouverte avant
// l'installation des hooks), running vrai tant qu'un SubagentStart n'a pas son SubagentStop.
func subagentsFromEvents(events []stateEvent, sessionID string) (running, known bool) {
	started := map[string]bool{}
	for _, event := range events {
		if event.Session != sessionID {
			continue
		}
		known = true
		switch event.Event {
		case "SessionStart", "SessionEnd":
			clear(started)
		case "SubagentStart":
			started[event.Agent] = true
		case "SubagentStop":
			delete(started, event.Agent)
		}
		if (event.Event == "Stop" || event.Event == "SubagentStop") && event.Subagents == 0 {
			clear(started) // la liste de Claude Code fait foi sur un compteur resté bloqué
		}
	}
	return len(started) > 0, known
}
