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

func guardCommand(authDir string) string {
	self, err := os.Executable()
	if err != nil {
		self = "gld"
	}
	q := shq
	if runtime.GOOS == "windows" {
		q = func(s string) string { return `"` + s + `"` }
	}
	return q(self) + " _guard " + q(authDir)
}

// guardSettings : le contenu du fichier managed-settings.json (ou de --settings) qui protège authDir.
func guardSettings(authDir string) map[string]any {
	return map[string]any{
		"permissions": map[string]any{"deny": guardDeny(authDir)},
		"hooks": map[string]any{"PreToolUse": []any{map[string]any{
			"matcher": "Bash",
			"hooks":   []any{map[string]any{"type": "command", "command": guardCommand(authDir)}},
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
	c := guardNorm(cmd)
	if sqliteRe.MatchString(c) {
		return true
	}
	needles := []string{"auth.db", "auth.key", "gld_auth_dir"}
	if authDir != "" {
		needles = append(needles, authDir)
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if rel, err := filepath.Rel(home, authDir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				needles = append(needles, "~/"+rel, "$home/"+rel, "${home}/"+rel, filepath.ToSlash(rel))
			}
		}
	}
	if v := os.Getenv("GLD_AUTH_DIR"); v != "" {
		needles = append(needles, v)
	}
	for _, n := range needles {
		if n = guardNorm(n); n != "" && strings.Contains(c, n) {
			return true
		}
	}
	return false
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
	if h.Tool != "Bash" {
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
				Hooks   []struct{ Command string }
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
	for _, g := range m.Hooks.PreToolUse {
		for _, h := range g.Hooks {
			if g.Matcher == "Bash" && strings.Contains(h.Command, " _guard") {
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
	if len(args) != 1 || args[0] != "managed-settings" {
		return helpText(out, L("cli.auth.help"))
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
	return nil
}
