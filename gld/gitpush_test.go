package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMaskURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://u:secret@github.com/a/b.git":         "https://github.com/a/b.git",
		"https://ghp_jeton@github.com/a/b.git":        "https://github.com/a/b.git",
		"ssh://git:secret@host/a/b.git":               "ssh://git@host/a/b.git",
		"ssh://git@host/a/b.git":                      "ssh://git@host/a/b.git",
		"https://github.com/a/b.git":                  "https://github.com/a/b.git",
		"git@github.com:a/b.git":                      "git@github.com:a/b.git",
		"https://alice:p^w%@git.example.com/o/r.git":  "https://git.example.com/o/r.git",
		"https://alice:pa|ss@git.example.com/o/r.git": "https://git.example.com/o/r.git",
		"https://a:b@c@git.example.com/o/r@x.git":     "https://git.example.com/o/r@x.git",
	} {
		if got := maskURL(in); got != want {
			t.Errorf("maskURL(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	data, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v : %v\n%s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

// Pousse vers un remote choisi parmi deux ; un remote inconnu et un HEAD détaché sont refusés ; la première fois, l'amont est posé.
func TestGitPush(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	root := t.TempDir()
	argument, body, work := root+"/a.git", root+"/b.git", root+"/work"
	git(t, root, "init", "-q", "--bare", argument)
	git(t, root, "init", "-q", "--bare", body)
	git(t, root, "init", "-q", "-b", "main", work)
	git(t, work, "remote", "add", "origin", argument)
	git(t, work, "remote", "add", "miroir", "https://u:pw@example.org/x.git")
	git(t, work, "remote", "set-url", "--push", "miroir", body)
	git(t, work, "commit", "-q", "--allow-empty", "-m", "un")

	rs := gitRemotes(work)
	if len(rs) != 2 || rs[0].Name != "miroir" || rs[1].Name != "origin" {
		t.Fatalf("%+v", rs)
	}
	if _, err := gitPush(work, "nimporte"); err == nil {
		t.Fatal("remote inconnu accepté")
	}
	if out, err := gitPush(work, "origin"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := git(t, argument, "rev-parse", "main"); got != git(t, work, "rev-parse", "HEAD") {
		t.Fatalf("a.git en retard : %s", got)
	}
	if up := git(t, work, "rev-parse", "--abbrev-ref", "@{u}"); up != "origin/main" {
		t.Fatalf("amont %q", up)
	}
	git(t, work, "commit", "-q", "--allow-empty", "-m", "deux")
	if out, err := gitPush(work, "miroir"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := git(t, body, "rev-parse", "main"); got != git(t, work, "rev-parse", "HEAD") {
		t.Fatalf("b.git en retard : %s", got)
	}
	git(t, work, "checkout", "-q", "--detach")
	if _, err := gitPush(work, "origin"); err == nil {
		t.Fatal("HEAD détaché poussé")
	}
}

// Un hook pre-push qui refuse : sa sortie arrive dans le flux, suivie du code de retour non nul de git ; un push sans hook rend 0.
func TestStreamGitPushReportsHookOutputAndExitCode(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	root := t.TempDir()
	bareRemote, workDir := root+"/remote.git", root+"/work"
	git(t, root, "init", "-q", "--bare", bareRemote)
	git(t, root, "init", "-q", "-b", "main", workDir)
	git(t, workDir, "remote", "add", "origin", bareRemote)
	git(t, workDir, "commit", "-q", "--allow-empty", "-m", "un")
	hookPath := workDir + "/.git/hooks/pre-push"
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho hook-refuse-le-push >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	hookRefusedFrames := decodeFrames(t, workDir, "origin")
	if !strings.Contains(hookRefusedFrames.output, "hook-refuse-le-push") {
		t.Errorf("sortie du hook absente : %q", hookRefusedFrames.output)
	}
	if hookRefusedFrames.exit == nil || *hookRefusedFrames.exit == 0 {
		t.Errorf("code de retour attendu non nul, obtenu %v", hookRefusedFrames.exit)
	}

	if err := os.Remove(hookPath); err != nil {
		t.Fatal(err)
	}
	acceptedFrames := decodeFrames(t, workDir, "origin")
	if acceptedFrames.exit == nil || *acceptedFrames.exit != 0 {
		t.Errorf("code de retour attendu 0, obtenu %v (%q)", acceptedFrames.exit, acceptedFrames.output)
	}
}

type streamedPush struct {
	output string
	exit   *int
}

func decodeFrames(t *testing.T, dir, remote string) streamedPush {
	t.Helper()
	recorder := httptest.NewRecorder()
	streamGitPush(recorder, dir, remote)
	var streamed streamedPush
	decoder := json.NewDecoder(recorder.Body)
	for decoder.More() {
		var frame ndjsonFrame
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		streamed.output += string(frame.Out)
		if frame.Exit != nil {
			streamed.exit = frame.Exit
		}
	}
	return streamed
}
