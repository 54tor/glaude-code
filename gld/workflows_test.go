package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func wfFixture(t *testing.T) (p, base string) {
	d := filepath.Join(t.TempDir(), "-proj")
	base = filepath.Join(d, "11111111-2222-3333-4444-555555555555")
	for _, s := range []string{"workflows", "subagents/workflows/wf_run-2"} {
		if err := os.MkdirAll(filepath.Join(base, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return base + ".jsonl", base
}

func TestSessionWorkflows(t *testing.T) {
	p, base := wfFixture(t)
	done := `{"runId":"wf_abc-123","taskId":"t1","workflowName":"audit","status":"completed","startTime":2000,"durationMs":5000,"totalTokens":900,"totalToolCalls":4,
"script":"x","result":{"a":1},"phases":[{"title":"One","detail":"d"}],"logs":["hi"],
"workflowProgress":[{"type":"workflow_phase","index":0,"title":"One"},{"type":"workflow_agent","label":"a1","phaseTitle":"One","state":"done","tokens":500,"toolCalls":2,"durationMs":3000,"lastToolName":"Read"},{"type":"workflow_agent","label":"a2","phaseTitle":"One","state":"done","tokens":400}]}`
	os.WriteFile(filepath.Join(base, "workflows", "wf_abc-123.json"), []byte(done), 0o600)
	os.WriteFile(filepath.Join(base, "workflows", "wf_bad.json"), []byte("{oups"), 0o600)
	os.WriteFile(filepath.Join(base, "workflows", "x..json"), []byte("{}"), 0o600)
	ad := filepath.Join(base, "subagents/workflows/wf_run-2")
	for _, a := range []struct{ id, phase string }{{"a1", "P"}, {"b2", "P"}} {
		os.WriteFile(filepath.Join(ad, "agent-"+a.id+".meta.json"), []byte(`{"description":"job `+a.id+`","workflowPhase":"`+a.phase+`"}`), 0o600)
		os.WriteFile(filepath.Join(ad, "agent-"+a.id+".jsonl"), []byte("{}"), 0o600)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(ad, "agent-b2.jsonl"), old, old)

	got := sessionWorkflows(p)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	r := got[0] // le run en cours date d'aujourd'hui, l'autre de 1970
	if r.RunID != "wf_run-2" || r.Status != "running" || len(r.Agents) != 2 || r.Agents[0].State != "running" || r.Agents[1].State != "done" || len(r.Phases) != 1 {
		t.Fatalf("run en cours : %+v", r)
	}
	f := got[1]
	if f.RunID != "wf_abc-123" || f.TaskID != "t1" || f.Name != "audit" || f.TotalTokens != 900 || len(f.Agents) != 2 || f.Agents[0].LastTool != "Read" || f.Agents[0].Phase != "One" || f.Logs[0] != "hi" {
		t.Fatalf("run terminé : %+v", f)
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
