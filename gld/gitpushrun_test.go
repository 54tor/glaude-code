package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// La session pousse chaque remote à tour de rôle, journalise la sortie et laisse un état final lisible sans gld.
func TestRunPushSessionPushesRemotesInOrderAndRecordsStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	oldRoot := root
	t.Cleanup(func() { root = oldRoot })
	root = filepath.Join(t.TempDir(), "projects")
	scratch := t.TempDir()
	firstBare, secondBare, work := scratch+"/a.git", scratch+"/b.git", scratch+"/work"
	git(t, scratch, "init", "-q", "--bare", firstBare)
	git(t, scratch, "init", "-q", "--bare", secondBare)
	git(t, scratch, "init", "-q", "-b", "main", work)
	git(t, work, "remote", "add", "origin", firstBare)
	git(t, work, "remote", "add", "miroir", secondBare)
	git(t, work, "commit", "-q", "--allow-empty", "-m", "un")

	runID := "testrun01"
	initial := PushRunStatus{ID: runID, Dir: work, Branch: "main", Remotes: []PushRemoteState{
		{Name: "origin", State: pushStatePending}, {Name: "miroir", State: pushStatePending}, {Name: "absent", State: pushStatePending}}}
	if err := mkdirTrusted(pushRunDir(runID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePushStatus(initial); err != nil {
		t.Fatal(err)
	}

	if exitCode := runPushSession(runID); exitCode != 0 {
		t.Fatalf("code de sortie %d", exitCode)
	}

	finalStatus, err := readPushStatus(runID)
	if err != nil {
		t.Fatal(err)
	}
	statesObtenus := []string{}
	for _, remote := range finalStatus.Remotes {
		statesObtenus = append(statesObtenus, remote.State)
	}
	statesAttendus := "succeeded,succeeded,failed"
	if !finalStatus.Done || strings.Join(statesObtenus, ",") != statesAttendus {
		t.Fatalf("done=%v états %v, attendu %s", finalStatus.Done, statesObtenus, statesAttendus)
	}
	headHash := git(t, work, "rev-parse", "HEAD")
	if git(t, secondBare, "rev-parse", "main") != headHash {
		t.Error("le second remote n'a pas reçu la branche")
	}
	loggedOutput, _ := readPushOutput(runID, 0)
	if len(loggedOutput) == 0 {
		t.Error("journal vide")
	}
}
