package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
		{"remove", "", "x", false, []string{"--yes", skillsPkg, "remove", "x", "-a", "claude-code", "-y", "-p"}},
	}
	for _, record := range ok {
		got, err := npxSkillsArgs(record.act, record.src, record.sk, record.global)
		if err != nil || !slices.Equal(got, record.want) {
			t.Errorf("%s : %v %v", record.act, got, err)
		}
	}
	bad := []struct{ act, src, sk string }{
		{"add", "x; rm -rf ~", "x"}, {"add", "../x", "x"}, {"add", "http://a/b", "x"}, {"add", "a b/c", "x"}, {"add", "-g/x", "x"},
		{"add", "o/r", "../x"}, {"add", "o/r", "-x"}, {"exec", "o/r", "x"}, {"add", "", "x"},
	}
	for _, record := range bad {
		if got, err := npxSkillsArgs(record.act, record.src, record.sk, true); err == nil {
			t.Errorf("%+v accepté : %v", record, got)
		}
	}
}

func fakeNpx(t *testing.T, callback func(ctx context.Context, dir string, args []string) (string, error)) {
	old := runNpxSkills
	runNpxSkills = callback
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
	linkManaged(t, "a")
	count := 0
	fakeNpx(t, func(context.Context, string, []string) (string, error) { count++; return "", nil })
	for _, act := range []string{"update", "remove"} {
		if _, err := skillsAct(context.Background(), act, "", "maison", "user", ""); err == nil {
			t.Errorf("%s : skill non verrouillé accepté", act)
		}
	}
	if _, err := skillsAct(context.Background(), "remove", "", "a", "user", ""); err != nil || count != 1 {
		t.Errorf("skill verrouillé : err %v, appels %d", err, count)
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
	if skillHits := parseSkillHits("No skills found for \"zzz\"\n"); len(skillHits) != 0 {
		t.Errorf("sans résultat : %+v", skillHits)
	}
}

func TestSkillsFindRequete(t *testing.T) {
	lockHome(t, "")
	count := 0
	fakeNpx(t, func(_ context.Context, _ string, args []string) (string, error) {
		count++
		if !slices.Equal(args, []string{"--yes", skillsPkg, "find", "pdf"}) {
			t.Errorf("args %v", args)
		}
		return "a/b@c 1K installs\n└ https://skills.sh/a/b/c\n", nil
	})
	if skillHits, err := skillsFind(context.Background(), " pdf "); err != nil || len(skillHits) != 1 {
		t.Errorf("%v %v", skillHits, err)
	}
	for _, query := range []string{"", "  ", "-g", strings.Repeat("x", 81), "a\nb"} {
		if _, err := skillsFind(context.Background(), query); err == nil {
			t.Errorf("requête %q acceptée", query)
		}
	}
	if count != 1 {
		t.Errorf("appels : %d", count)
	}
}

func TestSkillUpdateState(t *testing.T) {
	trees := map[string]string{"": "root", "skills/x": "h2", "y": "h1"}
	gh := func(path, hash string) LockEntry {
		return LockEntry{Source: "o/r", SourceType: "github", SkillPath: path, Hash: hash}
	}
	cases := []struct {
		name  string
		e     LockEntry
		trees map[string]string
		want  string
	}{
		{"égal", gh("skills/x/SKILL.md", "h2"), trees, "current"},
		{"différent", gh("skills/x/SKILL.md", "old"), trees, "update"},
		{"racine", gh("SKILL.md", "root"), trees, "current"},
		{"autre source", LockEntry{Source: "o/r", SourceType: "local", SkillPath: "y/SKILL.md", Hash: "h1"}, trees, "unknown"},
		{"sans arbre", gh("y/SKILL.md", "h1"), nil, "unknown"},
		{"dossier absent", gh("z/SKILL.md", "h1"), trees, "unknown"},
		{"sans hash", gh("y/SKILL.md", ""), trees, "unknown"},
	}
	for _, record := range cases {
		if got := skillUpdateState(record.e, record.trees); got != record.want {
			t.Errorf("%s : %s, attendu %s", record.name, got, record.want)
		}
	}
}

func fakeTrees(t *testing.T, callback func(owner, repo string) (map[string]string, error)) {
	old := remoteTrees
	remoteTrees = func(_ context.Context, output, raw string) (map[string]string, error) { return callback(output, raw) }
	updCache.Lock()
	updCache.m = nil
	updCache.lastForce = time.Time{}
	updCache.Unlock()
	t.Cleanup(func() { remoteTrees = old })
}

const twoSkillsLock = `{"skills":{"a":{"source":"o/r","sourceType":"github","skillPath":"a/SKILL.md","skillFolderHash":"h1"},"b":{"source":"o/r","sourceType":"github","skillPath":"b/SKILL.md","skillFolderHash":"zz"}}}`

func TestCheckSkillUpdatesCache(t *testing.T) {
	lockHome(t, twoSkillsLock)
	count := 0
	fakeTrees(t, func(output, raw string) (map[string]string, error) {
		count++
		return map[string]string{"a": "h1", "b": "h2"}, nil
	})
	st := checkSkillUpdates(context.Background(), false)
	if st["u/a"] != "current" || st["u/b"] != "update" || count != 1 {
		t.Errorf("%v, appels %d", st, count)
	}
	checkSkillUpdates(context.Background(), false)
	if count != 1 {
		t.Errorf("cache ignoré : %d appels", count)
	}
	updCache.Lock()
	updCache.lastForce = time.Time{}
	updCache.Unlock()
	checkSkillUpdates(context.Background(), true)
	if count != 2 {
		t.Errorf("force ignoré : %d appels", count)
	}
}

func TestCheckSkillUpdatesLimite(t *testing.T) {
	lockHome(t, twoSkillsLock)
	fakeTrees(t, func(output, raw string) (map[string]string, error) { return nil, errors.New("HTTP 403") })
	st := checkSkillUpdates(context.Background(), false)
	if st["u/a"] != "unknown" || st["u/b"] != "unknown" {
		t.Errorf("%v", st)
	}
}

func TestSkillsFindExclusion(t *testing.T) {
	lockHome(t, "")
	fakeNpx(t, func(context.Context, string, []string) (string, error) { return "", nil })
	npxMu.Lock()
	_, err := skillsFind(context.Background(), "pdf")
	npxMu.Unlock()
	if err == nil || !strings.Contains(err.Error(), L("srv.skills.npxBusy")) {
		t.Errorf("find pendant une action : %v", err)
	}
}

func postSkillNpx(t *testing.T, body string) (int, map[string]any) {
	mux := http.NewServeMux()
	routesSkills(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/skills/installations", strings.NewReader(body)))
	var lookup map[string]any
	json.Unmarshal(recorder.Body.Bytes(), &lookup)
	return recorder.Code, lookup
}

func TestRouteSkillNpx(t *testing.T) {
	lockHome(t, "")
	var calls [][]string
	fakeNpx(t, func(_ context.Context, _ string, values []string) (string, error) {
		calls = append(calls, values)
		return "fait\n", nil
	})
	if code, _ := postSkillNpx(t, "pas du json"); code != 400 {
		t.Errorf("corps invalide : %d", code)
	}
	if _, lookup := postSkillNpx(t, `{"Action":"exec","Source":"o/r","Skill":"x","Scope":"user"}`); lookup["error"] == nil {
		t.Errorf("action inconnue : %v", lookup)
	}
	_, localLookup := postSkillNpx(t, `{"Action":"add","Source":"o/r","Skill":"x","Scope":"user"}`)
	if localLookup["message"] != "fait" || len(calls) != 1 || !slices.Contains(calls[0], "add") {
		t.Errorf("add : %v %v", localLookup, calls)
	}
}

func TestSkillsActRefuseProjetAvecSkillsLocal(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	t.Setenv("HOME", t.TempDir())
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	count := 0
	fakeNpx(t, func(context.Context, string, []string) (string, error) { count++; return "", nil })
	if _, err := skillsAct(context.Background(), "add", "o/r", "x", "project", "-p"); err != nil || count != 1 {
		t.Fatalf("projet sain refusé : %v, appels %d", err, count)
	}
	put(t, filepath.Join(proj, "node_modules", "skills", "package.json"), "{}", old)
	if _, err := skillsAct(context.Background(), "add", "o/r", "x", "project", "-p"); err == nil || count != 1 {
		t.Errorf("projet piégé accepté : %v, appels %d", err, count)
	}
	parent := t.TempDir()
	put(t, filepath.Join(parent, "node_modules", "skills", "package.json"), "{}", old)
	sub := filepath.Join(parent, "a", "b")
	os.MkdirAll(sub, 0o755)
	if !shadowsSkillsPkg(sub) || shadowsSkillsPkg(t.TempDir()) {
		t.Error("recherche dans les dossiers parents")
	}
}

func linkManaged(t *testing.T, name string) {
	home, _ := os.UserHomeDir()
	put(t, filepath.Join(home, ".agents", "skills", name, "SKILL.md"), "---\ndescription: x\n---\nx\n", old)
	if err := os.MkdirAll(filepath.Join(cfgDir(), "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".agents", "skills", name), filepath.Join(cfgDir(), "skills", name)); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsActRefuseSkillEcritAMain(t *testing.T) {
	lockHome(t, `{"skills":{"foo":{"source":"o/r","sourceType":"github"}}}`)
	put(t, filepath.Join(cfgDir(), "skills", "foo", "SKILL.md"), "---\ndescription: à la main\n---\nx\n", old)
	count := 0
	fakeNpx(t, func(context.Context, string, []string) (string, error) { count++; return "", nil })
	for _, act := range []string{"update", "remove"} {
		if _, err := skillsAct(context.Background(), act, "", "foo", "user", ""); err == nil {
			t.Errorf("%s : skill écrit à la main accepté", act)
		}
	}
	if count != 0 {
		t.Errorf("npx lancé %d fois", count)
	}
}

func TestNpxEnv(t *testing.T) {
	got := npxEnv([]string{"A=1", "GLD_WEB_TOKEN=x", "GLD_WEB_CHILD_TOKEN=y", "GLD_AUTH_KEY=z", "NODE_OPTIONS=--inspect"})
	has := func(env []string, kv string) bool { return slices.Contains(env, kv) }
	if !has(got, "A=1") || !has(got, "npm_config_registry=https://registry.npmjs.org/") || !has(got, "NODE_OPTIONS=") {
		t.Errorf("%v", got)
	}
	for _, entry := range got {
		if strings.HasPrefix(entry, "GLD_WEB_") || strings.HasPrefix(entry, "GLD_AUTH_KEY=") || entry == "NODE_OPTIONS=--inspect" {
			t.Errorf("fuite : %s", entry)
		}
	}
	t.Setenv("PATH", "/x/bin")
	if values := npxEnv(nil); !has(values, "PATH=/x/bin") {
		t.Errorf("sans base, l'environnement du processus est repris : %v", values)
	}
}

func TestPersonalLockPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	if got := personalLockPath(); got != filepath.Join(home, ".agents", ".skill-lock.json") {
		t.Error(got)
	}
	input := t.TempDir()
	t.Setenv("XDG_STATE_HOME", input)
	if got := personalLockPath(); got != filepath.Join(input, "skills", ".skill-lock.json") {
		t.Error(got)
	}
}

func TestRunNpxSkillsNeTraînePas(t *testing.T) {
	bin := t.TempDir()
	put(t, filepath.Join(bin, "npx"), "#!/bin/sh\nsleep 30 &\nsleep 30\n", old)
	os.Chmod(filepath.Join(bin, "npx"), 0o755)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	oldD := npxWaitDelay
	npxWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { npxWaitDelay = oldD })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	runNpxSkills(ctx, t.TempDir(), []string{"x"})
	if duration := time.Since(start); duration > 3*time.Second {
		t.Errorf("rendu après %v : le petit-enfant retient le tube", duration)
	}
}

func TestCheckSkillUpdatesForceLimite(t *testing.T) {
	lockHome(t, twoSkillsLock)
	count := 0
	fakeTrees(t, func(output, raw string) (map[string]string, error) {
		count++
		return map[string]string{"a": "h1", "b": "h2"}, nil
	})
	checkSkillUpdates(context.Background(), true)
	checkSkillUpdates(context.Background(), true)
	if count != 1 {
		t.Errorf("deux forçages de suite : %d appels", count)
	}
}
