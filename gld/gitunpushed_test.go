package main

import (
	"os/exec"
	"testing"
)

// Chaque remote ne liste que les commits qu'il n'a pas ; un remote sans la branche les voit tous.
func TestGitUnpushedListsMissingCommitsPerRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	root := t.TempDir()
	firstBare, secondBare, work := root+"/a.git", root+"/b.git", root+"/work"
	git(t, root, "init", "-q", "--bare", firstBare)
	git(t, root, "init", "-q", "--bare", secondBare)
	git(t, root, "init", "-q", "-b", "main", work)
	git(t, work, "remote", "add", "origin", firstBare)
	git(t, work, "remote", "add", "miroir", secondBare)
	git(t, work, "commit", "-q", "--allow-empty", "-m", "un")
	if output, err := gitPush(work, "origin"); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	git(t, work, "fetch", "-q", "origin")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "deux")

	unpushed := gitUnpushed(work)
	totalsObtenus := map[string]int{}
	for _, remote := range unpushed {
		totalsObtenus[remote.Name] = remote.Total
	}
	totalsAttendus := map[string]int{"origin": 1, "miroir": 2}
	for name, totalAttendu := range totalsAttendus {
		if totalsObtenus[name] != totalAttendu {
			t.Errorf("%s : total %d, attendu %d (%+v)", name, totalsObtenus[name], totalAttendu, unpushed)
		}
	}
	for _, remote := range unpushed {
		isTrackedAttendu := remote.Name == "origin"
		if remote.Tracked != isTrackedAttendu {
			t.Errorf("%s : tracked %v, attendu %v", remote.Name, remote.Tracked, isTrackedAttendu)
		}
		if remote.Name == "origin" && (len(remote.Commits) != 1 || remote.Commits[0].Subject != "deux") {
			t.Errorf("origin : %+v", remote.Commits)
		}
	}
}
