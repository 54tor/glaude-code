package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseGitStatus(t *testing.T) {
	out := "# branch.oid abc\x00# branch.head main\x00# branch.upstream origin/main\x00# branch.ab +2 -1\x00" +
		"1 .M N... 100644 100644 100644 a b f.go\x002 R. N... 100644 100644 100644 a b R100 n.go\x00o.go\x00u UU N... 1 1 1 1 a b c m.go\x00? new.txt\x00? other\x00"
	want := GitState{Branch: "main", Upstream: true, Ahead: 2, Behind: 1, Changed: 3, Untrack: 2}
	if got := parseGitStatus(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if got := parseGitStatus("# branch.head (detached)\x00"); !got.Detached || got.Branch != "" {
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
		return "# branch.head dev\x001 .M N... 1 1 1 a b f\x00", nil
	}
	for index := 0; index < 2; index++ {
		got := gitStates([]string{"/depot", "/pas-un-depot"})
		if len(got) != 1 || got["/depot"].Branch != "dev" || got["/depot"].Changed != 1 {
			t.Fatalf("%+v", got)
		}
	}
	if char := calls.Load(); char != 2 {
		t.Fatalf("relu malgré le cache : %d appels", char)
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
	if gitState := parseGitStatus(out); gitState.Branch != "main" || gitState.Untrack != 1 {
		t.Fatalf("%+v\n%s", gitState, out)
	}
}

func TestParseGitChanges(t *testing.T) {
	out := "# branch.head main\x00" +
		"1 .M N... 100644 100644 100644 a b src/mon fichier.go\x00" +
		"1 A. N... 000000 100644 100644 a b new.go\x00" +
		"2 R. N... 100644 100644 100644 a b R100 neuf.go\x00ancien.go\x00" +
		"u UU N... 1 1 1 1 a b c conflit.go\x00" +
		"? libre.txt\x00"
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
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("%d : %+v au lieu de %+v", index, got[index], want[index])
		}
	}
}

// Les sous-dossiers qui contiennent un .git sont listés (pas les cachés, ni node_modules ni vendor).
func TestSubRepos(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"8.2/.git", "8.1/.git", "docs", ".cache/.git", "node_modules/.git", "vendor/.git"} {
		put(t, dir+"/"+name+"/x", "x\n", old)
	}
	got := subRepos(dir)
	if len(got) != 2 || got[0] != dir+"/8.1" || got[1] != dir+"/8.2" {
		t.Fatalf("%v", got)
	}
}

// Les add-dir des réglages donnent leurs dépôts au niveau 0 (le dossier) et au niveau 1 (ses sous-dossiers), chemins relatifs et ~ compris.
func TestAddDirRepos(t *testing.T) {
	oldRoot := root
	t.Cleanup(func() { root = oldRoot })
	root = filepath.Join(t.TempDir(), "projects")
	proj, ext := t.TempDir(), t.TempDir()
	for _, name := range []string{ext + "/.git", ext + "/sub/.git", ext + "/deep/x/.git", proj + "/rel/.git"} {
		put(t, name+"/x", "x\n", old)
	}
	put(t, proj+"/.claude/settings.json", `{"permissions":{"additionalDirectories":["`+ext+`","rel","/absent"]}}`, old)
	got := sessionRepos(proj)
	want := []string{ext, ext + "/sub", proj + "/rel"}
	sort.Strings(want)
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Fatalf("%v, attendu %v", got, want)
	}
}

// Les chemins accentués, avec guillemet ou renommés arrivent tels quels, sans l'échappement octal de git (« "\303\251t\303\251" »).
func TestGitChangesCheminsReels(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	put(t, dir+"/vieux nom.txt", "x\n", old)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "un")
	git(t, dir, "mv", "vieux nom.txt", "nouvel été.txt")
	put(t, dir+"/été.txt", "x\n", old)
	put(t, dir+`/q"uote.txt`, "x\n", old)
	out, err := runGitStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[GitChange]bool{
		{Path: "nouvel été.txt", From: "vieux nom.txt", Status: "R "}: true,
		{Path: "été.txt", Status: "??"}:                               true,
		{Path: `q"uote.txt`, Status: "??"}:                            true,
	}
	got := parseGitChanges(out)
	for _, gitChange := range got {
		if !want[gitChange] {
			t.Errorf("inattendu : %+v", gitChange)
		}
	}
	if gitState := parseGitStatus(out); len(got) != 3 || gitState.Changed != 1 || gitState.Untrack != 2 || gitState.Branch != "main" {
		t.Fatalf("%+v %+v\n%q", got, gitState, out)
	}
}

func TestGitPrefix(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	put(t, dir+"/sub/dir/f.txt", "x\n", old)
	put(t, dir+"/top.txt", "x\n", old)
	if path := gitPrefix(dir); path != "" {
		t.Fatalf("racine : %q", path)
	}
	if path := gitPrefix(dir + "/sub/dir"); path != "sub/dir/" {
		t.Fatalf("sous-dossier : %q", path)
	}
	out, err := runGitStatus(dir + "/sub/dir")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, gitChange := range parseGitChanges(out) {
		paths = append(paths, gitChange.Path)
	}
	if len(paths) != 2 {
		t.Fatalf("les changements hors du sous-dossier restent renvoyés : %v", paths)
	}
	if gitPrefix(t.TempDir()) != "" {
		t.Fatal("hors dépôt")
	}
}

// Le préfixe gardé expire avec l'état git du dossier : un « git init » ultérieur est vu.
func TestGitPrefixExpires(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	gitPrefixMu.Lock()
	gitPrefixes[dir] = gitPrefixEntry{"vieux/", time.Now().Add(-2 * gitTTL)}
	gitPrefixMu.Unlock()
	if path := gitPrefix(dir); path != "" {
		t.Fatalf("préfixe périmé servi : %q", path)
	}
	gitPrefixMu.Lock()
	gitPrefixes[dir] = gitPrefixEntry{"frais/", time.Now()}
	gitPrefixMu.Unlock()
	if path := gitPrefix(dir); path != "frais/" {
		t.Fatalf("préfixe frais relu : %q", path)
	}
}

// Chaque remote est comparé à la branche courante ; un remote sans la branche est « missing ».
func TestParseRemoteSync(t *testing.T) {
	refs := "refs/remotes/origin/main 0 2\nrefs/remotes/origin/HEAD 0 2\nrefs/remotes/backup/main 3 0\nrefs/remotes/backup/feat/x 1 1\n"
	got := parseRemoteSync("origin\nbackup\nmirror\n", refs, "main")
	want := []GitRemoteSync{{Name: "origin", Ahead: 2}, {Name: "backup", Behind: 3}, {Name: "mirror", Missing: true}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	if got := parseRemoteSync("backup", refs, "feat/x"); len(got) != 1 || got[0].Ahead != 1 || got[0].Behind != 1 {
		t.Fatalf("%+v", got)
	}
}

// Avec un vrai dépôt : deux remotes locaux, l'un a la branche en retard, l'autre ne l'a pas.
func TestRunGitRemotesReel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	root := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=u", "-c", "user.email=u@example.com"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v : %v %s", args, err, out)
		}
	}
	work, withBranch, withoutBranch := root+"/work", root+"/a.git", root+"/b.git"
	for _, dir := range []string{work, withBranch, withoutBranch} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(withBranch, "init", "-q", "--bare")
	git(withoutBranch, "init", "-q", "--bare")
	git(work, "init", "-q", "-b", "main")
	git(work, "commit", "-q", "--allow-empty", "-m", "un")
	git(work, "remote", "add", "a", withBranch)
	git(work, "remote", "add", "b", withoutBranch)
	git(work, "push", "-q", "a", "main")
	git(work, "commit", "-q", "--allow-empty", "-m", "deux")
	git(work, "fetch", "-q", "a")
	got := runGitRemotes(work, "main")
	want := []GitRemoteSync{{Name: "a", Ahead: 1}, {Name: "b", Missing: true}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
}
