package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNpxSkillsArgs(t *testing.T) {
	ok := []struct {
		act, src, sk string
		global       bool
		want         []string
	}{
		{"add", "o/r", "x", true, []string{"--yes", skillsPkg, "add", "o/r", "-s", "x", "-a", "claude-code", "-y", "-g"}},
		{"add", "https://github.com/o/r", "x", false, []string{"--yes", skillsPkg, "add", "https://github.com/o/r", "-s", "x", "-a", "claude-code", "-y"}},
		{"update", "", "x", true, []string{"--yes", skillsPkg, "update", "x", "-y", "-g"}},
		{"remove", "", "x", false, []string{"--yes", skillsPkg, "remove", "x", "-y", "-p"}},
	}
	for _, c := range ok {
		got, err := npxSkillsArgs(c.act, c.src, c.sk, c.global)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s : %v %v", c.act, got, err)
		}
	}
	bad := []struct{ act, src, sk string }{
		{"add", "x; rm -rf ~", "x"}, {"add", "../x", "x"}, {"add", "http://a/b", "x"}, {"add", "a b/c", "x"}, {"add", "-g/x", "x"},
		{"add", "o/r", "../x"}, {"add", "o/r", "-x"}, {"exec", "o/r", "x"}, {"add", "", "x"},
	}
	for _, c := range bad {
		if got, err := npxSkillsArgs(c.act, c.src, c.sk, true); err == nil {
			t.Errorf("%+v accepté : %v", c, got)
		}
	}
}

func fakeNpx(t *testing.T, f func(ctx context.Context, dir string, args []string) (string, error)) {
	old := runNpxSkills
	runNpxSkills = f
	t.Cleanup(func() { runNpxSkills = old })
}

func lockHome(t *testing.T, lock string) string {
	use(t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	if lock != "" {
		put(t, filepath.Join(home, ".agents", ".skill-lock.json"), lock, old)
	}
	return home
}

func TestSkillsActRefuseSkillNonVerrouille(t *testing.T) {
	lockHome(t, `{"skills":{"a":{"source":"o/r","sourceType":"github"}}}`)
	n := 0
	fakeNpx(t, func(context.Context, string, []string) (string, error) { n++; return "", nil })
	for _, act := range []string{"update", "remove"} {
		if _, err := skillsAct(context.Background(), act, "", "maison", "user", ""); err == nil {
			t.Errorf("%s : skill non verrouillé accepté", act)
		}
	}
	if _, err := skillsAct(context.Background(), "remove", "", "a", "user", ""); err != nil || n != 1 {
		t.Errorf("skill verrouillé : err %v, appels %d", err, n)
	}
	if _, err := skillsAct(context.Background(), "add", "o/r", "x", "project", "../nope"); err == nil {
		t.Error("projet invalide accepté")
	}
}

func TestSkillsActAddEtDossier(t *testing.T) {
	home := lockHome(t, "")
	var gotDir string
	var gotArgs []string
	fakeNpx(t, func(_ context.Context, dir string, args []string) (string, error) {
		gotDir, gotArgs = dir, args
		return "ok\n", nil
	})
	out, err := skillsAct(context.Background(), "add", "o/r", "x", "user", "")
	if err != nil || out != "ok" || gotDir != home || !slices.Contains(gotArgs, "-g") {
		t.Errorf("%q %v dir %q args %v", out, err, gotDir, gotArgs)
	}
}

func TestSkillsActExclusion(t *testing.T) {
	lockHome(t, "")
	in, release := make(chan struct{}), make(chan struct{})
	fakeNpx(t, func(context.Context, string, []string) (string, error) { close(in); <-release; return "", nil })
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); skillsAct(context.Background(), "add", "o/r", "x", "user", "") }()
	<-in
	if _, err := skillsAct(context.Background(), "add", "o/r", "y", "user", ""); err == nil || !strings.Contains(err.Error(), L("srv.skills.npxBusy")) {
		t.Errorf("seconde action : %v", err)
	}
	close(release)
	wg.Wait()
	fakeNpx(t, func(context.Context, string, []string) (string, error) { return "", nil })
	if _, err := skillsAct(context.Background(), "add", "o/r", "y", "user", ""); err != nil {
		t.Errorf("verrou non libéré : %v", err)
	}
}

func TestSkillsActDelai(t *testing.T) {
	lockHome(t, "")
	oldT := npxTimeout
	npxTimeout = 50 * time.Millisecond
	t.Cleanup(func() { npxTimeout = oldT })
	fakeNpx(t, func(ctx context.Context, _ string, _ []string) (string, error) { <-ctx.Done(); return "", ctx.Err() })
	if _, err := skillsAct(context.Background(), "add", "o/r", "x", "user", ""); err == nil {
		t.Error("délai non signalé")
	}
	fakeNpx(t, func(context.Context, string, []string) (string, error) { return "", nil })
	if _, err := skillsAct(context.Background(), "add", "o/r", "x", "user", ""); err != nil {
		t.Errorf("verrou non libéré : %v", err)
	}
}

func TestStripANSI(t *testing.T) {
	if got := stripANSI("\x1b[38;5;145mo/r@x\x1b[0m"); got != "o/r@x" {
		t.Error(got)
	}
}

func TestParseSkillHits(t *testing.T) {
	out := "\nInstall with npx skills add <owner/repo@skill>\n\nanthropics/skills@pdf 217.5K installs\n└ https://skills.sh/anthropics/skills/pdf\n\ncoreyhaines31/marketingskills@lead-magnets 113.4K installs\n└ https://skills.sh/coreyhaines31/marketingskills/lead-magnets\n"
	got := parseSkillHits(out)
	want := SkillHit{"anthropics/skills@pdf", "anthropics/skills", "pdf", "217.5K", "https://skills.sh/anthropics/skills/pdf"}
	if len(got) != 2 || got[0] != want || got[1].Skill != "lead-magnets" {
		t.Errorf("%+v", got)
	}
	if h := parseSkillHits("No skills found for \"zzz\"\n"); len(h) != 0 {
		t.Errorf("sans résultat : %+v", h)
	}
}

func TestSkillsFindRequete(t *testing.T) {
	lockHome(t, "")
	n := 0
	fakeNpx(t, func(_ context.Context, _ string, args []string) (string, error) {
		n++
		if !slices.Equal(args, []string{"--yes", skillsPkg, "find", "pdf"}) {
			t.Errorf("args %v", args)
		}
		return "a/b@c 1K installs\n└ https://skills.sh/a/b/c\n", nil
	})
	if h, err := skillsFind(context.Background(), " pdf "); err != nil || len(h) != 1 {
		t.Errorf("%v %v", h, err)
	}
	for _, q := range []string{"", "  ", "-g", strings.Repeat("x", 81), "a\nb"} {
		if _, err := skillsFind(context.Background(), q); err == nil {
			t.Errorf("requête %q acceptée", q)
		}
	}
	if n != 1 {
		t.Errorf("appels : %d", n)
	}
}
