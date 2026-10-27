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
	var out []string
	for _, e := range base {
		k, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(k, "GLD_WEB_") || k == "GLD_AUTH_KEY" || k == "NODE_OPTIONS" || k == "npm_config_node_options" || k == "npm_config_registry" {
			continue
		}
		out = append(out, e)
	}
	return append(out, "npm_config_registry=https://registry.npmjs.org/", "npm_config_node_options=", "NODE_OPTIONS=")
}

// skillsHome : le home où npx pose ses fichiers (celui du compte cible sous GLD_RUN_AS).
func skillsHome() string {
	if h := runAsHome(); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// personalLockPath : le verrou personnel de `npx skills` ($XDG_STATE_HOME/skills/.skill-lock.json si la variable est définie).
func personalLockPath() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "skills", ".skill-lock.json")
	}
	return filepath.Join(skillsHome(), ".agents", ".skill-lock.json")
}

// managedByNpx : le skill est un lien vers le dossier canonique de `npx skills`. Le verrou personnel est partagé par tous les
// agents et tous les homes : y figurer ne prouve pas que le dossier de ce home vient de lui.
func managedByNpx(skillsDir, canonDir, name string) bool {
	p := filepath.Join(skillsDir, name)
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	got, err1 := filepath.EvalSymlinks(p)
	want, err2 := filepath.EvalSymlinks(filepath.Join(canonDir, name))
	return err1 == nil && err2 == nil && got == want
}

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

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

var runNpxSkills = func(ctx context.Context, dir string, args []string) (string, error) {
	cmd := withPath(exec.CommandContext(ctx, "npx", args...))
	cmd.Dir = dir
	cmd.Env = npxEnv(cmd.Env)
	cmd.WaitDelay = npxWaitDelay
	b, err := cmd.CombinedOutput()
	return stripANSI(string(b)), err
}

// skillsAct : add, update ou remove, pour l'utilisateur ou pour un projet. Update et remove ne touchent que ce que le fichier de
// verrouillage de la portée déclare : un skill écrit à la main ou venu d'un plugin n'est jamais modifié par `npx skills`.
func skillsAct(ctx context.Context, action, source, skill, scope, project string) (string, error) {
	var dir, lock, skillsDir, canon string
	switch scope {
	case "user":
		dir = skillsHome()
		lock, skillsDir, canon = personalLockPath(), filepath.Join(cfgDir(), "skills"), filepath.Join(dir, ".agents", "skills")
	case "project":
		if !safeDir(project) {
			return "", terr("srv.plugins.projetRequis")
		}
		dir = projectCwd(project)
		if fi, e := os.Stat(dir); dir == "" || e != nil || !fi.IsDir() {
			return "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
		lock, skillsDir, canon = filepath.Join(dir, "skills-lock.json"), filepath.Join(dir, ".claude", "skills"), filepath.Join(dir, ".agents", "skills")
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
		if _, ok := readLock(lock)[skill]; !ok || !managedByNpx(skillsDir, canon, skill) {
			return "", terr("srv.skills.npxNotLocked")
		}
	}
	if !npxMu.TryLock() {
		return "", terr("srv.skills.npxBusy")
	}
	defer npxMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, npxTimeout)
	defer cancel()
	out, err := runNpxSkills(ctx, dir, args)
	out = strings.TrimSpace(out)
	if errors.Is(err, exec.ErrNotFound) {
		return out, terr("srv.skills.npxMissing")
	}
	return out, err
}

type SkillHit struct {
	Ref      string `json:"ref"`
	Source   string `json:"source"`
	Skill    string `json:"skill"`
	Installs string `json:"installs"`
	URL      string `json:"url"`
}

// parseSkillHits : la sortie de `npx skills find` (ANSI déjà retiré) : « propriétaire/dépôt@skill N installs », puis « └ URL ».
func parseSkillHits(out string) []SkillHit {
	hits := []SkillHit{}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if u, ok := strings.CutPrefix(l, "└"); ok && len(hits) > 0 && hits[len(hits)-1].URL == "" {
			hits[len(hits)-1].URL = strings.TrimSpace(u)
			continue
		}
		f := strings.Fields(l)
		if len(f) != 3 || f[2] != "installs" {
			continue
		}
		i := strings.LastIndex(f[0], "@")
		if i < 1 || !skillRepoRe.MatchString(f[0][:i]) || !skillName.MatchString(f[0][i+1:]) {
			continue
		}
		hits = append(hits, SkillHit{Ref: f[0], Source: f[0][:i], Skill: f[0][i+1:], Installs: f[1]})
	}
	return hits
}

// skillsFind : `npx skills find <q>`. Un q qui commencerait par « - » serait lu comme une option.
func skillsFind(ctx context.Context, q string) ([]SkillHit, error) {
	q = strings.TrimSpace(q)
	if q == "" || len([]rune(q)) > 80 || q[0] == '-' || strings.ContainsAny(q, "\r\n") {
		return nil, terr("srv.http_util.requeteInvalide")
	}
	if !npxMu.TryLock() { // la route n'exige que le droit de lecture : pas de lancements en parallèle
		return nil, terr("srv.skills.npxBusy")
	}
	defer npxMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := runNpxSkills(ctx, skillsHome(), []string{"--yes", skillsPkg, "find", q})
	if errors.Is(err, exec.ErrNotFound) {
		return nil, terr("srv.skills.npxMissing")
	}
	if err != nil {
		return nil, err
	}
	return parseSkillHits(out), nil
}

// remoteTrees : les arbres git d'un dépôt GitHub (chemin → SHA, "" pour la racine). `skillFolderHash` du verrou est le SHA de
// l'arbre du dossier du skill : l'égalité avec celui du dépôt dit si le skill a changé. Sans jeton : 60 requêtes par heure.
var remoteTrees = func(ctx context.Context, owner, repo string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+owner+"/"+repo+"/git/trees/HEAD?recursive=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gld")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var d struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Tree      []struct{ Path, Type, SHA string }
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&d) != nil || d.Truncated {
		return nil, errors.New("arbre illisible ou tronqué")
	}
	out := map[string]string{"": d.SHA}
	for _, e := range d.Tree {
		if e.Type == "tree" {
			out[e.Path] = e.SHA
		}
	}
	return out, nil
}

var githubURLRe = regexp.MustCompile(`^https://github\.com/([\w.-]+)/([\w.-]+?)(\.git)?/?$`)

func lockRepo(e LockEntry) (owner, repo string, ok bool) {
	if e.SourceType != "github" {
		return "", "", false
	}
	if skillRepoRe.MatchString(e.Source) {
		o, r, _ := strings.Cut(e.Source, "/")
		return o, r, true
	}
	if m := githubURLRe.FindStringSubmatch(e.SourceURL); m != nil {
		return m[1], m[2], true
	}
	return "", "", false
}

// skillUpdateState : update, current ou unknown. Jamais « current » sans preuve.
func skillUpdateState(e LockEntry, trees map[string]string) string {
	if e.SourceType != "github" || trees == nil || e.Hash == "" || e.SkillPath == "" {
		return "unknown"
	}
	dir := path.Dir(e.SkillPath)
	if dir == "." {
		dir = ""
	}
	sha, ok := trees[dir]
	switch {
	case !ok:
		return "unknown"
	case sha == e.Hash:
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

const forceMin = 30 * time.Second // /skills-updates?force=1 ne demande que mem_read : sans plancher, il viderait le quota GitHub

// checkSkillUpdates : l'état de chaque skill du verrou personnel et des verrous de projet, clé = celle de listSkills.
// Une requête par dépôt, gardée 6 h (5 min après un échec : limite de débit, hors ligne).
func checkSkillUpdates(ctx context.Context, force bool) map[string]string {
	type item struct {
		key string
		e   LockEntry
	}
	var items []item
	for n, e := range readLock(personalLockPath()) {
		items = append(items, item{"u/" + n, e})
	}
	for _, p := range liveProjects() {
		for n, e := range readLock(filepath.Join(p.Project, "skills-lock.json")) {
			items = append(items, item{"p/" + p.Dir + "/" + n, e})
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
	out := map[string]string{}
	fetched := map[string]bool{}
	for _, it := range items {
		o, r, ok := lockRepo(it.e)
		if !ok {
			out[it.key] = "unknown"
			continue
		}
		k := o + "/" + r
		c, hit := updCache.m[k]
		ttl := 6 * time.Hour
		if c.trees == nil {
			ttl = 5 * time.Minute
		}
		if (force && !fetched[k]) || !hit || time.Since(c.at) > ttl {
			fetched[k] = true
			t, err := remoteTrees(ctx, o, r)
			if err != nil {
				t = nil
			}
			c = updRepo{time.Now(), t}
			updCache.m[k] = c
		}
		out[it.key] = skillUpdateState(it.e, c.trees)
	}
	return out
}

// shadowsSkillsPkg : npx remonte les dossiers parents à la recherche d'un node_modules/skills et le préfère au paquet épinglé.
func shadowsSkillsPkg(dir string) bool {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, "node_modules", "skills")); err == nil {
			return true
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}
