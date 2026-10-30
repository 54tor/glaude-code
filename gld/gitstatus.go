package main

// État git des dossiers de projet : branche, retard et avance sur l'amont, nombre de fichiers modifiés. Lecture seule, sans verrou
// (--no-optional-locks), via `git status --porcelain=v2` : le format stable prévu pour les programmes. Les résultats sont gardés
// quelques secondes ; l'interface les relit d'elle-même, sans bouton.

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GitState struct {
	Branch   string `json:"branch"` // "" : HEAD détaché
	Detached bool   `json:"detached,omitempty"`
	Upstream bool   `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Changed  int    `json:"changed"`   // fichiers suivis modifiés, ajoutés, supprimés ou en conflit
	Untrack  int    `json:"untracked"` // fichiers non suivis
}

const gitTTL = 20 * time.Second

var (
	gitMu    sync.Mutex
	gitCache = map[string]gitEntry{}
	gitSem   = make(chan struct{}, 4) // au plus 4 git en même temps
	gitRun   = runGitStatus           // remplaçable dans les tests
)

type gitEntry struct {
	st  GitState
	ok  bool   // false : pas un dépôt (ou git a échoué)
	out string // sortie brute de git status, pour la liste des fichiers
	at  time.Time
}

// GitChange : un fichier en cours de modification. Status : deux lettres comme `git status --short` (index puis arbre de travail),
// « ?? » pour un fichier non suivi, « UU » et assimilés pour un conflit.
type GitChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	From   string `json:"from,omitempty"` // ancien nom d'un fichier renommé
}

// parseGitChanges lit les entrées de fichiers de `git status --porcelain=v2 -z` : séparées par NUL, chemins jamais échappés
// (accents, espaces, guillemets tels quels) ; un renommage est suivi de l'ancien nom dans l'entrée suivante.
func parseGitChanges(out string) []GitChange {
	var cs []GitChange
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		l := recs[i]
		switch {
		case strings.HasPrefix(l, "1 "):
			if f := strings.SplitN(l, " ", 9); len(f) == 9 {
				cs = append(cs, GitChange{Path: f[8], Status: strings.ReplaceAll(f[1], ".", " ")})
			}
		case strings.HasPrefix(l, "2 "):
			if f := strings.SplitN(l, " ", 10); len(f) == 10 && i+1 < len(recs) {
				i++
				cs = append(cs, GitChange{Path: f[9], From: recs[i], Status: strings.ReplaceAll(f[1], ".", " ")})
			}
		case strings.HasPrefix(l, "u "):
			if f := strings.SplitN(l, " ", 11); len(f) == 11 {
				cs = append(cs, GitChange{Path: f[10], Status: f[1]})
			}
		case strings.HasPrefix(l, "? "):
			cs = append(cs, GitChange{Path: strings.TrimPrefix(l, "? "), Status: "??"})
		}
	}
	return cs
}

// parseGitStatus lit la sortie de `git status --porcelain=v2 --branch -z`.
func parseGitStatus(out string) GitState {
	var s GitState
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		l := recs[i]
		switch {
		case strings.HasPrefix(l, "# branch.head "):
			if h := strings.TrimPrefix(l, "# branch.head "); h == "(detached)" {
				s.Detached = true
			} else {
				s.Branch = h
			}
		case strings.HasPrefix(l, "# branch.upstream "):
			s.Upstream = true
		case strings.HasPrefix(l, "# branch.ab "):
			f := strings.Fields(strings.TrimPrefix(l, "# branch.ab "))
			if len(f) == 2 {
				s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[0], "+"))
				s.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[1], "-"))
			}
		case strings.HasPrefix(l, "2 "):
			s.Changed++
			i++ // l'ancien nom
		case strings.HasPrefix(l, "1 "), strings.HasPrefix(l, "u "):
			s.Changed++
		case strings.HasPrefix(l, "? "):
			s.Untrack++
		}
	}
	return s
}

// runGitStatus : core.fsmonitor est neutralisé, car un dépôt qui ne nous appartient pas pourrait y mettre une commande à exécuter.
func runGitStatus(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir, "status", "--porcelain=v2", "--branch", "-z")
	cmd.Env = append(childEnv(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	applyRunAs(cmd)
	b, err := cmd.Output()
	return string(b), err
}

type gitPrefixEntry struct {
	p  string
	at time.Time
}

var (
	gitPrefixes = map[string]gitPrefixEntry{}
	gitPrefixMu sync.Mutex
)

// gitPrefix : chemin de dir sous la racine de son dépôt (« sub/dir/ », vide à la racine). Les chemins de git status sont relatifs à
// la racine ; l'interface s'en sert pour les ramener au dossier. Gardé gitTTL comme l'état du dossier (un « git init »
// ultérieur change la racine), un échec vaut « ».
func gitPrefix(dir string) string {
	gitPrefixMu.Lock()
	e, ok := gitPrefixes[dir]
	gitPrefixMu.Unlock()
	if ok && time.Since(e.at) <= gitTTL {
		return e.p
	}
	var p string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir, "rev-parse", "--show-prefix")
	cmd.Env = append(childEnv(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	applyRunAs(cmd)
	if b, err := cmd.Output(); err == nil {
		p = strings.TrimSpace(string(b))
		gitPrefixMu.Lock()
		gitPrefixes[dir] = gitPrefixEntry{p, time.Now()}
		gitPrefixMu.Unlock()
	}
	return p
}

// gitStates : l'état de chaque dossier. Ce qui est périmé est relu en parallèle, dans la limite de 2 s ; le reste de la liste
// arrive à la requête suivante.
func gitStates(dirs []string) map[string]GitState {
	now := time.Now()
	var stale []string
	gitMu.Lock()
	for _, d := range dirs {
		if e, ok := gitCache[d]; !ok || now.Sub(e.at) > gitTTL {
			stale = append(stale, d)
		}
	}
	gitMu.Unlock()
	var wg sync.WaitGroup
	for _, d := range stale {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gitSem <- struct{}{}
			defer func() { <-gitSem }()
			out, err := gitRun(d)
			e := gitEntry{at: time.Now()}
			if err == nil {
				e.st, e.ok, e.out = parseGitStatus(out), true, out
			}
			gitMu.Lock()
			if len(gitCache) > 2000 {
				clear(gitCache)
			}
			gitCache[d] = e
			gitMu.Unlock()
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	out := map[string]GitState{}
	gitMu.Lock()
	defer gitMu.Unlock()
	for _, d := range dirs {
		if e := gitCache[d]; e.ok {
			out[d] = e.st
		}
	}
	return out
}

// subRepos : les sous-dossiers directs de dir qui sont des dépôts git (un « .git » dedans). Cas courant : les sessions vivent
// dans ~/php, les dépôts dans ~/php/8.1, ~/php/8.2… Résultat gardé une minute.
var (
	reposMu    sync.Mutex
	reposCache = map[string]reposEntry{}
)

type reposEntry struct {
	dirs []string
	at   time.Time
}

func isRepoDir(d string) bool {
	fi, err := os.Lstat(filepath.Join(d, ".git"))
	return err == nil && fi.IsDir() && ownedByMe(fi)
}

// addDirRepos : les dépôts des additionalDirectories de la session (réglages utilisateur, projet et local), au niveau 0 (le
// dossier lui-même) et au niveau 1 (ses sous-dossiers directs). Les chemins relatifs se lisent depuis le dossier du projet.
func addDirRepos(project string) []string {
	home, _ := os.UserHomeDir()
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, f := range []string{filepath.Join(cfgDir(), "settings.json"), filepath.Join(project, ".claude", "settings.json"), filepath.Join(project, ".claude", "settings.local.json")} {
		b, err := readConfigFile(f)
		if err != nil {
			continue
		}
		doc, err := parseOrdered(b)
		if err != nil {
			continue
		}
		v, _ := lookup(doc, "permissions", "additionalDirectories")
		for _, d := range strList(v) {
			switch {
			case d == "~" || strings.HasPrefix(d, "~/"):
				d = filepath.Join(home, d[1:])
			case !filepath.IsAbs(d):
				d = filepath.Join(project, d)
			}
			d = filepath.Clean(d)
			if isRepoDir(d) {
				add(d)
			}
			for _, s := range subRepos(d) {
				add(s)
			}
			if len(out) >= 40 {
				return out
			}
		}
	}
	return out
}

// sessionRepos : les dépôts connus sous le dossier d'une session, hors le dossier lui-même : sous-dossiers directs et add-dir.
func sessionRepos(project string) []string {
	seen := map[string]bool{project: true}
	var out []string
	for _, d := range append(subRepos(project), addDirRepos(project)...) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func subRepos(dir string) []string {
	reposMu.Lock()
	e, ok := reposCache[dir]
	reposMu.Unlock()
	if ok && time.Since(e.at) < time.Minute {
		return e.dirs
	}
	var out []string
	ents, _ := readDirTrusted(dir)
	for _, d := range ents {
		n := d.Name()
		if !d.IsDir() || strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor" {
			continue
		}
		// Un vrai dépôt : « .git » est un dossier (pas un fichier qui renverrait ailleurs) et appartient à l'utilisateur de gld ;
		// un dépôt découvert au hasard d'un dossier n'est pas interrogé s'il vient de quelqu'un d'autre.
		if isRepoDir(filepath.Join(dir, n)) {
			out = append(out, filepath.Join(dir, n))
		}
		if len(out) >= 40 {
			break
		}
	}
	sort.Strings(out)
	reposMu.Lock()
	if len(reposCache) > 2000 {
		clear(reposCache)
	}
	reposCache[dir] = reposEntry{out, time.Now()}
	reposMu.Unlock()
	return out
}

// knownRepo : dir est un dossier de sessions ou un dépôt découvert dessous. On n'interroge jamais un chemin venu du navigateur
// qui ne soit pas l'un d'eux.
func knownRepo(dir string) bool {
	for _, g := range sessions() {
		if g.Missing || g.Project == "" {
			continue
		}
		if g.Project == dir {
			return true
		}
		for _, s := range sessionRepos(g.Project) {
			if s == dir {
				return true
			}
		}
	}
	return false
}

const maxChanges = 300

func routesGit(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/git/changes", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		if !knownRepo(dir) {
			writeJSON(w, 200, map[string]any{"files": []GitChange{}, "prefix": ""})
			return
		}
		gitStates([]string{dir}) // relu si le résultat gardé est périmé
		gitMu.Lock()
		out := gitCache[dir].out
		gitMu.Unlock()
		cs := parseGitChanges(out)
		more := len(cs) - maxChanges
		if more > 0 {
			cs = cs[:maxChanges]
		}
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].Path < cs[j].Path })
		if cs == nil {
			cs = []GitChange{}
		}
		writeJSON(w, 200, map[string]any{"files": cs, "more": max(more, 0), "prefix": gitPrefix(dir)})
	})
	mux.HandleFunc("GET /v1/git/status", func(w http.ResponseWriter, r *http.Request) {
		var dirs []string
		repos := map[string][]string{}
		for _, g := range sessions() {
			if g.Missing || g.Project == "" {
				continue
			}
			dirs = append(dirs, g.Project)
			if sub := sessionRepos(g.Project); len(sub) > 0 {
				repos[g.Project] = sub
				dirs = append(dirs, sub...)
			}
		}
		writeJSON(w, 200, map[string]any{"status": gitStates(dirs), "repos": repos})
	})
}
