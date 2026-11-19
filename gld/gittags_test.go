package main

import (
	"io"
	"os/exec"
	"slices"
	"testing"
)

// Les tags cochés partent vers le remote sans forcer ; un tag non coché reste local.
func TestPushRemoteAndTagsPushesOnlyChosenTags(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	root := t.TempDir()
	bare, work := root+"/a.git", root+"/work"
	git(t, root, "init", "-q", "--bare", bare)
	git(t, root, "init", "-q", "-b", "main", work)
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "commit", "-q", "--allow-empty", "-m", "un")
	git(t, work, "tag", "v1")
	git(t, work, "tag", "v2")

	if code := pushRemoteAndTags(work, "origin", []string{"v1"}, false, io.Discard); code != 0 {
		t.Fatalf("code %d, attendu 0", code)
	}
	tagsObtenus := gitTagNames(bare)
	if !slices.Equal(tagsObtenus, []string{"v1"}) {
		t.Errorf("tags distants %v, attendu [v1]", tagsObtenus)
	}
}

// Un tag recréé sur un autre commit est refusé tel quel, et n'est écrasé qu'avec forceTags.
func TestPushRemoteAndTagsForcesMovedTagOnlyWhenAsked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	root := t.TempDir()
	bare, work := root+"/a.git", root+"/work"
	git(t, root, "init", "-q", "--bare", bare)
	git(t, root, "init", "-q", "-b", "main", work)
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "commit", "-q", "--allow-empty", "-m", "un")
	git(t, work, "tag", "v1")
	pushRemoteAndTags(work, "origin", []string{"v1"}, false, io.Discard)
	git(t, work, "commit", "-q", "--allow-empty", "-m", "deux")
	git(t, work, "tag", "-f", "v1")

	if code := pushRemoteAndTags(work, "origin", []string{"v1"}, false, io.Discard); code == 0 {
		t.Errorf("sans force : code 0, attendu un refus")
	}
	if code := pushRemoteAndTags(work, "origin", []string{"v1"}, true, io.Discard); code != 0 {
		t.Errorf("avec force : code %d, attendu 0", code)
	}
}
