package main

// Pousser la branche courante d'un dossier de projet vers l'un des remotes du dépôt. Jamais de --force, jamais d'invite :
// un identifiant demandé fait échouer le push avec le message de git au lieu de bloquer.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	schemeSeparator = "://"
	gitPushTimeout  = 15 * time.Minute // un hook pre-push peut lancer un build ou des tests
	gitPushBodyMax  = 1 << 10
	ndjsonMediaType = "application/x-ndjson"
	exitCodeUnknown = -1
)

type GitRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"` // sans identifiants
}

// maskURL retire les identifiants d'une URL avant de l'afficher. En http(s), tout le userinfo part : un jeton s'y écrit aussi
// bien à la place du nom (https://<jeton>@github.com/…) qu'à celle du mot de passe. En ssh, seul le mot de passe part.
func maskURL(raw string) string {
	parsedURL, err := url.Parse(raw)
	if err != nil {
		return stripUserinfo(raw)
	}
	if parsedURL.User == nil {
		return raw
	}
	if parsedURL.Scheme == "http" || parsedURL.Scheme == "https" {
		parsedURL.User = nil
	} else if _, hasPassword := parsedURL.User.Password(); hasPassword {
		parsedURL.User = url.User(parsedURL.User.Username())
	}
	return parsedURL.String()
}

// stripUserinfo : repli quand net/url refuse l'adresse (git accepte des mots de passe que Go rejette) : tout ce qui
// précède le dernier « @ » de l'autorité est retiré.
func stripUserinfo(raw string) string {
	schemeIndex := strings.Index(raw, schemeSeparator)
	if schemeIndex < 0 {
		return raw // forme scp (git@hote:chemin) : pas de mot de passe possible
	}
	authorityStart := schemeIndex + len(schemeSeparator)
	rest := raw[authorityStart:]
	authority := rest
	if slashIndex := strings.IndexByte(rest, '/'); slashIndex >= 0 {
		authority = rest[:slashIndex]
	}
	if atIndex := strings.LastIndexByte(authority, '@'); atIndex >= 0 {
		return raw[:authorityStart] + rest[atIndex+1:]
	}
	return raw
}

// gitStreamTo exécute git dans dir ; stdout et stderr vont vers output, au fil de l'eau.
func gitStreamTo(dir string, output io.Writer, args ...string) error {
	commandContext, cancel := context.WithTimeout(context.Background(), gitPushTimeout)
	defer cancel()
	gitArgs := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.CommandContext(commandContext, "git", gitArgs...) // #nosec G702 -- exécutable fixe, arguments en tableau, sans shell
	cmd.WaitDelay = time.Second
	cmd.Env = append(childEnv(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		if sshCommand, _ := gitOut1(dir, "config", "core.sshCommand"); sshCommand == "" {
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes") // pas de demande de phrase de passe sur le terminal de gld
		}
	}
	applyRunAs(cmd)
	cmd.Stdout = output
	cmd.Stderr = output
	return cmd.Run()
}

func gitOut1(dir string, args ...string) (string, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "git", append([]string{"-C", dir}, args...)...) // #nosec G702 -- exécutable fixe, arguments en tableau, sans shell
	cmd.WaitDelay = time.Second
	cmd.Env = environNoAuth()
	applyRunAs(cmd)
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

// gitRemotes : les remotes du dépôt, avec l'adresse de poussée (`git remote -v`, lignes « (push) »).
func gitRemotes(dir string) []GitRemote {
	remoteOutput, _ := gitOut1(dir, "remote", "-v")
	var remotes []GitRemote
	for _, remoteLine := range strings.Split(remoteOutput, "\n") {
		fields := strings.Fields(remoteLine)
		isPushLine := len(fields) == 3 && fields[2] == "(push)"
		if isPushLine {
			remotes = append(remotes, GitRemote{fields[0], maskURL(fields[1])})
		}
	}
	return remotes
}

var pushMu sync.Mutex // un push à la fois : deux pushes du même dépôt se marcheraient dessus

// gitPush pousse la branche courante vers remote et rend la sortie complète de git (hooks compris).
func gitPush(dir, remote string) (string, error) {
	var pushOutput bytes.Buffer
	err := gitPushTo(dir, remote, &pushOutput)
	return strings.TrimSpace(pushOutput.String()), err
}

// gitPushTo pousse la branche courante vers remote (qui doit exister dans le dépôt) et écrit la sortie au fil de l'eau dans output.
// Sans amont, la branche est liée au remote (-u).
func gitPushTo(dir, remote string, output io.Writer) error {
	isKnownRemote := slices.ContainsFunc(gitRemotes(dir), func(listedRemote GitRemote) bool {
		return listedRemote.Name == remote
	})
	if !isKnownRemote {
		io.WriteString(output, L("srv.gitpush.unknownRemote", "A1", remote))
		return os.ErrInvalid
	}
	if branch, _ := gitOut1(dir, "symbolic-ref", "--short", "-q", "HEAD"); branch == "" {
		io.WriteString(output, L("srv.gitpush.detached"))
		return os.ErrInvalid
	}
	args := []string{"push"}
	if upstream, _ := gitOut1(dir, "rev-parse", "--abbrev-ref", "@{u}"); upstream == "" {
		args = append(args, "-u")
	}
	pushMu.Lock()
	defer pushMu.Unlock()
	err := gitStreamTo(dir, output, append(args, "--", remote, "HEAD")...)
	gitMu.Lock()
	delete(gitCache, dir) // l'avance sur l'amont a changé : relu au prochain passage
	gitMu.Unlock()
	return err
}

// exitCodeOf : le code de sortie de git, exitCodeUnknown si le processus n'a pas pu démarrer ou a été interrompu.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return exitErr.ExitCode()
	}
	return exitCodeUnknown
}

// ndjsonFrame : une ligne du flux de poussée. out porte des octets bruts (base64 en JSON) pour ne jamais couper un caractère UTF-8 ;
// exit n'apparaît qu'une fois, en dernière ligne.
type ndjsonFrame struct {
	Out  []byte `json:"out,omitempty"`
	Exit *int   `json:"exit,omitempty"`
}

// ndjsonWriter envoie chaque écriture de git au client sans attendre la fin du push.
type ndjsonWriter struct {
	mu         sync.Mutex // la sortie de git et le signal de vie écrivent en même temps
	encoder    *json.Encoder
	controller *http.ResponseController
}

func (frameWriter *ndjsonWriter) send(frame ndjsonFrame) error {
	frameWriter.mu.Lock()
	defer frameWriter.mu.Unlock()
	if err := frameWriter.encoder.Encode(frame); err != nil {
		return err
	}
	return frameWriter.controller.Flush()
}

func (frameWriter *ndjsonWriter) Write(chunk []byte) (int, error) {
	if err := frameWriter.send(ndjsonFrame{Out: chunk}); err != nil {
		return 0, err
	}
	return len(chunk), nil
}

// heartbeatInterval : une ligne vide ({}) à ce rythme pendant un hook silencieux, pour qu'un proxy ou un tunnel ne coupe pas le flux.
var heartbeatInterval = 15 * time.Second

func (frameWriter *ndjsonWriter) beat(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			frameWriter.send(ndjsonFrame{})
		}
	}
}

func streamGitPush(responseWriter http.ResponseWriter, dir, remote string) {
	headers := responseWriter.Header()
	headers.Set("Content-Type", ndjsonMediaType)
	headers.Set("Cache-Control", "no-store")
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Accel-Buffering", "no")
	responseWriter.WriteHeader(http.StatusOK)
	frameWriter := &ndjsonWriter{encoder: json.NewEncoder(responseWriter), controller: http.NewResponseController(responseWriter)}
	stopBeat := make(chan struct{})
	go frameWriter.beat(heartbeatInterval, stopBeat)
	exitCode := exitCodeOf(gitPushTo(dir, remote, frameWriter))
	close(stopBeat)
	frameWriter.send(ndjsonFrame{Exit: &exitCode})
}

func routesGitPush(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/git/remotes", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		remotes := []GitRemote{}
		if knownRepo(dir) {
			remotes = append(remotes, gitRemotes(dir)...)
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"remotes": remotes})
	})
	mux.HandleFunc("POST /v1/git/pushes", func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload struct{ Dir, Remote string }
		if !decodeBody(responseWriter, request, &payload, gitPushBodyMax) {
			return
		}
		if !knownRepo(payload.Dir) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.dossierInvalide"))
			return
		}
		if request.Header.Get("Accept") == ndjsonMediaType {
			streamGitPush(responseWriter, payload.Dir, payload.Remote)
			return
		}
		pushOutput, err := gitPush(payload.Dir, payload.Remote)
		if err != nil {
			writeErr(responseWriter, http.StatusBadGateway, "push_failed", cmp.Or(pushOutput, err.Error()))
			return
		}
		writeJSON(responseWriter, http.StatusCreated, map[string]any{"message": cmp.Or(pushOutput, L("srv.gitpush.pushed"))})
	})
}
