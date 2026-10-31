package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
		`cat /proc/$PPID/environ`:           true,
		`tr '\0' '\n' </proc/123/environ`:   true,
		`grep environ notes.md`:             false,
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
	data, _ := json.Marshal(guardSettings(dir))
	want := `{"hooks":{"PreToolUse":[{"hooks":[{"command":"CMD","onFailure":"block","type":"command"}],"matcher":"Bash|PowerShell|Monitor"}]},` +
		`"permissions":{"deny":["Read(//srv/x/gld/**)","Edit(//srv/x/gld/**)","Read(//srv/x/gld/auth.db)","Edit(//srv/x/gld/auth.db)","Read(//srv/x/gld/auth.key)","Edit(//srv/x/gld/auth.key)","Read(//proc/*/environ)"]},` +
		`"sandbox":{"filesystem":{"denyRead":["/srv/x/gld"],"denyWrite":["/srv/x/gld"]}}}`
	cmd, _ := json.Marshal(guardCommand(dir))
	if got := strings.Replace(string(data), string(cmd), `"CMD"`, 1); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if want := shq(guardSelf()) + " _guard '/srv/x/gld'"; guardCommand(dir) != want || !filepath.IsAbs(guardSelf()) {
		t.Fatalf("commande du hook : %s", guardCommand(dir))
	}
}

func TestGuardHookCommandLine(t *testing.T) {
	for _, record := range []struct {
		self, dir string
		win       bool
		want      string
	}{
		{"/opt/g ld/gld", "/home/a'b/.config/gld", false, `'/opt/g ld/gld' _guard '/home/a'\''b/.config/gld'`},
		{`C:\Program Files\gld\gld.exe`, `C:\Users\x\AppData\Roaming\gld`, true, `"C:/Program Files/gld/gld.exe" _guard "C:/Users/x/AppData/Roaming/gld"`},
		{`C:\a$b\gld.exe`, "C:\\d`e", true, `"C:/a\$b/gld.exe" _guard "C:/d\` + "`" + `e"`},
	} {
		if got := hookCommandLine(record.self, record.dir, record.win); got != record.want {
			t.Errorf("hookCommandLine(%q, %q, %v) = %s, attendu %s", record.self, record.dir, record.win, got, record.want)
		}
	}
}

func TestGuardHookWindowsSpellings(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", "")
	for cmd, want := range map[string]bool{
		`type auth.db.`:         true,
		`type auth.db...`:       true,
		`type "auth.db."`:       true,
		`more < auth.db::$DATA`: true,
		`copy auth.db-wal x`:    true,
		`type auth.key. > x`:    true,
		`type auth.db.bak`:      false,
		`dir gld.bak`:           false,
		`dir /srv/gld-backups`:  false,
		`type C:\srv\gld\x`:     true,
		`type C:\srv\gld.bak\x`: false,
		`type C:\srv\gld.\x`:    true,
	} {
		if got := guardHook(cmd, `C:\srv\gld`); got != want {
			t.Errorf("guardHook(%q) = %v, attendu %v", cmd, got, want)
		}
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
		{`{"tool_name":"PowerShell","tool_input":{"command":"Get-Content /a/auth.db"}}`, 2},
		{`{"tool_name":"Monitor","tool_input":{"command":"tail -F /a/x"}}`, 2},
		{`{"tool_name":"PowerShell","tool_input":{"command":"Get-ChildItem"}}`, 0},
		{`pas du json`, 2},
	}
	for _, record := range cases {
		var buffer bytes.Buffer
		if got := runGuard(strings.NewReader(record.in), &buffer, "/a"); got != record.want || (got == 2) != (buffer.Len() > 0) {
			t.Errorf("%s : code %d (stderr %q), attendu %d", record.in, got, buffer.String(), record.want)
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
	lookup := guardSettings(dir)
	lookup["model"] = "opus" // contenu étranger toléré
	lookup["permissions"].(map[string]any)["deny"] = append([]string{"Bash(rm:*)"}, guardDeny(dir)...)
	data, _ := json.Marshal(lookup)
	os.WriteFile(managedPath, data, 0o600)
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
	var buffer bytes.Buffer
	warnGuard(false, &buffer)
	if buffer.Len() != 0 {
		t.Fatalf("mode ouvert : %q", buffer.String())
	}
	warnGuard(true, &buffer)
	warnGuard(true, &buffer)
	if count := strings.Count(buffer.String(), "\n"); count != 1 || !strings.Contains(buffer.String(), managedPath) {
		t.Fatalf("un seul avertissement attendu : %q", buffer.String())
	}

	guardWarned.Store(false)
	dir, _ := absAuthDir()
	data, _ := json.Marshal(guardSettings(dir))
	os.WriteFile(managedPath, data, 0o600)
	buffer.Reset()
	warnGuard(true, &buffer)
	if buffer.Len() != 0 {
		t.Fatalf("installé : aucun avertissement attendu, eu %q", buffer.String())
	}
}

func TestCliAuthManagedSettings(t *testing.T) {
	managedFixture(t, "")
	var out bytes.Buffer
	if err := cliAuth(context.Background(), []string{"managed-settings"}, &out); err != nil {
		t.Fatal(err)
	}
	var lookup map[string]any
	if err := json.Unmarshal(out.Bytes(), &lookup); err != nil {
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
	if values := guardArgs(); values != nil {
		t.Fatalf("sans base : %v", values)
	}
	dir, _ := absAuthDir()
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "auth.db"), nil, 0o600)
	localValues := guardArgs()
	if len(localValues) != 2 || localValues[0] != "--settings" || !json.Valid([]byte(localValues[1])) {
		t.Fatalf("avec base : %v", localValues)
	}
}

func TestGuardHookPathBoundaries(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("GLD_AUTH_DIR", "")
	short := filepath.Join(home, "gld")
	for cmd, want := range map[string]bool{
		`go build ./gld`:       false,
		`cd gld && ls`:         false,
		`cat ~/gld/x`:          true,
		`ls "$HOME"/gld`:       true,
		`ls ${HOME}/gld/`:      true,
		`ls ` + short:          true,
		`ls ` + short + `-old`: false,
	} {
		if got := guardHook(cmd, short); got != want {
			t.Errorf("dossier %s, guardHook(%q) = %v, attendu %v", short, cmd, got, want)
		}
	}
	for cmd, want := range map[string]bool{
		`ls /srv/gld-backups`: false,
		`ls /srv/gld.bak`:     false,
		`ls /srv/gld`:         true,
		`cat /srv/gld/x`:      true,
		`ls /srv/gld;echo`:    true,
	} {
		if got := guardHook(cmd, "/srv/gld"); got != want {
			t.Errorf("guardHook(%q) = %v, attendu %v", cmd, got, want)
		}
	}
}

func TestWithGuardStripsUserSettings(t *testing.T) {
	managedFixture(t, "")
	dir, _ := absAuthDir()
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "auth.db"), nil, 0o600)
	values := guardArgs()
	for _, in := range [][]string{
		{"--settings", "/tmp/evil.json", "--model", "opus", "--settings=/tmp/e2.json", "--resume", "id1", "--", "p"},
		{"--settings=/tmp/e2.json", "-p", "--resume", "id1", "--output-format", "stream-json", "--", "--settings x"},
		{"--settings", "/tmp/evil.json"},
	} {
		out := withGuard(in)
		count := 0
		for index, argument := range out {
			if argument == "/tmp/evil.json" || argument == "--settings=/tmp/e2.json" {
				t.Fatalf("réglage utilisateur conservé : %v", out)
			}
			if argument == "--settings" {
				count++
				if out[index+1] != values[1] {
					t.Fatalf("mauvais --settings : %v", out)
				}
				if index+2 < len(out) && out[index+2] != "--resume" && out[index+2] != "--" {
					t.Fatalf("garde-fou pas juste avant --resume ou le prompt : %v", out)
				}
			}
		}
		if count != 1 {
			t.Fatalf("--settings du garde-fou attendu une fois : %v", out)
		}
		if line := in[len(in)-1]; line == "--settings x" && out[len(out)-1] != line {
			t.Fatalf("prompt modifié : %v", out)
		}
	}
}

func TestManagedInstalledExact(t *testing.T) {
	managedFixture(t, "")
	dir, _ := absAuthDir()
	write := func(lookup map[string]any) {
		data, _ := json.Marshal(lookup)
		os.WriteFile(managedPath, data, 0o600)
	}
	hookOn := func(matcher, cmd string, args []string, onFailure string) map[string]any {
		lookup := guardSettings(dir)
		localLookup := map[string]any{"type": "command", "command": cmd, "args": args}
		if onFailure != "" {
			localLookup["onFailure"] = onFailure
		}
		lookup["hooks"] = map[string]any{"PreToolUse": []any{map[string]any{"matcher": matcher, "hooks": []any{localLookup}}}}
		return lookup
	}
	hook := func(matcher, cmd string, args []string) map[string]any { return hookOn(matcher, cmd, args, "block") }
	self, args, line := guardSelf(), []string{"_guard", dir}, guardCommand(dir)
	for _, record := range []struct {
		m    map[string]any
		want bool
	}{
		{hook("Bash|PowerShell|Monitor", line, nil), true},
		{hookOn("Bash|PowerShell|Monitor", line, nil, ""), false}, // sans onFailure, un hook en échec laisse passer
		{hookOn("Bash|PowerShell|Monitor", line, nil, "ignore"), false},
		{hook("Bash", line+" ", nil), false},
		{hook("Bash", hookCommandLine(self, "/autre/dossier", false), nil), false},
		{hook("Bash|PowerShell", self, args), true}, // forme exec du commit précédent
		{hook("Bash", self, args), true},
		{hook("PowerShell", self, args), false},
		{hook("Bash", "/autre/gld", args), false},
		{hook("Bash", self, []string{"_guard", "/autre/dossier"}), false},
	} {
		write(record.m)
		if got := managedInstalled(); got != record.want {
			data, _ := json.Marshal(record.m["hooks"])
			t.Errorf("%s : %v, attendu %v", data, got, record.want)
		}
	}
	other := guardSettings("/autre/dossier")
	write(other)
	guardWarned.Store(false)
	t.Cleanup(func() { guardWarned.Store(false) })
	var buffer bytes.Buffer
	warnGuard(true, &buffer)
	if buffer.Len() == 0 {
		t.Fatal("fichier d'un autre dossier : avertissement attendu")
	}
}

func TestCliAuthUnknownSubcommand(t *testing.T) {
	if err := cliAuth(context.Background(), []string{"nimporte"}, io.Discard); err == nil || !strings.Contains(err.Error(), "nimporte") {
		t.Fatalf("sous-commande inconnue acceptée : %v", err)
	}
}
