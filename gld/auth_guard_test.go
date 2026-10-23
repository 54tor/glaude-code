package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardHookDenies(t *testing.T) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "gld")
	t.Setenv("GLD_AUTH_DIR", "")
	for cmd, want := range map[string]bool{
		`cat ~/.config/gld/auth.db`:         true,
		`sqlite3 x`:                         true,
		`python -c "open('auth.key')"`:      true,
		`python3 -c "import sqlite3"`:       true,
		`cp auth.db /tmp/x`:                 true,
		`cat "$HOME/.config/gld/auth"."db"`: true,
		`cat au\th.d"b"`:                    true,
		`ls ~/.config/gld`:                  true,
		`tar c "$HOME"/.config/gld`:         true,
		`ls ` + dir:                         true,
		`echo $GLD_AUTH_DIR`:                true,
		`ls`:                                false,
		`echo hello`:                        false,
		`go test ./...`:                     false,
		`pip install pysqlite3-binary`:      false,
		`cat ~/.config/other/auth.json`:     false,
		`f=auth; cat ~/.config/gl?/$f.d?`:   false, // contournement connu : variables et jokers
	} {
		if got := guardHook(cmd, dir); got != want {
			t.Errorf("guardHook(%q) = %v, attendu %v", cmd, got, want)
		}
	}
}

func TestGuardSettingsShape(t *testing.T) {
	dir := "/srv/x/gld"
	b, _ := json.Marshal(guardSettings(dir))
	want := `{"hooks":{"PreToolUse":[{"hooks":[{"command":"CMD","type":"command"}],"matcher":"Bash"}]},` +
		`"permissions":{"deny":["Read(//srv/x/gld/**)","Edit(//srv/x/gld/**)","Read(//srv/x/gld/auth.db)","Edit(//srv/x/gld/auth.db)","Read(//srv/x/gld/auth.key)","Edit(//srv/x/gld/auth.key)"]},` +
		`"sandbox":{"filesystem":{"denyRead":["/srv/x/gld"],"denyWrite":["/srv/x/gld"]}}}`
	cmd, _ := json.Marshal(guardCommand(dir))
	if got := strings.Replace(string(b), string(cmd), `"CMD"`, 1); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if !strings.HasSuffix(guardCommand(dir), " _guard '/srv/x/gld'") || !filepath.IsAbs(strings.Trim(strings.Fields(guardCommand(dir))[0], "'")) {
		t.Fatalf("commande du hook : %s", guardCommand(dir))
	}
}

func TestGuardProtocol(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`{"tool_name":"Bash","tool_input":{"command":"cat /a/auth.db"}}`, 2},
		{`{"tool_name":"Bash","tool_input":{"command":"ls"}}`, 0},
		{`{"tool_name":"Read","tool_input":{"file_path":"/a/auth.db"}}`, 0},
		{`pas du json`, 2},
	}
	for _, c := range cases {
		var e bytes.Buffer
		if got := runGuard(strings.NewReader(c.in), &e, "/a"); got != c.want || (got == 2) != (e.Len() > 0) {
			t.Errorf("%s : code %d (stderr %q), attendu %d", c.in, got, e.String(), c.want)
		}
	}
}

func managedFixture(t *testing.T, content string) {
	t.Helper()
	t.Setenv("GLD_AUTH_DIR", filepath.Join(t.TempDir(), "gld"))
	old := managedPath
	managedPath = filepath.Join(t.TempDir(), "managed-settings.json")
	t.Cleanup(func() { managedPath = old })
	if content != "" {
		if err := os.WriteFile(managedPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagedInstalled(t *testing.T) {
	managedFixture(t, "")
	if managedInstalled() {
		t.Fatal("absent : faux attendu")
	}
	dir, _ := absAuthDir()
	m := guardSettings(dir)
	m["model"] = "opus" // contenu étranger toléré
	m["permissions"].(map[string]any)["deny"] = append([]string{"Bash(rm:*)"}, guardDeny(dir)...)
	b, _ := json.Marshal(m)
	os.WriteFile(managedPath, b, 0o600)
	if !managedInstalled() {
		t.Fatal("installé : vrai attendu")
	}
	os.WriteFile(managedPath, []byte(`{"permissions":{"deny":[]}}`), 0o600)
	if managedInstalled() {
		t.Fatal("incomplet : faux attendu")
	}
}

func TestManagedWarningOnlyInAuthMode(t *testing.T) {
	managedFixture(t, "")
	guardWarned.Store(false)
	t.Cleanup(func() { guardWarned.Store(false) })
	var w bytes.Buffer
	warnGuard(false, &w)
	if w.Len() != 0 {
		t.Fatalf("mode ouvert : %q", w.String())
	}
	warnGuard(true, &w)
	warnGuard(true, &w)
	if n := strings.Count(w.String(), "\n"); n != 1 || !strings.Contains(w.String(), managedPath) {
		t.Fatalf("un seul avertissement attendu : %q", w.String())
	}

	guardWarned.Store(false)
	dir, _ := absAuthDir()
	b, _ := json.Marshal(guardSettings(dir))
	os.WriteFile(managedPath, b, 0o600)
	w.Reset()
	warnGuard(true, &w)
	if w.Len() != 0 {
		t.Fatalf("installé : aucun avertissement attendu, eu %q", w.String())
	}
}

func TestCliAuthManagedSettings(t *testing.T) {
	managedFixture(t, "")
	var out bytes.Buffer
	if err := cliAuth(context.Background(), []string{"managed-settings"}, &out); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("sortie non JSON : %v", err)
	}
	dir, _ := absAuthDir()
	if !strings.Contains(out.String(), "Read(/"+dir+"/**)") {
		t.Fatalf("chemin absolu du dossier absent : %s", out.String())
	}
	if _, err := os.Stat(managedPath); err == nil {
		t.Fatal("la commande ne doit rien écrire")
	}
}

func TestGuardArgsOnlyWithAuthDB(t *testing.T) {
	managedFixture(t, "")
	if a := guardArgs(); a != nil {
		t.Fatalf("sans base : %v", a)
	}
	dir, _ := absAuthDir()
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "auth.db"), nil, 0o600)
	a := guardArgs()
	if len(a) != 2 || a[0] != "--settings" || !json.Valid([]byte(a[1])) {
		t.Fatalf("avec base : %v", a)
	}
}
