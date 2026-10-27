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

func str(m map[string]any, k string) string   { s, _ := m[k].(string); return s }
func boolean(m map[string]any, k string) bool { b, _ := m[k].(bool); return b }

// readJSONFile : lit un fichier régulier (pas un lien) borné à 1 Mio et le décode en any.
func readJSONFile(path string) (any, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, teamFileMax+1))
	if err != nil || len(b) > teamFileMax {
		return nil, false
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil, false
	}
	return v, true
}

// jsonFiles : les *.json directement dans dir, triés, sans suivre les liens ; les liens et le surplus vont dans unknown.
func jsonFiles(dir, rel string, max int, unknown *[]string) []string {
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil
	}
	if !fi.IsDir() {
		*unknown = append(*unknown, rel)
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if !e.Type().IsRegular() {
			*unknown = append(*unknown, rel+"/"+e.Name())
			continue
		}
		if len(out) >= max {
			*unknown = append(*unknown, rel+"/"+e.Name())
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

func readTeam(cfgRoot, name string) (TeamFiles, error) {
	if !teamNameRe.MatchString(name) {
		return TeamFiles{}, terr("srv.teams_files.nomInvalide")
	}
	dir := filepath.Join(cfgRoot, "teams", name)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return TeamFiles{}, terr("srv.teams_files.equipeIntrouvable")
	}
	tf := TeamFiles{Name: name}
	cfgRel := "teams/" + name + "/config.json"
	if v, ok := readJSONFile(filepath.Join(dir, "config.json")); !ok {
		tf.Unknown = append(tf.Unknown, cfgRel)
	} else if c, ok := v.(map[string]any); !ok {
		tf.Unknown = append(tf.Unknown, cfgRel)
	} else {
		tf.LeadSessionID = str(c, "leadSessionId")
		if lp, ok := c["leadProcess"].(map[string]any); ok {
			if p, ok := lp["pid"].(float64); ok {
				tf.LeadPID = int(p)
			}
		}
		ms, _ := c["members"].([]any)
		for _, x := range ms {
			m, ok := x.(map[string]any)
			if !ok {
				continue
			}
			pane := str(m, "tmuxPaneId")
			mem := TeamMember{
				Name: str(m, "name"), AgentID: str(m, "agentId"), Backend: str(m, "backendType"),
				Lead:   str(m, "agentType") == "team-lead" || pane == "leader",
				Active: boolean(m, "isActive"),
			}
			if teamPaneRe.MatchString(pane) {
				mem.Pane = pane
			}
			tf.Members = append(tf.Members, mem)
		}
	}

	inRel := "teams/" + name + "/inboxes"
	for _, fn := range jsonFiles(filepath.Join(dir, "inboxes"), inRel, teamInboxMax, &tf.Unknown) {
		v, ok := readJSONFile(filepath.Join(dir, "inboxes", fn))
		arr, isArr := v.([]any)
		if !ok || !isArr {
			tf.Unknown = append(tf.Unknown, inRel+"/"+fn)
			continue
		}
		owner := strings.TrimSuffix(fn, ".json")
		for _, x := range arr {
			if m, ok := x.(map[string]any); ok {
				tf.Inbox = append(tf.Inbox, inboxMsg(m, owner))
			}
		}
	}
	sort.SliceStable(tf.Inbox, func(i, j int) bool {
		a, b := tf.Inbox[i], tf.Inbox[j]
		if a.At != b.At {
			return a.At < b.At
		}
		return a.ID < b.ID
	})

	tkRel := "tasks/" + name
	tkDir := filepath.Join(cfgRoot, "tasks", name)
	for _, fn := range jsonFiles(tkDir, tkRel, teamTasksMax, &tf.Unknown) {
		v, ok := readJSONFile(filepath.Join(tkDir, fn))
		var items []any
		switch t := v.(type) {
		case []any:
			items = t
		case map[string]any:
			items = []any{t}
		}
		var got []TeamTask
		valid := ok && len(items) > 0
		for _, x := range items {
			t, good := taskOf(x)
			if !good {
				valid = false
				break
			}
			got = append(got, t)
		}
		if !valid {
			tf.Unknown = append(tf.Unknown, tkRel+"/"+fn)
			continue
		}
		tf.Tasks = append(tf.Tasks, got...)
	}
	return tf, nil
}

func taskOf(x any) (TeamTask, bool) {
	m, ok := x.(map[string]any)
	if !ok {
		return TeamTask{}, false
	}
	t := TeamTask{ID: str(m, "id"), Title: str(m, "subject"), Status: str(m, "status")}
	if t.Title == "" {
		t.Title = str(m, "title")
	}
	return t, t.ID != "" && t.Title != "" && t.Status != ""
}

func inboxMsg(m map[string]any, owner string) InboxMsg {
	msg := InboxMsg{
		ID: str(m, "msg_id"), From: str(m, "from"), To: owner, Text: str(m, "text"),
		Summary: str(m, "summary"), Kind: str(m, "type"), Read: boolean(m, "read"),
	}
	if ts, err := time.Parse(time.RFC3339, str(m, "timestamp")); err == nil {
		msg.At = ts.UnixMilli()
	}
	if msg.Kind == "message" || msg.Kind == "" {
		msg.Kind = "message"
		var inner map[string]any
		if strings.HasPrefix(strings.TrimSpace(msg.Text), "{") && json.Unmarshal([]byte(msg.Text), &inner) == nil && str(inner, "type") == "idle_notification" {
			msg.Kind = "idle"
		}
	}
	return msg
}

func listTeams(cfgRoot string) []string {
	ents, _ := os.ReadDir(filepath.Join(cfgRoot, "teams"))
	var out []string
	for _, e := range ents {
		if e.IsDir() && teamNameRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
