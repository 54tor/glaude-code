package main

// État git des dossiers de projet : branche, retard et avance sur l'amont, nombre de fichiers modifiés. Lecture seule, sans verrou
// (--no-optional-locks), via `git status --porcelain=v2` : le format stable prévu pour les programmes. Les résultats sont gardés
// quelques secondes ; l'interface les relit d'elle-même, sans bouton.

import (
	"context"
	"net/http"
	"os/exec"
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
	st GitState
	ok bool // false : pas un dépôt (ou git a échoué)
	at time.Time
}

// parseGitStatus lit la sortie de `git status --porcelain=v2 --branch`.
func parseGitStatus(out string) GitState {
	var s GitState
	for _, l := range strings.Split(out, "\n") {
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
		case strings.HasPrefix(l, "1 "), strings.HasPrefix(l, "2 "), strings.HasPrefix(l, "u "):
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
	cmd := exec.CommandContext(ctx, "git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir, "status", "--porcelain=v2", "--branch")
	cmd.Env = append(childEnv(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	b, err := cmd.Output()
	return string(b), err
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
				e.st, e.ok = parseGitStatus(out), true
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

func routesGit(mux *http.ServeMux) {
	mux.HandleFunc("GET /git-status", func(w http.ResponseWriter, r *http.Request) {
		var dirs []string
		for _, g := range sessions() {
			if !g.Missing && g.Project != "" {
				dirs = append(dirs, g.Project)
			}
		}
		writeJSON(w, 200, map[string]any{"status": gitStates(dirs)})
	})
}
