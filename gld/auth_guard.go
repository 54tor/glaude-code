package main

// Garde-fou des sessions Claude : règles de refus, hook Bash et bac à sable autour du dossier d'authentification.
// Il vise l'accident ou la dérive de Claude, pas un utilisateur malveillant : le terminal web, un script ou un shell
// ordinaire ne passent pas par les outils de Claude.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
)

// managedPath : emplacement du fichier « managed » de Claude Code, que seul l'administrateur peut installer.
var managedPath = map[string]string{
	"darwin":  "/Library/Application Support/ClaudeCode/managed-settings.json",
	"windows": `C:\Program Files\ClaudeCode\managed-settings.json`,
}[runtime.GOOS]

func init() {
	if managedPath == "" {
		managedPath = "/etc/claude-code/managed-settings.json"
	}
}

// rulePath : un chemin absolu au format des règles Read/Edit (« //chemin », lecteur Windows en « /c/... »).
func rulePath(p string) string {
	if v := filepath.VolumeName(p); len(v) == 2 && v[1] == ':' {
		p = "/" + strings.ToLower(v[:1]) + p[2:]
	}
	return "/" + filepath.ToSlash(p)
}

func guardDeny(authDir string) []string {
	var out []string
	for _, p := range []string{rulePath(authDir) + "/**", rulePath(filepath.Join(authDir, "auth.db")), rulePath(filepath.Join(authDir, "auth.key"))} {
		out = append(out, "Read("+p+")", "Edit("+p+")") // Edit couvre Write : une règle Write(chemin) n'est jamais appliquée
	}
	return out
}

func guardSelf() string {
	self, err := os.Executable()
	if err != nil {
		return "gld"
	}
	return self
}

// guardCommand : la ligne de commande du hook, en une chaîne (la forme exec, command + args, n'existe que dans les
// versions récentes de Claude Code ; une plus ancienne lancerait le binaire seul et laisserait tout passer).
func guardCommand(authDir string) string {
	return hookCommandLine(guardSelf(), authDir, runtime.GOOS == "windows")
}

// hookCommandLine : sous Windows, le hook passe par Git Bash : barres obliques et guillemets doubles, où $, ` et "
// restent à échapper.
func hookCommandLine(self, authDir string, windows bool) string {
	q := shq
	if windows {
		q = func(s string) string {
			s = strings.ReplaceAll(s, `\`, "/")
			return `"` + strings.NewReplacer(`"`, `\"`, "$", `\$`, "`", "\\`").Replace(s) + `"`
		}
	}
	return q(self) + " _guard " + q(authDir)
}

// guardTools : les outils de Claude qui exécutent une commande shell reçue dans tool_input.command.
var guardTools = []string{"Bash", "PowerShell", "Monitor"}

// guardSettings : le contenu du fichier managed-settings.json (ou de --settings) qui protège authDir.
func guardSettings(authDir string) map[string]any {
	return map[string]any{
		"permissions": map[string]any{"deny": guardDeny(authDir)},
		"hooks": map[string]any{"PreToolUse": []any{map[string]any{
			"matcher": strings.Join(guardTools, "|"),
			// Un hook qui échoue (binaire déplacé, délai dépassé) bloque la commande au lieu de la laisser passer.
			"hooks": []any{map[string]any{"type": "command", "command": guardCommand(authDir), "onFailure": "block"}},
		}}},
		// Appliqué par l'OS seulement quand le bac à sable de Claude Code est activé (sandbox.enabled).
		"sandbox": map[string]any{"filesystem": map[string]any{"denyRead": []string{authDir}, "denyWrite": []string{authDir}}},
	}
}

var sqliteRe = regexp.MustCompile(`(^|[^a-z0-9_.-])sqlite3?($|[^a-z0-9_-])`)

// guardNorm retire guillemets et barres obliques inverses : `"auth"."db"` redevient auth.db.
func guardNorm(s string) string {
	return strings.ToLower(strings.NewReplacer(`"`, "", `'`, "", `\`, "", "`", "").Replace(s))
}

// guardHook : refuse une commande Bash qui cite la base, la clé ou le dossier d'authentification, ou lance sqlite.
// ponytail: heuristique sur le texte, sans évaluer le shell ; variables, jokers, encodages ou scripts la contournent.
func guardHook(cmd string, authDir string) (deny bool) {
	// Deux lectures : barres obliques inverses retirées (au\th.db) ou lues comme séparateurs (C:\Users\...).
	cs := []string{guardNorm(cmd), guardNorm(strings.ReplaceAll(cmd, `\`, "/"))}
	if sqliteRe.MatchString(cs[0]) || strings.Contains(cs[0], "gld_auth_dir") {
		return true
	}
	// -wal, -shm et -journal : les fichiers annexes de SQLite contiennent aussi les données.
	paths := []string{"auth.db", "auth.key", "auth.db-wal", "auth.db-shm", "auth.db-journal"}
	if authDir != "" {
		paths = append(paths, authDir)
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if rel, err := filepath.Rel(home, authDir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				rel = filepath.ToSlash(rel)
				paths = append(paths, "~/"+rel, "$home/"+rel, "${home}/"+rel)
				if strings.Contains(rel, "/") { // seul, un nom court comme « gld » refuserait presque tout
					paths = append(paths, rel)
				}
			}
		}
	}
	if v := os.Getenv("GLD_AUTH_DIR"); v != "" {
		paths = append(paths, v)
	}
	for _, n := range paths {
		n = strings.TrimRight(guardNorm(strings.ReplaceAll(n, `\`, "/")), "/")
		for _, c := range cs {
			if n != "" && containsPath(c, n) {
				return true
			}
		}
	}
	return false
}

const nameChars = "abcdefghijklmnopqrstuvwxyz0123456789_-"

// containsPath : n apparaît dans c suivi d'une fin de chemin (/srv/gld vise /srv/gld/x, pas /srv/gld-backups).
// Windows ignore les points finaux (auth.db. est auth.db) : un point ne sépare un autre nom que suivi d'un
// caractère de nom (auth.db.bak). « : » (flux NTFS, auth.db::$DATA) est une fin de chemin.
func containsPath(c, n string) bool {
	for i := 0; ; {
		j := strings.Index(c[i:], n)
		if j < 0 {
			return false
		}
		end := i + j + len(n)
		switch {
		case end == len(c):
			return true
		case c[end] == '.':
			if end+1 == len(c) || !strings.ContainsRune(nameChars, rune(c[end+1])) {
				return true
			}
		case !strings.ContainsRune(nameChars, rune(c[end])):
			return true
		}
		i += j + 1
	}
}

// runGuard : le hook PreToolUse (`gld _guard <dossier>`). Code 2 = refus, raison sur stderr. Une entrée illisible
// est refusée : sinon une commande assez longue pour dépasser la limite passerait sans contrôle.
func runGuard(in io.Reader, errw io.Writer, authDir string) int {
	var h struct {
		Tool  string `json:"tool_name"`
		Input struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if json.NewDecoder(io.LimitReader(in, 8<<20)).Decode(&h) != nil {
		fmt.Fprintln(errw, L("cli.guard.badInput"))
		return 2
	}
	if !slices.Contains(guardTools, h.Tool) {
		return 0
	}
	if authDir == "" {
		authDir, _ = absAuthDir()
	}
	if guardHook(h.Input.Command, authDir) {
		fmt.Fprintln(errw, L("cli.guard.denied"))
		return 2
	}
	return 0
}

func absAuthDir() (string, error) {
	d, err := authDir()
	if err != nil {
		return "", err
	}
	return filepath.Abs(d)
}

// managedInstalled : le fichier managed existe et porte nos règles de refus et le hook (le reste est toléré).
func managedInstalled() bool {
	dir, err := absAuthDir()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(managedPath)
	if err != nil {
		return false
	}
	var m struct {
		Permissions struct{ Deny []string } `json:"permissions"`
		Hooks       struct {
			PreToolUse []struct {
				Matcher string
				Hooks   []struct {
					Command string
					Args    []string // forme exec, générée par une version précédente de gld
				}
			}
		} `json:"hooks"`
	}
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	for _, r := range guardDeny(dir) {
		if !slices.Contains(m.Permissions.Deny, r) {
			return false
		}
	}
	line, self, args := guardCommand(dir), guardSelf(), []string{"_guard", dir}
	for _, g := range m.Hooks.PreToolUse {
		for _, h := range g.Hooks {
			ok := (h.Args == nil && h.Command == line) || (h.Args != nil && h.Command == self && slices.Equal(h.Args, args))
			if ok && slices.Contains(strings.Split(g.Matcher, "|"), "Bash") {
				return true
			}
		}
	}
	return false
}

var guardWarned atomic.Bool

// warnGuard : un seul avertissement, au premier passage en mode multi-utilisateur, si le fichier managed manque.
func warnGuard(on bool, w io.Writer) {
	if !on || guardWarned.Load() || managedInstalled() || !guardWarned.CompareAndSwap(false, true) {
		return
	}
	fmt.Fprintln(w, tr(cliLang(), "cli.main.guardMissing", map[string]any{"A1": managedPath}))
}

// guardArgs : les mêmes réglages passés en --settings aux sessions que gld lance, dès qu'une base de comptes existe.
// Ils ne peuvent pas affaiblir le fichier managed et s'ajoutent aux règles des réglages de l'utilisateur.
// withGuard : argv de claude avec les réglages du garde-fou, placés juste avant --resume ou le prompt ; un --settings
// fourni par l'utilisateur est retiré, sinon il pourrait remplacer celui du garde-fou.
func withGuard(argv []string) []string {
	g := guardArgs()
	if g == nil {
		return argv
	}
	var out []string
	at := -1
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--":
			if at < 0 {
				at = len(out)
			}
			out = append(out, argv[i:]...)
			i = len(argv)
		case a == "--settings":
			i++
		case strings.HasPrefix(a, "--settings="):
		default:
			if a == "--resume" && at < 0 {
				at = len(out)
			}
			out = append(out, a)
		}
	}
	if at < 0 {
		at = len(out)
	}
	return slices.Concat(out[:at], g, out[at:])
}

func guardArgs() []string {
	dir, err := absAuthDir()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.db")); err != nil {
		return nil
	}
	b, _ := json.Marshal(guardSettings(dir))
	return []string{"--settings", string(b)}
}

func cliAuth(_ context.Context, args []string, out io.Writer) error {
	f, err := parseFlags(args, nil, nil, nil)
	if err == errHelp || (err == nil && len(f.pos) == 0) {
		return helpText(out, L("cli.auth.help"))
	}
	if err != nil {
		return err
	}
	if len(f.pos) != 1 || f.pos[0] != "managed-settings" {
		return terr("srv.auth.sousCommandeInconnue", "A1", strings.Join(f.pos, " "))
	}
	dir, err := absAuthDir()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(guardSettings(dir)); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, L("cli.auth.managedTarget", "A1", managedPath))
	fmt.Fprintln(os.Stderr, L("cli.auth.minVersion"))
	return nil
}
