package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const loopWait = 10 * time.Second

// withHomeCtx : remplace le contexte d'arrêt du serveur par un contexte annulable, restauré à la fin.
func withHomeCtx(t *testing.T) context.CancelFunc {
	t.Helper()
	previous := homeCtx
	cancellable, cancel := context.WithCancel(context.Background())
	homeCtx = cancellable
	t.Cleanup(func() {
		cancel()
		homeCtx = previous
	})
	return cancel
}

// runLoop lance une boucle dans une goroutine et rend un canal fermé quand elle revient.
func runLoop(loop func()) <-chan struct{} {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		loop()
	}()
	return finished
}

func expectReturn(t *testing.T, finished <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-finished:
	case <-time.After(loopWait):
		t.Fatalf("%s ne s'arrête pas à l'annulation du contexte", name)
	}
}

func TestKeepWarmLoopPingsThenStops(t *testing.T) {
	handler, cfg, _ := web(t)
	openAndIdle(t, id1)
	v1ok(t, handler, "PUT", "/v1/keepwarm", `{"dir":"-p","id":"`+id1+`","on":true,"max":2}`, "activé")
	staleAt := time.Now().Add(-57 * time.Minute)
	transcriptPath := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	line := `{"type":"user","cwd":"/p","timestamp":"` + staleAt.UTC().Format(time.RFC3339Nano) + `","message":{"content":"x"}}` + "\n"
	os.WriteFile(transcriptPath, []byte(line), 0o600)
	os.Chtimes(transcriptPath, staleAt, staleAt)
	forgetInfo(transcriptPath)

	pinged := make(chan string, 4)
	previousPing := warmPing
	t.Cleanup(func() { warmPing = previousPing })
	warmPing = func(sessionID string) error { pinged <- sessionID; return nil }

	cancel := withHomeCtx(t)
	finished := runLoop(func() { keepWarmLoop(5 * time.Millisecond) })
	select {
	case sessionID := <-pinged:
		if sessionID != id1 {
			t.Fatalf("ping sur %s", sessionID)
		}
	case <-time.After(loopWait):
		t.Fatal("la boucle n'a pas pingué la session au cache presque froid")
	}
	cancel()
	expectReturn(t, finished, "keepWarmLoop")
}

func TestKeepWarmLoopStopsWithoutTicking(t *testing.T) {
	cancel := withHomeCtx(t)
	cancel()
	pinged := false
	previousPing := warmPing
	t.Cleanup(func() { warmPing = previousPing })
	warmPing = func(string) error { pinged = true; return nil }
	expectReturn(t, runLoop(func() { keepWarmLoop(time.Hour) }), "keepWarmLoop")
	if pinged {
		t.Fatal("ping après annulation")
	}
}

func TestTitleLoopFlushesEndedSession(t *testing.T) {
	previousTitleRun, previousLive, previousRoot := titleRun, liveSessions, root
	t.Cleanup(func() { titleRun, liveSessions, root = previousTitleRun, previousLive, previousRoot })
	root = filepath.Join(t.TempDir(), "projects")
	projectDir := filepath.Join(root, "-p")
	os.MkdirAll(projectDir, 0o755)
	transcriptPath := filepath.Join(projectDir, id1+".jsonl")
	os.WriteFile(transcriptPath, []byte(`{"type":"user","message":{"content":"corrige le login"}}`+"\n"), 0o600)
	titleRun = func(string) (string, error) { return "Bug de login", nil }
	liveSessions = func() map[string]bool { return map[string]bool{} }
	deferTitle(id1, transcriptPath)

	cancel := withHomeCtx(t)
	finished := runLoop(func() { titleLoop(5 * time.Millisecond) })
	deadline := time.Now().Add(loopWait)
	for !hasTitle(transcriptPath) {
		if time.Now().After(deadline) {
			t.Fatal("la boucle n'a pas posé le titre")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	expectReturn(t, finished, "titleLoop")
}

func TestQueueLoopDrainsAndStops(t *testing.T) {
	queue, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	queue.Add("s", "host", "", "un")

	loopContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	finished := runLoop(func() { queueLoop(loopContext, queue) })
	deadline := time.Now().Add(loopWait)
	for len(queue.List("s")) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("la file n'a pas été vidée par la boucle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	expectReturn(t, finished, "queueLoop")
	if len(*sent) != 1 || (*sent)[0] != "s|un" {
		t.Fatalf("envois = %v", *sent)
	}
}

// fakeNotifySend place un notify-send factice dans le PATH : sa présence suffit à armer watchWaiting.
func fakeNotifySend(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	os.WriteFile(filepath.Join(binDir, "notify-send"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", binDir)
}

func captureNotifications(t *testing.T) (*sync.Mutex, *[]string) {
	t.Helper()
	previousNotify := notifySend
	t.Cleanup(func() { notifySend = previousNotify })
	var mu sync.Mutex
	var received []string
	notifySend = func(title, body string) {
		mu.Lock()
		received = append(received, title+"|"+body)
		mu.Unlock()
	}
	return &mu, &received
}

func TestWatchWaitingNotifiesNewWaiter(t *testing.T) {
	fakeNotifySend(t)
	mu, received := captureNotifications(t)
	previousWaiting, previousRoot := waiting, root
	t.Cleanup(func() { waiting, root = previousWaiting, previousRoot })
	root = filepath.Join(t.TempDir(), "projects")
	waiting = func() []string { return []string{id1} }
	previousLastAPI := lastAPI.Load()
	t.Cleanup(func() { lastAPI.Store(previousLastAPI) })
	lastAPI.Store(0)

	cancel := withHomeCtx(t)
	finished := runLoop(func() { watchWaiting(5 * time.Millisecond) })
	deadline := time.Now().Add(loopWait)
	for {
		mu.Lock()
		count := len(*received)
		mu.Unlock()
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("aucune notification pour la session qui attend")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	expectReturn(t, finished, "watchWaiting")
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(*received) != 1 || (*received)[0] != "Claude attend|"+id1 {
		t.Fatalf("une session déjà signalée est renotifiée, ou titre inattendu : %v", *received)
	}
}

func TestWatchWaitingSilentWhenTabOpen(t *testing.T) {
	fakeNotifySend(t)
	mu, received := captureNotifications(t)
	previousWaiting := waiting
	ticked := make(chan struct{}, 64)
	waiting = func() []string {
		select {
		case ticked <- struct{}{}:
		default:
		}
		return []string{id1}
	}
	t.Cleanup(func() { waiting = previousWaiting })
	previousLastAPI := lastAPI.Load()
	t.Cleanup(func() { lastAPI.Store(previousLastAPI) })
	lastAPI.Store(time.Now().Add(time.Hour).UnixNano())

	cancel := withHomeCtx(t)
	finished := runLoop(func() { watchWaiting(5 * time.Millisecond) })
	for range 3 {
		select {
		case <-ticked:
		case <-time.After(loopWait):
			t.Fatal("la boucle ne tourne pas")
		}
	}
	cancel()
	expectReturn(t, finished, "watchWaiting")
	mu.Lock()
	defer mu.Unlock()
	if len(*received) != 0 {
		t.Fatalf("notification malgré un onglet ouvert : %v", *received)
	}
}

func TestWatchWaitingNeedsNotifySend(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	withHomeCtx(t)
	expectReturn(t, runLoop(func() { watchWaiting(time.Hour) }), "watchWaiting sans notify-send")
}

func TestWarmLoadKeepsOnlyValidStates(t *testing.T) {
	configDir := t.TempDir()
	use(configDir)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	previousState := warmState
	t.Cleanup(func() { warmState = previousState })
	warmState = map[string]*WarmState{}

	saved := `{"` + id1 + `":{"On":true,"Max":3,"Pings":1},"` + id2 + `":{"On":true,"Max":0},"` + id3 + `":null,"pas-un-id":{"On":true,"Max":2}}`
	os.WriteFile(filepath.Join(configDir, "gld-web-keepwarm.json"), []byte(saved), 0o600)
	warmMu.Lock()
	warmLoad()
	warmMu.Unlock()
	if len(warmState) != 1 || warmState[id1] == nil || warmState[id1].Max != 3 || warmState[id1].Pings != 1 {
		t.Fatalf("états chargés = %+v", warmState)
	}

	warmState = map[string]*WarmState{}
	os.WriteFile(filepath.Join(configDir, "gld-web-keepwarm.json"), []byte("{illisible"), 0o600)
	warmLoad()
	if len(warmState) != 0 {
		t.Fatalf("fichier corrompu chargé : %+v", warmState)
	}
}
