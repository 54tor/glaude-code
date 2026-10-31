package main

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

var teamNameRe = regexp.MustCompile("^session-[0-9a-f]{8}$")

// Le pane finit en argument de « tmux -t » : on n'accepte que %N.
var teamPaneRe = regexp.MustCompile(`^%[0-9]+$`)

const (
	teamFileMax  = 1 << 20
	teamInboxMax = 64
	teamTasksMax = 200
)

type TeamMember struct {
	Name, AgentID, Pane, Backend string
	Lead, Active                 bool
}

type TeamTask struct{ ID, Title, Status string }

type InboxMsg struct {
	ID, From, To, Text, Summary, Kind string
	At                                int64
	Read                              bool
}

type TeamFiles struct {
	Name, LeadSessionID string
	LeadPID             int
	Members             []TeamMember
	Tasks               []TeamTask
	Inbox               []InboxMsg
	Unknown             []string
}

func str(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return value
}

func boolean(fields map[string]any, key string) bool {
	value, _ := fields[key].(bool)
	return value
}

// readJSONFile : lit un fichier régulier (pas un lien) borné à 1 Mio et le décode en any.
func readJSONFile(path string) (any, bool) {
	fileInfo, err := os.Lstat(path)
	if err != nil || !fileInfo.Mode().IsRegular() {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, teamFileMax+1))
	if err != nil || len(content) > teamFileMax {
		return nil, false
	}
	var parsedJSON any
	if json.Unmarshal(content, &parsedJSON) != nil {
		return nil, false
	}
	return parsedJSON, true
}

// jsonFiles : les *.json directement dans dir, triés, sans suivre les liens ; les liens et le surplus vont dans unknown.
func jsonFiles(dir, rel string, maxFiles int, unknown *[]string) []string {
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return nil
	}
	if !dirInfo.IsDir() {
		*unknown = append(*unknown, rel)
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			*unknown = append(*unknown, rel+"/"+entry.Name())
			continue
		}
		if len(names) >= maxFiles {
			*unknown = append(*unknown, rel+"/"+entry.Name())
			continue
		}
		names = append(names, entry.Name())
	}
	return names
}

func readTeam(cfgRoot, name string) (TeamFiles, error) {
	if !teamNameRe.MatchString(name) {
		return TeamFiles{}, terr("srv.teams_files.nomInvalide")
	}
	dir := filepath.Join(cfgRoot, "teams", name)
	if dirInfo, err := os.Lstat(dir); err != nil || !dirInfo.IsDir() {
		return TeamFiles{}, terr("srv.teams_files.equipeIntrouvable")
	}
	teamFiles := TeamFiles{Name: name}
	cfgRel := "teams/" + name + "/config.json"
	if config, isReadable := readJSONFile(filepath.Join(dir, "config.json")); !isReadable {
		teamFiles.Unknown = append(teamFiles.Unknown, cfgRel)
	} else if configMap, isObject := config.(map[string]any); !isObject {
		teamFiles.Unknown = append(teamFiles.Unknown, cfgRel)
	} else {
		teamFiles.LeadSessionID = str(configMap, "leadSessionId")
		if leadProcess, hasLeadProcess := configMap["leadProcess"].(map[string]any); hasLeadProcess {
			if pid, hasPID := leadProcess["pid"].(float64); hasPID {
				teamFiles.LeadPID = int(pid)
			}
		}
		members, _ := configMap["members"].([]any)
		for _, rawMember := range members {
			memberFields, isObject := rawMember.(map[string]any)
			if !isObject {
				continue
			}
			pane := str(memberFields, "tmuxPaneId")
			member := TeamMember{
				Name: str(memberFields, "name"), AgentID: str(memberFields, "agentId"), Backend: str(memberFields, "backendType"),
				Lead:   str(memberFields, "agentType") == "team-lead" || pane == "leader",
				Active: boolean(memberFields, "isActive"),
			}
			if teamPaneRe.MatchString(pane) {
				member.Pane = pane
			}
			teamFiles.Members = append(teamFiles.Members, member)
		}
	}

	inRel := "teams/" + name + "/inboxes"
	for _, fileName := range jsonFiles(filepath.Join(dir, "inboxes"), inRel, teamInboxMax, &teamFiles.Unknown) {
		parsedJSON, isReadable := readJSONFile(filepath.Join(dir, "inboxes", fileName))
		messages, isArray := parsedJSON.([]any)
		if !isReadable || !isArray {
			teamFiles.Unknown = append(teamFiles.Unknown, inRel+"/"+fileName)
			continue
		}
		owner := strings.TrimSuffix(fileName, ".json")
		for _, rawMessage := range messages {
			if messageFields, isObject := rawMessage.(map[string]any); isObject {
				teamFiles.Inbox = append(teamFiles.Inbox, inboxMsg(messageFields, owner))
			}
		}
	}
	sort.SliceStable(teamFiles.Inbox, func(left, right int) bool {
		leftMessage, rightMessage := teamFiles.Inbox[left], teamFiles.Inbox[right]
		if leftMessage.At != rightMessage.At {
			return leftMessage.At < rightMessage.At
		}
		return leftMessage.ID < rightMessage.ID
	})

	tasksRel := "tasks/" + name
	tasksDir := filepath.Join(cfgRoot, "tasks", name)
	for _, fileName := range jsonFiles(tasksDir, tasksRel, teamTasksMax, &teamFiles.Unknown) {
		parsedJSON, isReadable := readJSONFile(filepath.Join(tasksDir, fileName))
		var items []any
		switch typedValue := parsedJSON.(type) {
		case []any:
			items = typedValue
		case map[string]any:
			items = []any{typedValue}
		}
		var got []TeamTask
		valid := isReadable && len(items) > 0
		for _, rawTask := range items {
			task, isValid := taskOf(rawTask)
			if !isValid {
				valid = false
				break
			}
			got = append(got, task)
		}
		if !valid {
			teamFiles.Unknown = append(teamFiles.Unknown, tasksRel+"/"+fileName)
			continue
		}
		teamFiles.Tasks = append(teamFiles.Tasks, got...)
	}
	return teamFiles, nil
}

func taskOf(rawTask any) (TeamTask, bool) {
	fields, isObject := rawTask.(map[string]any)
	if !isObject {
		return TeamTask{}, false
	}
	task := TeamTask{ID: str(fields, "id"), Title: str(fields, "subject"), Status: str(fields, "status")}
	if task.Title == "" {
		task.Title = str(fields, "title")
	}
	return task, task.ID != "" && task.Title != "" && task.Status != ""
}

func inboxMsg(fields map[string]any, owner string) InboxMsg {
	message := InboxMsg{
		ID: str(fields, "msg_id"), From: str(fields, "from"), To: owner, Text: str(fields, "text"),
		Summary: str(fields, "summary"), Kind: str(fields, "type"), Read: boolean(fields, "read"),
	}
	if sentAt, err := time.Parse(time.RFC3339, str(fields, "timestamp")); err == nil {
		message.At = sentAt.UnixMilli()
	}
	if message.Kind == "message" || message.Kind == "" {
		message.Kind = "message"
		var inner map[string]any
		if strings.HasPrefix(strings.TrimSpace(message.Text), "{") && json.Unmarshal([]byte(message.Text), &inner) == nil && str(inner, "type") == "idle_notification" {
			message.Kind = "idle"
			message.Text, _ = inner["result"].(string) // le compte rendu du coéquipier au chef
		}
	}
	return message
}

func listTeams(cfgRoot string) []string {
	entries, _ := os.ReadDir(filepath.Join(cfgRoot, "teams"))
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && teamNameRe.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}
