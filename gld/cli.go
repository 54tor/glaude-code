package main

// Commandes en ligne de commande (anciens scripts bin/gld-*), dans le même binaire que le serveur :
//
//	gld matin …     ou le binaire appelé gld-matin (lien)
//
// Sans bash, jq ni coreutils : elles tournent telles quelles sous Windows.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type cliFunc func(ctx context.Context, args []string, out io.Writer) error

const (
	dayDuration   = 24 * time.Hour
	shortIDLength = 8
)

var cliCommands = map[string]cliFunc{"matin": cliMatin, "user": cliUser, "auth": cliAuth}

// dispatchCLI exécute une commande si argv la désigne. handled=false : c'est le serveur qu'on lance.
func dispatchCLI(argv []string) (exitCode int, handled bool) {
	binaryName := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	name, args := strings.TrimPrefix(binaryName, "gld-"), argv[1:]
	isExplicitSubcommand := binaryName == name || cliCommands[name] == nil // pas appelé gld-<commande>
	if isExplicitSubcommand {
		if len(args) == 0 {
			return 0, false
		}
		name, args = args[0], args[1:]
		if name == "_run" && len(args) == 1 {
			cliMode = true
			return runTab(args[0]), true
		}
		if name == "_canread" && len(args) == 2 { // vérification de GLD_RUN_AS, sous le compte cible (runas_unix.go)
			cliMode = true
			return runCanRead(args[0], args[1]), true
		}
		if name == "_spec" && len(args) == 0 { // fichier d'un onglet, créé sous le compte cible
			cliMode = true
			return runSpec(), true
		}
		if name == "_loaded" { // hook InstructionsLoaded de Claude Code (loaded.go)
			cliMode = true
			return runLoaded(os.Stdin), true
		}
		if name == "_guard" && len(args) <= 1 { // hook PreToolUse de Claude Code (auth_guard.go)
			cliMode = true
			return runGuard(os.Stdin, os.Stderr, strings.Join(args, "")), true
		}
		if strings.HasPrefix(name, "_") && cliCommands[name] == nil { // sous-commande interne inconnue : jamais de serveur lancé par un hook
			return 2, true
		}
	}
	command := cliCommands[name]
	if command == nil {
		return 0, false
	}
	cliMode = true
	err := command(context.Background(), args, os.Stdout)
	switch {
	case err == nil, errors.Is(err, errHelp):
		return 0, true
	}
	fmt.Fprintf(os.Stderr, "gld-%s : %v\n", name, err)
	return 1, true
}

var (
	errHelp = terr("srv.cli.aide")
)

// flags : analyse minimale à la façon des scripts : options booléennes, options à valeur, le reste en positionnels.
type flags struct {
	bools map[string]bool
	vals  map[string]string
	pos   []string
}

// parseFlags : booleanFlags liste les options sans valeur, valueFlags celles qui en prennent une. Un alias se déclare avec « -n=--dry-run ».
func parseFlags(args []string, booleanFlags, valueFlags []string, alias map[string]string) (*flags, error) {
	parsed := &flags{bools: map[string]bool{}, vals: map[string]string{}}
	for argIndex := 0; argIndex < len(args); argIndex++ {
		arg := args[argIndex]
		if canonical, isAlias := alias[arg]; isAlias {
			arg = canonical
		}
		switch {
		case slices.Contains(booleanFlags, arg):
			parsed.bools[arg] = true
		case slices.Contains(valueFlags, arg):
			if argIndex+1 < len(args) {
				argIndex++
				parsed.vals[arg] = args[argIndex]
			} else {
				parsed.vals[arg] = ""
			}
		case arg == "-h" || arg == "--help":
			return nil, errHelp
		case strings.HasPrefix(arg, "-") && arg != "-":
			return nil, terr("srv.cli.optionInconnue", "A1", args[argIndex])
		default:
			parsed.pos = append(parsed.pos, arg)
		}
	}
	return parsed, nil
}

func (parsedFlags *flags) int(name string, fallback int) int {
	value, found := parsedFlags.vals[name]
	if !found {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return number
}

func envInt(fallback int, keys ...string) int {
	for _, key := range keys {
		if number, err := strconv.Atoi(os.Getenv(key)); err == nil {
			return number
		}
	}
	return fallback
}

func envStr(fallback string, keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return fallback
}

// project : un dossier de projet et le dossier de ses transcripts.
type project struct{ Path, Transcripts, Alias string }

// resolveProject : le projet cible (ou le dossier courant) et ses transcripts. Encodage de Claude : tout caractère non
// alphanumérique devient « - ». Un dossier de travail additionnel n'a pas de transcript à lui : Claude y dépose un
// .session-aliases qui pointe le dossier canonique, qu'on suit.
func resolveProject(target string) (project, error) {
	if target == "" {
		target, _ = os.Getwd()
	}
	absolutePath, err := filepath.Abs(target)
	if err != nil {
		return project{}, err
	}
	if dirInfo, statErr := os.Stat(absolutePath); statErr != nil || !dirInfo.IsDir() {
		return project{}, terr("srv.cli.projetIntrouvable", "A1", target)
	}
	resolved := project{Path: absolutePath, Transcripts: filepath.Join(root, nonAlnum.ReplaceAllString(absolutePath, "-"))}
	if transcripts, _ := filepath.Glob(filepath.Join(resolved.Transcripts, "*.jsonl")); len(transcripts) == 0 {
		if aliasContent, readErr := os.ReadFile(filepath.Join(resolved.Transcripts, ".session-aliases")); readErr == nil {
			canonicalLine, _, _ := strings.Cut(string(aliasContent), "\n")
			canonicalDir := strings.TrimSpace(canonicalLine)
			if canonicalInfo, statErr := os.Stat(canonicalDir); statErr == nil && canonicalInfo.IsDir() {
				resolved.Transcripts, resolved.Alias = canonicalDir, canonicalDir
			}
		}
	}
	if dirInfo, statErr := os.Stat(resolved.Transcripts); statErr != nil || !dirInfo.IsDir() {
		return resolved, terr("srv.cli.aucuneSessionClaudePour", "A1", absolutePath, "A2", resolved.Transcripts)
	}
	return resolved, nil
}

// transcriptFile : un transcript d'un dossier, avec ce qu'on en sait.
type transcriptFile struct {
	Path string
	Info os.FileInfo
	S    Session
}

// listTranscripts : les *.jsonl (non vides, sauf withEmpty) d'un dossier de transcripts, du plus récent au plus ancien (ou l'inverse).
func listTranscripts(dir string, oldestFirst, withEmpty bool) []transcriptFile {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var transcripts []transcriptFile
	for _, path := range paths {
		fileInfo, err := os.Stat(path)
		if err != nil {
			continue
		}
		isSkippedEmpty := fileInfo.Size() == 0 && !withEmpty
		if !fileInfo.Mode().IsRegular() || isSkippedEmpty {
			continue
		}
		transcripts = append(transcripts, transcriptFile{Path: path, Info: fileInfo, S: info(path, fileInfo)})
	}
	sort.SliceStable(transcripts, func(left, right int) bool {
		if oldestFirst {
			return transcripts[left].Info.ModTime().Before(transcripts[right].Info.ModTime())
		}
		return transcripts[left].Info.ModTime().After(transcripts[right].Info.ModTime())
	})
	return transcripts
}

// label : le titre d'une session et sa marque (« ~ » quand il est déduit du premier prompt), "" si elle n'en a aucun.
func (file transcriptFile) label() (title, mark string) {
	if file.S.Named {
		return file.S.Title, " "
	}
	if file.S.Title == "(sans titre)" {
		return "", "~"
	}
	return file.S.Title, "~"
}

func younger(fileInfo os.FileInfo, days int) bool {
	return time.Since(fileInfo.ModTime()) < time.Duration(days)*dayDuration
}

// pad : complète à droite jusqu'à width caractères, comme printf %-ns.
func pad(text string, width int) string {
	if length := len([]rune(text)); length < width {
		return text + strings.Repeat(" ", width-length)
	}
	return text
}

func mdate(fileInfo os.FileInfo) string { return fileInfo.ModTime().Format("01-02_15:04") }

func short(id string) string { return cut(id, shortIDLength) }

func helpText(output io.Writer, text string) error {
	fmt.Fprintln(output, strings.TrimSpace(text))
	return errHelp
}
