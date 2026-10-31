package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func wfFixture(t *testing.T) (path, base string) {
	dir := filepath.Join(t.TempDir(), "-proj")
	base = filepath.Join(dir, "11111111-2222-3333-4444-555555555555")
	for _, text := range []string{"workflows", "subagents/workflows/wf_run-2"} {
		if err := os.MkdirAll(filepath.Join(base, text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return base + ".jsonl", base
}

func TestSessionWorkflows(t *testing.T) {
	path, base := wfFixture(t)
	done := `{"runId":"wf_abc-123","taskId":"t1","workflowName":"audit","status":"completed","startTime":2000,"durationMs":5000,"totalTokens":900,"totalToolCalls":4,
"script":"x","result":{"a":1},"phases":[{"title":"One","detail":"d"}],"logs":["hi"],
"workflowProgress":[{"type":"workflow_phase","index":0,"title":"One"},{"type":"workflow_agent","label":"a1","phaseTitle":"One","state":"done","tokens":500,"toolCalls":2,"durationMs":3000,"lastToolName":"Read"},{"type":"workflow_agent","label":"a2","phaseTitle":"One","state":"done","tokens":400}]}`
	os.WriteFile(filepath.Join(base, "workflows", "wf_abc-123.json"), []byte(done), 0o600)
	os.WriteFile(filepath.Join(base, "workflows", "wf_bad.json"), []byte("{oups"), 0o600)
	os.WriteFile(filepath.Join(base, "workflows", "x..json"), []byte("{}"), 0o600)
	ad := filepath.Join(base, "subagents/workflows/wf_run-2")
	for _, record := range []struct{ id, phase string }{{"a1", "P"}, {"b2", "P"}} {
		os.WriteFile(filepath.Join(ad, "agent-"+record.id+".meta.json"), []byte(`{"description":"job `+record.id+`","workflowPhase":"`+record.phase+`"}`), 0o600)
		os.WriteFile(filepath.Join(ad, "agent-"+record.id+".jsonl"), []byte("{}"), 0o600)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(ad, "agent-b2.jsonl"), old, old)

	got := sessionWorkflows(path)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	workflow := got[0] // le run en cours date d'aujourd'hui, l'autre de 1970
	if workflow.RunID != "wf_run-2" || workflow.Status != "running" || len(workflow.Agents) != 2 || workflow.Agents[0].State != "running" || workflow.Agents[1].State != "done" || len(workflow.Phases) != 1 {
		t.Fatalf("run en cours : %+v", workflow)
	}
	localWorkflow := got[1]
	if localWorkflow.RunID != "wf_abc-123" || localWorkflow.TaskID != "t1" || localWorkflow.Name != "audit" || localWorkflow.TotalTokens != 900 || len(localWorkflow.Agents) != 2 || localWorkflow.Agents[0].LastTool != "Read" || localWorkflow.Agents[0].Phase != "One" || localWorkflow.Logs[0] != "hi" {
		t.Fatalf("run terminé : %+v", localWorkflow)
	}
	if _, ok := loadWorkflow(base, "../etc"); ok {
		t.Fatal("runId invalide accepté")
	}
	if _, ok := loadWorkflow(base, "wf_bad"); ok {
		t.Fatal("json corrompu accepté")
	}
	if got := sessionWorkflows(filepath.Join(t.TempDir(), "none.jsonl")); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestRunningWorkflowUsesJournal(t *testing.T) {
	_, base := wfFixture(t)
	ad := filepath.Join(base, "subagents/workflows/wf_run-2")
	for _, id := range []string{"a1", "b2"} {
		os.WriteFile(filepath.Join(ad, "agent-"+id+".meta.json"), []byte(`{"description":"job `+id+`","workflowPhase":"P"}`), 0o600)
		os.WriteFile(filepath.Join(ad, "agent-"+id+".jsonl"), []byte("{}"), 0o600) // récents tous les deux
	}
	os.WriteFile(filepath.Join(ad, "journal.jsonl"), []byte(`{"type":"launched"}
{"type":"started","agentId":"a1"}
{"type":"result","agentId":"a1","result":{}}
{"type":"started","agentId":"b2"}
`), 0o600)
	workflow, ok := runningWorkflow(base, "wf_run-2")
	if !ok || workflow.Status != "running" || workflow.Agents[0].State != "done" || workflow.Agents[1].State != "running" {
		t.Fatalf("%+v", workflow)
	}
}
