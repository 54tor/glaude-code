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
func rulePath(path string) string {
	if volume := filepath.VolumeName(path); len(volume) == 2 && volume[1] == ':' {
		path = "/" + strings.ToLower(volume[:1]) + path[2:]
	}
	return "/" + filepath.ToSlash(path)
}

func guardDeny(authDir string) []string {
	var rules []string
	for _, rulePattern := range []string{rulePath(authDir) + "/**", rulePath(filepath.Join(authDir, "auth.db")), rulePath(filepath.Join(authDir, "auth.key"))} {
		rules = append(rules, "Read("+rulePattern+")", "Edit("+rulePattern+")") // Edit couvre Write : une règle Write(chemin) n'est jamais appliquée
	}
	return append(rules, "Read(//proc/*/environ)") // GLD_AUTH_KEY y reste lisible quand elle vient de l'environnement de gld
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
	quote := shq
	if windows {
		quote = func(value string) string {
			value = strings.ReplaceAll(value, `\`, "/")
			return `"` + strings.NewReplacer(`"`, `\"`, "$", `\$`, "`", "\\`").Replace(value) + `"`
		}
	}
	return quote(self) + " _guard " + quote(authDir)
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

var procEnvironRe = regexp.MustCompile(`/proc/[^ ;|&]*/environ`)

var sqliteRe = regexp.MustCompile(`(^|[^a-z0-9_.-])sqlite3?($|[^a-z0-9_-])`)

// guardNorm retire guillemets et barres obliques inverses : `"auth"."db"` redevient auth.db.
func guardNorm(text string) string {
	return strings.ToLower(strings.NewReplacer(`"`, "", `'`, "", `\`, "", "`", "").Replace(text))
}

// guardHook : refuse une commande Bash qui cite la base, la clé ou le dossier d'authentification, ou lance sqlite.
// ponytail: heuristique sur le texte, sans évaluer le shell ; variables, jokers, encodages ou scripts la contournent.
func guardHook(cmd string, authDir string) (deny bool) {
	// Deux lectures : barres obliques inverses retirées (au\th.db) ou lues comme séparateurs (C:\Users\...).
	normalizedCommands := []string{guardNorm(cmd), guardNorm(strings.ReplaceAll(cmd, `\`, "/"))}
	if sqliteRe.MatchString(normalizedCommands[0]) || strings.Contains(normalizedCommands[0], "gld_auth_") || procEnvironRe.MatchString(normalizedCommands[1]) {
		return true
	}
	// -wal, -shm et -journal : les fichiers annexes de SQLite contiennent aussi les données.
	paths := []string{"auth.db", "auth.key", "auth.db-wal", "auth.db-shm", "auth.db-journal"}
	if authDir != "" {
		paths = append(paths, authDir)
		if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
			if rel, relErr := filepath.Rel(home, authDir); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				rel = filepath.ToSlash(rel)
				paths = append(paths, "~/"+rel, "$home/"+rel, "${home}/"+rel)
				if strings.Contains(rel, "/") { // seul, un nom court comme « gld » refuserait presque tout
					paths = append(paths, rel)
				}
			}
		}
	}
	if envAuthDir := os.Getenv("GLD_AUTH_DIR"); envAuthDir != "" {
		paths = append(paths, envAuthDir)
	}
	for _, candidate := range paths {
		candidate = strings.TrimRight(guardNorm(strings.ReplaceAll(candidate, `\`, "/")), "/")
		for _, normalizedCommand := range normalizedCommands {
			if candidate != "" && containsPath(normalizedCommand, candidate) {
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
func containsPath(text, path string) bool {
	for searchStart := 0; ; {
		matchOffset := strings.Index(text[searchStart:], path)
		if matchOffset < 0 {
			return false
		}
		end := searchStart + matchOffset + len(path)
		switch {
		case end == len(text):
			return true
		case text[end] == '.':
			if end+1 == len(text) || !strings.ContainsRune(nameChars, rune(text[end+1])) {
				return true
			}
		case !strings.ContainsRune(nameChars, rune(text[end])):
			return true
		}
		searchStart += matchOffset + 1
	}
}

// runGuard : le hook PreToolUse (`gld _guard <dossier>`). Code 2 = refus, raison sur stderr. Une entrée illisible
// est refusée : sinon une commande assez longue pour dépasser la limite passerait sans contrôle.
func runGuard(input io.Reader, errorOutput io.Writer, authDir string) int {
	var payload struct {
		Tool  string `json:"tool_name"`
		Input struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if json.NewDecoder(io.LimitReader(input, 8<<20)).Decode(&payload) != nil {
		fmt.Fprintln(errorOutput, L("cli.guard.badInput"))
		return 2
	}
	if !slices.Contains(guardTools, payload.Tool) {
		return 0
	}
	if authDir == "" {
		authDir, _ = absAuthDir()
	}
	if guardHook(payload.Input.Command, authDir) {
		fmt.Fprintln(errorOutput, L("cli.guard.denied"))
		return 2
	}
	return 0
}

func absAuthDir() (string, error) {
	dir, err := authDir()
	if err != nil {
		return "", err
	}
	return filepath.Abs(dir)
}

// managedInstalled : le fichier managed existe et porte nos règles de refus et le hook bloquant (le reste est toléré).
func managedInstalled() bool {
	dir, err := absAuthDir()
	if err != nil {
		return false
	}
	content, err := os.ReadFile(managedPath)
	if err != nil {
		return false
	}
	var settings struct {
		Permissions struct{ Deny []string } `json:"permissions"`
		Hooks       struct {
			PreToolUse []struct {
				Matcher string
				Hooks   []struct {
					Command   string
					Args      []string // forme exec, générée par une version précédente de gld
					OnFailure string   `json:"onFailure"`
				}
			}
		} `json:"hooks"`
	}
	if json.Unmarshal(content, &settings) != nil {
		return false
	}
	for _, rule := range guardDeny(dir) {
		if !slices.Contains(settings.Permissions.Deny, rule) {
			return false
		}
	}
	line, self, args := guardCommand(dir), guardSelf(), []string{"_guard", dir}
	for _, matcherGroup := range settings.Hooks.PreToolUse {
		for _, hook := range matcherGroup.Hooks {
			matchesGuard := (hook.Args == nil && hook.Command == line) || (hook.Args != nil && hook.Command == self && slices.Equal(hook.Args, args))
			// Sans onFailure « block », un hook en échec laisse passer la commande : ce n'est pas le garde-fou.
			if matchesGuard && hook.OnFailure == "block" && slices.Contains(strings.Split(matcherGroup.Matcher, "|"), "Bash") {
				return true
			}
		}
	}
	return false
}

var guardWarned atomic.Bool

// warnGuard : un seul avertissement, au premier passage en mode multi-utilisateur, si le fichier managed manque.
func warnGuard(enabled bool, output io.Writer) {
	if !enabled || guardWarned.Load() || managedInstalled() || !guardWarned.CompareAndSwap(false, true) {
		return
	}
	fmt.Fprintln(output, tr(cliLang(), "cli.main.guardMissing", map[string]any{"A1": managedPath}))
}

// guardArgs : les mêmes réglages passés en --settings aux sessions que gld lance, dès qu'une base de comptes existe.
// Ils ne peuvent pas affaiblir le fichier managed et s'ajoutent aux règles des réglages de l'utilisateur.
// withGuard : argv de claude avec les réglages du garde-fou, placés juste avant --resume ou le prompt ; un --settings
// fourni par l'utilisateur est retiré, sinon il pourrait remplacer celui du garde-fou.
func withGuard(argv []string) []string {
	guardSettingsArgs := guardArgs()
	if guardSettingsArgs == nil {
		return argv
	}
	var filtered []string
	insertAt := -1
	for argIndex := 0; argIndex < len(argv); argIndex++ {
		arg := argv[argIndex]
		switch {
		case arg == "--":
			if insertAt < 0 {
				insertAt = len(filtered)
			}
			filtered = append(filtered, argv[argIndex:]...)
			argIndex = len(argv)
		case arg == "--settings":
			argIndex++
		case strings.HasPrefix(arg, "--settings="):
		default:
			if arg == "--resume" && insertAt < 0 {
				insertAt = len(filtered)
			}
			filtered = append(filtered, arg)
		}
	}
	if insertAt < 0 {
		insertAt = len(filtered)
	}
	return slices.Concat(filtered[:insertAt], guardSettingsArgs, filtered[insertAt:])
}

func guardArgs() []string {
	dir, err := absAuthDir()
	if err != nil {
		return nil
	}
	if _, statErr := os.Stat(filepath.Join(dir, "auth.db")); statErr != nil {
		return nil
	}
	settingsJSON, _ := json.Marshal(guardSettings(dir))
	return []string{"--settings", string(settingsJSON)}
}

func cliAuth(_ context.Context, args []string, output io.Writer) error {
	parsedFlags, err := parseFlags(args, nil, nil, nil)
	if err == errHelp || (err == nil && len(parsedFlags.pos) == 0) {
		return helpText(output, L("cli.auth.help"))
	}
	if err != nil {
		return err
	}
	if len(parsedFlags.pos) != 1 || parsedFlags.pos[0] != "managed-settings" {
		return terr("srv.auth.sousCommandeInconnue", "A1", strings.Join(parsedFlags.pos, " "))
	}
	dir, err := absAuthDir()
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(guardSettings(dir)); encodeErr != nil {
		return encodeErr
	}
	fmt.Fprintln(os.Stderr, L("cli.auth.managedTarget", "A1", managedPath))
	fmt.Fprintln(os.Stderr, L("cli.auth.minVersion"))
	return nil
}
