package main

// Pousser la branche courante d'un dossier de projet vers l'un des remotes du dépôt. Jamais de --force, jamais d'invite :
// un identifiant demandé fait échouer le push avec le message de git au lieu de bloquer.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type GitRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"` // sans identifiants
}

// maskURL retire les identifiants d'une URL avant de l'afficher. En http(s), tout le userinfo part : un jeton s'y écrit aussi
// bien à la place du nom (https://<jeton>@github.com/…) qu'à celle du mot de passe. En ssh, seul le mot de passe part.
func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		u.User = nil
	} else if _, has := u.User.Password(); has {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

func gitOut(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir}, args...)...)
	cmd.Env = append(childEnv(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		if c, _ := gitOut1(dir, "config", "core.sshCommand"); c == "" {
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes") // pas de demande de phrase de passe sur le terminal de gld
		}
	}
	b, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func gitOut1(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

// gitRemotes : les remotes du dépôt, avec l'adresse de poussée (`git remote -v`, lignes « (push) »).
func gitRemotes(dir string) []GitRemote {
	out, _ := gitOut1(dir, "remote", "-v")
	var rs []GitRemote
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 3 && f[2] == "(push)" {
			rs = append(rs, GitRemote{f[0], maskURL(f[1])})
		}
	}
	return rs
}

var pushMu sync.Mutex // un push à la fois : deux pushes du même dépôt se marcheraient dessus

// gitPush pousse la branche courante vers remote (qui doit exister dans le dépôt). Sans amont, la branche est liée au remote (-u).
func gitPush(dir, remote string) (string, error) {
	known := false
	for _, r := range gitRemotes(dir) {
		known = known || r.Name == remote
	}
	if !known {
		return L("srv.gitpush.unknownRemote", "A1", remote), os.ErrInvalid
	}
	if b, _ := gitOut1(dir, "symbolic-ref", "--short", "-q", "HEAD"); b == "" {
		return L("srv.gitpush.detached"), os.ErrInvalid
	}
	args := []string{"push"}
	if up, _ := gitOut1(dir, "rev-parse", "--abbrev-ref", "@{u}"); up == "" {
		args = append(args, "-u")
	}
	pushMu.Lock()
	defer pushMu.Unlock()
	out, err := gitOut(dir, append(args, "--", remote, "HEAD")...)
	gitMu.Lock()
	delete(gitCache, dir) // l'avance sur l'amont a changé : relu au prochain passage
	gitMu.Unlock()
	return out, err
}

func routesGitPush(mux *http.ServeMux) {
	mux.HandleFunc("GET /git-remotes", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		rs := []GitRemote{}
		if knownRepo(dir) {
			rs = append(rs, gitRemotes(dir)...)
		}
		writeJSON(w, 200, map[string]any{"remotes": rs})
	})
	mux.HandleFunc("POST /git-push", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, Remote string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil || !knownRepo(req.Dir) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.routes_sessions.dossierInvalide")})
			return
		}
		out, err := gitPush(req.Dir, req.Remote)
		if out == "" && err == nil {
			out = L("srv.gitpush.pushed")
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
	})
}
