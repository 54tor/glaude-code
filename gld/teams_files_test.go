package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tName = "session-9d087000"

func teamRoot(t *testing.T, fixtures bool) (string, string) {
	root := t.TempDir()
	d := filepath.Join(root, "teams", tName)
	if err := os.MkdirAll(filepath.Join(d, "inboxes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if fixtures {
		for src, dst := range map[string]string{"config.json": "config.json", "inbox-ada.json": "inboxes/ada.json", "inbox-team-lead.json": "inboxes/team-lead.json"} {
			b, err := os.ReadFile(filepath.Join("testdata", "teams", src))
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(d, dst), b, 0o644)
		}
	}
	return root, d
}

func TestReadTeamFixture(t *testing.T) {
	root, _ := teamRoot(t, true)
	tf, err := readTeam(root, tName)
	if err != nil {
		t.Fatal(err)
	}
	if tf.LeadSessionID != "9d087000-13eb-4519-9823-771e3a943e4c" || tf.LeadPID != 3762253 || len(tf.Members) != 4 || len(tf.Unknown) != 0 || len(tf.Tasks) != 0 {
		t.Fatalf("%+v", tf)
	}
	lead, ada := tf.Members[0], tf.Members[1]
	if !lead.Lead || lead.Pane != "" || lead.Backend != "in-process" || ada.Lead || ada.Pane != "%67" || ada.Backend != "tmux" || ada.AgentID != "ada@"+tName {
		t.Fatalf("%+v %+v", lead, ada)
	}
	if len(tf.Inbox) != 2 {
		t.Fatalf("%+v", tf.Inbox)
	}
	m := tf.Inbox[0]
	if m.From != "team-lead" || m.To != "ada" || m.Kind != "message" || m.At == 0 || m.ID == "" || m.Summary == "" || m.Read || tf.Inbox[1].At < m.At {
		t.Fatalf("%+v", m)
	}
}

func TestReadTeamBadName(t *testing.T) {
	for _, n := range []string{"../x", "session-ZZ", ""} {
		if _, err := readTeam(t.TempDir(), n); !isMsg(err, "srv.teams_files.nomInvalide") {
			t.Fatalf("%q: %v", n, err)
		}
	}
	if _, err := readTeam(t.TempDir(), tName); !isMsg(err, "srv.teams_files.equipeIntrouvable") {
		t.Fatal("absent")
	}
}

func TestReadTeamTruncatedJSON(t *testing.T) {
	root, d := teamRoot(t, true)
	os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"name": "x", "members": [{"na`), 0o644)
	tf, err := readTeam(root, tName)
	if err != nil || len(tf.Unknown) != 1 || tf.Unknown[0] != "teams/"+tName+"/config.json" || len(tf.Inbox) != 2 {
		t.Fatalf("%v %+v", err, tf)
	}
}

func TestReadTeamExtraFields(t *testing.T) {
	root, d := teamRoot(t, false)
	os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"futur":{"a":1},"members":[{"name":"a","tmuxPaneId":"%3","zzz":[1]}]}`), 0o644)
	tf, err := readTeam(root, tName)
	if err != nil || len(tf.Unknown) != 0 || len(tf.Members) != 1 || tf.Members[0].Pane != "%3" {
		t.Fatalf("%v %+v", err, tf)
	}
}

func TestListTeamsIgnoresStrangeDirs(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"session-bbbbbbbb", "session-aaaaaaaa", "session-ZZ", "autre", "session-cccccccc"} {
		os.MkdirAll(filepath.Join(root, "teams", n), 0o755)
	}
	os.WriteFile(filepath.Join(root, "teams", "session-dddddddd"), nil, 0o644)
	os.Symlink(root, filepath.Join(root, "teams", "session-eeeeeeee"))
	got := strings.Join(listTeams(root), ",")
	if got != "session-aaaaaaaa,session-bbbbbbbb,session-cccccccc" {
		t.Fatal(got)
	}
	if len(listTeams(t.TempDir())) != 0 {
		t.Fatal("vide")
	}
}

func TestReadTeamPaneValidated(t *testing.T) {
	root, d := teamRoot(t, false)
	os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"members":[{"name":"a","tmuxPaneId":"%1; rm -rf"},{"name":"b","tmuxPaneId":"%12"}]}`), 0o644)
	tf, _ := readTeam(root, tName)
	if tf.Members[0].Pane != "" || tf.Members[1].Pane != "%12" {
		t.Fatalf("%+v", tf.Members)
	}
}

func TestReadTeamSymlinkSkipped(t *testing.T) {
	root, d := teamRoot(t, true)
	out := filepath.Join(root, "dehors.json")
	os.WriteFile(out, []byte(`[{"from":"x","msg_id":"z"}]`), 0o644)
	os.Symlink(out, filepath.Join(d, "inboxes", "evil.json"))
	os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	os.Symlink(root, filepath.Join(root, "tasks", tName))
	tf, err := readTeam(root, tName)
	if err != nil || len(tf.Inbox) != 2 || len(tf.Unknown) != 2 || !strings.HasSuffix(tf.Unknown[0], "inboxes/evil.json") || tf.Unknown[1] != "tasks/"+tName {
		t.Fatalf("%v %+v", err, tf)
	}
	// config.json lui-même en lien
	os.Remove(filepath.Join(d, "config.json"))
	cfg := filepath.Join(root, "cfg-dehors.json")
	os.WriteFile(cfg, []byte(`{"members":[{"name":"intrus"}]}`), 0o644)
	os.Symlink(cfg, filepath.Join(d, "config.json"))
	tf, _ = readTeam(root, tName)
	if len(tf.Members) != 0 {
		t.Fatal("lien suivi")
	}
}

func TestReadTeamIdleKind(t *testing.T) {
	root, _ := teamRoot(t, true)
	tf, _ := readTeam(root, tName)
	var idle, msg int
	for _, m := range tf.Inbox {
		switch m.Kind {
		case "idle":
			idle++
		case "message":
			msg++
		}
	}
	if idle != 1 || msg != 1 {
		t.Fatalf("%+v", tf.Inbox)
	}
}

func TestReadTeamWrongTypes(t *testing.T) {
	root, d := teamRoot(t, false)
	os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"members":{"a":1},"leadProcess":"x","leadSessionId":5}`), 0o644)
	os.WriteFile(filepath.Join(d, "inboxes", "a.json"), []byte(`[{"from":3,"timestamp":12,"text":null,"read":"oui","msg_id":[]}, 7, null]`), 0o644)
	os.WriteFile(filepath.Join(d, "inboxes", "b.json"), []byte(`{"pas":"un tableau"}`), 0o644)
	tf, err := readTeam(root, tName)
	if err != nil || len(tf.Inbox) != 1 || tf.Inbox[0].At != 0 || tf.Inbox[0].Kind != "message" || len(tf.Unknown) != 1 {
		t.Fatalf("%v %+v", err, tf)
	}
}

func TestReadTeamTasks(t *testing.T) {
	root, _ := teamRoot(t, false)
	td := filepath.Join(root, "tasks", tName)
	os.MkdirAll(td, 0o755)
	os.WriteFile(filepath.Join(td, "1.json"), []byte(`{"id":"1","subject":"A","status":"pending"}`), 0o644)
	os.WriteFile(filepath.Join(td, "2.json"), []byte(`[{"id":"2","title":"B","status":"done"}]`), 0o644)
	os.WriteFile(filepath.Join(td, "3.json"), []byte(`{"id":1}`), 0o644)
	tf, _ := readTeam(root, tName)
	if len(tf.Tasks) != 2 || tf.Tasks[1].Title != "B" || len(tf.Unknown) != 2 {
		t.Fatalf("%+v", tf)
	}
}
