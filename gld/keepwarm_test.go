package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openAndIdle : les sessions données sont ouvertes dans tmux et attendent un message.
func openAndIdle(t *testing.T, ids ...string) {
	oldLive, oldWait := liveSessions, waiting
	t.Cleanup(func() { liveSessions, waiting = oldLive, oldWait })
	m := map[string]bool{}
	for _, id := range ids {
		m[id] = true
	}
	liveSessions = func() map[string]bool { return m }
	waiting = func() []string { return ids }
}

func TestKeepWarm(t *testing.T) {
	h, cfg, _ := web(t)
	openAndIdle(t, id1)
	ok(t, h, "/keepwarm", `{"dir":"-p","id":"`+id1+`","on":true,"max":2}`, true, "activé")
	ok(t, h, "/keepwarm", `{"dir":"-p","id":"`+id2+`","on":true}`, false, "") // session inexistante
	if _, err := os.Stat(filepath.Join(cfg, "gld-web-keepwarm.json")); err != nil {
		t.Fatal("état non enregistré")
	}

	var pings int
	old := warmPing
	defer func() { warmPing = old }()
	warmPing = func(id string) error { pings++; return nil }
	mtime := func(now time.Time, ago time.Duration) {
		// la date d'une session est celle de son dernier message (horodatage), plafonnée au mtime du fichier
		p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
		at := now.Add(-ago)
		os.WriteFile(p, []byte(`{"type":"user","cwd":"/p","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+`","message":{"content":"x"}}`+"\n"), 0o600)
		os.Chtimes(p, at, at)
		forgetInfo(p)
	}
	now := time.Now()

	mtime(now, 10*time.Minute) // cache encore chaud (50 min restantes) : rien
	warmTick(now)
	if pings != 0 {
		t.Fatal("ping trop tôt")
	}
	mtime(now, 57*time.Minute) // 3 min restantes : ping
	warmTick(now)
	if pings != 1 || warmState[id1].Pings != 1 {
		t.Fatalf("%d %+v", pings, warmState[id1])
	}
	warmTick(now.Add(time.Minute)) // trop proche du dernier ping
	if pings != 1 {
		t.Fatal("deux pings en moins de 4 minutes")
	}
	later := now.Add(50 * time.Minute) // le ping a rafraîchi la session ; une heure plus tard elle redescend sous 5 minutes
	mtime(later, 57*time.Minute)
	warmTick(later)
	mtime(later.Add(50*time.Minute), 57*time.Minute)
	warmTick(later.Add(50 * time.Minute)) // limite atteinte
	if pings != 2 || warmState[id1].On {
		t.Fatalf("limite de 2 pings : %d %+v", pings, warmState[id1])
	}

	// une session déjà froide arrête le maintien sans rien envoyer
	ok(t, h, "/keepwarm", `{"dir":"-p","id":"`+id1+`","on":true}`, true, "activé")
	mtime(now, 2*time.Hour)
	warmTick(now)
	if pings != 2 || warmState[id1].On || warmState[id1].Note == "" {
		t.Fatalf("session froide : %d %+v", pings, warmState[id1])
	}
}

// Les pings d'un passage se suivent (jusqu'à 5 min chacun) : une session désactivée entre-temps n'est plus pinguée.
func TestKeepWarmStoppedDuringTick(t *testing.T) {
	h, cfg, _ := web(t)
	openAndIdle(t, id1, id3)
	now := time.Now()
	for _, id := range []string{id1, id3} {
		p := filepath.Join(cfg, "projects", "-p", id+".jsonl")
		at := now.Add(-57 * time.Minute)
		os.WriteFile(p, []byte(`{"type":"user","cwd":"/p","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+`","message":{"content":"x"}}`+"\n"), 0o600)
		os.Chtimes(p, at, at)
		forgetInfo(p)
		ok(t, h, "/keepwarm", `{"dir":"-p","id":"`+id+`","on":true}`, true, "activé")
	}
	var pinged []string
	old := warmPing
	defer func() { warmPing = old }()
	warmPing = func(id string) error {
		pinged = append(pinged, id)
		other := map[string]string{id1: id3, id3: id1}[id]
		ok(t, h, "/keepwarm", `{"dir":"-p","id":"`+other+`","on":false}`, true, "arrêté")
		return nil
	}
	warmTick(now)
	if len(pinged) != 1 {
		t.Fatalf("session arrêtée pendant le passage pinguée quand même : %v", pinged)
	}
}

// Une session fermée n'est jamais pinguée : `claude -p` ne relirait pas le cache du mode interactif.
func TestKeepWarmSessionFermee(t *testing.T) {
	h, cfg, _ := web(t)
	openAndIdle(t)
	ok(t, h, "/keepwarm", `{"dir":"-p","id":"`+id1+`","on":true}`, false, "session fermée")

	openAndIdle(t, id1)
	ok(t, h, "/keepwarm", `{"dir":"-p","id":"`+id1+`","on":true}`, true, "activé")
	openAndIdle(t) // fermée ensuite
	pings := 0
	old := warmPing
	defer func() { warmPing = old }()
	warmPing = func(string) error { pings++; return nil }
	now := time.Now()
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	at := now.Add(-57 * time.Minute)
	os.WriteFile(p, []byte(`{"type":"user","cwd":"/p","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+`","message":{"content":"x"}}`+"\n"), 0o600)
	os.Chtimes(p, at, at)
	forgetInfo(p)
	warmTick(now)
	if pings != 0 || warmState[id1].On || warmState[id1].Note != closedNote {
		t.Fatalf("session fermée pinguée : %d %+v", pings, warmState[id1])
	}
}
