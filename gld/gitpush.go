package main

// Pousser la branche courante d'un dossier de projet vers l'un des remotes du dépôt. Jamais de --force, jamais d'invite :
// un identifiant demandé fait échouer le push avec le message de git au lieu de bloquer.

import (
	"cmp"
	"context"
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
	if err != nil {
		return stripUserinfo(raw)
	}
	if u.User == nil {
		return raw
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		u.User = nil
	} else if _, has := u.User.Password(); has {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

// stripUserinfo : repli quand net/url refuse l'adresse (git accepte des mots de passe que Go rejette) : tout ce qui
// précède le dernier « @ » de l'autorité est retiré.
func stripUserinfo(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return raw // forme scp (git@hote:chemin) : pas de mot de passe possible
	}
	rest := raw[i+3:]
	auth := rest
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		auth = rest[:j]
	}
	if k := strings.LastIndexByte(auth, '@'); k >= 0 {
		return raw[:i+3] + rest[k+1:]
	}
	return raw
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
	applyRunAs(cmd)
	b, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func gitOut1(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.WaitDelay = time.Second
	applyRunAs(cmd)
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
	mux.HandleFunc("GET /v1/git/remotes", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		rs := []GitRemote{}
		if knownRepo(dir) {
			rs = append(rs, gitRemotes(dir)...)
		}
		writeJSON(w, 200, map[string]any{"remotes": rs})
	})
	mux.HandleFunc("POST /v1/git/pushes", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, Remote string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if !knownRepo(req.Dir) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.dossierInvalide"))
			return
		}
		out, err := gitPush(req.Dir, req.Remote)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "push_failed", cmp.Or(out, err.Error()))
			return
		}
		writeJSON(w, 201, map[string]any{"message": cmp.Or(out, L("srv.gitpush.pushed"))})
	})
}
