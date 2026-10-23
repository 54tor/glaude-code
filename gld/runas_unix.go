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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
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
	selfExe       = selfExeOS
	binaryHasCaps = binaryHasCapsOS
	lstatOwner    = lstatOwnerOS
	hasACL        = hasACLOS
	runAsGOOS     = runtime.GOOS
	access        = unix.Access
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

// Variables jamais transmises au compte cible : réglages de la passerelle (GLD_AUTH_*, GLD_WEB_* et GLD_RUN_AS*, par préfixe),
// identité et sockets de son compte (TMUX désignerait le serveur tmux de la passerelle, SSH_AUTH_SOCK son agent). Les autres
// secrets de l'opérateur (DATABASE_URL, AWS_*…) passent : à retirer de l'environnement du service.
var runAsDrop = map[string]bool{
	"GLD_RUN_AS": true, "GLD_RUN_AS_EXPERIMENTAL": true,
	"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "MAIL": true,
	"TMUX": true, "TMUX_PANE": true, "TMUX_TMPDIR": true,
	"XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true, "XDG_STATE_HOME": true,
	"DBUS_SESSION_BUS_ADDRESS": true, "SSH_AUTH_SOCK": true,
}

func runAsDropped(k string) bool {
	return runAsDrop[k] || strings.HasPrefix(k, "GLD_AUTH_") || strings.HasPrefix(k, "GLD_WEB_")
}

// runAsUmask : un exec.Cmd ne fixe pas le umask de l'enfant ; un sh intermédiaire le pose puis s'efface (exec, même pid).
const runAsUmask = `umask 077 && exec "$0" "$@"`

// applyRunAs fait lancer cmd sous le compte cible ; sans GLD_RUN_AS, cmd reste intact. keep : variables à transmettre malgré
// runAsDropped (jeton, port et adresse d'un gld enfant).
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
	if cmd.Err == nil && cmd.Path != "" {
		cmd.Args = append([]string{"/bin/sh", "-c", runAsUmask, cmd.Path}, cmd.Args[1:]...)
		cmd.Path = "/bin/sh"
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env)+5)
	hasConf := false
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if runAsDropped(k) && !slices.Contains(keep, k) {
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

// capField : un champ hexadécimal de /proc/self/status ; absent ou illisible, c'est une erreur (jamais 0 par défaut).
func capField(status []byte, key string) (uint64, error) {
	for _, l := range strings.Split(string(status), "\n") {
		if v, ok := strings.CutPrefix(l, key); ok {
			return strconv.ParseUint(strings.TrimSpace(v), 16, 64)
		}
	}
	return 0, errors.New(key + " absent")
}

// checkRunAsSeparation : au démarrage du serveur, si GLD_RUN_AS est fixé, vérifie la séparation ; une erreur arrête gld
// (jamais de repli silencieux sous le compte de la passerelle).
func checkRunAsSeparation() error {
	a, err := parseRunAs(os.Getenv("GLD_RUN_AS"))
	if a == nil || err != nil {
		return err
	}
	if os.Getenv("GLD_RUN_AS_EXPERIMENTAL") != "1" {
		return terr("srv.runas.experimentalRequired")
	}
	// Root seulement : passer de root à un uid non nul vide toutes les capacités des enfants, ce que ne garantit pas une
	// passerelle non root dotée de capacités (ensemble ambiant conservé, capacités de fichier reprises par chaque gld relancé).
	if gatewayEUID() != 0 {
		return terr("srv.runas.notPrivileged")
	}
	if runAsGOOS == "linux" { // ailleurs (macOS) ni /proc ni capacités
		st, err := procStatus()
		if err != nil {
			return terr("srv.runas.procStatus", "A1", err)
		}
		for _, k := range []string{"CapAmb:", "CapInh:"} {
			n, err := capField(st, k)
			if err != nil {
				return terr("srv.runas.procStatus", "A1", err)
			}
			if n != 0 {
				return terr("srv.runas.inheritedCaps")
			}
		}
	}
	bin, err := selfExe()
	if err != nil {
		return err
	}
	if has, err := binaryHasCaps(bin); err != nil || has {
		return terr("srv.runas.fileCaps", "A1", bin)
	}
	if p, err := unsafeChain(bin, a.cred); err != nil || p != "" {
		return chainErr("srv.runas.binaryWritable", a.name, p, err)
	}
	dir, err := authDir()
	if err != nil {
		return err
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) { // avant toute création : un chemin relatif dépendrait du dossier courant
		return terr("srv.runas.authDirRelative", "A1", dir)
	}
	if within(dir, a.home) {
		return terr("srv.runas.homeInAuthDir", "A1", a.home, "A2", dir)
	}
	if within(a.home, dir) {
		return terr("srv.runas.authDirInHome", "A1", dir, "A2", a.home)
	}
	// Le parent existant est jugé avant que MkdirAll n'écrive dessous.
	existing := dir
	for {
		_, _, _, err := lstatOwner(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || existing == "/" {
			return chainErr("srv.runas.ancestorWritable", a.name, existing, err)
		}
		existing = filepath.Dir(existing)
	}
	if existing != dir {
		if p, err := unsafeChainFrom(existing, a.cred, true, 0); err != nil || p != "" {
			return chainErr("srv.runas.ancestorWritable", a.name, p, err)
		}
	}
	if err := ensureAuthDir(dir); err != nil {
		return terr("srv.runas.authDirCreate", "A1", dir, "A2", err)
	}
	if p, err := unsafeChain(dir, a.cred); err != nil || p != "" {
		return chainErr("srv.runas.ancestorWritable", a.name, p, err)
	}
	runAsAcct = a
	auth, binW, err := canReadAs(dir, bin)
	switch {
	case err != nil:
		err = terr("srv.runas.helperFailed", "A1", a.name, "A2", err)
	case auth:
		err = terr("srv.runas.authDirReadable", "A1", a.name, "A2", dir)
	case binW:
		err = terr("srv.runas.binaryWritable", "A1", a.name, "A2", bin)
	}
	if err != nil {
		runAsAcct = nil
	}
	return err
}

func chainErr(key, name, p string, err error) error {
	if errors.Is(err, errACL) {
		return terr("srv.runas.acl", "A1", p)
	}
	if err != nil {
		p = err.Error()
	}
	return terr(key, "A1", name, "A2", p)
}

// ensureAuthDir : le dossier des comptes existe au moment de la vérification (sinon ses parents seuls seraient jugés) ; absent,
// il est créé en 0700 puis relu : un dossier réel, de root, sans droit pour le groupe ni les autres.
func ensureAuthDir(dir string) error {
	if _, _, _, err := lstatOwner(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return pathless(err)
		}
	}
	uid, _, mode, err := lstatOwner(dir)
	switch {
	case err != nil:
		return pathless(err)
	case !mode.IsDir() || uid != 0 || mode.Perm()&0o077 != 0 || mode&os.ModeSticky != 0:
		return errors.New(mode.String())
	}
	return nil
}

// pathless : l'erreur sans le chemin, que le message cite déjà.
func pathless(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return errors.New(pe.Op + ": " + pe.Err.Error())
	}
	return err
}

// within : p est dir ou un chemin sous dir (liens résolus).
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

func lstatOwnerOS(p string) (uid, gid uint32, mode os.FileMode, err error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return 0, 0, 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, errors.New(p + ": stat")
	}
	return st.Uid, st.Gid, fi.Mode(), nil
}

// targetCanWrite : le compte cible peut modifier cette entrée (fichier) ou en créer, renommer, remplacer dedans (dossier).
// La dérogation root + sticky (/tmp) ne vaut que pour un parent, jamais pour l'élément vérifié lui-même.
func targetCanWrite(uid, gid uint32, mode os.FileMode, c *syscall.Credential, ancestor bool) bool {
	switch {
	case uid == c.Uid:
		return true
	case ancestor && uid == 0 && mode&os.ModeSticky != 0:
		return false
	case mode.Perm()&0o002 != 0:
		return true
	}
	return mode.Perm()&0o020 != 0 && (gid == c.Gid || slices.Contains(c.Groups, gid))
}

var errACL = errors.New("posix acl")

// unsafeChain : le premier élément de p ou de ses parents jusqu'à / que le compte cible pourrait modifier ("" : aucun).
// Deux parcours : le chemin tel que configuré, élément par élément (un lien n'y est admis que s'il est à root, son dossier
// étant lui-même vérifié), puis le chemin résolu. Une ACL POSIX sur un élément fait refuser : les bits de mode ne suffisent plus.
func unsafeChain(p string, c *syscall.Credential) (string, error) {
	return unsafeChainFrom(p, c, false, 0)
}

// linkDepth : borne des liens suivis dans unsafeChain (une boucle de liens ne la fait pas tourner sans fin).
const linkDepth = 16

// unsafeChainFrom : leafAncestor, p est lui-même un parent (dérogation sticky permise) ; depth, liens déjà suivis. Un lien
// de root est accepté si la chaîne de sa cible l'est aussi (cible relative : par rapport au dossier du lien).
func unsafeChainFrom(p string, c *syscall.Credential, leafAncestor bool, depth int) (string, error) {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return p, errors.New(p + ": relative path")
	}
	if depth > linkDepth {
		return p, errors.New(p + ": too many links")
	}
	check := func(q string, ancestor bool) (string, error) {
		uid, gid, mode, err := lstatOwner(q)
		if err != nil {
			return q, err
		}
		if mode&os.ModeSymlink != 0 {
			if uid != 0 {
				return q, nil
			}
			to, err := os.Readlink(q)
			if err != nil {
				return q, err
			}
			if !filepath.IsAbs(to) {
				to = filepath.Join(filepath.Dir(q), to)
			}
			return unsafeChainFrom(to, c, ancestor, depth+1)
		}
		if acl, err := hasACL(q); err != nil || acl {
			if err == nil {
				err = errACL
			}
			return q, err
		}
		if targetCanWrite(uid, gid, mode, c, ancestor) {
			return q, nil
		}
		return "", nil
	}
	walk := func(q string) (string, error) {
		for ancestor := leafAncestor; ; ancestor = true {
			if bad, err := check(q, ancestor); err != nil || bad != "" {
				return bad, err
			}
			if q == "/" {
				return "", nil
			}
			q = filepath.Dir(q)
		}
	}
	if bad, err := walk(p); err != nil || bad != "" {
		return bad, err
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p, err
	}
	return walk(r)
}

// hasACLOS : une ACL POSIX (accès, ou par défaut pour les fichiers à venir) sur p ; Linux seulement.
func hasACLOS(p string) (bool, error) {
	if runAsGOOS != "linux" {
		return false, nil
	}
	for _, a := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		_, err := unix.Getxattr(p, a, nil)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, unix.ENODATA), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP):
			continue
		}
		return false, err
	}
	return false, nil
}

func selfExeOS() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// binaryHasCapsOS : capacités de fichier (Linux seulement) ; leur absence ou un système de fichiers sans xattr ne bloque pas.
func binaryHasCapsOS(p string) (bool, error) {
	if runAsGOOS != "linux" {
		return false, nil
	}
	_, err := unix.Getxattr(p, "security.capability", nil)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.ENODATA), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP):
		return false, nil
	}
	return false, err
}

// canReadAsHelper relance gld sous le compte cible (« gld _canread <dossier> <binaire> ») : code 0 aucun accès, 3 dossier des
// comptes accessible, 4 binaire modifiable ; tout autre résultat est une erreur.
func canReadAsHelper(dir, bin string) (auth, binW bool, err error) {
	cmd := exec.Command(bin, "_canread", dir, bin)
	cmd.Dir = "/"
	applyRunAs(cmd)
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return false, false, nil
	case errors.As(err, &ee) && ee.ExitCode() == 3:
		return true, false, nil
	case errors.As(err, &ee) && ee.ExitCode() == 4:
		return false, true, nil
	}
	return false, false, err
}

// accessAs : vrai si le compte courant (réel) a l'un des droits mode ; absent ou refusé : faux ; autre erreur : remontée.
func accessAs(p string, mode uint32) (bool, error) {
	err := access(p, mode)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.ENOENT), errors.Is(err, unix.EPERM), errors.Is(err, unix.EROFS):
		return false, nil
	}
	return false, err
}

// runCanRead : le corps de « gld _canread », exécuté sous le compte cible ; lecture, écriture et traversée testées.
func runCanRead(dir, bin string) int {
	type chk struct {
		p    string
		mode uint32
	}
	checks := []chk{{dir, unix.R_OK}, {dir, unix.W_OK}, {dir, unix.X_OK}}
	for _, f := range []string{"auth.db", "auth.db-wal", "auth.db-shm", "auth.db-journal", "auth.key"} {
		checks = append(checks, chk{filepath.Join(dir, f), unix.R_OK}, chk{filepath.Join(dir, f), unix.W_OK})
	}
	for _, c := range checks {
		ok, err := accessAs(c.p, c.mode)
		if err != nil {
			return 1
		}
		if ok {
			return 3
		}
	}
	for _, p := range []string{bin, filepath.Dir(bin)} {
		ok, err := accessAs(p, unix.W_OK)
		if err != nil {
			return 1
		}
		if ok {
			return 4
		}
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

// procStatusOf : /proc/<pid>/status (remplaçable dans les tests).
var procStatusOf = func(pid int) ([]byte, error) { return os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status") }

// runAsOwns : sous GLD_RUN_AS, la passerelle (souvent root) ne signale qu'un processus du compte cible ; le pid vient
// d'une commande exécutée sous ce compte, qui pourrait désigner n'importe quel processus. Hors Linux, pas de /proc : refus.
func runAsOwns(pid int) error {
	if runAsAcct == nil {
		return nil
	}
	if runAsGOOS != "linux" {
		return errors.New("signal refused under GLD_RUN_AS outside Linux")
	}
	b, err := procStatusOf(pid)
	if err != nil {
		return err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "Uid:"); ok {
			f := strings.Fields(v)
			if len(f) != 4 {
				break
			}
			for _, x := range f {
				if n, err := strconv.ParseUint(x, 10, 32); err != nil || uint32(n) != runAsAcct.cred.Uid {
					return fmt.Errorf("process %d not owned by the target account", pid)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("process %d: uid unreadable", pid)
}
