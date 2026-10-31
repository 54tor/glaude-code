package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdEnv : faux claude (journal dans $FAKE_LOG, échoue sur « bad@m ») et listes installées simulées par les coutures.
func cmdEnv(t *testing.T, installed, markets string) (log string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	log = filepath.Join(home, "calls")
	t.Setenv("FAKE_LOG", log)
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho \"$*\" >> \"$FAKE_LOG\"\nfor a; do [ \"$a\" = bad@m ] && { echo boom; exit 1; }; done\nexit 0\n"), 0o755)
	t.Setenv("PATH", t.TempDir())
	cfg := filepath.Join(home, ".claude")
	use(cfg)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	oldP, oldM := pluginRaw, marketRaw
	t.Cleanup(func() { pluginRaw, marketRaw = oldP, oldM })
	pluginRaw = func() ([]byte, error) { return []byte(`{"installed":` + installed + `,"available":[]}`), nil }
	marketRaw = func() ([]byte, error) { return []byte(markets), nil }
	return log
}

func cmdArchive(t *testing.T, files map[string]string) []byte {
	lookup := map[string][]byte{}
	for key, value := range files {
		lookup[key] = []byte(value)
	}
	data, err := writeBackupZip(lookup, BackupManifest{Format: backupFormat, Gld: "t", Date: "2026-01-01T00:00:00Z", Cats: backupCats})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func resOf(res []BackupResult, id string) BackupResult {
	for _, backupResult := range res {
		if backupResult.ID == id {
			return backupResult
		}
	}
	return BackupResult{ID: id, Status: "absent"}
}

const plugDoc = `{"marketplaces":[{"name":"have","source":"github","repo":"o/have"},{"name":"newm","source":"github","repo":"o/newm"}],
"plugins":[{"id":"a@m","version":"1","scope":"user","enabled":true},{"id":"c@m","version":"1","scope":"user","enabled":true},{"id":"d@m","version":"1","scope":"user","enabled":false}]}`

func TestStagePluginsPlan(t *testing.T) {
	cmdEnv(t, `[{"id":"a@m","version":"1","scope":"user","enabled":true},{"id":"c@m","version":"1","scope":"user","enabled":false}]`,
		`[{"name":"have","source":"github","repo":"o/have"}]`)
	backupPlan, err := stageBackup(cmdArchive(t, map[string]string{"plugins.json": plugDoc}))
	if err != nil {
		t.Fatal(err)
	}
	defer dropStage(backupPlan.ID)
	for id, st := range map[string]string{"market:have": "same", "market:newm": "new", "plugin:user:a@m": "same", "plugin:user:c@m": "diff", "plugin:user:d@m": "new"} {
		if it, ok := syncItem(backupPlan.Items, id); !ok || it.Status != st || it.Cat != "plugins" {
			t.Errorf("%s : %+v", id, it)
		}
	}
}

func TestStageSkipsProjectPluginWithoutDir(t *testing.T) {
	cmdEnv(t, `[]`, `[]`)
	doc := `{"marketplaces":[],"plugins":[{"id":"p@m","version":"1","scope":"project","enabled":true,"project":"/home/bob/absent"}]}`
	backupPlan, err := stageBackup(cmdArchive(t, map[string]string{"plugins.json": doc}))
	if err != nil {
		t.Fatal(err)
	}
	defer dropStage(backupPlan.ID)
	if _, ok := syncItem(backupPlan.Items, "plugin:project:p@m"); ok || len(backupPlan.Skipped) != 1 {
		t.Fatalf("%+v %v", backupPlan.Items, backupPlan.Skipped)
	}
}

func TestApplyOrder(t *testing.T) {
	log := cmdEnv(t, `[{"id":"c@m","version":"1","scope":"user","enabled":false}]`, `[]`)
	backupPlan, err := stageBackup(cmdArchive(t, map[string]string{"plugins.json": plugDoc}))
	if err != nil {
		t.Fatal(err)
	}
	picks := []SyncPick{{"plugin:user:d@m", "replace"}, {"plugin:user:c@m", "replace"}, {"market:newm", "replace"}}
	res, err := applyBackup(backupPlan.ID, picks, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, backupResult := range res {
		if backupResult.Status != "ok" {
			t.Errorf("%+v", backupResult)
		}
	}
	data, _ := os.ReadFile(log)
	got := string(data)
	index, innerIndex := strings.Index(got, "marketplace add"), strings.Index(got, "plugin install d@m")
	if index < 0 || innerIndex < 0 || index > innerIndex {
		t.Fatalf("ordre : %q", got)
	}
	if !strings.Contains(got, "plugin enable c@m -s user") || !strings.Contains(got, "plugin disable d@m -s user") {
		t.Fatalf("état : %q", got)
	}
	if strings.Contains(got, "install c@m") {
		t.Fatalf("plugin installé réinstallé : %q", got)
	}
}

func TestApplyPluginFailureContinues(t *testing.T) {
	cmdEnv(t, `[]`, `[]`)
	doc := `{"marketplaces":[],"plugins":[{"id":"a@m","scope":"user","enabled":true},{"id":"bad@m","scope":"user","enabled":true},{"id":"c@m","scope":"user","enabled":true}]}`
	backupPlan, err := stageBackup(cmdArchive(t, map[string]string{"plugins.json": doc}))
	if err != nil {
		t.Fatal(err)
	}
	res, _ := applyBackup(backupPlan.ID, []SyncPick{{"plugin:user:a@m", "replace"}, {"plugin:user:bad@m", "replace"}, {"plugin:user:c@m", "replace"}}, nil)
	if resOf(res, "plugin:user:a@m").Status != "ok" || resOf(res, "plugin:user:c@m").Status != "ok" {
		t.Fatalf("%+v", res)
	}
	if backupResult := resOf(res, "plugin:user:bad@m"); backupResult.Status != "failed" || !strings.Contains(backupResult.Message, "boom") {
		t.Fatalf("%+v", backupResult)
	}
}

const mcpDoc = `{"servers":{"gh":{"command":"gh","args":["x"]},"web":{"type":"http","url":"https://example.com/mcp"}},"stripped":{"gh":["env"]}}`

func TestStageMcpPlan(t *testing.T) {
	cmdEnv(t, `[]`, `[]`)
	os.WriteFile(claudeJSON(), []byte(`{"mcpServers":{"web":{"type":"http","url":"https://example.com/mcp"}}}`), 0o600)
	backupPlan, err := stageBackup(cmdArchive(t, map[string]string{"mcp.json": mcpDoc}))
	if err != nil {
		t.Fatal(err)
	}
	defer dropStage(backupPlan.ID)
	gh, _ := syncItem(backupPlan.Items, "mcp:gh")
	web, _ := syncItem(backupPlan.Items, "mcp:web")
	if gh.Status != "new" || gh.Risk != "exec" || !strings.Contains(gh.From, "env") || web.Status != "same" || web.Risk != "" || gh.Cat != "mcp" {
		t.Fatalf("%+v %+v", gh, web)
	}
}

func TestApplyMcpAdd(t *testing.T) {
	cmdEnv(t, `[]`, `[]`)
	old := mcpAddJSON
	t.Cleanup(func() { mcpAddJSON = old })
	var name string
	var doc []byte
	mcpAddJSON = func(localName string, data []byte) error { name, doc = localName, data; return nil }
	backupPlan, _ := stageBackup(cmdArchive(t, map[string]string{"mcp.json": mcpDoc}))
	res, _ := applyBackup(backupPlan.ID, []SyncPick{{"mcp:gh", "replace"}}, nil)
	if resOf(res, "mcp:gh").Status != "ok" || name != "gh" || strings.Contains(string(doc), "env") || !strings.Contains(string(doc), `"command"`) {
		t.Fatalf("%+v %q %s", res, name, doc)
	}
}

func TestMcpAddNameRejected(t *testing.T) {
	log := cmdEnv(t, `[]`, `[]`)
	for _, name := range []string{"a b", "-x", ""} {
		if err := mcpAddJSON(name, []byte(`{}`)); err == nil {
			t.Errorf("accepté : %q", name)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("claude lancé")
	}
}

func TestMcpAddRuns(t *testing.T) {
	log := cmdEnv(t, `[]`, `[]`)
	if err := mcpAddJSON("gh", []byte(`{"command":"gh"}`)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), `mcp add-json gh {"command":"gh"} -s user`) {
		t.Fatalf("%q", data)
	}
}

func TestApplyBackupUnownedPickSkipped(t *testing.T) {
	cmdEnv(t, `[]`, `[]`)
	backupPlan, _ := stageBackup(cmdArchive(t, map[string]string{"plugins.json": `{"marketplaces":[],"plugins":[]}`}))
	res, _ := applyBackup(backupPlan.ID, []SyncPick{{"zzz:inconnu", "replace"}}, nil)
	if backupResult := resOf(res, "zzz:inconnu"); backupResult.Status != "skipped" {
		t.Fatalf("%+v", res)
	}
}

func TestApplyDashValuesRefused(t *testing.T) {
	log := cmdEnv(t, `[]`, `[]`)
	doc := `{"marketplaces":[{"name":"x","source":"github","repo":"-x/y"}],"plugins":[{"id":"--help","scope":"user","enabled":true}]}`
	backupPlan, err := stageBackup(cmdArchive(t, map[string]string{"plugins.json": doc}))
	if err != nil {
		t.Fatal(err)
	}
	if len(backupPlan.Items) != 0 {
		t.Fatalf("plan : %+v", backupPlan.Items)
	}
	res, _ := applyBackup(backupPlan.ID, []SyncPick{{"market:x", "replace"}, {"plugin:user:--help", "replace"}}, nil)
	if resOf(res, "market:x").Status != "failed" || resOf(res, "plugin:user:--help").Status != "failed" {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("claude lancé")
	}
}

func TestMcpAddDocNotObject(t *testing.T) {
	log := cmdEnv(t, `[]`, `[]`)
	for _, dir := range []string{`-1`, `null`, `[]`, `"x"`, ``} {
		if err := mcpAddJSON("gh", []byte(dir)); err == nil {
			t.Errorf("accepté : %q", dir)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("claude lancé")
	}
}
