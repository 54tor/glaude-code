//go:build !windows

package main

// Séparation par compte OS (GLD_RUN_AS) : la passerelle garde son identité (propriétaire de auth.db et auth.key) et lance
// sous le compte cible tout processus qu'un utilisateur peut influencer : claude, tmux, git, les gld enfants des homes.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

type runAsAccount struct {
	cred              *syscall.Credential
	name, home, shell string
}

// runAsAcct : le compte cible, nil hors GLD_RUN_AS (et toujours nil dans les processus lancés sous ce compte).
var runAsAcct *runAsAccount

// Remplaçables dans les tests.
var (
	lookupUser    = user.Lookup
	lookupUserID  = user.LookupId
	lookupGroupID = user.LookupGroupId
	userGroupIDs  = (*user.User).GroupIds
	gatewayUID    = os.Getuid
	gatewayEUID   = os.Geteuid
	procStatus    = func() ([]byte, error) { return os.ReadFile("/proc/self/status") }
	canReadAs     = canReadAsHelper
)

// runAs lit GLD_RUN_AS (nom, uid ou uid:gid) ; nil sans erreur si la variable est vide.
func runAs() (*syscall.Credential, error) {
	a, err := parseRunAs(os.Getenv("GLD_RUN_AS"))
	if a == nil || err != nil {
		return nil, err
	}
	return a.cred, nil
}

func parseRunAs(spec string) (*runAsAccount, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	who, gidS, hasGid := strings.Cut(spec, ":")
	var u *user.User
	var err error
	if _, e := strconv.ParseUint(who, 10, 32); e == nil {
		u, err = lookupUserID(who)
	} else {
		u, err = lookupUser(who)
	}
	if err != nil || u == nil {
		return nil, terr("srv.runas.unknownUser", "A1", who)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, terr("srv.runas.unknownUser", "A1", who)
	}
	if !hasGid {
		gidS = u.Gid
	} else if _, err := lookupGroupID(gidS); err != nil {
		return nil, terr("srv.runas.unknownGroup", "A1", gidS)
	}
	gid, err := strconv.ParseUint(gidS, 10, 32)
	if err != nil {
		return nil, terr("srv.runas.unknownGroup", "A1", gidS)
	}
	ids, err := userGroupIDs(u)
	if err != nil {
		return nil, terr("srv.runas.unknownGroup", "A1", u.Username)
	}
	var groups []uint32
	for _, s := range ids {
		g, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return nil, terr("srv.runas.unknownGroup", "A1", s)
		}
		groups = append(groups, uint32(g))
	}
	switch {
	case uid == 0 || gid == 0 || slices.Contains(groups, 0):
		return nil, terr("srv.runas.root")
	case int(uid) == gatewayUID():
		return nil, terr("srv.runas.self", "A1", u.Username)
	}
	shell := loginShell(u.Username)
	return &runAsAccount{
		cred: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups},
		name: u.Username, home: u.HomeDir, shell: shell,
	}, nil
}

// loginShell : le shell de /etc/passwd (os/user ne l'expose pas), /bin/sh à défaut (macOS : annuaire, pas de passwd).
func loginShell(name string) string {
	b, _ := os.ReadFile("/etc/passwd")
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Split(l, ":")
		if len(f) == 7 && f[0] == name && f[6] != "" {
			return f[6]
		}
	}
	return "/bin/sh"
}

func runAsOn() bool { return runAsAcct != nil }

func runAsHome() string {
	if runAsAcct == nil {
		return ""
	}
	return runAsAcct.home
}

// tmuxDetached : les sessions vont dans le serveur tmux du compte cible, sans client attaché (aucun bureau sous ce compte) ;
// vrai aussi dans un gld enfant lancé sous ce compte.
func tmuxDetached() bool { return runAsAcct != nil || os.Getenv(runAsChildEnv) == "1" }

const runAsChildEnv = "GLD_RUN_AS_CHILD"

// Variables jamais transmises au compte cible : secrets et réglages de la passerelle, identité et sockets de son compte
// (TMUX désignerait le serveur tmux de la passerelle, SSH_AUTH_SOCK son agent).
var runAsDrop = map[string]bool{
	"GLD_AUTH_KEY": true, "GLD_AUTH_DIR": true, "GLD_WEB_TOKEN": true, childTokenEnv: true, "GLD_RUN_AS": true,
	"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "MAIL": true,
	"TMUX": true, "TMUX_PANE": true, "TMUX_TMPDIR": true,
	"XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true, "XDG_STATE_HOME": true,
	"DBUS_SESSION_BUS_ADDRESS": true, "SSH_AUTH_SOCK": true,
}

// applyRunAs fait lancer cmd sous le compte cible ; sans GLD_RUN_AS, cmd reste intact. keep : variables à transmettre malgré
// runAsDrop (le jeton d'un gld enfant).
func applyRunAs(cmd *exec.Cmd, keep ...string) {
	a := runAsAcct
	if a == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	c := *a.cred
	c.Groups = slices.Clone(a.cred.Groups)
	cmd.SysProcAttr.Credential = &c
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env)+5)
	hasConf := false
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if runAsDrop[k] && !slices.Contains(keep, k) {
			continue
		}
		hasConf = hasConf || k == "CLAUDE_CONFIG_DIR"
		out = append(out, e)
	}
	out = append(out, "HOME="+a.home, "USER="+a.name, "LOGNAME="+a.name, "SHELL="+a.shell)
	if !hasConf && root != "" {
		// sinon claude prendrait le ~/.claude du compte cible alors que la passerelle lit le sien
		out = append(out, "CLAUDE_CONFIG_DIR="+filepath.Dir(root))
	}
	cmd.Env = out
}

// Capacités Linux : CAP_KILL (5), CAP_SETGID (6), CAP_SETUID (7).
const runAsCaps = 1<<5 | 1<<6 | 1<<7

func capField(status []byte, key string) uint64 {
	for _, l := range strings.Split(string(status), "\n") {
		if v, ok := strings.CutPrefix(l, key); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
			return n
		}
	}
	return 0
}

// checkRunAsSeparation : au démarrage du serveur, si GLD_RUN_AS est fixé, vérifie que la séparation est réelle ; une erreur
// arrête gld (jamais de repli silencieux sous le compte de la passerelle).
func checkRunAsSeparation() error {
	a, err := parseRunAs(os.Getenv("GLD_RUN_AS"))
	if a == nil || err != nil {
		return err
	}
	if gatewayEUID() != 0 {
		st, _ := procStatus()
		if capField(st, "CapEff:")&runAsCaps != runAsCaps {
			return terr("srv.runas.notPrivileged")
		}
		// Les capacités ambiantes passeraient aux sessions (un changement d'uid sans root ne les efface pas).
		if capField(st, "CapAmb:") != 0 {
			return terr("srv.runas.ambientCaps")
		}
	}
	dir, err := authDir()
	if err != nil {
		return err
	}
	if within(dir, a.home) {
		return terr("srv.runas.homeInAuthDir", "A1", a.home, "A2", dir)
	}
	runAsAcct = a
	readable, err := canReadAs(dir)
	if err == nil && readable {
		err = terr("srv.runas.authDirReadable", "A1", a.name, "A2", dir)
	} else if err != nil {
		err = terr("srv.runas.helperFailed", "A1", a.name, "A2", err)
	}
	if err != nil {
		runAsAcct = nil
		return err
	}
	return nil
}

// within : p est dir ou un chemin sous dir.
func within(dir, p string) bool {
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	if q, err := filepath.EvalSymlinks(p); err == nil {
		p = q
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// canReadAsHelper relance gld sous le compte cible (« gld _canread <dossier> ») : code 0 illisible, 3 lisible.
func canReadAsHelper(dir string) (bool, error) {
	self, err := os.Executable()
	if err != nil {
		return false, err
	}
	cmd := exec.Command(self, "_canread", dir)
	cmd.Dir = "/"
	applyRunAs(cmd)
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return false, nil
	case errors.As(err, &ee) && ee.ExitCode() == 3:
		return true, nil
	}
	return false, err
}

// runCanRead : le corps de « gld _canread », exécuté sous le compte cible.
func runCanRead(dir string) int {
	readable := false
	if _, err := os.ReadDir(dir); err == nil {
		readable = true
	}
	for _, f := range []string{"auth.db", "auth.db-wal", "auth.key"} {
		if fh, err := os.Open(filepath.Join(dir, f)); err == nil {
			fh.Close()
			readable = true
		}
	}
	if readable {
		return 3
	}
	return 0
}

// writeSpecAsTarget : le fichier de l'onglet est créé par le compte cible (« gld _spec »), seul à devoir le lire puis l'effacer.
func writeSpecAsTarget(self string, spec []byte) (string, error) {
	cmd := exec.Command(self, "_spec")
	cmd.Dir = "/"
	cmd.Stdin = strings.NewReader(string(spec))
	applyRunAs(cmd)
	out, err := cmd.Output()
	name := strings.TrimSpace(string(out))
	if err == nil && name == "" {
		err = errors.New("gld _spec")
	}
	return name, err
}

// runSpec : le corps de « gld _spec » : copie stdin dans un fichier temporaire et en imprime le chemin.
func runSpec() int {
	f, err := os.CreateTemp("", "gld-*.json")
	if err != nil {
		return 1
	}
	_, err = io.Copy(f, io.LimitReader(os.Stdin, 8<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return 1
	}
	fmt.Println(f.Name())
	return 0
}
