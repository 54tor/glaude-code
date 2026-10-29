package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBgTasks(t *testing.T) {
	d := filepath.Join(t.TempDir(), "-proj")
	os.MkdirAll(d, 0o755)
	p := filepath.Join(d, "11111111-2222-3333-4444-555555555555.jsonl")
	lines := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"sleep 9","description":"Attente","run_in_background":true}}]}}
{"type":"user","timestamp":"T1","message":{"content":[{"tool_use_id":"tu1","type":"tool_result"}]},"toolUseResult":{"backgroundTaskId":"abc1"}}
{"type":"user","timestamp":"T2","message":{"content":[{"tool_use_id":"tu2","type":"tool_result"}]},"toolUseResult":{"backgroundTaskId":"abc2"}}
{"type":"user","timestamp":"T5","message":{"content":[{"tool_use_id":"tu9","type":"tool_result","content":"{\"message\":\"Successfully stopped task: abc2 (sleep 9)\"}"}]}}
{"type":"queue-operation","operation":"enqueue","timestamp":"T3","content":"<task-notification>\n<task-id>abc1</task-id>\n<status>completed</status>\n<summary>fini</summary>\n</task-notification>"}
{"type":"queue-operation","operation":"enqueue","timestamp":"T4","content":"<task-notification>\n<task-id>mon9</task-id>\n<status>failed</status>\n<summary>Monitor KO</summary>\n</task-notification>"}
`
	os.WriteFile(p, []byte(lines), 0o600)
	got := bgTasks(p, true)
	if len(got) != 3 || got[0].ID != "abc1" || got[0].Status != "completed" || got[0].Desc != "Attente" || got[0].End != "T3" ||
		got[1].ID != "abc2" || got[1].Status != "killed" || got[1].End != "T5" || got[2].ID != "mon9" || got[2].Status != "failed" || got[2].Desc != "Monitor KO" {
		t.Fatalf("%+v", got)
	}
	if _, ok := bgOutput(p, "abc1"); ok {
		t.Fatal("sortie lue sans fichier au chemin attendu")
	}
}

func TestBgOutRe(t *testing.T) {
	ok := "/tmp/claude-1000/-home-x/88ff150d-1017-4dc6-9be0-350f74f97c0a/tasks/b2yjugdt9.output"
	if !bgOutRe.MatchString(ok) || bgOutRe.MatchString("/etc/passwd") || bgOutRe.MatchString(ok+"/../x") || bgOutRe.MatchString("/tmp/claude-1/../../etc/tasks/a.output") {
		t.Fatal("forme du chemin de sortie")
	}
}

func TestBgOutputOKRefuse(t *testing.T) {
	for _, p := range []string{"/etc/passwd", "/tmp/claude-1/x/../../etc/y/11111111-2222-3333-4444-555555555555/tasks/a.output", "/tmp/claude-1000/-p/11111111-2222-3333-4444-555555555555/tasks/a.output/", ""} {
		if bgOutputOK(p) {
			t.Errorf("accepté : %q", p)
		}
	}
}

func TestBgKilled(t *testing.T) {
	d := t.TempDir()
	for body, want := range map[string]bool{"ok\n\n[killed]\n": true, "[killed]": true, "fin": false, "": false} {
		p := filepath.Join(d, "o")
		os.WriteFile(p, []byte(body), 0o600)
		if bgKilled(p) != want {
			t.Fatalf("%q", body)
		}
	}
}
