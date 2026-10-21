// Mémoire de Claude : globale (~/.claude/CLAUDE.md, rules/), par projet (projects/<dossier>/memory/)
// et fichiers CLAUDE.md du projet. Seuls ces fichiers-là sont lisibles, modifiables ou supprimables.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type MemFile struct {
	Key   string  `json:"key"` // g/…, m/<dossier>/…, p/<dossier>/…
	Name  string  `json:"name"`
	KB    int64   `json:"kb"`
	Mtime float64 `json:"mtime"`
	Path  string  `json:"path"` // chemin complet, pour le copier

	Type   string   `json:"type,omitempty"`   // metadata.type du front matter (entrées de mémoire)
	Paths  []string `json:"paths,omitempty"`  // règle scopée : globs du front matter `paths:`
	Tokens int      `json:"tokens"`           // estimation (calibrée sur les comptes réels), ou compte réel si Exact
	Exact  bool     `json:"exact,omitempty"`  // Tokens est un compte réel (voir tokens.go)
	Always bool     `json:"always,omitempty"` // chargé à chaque session
	Off    bool     `json:"off,omitempty"`    // règle désactivée (fichier .md.off)
	Warn   string   `json:"warn,omitempty"`   // front matter invalide
}

type MemProject struct {
	Dir     string    `json:"dir"`
	Project string    `json:"project"`
	Missing bool      `json:"missing"` // dossier du projet introuvable : Project est alors deviné depuis le nom
	Files   []MemFile `json:"files"`
}

const memMax = 1 << 20

var (
	mdName   = regexp.MustCompile(`^[^./\\][^/\\]*\.md$`)
	ruleName = regexp.MustCompile(`^[^./\\][^/\\]*\.md(\.off)?$`) // une règle désactivée garde son nom + « .off »
	// Fichiers du dossier de travail d'un projet affichés dans la zone Mémoire. Seuls les CLAUDE.md sont chargés par Claude Code ;
	// les autres (AGENTS.md, CONVENTIONS.md, PLAN.md, TOOLING.md, README.md) sont de la documentation du projet, qu'on lit et édite ici.
	projectFiles = []string{"CLAUDE.md", "CLAUDE.local.md", ".claude/CLAUDE.md", "AGENTS.md", "CONVENTIONS.md", "PLAN.md", "TOOLING.md", "README.md"}
)

// globalClaude : ce chemin est le CLAUDE.md global (~/.claude/CLAUDE.md), même atteint comme fichier d'un projet
// (le projet « ~ » a pour .claude/CLAUDE.md… le fichier global). Il n'est alors géré que dans la section Globale.
func globalClaude(path string) bool {
	g := filepath.Join(cfgDir(), "CLAUDE.md")
	if filepath.Clean(path) == filepath.Clean(g) {
		return true
	}
	a, err1 := os.Stat(path)
	b, err2 := os.Stat(g)
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}

func safeDir(d string) bool { return d != "" && d != ".." && !strings.ContainsAny(d, `/\`) }

// projectCwd : le dossier de travail d'un dossier de transcripts (sa session la plus récente, sinon son nom décodé).
func projectCwd(dir string) string {
	files, _ := filepath.Glob(filepath.Join(root, dir, "*.jsonl"))
	var best os.FileInfo
	var bestP string
	for _, p := range files {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 && (best == nil || st.ModTime().After(best.ModTime())) {
			best, bestP = st, p
		}
	}
	if best != nil {
		if c := info(bestP, best).Cwd; c != "" {
			return c
		}
	}
	return decodeDir(dir)
}

// memResolve traduit une clé en (dossier de base, chemin relatif), après validation stricte.
func memResolve(key string) (string, string, error) {
	bad := errors.New("fichier de mémoire invalide")
	switch {
	case key == "g/CLAUDE.md":
		return cfgDir(), "CLAUDE.md", nil
	case strings.HasPrefix(key, "g/rules/"):
		if n := key[8:]; ruleName.MatchString(n) {
			return filepath.Join(cfgDir(), "rules"), n, nil
		}
	case strings.HasPrefix(key, "m/"):
		if d, n, ok := strings.Cut(key[2:], "/"); ok && safeDir(d) && mdName.MatchString(n) {
			return filepath.Join(root, d, "memory"), n, nil
		}
	case strings.HasPrefix(key, "r/"): // règle d'un projet : <dossier du projet>/.claude/rules/<nom>
		if d, n, ok := strings.Cut(key[2:], "/"); ok && safeDir(d) && ruleName.MatchString(n) {
			base, err := projectRulesDir(projectCwd(d))
			return base, n, err
		}
	case strings.HasPrefix(key, "p/"):
		if d, f, ok := strings.Cut(key[2:], "/"); ok && safeDir(d) {
			for _, a := range projectFiles {
				if f == a {
					c := projectCwd(d)
					if fi, err := os.Stat(c); c == "" || err != nil || !fi.IsDir() {
						return "", "", errors.New("dossier du projet introuvable")
					}
					if globalClaude(filepath.Join(c, filepath.FromSlash(f))) {
						return "", "", errors.New("c'est le CLAUDE.md global : il se gère dans la section Globale")
					}
					return c, filepath.FromSlash(f), nil
				}
			}
		}
	}
	return "", "", bad
}

// projectRulesDir : le dossier .claude/rules d'un projet ; refuse un projet absent, un lien symbolique et le dossier des règles
// globales (un projet lancé depuis ~).
func projectRulesDir(cwd string) (string, error) {
	if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
		return "", errors.New("dossier du projet introuvable")
	}
	rules := filepath.Join(cwd, ".claude", "rules")
	if rules == filepath.Join(cfgDir(), "rules") {
		return "", errors.New("ce sont les règles globales : elles se gèrent dans la section Globale")
	}
	for _, p := range []string{filepath.Join(cwd, ".claude"), rules} {
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New(".claude est un lien symbolique : refusé")
		}
	}
	return rules, nil
}

func memFile(key, path string) (MemFile, bool) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return MemFile{}, false
	}
	f := MemFile{Key: key, Name: filepath.Base(path), KB: fi.Size() / 1024, Mtime: float64(fi.ModTime().UnixNano()) / 1e9, Path: path}
	annotate(&f)
	return f, true
}

// MemTarget : un projet vers la mémoire duquel une entrée peut être déplacée.
type MemTarget struct {
	Dir     string `json:"dir"`
	Project string `json:"project"`
}

// liveProjects : les projets dont le dossier de travail existe encore, avec ou sans fichier de mémoire. Les réglages, skills et
// .mcp.json d'un projet vivent dans son dossier, pas dans ~/.claude.
func liveProjects() []MemTarget {
	_, _, targets := memList()
	out := []MemTarget{}
	for _, t := range targets {
		if fi, err := os.Stat(t.Project); t.Project != "" && err == nil && fi.IsDir() {
			out = append(out, t)
		}
	}
	return out
}

func memList() ([]MemFile, []MemProject, []MemTarget) {
	global := []MemFile{}
	if f, ok := memFile("g/CLAUDE.md", filepath.Join(cfgDir(), "CLAUDE.md")); ok {
		global = append(global, f)
	}
	rules, _ := filepath.Glob(filepath.Join(cfgDir(), "rules", "*.md*"))
	for _, p := range rules {
		if f, ok := memFile("g/rules/"+filepath.Base(p), p); ok && ruleName.MatchString(f.Name) {
			f.Name = "rules/" + f.Name
			global = append(global, f)
		}
	}
	projects := []MemProject{}
	targets := []MemTarget{}
	dirs, _ := os.ReadDir(root)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		mp := MemProject{Dir: d.Name(), Files: []MemFile{}}
		mems, _ := filepath.Glob(filepath.Join(root, d.Name(), "memory", "*.md"))
		for _, p := range mems {
			if f, ok := memFile("m/"+d.Name()+"/"+filepath.Base(p), p); ok && mdName.MatchString(f.Name) {
				mp.Files = append(mp.Files, f)
			}
		}
		mp.Project = projectCwd(d.Name())
		if fi, err := os.Stat(mp.Project); mp.Project == "" || err != nil || !fi.IsDir() {
			mp.Missing = true
			if mp.Project == "" { // nom encodé (« / » devenus « - ») : approximation lisible
				mp.Project = strings.ReplaceAll(d.Name(), "-", "/")
			}
		} else {
			for _, a := range projectFiles {
				if path := filepath.Join(mp.Project, filepath.FromSlash(a)); globalClaude(path) {
					continue // déjà listé dans la section Globale
				} else if f, ok := memFile("p/"+d.Name()+"/"+a, path); ok {
					f.Name = a
					mp.Files = append(mp.Files, f)
				}
			}
			if rdir, err := projectRulesDir(mp.Project); err == nil {
				rules, _ := filepath.Glob(filepath.Join(rdir, "*.md*"))
				for _, p := range rules {
					if f, ok := memFile("r/"+d.Name()+"/"+filepath.Base(p), p); ok && ruleName.MatchString(f.Name) {
						f.Name = "rules/" + f.Name
						mp.Files = append(mp.Files, f)
					}
				}
			}
		}
		if len(mp.Files) > 0 {
			projects = append(projects, mp)
		}
		targets = append(targets, MemTarget{d.Name(), mp.Project})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Project < projects[j].Project })
	sort.Slice(targets, func(i, j int) bool { return targets[i].Project < targets[j].Project })
	return global, projects, targets
}

func memRead(key string) (string, error) {
	base, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer r.Close()
	b, err := r.ReadFile(rel)
	if err == nil && (len(b) > memMax || !utf8.Valid(b)) {
		err = errors.New("fichier trop gros ou pas du texte")
	}
	return string(b), err
}

// memSave remplace le fichier (écriture atomique, droits conservés) ou le crée ; la version remplacée va à la corbeille.
func memSave(key, text string) error { return memWrite(key, text, true) }

// memWrite : keep met d'abord la version actuelle à la corbeille (pas pour les mises à jour d'index, dérivées).
func memWrite(key, text string, keep bool) error {
	if len(text) > memMax || !utf8.ValidString(text) {
		return errors.New("texte trop long ou invalide")
	}
	base, rel, err := memResolve(key)
	if err != nil {
		return err
	}
	if keep {
		if old, err := memRead(key); err == nil && old != text {
			if err := toTrash(key, "remplacé"); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer r.Close()
	mode := os.FileMode(0o644)
	if fi, err := r.Stat(rel); err == nil {
		mode = fi.Mode().Perm()
	}
	if d := filepath.Dir(rel); d != "." {
		if err := r.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	tmp := rel + ".gld-web.tmp"
	if err := r.WriteFile(tmp, []byte(text), mode); err != nil {
		return err
	}
	if err := r.Rename(tmp, rel); err != nil {
		r.Remove(tmp)
		return err
	}
	return nil
}

// memRemove met le fichier à la corbeille puis le supprime ; pour une entrée de mémoire de projet,
// sa ligne d'index dans MEMORY.md part aussi.
func memRemove(key string) error { return memDelete(key, true) }

func memDelete(key string, trash bool) error {
	base, rel, err := memResolve(key)
	if err != nil {
		return err
	}
	if trash {
		if err := toTrash(key, "supprimé"); err != nil {
			return err
		}
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.Remove(rel); err != nil {
		return err
	}
	if !strings.HasPrefix(key, "m/") || rel == "MEMORY.md" {
		return nil
	}
	idx, err := r.ReadFile("MEMORY.md")
	if err != nil {
		return nil // pas d'index : rien à nettoyer
	}
	var keep []string
	for _, l := range strings.SplitAfter(string(idx), "\n") {
		if !strings.Contains(l, "]("+rel+")") {
			keep = append(keep, l)
		}
	}
	if out := strings.Join(keep, ""); out != string(idx) {
		return memWrite(strings.TrimSuffix(key, rel)+"MEMORY.md", out, false)
	}
	return nil
}

// mergePrompt : la demande de fusion faite à Claude, et le dossier où la lancer (le projet concerné, sinon ~).
func mergePrompt(keys []string) (string, string, error) {
	if len(keys) < 2 || len(keys) > 20 {
		return "", "", errors.New("choisis de 2 à 20 fichiers")
	}
	home, _ := os.UserHomeDir()
	cwd := ""
	var b strings.Builder
	b.WriteString("Fusionne ces fichiers de mémoire de Claude Code en un seul, sans perdre d'information utile :\n")
	for _, k := range keys {
		base, rel, err := memResolve(k)
		if err != nil {
			return "", "", err
		}
		p := filepath.Join(base, rel)
		if _, err := os.Stat(p); err != nil {
			return "", "", errors.New("introuvable : " + p)
		}
		b.WriteString("- " + p + "\n")
		if d, _, ok := strings.Cut(k[2:], "/"); cwd == "" && (k[0] == 'm' || k[0] == 'p') && ok {
			if c := projectCwd(d); c != "" {
				if fi, err := os.Stat(c); err == nil && fi.IsDir() {
					cwd = c
				}
			}
		}
	}
	b.WriteString("\nRègles :\n" +
		"- garde le format des fichiers (en-tête YAML name / description / metadata.type quand il y en a un) ;\n" +
		"- supprime les doublons ; signale-moi les informations contradictoires ou périmées avant de trancher ;\n" +
		"- écris le résultat dans le premier fichier, puis supprime les autres ;\n" +
		"- mets à jour MEMORY.md (une ligne par entrée) s'il référence ces fichiers ;\n" +
		"- montre-moi le résultat et attends mon accord avant d'écrire ou de supprimer quoi que ce soit.\n")
	if cwd == "" {
		cwd = home
	}
	return b.String(), cwd, nil
}

// memMove déplace une entrée de mémoire de projet ou une règle globale vers la mémoire d'un autre projet
// (to = son dossier de transcripts) ou vers les règles globales (to = "global"). Sans écraser ; l'index suit.
func memMove(key, to string) (string, error) { return memTransfer(key, to, false) }

// memTransfer : déplace une entrée ou une règle vers un projet (ou « global »), ou la copie si keep.
func memTransfer(key, to string, keep bool) (string, error) {
	if !strings.HasPrefix(key, "m/") && !strings.HasPrefix(key, "g/rules/") && !strings.HasPrefix(key, "r/") {
		return "", errors.New("seules les entrées de mémoire et les règles se déplacent")
	}
	_, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	dst := "g/rules/" + rel
	switch {
	case to == "global":
	case strings.HasPrefix(to, "r:"): // règles d'un projet (to = « r:<dossier> »)
		if !safeDir(to[2:]) {
			return "", errors.New("destination invalide")
		}
		dst = "r/" + to[2:] + "/" + rel
		if _, _, err := memResolve(dst); err != nil {
			return "", err
		}
	case strings.HasPrefix(key, "r/"):
		return "", errors.New("une règle de projet va vers les règles globales ou celles d'un autre projet")
	default:
		if !safeDir(to) {
			return "", errors.New("destination invalide")
		}
		if fi, err := os.Stat(filepath.Join(root, to)); err != nil || !fi.IsDir() {
			return "", errors.New("projet de destination introuvable")
		}
		dst = "m/" + to + "/" + rel
	}
	if dst == key {
		return "", errors.New("déjà à cet endroit")
	}
	if _, err := memRead(dst); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("un fichier du même nom existe déjà à destination")
	}
	text, err := memRead(key)
	if err != nil {
		return "", err
	}
	line := "- [" + strings.TrimSuffix(rel, ".md") + "](" + rel + ")\n"
	if strings.HasPrefix(key, "m/") { // la ligne d'index d'origine, avec son accroche, part avec l'entrée
		if idx, err := memRead(key[:strings.LastIndex(key, "/")+1] + "MEMORY.md"); err == nil {
			for _, l := range strings.SplitAfter(idx, "\n") {
				if strings.Contains(l, "]("+rel+")") {
					line = strings.TrimRight(l, "\n") + "\n"
					break
				}
			}
		}
	}
	if err := memSave(dst, text); err != nil {
		return "", err
	}
	if !keep {
		if err := memDelete(key, false); err != nil { // déplacé, pas supprimé : rien à mettre à la corbeille
			return "", err
		}
	}
	if strings.HasPrefix(dst, "m/") && rel != "MEMORY.md" {
		ik := dst[:strings.LastIndex(dst, "/")+1] + "MEMORY.md"
		idx, _ := memRead(ik)
		if idx != "" && !strings.HasSuffix(idx, "\n") {
			idx += "\n"
		}
		if err := memWrite(ik, idx+line, false); err != nil {
			return "", err
		}
	}
	return dst, nil
}
