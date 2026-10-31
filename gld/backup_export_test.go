package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
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

func unzipMap(t *testing.T, data []byte) map[string]string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, file := range zr.File {
		readCloser, _ := file.Open()
		data, _ := io.ReadAll(readCloser)
		readCloser.Close()
		out[file.Name] = string(data)
	}
	return out
}

func TestExportBackupCats(t *testing.T) {
	exportEnv(t)
	data, count, err := exportBackup([]string{"skills"})
	if err != nil || count != 1 {
		t.Fatalf("%v %d", err, count)
	}
	files := unzipMap(t, data)
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
	data, _, err := exportBackup([]string{"settings"})
	if err != nil {
		t.Fatal(err)
	}
	text := unzipMap(t, data)["home/settings.json"]
	for _, no := range []string{"\"env\"", "apiKeyHelper", "enabledPlugins"} {
		if bytes.Contains([]byte(text), []byte(no)) {
			t.Errorf("%s dans %s", no, text)
		}
	}
	if !bytes.Contains([]byte(text), []byte("permissions")) || !bytes.Contains([]byte(text), []byte("model")) {
		t.Errorf("réglages perdus : %s", text)
	}
}

func TestExportBackupMcpStripped(t *testing.T) {
	exportEnv(t)
	data, _, err := exportBackup([]string{"mcp"})
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Servers  map[string]map[string]any `json:"servers"`
		Stripped map[string][]string       `json:"stripped"`
	}
	if err := json.Unmarshal([]byte(unzipMap(t, data)["mcp.json"]), &record); err != nil {
		t.Fatal(err)
	}
	if _, ok := record.Servers["gh"]["env"]; ok || record.Servers["gh"]["command"] != "gh" {
		t.Errorf("serveur : %+v", record.Servers)
	}
	if len(record.Stripped["gh"]) != 1 || record.Stripped["gh"][0] != "env" {
		t.Errorf("stripped : %+v", record.Stripped)
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
	data, _, err := exportBackup([]string{"plugins"})
	if err != nil {
		t.Fatal(err)
	}
	var localPluginsDoc pluginsDoc
	if err := json.Unmarshal([]byte(unzipMap(t, data)["plugins.json"]), &localPluginsDoc); err != nil || len(localPluginsDoc.Plugins) != 1 || len(localPluginsDoc.Marketplaces) != 1 {
		t.Fatalf("%v %+v", err, localPluginsDoc)
	}
}

func TestExportBackupBadCats(t *testing.T) {
	exportEnv(t)
	for _, values := range [][]string{nil, {}, {"x"}, {"skills", "x"}} {
		if _, _, err := exportBackup(values); err == nil {
			t.Errorf("%v accepté", values)
		}
	}
}

func TestExportBackupNoNestedOrArgvSecrets(t *testing.T) {
	cfg := exportEnv(t)
	put(t, filepath.Join(cfg, "settings.json"), `{"model":"opus","foo":{"apiToken":"SECRETVAL1","bar":{"x":1},"list":[{"passwd":"SECRETVAL2","ok":1}]},"permissions":{"allow":["Bash(ls:*)"]}}`, old)
	put(t, filepath.Join(cfg, ".claude.json"), `{"mcpServers":{
"leaky":{"command":"x","args":["--api-key","SECRETVAL3"],"authToken":"SECRETVAL4","nested":{"clientSecret":"SECRETVAL5"}},
"remote":{"type":"http","url":"https://example.com/mcp?token=SECRETVAL6"},
"fine":{"command":"npx","args":["-y","pkg"],"url":"https://example.com/mcp"}}}`, old)
	data, _, err := exportBackup([]string{"settings", "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, file := range zr.File {
		readCloser, _ := file.Open()
		data, _ := io.ReadAll(readCloser)
		readCloser.Close()
		if bytes.Contains(data, []byte("SECRETVAL")) {
			t.Errorf("secret dans %s : %s", file.Name, data)
		}
	}
	files := unzipMap(t, data)
	for _, ok := range []string{"opus", "Bash(ls:*)", `"ok": 1`} {
		if !bytes.Contains([]byte(files["home/settings.json"]), []byte(ok)) {
			t.Errorf("%s perdu : %s", ok, files["home/settings.json"])
		}
	}
	var record struct {
		Servers  map[string]map[string]any `json:"servers"`
		Stripped map[string][]string       `json:"stripped"`
	}
	if err := json.Unmarshal([]byte(files["mcp.json"]), &record); err != nil {
		t.Fatal(err)
	}
	if record.Servers["fine"]["url"] != "https://example.com/mcp" || record.Servers["fine"]["args"] == nil || record.Servers["leaky"]["command"] != "x" {
		t.Errorf("serveurs bénins perdus : %+v", record.Servers)
	}
	if !slices.Contains(record.Stripped["leaky"], "args") || !slices.Contains(record.Stripped["remote"], "url") {
		t.Errorf("stripped : %+v", record.Stripped)
	}
}
