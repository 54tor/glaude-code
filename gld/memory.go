// Mémoire de Claude : globale (~/.claude/CLAUDE.md, rules/), par projet (projects/<dossier>/memory/)
// et fichiers CLAUDE.md du projet. Seuls ces fichiers-là sont lisibles, modifiables ou supprimables.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
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
	Gld    bool     `json:"gld,omitempty"`    // règle livrée par gld (metadata.source: gld)
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
func globalClaude(path string) bool { return isCfgEntry(path, "CLAUDE.md") }

// cfgDirs : le dossier de config de ce processus, la config principale (vue d'un home enfant) et celle de chaque home connu.
func cfgDirs() []string {
	out := []string{cfgDir()}
	homes := readHomes()
	if m := os.Getenv(ideMainCfgEnv); m != "" && filepath.IsAbs(m) {
		out = append(out, m)
		homes = append(homes, readHomesFile(filepath.Join(m, homesFileName))...)
	}
	for _, h := range homes {
		if filepath.IsAbs(h.Dir) {
			out = append(out, h.Dir)
		}
	}
	return out
}

// isCfgEntry : path est <config d'un home>/<name> (même fichier ou dossier, liens suivis).
func isCfgEntry(path, name string) bool {
	a, errA := os.Stat(path)
	for _, c := range cfgDirs() {
		g := filepath.Join(c, name)
		if filepath.Clean(path) == g {
			return true
		}
		if b, err := os.Stat(g); errA == nil && err == nil && os.SameFile(a, b) {
			return true
		}
	}
	return false
}

func safeDir(d string) bool { return d != "" && d != "." && d != ".." && !strings.ContainsAny(d, `/\`) }

// knownProjectCwd : le dossier de travail d'un projet dont le dossier de transcripts existe sous projects/ (un nom reçu
// d'un client ne se décode jamais vers un dossier arbitraire du disque).
func knownProjectCwd(d string) (string, bool) {
	if !safeDir(d) || !isDirPath(filepath.Join(root, d)) {
		return "", false
	}
	c := projectCwd(d)
	return c, c != "" && isDirPath(c)
}

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
	bad := terr("srv.memory.fichierDeMemoireInvalide")
	switch {
	case key == "g/CLAUDE.md":
		return cfgDir(), "CLAUDE.md", nil
	case strings.HasPrefix(key, "g/rules/"):
		if n := key[8:]; ruleName.MatchString(n) {
			return filepath.Join(cfgDir(), "rules"), n, nil
		}
	case strings.HasPrefix(key, "m/"):
		// Le dossier du projet doit exister : sans quoi chaque nom reçu créerait un dossier dans projects/.
		if d, n, ok := strings.Cut(key[2:], "/"); ok && safeDir(d) && mdName.MatchString(n) && isDirPath(filepath.Join(root, d)) {
			return filepath.Join(root, d, "memory"), n, nil
		}
	case strings.HasPrefix(key, "r/"): // règle d'un projet : <dossier du projet>/.claude/rules/<nom>
		if d, n, ok := strings.Cut(key[2:], "/"); ok && safeDir(d) && ruleName.MatchString(n) {
			c, ok := knownProjectCwd(d)
			if !ok {
				return "", "", terr("srv.memory.dossierDuProjetIntrouvable")
			}
			base, err := projectRulesDir(c)
			return base, n, err
		}
	case strings.HasPrefix(key, "p/"):
		if d, f, ok := strings.Cut(key[2:], "/"); ok && safeDir(d) {
			for _, a := range projectFiles {
				if f == a {
					c, ok := knownProjectCwd(d)
					if !ok {
						return "", "", terr("srv.memory.dossierDuProjetIntrouvable")
					}
					if globalClaude(filepath.Join(c, filepath.FromSlash(f))) {
						return "", "", terr("srv.memory.cEstLeClaude")
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
		return "", terr("srv.memory.dossierDuProjetIntrouvable")
	}
	rules := filepath.Join(cwd, ".claude", "rules")
	if isCfgEntry(rules, "rules") {
		return "", terr("srv.memory.ceSontLesRegles")
	}
	for _, p := range []string{filepath.Join(cwd, ".claude"), rules} {
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", terr("srv.memory.claudeEstUnLien")
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

// memTargets : un projet par dossier de transcripts, sans lire ses fichiers de mémoire (ce que memList fait en plus).
func memTargets() []MemTarget {
	dirs, _ := readDirTrusted(root)
	targets := []MemTarget{}
	for _, d := range dirs {
		if d.IsDir() {
			targets = append(targets, MemTarget{d.Name(), projectCwd(d.Name())})
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Project < targets[j].Project })
	return targets
}

// liveProjects : les projets dont le dossier de travail existe encore, avec ou sans fichier de mémoire. Les réglages, skills et
// .mcp.json d'un projet vivent dans son dossier, pas dans ~/.claude. ~ et ~/.claude n'en sont pas : leur .claude/ est le
// dossier de configuration lui-même, déjà lu comme utilisateur.
func liveProjects() []MemTarget {
	home, _ := os.UserHomeDir()
	targets := memTargets()
	out := []MemTarget{}
	for _, t := range targets {
		if t.Project == "" || t.Project == cfgDir() || (home != "" && t.Project == home) {
			continue
		}
		if fi, err := os.Stat(t.Project); err == nil && fi.IsDir() {
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
	dirs, _ := readDirTrusted(root)
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
		err = terr("srv.memory.fichierTropGrosOu")
	}
	return string(b), err
}

// memSave remplace le fichier (écriture atomique, droits conservés) ou le crée ; la version remplacée va à la corbeille.
func memSave(key, text string) error { return memWrite(key, text, true) }

var memWriteMu sync.Mutex // deux écritures de la même entrée se partageaient le même fichier temporaire

// memWrite : keep met d'abord la version actuelle à la corbeille (pas pour les mises à jour d'index, dérivées).
func memWrite(key, text string, keep bool) error {
	if len(text) > memMax || !utf8.ValidString(text) {
		return terr("srv.memory.texteTropLongOu")
	}
	memWriteMu.Lock()
	defer memWriteMu.Unlock()
	base, rel, err := memResolve(key)
	if err != nil {
		return err
	}
	old, rerr := memRead(key)
	if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return rerr // trop gros ou pas en UTF-8 : le remplacer le perdrait sans passer par la corbeille
	}
	if keep && rerr == nil && old != text {
		if err := toTrash(key, "remplacé"); err != nil {
			return err
		}
	}
	if err := mkdirTrusted(base, 0o755); err != nil {
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

const mergeMax = 100

// mergePrompt : la demande de fusion faite à Claude, et le dossier où la lancer (le projet concerné, sinon ~).
// out : le document de sortie, l'un des fichiers choisis (vide : le premier).
func mergePrompt(keys []string, out string) (string, string, error) {
	if len(keys) < 2 || len(keys) > mergeMax {
		return "", "", terr("srv.memory.choisisDeAFichiers", "A1", mergeMax)
	}
	if out == "" {
		out = keys[0]
	}
	// Hors sélection : CLAUDE.md global ou fichier du projet (CLAUDE.md, AGENTS.md…), à créer s'il n'existe pas.
	outPath, extra := "", ""
	if !slices.Contains(keys, out) {
		if out != "g/CLAUDE.md" && !strings.HasPrefix(out, "p/") {
			return "", "", terr("srv.memory.documentDeSortieInvalide")
		}
		base, rel, err := memResolve(out)
		if err != nil {
			return "", "", err
		}
		outPath, extra = filepath.Join(base, rel), L("prompt.mergeOutExtra")
	}
	home, _ := os.UserHomeDir()
	cwd := ""
	var b strings.Builder
	b.WriteString(L("prompt.mergeHeader") + "\n")
	for _, k := range keys {
		base, rel, err := memResolve(k)
		if err != nil {
			return "", "", err
		}
		p := filepath.Join(base, rel)
		if _, err := os.Stat(p); err != nil {
			return "", "", terr("srv.memory.introuvable", "A1", p)
		}
		b.WriteString("- " + p + "\n")
		if k == out {
			outPath = p
		}
		if d, _, ok := strings.Cut(k[2:], "/"); cwd == "" && (k[0] == 'm' || k[0] == 'p') && ok {
			if c := projectCwd(d); c != "" {
				if fi, err := os.Stat(c); err == nil && fi.IsDir() {
					cwd = c
				}
			}
		}
	}
	b.WriteString("\n" + L("prompt.mergeRules", "A1", outPath, "A2", extra) + "\n")
	if cwd == "" { // règles globales : le dossier de configuration, plutôt que ~ tout entier (autre dossier à approuver)
		if c := filepath.Join(home, ".claude"); isDirPath(c) {
			cwd = c
		} else {
			cwd = home
		}
	}
	return b.String(), cwd, nil
}

// memTransfer déplace une entrée de mémoire de projet ou une règle vers la mémoire d'un autre projet (to = son dossier de
// transcripts) ou vers les règles globales (to = "global"), ou la copie si keep. Sans écraser ; l'index suit.
func memTransfer(key, to string, keep bool) (string, error) {
	if !strings.HasPrefix(key, "m/") && !strings.HasPrefix(key, "g/rules/") && !strings.HasPrefix(key, "r/") {
		return "", terr("srv.memory.seulesLesEntreesDe")
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
			return "", terr("srv.memory.destinationInvalide")
		}
		dst = "r/" + to[2:] + "/" + rel
		if _, _, err := memResolve(dst); err != nil {
			return "", err
		}
	case strings.HasPrefix(key, "r/") && to != strings.Split(key, "/")[1]: // une règle ne retourne qu'à la mémoire de son propre projet
		return "", terr("srv.memory.uneRegleDeProjet")
	default:
		if !safeDir(to) {
			return "", terr("srv.memory.destinationInvalide")
		}
		if fi, err := os.Stat(filepath.Join(root, to)); err != nil || !fi.IsDir() {
			return "", terr("srv.memory.projetDeDestinationIntrouvable")
		}
		dst = "m/" + to + "/" + rel
	}
	if dst == key {
		return "", terr("srv.memory.dejaACetEndroit")
	}
	if _, err := memRead(dst); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", terr("srv.memory.unFichierDuMeme")
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

func isDirPath(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// memRename renomme un fichier dans son dossier ; la ligne d'index d'une entrée de projet suit.
func memRename(key, name string) (string, error) {
	_, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	if rel == "MEMORY.md" || !(strings.HasPrefix(key, "m/") || strings.HasPrefix(key, "g/rules/") || strings.HasPrefix(key, "r/")) {
		return "", terr("srv.memory.seulesLesEntreesDe")
	}
	dst := key[:strings.LastIndex(key, "/")+1] + name
	if dst == key {
		return "", terr("srv.memory.dejaACetEndroit")
	}
	if _, _, err := memResolve(dst); err != nil || name == "MEMORY.md" {
		return "", terr("srv.memory.destinationInvalide")
	}
	if _, err := memRead(dst); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", terr("srv.memory.unFichierDuMeme")
	}
	text, err := memRead(key)
	if err != nil {
		return "", err
	}
	line := "- [" + strings.TrimSuffix(name, ".md") + "](" + name + ")\n"
	ik := key[:strings.LastIndex(key, "/")+1] + "MEMORY.md"
	if strings.HasPrefix(key, "m/") {
		if idx, err := memRead(ik); err == nil {
			for _, l := range strings.SplitAfter(idx, "\n") {
				if strings.Contains(l, "]("+rel+")") {
					line = strings.ReplaceAll(strings.TrimRight(l, "\n"), "]("+rel+")", "]("+name+")") + "\n"
					break
				}
			}
		}
	}
	if err := memSave(dst, text); err != nil {
		return "", err
	}
	if err := memDelete(key, false); err != nil {
		return "", err
	}
	if strings.HasPrefix(key, "m/") {
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
