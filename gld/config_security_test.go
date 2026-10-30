package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskSettingsStrict(t *testing.T) {
	v, err := parseOrdered([]byte(`{"env":{"HTTPS_PROXY":"http://u:pw@proxy:8080","N":{"deep":["x1"]}},
"authToken":{"inner":{"a":["nested-secret"]}},
"hooks":{"Stop":[{"matcher":"m1","hooks":[{"type":"command","command":"curl -u a:inlinepw"}]}]},
"permissions":{"allow":["Bash(curl -H \"Authorization: Bearer abc123\")","Bash(make:*)"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	loose := formatOrdered(maskSettings(v, false))
	for _, s := range []string{"nested-secret"} {
		if strings.Contains(loose, s) {
			t.Errorf("drapeau non propagé : %s", loose)
		}
	}
	if !strings.Contains(loose, "http://u:pw@proxy:8080") || !strings.Contains(loose, "inlinepw") {
		t.Errorf("le mode normal doit garder env et hooks : %s", loose)
	}
	strict := formatOrdered(maskSettings(v, true))
	for _, s := range []string{"pw@proxy", "x1", "inlinepw", "abc123"} {
		if strings.Contains(strict, s) {
			t.Errorf("%q visible en mode strict : %s", s, strict)
		}
	}
	for _, s := range []string{"HTTPS_PROXY", "m1", "Bash(make:*)"} {
		if !strings.Contains(strict, s) {
			t.Errorf("%q devrait rester visible : %s", s, strict)
		}
	}
}

func TestMcpStrictShowsNoArgs(t *testing.T) {
	v, _ := parseOrdered([]byte(`{"s":{"command":"npx","args":["-y","pg","postgresql://u:pw@h/db"],"url":"https://h.example/tok123/x?k=1"}}`))
	m := mcpFrom(v, "user", "", true)[0]
	if m.Command != "npx" || m.ArgCount != 3 || m.URL != "https://h.example" {
		t.Errorf("vue stricte : %+v", m)
	}
	if got := mcpFrom(v, "user", "", false)[0]; !strings.Contains(got.Command, "postgresql://") || got.ArgCount != 0 {
		t.Errorf("vue normale changée : %+v", got)
	}
}

func TestMcpMaskURLFailsClosed(t *testing.T) {
	for _, raw := range []string{"https://u:p^w@%zz", "not a url with token=abc", "/path/only?token=abc", "http://a b/"} {
		if got := mcpMaskURL(raw, false); got != "" {
			t.Errorf("mcpMaskURL(%q) = %q", raw, got)
		}
	}
}

func TestDecodeDepthLimit(t *testing.T) {
	ok := strings.Repeat("[", maxJSONDepth) + strings.Repeat("]", maxJSONDepth)
	if _, err := parseOrdered([]byte(ok)); err != nil {
		t.Errorf("profondeur permise refusée : %v", err)
	}
	deep := strings.Repeat("[", 5_000_000)
	if _, err := parseOrdered([]byte(deep)); err == nil {
		t.Error("imbrication excessive acceptée")
	}
	if _, err := parseOrdered([]byte(strings.Repeat(`{"a":`, maxJSONDepth+1))); err == nil {
		t.Error("objets imbriqués au-delà de la limite acceptés")
	}
}

func TestReadConfigRefusesHugeFile(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "settings.json"), "{\"a\":\""+strings.Repeat("x", maxConfigBytes)+"\"}", old)
	f, v := readConfig("u")
	if v != nil || f.Error == "" {
		t.Errorf("gros fichier accepté : %+v", f)
	}
}

func TestConfigRoutesHideSecretsWithoutConfigWrite(t *testing.T) {
	s := testStore(t)
	cfg := t.TempDir()
	use(cfg)
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"db":{"command":"srv","args":["postgresql://u:pw0@h/db"]}}}`, old)
	put(t, filepath.Join(cfg, "settings.json"), `{"env":{"HTTPS_PROXY":"http://u:pw1@p:1"},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"run pw2"}]}]}}`, old)
	mux := http.NewServeMux()
	routesConfig(mux)
	g := &gateRig{s: s, h: authGate(mux, s, mux), cfg: cfg}
	ro := g.user(t, "lecteur", false)
	g.grant(t, ro, "", RightMemRead)
	rw := g.user(t, "reglages", false)
	g.grant(t, rw, "", RightMemRead, RightConfigWrite)
	get := func(u gateUser, path string) string {
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, g.req("GET", path, &u, "", ""))
		if w.Code != 200 {
			t.Fatalf("%s : %d", path, w.Code)
		}
		return w.Body.String()
	}
	for _, p := range []string{"/v1/config", "/v1/config/effective", "/v1/config/mcp", "/v1/config/projects"} {
		b := get(ro, p)
		for _, sec := range []string{"pw0", "pw1", "pw2"} {
			if strings.Contains(b, sec) {
				t.Errorf("%s : %s visible sans config_write", p, sec)
			}
		}
	}
	if b := get(rw, "/v1/config"); !strings.Contains(b, "pw1") || !strings.Contains(b, "pw2") {
		t.Errorf("config_write doit garder la vue complète : %s", b)
	}
	if b := get(rw, "/v1/config/mcp"); !strings.Contains(b, "postgresql://") {
		t.Errorf("config_write doit voir les arguments : %s", b)
	}
	if b := get(ro, "/v1/config/mcp"); !strings.Contains(b, `"argCount":1`) {
		t.Errorf("nombre d'arguments absent : %s", b)
	}
}
