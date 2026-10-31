package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScreenIdleDetectsEmptyPromptWithStaleBackgroundTasks(t *testing.T) {
	idleScreen := "texte\n────\n❯ \n────\n  ⏵⏵ auto mode on\n\n  ◯ tests-ui  Tests\n"
	if !screenIdle(idleScreen) {
		t.Fatal("écran au repos non reconnu")
	}
	if screenIdle(idleScreen + "esc to interrupt") {
		t.Fatal("écran occupé pris pour du repos")
	}
	if screenIdle("texte\n❯ du texte tapé\n") {
		t.Fatal("prompt non vide pris pour du repos")
	}
}

func TestIdleByScreenWaitsForSettle(t *testing.T) {
	screenFn = func(string) (string, error) { return "❯ \n", nil }
	defer func() { screenFn = sessionScreen }()
	start := time.Now()
	if idleByScreen("sess-idle-test", start) {
		t.Fatal("repos accepté dès la première vue")
	}
	if !idleByScreen("sess-idle-test", start.Add(idleScreenSettle)) {
		t.Fatal("repos stable refusé")
	}
}

func TestShownStatusTurnsStuckBusyIntoIdle(t *testing.T) {
	use(t.TempDir())
	put(t, filepath.Join(cfgDir(), "sessions", "1.json"), fmt.Sprintf(`{"pid":%d,"sessionId":%q,"status":"busy"}`, os.Getpid(), id1), old)
	screenFn = func(string) (string, error) { return "❯ \n", nil }
	defer func() { screenFn = sessionScreen }()
	idleSeenMu.Lock()
	idleSeenAt[id1] = time.Now().Add(-idleScreenSettle)
	idleSeenMu.Unlock()
	if status, _ := shownStatus(id1); status != "idle" {
		t.Fatalf("busy figé non corrigé : %q", status)
	}
	screenFn = func(string) (string, error) { return "esc to interrupt\n", nil }
	if status, _ := shownStatus(id1); status != "busy" {
		t.Fatalf("vrai busy corrigé à tort : %q", status)
	}
}
