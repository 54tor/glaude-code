package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
)

func exportEnv(t *testing.T) string {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "settings.json"), `{"model":"opus","env":{"X":"1"},"apiKeyHelper":"x","enabledPlugins":{"a@m":true},"permissions":{"allow":["Bash(ls:*)"]}}`, old)
	put(t, filepath.Join(cfg, "skills", "deploy", "SKILL.md"), "x\n", old)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "regles\n", old)
	put(t, filepath.Join(cfg, ".claude.json"), `{"mcpServers":{"gh":{"command":"gh","env":{"TOKEN":"s"}}}}`, old)
	return cfg
}

func unzipMap(t *testing.T, b []byte) map[string]string {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		r, _ := f.Open()
		c, _ := io.ReadAll(r)
		r.Close()
		out[f.Name] = string(c)
	}
	return out
}

func TestExportBackupCats(t *testing.T) {
	exportEnv(t)
	b, n, err := exportBackup([]string{"skills"})
	if err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	files := unzipMap(t, b)
	for _, no := range []string{"home/settings.json", "mcp.json", "plugins.json", "home/CLAUDE.md"} {
		if _, ok := files[no]; ok {
			t.Errorf("%s présent", no)
		}
	}
	if _, ok := files["home/skills/deploy/SKILL.md"]; !ok {
		t.Errorf("skill absent : %v", files)
	}
}

func TestExportBackupSettingsNoSecrets(t *testing.T) {
	exportEnv(t)
	b, _, err := exportBackup([]string{"settings"})
	if err != nil {
		t.Fatal(err)
	}
	s := unzipMap(t, b)["home/settings.json"]
	for _, no := range []string{"\"env\"", "apiKeyHelper", "enabledPlugins"} {
		if bytes.Contains([]byte(s), []byte(no)) {
			t.Errorf("%s dans %s", no, s)
		}
	}
	if !bytes.Contains([]byte(s), []byte("permissions")) || !bytes.Contains([]byte(s), []byte("model")) {
		t.Errorf("réglages perdus : %s", s)
	}
}

func TestExportBackupMcpStripped(t *testing.T) {
	exportEnv(t)
	b, _, err := exportBackup([]string{"mcp"})
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Servers  map[string]map[string]any `json:"servers"`
		Stripped map[string][]string       `json:"stripped"`
	}
	if err := json.Unmarshal([]byte(unzipMap(t, b)["mcp.json"]), &d); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Servers["gh"]["env"]; ok || d.Servers["gh"]["command"] != "gh" {
		t.Errorf("serveur : %+v", d.Servers)
	}
	if len(d.Stripped["gh"]) != 1 || d.Stripped["gh"][0] != "env" {
		t.Errorf("stripped : %+v", d.Stripped)
	}
}

func TestExportBackupPlugins(t *testing.T) {
	exportEnv(t)
	op, om := pluginRaw, marketRaw
	defer func() { pluginRaw, marketRaw = op, om }()
	pluginRaw = func() ([]byte, error) {
		return []byte(`{"installed":[{"id":"a@m","version":"1","scope":"user","enabled":true}],"available":[]}`), nil
	}
	marketRaw = func() ([]byte, error) { return []byte(`[{"name":"m","source":"github","repo":"o/r"}]`), nil }
	b, _, err := exportBackup([]string{"plugins"})
	if err != nil {
		t.Fatal(err)
	}
	var d pluginsDoc
	if err := json.Unmarshal([]byte(unzipMap(t, b)["plugins.json"]), &d); err != nil || len(d.Plugins) != 1 || len(d.Marketplaces) != 1 {
		t.Fatalf("%v %+v", err, d)
	}
}

func TestExportBackupBadCats(t *testing.T) {
	exportEnv(t)
	for _, c := range [][]string{nil, {}, {"x"}, {"skills", "x"}} {
		if _, _, err := exportBackup(c); err == nil {
			t.Errorf("%v accepté", c)
		}
	}
}
