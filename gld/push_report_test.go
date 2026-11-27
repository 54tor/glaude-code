package main

import (
	"strings"
	"testing"
)

func TestPushReportTextNamesFailedRemoteAndKeepsOutputTail(t *testing.T) {
	oldRoot := root
	t.Cleanup(func() { root = oldRoot })
	root = t.TempDir() + "/projects"
	runID := "reportrun01"
	exitCode := 1
	status := PushRunStatus{ID: runID, Dir: "/depot", Branch: "main", Done: true, Remotes: []PushRemoteState{
		{Name: "origin", State: pushStateSucceeded},
		{Name: "miroir", State: pushStateFailed, Exit: &exitCode},
	}}
	if err := mkdirTrusted(pushRunDir(runID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePushStatus(status); err != nil {
		t.Fatal(err)
	}
	if err := rWriteAtomic(cfgDir(), pushRunFile(runID, pushRunLogFile), []byte("\x1b[31mhook refuse\x1b[0m\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	texteObtenu := pushReportText(status)

	for _, attendu := range []string{"miroir", "hook refuse"} {
		if !strings.Contains(texteObtenu, attendu) {
			t.Errorf("%q absent de %q", attendu, texteObtenu)
		}
	}
	if strings.Contains(texteObtenu, "origin") || strings.Contains(texteObtenu, "\x1b") {
		t.Errorf("remote réussi ou séquence ANSI présents : %q", texteObtenu)
	}
}
