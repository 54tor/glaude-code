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
	account, err := parseRunAs(os.Getenv("GLD_RUN_AS"))
	if account == nil || err != nil {
		return nil, err
	}
	return account.cred, nil
}

func parseRunAs(spec string) (*runAsAccount, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	userSpec, groupSpec, hasGroup := strings.Cut(spec, ":")
	var targetUser *user.User
	var err error
	if _, uidParseErr := strconv.ParseUint(userSpec, 10, 32); uidParseErr == nil {
		targetUser, err = lookupUserID(userSpec)
	} else {
		targetUser, err = lookupUser(userSpec)
	}
	if err != nil || targetUser == nil {
		return nil, terr("srv.runas.unknownUser", "A1", userSpec)
	}
	userID, err := strconv.ParseUint(targetUser.Uid, 10, 32)
	if err != nil {
		return nil, terr("srv.runas.unknownUser", "A1", userSpec)
	}
	if !hasGroup {
		groupSpec = targetUser.Gid
	} else if _, groupLookupErr := lookupGroupID(groupSpec); groupLookupErr != nil {
		return nil, terr("srv.runas.unknownGroup", "A1", groupSpec)
	}
	groupID, err := strconv.ParseUint(groupSpec, 10, 32)
	if err != nil {
		return nil, terr("srv.runas.unknownGroup", "A1", groupSpec)
	}
	groupIDTexts, err := userGroupIDs(targetUser)
	if err != nil {
		return nil, terr("srv.runas.unknownGroup", "A1", targetUser.Username)
	}
	var groups []uint32
	for _, groupIDText := range groupIDTexts {
		parsedGroupID, groupParseErr := strconv.ParseUint(groupIDText, 10, 32)
		if groupParseErr != nil {
			return nil, terr("srv.runas.unknownGroup", "A1", groupIDText)
		}
		groups = append(groups, uint32(parsedGroupID))
	}
	switch {
	case userID == 0 || groupID == 0 || slices.Contains(groups, 0):
		return nil, terr("srv.runas.root")
	case int(userID) == gatewayUID():
		return nil, terr("srv.runas.self", "A1", targetUser.Username)
	}
	shell := loginShell(targetUser.Username)
	return &runAsAccount{
		cred: &syscall.Credential{Uid: uint32(userID), Gid: uint32(groupID), Groups: groups},
		name: targetUser.Username, home: targetUser.HomeDir, shell: shell,
	}, nil
}

// loginShell : le shell de /etc/passwd (os/user ne l'expose pas), /bin/sh à défaut (macOS : annuaire, pas de passwd).
func loginShell(name string) string {
	passwd, _ := os.ReadFile("/etc/passwd")
	for _, passwdLine := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(passwdLine, ":")
		if len(fields) == 7 && fields[0] == name && fields[6] != "" {
			return fields[6]
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

func runAsDropped(name string) bool {
	return runAsDrop[name] || strings.HasPrefix(name, "GLD_AUTH_") || strings.HasPrefix(name, "GLD_WEB_")
}

// runAsUmask : un exec.Cmd ne fixe pas le umask de l'enfant ; un sh intermédiaire le pose puis s'efface (exec, même pid).
const runAsUmask = `umask 077 && exec "$0" "$@"`

// applyRunAs fait lancer cmd sous le compte cible ; sans GLD_RUN_AS, cmd reste intact. keep : variables à transmettre malgré
// runAsDropped (jeton, port et adresse d'un gld enfant).
func applyRunAs(cmd *exec.Cmd, keep ...string) {
	account := runAsAcct
	if account == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	credential := *account.cred
	credential.Groups = slices.Clone(account.cred.Groups)
	cmd.SysProcAttr.Credential = &credential
	if cmd.Err == nil && cmd.Path != "" {
		cmd.Args = append([]string{"/bin/sh", "-c", runAsUmask, cmd.Path}, cmd.Args[1:]...)
		cmd.Path = "/bin/sh"
	}
	environment := cmd.Env
	if environment == nil {
		environment = os.Environ()
	}
	filtered := make([]string, 0, len(environment)+5)
	hasConfigDir := false
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if runAsDropped(name) && !slices.Contains(keep, name) {
			continue
		}
		hasConfigDir = hasConfigDir || name == "CLAUDE_CONFIG_DIR"
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, "HOME="+account.home, "USER="+account.name, "LOGNAME="+account.name, "SHELL="+account.shell)
	if !hasConfigDir && root != "" {
		// sinon claude prendrait le ~/.claude du compte cible alors que la passerelle lit le sien
		filtered = append(filtered, "CLAUDE_CONFIG_DIR="+filepath.Dir(root))
	}
	cmd.Env = filtered
}

// capField : un champ hexadécimal de /proc/self/status ; absent ou illisible, c'est une erreur (jamais 0 par défaut).
func capField(status []byte, key string) (uint64, error) {
	for _, statusLine := range strings.Split(string(status), "\n") {
		if value, found := strings.CutPrefix(statusLine, key); found {
			return strconv.ParseUint(strings.TrimSpace(value), 16, 64)
		}
	}
	return 0, errors.New(key + " absent")
}

// checkRunAsSeparation : au démarrage du serveur, si GLD_RUN_AS est fixé, vérifie la séparation ; une erreur arrête gld
// (jamais de repli silencieux sous le compte de la passerelle).
func checkRunAsSeparation() error {
	account, err := parseRunAs(os.Getenv("GLD_RUN_AS"))
	if account == nil || err != nil {
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
		status, statusErr := procStatus()
		if statusErr != nil {
			return terr("srv.runas.procStatus", "A1", statusErr)
		}
		for _, field := range []string{"CapAmb:", "CapInh:"} {
			value, fieldErr := capField(status, field)
			if fieldErr != nil {
				return terr("srv.runas.procStatus", "A1", fieldErr)
			}
			if value != 0 {
				return terr("srv.runas.inheritedCaps")
			}
		}
	}
	binaryPath, err := selfExe()
	if err != nil {
		return err
	}
	if hasCaps, capsErr := binaryHasCaps(binaryPath); capsErr != nil || hasCaps {
		return terr("srv.runas.fileCaps", "A1", binaryPath)
	}
	if unsafePath, chainCheckErr := unsafeChain(binaryPath, account.cred); chainCheckErr != nil || unsafePath != "" {
		return chainErr("srv.runas.binaryWritable", account.name, unsafePath, chainCheckErr)
	}
	dir, err := authDir()
	if err != nil {
		return err
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) { // avant toute création : un chemin relatif dépendrait du dossier courant
		return terr("srv.runas.authDirRelative", "A1", dir)
	}
	if within(dir, account.home) {
		return terr("srv.runas.homeInAuthDir", "A1", account.home, "A2", dir)
	}
	if within(account.home, dir) {
		return terr("srv.runas.authDirInHome", "A1", dir, "A2", account.home)
	}
	// Le parent existant est jugé avant que MkdirAll n'écrive dessous.
	existing := dir
	for {
		_, _, _, lstatErr := lstatOwner(existing)
		if lstatErr == nil {
			break
		}
		if !errors.Is(lstatErr, os.ErrNotExist) || existing == "/" {
			return chainErr("srv.runas.ancestorWritable", account.name, existing, lstatErr)
		}
		existing = filepath.Dir(existing)
	}
	if existing != dir {
		if unsafePath, chainCheckErr := unsafeChainFrom(existing, account.cred, true, 0); chainCheckErr != nil || unsafePath != "" {
			return chainErr("srv.runas.ancestorWritable", account.name, unsafePath, chainCheckErr)
		}
	}
	if ensureErr := ensureAuthDir(dir); ensureErr != nil {
		return terr("srv.runas.authDirCreate", "A1", dir, "A2", ensureErr)
	}
	if unsafePath, chainCheckErr := unsafeChain(dir, account.cred); chainCheckErr != nil || unsafePath != "" {
		return chainErr("srv.runas.ancestorWritable", account.name, unsafePath, chainCheckErr)
	}
	runAsAcct = account
	authReadable, binaryWritable, err := canReadAs(dir, binaryPath)
	switch {
	case err != nil:
		err = terr("srv.runas.helperFailed", "A1", account.name, "A2", err)
	case authReadable:
		err = terr("srv.runas.authDirReadable", "A1", account.name, "A2", dir)
	case binaryWritable:
		err = terr("srv.runas.binaryWritable", "A1", account.name, "A2", binaryPath)
	}
	if err != nil {
		runAsAcct = nil
	}
	return err
}

func chainErr(key, name, path string, err error) error {
	if errors.Is(err, errACL) {
		return terr("srv.runas.acl", "A1", path)
	}
	if err != nil {
		path = err.Error()
	}
	return terr(key, "A1", name, "A2", path)
}

// ensureAuthDir : le dossier des comptes existe au moment de la vérification (sinon ses parents seuls seraient jugés) ; absent,
// il est créé en 0700 puis relu : un dossier réel, de root, sans droit pour le groupe ni les autres.
func ensureAuthDir(dir string) error {
	if _, _, _, lstatErr := lstatOwner(dir); errors.Is(lstatErr, os.ErrNotExist) {
		if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
			return pathless(mkdirErr)
		}
	}
	ownerUID, _, mode, err := lstatOwner(dir)
	switch {
	case err != nil:
		return pathless(err)
	case !mode.IsDir() || ownerUID != 0 || mode.Perm()&0o077 != 0 || mode&os.ModeSticky != 0:
		return errors.New(mode.String())
	}
	return nil
}

// pathless : l'erreur sans le chemin, que le message cite déjà.
func pathless(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return errors.New(pathErr.Op + ": " + pathErr.Err.Error())
	}
	return err
}

// within : path est dir ou un chemin sous dir (liens résolus).
func within(dir, path string) bool {
	if resolvedDir, dirEvalErr := filepath.EvalSymlinks(dir); dirEvalErr == nil {
		dir = resolvedDir
	}
	if resolvedPath, pathEvalErr := filepath.EvalSymlinks(path); pathEvalErr == nil {
		path = resolvedPath
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func lstatOwnerOS(path string) (ownerUID, ownerGID uint32, mode os.FileMode, err error) {
	fileInfo, err := os.Lstat(path) // #nosec G703 -- chemin géré par gld par conception, confiné par rootfs.go sous GLD_RUN_AS
	if err != nil {
		return 0, 0, 0, err
	}
	stat, isUnixStat := fileInfo.Sys().(*syscall.Stat_t)
	if !isUnixStat {
		return 0, 0, 0, errors.New(path + ": stat")
	}
	return stat.Uid, stat.Gid, fileInfo.Mode(), nil
}

// targetCanWrite : le compte cible peut modifier cette entrée (fichier) ou en créer, renommer, remplacer dedans (dossier).
// La dérogation root + sticky (/tmp) ne vaut que pour un parent, jamais pour l'élément vérifié lui-même.
func targetCanWrite(ownerUID, ownerGID uint32, mode os.FileMode, credential *syscall.Credential, ancestor bool) bool {
	switch {
	case ownerUID == credential.Uid:
		return true
	case ancestor && ownerUID == 0 && mode&os.ModeSticky != 0:
		return false
	case mode.Perm()&0o002 != 0:
		return true
	}
	return mode.Perm()&0o020 != 0 && (ownerGID == credential.Gid || slices.Contains(credential.Groups, ownerGID))
}

var errACL = errors.New("posix acl")

// unsafeChain : le premier élément de path ou de ses parents jusqu'à / que le compte cible pourrait modifier ("" : aucun).
// Deux parcours : le chemin tel que configuré, élément par élément (un lien n'y est admis que s'il est à root, son dossier
// étant lui-même vérifié), puis le chemin résolu. Une ACL POSIX sur un élément fait refuser : les bits de mode ne suffisent plus.
func unsafeChain(path string, credential *syscall.Credential) (string, error) {
	return unsafeChainFrom(path, credential, false, 0)
}

// linkDepth : borne des liens suivis dans unsafeChain (une boucle de liens ne la fait pas tourner sans fin).
const linkDepth = 16

// unsafeChainFrom : leafAncestor, path est lui-même un parent (dérogation sticky permise) ; depth, liens déjà suivis. Un lien
// de root est accepté si la chaîne de sa cible l'est aussi (cible relative : par rapport au dossier du lien).
func unsafeChainFrom(path string, credential *syscall.Credential, leafAncestor bool, depth int) (string, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return path, errors.New(path + ": relative path")
	}
	if depth > linkDepth {
		return path, errors.New(path + ": too many links")
	}
	checkEntry := func(entryPath string, ancestor bool) (string, error) {
		ownerUID, ownerGID, mode, lstatErr := lstatOwner(entryPath)
		if lstatErr != nil {
			return entryPath, lstatErr
		}
		if mode&os.ModeSymlink != 0 {
			if ownerUID != 0 {
				return entryPath, nil
			}
			linkTarget, readlinkErr := os.Readlink(entryPath)
			if readlinkErr != nil {
				return entryPath, readlinkErr
			}
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(entryPath), linkTarget)
			}
			return unsafeChainFrom(linkTarget, credential, ancestor, depth+1)
		}
		if hasEntryACL, aclErr := hasACL(entryPath); aclErr != nil || hasEntryACL {
			if aclErr == nil {
				aclErr = errACL
			}
			return entryPath, aclErr
		}
		if targetCanWrite(ownerUID, ownerGID, mode, credential, ancestor) {
			return entryPath, nil
		}
		return "", nil
	}
	walkUp := func(current string) (string, error) {
		for ancestor := leafAncestor; ; ancestor = true {
			if unsafeEntry, checkErr := checkEntry(current, ancestor); checkErr != nil || unsafeEntry != "" {
				return unsafeEntry, checkErr
			}
			if current == "/" {
				return "", nil
			}
			current = filepath.Dir(current)
		}
	}
	if unsafeEntry, walkErr := walkUp(path); walkErr != nil || unsafeEntry != "" {
		return unsafeEntry, walkErr
	}
	resolved, evalErr := filepath.EvalSymlinks(path)
	if evalErr != nil {
		return path, evalErr
	}
	return walkUp(resolved)
}

// hasACLOS : une ACL POSIX (accès, ou par défaut pour les fichiers à venir) sur p ; Linux seulement.
func hasACLOS(path string) (bool, error) {
	if runAsGOOS != "linux" {
		return false, nil
	}
	for _, attribute := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		_, err := unix.Getxattr(path, attribute, nil)
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
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(executable)
}

// binaryHasCapsOS : capacités de fichier (Linux seulement) ; leur absence ou un système de fichiers sans xattr ne bloque pas.
func binaryHasCapsOS(path string) (bool, error) {
	if runAsGOOS != "linux" {
		return false, nil
	}
	_, err := unix.Getxattr(path, "security.capability", nil)
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
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return false, false, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 3:
		return true, false, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 4:
		return false, true, nil
	}
	return false, false, err
}

// accessAs : vrai si le compte courant (réel) a l'un des droits mode ; absent ou refusé : faux ; autre erreur : remontée.
func accessAs(path string, mode uint32) (bool, error) {
	err := access(path, mode)
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
	type accessCheck struct {
		p    string
		mode uint32
	}
	checks := []accessCheck{{dir, unix.R_OK}, {dir, unix.W_OK}, {dir, unix.X_OK}}
	for _, fileName := range []string{"auth.db", "auth.db-wal", "auth.db-shm", "auth.db-journal", "auth.key"} {
		checks = append(checks, accessCheck{filepath.Join(dir, fileName), unix.R_OK}, accessCheck{filepath.Join(dir, fileName), unix.W_OK})
	}
	for _, check := range checks {
		hasAccess, accessErr := accessAs(check.p, check.mode)
		if accessErr != nil {
			return 1
		}
		if hasAccess {
			return 3
		}
	}
	for _, path := range []string{bin, filepath.Dir(bin)} {
		hasAccess, accessErr := accessAs(path, unix.W_OK)
		if accessErr != nil {
			return 1
		}
		if hasAccess {
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
	output, err := cmd.Output()
	name := strings.TrimSpace(string(output))
	if err == nil && name == "" {
		err = errors.New("gld _spec")
	}
	return name, err
}

// runSpec : le corps de « gld _spec » : copie stdin dans un fichier temporaire et en imprime le chemin.
func runSpec() int {
	specFile, err := os.CreateTemp("", "gld-*.json")
	if err != nil {
		return 1
	}
	_, err = io.Copy(specFile, io.LimitReader(os.Stdin, 8<<20))
	if closeErr := specFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(specFile.Name())
		return 1
	}
	fmt.Println(specFile.Name())
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
	status, err := procStatusOf(pid)
	if err != nil {
		return err
	}
	for _, statusLine := range strings.Split(string(status), "\n") {
		if value, found := strings.CutPrefix(statusLine, "Uid:"); found {
			fields := strings.Fields(value)
			if len(fields) != 4 {
				break
			}
			for _, field := range fields {
				if parsedUID, parseErr := strconv.ParseUint(field, 10, 32); parseErr != nil || uint32(parsedUID) != runAsAcct.cred.Uid {
					return fmt.Errorf("process %d not owned by the target account", pid)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("process %d: uid unreadable", pid)
}
