package main

import (
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
