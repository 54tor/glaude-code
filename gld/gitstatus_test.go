package main

import (
	"errors"
	"os/exec"
	"sync/atomic"
	"testing"
)

func TestParseGitStatus(t *testing.T) {
	out := "# branch.oid abc\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +2 -1\n" +
		"1 .M N... 100644 100644 100644 a b f.go\n2 R. N... 100644 100644 100644 a b R100 n.go\to.go\nu UU N... 1 1 1 1 a b c m.go\n? new.txt\n? other\n"
	want := GitState{Branch: "main", Upstream: true, Ahead: 2, Behind: 1, Changed: 3, Untrack: 2}
	if got := parseGitStatus(out); got != want {
		t.Fatalf("%+v", got)
	}
	if got := parseGitStatus("# branch.head (detached)\n"); !got.Detached || got.Branch != "" {
		t.Fatalf("%+v", got)
	}
}

// Un dossier qui n'est pas un dépôt est absent du résultat ; l'état est gardé entre deux requêtes proches.
func TestGitStatesCache(t *testing.T) {
	old := gitRun
	defer func() { gitRun = old; gitMu.Lock(); clear(gitCache); gitMu.Unlock() }()
	gitMu.Lock()
	clear(gitCache)
	gitMu.Unlock()
	var calls atomic.Int32 // gitStates relit les dossiers en parallèle
	gitRun = func(dir string) (string, error) {
		calls.Add(1)
		if dir == "/pas-un-depot" {
			return "", errors.New("not a git repository")
		}
		return "# branch.head dev\n1 .M N... 1 1 1 a b f\n", nil
	}
	for i := 0; i < 2; i++ {
		got := gitStates([]string{"/depot", "/pas-un-depot"})
		if len(got) != 1 || got["/depot"].Branch != "dev" || got["/depot"].Changed != 1 {
			t.Fatalf("%+v", got)
		}
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("relu malgré le cache : %d appels", n)
	}
}

// Avec un vrai dépôt : la branche et un fichier non suivi.
func TestRunGitStatusReel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q", "-b", "main").Run(); err != nil {
		t.Skip("git init impossible")
	}
	put(t, dir+"/a.txt", "x\n", old)
	out, err := runGitStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := parseGitStatus(out); s.Branch != "main" || s.Untrack != 1 {
		t.Fatalf("%+v\n%s", s, out)
	}
}

func TestParseGitChanges(t *testing.T) {
	out := "# branch.head main\n" +
		"1 .M N... 100644 100644 100644 a b src/mon fichier.go\n" +
		"1 A. N... 000000 100644 100644 a b new.go\n" +
		"2 R. N... 100644 100644 100644 a b R100 neuf.go\tancien.go\n" +
		"u UU N... 1 1 1 1 a b c conflit.go\n" +
		"? libre.txt\n"
	want := []GitChange{
		{Path: "src/mon fichier.go", Status: " M"},
		{Path: "new.go", Status: "A "},
		{Path: "neuf.go", From: "ancien.go", Status: "R "},
		{Path: "conflit.go", Status: "UU"},
		{Path: "libre.txt", Status: "??"},
	}
	got := parseGitChanges(out)
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d : %+v au lieu de %+v", i, got[i], want[i])
		}
	}
}

// Les sous-dossiers qui contiennent un .git sont listés (pas les cachés, ni node_modules ni vendor).
func TestSubRepos(t *testing.T) {
	d := t.TempDir()
	for _, n := range []string{"8.2/.git", "8.1/.git", "docs", ".cache/.git", "node_modules/.git", "vendor/.git"} {
		put(t, d+"/"+n+"/x", "x\n", old)
	}
	got := subRepos(d)
	if len(got) != 2 || got[0] != d+"/8.1" || got[1] != d+"/8.2" {
		t.Fatalf("%v", got)
	}
}
