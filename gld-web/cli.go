package main

// Commandes en ligne de commande (anciens scripts bin/gld-*), dans le même binaire que le serveur :
//
//	gld-web matin …     ou le binaire appelé gld-matin (lien)
//
// Sans bash, jq ni coreutils : elles tournent telles quelles sous Windows.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type cliFunc func(ctx context.Context, args []string, out io.Writer) error

var cliCommands = map[string]cliFunc{"matin": cliMatin}

// dispatchCLI exécute une commande si argv la désigne. ok=false : c'est le serveur qu'on lance.
func dispatchCLI(argv []string) (code int, ok bool) {
	base := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	name, args := strings.TrimPrefix(base, "gld-"), argv[1:]
	if base == name || cliCommands[name] == nil { // pas appelé gld-<commande> : sous-commande explicite
		if len(args) == 0 {
			return 0, false
		}
		name, args = args[0], args[1:]
		if name == "_run" && len(args) == 1 {
			return runTab(args[0]), true
		}
	}
	fn := cliCommands[name]
	if fn == nil {
		return 0, false
	}
	err := fn(context.Background(), args, os.Stdout)
	switch {
	case err == nil, errors.Is(err, errHelp):
		return 0, true
	}
	fmt.Fprintf(os.Stderr, "gld-%s : %v\n", name, err)
	return 1, true
}

var (
	errHelp = errors.New("aide")
)

// flags : analyse minimale à la façon des scripts : options booléennes, options à valeur, le reste en positionnels.
type flags struct {
	bools map[string]bool
	vals  map[string]string
	pos   []string
}

// parseFlags : bool liste les options sans valeur, val celles qui en prennent une. Un alias se déclare avec « -n=--dry-run ».
func parseFlags(args []string, bool_, val []string, alias map[string]string) (*flags, error) {
	f := &flags{bools: map[string]bool{}, vals: map[string]string{}}
	has := func(l []string, s string) bool {
		for _, x := range l {
			if x == s {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if r, ok := alias[a]; ok {
			a = r
		}
		switch {
		case has(bool_, a):
			f.bools[a] = true
		case has(val, a):
			if i+1 < len(args) {
				i++
				f.vals[a] = args[i]
			} else {
				f.vals[a] = ""
			}
		case a == "-h" || a == "--help":
			return nil, errHelp
		case strings.HasPrefix(a, "-") && a != "-":
			return nil, fmt.Errorf("option inconnue : %s", args[i])
		default:
			f.pos = append(f.pos, a)
		}
	}
	return f, nil
}

func (f *flags) int(name string, def int) int {
	if v, ok := f.vals[name]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt(def int, keys ...string) int {
	for _, k := range keys {
		if n, err := strconv.Atoi(os.Getenv(k)); err == nil {
			return n
		}
	}
	return def
}

func envStr(def string, keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return def
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
	abs, err := filepath.Abs(target)
	if err != nil {
		return project{}, err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return project{}, fmt.Errorf("projet introuvable : %s", target)
	}
	p := project{Path: abs, Transcripts: filepath.Join(root, nonAlnum.ReplaceAllString(abs, "-"))}
	if ls, _ := filepath.Glob(filepath.Join(p.Transcripts, "*.jsonl")); len(ls) == 0 {
		if b, err := os.ReadFile(filepath.Join(p.Transcripts, ".session-aliases")); err == nil {
			canon, _, _ := strings.Cut(string(b), "\n")
			if fi, err := os.Stat(strings.TrimSpace(canon)); err == nil && fi.IsDir() {
				p.Transcripts, p.Alias = strings.TrimSpace(canon), strings.TrimSpace(canon)
			}
		}
	}
	if fi, err := os.Stat(p.Transcripts); err != nil || !fi.IsDir() {
		return p, fmt.Errorf("aucune session Claude pour %s\n          (cherché dans %s)", abs, p.Transcripts)
	}
	return p, nil
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
	var out []transcriptFile
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() || (st.Size() == 0 && !withEmpty) {
			continue
		}
		out = append(out, transcriptFile{Path: p, Info: st, S: info(p, st)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if oldestFirst {
			return out[i].Info.ModTime().Before(out[j].Info.ModTime())
		}
		return out[i].Info.ModTime().After(out[j].Info.ModTime())
	})
	return out
}

// label : le titre d'une session et sa marque (« ~ » quand il est déduit du premier prompt), "" si elle n'en a aucun.
func (t transcriptFile) label() (title, mark string) {
	if t.S.Named {
		return t.S.Title, " "
	}
	if t.S.Title == "(sans titre)" {
		return "", "~"
	}
	return t.S.Title, "~"
}

func younger(fi os.FileInfo, days int) bool {
	return time.Since(fi.ModTime()) < time.Duration(days)*24*time.Hour
}

// trunc : coupe à n caractères (runes), sans rien ajouter.
func trunc(s string, n int) string { return cut(s, n) }

// pad : complète à droite jusqu'à n caractères, comme printf %-ns.
func pad(s string, n int) string {
	if k := len([]rune(s)); k < n {
		return s + strings.Repeat(" ", n-k)
	}
	return s
}

func mdate(fi os.FileInfo) string { return fi.ModTime().Format("01-02_15:04") }

func short(id string) string { return trunc(id, 8) }

func helpText(out io.Writer, text string) error {
	fmt.Fprintln(out, strings.TrimSpace(text))
	return errHelp
}
