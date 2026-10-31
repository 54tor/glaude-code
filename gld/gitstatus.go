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
	Branch   string          `json:"branch"` // "" : HEAD détaché
	Detached bool            `json:"detached,omitempty"`
	Upstream bool            `json:"upstream"`
	Ahead    int             `json:"ahead"`
	Behind   int             `json:"behind"`
	Changed  int             `json:"changed"`           // fichiers suivis modifiés, ajoutés, supprimés ou en conflit
	Untrack  int             `json:"untracked"`         // fichiers non suivis
	Remotes  []GitRemoteSync `json:"remotes,omitempty"` // position de la branche sur chaque remote, d'après les références locales (sans fetch)
}

// GitRemoteSync : la branche courante face à un remote. Missing : le remote n'a pas cette branche (Ahead/Behind valent alors 0).
type GitRemoteSync struct {
	Name    string `json:"name"`
	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
	Missing bool   `json:"missing,omitempty"`
}

const (
	gitTTL             = 20 * time.Second
	gitCommandTimeout  = 5 * time.Second
	gitStatesWait      = 2 * time.Second
	gitCacheMaxEntries = 2000
	maxReposListed     = 40
)

var (
	gitRemotesRun = runGitRemotes // remplaçable dans les tests
	gitMu         sync.Mutex
	gitCache      = map[string]gitEntry{}
	gitSem        = make(chan struct{}, 4) // au plus 4 git en même temps
	gitRun        = runGitStatus           // remplaçable dans les tests
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
func parseGitChanges(statusOutput string) []GitChange {
	var changes []GitChange
	records := strings.Split(statusOutput, "\x00")
	for recordIndex := 0; recordIndex < len(records); recordIndex++ {
		record := records[recordIndex]
		switch {
		case strings.HasPrefix(record, "1 "):
			if fields := strings.SplitN(record, " ", 9); len(fields) == 9 {
				changes = append(changes, GitChange{Path: fields[8], Status: strings.ReplaceAll(fields[1], ".", " ")})
			}
		case strings.HasPrefix(record, "2 "):
			if fields := strings.SplitN(record, " ", 10); len(fields) == 10 && recordIndex+1 < len(records) {
				recordIndex++
				changes = append(changes, GitChange{Path: fields[9], From: records[recordIndex], Status: strings.ReplaceAll(fields[1], ".", " ")})
			}
		case strings.HasPrefix(record, "u "):
			if fields := strings.SplitN(record, " ", 11); len(fields) == 11 {
				changes = append(changes, GitChange{Path: fields[10], Status: fields[1]})
			}
		case strings.HasPrefix(record, "? "):
			changes = append(changes, GitChange{Path: strings.TrimPrefix(record, "? "), Status: "??"})
		}
	}
	return changes
}

// parseGitStatus lit la sortie de `git status --porcelain=v2 --branch -z`.
func parseGitStatus(statusOutput string) GitState {
	var state GitState
	records := strings.Split(statusOutput, "\x00")
	for recordIndex := 0; recordIndex < len(records); recordIndex++ {
		record := records[recordIndex]
		switch {
		case strings.HasPrefix(record, "# branch.head "):
			if head := strings.TrimPrefix(record, "# branch.head "); head == "(detached)" {
				state.Detached = true
			} else {
				state.Branch = head
			}
		case strings.HasPrefix(record, "# branch.upstream "):
			state.Upstream = true
		case strings.HasPrefix(record, "# branch.ab "):
			aheadBehind := strings.Fields(strings.TrimPrefix(record, "# branch.ab "))
			if len(aheadBehind) == 2 {
				state.Ahead, _ = strconv.Atoi(strings.TrimPrefix(aheadBehind[0], "+"))
				state.Behind, _ = strconv.Atoi(strings.TrimPrefix(aheadBehind[1], "-"))
			}
		case strings.HasPrefix(record, "2 "):
			state.Changed++
			recordIndex++ // l'ancien nom
		case strings.HasPrefix(record, "1 "), strings.HasPrefix(record, "u "):
			state.Changed++
		case strings.HasPrefix(record, "? "):
			state.Untrack++
		}
	}
	return state
}

// runGitStatus : core.fsmonitor est neutralisé, car un dépôt qui ne nous appartient pas pourrait y mettre une commande à exécuter.
func runGitStatus(dir string) (string, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir, "status", "--porcelain=v2", "--branch", "-z") // #nosec G702 -- exécutable fixe, arguments en tableau, sans shell
	cmd.Env = append(childEnv(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	applyRunAs(cmd)
	output, err := cmd.Output()
	return string(output), err
}

// parseRemoteSync croise les remotes du dépôt (`git remote`) avec `git for-each-ref --format='%(refname) %(ahead-behind:HEAD)' refs/remotes` :
// une référence refs/remotes/<remote>/<branche> par remote qui a la branche, les autres sont « Missing ».
func parseRemoteSync(remoteNames, refsOutput, branch string) []GitRemoteSync {
	positions := map[string][2]int{}
	for _, refLine := range strings.Split(refsOutput, "\n") {
		fields := strings.Fields(refLine)
		if len(fields) != 3 {
			continue
		}
		behind, behindErr := strconv.Atoi(fields[1]) // %(ahead-behind:HEAD) compte du côté de la référence : ses avances sont nos retards
		ahead, aheadErr := strconv.Atoi(fields[2])
		if aheadErr == nil && behindErr == nil {
			positions[fields[0]] = [2]int{ahead, behind}
		}
	}
	var remotes []GitRemoteSync
	for _, name := range strings.Fields(remoteNames) {
		position, found := positions["refs/remotes/"+name+"/"+branch]
		remotes = append(remotes, GitRemoteSync{Name: name, Ahead: position[0], Behind: position[1], Missing: !found})
	}
	return remotes
}

func runGitRemotes(dir, branch string) []GitRemoteSync {
	remoteNames, err := gitOut1(dir, "remote")
	if err != nil || remoteNames == "" {
		return nil
	}
	refsOutput, _ := gitOut1(dir, "for-each-ref", "--format=%(refname) %(ahead-behind:HEAD)", "refs/remotes")
	return parseRemoteSync(remoteNames, refsOutput, branch)
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
	cached, found := gitPrefixes[dir]
	gitPrefixMu.Unlock()
	isFresh := found && time.Since(cached.at) <= gitTTL
	if isFresh {
		return cached.p
	}
	var prefix string
	commandContext, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir, "rev-parse", "--show-prefix") // #nosec G702 -- exécutable fixe, arguments en tableau, sans shell
	cmd.Env = append(childEnv(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	applyRunAs(cmd)
	if output, err := cmd.Output(); err == nil {
		prefix = strings.TrimSpace(string(output))
		gitPrefixMu.Lock()
		gitPrefixes[dir] = gitPrefixEntry{prefix, time.Now()}
		gitPrefixMu.Unlock()
	}
	return prefix
}

// gitStates : l'état de chaque dossier. Ce qui est périmé est relu en parallèle, dans la limite de 2 s ; le reste de la liste
// arrive à la requête suivante.
func gitStates(dirs []string) map[string]GitState {
	now := time.Now()
	var stale []string
	gitMu.Lock()
	for _, dir := range dirs {
		cached, found := gitCache[dir]
		isStale := !found || now.Sub(cached.at) > gitTTL
		if isStale {
			stale = append(stale, dir)
		}
	}
	gitMu.Unlock()
	var waitGroup sync.WaitGroup
	for _, dir := range stale {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			gitSem <- struct{}{}
			defer func() { <-gitSem }()
			statusOutput, err := gitRun(dir)
			entry := gitEntry{at: time.Now()}
			if err == nil {
				entry.st, entry.ok, entry.out = parseGitStatus(statusOutput), true, statusOutput
				if entry.st.Branch != "" {
					entry.st.Remotes = gitRemotesRun(dir, entry.st.Branch)
				}
			}
			gitMu.Lock()
			if len(gitCache) > gitCacheMaxEntries {
				clear(gitCache)
			}
			gitCache[dir] = entry
			gitMu.Unlock()
		}()
	}
	done := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(gitStatesWait):
	}
	states := map[string]GitState{}
	gitMu.Lock()
	defer gitMu.Unlock()
	for _, dir := range dirs {
		if entry := gitCache[dir]; entry.ok {
			states[dir] = entry.st
		}
	}
	return states
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

func isRepoDir(dir string) bool {
	gitInfo, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil && gitInfo.IsDir() && ownedByMe(gitInfo)
}

// addDirRepos : les dépôts des additionalDirectories de la session (réglages utilisateur, projet et local), au niveau 0 (le
// dossier lui-même) et au niveau 1 (ses sous-dossiers directs). Les chemins relatifs se lisent depuis le dossier du projet.
func addDirRepos(project string) []string {
	home, _ := os.UserHomeDir()
	var repos []string
	seen := map[string]bool{}
	addRepo := func(repoDir string) {
		if !seen[repoDir] {
			seen[repoDir] = true
			repos = append(repos, repoDir)
		}
	}
	settingsPaths := []string{
		filepath.Join(cfgDir(), "settings.json"),
		filepath.Join(project, ".claude", "settings.json"),
		filepath.Join(project, ".claude", "settings.local.json"),
	}
	for _, settingsPath := range settingsPaths {
		content, err := readConfigFile(settingsPath)
		if err != nil {
			continue
		}
		doc, err := parseOrdered(content)
		if err != nil {
			continue
		}
		directories, _ := lookup(doc, "permissions", "additionalDirectories")
		for _, dir := range strList(directories) {
			switch {
			case dir == "~" || strings.HasPrefix(dir, "~/"):
				dir = filepath.Join(home, dir[1:])
			case !filepath.IsAbs(dir):
				dir = filepath.Join(project, dir)
			}
			dir = filepath.Clean(dir)
			if isRepoDir(dir) {
				addRepo(dir)
			}
			for _, subRepo := range subRepos(dir) {
				addRepo(subRepo)
			}
			if len(repos) >= maxReposListed {
				return repos
			}
		}
	}
	return repos
}

// sessionRepos : les dépôts connus sous le dossier d'une session, hors le dossier lui-même : sous-dossiers directs et add-dir.
func sessionRepos(project string) []string {
	seen := map[string]bool{project: true}
	var repos []string
	for _, repo := range append(subRepos(project), addDirRepos(project)...) {
		if !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}
	return repos
}

func subRepos(dir string) []string {
	reposMu.Lock()
	cached, found := reposCache[dir]
	reposMu.Unlock()
	isFresh := found && time.Since(cached.at) < time.Minute
	if isFresh {
		return cached.dirs
	}
	var repos []string
	entries, _ := readDirTrusted(dir)
	for _, entry := range entries {
		name := entry.Name()
		isIgnoredDir := strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor"
		if !entry.IsDir() || isIgnoredDir {
			continue
		}
		// Un vrai dépôt : « .git » est un dossier (pas un fichier qui renverrait ailleurs) et appartient à l'utilisateur de gld ;
		// un dépôt découvert au hasard d'un dossier n'est pas interrogé s'il vient de quelqu'un d'autre.
		repoPath := filepath.Join(dir, name)
		if isRepoDir(repoPath) {
			repos = append(repos, repoPath)
		}
		if len(repos) >= maxReposListed {
			break
		}
	}
	sort.Strings(repos)
	reposMu.Lock()
	if len(reposCache) > gitCacheMaxEntries {
		clear(reposCache)
	}
	reposCache[dir] = reposEntry{repos, time.Now()}
	reposMu.Unlock()
	return repos
}

// knownRepo : dir est un dossier de sessions ou un dépôt découvert dessous. On n'interroge jamais un chemin venu du navigateur
// qui ne soit pas l'un d'eux.
func knownRepo(dir string) bool {
	for _, group := range sessions() {
		if group.Missing || group.Project == "" {
			continue
		}
		if group.Project == dir {
			return true
		}
		for _, repo := range sessionRepos(group.Project) {
			if repo == dir {
				return true
			}
		}
	}
	return false
}

const maxChanges = 300

func routesGit(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/git/changes", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		if !knownRepo(dir) {
			writeJSON(responseWriter, http.StatusOK, map[string]any{"files": []GitChange{}, "prefix": ""})
			return
		}
		gitStates([]string{dir}) // relu si le résultat gardé est périmé
		gitMu.Lock()
		statusOutput := gitCache[dir].out
		gitMu.Unlock()
		changes := parseGitChanges(statusOutput)
		more := len(changes) - maxChanges
		if more > 0 {
			changes = changes[:maxChanges]
		}
		sort.SliceStable(changes, func(left, right int) bool {
			return changes[left].Path < changes[right].Path
		})
		if changes == nil {
			changes = []GitChange{}
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"files": changes, "more": max(more, 0), "prefix": gitPrefix(dir)})
	})
	mux.HandleFunc("GET /v1/git/status", func(responseWriter http.ResponseWriter, request *http.Request) {
		var dirs []string
		repos := map[string][]string{}
		for _, group := range sessions() {
			if group.Missing || group.Project == "" {
				continue
			}
			dirs = append(dirs, group.Project)
			if nestedRepos := sessionRepos(group.Project); len(nestedRepos) > 0 {
				repos[group.Project] = nestedRepos
				dirs = append(dirs, nestedRepos...)
			}
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"status": gitStates(dirs), "repos": repos})
	})
}
