// Métadonnées et santé de la mémoire : front matter (type, paths), budget de contexte, index MEMORY.md,
// imports « @fichier », liens « [[nom]] », aperçu des fichiers qu'une règle scopée cible, recherche plein texte.
package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const indexLines = 200 // Claude Code ne charge que les 200 premières lignes de MEMORY.md

// frontMatter : le bloc YAML en tête du texte et le reste. ok est faux s'il n'y en a pas.
func frontMatter(text string) (yamlText, body string, ok bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text, false
	}
	rest := text[4:]
	i := strings.Index("\n"+rest, "\n---")
	if i < 0 {
		return "", text, false
	}
	yamlText = rest[:max(i-1, 0)]
	after := rest[min(i+3, len(rest)):]
	if j := strings.Index(after, "\n"); j >= 0 {
		after = after[j+1:]
	} else {
		after = ""
	}
	return yamlText, after, true
}

// parseMeta : le front matter décodé. Une erreur YAML est renvoyée telle quelle (affichée comme avertissement).
func parseMeta(text string) (map[string]any, error) {
	y, _, ok := frontMatter(text)
	if !ok {
		if strings.HasPrefix(text, "---\n") {
			return nil, errors.New("front matter non fermé (ligne « --- » manquante)")
		}
		return nil, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(y), &m); err != nil {
		return nil, errors.New("front matter invalide : " + cut(strings.ReplaceAll(err.Error(), "\n", " "), 120))
	}
	return m, nil
}

// globsOf : les motifs du champ `paths:` (liste ou chaîne, séparés par des virgules).
func globsOf(m map[string]any) ([]string, error) {
	var out []string
	switch v := m["paths"].(type) {
	case nil:
		return nil, nil
	case string:
		out = splitGlobs(v)
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, errors.New("`paths:` doit contenir des motifs texte non vides")
			}
			out = append(out, strings.TrimSpace(s))
		}
	default:
		return nil, errors.New("`paths:` doit être une liste de motifs")
	}
	for _, g := range out {
		if !doublestar.ValidatePattern(g) {
			return nil, errors.New("motif invalide : " + g)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("`paths:` est vide : la règle ne se chargerait jamais")
	}
	return out, nil
}

// splitGlobs sépare des motifs par des virgules, hors accolades : « a/*.{ts,tsx}, b/** » donne deux motifs.
func splitGlobs(s string) []string {
	var out []string
	depth, start := 0, 0
	add := func(end int) {
		if g := strings.TrimSpace(s[start:end]); g != "" {
			out = append(out, g)
		}
	}
	for i, c := range s {
		switch c {
		case '{':
			depth++
		case '}':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 {
				add(i)
				start = i + 1
			}
		}
	}
	add(len(s))
	return out
}

// isClaudeFile : un CLAUDE.md (celui que Claude Code charge), par opposition aux autres fichiers de documentation du projet.
func isClaudeFile(key string) bool {
	return strings.HasSuffix(key, "CLAUDE.md") || strings.HasSuffix(key, "CLAUDE.local.md")
}

// annotate complète un MemFile : type, portée, budget de contexte, chargement à chaque session, avertissement.
func annotate(f *MemFile) {
	f.Off = strings.HasSuffix(f.Name, ".off")
	text, err := memRead(f.Key)
	if err != nil {
		return
	}
	n := len(text)
	if strings.HasSuffix(f.Key, "/MEMORY.md") { // seules les 200 premières lignes comptent
		lines := strings.SplitAfter(text, "\n")
		if len(lines) > indexLines {
			n = len(strings.Join(lines[:indexLines], ""))
		}
	}
	f.Tokens = estimateTokens(n)
	if k, ok := knownTokens(text[:n]); ok {
		f.Tokens, f.Exact = k, true
	}
	m, err := parseMeta(text)
	if err != nil {
		f.Warn = err.Error()
	}
	if t, _ := m["type"].(string); t != "" {
		f.Type = t
	} else if md, _ := m["metadata"].(map[string]any); md != nil {
		f.Type, _ = md["type"].(string)
	}
	rule := strings.HasPrefix(f.Key, "g/rules/") || strings.HasPrefix(f.Key, "r/")
	if rule && m != nil {
		if _, has := m["paths"]; has {
			g, err := globsOf(m)
			if err != nil {
				f.Warn = err.Error()
			}
			f.Paths = g
			if g == nil {
				f.Paths = []string{}
			}
		}
	}
	switch {
	case f.Off:
	case f.Key == "g/CLAUDE.md", strings.HasPrefix(f.Key, "p/") && isClaudeFile(f.Key):
		f.Always = true
	case rule:
		f.Always = f.Paths == nil
	case strings.HasSuffix(f.Key, "/MEMORY.md"):
		f.Always = true
	}
}

// --- santé -------------------------------------------------------------------------------------

type Issue struct {
	Key  string `json:"key"`
	Kind string `json:"kind"` // index-ligne, index-absent, index-long, import, lien, front-matter, perime
	Msg  string `json:"msg"`
	Dir  string `json:"dir,omitempty"` // dossier de mémoire concerné, pour la réparation de l'index
}

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)
	wikiLink  = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	importRef = regexp.MustCompile(`(?:^|\s)@([~./\w][^\s]*[./][^\s]*)`)
)

const staleAfter = 180 * 24 * time.Hour

// memHealth : les incohérences de la mémoire, sans rien modifier.
func memHealth() []Issue {
	issues := []Issue{}
	global, projects, _ := memList()
	check := func(f MemFile) {
		if f.Warn != "" {
			issues = append(issues, Issue{Key: f.Key, Kind: "front-matter", Msg: f.Warn})
		}
	}
	imports := func(f MemFile) {
		text, err := memRead(f.Key)
		if err != nil || f.Off {
			return
		}
		base, rel, err := memResolve(f.Key)
		if err != nil {
			return
		}
		dir := filepath.Dir(filepath.Join(base, rel))
		home, _ := os.UserHomeDir()
		inCode := false
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "```") {
				inCode = !inCode
			}
			if inCode || strings.HasPrefix(strings.TrimSpace(l), "`") {
				continue
			}
			for _, m := range importRef.FindAllStringSubmatch(l, -1) {
				p := strings.TrimRight(m[1], ".,;:)")
				switch {
				case strings.HasPrefix(p, "~/"):
					p = filepath.Join(home, p[2:])
				case !filepath.IsAbs(p):
					p = filepath.Join(dir, p)
				}
				if _, err := os.Stat(p); err != nil {
					issues = append(issues, Issue{Key: f.Key, Kind: "import", Msg: "import introuvable : @" + m[1]})
				}
			}
		}
	}
	for _, f := range global {
		check(f)
		if f.Key == "g/CLAUDE.md" {
			imports(f)
		}
	}
	for _, p := range projects {
		names := map[string]bool{}
		var entries []MemFile
		for _, f := range p.Files {
			check(f)
			if strings.HasPrefix(f.Key, "p/") && isClaudeFile(f.Key) { // les @mentions d'un README ne sont pas des imports
				imports(f)
			}
			if strings.HasPrefix(f.Key, "m/") {
				names[f.Name] = true
				if f.Name != "MEMORY.md" {
					entries = append(entries, f)
				}
			}
		}
		if len(names) == 0 {
			continue
		}
		ik := "m/" + p.Dir + "/MEMORY.md"
		idx, err := memRead(ik)
		if err != nil && names["MEMORY.md"] {
			continue
		}
		listed := map[string]bool{}
		if err == nil {
			if n := len(strings.Split(strings.TrimRight(idx, "\n"), "\n")); n > indexLines {
				issues = append(issues, Issue{Key: ik, Kind: "index-long", Dir: p.Dir,
					Msg: "l'index fait " + strconv.Itoa(n) + " lignes : au-delà de " + strconv.Itoa(indexLines) + ", Claude Code ne les charge pas"})
			}
			for _, m := range mdLink.FindAllStringSubmatch(idx, -1) {
				listed[m[1]] = true
				if !names[m[1]] {
					issues = append(issues, Issue{Key: ik, Kind: "index-ligne", Dir: p.Dir, Msg: "l'index pointe vers « " + m[1] + " », qui n'existe pas"})
				}
			}
		}
		slugs := map[string]bool{}
		for _, f := range entries {
			slugs[strings.TrimSuffix(f.Name, ".md")] = true
			if !listed[f.Name] {
				issues = append(issues, Issue{Key: f.Key, Kind: "index-absent", Dir: p.Dir, Msg: "absent de MEMORY.md : Claude ne la verra pas"})
			}
			if time.Since(time.Unix(int64(f.Mtime), 0)) > staleAfter {
				issues = append(issues, Issue{Key: f.Key, Kind: "perime", Msg: "non modifiée depuis plus de six mois"})
			}
		}
		for _, f := range entries {
			text, err := memRead(f.Key)
			if err != nil {
				continue
			}
			for _, m := range wikiLink.FindAllStringSubmatch(text, -1) {
				if !slugs[strings.TrimSuffix(strings.TrimSpace(m[1]), ".md")] {
					issues = append(issues, Issue{Key: f.Key, Kind: "lien", Msg: "lien [[" + m[1] + "]] vers une entrée qui n'existe pas"})
				}
			}
		}
	}
	return issues
}

// memRepairIndex : retire de MEMORY.md les lignes qui pointent vers un fichier absent et y ajoute les entrées qui n'y sont pas.
func memRepairIndex(dir string) (removed, added int, err error) {
	if !safeDir(dir) {
		return 0, 0, errors.New("dossier invalide")
	}
	ik := "m/" + dir + "/MEMORY.md"
	idx, _ := memRead(ik)
	files, _ := filepath.Glob(filepath.Join(root, dir, "memory", "*.md"))
	have := map[string]bool{}
	for _, p := range files {
		have[filepath.Base(p)] = true
	}
	var keep []string
	listed := map[string]bool{}
	for _, l := range strings.SplitAfter(idx, "\n") {
		if m := mdLink.FindStringSubmatch(l); m != nil {
			if !have[m[1]] {
				removed++
				continue
			}
			listed[m[1]] = true
		}
		keep = append(keep, l)
	}
	out := strings.Join(keep, "")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	sort.Strings(files)
	for _, p := range files {
		n := filepath.Base(p)
		if n == "MEMORY.md" || listed[n] || !mdName.MatchString(n) {
			continue
		}
		text, _ := memRead("m/" + dir + "/" + n)
		title := strings.TrimSuffix(n, ".md")
		if m := nameRe.FindStringSubmatch(text); m != nil {
			title = m[1]
		}
		line := "- [" + title + "](" + n + ")"
		if m := descRe.FindStringSubmatch(text); m != nil {
			line += " — " + m[1]
		}
		out += line + "\n"
		added++
	}
	if removed+added == 0 {
		return 0, 0, nil
	}
	return removed, added, memSave(ik, out)
}

// --- portée des règles ----------------------------------------------------------------------------

// memMatches : les fichiers d'un projet ciblés par les motifs `paths:` d'une règle (au plus 50 renvoyés).
func memMatches(key, dir string) (files []string, total int, truncated bool, err error) {
	f, ok := memFileByKey(key)
	if !ok || len(f.Paths) == 0 {
		return nil, 0, false, errors.New("cette règle n'est pas scopée")
	}
	if !safeDir(dir) {
		return nil, 0, false, errors.New("projet invalide")
	}
	cwd := projectCwd(dir)
	if fi, e := os.Stat(cwd); cwd == "" || e != nil || !fi.IsDir() {
		return nil, 0, false, errors.New("dossier du projet introuvable")
	}
	visited := 0
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, ".venv": true, "target": true}
	filepath.WalkDir(cwd, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() && skip[d.Name()] && p != cwd {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if visited++; visited > 50000 {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(cwd, p)
		rel = filepath.ToSlash(rel)
		for _, g := range f.Paths {
			if ok, _ := doublestar.Match(g, rel); ok {
				if total++; total <= 50 {
					files = append(files, rel)
				}
				break
			}
		}
		return nil
	})
	return files, total, truncated, nil
}

func memFileByKey(key string) (MemFile, bool) {
	base, rel, err := memResolve(key)
	if err != nil {
		return MemFile{}, false
	}
	return memFile(key, filepath.Join(base, rel))
}

var pathsBlock = regexp.MustCompile(`(?m)^paths:[^\n]*\n(?:[ \t]+[^\n]*\n|-[ \t][^\n]*\n)*`)

// setPaths remplace (ou ajoute, ou retire si paths est vide) le champ `paths:` du front matter, sans toucher au reste.
func setPaths(text string, paths []string) (string, error) {
	var blk strings.Builder
	if len(paths) > 0 {
		blk.WriteString("paths:\n")
		for _, g := range paths {
			if !doublestar.ValidatePattern(g) || strings.ContainsAny(g, "\n\"") {
				return "", errors.New("motif invalide : " + g)
			}
			blk.WriteString("  - \"" + g + "\"\n")
		}
	}
	y, body, ok := frontMatter(text)
	if !ok {
		if blk.Len() == 0 {
			return text, nil
		}
		return "---\n" + blk.String() + "---\n\n" + text, nil
	}
	y = strings.TrimRight(y, "\n")
	if y != "" {
		y += "\n"
	}
	if pathsBlock.MatchString(y) {
		y = pathsBlock.ReplaceAllString(y, "")
	}
	y += blk.String()
	if strings.TrimSpace(y) == "" {
		return strings.TrimLeft(body, "\n"), nil
	}
	out := "---\n" + y + "---\n" + body
	if _, err := parseMeta(out); err != nil {
		return "", err
	}
	return out, nil
}

// memTypes : les types d'entrée de mémoire prévus par Claude Code.
var memTypes = []string{"user", "feedback", "project", "reference"}

var (
	typeIndented = regexp.MustCompile(`(?m)^([ \t]+type:)[^\n]*$`)
	typeTop      = regexp.MustCompile(`(?m)^type:[^\n]*$`)
	metaLine     = regexp.MustCompile(`(?m)^metadata:[ \t]*$`)
)

// setType fixe metadata.type dans le front matter (créé au besoin), sans toucher aux autres champs ; "" retire le type.
func setType(text, typ string) (string, error) {
	if typ != "" && !slices.Contains(memTypes, typ) {
		return "", errors.New("type inconnu : " + typ)
	}
	y, body, ok := frontMatter(text)
	if !ok {
		if typ == "" {
			return text, nil
		}
		return "---\nmetadata:\n  type: " + typ + "\n---\n\n" + text, nil
	}
	// Un en-tête déjà invalide pour un lecteur YAML strict (description avec « : » non guillemetée, très courant) se modifie quand même :
	// la modification est textuelle, et on ne refuse que de rendre invalide un en-tête qui ne l'était pas.
	_, origErr := parseMeta(text)
	y = strings.TrimRight(y, "\n") + "\n"
	switch {
	case typ == "":
		y = typeIndented.ReplaceAllString(y, "")
		y = typeTop.ReplaceAllString(y, "")
	case typeIndented.MatchString(y):
		done := false
		y = typeIndented.ReplaceAllStringFunc(y, func(l string) string {
			if done {
				return l
			}
			done = true
			return typeIndented.FindStringSubmatch(l)[1] + " " + typ
		})
	case typeTop.MatchString(y):
		y = typeTop.ReplaceAllString(y, "type: "+typ)
	case metaLine.MatchString(y):
		y = metaLine.ReplaceAllString(y, "metadata:\n  type: "+typ)
	default:
		y += "metadata:\n  type: " + typ + "\n"
	}
	y = regexp.MustCompile(`\n{2,}`).ReplaceAllString(y, "\n")
	out := "---\n" + y + "---\n" + body
	if origErr == nil {
		if _, err := parseMeta(out); err != nil {
			return "", err
		}
	}
	return out, nil
}

// --- recherche plein texte ---------------------------------------------------------------------------

// memSearch : les fichiers de mémoire dont le contenu contient q (insensible à la casse), avec la première ligne trouvée.
func memSearch(q string) map[string]string {
	hits := map[string]string{}
	q = strings.ToLower(strings.TrimSpace(q))
	if len(q) < 2 {
		return hits
	}
	global, projects, _ := memList()
	all := append([]MemFile{}, global...)
	for _, p := range projects {
		all = append(all, p.Files...)
	}
	for _, f := range all {
		text, err := memRead(f.Key)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(text, "\n") {
			if strings.Contains(strings.ToLower(l), q) {
				hits[f.Key] = cut(strings.TrimSpace(l), 120)
				break
			}
		}
	}
	return hits
}

// memToggle : désactive une règle globale (x.md devient x.md.off, Claude ne la charge plus) ou la réactive.
func memToggle(key string) (string, error) {
	if !strings.HasPrefix(key, "g/rules/") {
		return "", errors.New("seules les règles globales se désactivent")
	}
	base, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	to := rel + ".off"
	if strings.HasSuffix(rel, ".off") {
		to = strings.TrimSuffix(rel, ".off")
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if _, err := r.Stat(to); err == nil {
		return "", errors.New("« " + to + " » existe déjà")
	}
	if err := r.Rename(rel, to); err != nil {
		return "", err
	}
	return "g/rules/" + to, nil
}
