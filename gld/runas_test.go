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

	"golang.org/x/sys/unix"
)

// fakeAccounts : alice (1500, groupes 1500 et 27), wheel0 (1600, membre du groupe 0), root ; groupes connus 1500, 1700, 27, 0.
func fakeAccounts(t *testing.T, self int) (bin string) {
	users := map[string]*user.User{
		"alice":  {Uid: "1500", Gid: "1500", Username: "alice", HomeDir: "/home/alice"},
		"wheel0": {Uid: "1600", Gid: "1600", Username: "wheel0", HomeDir: "/home/wheel0"},
		"root":   {Uid: "0", Gid: "0", Username: "root", HomeDir: "/root"},
	}
	groups := map[string][]string{"alice": {"1500", "27"}, "wheel0": {"1600", "0"}, "root": {"0"}}
	known := map[string]bool{"1500": true, "1600": true, "1700": true, "27": true, "0": true}
	for _, value := range []any{&lookupUser, &lookupUserID, &lookupGroupID, &userGroupIDs, &gatewayUID, &gatewayEUID, &procStatus,
		&canReadAs, &selfExe, &binaryHasCaps, &lstatOwner, &hasACL, &runAsGOOS, &access, &runAsAcct} {
		saveVar(t, value)
	}
	lookupUser = func(name string) (*user.User, error) {
		if localUser, ok := users[name]; ok {
			return localUser, nil
		}
		return nil, user.UnknownUserError(name)
	}
	lookupUserID = func(id string) (*user.User, error) {
		for _, localUser := range users {
			if localUser.Uid == id {
				return localUser, nil
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
	userGroupIDs = func(localUser *user.User) ([]string, error) { return groups[localUser.Username], nil }
	gatewayUID = func() int { return self }
	gatewayEUID = func() int { return 0 }
	procStatus = func() ([]byte, error) { return []byte("CapInh:\t0000000000000000\nCapAmb:\t0000000000000000\n"), nil }
	canReadAs = func(string, string) (bool, bool, error) { return false, false, nil }
	bin = filepath.Join(t.TempDir(), "gld")
	os.WriteFile(bin, nil, 0o755)
	selfExe = func() (string, error) { return bin, nil }
	binaryHasCaps = func(string) (bool, error) { return false, nil }
	lstatOwner = rootOwned
	hasACL = func(string) (bool, error) { return false, nil }
	runAsGOOS = "linux"
	runAsAcct = nil
	t.Setenv("GLD_RUN_AS_EXPERIMENTAL", "1")
	return bin
}

// authTempDir : un dossier des comptes valide (0700).
func authTempDir(t *testing.T) string {
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	return dir
}

// rootOwned : le vrai Lstat, mais tout appartient à root (les tests ne tournent pas en root).
func rootOwned(path string) (uint32, uint32, os.FileMode, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, 0, 0, err
	}
	return 0, 0, fi.Mode(), nil
}

// saveVar : remet la variable pointée (func ou *runAsAccount) à sa valeur en fin de test.
func saveVar(t *testing.T, value any) {
	localValue := reflect.ValueOf(value).Elem()
	old := reflect.New(localValue.Type()).Elem()
	old.Set(localValue)
	t.Cleanup(func() { localValue.Set(old) })
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
	for _, record := range cases {
		runAsAccount, err := parseRunAs(record.spec)
		switch {
		case record.err != "":
			if err == nil || err.Error() != record.err || runAsAccount != nil {
				t.Errorf("%q : %v, %v ; attendu %q", record.spec, runAsAccount, err, record.err)
			}
		case record.spec == "":
			if runAsAccount != nil || err != nil {
				t.Errorf("vide : %v, %v", runAsAccount, err)
			}
		case err != nil:
			t.Errorf("%q : %v", record.spec, err)
		case runAsAccount.cred.Uid != record.uid || runAsAccount.cred.Gid != record.gid || !slices.Equal(runAsAccount.cred.Groups, []uint32{1500, 27}) || runAsAccount.home != "/home/alice" || runAsAccount.name != "alice":
			t.Errorf("%q : %+v %+v", record.spec, runAsAccount, runAsAccount.cred)
		}
	}
	gatewayUID = func() int { return 1500 }
	if _, err := parseRunAs("alice"); err == nil || err.Error() != L("srv.runas.self", "A1", "alice") {
		t.Errorf("son propre compte accepté : %v", err)
	}
	t.Setenv("GLD_RUN_AS", "1500")
	gatewayUID = func() int { return 1000 }
	if credential, err := runAs(); err != nil || credential.Uid != 1500 {
		t.Errorf("runAs : %+v %v", credential, err)
	}
}

func testAccount() *runAsAccount {
	return &runAsAccount{cred: &syscall.Credential{Uid: 1500, Gid: 1500, Groups: []uint32{1500, 27}}, name: "alice", home: "/home/alice", shell: "/bin/zsh"}
}

func envMap(env []string) map[string]string {
	lookup := map[string]string{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if _, dup := lookup[key]; dup {
			lookup[key+"#dup"] = value
		}
		lookup[key] = value
	}
	return lookup
}

func TestApplyRunAsSetsCredential(t *testing.T) {
	fakeAccounts(t, 1000)
	runAsAcct = testAccount()
	attr := &syscall.SysProcAttr{Setsid: true, Setpgid: true, Setctty: true, Ctty: 3}
	cmd := exec.Command("/bin/true", "-p", "a b")
	cmd.SysProcAttr = attr
	cmd.Env = []string{"PATH=/usr/bin", "GLD_AUTH_KEY=k", "GLD_AUTH_OTHER=o", "GLD_WEB_TOKEN=t", "GLD_WEB_PORT=1", childTokenEnv + "=c",
		"GLD_RUN_AS=alice", "GLD_RUN_AS_EXPERIMENTAL=1", "GLD_AUTH_DIR=/etc/gld",
		"HOME=/root", "USER=root", "LOGNAME=root", "SHELL=/bin/bash", "TMUX=/tmp/tmux-0/default,1,0", "TMUX_TMPDIR=/run", "SSH_AUTH_SOCK=/s",
		"XDG_RUNTIME_DIR=/run/user/0", "CLAUDE_CONFIG_DIR=/srv/home1", "CLAUDE_CODE_X=1", "ANTHROPIC_API_KEY=a", "FOO=bar"}
	applyRunAs(cmd)
	if cmd.SysProcAttr != attr || !attr.Setsid || !attr.Setpgid || !attr.Setctty || attr.Ctty != 3 {
		t.Fatalf("SysProcAttr existant écrasé : %+v", cmd.SysProcAttr)
	}
	credential := attr.Credential
	if credential == nil || credential.Uid != 1500 || credential.Gid != 1500 || !slices.Equal(credential.Groups, []uint32{1500, 27}) || credential.NoSetGroups {
		t.Fatalf("Credential : %+v", credential)
	}
	credential.Groups[0] = 9
	if runAsAcct.cred.Groups[0] != 1500 {
		t.Error("Credential partagé avec le compte : une commande pourrait le modifier")
	}
	if cmd.Path != "/bin/sh" || !slices.Equal(cmd.Args, []string{"/bin/sh", "-c", runAsUmask, "/bin/true", "-p", "a b"}) {
		t.Errorf("umask 077 non posé : %q %q", cmd.Path, cmd.Args)
	}
	lookup := envMap(cmd.Env)
	for _, key := range []string{"GLD_AUTH_KEY", "GLD_AUTH_OTHER", "GLD_WEB_TOKEN", "GLD_WEB_PORT", childTokenEnv, "GLD_RUN_AS", "GLD_RUN_AS_EXPERIMENTAL",
		"GLD_AUTH_DIR", "TMUX", "TMUX_TMPDIR", "SSH_AUTH_SOCK", "XDG_RUNTIME_DIR"} {
		if _, ok := lookup[key]; ok {
			t.Errorf("%s transmis au compte cible", key)
		}
	}
	want := map[string]string{"HOME": "/home/alice", "USER": "alice", "LOGNAME": "alice", "SHELL": "/bin/zsh", "CLAUDE_CONFIG_DIR": "/srv/home1",
		"CLAUDE_CODE_X": "1", "ANTHROPIC_API_KEY": "a", "FOO": "bar", "PATH": "/usr/bin"}
	for key, value := range want {
		if lookup[key] != value || lookup[key+"#dup"] != "" {
			t.Errorf("%s = %q (doublon %q), attendu %q", key, lookup[key], lookup[key+"#dup"], value)
		}
	}

	// gld enfant : jeton, port et adresse passent ; sans CLAUDE_CONFIG_DIR, celui de la passerelle est fixé.
	old := root
	root = "/srv/main/.claude/projects"
	t.Cleanup(func() { root = old })
	cmd = exec.Command("/bin/true", "web")
	cmd.Env = []string{childTokenEnv + "=c", "GLD_WEB_PORT=1", "GLD_WEB_LISTEN=127.0.0.1", "GLD_WEB_TOKEN=t"}
	applyRunAs(cmd, childTokenEnv, "GLD_WEB_PORT", "GLD_WEB_LISTEN")
	lookup = envMap(cmd.Env)
	if lookup[childTokenEnv] != "c" || lookup["GLD_WEB_PORT"] != "1" || lookup["GLD_WEB_LISTEN"] != "127.0.0.1" || lookup["GLD_WEB_TOKEN"] != "" || lookup["CLAUDE_CONFIG_DIR"] != "/srv/main/.claude" {
		t.Errorf("env enfant : %v", cmd.Env)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil || cmd.SysProcAttr.Credential.Uid != 1500 {
		t.Errorf("SysProcAttr absent : %+v", cmd.SysProcAttr)
	}

	// commande introuvable : laissée telle quelle, l'erreur de lancement reste celle d'origine.
	cmd = exec.Command("gld-introuvable-xyz")
	applyRunAs(cmd)
	if cmd.Err == nil || cmd.Args[0] != "gld-introuvable-xyz" {
		t.Errorf("commande introuvable enveloppée : %q", cmd.Args)
	}

	// withPath : ~/.local/bin du compte cible, puis la séparation.
	cmd = withPath(exec.Command("/bin/true"))
	if lookup := envMap(cmd.Env); !strings.HasPrefix(lookup["PATH"], "/home/alice/.local/bin:") || lookup["HOME"] != "/home/alice" || cmd.SysProcAttr == nil {
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
		cmd := exec.Command("/bin/true", "new-session", "-t", "$1")
		cmd.Env = []string{"TERM=xterm-256color", "GLD_AUTH_KEY=k", "HOME=/root"}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return cmd
	}
	localCmd, anotherCmd := mk(), mk()
	applyRunAs(localCmd, childTokenEnv)
	if !reflect.DeepEqual(localCmd.Args, anotherCmd.Args) || !reflect.DeepEqual(localCmd.Env, anotherCmd.Env) || !reflect.DeepEqual(localCmd.SysProcAttr, anotherCmd.SysProcAttr) || localCmd.Path != anotherCmd.Path {
		t.Errorf("commande modifiée sans GLD_RUN_AS : %+v", localCmd)
	}
	extraCmd := exec.Command("git", "status")
	applyRunAs(extraCmd)
	if extraCmd.Env != nil || extraCmd.SysProcAttr != nil {
		t.Errorf("Env ou SysProcAttr créés sans GLD_RUN_AS : %v %+v", extraCmd.Env, extraCmd.SysProcAttr)
	}
	if cmd, _ := launcherCmd("tmux", "t", []string{"x"}); slices.Contains(cmd.Args, "-d") {
		t.Errorf("lanceur tmux détaché sans GLD_RUN_AS : %v", cmd.Args)
	}
}

func TestRunAsLauncherIsDetachedTmux(t *testing.T) {
	fakeAccounts(t, 1000)
	runAsAcct = testAccount()
	if launcherAvailable("kitty") || launcherAvailable("terminator-tmux") || tmuxLauncher() != "" {
		t.Error("un émulateur de bureau est proposé sous GLD_RUN_AS")
	}
	cmd, err := launcherCmd("tmux", "a#b", []string{"/usr/bin/gld", "_run", "/tmp/x.json"})
	if err != nil || !slices.Equal(cmd.Args[:9], []string{"tmux", "new-session", "-d", "-x", "160", "-y", "48", "-n", "a##b"}) {
		t.Errorf("lanceur : %v %v", cmd, err)
	}
	runAsAcct = nil
	t.Setenv(runAsChildEnv, "1")
	if !tmuxDetached() {
		t.Error("gld enfant : tmux attaché")
	}
}

func TestRunAsRequiresExperimentalOptIn(t *testing.T) {
	fakeAccounts(t, 1000)
	t.Setenv("GLD_AUTH_DIR", authTempDir(t))
	t.Setenv("GLD_RUN_AS", "alice")
	t.Setenv("GLD_RUN_AS_EXPERIMENTAL", "")
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.experimentalRequired") || runAsAcct != nil {
		t.Fatalf("démarré sans GLD_RUN_AS_EXPERIMENTAL : %v", err)
	}
	t.Setenv("GLD_RUN_AS_EXPERIMENTAL", "1")
	if err := checkRunAsSeparation(); err != nil || runAsAcct == nil {
		t.Fatalf("refusé avec GLD_RUN_AS_EXPERIMENTAL=1 : %v", err)
	}
}

func TestRunAsRefusesNotPrivileged(t *testing.T) {
	bin := fakeAccounts(t, 1000)
	t.Setenv("GLD_AUTH_DIR", authTempDir(t))
	t.Setenv("GLD_RUN_AS", "alice")
	status := func(eff, inh, amb uint64) func() ([]byte, error) {
		return func() ([]byte, error) {
			return []byte("Name:\tgld\nCapInh:\t" + strconv.FormatUint(inh, 16) + "\nCapEff:\t" + strconv.FormatUint(eff, 16) +
				"\nCapAmb:\t" + strconv.FormatUint(amb, 16) + "\n"), nil
		}
	}
	for _, record := range []struct {
		euid          int
		eff, inh, amb uint64
		err           string
	}{
		{1000, 0, 0, 0, L("srv.runas.notPrivileged")},
		{1000, 0xe0, 0, 0, L("srv.runas.notPrivileged")}, // CAP_KILL, CAP_SETGID, CAP_SETUID sans root : refusé
		{0, 0x1ffffffffff, 0, 1 << 7, L("srv.runas.inheritedCaps")},
		{0, 0x1ffffffffff, 1 << 7, 0, L("srv.runas.inheritedCaps")},
		{0, 0x1ffffffffff, 0, 0, ""},
	} {
		gatewayEUID = func() int { return record.euid }
		procStatus = status(record.eff, record.inh, record.amb)
		runAsAcct = nil
		err := checkRunAsSeparation()
		if record.err == "" {
			if err != nil || runAsAcct == nil {
				t.Errorf("%+v : %v", record, err)
			}
		} else if err == nil || err.Error() != record.err || runAsAcct != nil {
			t.Errorf("%+v : %v, attendu %q", record, err, record.err)
		}
	}
	gatewayEUID = func() int { return 0 }
	procStatus = func() ([]byte, error) { return nil, os.ErrNotExist }
	runAsAcct = nil
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.procStatus", "A1", os.ErrNotExist) || runAsAcct != nil {
		t.Errorf("/proc/self/status illisible sous Linux accepté : %v", err)
	}
	runAsGOOS = "darwin" // pas de /proc : vérification sautée
	runAsAcct = nil
	if err := checkRunAsSeparation(); err != nil || runAsAcct == nil {
		t.Errorf("macOS : %v", err)
	}
	runAsGOOS = "linux"
	procStatus = status(0, 0, 0)
	for _, record := range []struct {
		has bool
		err error
	}{{true, nil}, {false, errors.New("EIO")}} {
		binaryHasCaps = func(string) (bool, error) { return record.has, record.err }
		runAsAcct = nil
		if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.fileCaps", "A1", bin) || runAsAcct != nil {
			t.Errorf("capacités de fichier %+v : %v", record, err)
		}
	}
}

func TestRunAsRefusesReadableAuthDir(t *testing.T) {
	bin := fakeAccounts(t, 1000)
	dir := authTempDir(t)
	t.Setenv("GLD_AUTH_DIR", dir)
	t.Setenv("GLD_RUN_AS", "alice")
	var asked string
	canReadAs = func(dir, bin string) (bool, bool, error) {
		asked = dir + " " + bin
		if runAsAcct == nil {
			t.Error("vérification lancée hors du compte cible")
		}
		return true, false, nil
	}
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.authDirReadable", "A1", "alice", "A2", dir) || runAsAcct != nil || asked != dir+" "+bin {
		t.Fatalf("dossier accessible accepté : %v (%q)", err, asked)
	}
	canReadAs = func(string, string) (bool, bool, error) { return false, true, nil }
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.binaryWritable", "A1", "alice", "A2", bin) || runAsAcct != nil {
		t.Fatalf("binaire modifiable accepté : %v", err)
	}
	canReadAs = func(string, string) (bool, bool, error) { return false, false, errors.New("permission denied") }
	if err := checkRunAsSeparation(); err == nil || !strings.Contains(err.Error(), "permission denied") || runAsAcct != nil {
		t.Fatalf("échec de la vérification accepté : %v", err)
	}
	canReadAs = func(string, string) (bool, bool, error) { return false, false, nil }
	if err := checkRunAsSeparation(); err != nil || runAsAcct == nil || runAsAcct.name != "alice" {
		t.Fatalf("séparation correcte refusée : %v", err)
	}

	for auth, want := range map[string]string{
		"/home/alice/.config/gld": L("srv.runas.authDirInHome", "A1", "/home/alice/.config/gld", "A2", "/home/alice"),
		"/home":                   L("srv.runas.homeInAuthDir", "A1", "/home/alice", "A2", "/home"),
	} {
		runAsAcct = nil
		t.Setenv("GLD_AUTH_DIR", auth)
		if err := checkRunAsSeparation(); err == nil || err.Error() != want {
			t.Errorf("%s : %v, attendu %q", auth, err, want)
		}
	}
}

type fakeStat struct {
	uid, gid uint32
	mode     os.FileMode
	err      error
}

// overStat : rootOwned, sauf pour les chemins de m.
func overStat(lookup map[string]fakeStat) {
	lstatOwner = func(path string) (uint32, uint32, os.FileMode, error) {
		if fakeStat, ok := lookup[path]; ok {
			return fakeStat.uid, fakeStat.gid, fakeStat.mode, fakeStat.err
		}
		return rootOwned(path)
	}
}

func TestRunAsRefusesUnsafeChain(t *testing.T) {
	bin := fakeAccounts(t, 1000)
	t.Setenv("GLD_RUN_AS", "alice")
	base, _ := filepath.EvalSymlinks(t.TempDir())
	auth := filepath.Join(base, "auth")
	t.Setenv("GLD_AUTH_DIR", auth)

	// dossier absent : créé en 0700 avant la vérification, puis accepté
	if err := checkRunAsSeparation(); err != nil || runAsAcct == nil {
		t.Fatalf("dossier absent : %v", err)
	}
	if fi, err := os.Lstat(auth); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dossier des comptes non créé en 0700 : %v %v", fi, err)
	}

	fileMode := os.ModeDir
	anc := func(path string) string { return L("srv.runas.ancestorWritable", "A1", "alice", "A2", path) }
	binW := func(path string) string { return L("srv.runas.binaryWritable", "A1", "alice", "A2", path) }
	for _, record := range []struct {
		name string
		m    map[string]fakeStat
		want string
	}{
		{"cible propriétaire", map[string]fakeStat{auth: {1500, 1500, fileMode | 0o700, nil}}, L("srv.runas.authDirCreate", "A1", auth, "A2", (fileMode | 0o700).String())},
		{"dossier des comptes sticky", map[string]fakeStat{auth: {0, 0, fileMode | os.ModeSticky | 0o777, nil}}, L("srv.runas.authDirCreate", "A1", auth, "A2", (fileMode | os.ModeSticky | 0o777).String())},
		{"parent propriété de la cible", map[string]fakeStat{base: {1500, 1500, fileMode | 0o700, nil}}, anc(base)},
		{"groupe supplémentaire", map[string]fakeStat{base: {0, 27, fileMode | 0o775, nil}}, anc(base)},
		{"groupe principal", map[string]fakeStat{base: {0, 1500, fileMode | 0o770, nil}}, anc(base)},
		{"écriture pour tous", map[string]fakeStat{"/": {0, 0, fileMode | 0o757, nil}}, binW("/")}, // le binaire, vérifié d'abord, partage /
		{"parent root sticky", map[string]fakeStat{base: {0, 0, fileMode | os.ModeSticky | 0o777, nil}}, ""},
		{"sticky non root", map[string]fakeStat{base: {1000, 1000, fileMode | os.ModeSticky | 0o777, nil}}, anc(base)},
		{"groupe étranger", map[string]fakeStat{base: {0, 50, fileMode | 0o775, nil}}, ""},
		{"erreur de stat", map[string]fakeStat{base: {0, 0, 0, errors.New("EIO")}}, anc("EIO")},
		{"binaire de la cible", map[string]fakeStat{bin: {1500, 1500, 0o755, nil}}, binW(bin)},
		{"binaire 0757", map[string]fakeStat{bin: {0, 0, 0o757, nil}}, binW(bin)},
		{"dossier du binaire", map[string]fakeStat{filepath.Dir(bin): {0, 27, fileMode | 0o775, nil}}, binW(filepath.Dir(bin))},
		{"binaire sticky (pas un parent)", map[string]fakeStat{bin: {0, 0, os.ModeSticky | 0o777, nil}}, binW(bin)},
	} {
		overStat(record.m)
		runAsAcct = nil
		err := checkRunAsSeparation()
		if record.want == "" {
			if err != nil {
				t.Errorf("%s : %v", record.name, err)
			}
		} else if err == nil || err.Error() != record.want || runAsAcct != nil {
			t.Errorf("%s : %v, attendu %q", record.name, err, record.want)
		}
	}
	lstatOwner = rootOwned

	// ACL POSIX sur un élément, ou illisible : refus
	for _, record := range []struct {
		acl bool
		err error
	}{{true, nil}, {false, errors.New("EIO")}} {
		hasACL = func(path string) (bool, error) {
			if path == base {
				return record.acl, record.err
			}
			return false, nil
		}
		runAsAcct = nil
		if err := checkRunAsSeparation(); err == nil || runAsAcct != nil || (record.err == nil && err.Error() != L("srv.runas.acl", "A1", base)) {
			t.Errorf("ACL %+v : %v", record, err)
		}
	}
	hasACL = func(string) (bool, error) { return false, nil }

	// lien dans le chemin configuré : A/lien -> B/reel ; le chemin résolu ne passe pas par A
	argument, body := filepath.Join(base, "A"), filepath.Join(base, "B", "reel")
	os.MkdirAll(argument, 0o700)
	os.MkdirAll(filepath.Join(body, "auth"), 0o700)
	link := filepath.Join(argument, "lien")
	if err := os.Symlink(body, link); err != nil {
		t.Fatal(err)
	}
	viaLink := filepath.Join(link, "auth")
	t.Setenv("GLD_AUTH_DIR", viaLink)
	for _, record := range []struct {
		name string
		m    map[string]fakeStat
		want string
	}{
		{"lien de root dans un dossier sûr", nil, ""},
		{"lien qui n'est pas à root", map[string]fakeStat{link: {1000, 1000, os.ModeSymlink | 0o777, nil}}, anc(link)},
		{"lien dans un dossier modifiable par la cible", map[string]fakeStat{argument: {0, 0, fileMode | 0o777, nil}}, anc(argument)},
	} {
		overStat(record.m)
		runAsAcct = nil
		err := checkRunAsSeparation()
		if record.want == "" {
			if err != nil {
				t.Errorf("%s : %v", record.name, err)
			}
		} else if err == nil || err.Error() != record.want {
			t.Errorf("%s : %v, attendu %q", record.name, err, record.want)
		}
	}
}

func TestRunAsRefusesBeforeCreating(t *testing.T) {
	fakeAccounts(t, 1000)
	t.Setenv("GLD_RUN_AS", "alice")
	base, _ := filepath.EvalSymlinks(t.TempDir())
	parent := filepath.Join(base, "p")
	os.Mkdir(parent, 0o755)
	t.Setenv("GLD_AUTH_DIR", filepath.Join(parent, "x", "auth"))
	overStat(map[string]fakeStat{parent: {0, 0, os.ModeDir | 0o777, nil}})
	err := checkRunAsSeparation()
	if err == nil || err.Error() != L("srv.runas.ancestorWritable", "A1", "alice", "A2", parent) || runAsAcct != nil {
		t.Fatalf("parent modifiable par la cible accepté : %v", err)
	}
	if ents, _ := os.ReadDir(parent); len(ents) != 0 {
		t.Fatalf("dossier créé malgré le refus : %v", ents)
	}

	// parent root sticky (/tmp) : accepté, le dossier est créé
	overStat(map[string]fakeStat{parent: {0, 0, os.ModeDir | os.ModeSticky | 0o777, nil}})
	if err := checkRunAsSeparation(); err != nil {
		t.Fatalf("parent root sticky refusé : %v", err)
	}
	lstatOwner = rootOwned

	// chemin relatif : refusé, rien n'est créé dans le dossier courant
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("GLD_AUTH_DIR", filepath.Join("rel", "auth"))
	runAsAcct = nil
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.authDirRelative", "A1", "rel/auth") {
		t.Fatalf("chemin relatif accepté : %v", err)
	}
	if ents, _ := os.ReadDir(cwd); len(ents) != 0 {
		t.Fatalf("dossier relatif créé : %v", ents)
	}
}

func TestRunAsFollowsLinkChains(t *testing.T) {
	fakeAccounts(t, 1000)
	t.Setenv("GLD_RUN_AS", "alice")
	base, _ := filepath.EvalSymlinks(t.TempDir())
	argument, body, content := filepath.Join(base, "A"), filepath.Join(base, "B"), filepath.Join(base, "C")
	for _, dir := range []string{argument, body, filepath.Join(content, "auth")} {
		os.MkdirAll(dir, 0o700)
	}
	// A/l1 -> ../B/l2 (relatif) -> C ; le chemin configuré passe par A, le chemin résolu par C : B n'est vu qu'en suivant l1
	os.Symlink(filepath.Join("..", "B", "l2"), filepath.Join(argument, "l1"))
	os.Symlink(content, filepath.Join(body, "l2"))
	t.Setenv("GLD_AUTH_DIR", filepath.Join(argument, "l1", "auth"))
	if err := checkRunAsSeparation(); err != nil {
		t.Fatalf("chaîne de liens sûre refusée : %v", err)
	}
	overStat(map[string]fakeStat{body: {0, 0, os.ModeDir | 0o777, nil}})
	runAsAcct = nil
	if err := checkRunAsSeparation(); err == nil || err.Error() != L("srv.runas.ancestorWritable", "A1", "alice", "A2", body) {
		t.Fatalf("dossier intermédiaire modifiable accepté : %v", err)
	}
	lstatOwner = rootOwned

	// boucle de liens : refus, pas de récursion sans fin
	os.Symlink(filepath.Join(base, "L2"), filepath.Join(base, "L1"))
	os.Symlink(filepath.Join(base, "L1"), filepath.Join(base, "L2"))
	if path, err := unsafeChain(filepath.Join(base, "L1", "auth"), testAccount().cred); err == nil {
		t.Fatalf("boucle de liens acceptée : %q", path)
	}
}

func TestCapFieldFailsClosed(t *testing.T) {
	fakeAccounts(t, 1000)
	t.Setenv("GLD_AUTH_DIR", authTempDir(t))
	t.Setenv("GLD_RUN_AS", "alice")
	for _, st := range []string{
		"CapInh:\t0000000000000000\n",              // CapAmb absent
		"CapAmb:\t0000000000000000\n",              // CapInh absent
		"CapInh:\tzz\nCapAmb:\t0000000000000000\n", // illisible
		"CapInh:\t0000000000000000\nCapAmb:\t\n",   // vide
		"",
	} {
		procStatus = func() ([]byte, error) { return []byte(st), nil }
		runAsAcct = nil
		err := checkRunAsSeparation()
		if err == nil || !strings.HasPrefix(err.Error(), strings.Split(L("srv.runas.procStatus", "A1", "@"), "@")[0]) || runAsAcct != nil {
			t.Errorf("%q : %v", st, err)
		}
	}
	if count, err := capField([]byte("CapAmb:\t00000000000000a0\n"), "CapAmb:"); err != nil || count != 0xa0 {
		t.Errorf("capField : %x %v", count, err)
	}
}

func TestRunCanRead(t *testing.T) {
	saveVar(t, &access)
	dir, bin := "/auth", "/opt/gld/gld"
	allow := map[string]bool{}
	access = func(path string, mode uint32) error {
		if allow[path+" "+strconv.Itoa(int(mode))] {
			return nil
		}
		if path == "/eio" {
			return syscall.EIO
		}
		return syscall.EACCES
	}
	if runCanRead(dir, bin) != 0 {
		t.Error("aucun accès signalé comme accès")
	}
	for key, want := range map[string]int{
		"/auth 2": 3, "/auth 1": 3, "/auth 4": 3, "/auth/auth.db 2": 3, "/auth/auth.key 4": 3, "/auth/auth.db-wal 4": 3, "/auth/auth.db-shm 4": 3, "/auth/auth.db-shm 2": 3, "/auth/auth.db-journal 4": 3, "/auth/auth.db-journal 2": 3,
		"/opt/gld/gld 2": 4, "/opt/gld 2": 4,
	} {
		allow = map[string]bool{key: true}
		if got := runCanRead(dir, bin); got != want {
			t.Errorf("%s : %d, attendu %d", key, got, want)
		}
	}
	allow = map[string]bool{}
	if runCanRead("/eio", bin) != 1 {
		t.Error("erreur d'accès non remontée")
	}

	access = unix.Access
	real := t.TempDir()
	if runCanRead(real, "/usr/bin/true") != 3 {
		t.Error("dossier accessible non signalé")
	}
	if !within("/a/b", "/a/b") || !within("/a/b", "/a/b/c") || within("/a/b", "/a/bc") || within("/a/b", "/a") {
		t.Error("within")
	}
}

func TestSpecPath(t *testing.T) {
	tmp := filepath.Clean(os.TempDir())
	for name, want := range map[string]bool{
		filepath.Join(tmp, "gld-123.json"):                true,
		filepath.Join(tmp, "x", "gld-123.json"):           false,
		filepath.Join(tmp, "gld-123.txt"):                 false,
		tmp + "/./gld-1.json":                             false,
		tmp + "/../" + filepath.Base(tmp) + "/gld-1.json": false,
		"/etc/passwd": false,
		"gld-1.json":  false,
		"":            false,
	} {
		if specPath(name) != want {
			t.Errorf("specPath(%q) != %v", name, want)
		}
	}
}

// Changement de compte réel : seulement dans un espace de noms où l'uid 1000 est projeté, par exemple
// `unshare --user --map-root-user --map-auto` avec GLD_RUNAS_E2E=1.
func TestRunAsRealSwitch(t *testing.T) {
	if os.Getenv("GLD_RUNAS_E2E") != "1" || os.Geteuid() != 0 {
		t.Skip("GLD_RUNAS_E2E=1 sous root (espace de noms utilisateur) requis")
	}
	fakeAccounts(t, 0)
	access = unix.Access
	runAsAcct = &runAsAccount{cred: &syscall.Credential{Uid: 1000, Gid: 1000}, name: "u1000", home: "/nonexistent", shell: "/bin/sh"}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "auth.db"), []byte("x"), 0o600)
	cmd := exec.Command("sh", "-c", "id -u; umask; cat "+filepath.Join(dir, "auth.db")+" 2>&1; ls "+dir+" 2>&1; echo HOME=$HOME")
	cmd.Dir = "/"
	applyRunAs(cmd)
	out, _ := cmd.CombinedOutput()
	text := string(out)
	if !strings.HasPrefix(text, "1000\n0077\n") || strings.Contains(text, "\nx") || !strings.Contains(text, "HOME=/nonexistent") || strings.Count(text, "ermission") < 2 {
		t.Fatalf("sortie sous le compte cible :\n%s", text)
	}
}

func TestRunAsOwns(t *testing.T) {
	old, oldS, oldOS := runAsAcct, procStatusOf, runAsGOOS
	t.Cleanup(func() { runAsAcct, procStatusOf, runAsGOOS = old, oldS, oldOS })
	runAsAcct, runAsGOOS = nil, "linux"
	if runAsOwns(1) != nil {
		t.Fatal("hors GLD_RUN_AS : refus")
	}
	runAsAcct = testAccount()
	uid := "1500"
	procStatusOf = func(int) ([]byte, error) {
		return []byte("Name:\tclaude\nUid:\t" + uid + "\t" + uid + "\t1500\t1500\n"), nil
	}
	if err := runAsOwns(42); err != nil {
		t.Fatalf("processus du compte cible : %v", err)
	}
	uid = "0"
	if runAsOwns(42) == nil {
		t.Fatal("processus root signalé")
	}
	uid = "1500"
	runAsGOOS = "darwin"
	if runAsOwns(42) == nil {
		t.Fatal("hors Linux : signal accepté")
	}
}
