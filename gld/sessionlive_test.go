package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tline(typ, ts, msg string) string {
	return fmt.Sprintf(`{"type":%q,"timestamp":%q,"message":%s}`, typ, ts, msg) + "\n"
}

func TestEtatEnDirect(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	assistantTool := tline("assistant", ts, `{"model":"claude-opus-4","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"…"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}],"usage":{"input_tokens":2,"output_tokens":300,"cache_read_input_tokens":480000,"cache_creation_input_tokens":2000}}`)
	result := tline("user", ts, `{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}`)
	done := tline("assistant", ts, `{"model":"claude-opus-4","stop_reason":"end_turn","content":[{"type":"text","text":"fini"}],"usage":{"input_tokens":3,"output_tokens":50,"cache_read_input_tokens":1000,"cache_creation_input_tokens":0}}`)
	tp := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	put(t, tp, `{"cwd":"/tmp"}`+"\n"+tline("user", ts, `{"role":"user","content":"salut"}`)+assistantTool, old)
	put(t, filepath.Join(cfg, "sessions", "1.json"), fmt.Sprintf(`{"pid":%d,"sessionId":%q,"status":"busy"}`, os.Getpid(), id1), old)

	st := sessionLive("-p", id1)
	if !st.Open || !st.Thinking || !strings.HasPrefix(st.Activity, "exécute Bash : go test") || st.Context != 482002 || st.Out != 300 || st.Model != "claude-opus-4" {
		t.Fatalf("outil en cours : %+v", st)
	}
	put(t, tp, `{"cwd":"/tmp"}`+"\n"+assistantTool+result, old)
	if st = sessionLive("-p", id1); st.Activity != "réfléchit au résultat" || !st.Thinking {
		t.Fatalf("résultat reçu : %+v", st)
	}
	put(t, tp, `{"cwd":"/tmp"}`+"\n"+assistantTool+result+done, old)
	put(t, filepath.Join(cfg, "sessions", "1.json"), fmt.Sprintf(`{"pid":%d,"sessionId":%q,"status":"idle"}`, os.Getpid(), id1), old)
	if st = sessionLive("-p", id1); st.Activity != "attend votre message" || st.Thinking || st.Status != "idle" {
		t.Fatalf("en attente : %+v", st)
	}
	// session fermée : aucun processus à ce pid
	put(t, filepath.Join(cfg, "sessions", "1.json"), fmt.Sprintf(`{"pid":%d,"sessionId":%q,"status":"busy"}`, 2147483000, id1), old)
	if st = sessionLive("-p", id1); st.Open || st.Thinking {
		t.Fatalf("session fermée : %+v", st)
	}
	// sous-agents
	sub := filepath.Join(cfg, "projects", "-p", id1, "subagents")
	put(t, filepath.Join(sub, "agent-aaa.meta.json"), `{"agentType":"Explore","description":"Chercher les routes","finished":false}`, old)
	put(t, filepath.Join(sub, "agent-aaa.jsonl"), assistantTool, 0)
	put(t, filepath.Join(sub, "agent-bbb.meta.json"), `{"agentType":"web-fetch","description":"Lire une page","finished":true}`, old)
	put(t, filepath.Join(sub, "agent-bbb.jsonl"), done, old)
	subs := sessionLive("-p", id1).Subs
	if len(subs) != 2 || !subs[0].Running || subs[0].Desc != "Chercher les routes" || !strings.HasPrefix(subs[0].Activity, "exécute Bash") || subs[1].Running || subs[1].Activity != "terminé" {
		t.Fatalf("sous-agents : %+v", subs)
	}
	if st := sessionLive("..", id1); st.Open && st.Activity != "" {
		t.Fatal("dossier invalide accepté")
	}
}

func TestCompte(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, ".claude.json"), `{"oauthAccount":{"emailAddress":"a@b.c","organizationType":"claude_max","organizationRateLimitTier":"default_claude_max_20x","hasExtraUsageEnabled":true,"accountUuid":"secret-id"},"cachedUsageUtilization":{"fetchedAtMs":1792568161473,"utilization":{"five_hour":{"utilization":21,"resets_at":"2026-10-21T08:50:00+00:00"},"seven_day":{"utilization":49,"resets_at":"x"},"seven_day_opus":null,"inconnue":{"utilization":1.5,"limit_dollars":250,"used_dollars":3.2}}}}`, old)
	put(t, filepath.Join(cfg, "stats-cache.json"), `{"lastComputedDate":"2026-10-20","totalSessions":10,"totalMessages":99,"dailyActivity":[{"date":"2026-10-19","messageCount":5,"sessionCount":2,"toolCallCount":3}],"dailyModelTokens":[{"date":"2026-10-19","tokensByModel":{"m":1000}}],"modelUsage":{"m":{"inputTokens":1,"outputTokens":2,"cacheReadInputTokens":3,"cacheCreationInputTokens":4}}}`, old)
	a := accountInfo()
	if a.Email != "a@b.c" || a.Tier != "default_claude_max_20x" || !a.ExtraUsage || len(a.Limits) != 3 || a.Limits[0].Key != "five_hour" || a.Limits[0].Percent != 21 || a.Limits[2].Used == nil || a.LimitsAt != 1792568161.473 {
		t.Fatalf("compte : %+v", a)
	}
	if a.TotalSess != 10 || len(a.Days) != 1 || a.Days[0].Tokens["m"] != 1000 || len(a.Models) != 1 || a.Models[0].Out != 2 {
		t.Fatalf("activité : %+v", a)
	}
	if b, _ := jsonOf(a); strings.Contains(b, "secret-id") {
		t.Fatal("identifiant interne exposé")
	}
	// fichiers absents : réponse vide mais valide
	use(t.TempDir())
	t.Setenv("HOME", t.TempDir()) // sinon le repli lirait le vrai ~/.claude.json
	if a := accountInfo(); a.Email != "" || a.Limits == nil {
		t.Fatalf("sans fichiers : %+v", a)
	}
}

func jsonOf(v any) (string, error) { b, err := json.Marshal(v); return string(b), err }

func TestRafraichirLesLimites(t *testing.T) {
	reset := func() {
		usageMu.Lock()
		usageLast, usageBusy = time.Time{}, false
		usageMu.Unlock()
	}
	use(t.TempDir()) // le journal du rafraîchissement ne doit pas tomber dans le dossier du paquet
	old := usageStart
	defer func() { usageWG.Wait(); usageStart = old; reset() }()
	calls := make(chan struct{}, 4)
	usageStart = func() error { calls <- struct{}{}; return nil }
	reset()
	fresh := float64(time.Now().Unix())
	if refreshUsage(fresh, false) {
		t.Fatal("relevé récent : rien à faire")
	}
	if !refreshUsage(fresh-3600, false) {
		t.Fatal("relevé vieux d'une heure : doit partir")
	}
	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("la commande n'a pas été lancée")
	}
	usageWG.Wait() // la goroutine a journalisé et rendu la main
	if refreshUsage(fresh-3600, false) {
		t.Fatal("deux lancements à moins d'une minute")
	}
	reset()
	if !refreshUsage(fresh, true) {
		t.Fatal("refresh forcé ignoré")
	}
	<-calls
	usageWG.Wait()
}

func TestMessagesEnAttente(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	ev := `{"type":"queue-operation","operation":"enqueue","timestamp":"t1","content":"premier"}
{"type":"queue-operation","operation":"enqueue","timestamp":"t2","content":"deuxième"}
{"type":"queue-operation","operation":"dequeue","timestamp":"t3"}
{"type":"queue-operation","operation":"enqueue","timestamp":"t4","content":"repris"}
{"type":"queue-operation","operation":"remove","timestamp":"t5","content":"repris"}
{"type":"queue-operation","operation":"enqueue","timestamp":"t6","content":"<task-notification>x</task-notification>"}
`
	os.WriteFile(p, []byte(ev), 0o600)
	if q := pendingQueue(p); len(q) != 1 || q[0].T != "deuxième" {
		t.Fatalf("file d'attente : %v", q)
	}
}

func TestFileDAttenteAuDelaDeLaFenetre(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	filler := strings.Repeat(`{"type":"assistant","message":"`+strings.Repeat("x", 1000)+`"}`+"\n", 600) // ~600 Ko
	ev := `{"type":"queue-operation","operation":"enqueue","timestamp":"t1","content":"A"}` + "\n" + filler +
		`{"type":"queue-operation","operation":"enqueue","timestamp":"t2","content":"B"}` + "\n" +
		`{"type":"queue-operation","operation":"dequeue","timestamp":"t3"}` + "\n"
	os.WriteFile(p, []byte(ev), 0o600)
	if q := pendingQueue(p); len(q) != 1 || q[0].T != "B" {
		t.Fatalf("file d'attente : %v", q)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"queue-operation","operation":"enqueue","timestamp":"t4","content":"C"}` + "\n" + `{"type":"queue-op`)
	f.Close()
	if q := pendingQueue(p); len(q) != 2 || q[1].T != "C" {
		t.Fatalf("après ajout : %v", q)
	}
}
