//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// fakeAccounts : alice (1500, groupes 1500 et 27), wheel0 (1600, membre du groupe 0), root ; groupes connus 1500, 1700, 27, 0.
func fakeAccounts(t *testing.T, self int) {
	users := map[string]*user.User{
		"alice":  {Uid: "1500", Gid: "1500", Username: "alice", HomeDir: "/home/alice"},
		"wheel0": {Uid: "1600", Gid: "1600", Username: "wheel0", HomeDir: "/home/wheel0"},
		"root":   {Uid: "0", Gid: "0", Username: "root", HomeDir: "/root"},
	}
	groups := map[string][]string{"alice": {"1500", "27"}, "wheel0": {"1600", "0"}, "root": {"0"}}
	known := map[string]bool{"1500": true, "1600": true, "1700": true, "27": true, "0": true}
	saved := []any{lookupUser, lookupUserID, lookupGroupID, userGroupIDs, gatewayUID, gatewayEUID, procStatus, canReadAs, runAsAcct}
	t.Cleanup(func() {
		lookupUser = saved[0].(func(string) (*user.User, error))
		lookupUserID = saved[1].(func(string) (*user.User, error))
		lookupGroupID = saved[2].(func(string) (*user.Group, error))
		userGroupIDs = saved[3].(func(*user.User) ([]string, error))
		gatewayUID = saved[4].(func() int)
		gatewayEUID = saved[5].(func() int)
		procStatus = saved[6].(func() ([]byte, error))
		canReadAs = saved[7].(func(string) (bool, error))
		runAsAcct = saved[8].(*runAsAccount)
	})
	lookupUser = func(n string) (*user.User, error) {
		if u, ok := users[n]; ok {
			return u, nil
		}
		return nil, user.UnknownUserError(n)
	}
	lookupUserID = func(id string) (*user.User, error) {
		for _, u := range users {
			if u.Uid == id {
				return u, nil
			}
		}
		return nil, errors.New("unknown uid")
	}
	lookupGroupID = func(id string) (*user.Group, error) {
		if known[id] {
			return &user.Group{Gid: id}, nil
		}
		return nil, errors.New("unknown gid")
	}
	userGroupIDs = func(u *user.User) ([]string, error) { return groups[u.Username], nil }
	gatewayUID = func() int { return self }
	gatewayEUID = func() int { return 0 }
	procStatus = func() ([]byte, error) { return nil, os.ErrNotExist }
	canReadAs = func(string) (bool, error) { return false, nil }
	runAsAcct = nil
}

func TestParseRunAs(t *testing.T) {
	fakeAccounts(t, 1000)
	cases := []struct {
		spec, err string
		uid, gid  uint32
	}{
		{"", "", 0, 0},
		{"alice", "", 1500, 1500},
		{"1500", "", 1500, 1500},
		{"1500:1700", "", 1500, 1700},
		{"alice:1700", "", 1500, 1700},
		{"1500:9999", L("srv.runas.unknownGroup", "A1", "9999"), 0, 0},
		{"1500:staff", L("srv.runas.unknownGroup", "A1", "staff"), 0, 0},
		{"bob", L("srv.runas.unknownUser", "A1", "bob"), 0, 0},
		{"4242", L("srv.runas.unknownUser", "A1", "4242"), 0, 0},
		{"root", L("srv.runas.root"), 0, 0},
		{"0", L("srv.runas.root"), 0, 0},
		{"alice:0", L("srv.runas.root"), 0, 0},
		{"wheel0", L("srv.runas.root"), 0, 0},
	}
	for _, c := range cases {
		a, err := parseRunAs(c.spec)
		switch {
		case c.err != "":
			if err == nil || err.Error() != c.err || a != nil {
				t.Errorf("%q : %v, %v ; attendu %q", c.spec, a, err, c.err)
			}
		case c.spec == "":
			if a != nil || err != nil {
				t.Errorf("vide : %v, %v", a, err)
			}
		case err != nil:
			t.Errorf("%q : %v", c.spec, err)
		case a.cred.Uid != c.uid || a.cred.Gid != c.gid || !slices.Equal(a.cred.Groups, []uint32{1500, 27}) || a.home != "/home/alice" || a.name != "alice":
			t.Errorf("%q : %+v %+v", c.spec, a, a.cred)
		}
	}
	gatewayUID = func() int { return 1500 }
	if _, err := parseRunAs("alice"); err == nil || err.Error() != L("srv.runas.self", "A1", "alice") {
		t.Errorf("son propre compte accepté : %v", err)
	}
	t.Setenv("GLD_RUN_AS", "1500")
	gatewayUID = func() int { return 1000 }
	if c, err := runAs(); err != nil || c.Uid != 1500 {
		t.Errorf("runAs : %+v %v", c, err)
	}
}

func testAccount() *runAsAccount {
	return &runAsAccount{cred: &syscall.Credential{Uid: 1500, Gid: 1500, Groups: []uint32{1500, 27}}, name: "alice", home: "/home/alice", shell: "/bin/zsh"}
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		if _, dup := m[k]; dup {
			m[k+"#dup"] = v
		}
		m[k] = v
	}
	return m
}

func TestApplyRunAsSetsCredential(t *testing.T) {
	fakeAccounts(t, 1000)
	runAsAcct = testAccount()
	attr := &syscall.SysProcAttr{Setsid: true, Setpgid: true, Setctty: true, Ctty: 3}
	cmd := exec.Command("claude", "-p")
	cmd.SysProcAttr = attr
	cmd.Env = []string{"PATH=/usr/bin", "GLD_AUTH_KEY=k", "GLD_WEB_TOKEN=t", childTokenEnv + "=c", "GLD_RUN_AS=alice", "GLD_AUTH_DIR=/etc/gld",
		"HOME=/root", "USER=root", "LOGNAME=root", "SHELL=/bin/bash", "TMUX=/tmp/tmux-0/default,1,0", "TMUX_TMPDIR=/run", "SSH_AUTH_SOCK=/s",
		"XDG_RUNTIME_DIR=/run/user/0", "CLAUDE_CONFIG_DIR=/srv/home1", "FOO=bar"}
	applyRunAs(cmd)
	if cmd.SysProcAttr != attr || !attr.Setsid || !attr.Setpgid || !attr.Setctty || attr.Ctty != 3 {
		t.Fatalf("SysProcAttr existant écrasé : %+v", cmd.SysProcAttr)
	}
	c := attr.Credential
	if c == nil || c.Uid != 1500 || c.Gid != 1500 || !slices.Equal(c.Groups, []uint32{1500, 27}) || c.NoSetGroups {
		t.Fatalf("Credential : %+v", c)
	}
	c.Groups[0] = 9
	if runAsAcct.cred.Groups[0] != 1500 {
		t.Error("Credential partagé avec le compte : une commande pourrait le modifier")
	}
	m := envMap(cmd.Env)
	for _, k := range []string{"GLD_AUTH_KEY", "GLD_WEB_TOKEN", childTokenEnv, "GLD_RUN_AS", "GLD_AUTH_DIR", "TMUX", "TMUX_TMPDIR", "SSH_AUTH_SOCK", "XDG_RUNTIME_DIR"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s transmis au compte cible", k)
		}
	}
	want := map[string]string{"HOME": "/home/alice", "USER": "alice", "LOGNAME": "alice", "SHELL": "/bin/zsh", "CLAUDE_CONFIG_DIR": "/srv/home1", "FOO": "bar", "PATH": "/usr/bin"}
	for k, v := range want {
		if m[k] != v || m[k+"#dup"] != "" {
			t.Errorf("%s = %q (doublon %q), attendu %q", k, m[k], m[k+"#dup"], v)
		}
	}

	// gld enfant : son jeton passe ; sans CLAUDE_CONFIG_DIR, celui de la passerelle est fixé ; sans SysProcAttr, il est créé.
	old := root
	root = "/srv/main/.claude/projects"
	t.Cleanup(func() { root = old })
	cmd = exec.Command("gld", "web")
	cmd.Env = []string{childTokenEnv + "=c", "GLD_WEB_TOKEN=t"}
	applyRunAs(cmd, childTokenEnv)
	m = envMap(cmd.Env)
	if m[childTokenEnv] != "c" || m["GLD_WEB_TOKEN"] != "" || m["CLAUDE_CONFIG_DIR"] != "/srv/main/.claude" {
		t.Errorf("env enfant : %v", cmd.Env)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil || cmd.SysProcAttr.Credential.Uid != 1500 {
		t.Errorf("SysProcAttr absent : %+v", cmd.SysProcAttr)
	}

	// withPath : ~/.local/bin du compte cible, puis la séparation.
	cmd = withPath(exec.Command("claude", "-p"))
	if m := envMap(cmd.Env); !strings.HasPrefix(m["PATH"], "/home/alice/.local/bin:") || m["HOME"] != "/home/alice" || cmd.SysProcAttr == nil {
		t.Errorf("withPath sous GLD_RUN_AS : %v %+v", cmd.Env, cmd.SysProcAttr)
	}
}

func TestRunAsUnsetIsNoop(t *testing.T) {
	fakeAccounts(t, 1000)
	t.Setenv("GLD_RUN_AS", "")
	if err := checkRunAsSeparation(); err != nil || runAsAcct != nil || runAsOn() || tmuxDetached() {
		t.Fatalf("GLD_RUN_AS vide : %v %v", err, runAsAcct)
	}
	mk := func() *exec.Cmd {
		c := exec.Command("tmux", "new-session", "-t", "$1")
		c.Env = []string{"TERM=xterm-256color", "GLD_AUTH_KEY=k", "HOME=/root"}
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return c
	}
	a, b := mk(), mk()
	applyRunAs(a, childTokenEnv)
	if !reflect.DeepEqual(a.Args, b.Args) || !reflect.DeepEqual(a.Env, b.Env) || !reflect.DeepEqual(a.SysProcAttr, b.SysProcAttr) || a.Path != b.Path {
		t.Errorf("commande modifiée sans GLD_RUN_AS : %+v", a)
	}
	c := exec.Command("git", "status")
	applyRunAs(c)
	if c.Env != nil || c.SysProcAttr != nil {
		t.Errorf("Env ou SysProcAttr créés sans GLD_RUN_AS : %v %+v", c.Env, c.SysProcAttr)
	}
	if l, _ := launcherCmd("tmux", "t", []string{"x"}); slices.Contains(l.Args, "-d") {
		t.Errorf("lanceur tmux détaché sans GLD_RUN_AS : %v", l.Args)
	}
}

func TestRunAsLauncherIsDetachedTmux(t *testing.T) {
	fakeAccounts(t, 1000)
	runAsAcct = testAccount()
	if launcherAvailable("kitty") || launcherAvailable("terminator-tmux") || tmuxLauncher() != "" {
		t.Error("un émulateur de bureau est proposé sous GLD_RUN_AS")
	}
	c, err := launcherCmd("tmux", "a#b", []string{"/usr/bin/gld", "_run", "/tmp/x.json"})
	if err != nil || !slices.Equal(c.Args[:5], []string{"tmux", "new-session", "-d", "-n", "a##b"}) {
		t.Errorf("lanceur : %v %v", c, err)
	}
	runAsAcct = nil
	t.Setenv(runAsChildEnv, "1")
	if !tmuxDetached() {
		t.Error("gld enfant : tmux attaché")
	}
}

func TestRunAsRefusesNotPrivileged(t *testing.T) {
	fakeAccounts(t, 1000)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	t.Setenv("GLD_RUN_AS", "alice")
	gatewayEUID = func() int { return 1000 }
	caps := func(eff, amb uint64) {
		procStatus = func() ([]byte, error) {
			return []byte("Name:\tgld\nCapEff:\t" + strconv.FormatUint(eff, 16) + "\nCapAmb:\t" + strconv.FormatUint(amb, 16) + "\n"), nil
		}
	}
	for _, c := range []struct {
		eff, amb uint64
		err      string
	}{
		{0, 0, L("srv.runas.notPrivileged")},
		{1 << 7, 0, L("srv.runas.notPrivileged")}, // CAP_SETUID seul
		{runAsCaps, runAsCaps, L("srv.runas.ambientCaps")},
		{runAsCaps, 0, ""},
	} {
		caps(c.eff, c.amb)
		runAsAcct = nil
		err := checkRunAsSeparation()
		if c.err == "" {
			if err != nil || runAsAcct == nil {
				t.Errorf("capacités %x : %v", c.eff, err)
			}
		} else if err == nil || err.Error() != c.err || runAsAcct != nil {
			t.Errorf("capacités %x/%x : %v, attendu %q", c.eff, c.amb, err, c.err)
		}
	}
	procStatus = func() ([]byte, error) { return nil, os.ErrNotExist } // macOS : pas de /proc, euid 0 exigé
	runAsAcct = nil
	if err := checkRunAsSeparation(); err == nil || runAsAcct != nil {
		t.Error("démarré sans privilège vérifiable")
	}
}

func TestRunAsRefusesReadableAuthDir(t *testing.T) {
	fakeAccounts(t, 1000)
	dir := t.TempDir()
	t.Setenv("GLD_AUTH_DIR", dir)
	t.Setenv("GLD_RUN_AS", "alice")
	var asked string
	canReadAs = func(d string) (bool, error) {
		asked = d
		if runAsAcct == nil {
			t.Error("vérification lancée hors du compte cible")
		}
		return true, nil
	}
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.authDirReadable", "A1", "alice", "A2", dir) || runAsAcct != nil || asked != dir {
		t.Fatalf("dossier lisible accepté : %v (%q)", err, asked)
	}
	canReadAs = func(string) (bool, error) { return false, errors.New("permission denied") }
	if err := checkRunAsSeparation(); err == nil || !strings.Contains(err.Error(), "permission denied") || runAsAcct != nil {
		t.Fatalf("échec de la vérification accepté : %v", err)
	}
	canReadAs = func(string) (bool, error) { return false, nil }
	if err := checkRunAsSeparation(); err != nil || runAsAcct == nil || runAsAcct.name != "alice" {
		t.Fatalf("séparation correcte refusée : %v", err)
	}

	// dossier personnel du compte cible dans le dossier des comptes
	runAsAcct = nil
	t.Setenv("GLD_AUTH_DIR", "/home/alice/.config/gld")
	if err := checkRunAsSeparation(); err != nil {
		t.Fatalf("dossier des comptes sous le home accepté par la règle 3 : %v", err)
	}
	runAsAcct = nil
	t.Setenv("GLD_AUTH_DIR", "/home")
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.homeInAuthDir", "A1", "/home/alice", "A2", "/home") {
		t.Fatalf("home dans le dossier des comptes accepté : %v", err)
	}
}

func TestRunCanRead(t *testing.T) {
	dir := t.TempDir()
	if runCanRead(dir) != 3 {
		t.Error("dossier lisible non signalé")
	}
	if runCanRead(filepath.Join(dir, "absent")) != 0 {
		t.Error("dossier absent signalé lisible")
	}
	if !within("/a/b", "/a/b") || !within("/a/b", "/a/b/c") || within("/a/b", "/a/bc") || within("/a/b", "/a") {
		t.Error("within")
	}
}

// Changement de compte réel : seulement dans un espace de noms où l'uid 1000 est projeté, par exemple
// `unshare --user --map-root-user --map-auto` avec GLD_RUNAS_E2E=1.
func TestRunAsRealSwitch(t *testing.T) {
	if os.Getenv("GLD_RUNAS_E2E") != "1" || os.Geteuid() != 0 {
		t.Skip("GLD_RUNAS_E2E=1 sous root (espace de noms utilisateur) requis")
	}
	fakeAccounts(t, 0)
	runAsAcct = &runAsAccount{cred: &syscall.Credential{Uid: 1000, Gid: 1000}, name: "u1000", home: "/nonexistent", shell: "/bin/sh"}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "auth.db"), []byte("x"), 0o600)
	cmd := exec.Command("sh", "-c", "id -u; cat "+filepath.Join(dir, "auth.db")+" 2>&1; ls "+dir+" 2>&1; echo HOME=$HOME")
	cmd.Dir = "/"
	applyRunAs(cmd)
	out, _ := cmd.CombinedOutput()
	s := string(out)
	if !strings.HasPrefix(s, "1000\n") || strings.Contains(s, "\nx") || !strings.Contains(s, "HOME=/nonexistent") || strings.Count(s, "ermission") < 2 {
		t.Fatalf("sortie sous le compte cible :\n%s", s)
	}
}
