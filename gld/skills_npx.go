package main

// Le CLI `npx skills` (installation, mise à jour, retrait, recherche de skills). Le paquet est épinglé : un `@latest` exécuterait
// du code publié après notre dernière lecture. gld ne réécrit ni le format de verrouillage ni l'installation, il valide les
// arguments, lance la commande et lit le verrou.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const skillsPkg = "skills@1.7.4"

var (
	skillRepoRe  = regexp.MustCompile(`^[A-Za-z0-9][\w.-]{0,99}/[A-Za-z0-9][\w.-]{0,99}$`)
	skillURLRe   = regexp.MustCompile(`^https://[\w.-]+(:\d+)?/[\w./~%+-]{1,300}$`)
	ansiRe       = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	npxTimeout   = 5 * time.Minute
	npxWaitDelay = 5 * time.Second // npx lance sh puis node : un petit-enfant orphelin garderait le tube ouvert et le verrou pris
	npxMu        sync.Mutex        // une seule action à la fois : le verrou est un fichier partagé dont gld ne maîtrise pas le format
)

// npxEnv : l'environnement de npx, sans les jetons de gld. Le dossier d'un projet peut porter son .npmrc : l'environnement
// l'emporte sur lui, le paquet épinglé vient du registre public. Une base vide (Windows) reprend celle du processus.
func npxEnv(base []string) []string {
	if base == nil {
		base = environNoAuth()
	}
	var filtered []string
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GLD_WEB_") || key == "GLD_AUTH_KEY" || key == "NODE_OPTIONS" || key == "npm_config_node_options" || key == "npm_config_registry" {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "npm_config_registry=https://registry.npmjs.org/", "npm_config_node_options=", "NODE_OPTIONS=")
}

// skillsHome : le home où npx pose ses fichiers (celui du compte cible sous GLD_RUN_AS).
func skillsHome() string {
	if targetHome := runAsHome(); targetHome != "" {
		return targetHome
	}
	targetHome, _ := os.UserHomeDir()
	return targetHome
}

// personalLockPath : le verrou personnel de `npx skills` ($XDG_STATE_HOME/skills/.skill-lock.json si la variable est définie).
func personalLockPath() string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "skills", ".skill-lock.json")
	}
	return filepath.Join(skillsHome(), ".agents", ".skill-lock.json")
}

// managedByNpx : le skill est un lien vers le dossier canonique de `npx skills`. Le verrou personnel est partagé par tous les
// agents et tous les homes : y figurer ne prouve pas que le dossier de ce home vient de lui.
func managedByNpx(skillsDir, canonDir, name string) bool {
	skillPath := filepath.Join(skillsDir, name)
	if linkInfo, err := os.Lstat(skillPath); err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		return false
	}
	resolved, linkErr := filepath.EvalSymlinks(skillPath)
	expected, canonErr := filepath.EvalSymlinks(filepath.Join(canonDir, name))
	return linkErr == nil && canonErr == nil && resolved == expected
}

func stripANSI(text string) string { return ansiRe.ReplaceAllString(text, "") }

// npxSkillsArgs : les arguments à passer à `npx`. Tout ce qui vient du client est validé ici ; rien ne passe par un shell.
func npxSkillsArgs(action, source, skill string, global bool) ([]string, error) {
	if !skillName.MatchString(skill) {
		return nil, terr("srv.skills.npxBadName")
	}
	base := []string{"--yes", skillsPkg}
	scope := "-p"
	if global {
		scope = "-g"
	}
	switch action {
	case "add":
		if !skillRepoRe.MatchString(source) && !skillURLRe.MatchString(source) {
			return nil, terr("srv.skills.npxBadSource")
		}
		args := append(base, "add", source, "-s", skill, "-a", "claude-code", "-y")
		if global {
			args = append(args, scope)
		}
		return args, nil
	case "update":
		return append(base, action, skill, "-y", scope), nil
	case "remove": // sans -a, remove vise tous les agents, dont un skill de même nom écrit à la main chez un autre
		return append(base, action, skill, "-a", "claude-code", "-y", scope), nil
	}
	return nil, terr("srv.skills.npxBadAction")
}

var runNpxSkills = func(commandContext context.Context, dir string, args []string) (string, error) {
	cmd := withPath(exec.CommandContext(commandContext, "npx", args...))
	cmd.Dir = dir
	cmd.Env = npxEnv(cmd.Env)
	cmd.WaitDelay = npxWaitDelay
	output, err := cmd.CombinedOutput()
	return stripANSI(string(output)), err
}

// skillsAct : add, update ou remove, pour l'utilisateur ou pour un projet. Update et remove ne touchent que ce que le fichier de
// verrouillage de la portée déclare : un skill écrit à la main ou venu d'un plugin n'est jamais modifié par `npx skills`.
func skillsAct(requestContext context.Context, action, source, skill, scope, projectName string) (string, error) {
	var dir, lock, skillsDir, canonDir string
	switch scope {
	case "user":
		dir = skillsHome()
		lock, skillsDir, canonDir = personalLockPath(), filepath.Join(cfgDir(), "skills"), filepath.Join(dir, ".agents", "skills")
	case "project":
		if !safeDir(projectName) {
			return "", terr("srv.plugins.projetRequis")
		}
		dir = projectCwd(projectName)
		if dirInfo, statErr := os.Stat(dir); dir == "" || statErr != nil || !dirInfo.IsDir() {
			return "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
		lock, skillsDir, canonDir = filepath.Join(dir, "skills-lock.json"), filepath.Join(dir, ".claude", "skills"), filepath.Join(dir, ".agents", "skills")
		if shadowsSkillsPkg(dir) {
			return "", terr("srv.skills.npxUnsafeProject")
		}
	default:
		return "", terr("srv.skills.npxBadScope")
	}
	args, err := npxSkillsArgs(action, source, skill, scope == "user")
	if err != nil {
		return "", err
	}
	if action != "add" {
		if _, isLocked := readLock(lock)[skill]; !isLocked || !managedByNpx(skillsDir, canonDir, skill) {
			return "", terr("srv.skills.npxNotLocked")
		}
	}
	if !npxMu.TryLock() {
		return "", terr("srv.skills.npxBusy")
	}
	defer npxMu.Unlock()
	requestContext, cancel := context.WithTimeout(requestContext, npxTimeout)
	defer cancel()
	output, err := runNpxSkills(requestContext, dir, args)
	output = strings.TrimSpace(output)
	if errors.Is(err, exec.ErrNotFound) {
		return output, terr("srv.skills.npxMissing")
	}
	return output, err
}

type SkillHit struct {
	Ref      string `json:"ref"`
	Source   string `json:"source"`
	Skill    string `json:"skill"`
	Installs string `json:"installs"`
	URL      string `json:"url"`
}

// parseSkillHits : la sortie de `npx skills find` (ANSI déjà retiré) : « propriétaire/dépôt@skill N installs », puis « └ URL ».
func parseSkillHits(output string) []SkillHit {
	hits := []SkillHit{}
	for _, outputLine := range strings.Split(output, "\n") {
		outputLine = strings.TrimSpace(outputLine)
		if link, isURLLine := strings.CutPrefix(outputLine, "└"); isURLLine && len(hits) > 0 && hits[len(hits)-1].URL == "" {
			hits[len(hits)-1].URL = strings.TrimSpace(link)
			continue
		}
		fields := strings.Fields(outputLine)
		if len(fields) != 3 || fields[2] != "installs" {
			continue
		}
		atIndex := strings.LastIndex(fields[0], "@")
		if atIndex < 1 || !skillRepoRe.MatchString(fields[0][:atIndex]) || !skillName.MatchString(fields[0][atIndex+1:]) {
			continue
		}
		hits = append(hits, SkillHit{Ref: fields[0], Source: fields[0][:atIndex], Skill: fields[0][atIndex+1:], Installs: fields[1]})
	}
	return hits
}

// skillsFind : `npx skills find <q>`. Un q qui commencerait par « - » serait lu comme une option.
func skillsFind(requestContext context.Context, query string) ([]SkillHit, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 80 || query[0] == '-' || strings.ContainsAny(query, "\r\n") {
		return nil, terr("srv.http_util.requeteInvalide")
	}
	if !npxMu.TryLock() { // la route n'exige que le droit de lecture : pas de lancements en parallèle
		return nil, terr("srv.skills.npxBusy")
	}
	defer npxMu.Unlock()
	requestContext, cancel := context.WithTimeout(requestContext, 30*time.Second)
	defer cancel()
	output, err := runNpxSkills(requestContext, skillsHome(), []string{"--yes", skillsPkg, "find", query})
	if errors.Is(err, exec.ErrNotFound) {
		return nil, terr("srv.skills.npxMissing")
	}
	if err != nil {
		return nil, err
	}
	return parseSkillHits(output), nil
}

// remoteTrees : les arbres git d'un dépôt GitHub (chemin → SHA, "" pour la racine). `skillFolderHash` du verrou est le SHA de
// l'arbre du dossier du skill : l'égalité avec celui du dépôt dit si le skill a changé. Sans jeton : 60 requêtes par heure.
var remoteTrees = func(requestContext context.Context, owner, repo string) (map[string]string, error) {
	requestContext, cancel := context.WithTimeout(requestContext, 15*time.Second)
	defer cancel()
	apiRequest, err := http.NewRequestWithContext(requestContext, "GET", "https://api.github.com/repos/"+owner+"/"+repo+"/git/trees/HEAD?recursive=1", nil)
	if err != nil {
		return nil, err
	}
	apiRequest.Header.Set("Accept", "application/vnd.github+json")
	apiRequest.Header.Set("User-Agent", "gld")
	response, err := http.DefaultClient.Do(apiRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var treeListing struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Tree      []struct{ Path, Type, SHA string }
	}
	if json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&treeListing) != nil || treeListing.Truncated {
		return nil, errors.New("arbre illisible ou tronqué")
	}
	trees := map[string]string{"": treeListing.SHA}
	for _, entry := range treeListing.Tree {
		if entry.Type == "tree" {
			trees[entry.Path] = entry.SHA
		}
	}
	return trees, nil
}

var githubURLRe = regexp.MustCompile(`^https://github\.com/([\w.-]+)/([\w.-]+?)(\.git)?/?$`)

func lockRepo(entry LockEntry) (owner, repo string, isGitHub bool) {
	if entry.SourceType != "github" {
		return "", "", false
	}
	if skillRepoRe.MatchString(entry.Source) {
		ownerName, repoName, _ := strings.Cut(entry.Source, "/")
		return ownerName, repoName, true
	}
	if match := githubURLRe.FindStringSubmatch(entry.SourceURL); match != nil {
		return match[1], match[2], true
	}
	return "", "", false
}

// skillUpdateState : update, current ou unknown. Jamais « current » sans preuve.
func skillUpdateState(entry LockEntry, trees map[string]string) string {
	if entry.SourceType != "github" || trees == nil || entry.Hash == "" || entry.SkillPath == "" {
		return "unknown"
	}
	dir := path.Dir(entry.SkillPath)
	if dir == "." {
		dir = ""
	}
	sha, found := trees[dir]
	switch {
	case !found:
		return "unknown"
	case sha == entry.Hash:
		return "current"
	}
	return "update"
}

type updRepo struct {
	at    time.Time
	trees map[string]string // nil : la dernière tentative a échoué
}

var updCache struct {
	sync.Mutex
	m         map[string]updRepo
	lastForce time.Time
}

const forceMin = 30 * time.Second // /v1/skills/updates?force=1 ne demande que mem_read : sans plancher, il viderait le quota GitHub

// checkSkillUpdates : l'état de chaque skill du verrou personnel et des verrous de projet, clé = celle de listSkills.
// Une requête par dépôt, gardée 6 h (5 min après un échec : limite de débit, hors ligne).
func checkSkillUpdates(requestContext context.Context, force bool) map[string]string {
	type pendingSkill struct {
		key   string
		entry LockEntry
	}
	var pendingSkills []pendingSkill
	for name, entry := range readLock(personalLockPath()) {
		pendingSkills = append(pendingSkills, pendingSkill{"u/" + name, entry})
	}
	for _, liveProject := range liveProjects() {
		for name, entry := range readLock(filepath.Join(liveProject.Project, "skills-lock.json")) {
			pendingSkills = append(pendingSkills, pendingSkill{"p/" + liveProject.Dir + "/" + name, entry})
		}
	}
	updCache.Lock()
	defer updCache.Unlock()
	if updCache.m == nil {
		updCache.m = map[string]updRepo{}
	}
	if force && time.Since(updCache.lastForce) < forceMin {
		force = false
	}
	if force {
		updCache.lastForce = time.Now()
	}
	states := map[string]string{}
	fetched := map[string]bool{}
	for _, pending := range pendingSkills {
		ownerName, repoName, isGitHub := lockRepo(pending.entry)
		if !isGitHub {
			states[pending.key] = "unknown"
			continue
		}
		repoKey := ownerName + "/" + repoName
		cached, isCached := updCache.m[repoKey]
		ttl := 6 * time.Hour
		if cached.trees == nil {
			ttl = 5 * time.Minute
		}
		if (force && !fetched[repoKey]) || !isCached || time.Since(cached.at) > ttl {
			fetched[repoKey] = true
			trees, err := remoteTrees(requestContext, ownerName, repoName)
			if err != nil {
				trees = nil
			}
			cached = updRepo{time.Now(), trees}
			updCache.m[repoKey] = cached
		}
		states[pending.key] = skillUpdateState(pending.entry, cached.trees)
	}
	return states
}

// shadowsSkillsPkg : npx remonte les dossiers parents à la recherche d'un node_modules/skills et le préfère au paquet épinglé.
func shadowsSkillsPkg(dir string) bool {
	for currentDir := filepath.Clean(dir); ; currentDir = filepath.Dir(currentDir) {
		if _, err := os.Lstat(filepath.Join(currentDir, "node_modules", "skills")); err == nil {
			return true
		}
		if filepath.Dir(currentDir) == currentDir {
			return false
		}
	}
}
