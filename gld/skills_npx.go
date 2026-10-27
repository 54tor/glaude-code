package main

// Le CLI `npx skills` (installation, mise à jour, retrait, recherche de skills). Le paquet est épinglé : un `@latest` exécuterait
// du code publié après notre dernière lecture. gld ne réécrit ni le format de verrouillage ni l'installation, il valide les
// arguments, lance la commande et lit le verrou.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const skillsPkg = "skills@1.7.4"

var (
	skillRepoRe = regexp.MustCompile(`^[A-Za-z0-9][\w.-]{0,99}/[A-Za-z0-9][\w.-]{0,99}$`)
	skillURLRe  = regexp.MustCompile(`^https://[\w.-]+(:\d+)?/[\w./~%+-]{1,300}$`)
	ansiRe      = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	npxTimeout  = 5 * time.Minute
	npxMu       sync.Mutex // une seule action à la fois : le verrou est un fichier partagé dont gld ne maîtrise pas le format
)

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
	case "update", "remove":
		return append(base, action, skill, "-y", scope), nil
	}
	return nil, terr("srv.skills.npxBadAction")
}

var runNpxSkills = func(ctx context.Context, dir string, args []string) (string, error) {
	cmd := withPath(exec.CommandContext(ctx, "npx", args...))
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return stripANSI(string(b)), err
}

// skillsAct : add, update ou remove, pour l'utilisateur ou pour un projet. Update et remove ne touchent que ce que le fichier de
// verrouillage de la portée déclare : un skill écrit à la main ou venu d'un plugin n'est jamais modifié par `npx skills`.
func skillsAct(ctx context.Context, action, source, skill, scope, project string) (string, error) {
	var dir, lock string
	switch scope {
	case "user":
		home, _ := os.UserHomeDir()
		dir, lock = home, filepath.Join(home, ".agents", ".skill-lock.json")
	case "project":
		if !safeDir(project) {
			return "", terr("srv.plugins.projetRequis")
		}
		dir = projectCwd(project)
		if fi, e := os.Stat(dir); dir == "" || e != nil || !fi.IsDir() {
			return "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
		lock = filepath.Join(dir, "skills-lock.json")
	default:
		return "", terr("srv.skills.npxBadScope")
	}
	args, err := npxSkillsArgs(action, source, skill, scope == "user")
	if err != nil {
		return "", err
	}
	if action != "add" {
		if _, ok := readLock(lock)[skill]; !ok {
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	home, _ := os.UserHomeDir()
	out, err := runNpxSkills(ctx, home, []string{"--yes", skillsPkg, "find", q})
	if errors.Is(err, exec.ErrNotFound) {
		return nil, terr("srv.skills.npxMissing")
	}
	if err != nil {
		return nil, err
	}
	return parseSkillHits(out), nil
}
