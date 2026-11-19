package main

// Poussées durables : chaque poussée tourne dans sa propre session tmux détachée (« gld _push <id> »), avec un vrai terminal pour
// git et ses hooks. gld ne fait que la lancer puis relire son état sur disque : un redémarrage de gld ne l'interrompt pas, et son
// état reste déterminé (status.json écrit par la session, session tmux disparue sans « done » = interrompue).

import (
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"
)

const (
	pushRunsDirName    = "git-pushes"
	pushRunStatusFile  = "status.json"
	pushRunLogFile     = "out.log"
	pushRunOutputChunk = 256 << 10
	pushRunBodyMax     = 1 << 10
	pushRunPaneCols    = "160"
	pushRunPaneRows    = "48"
	pushRunStatusMax   = 64 << 10

	pushStatePending   = "pending"
	pushStateRunning   = "running"
	pushStateSucceeded = "succeeded"
	pushStateFailed    = "failed"
)

var pushRunIDRe = regexp.MustCompile(`^[0-9a-z]{8,32}$`)

type PushRemoteState struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Exit  *int   `json:"exit,omitempty"`
}

type PushRunStatus struct {
	ID          string            `json:"id"`
	Dir         string            `json:"dir"`
	Branch      string            `json:"branch"`
	Remotes     []PushRemoteState `json:"remotes"`
	Tags        []string          `json:"tags,omitempty"`
	ForceTags   bool              `json:"forceTags,omitempty"`
	Done        bool              `json:"done"`
	Interrupted bool              `json:"interrupted,omitempty"` // calculé à la lecture, jamais écrit
	Started     int64             `json:"started"`
}

func pushRunsRoot() string               { return filepath.Join(cfgDir(), pushRunsDirName) }
func pushRunDir(id string) string        { return filepath.Join(pushRunsRoot(), id) }
func pushRunFile(id, name string) string { return filepath.Join(pushRunDir(id), name) }
func pushSessionName(id string) string   { return "gld-push-" + id }

func newPushRunID() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 36) + strconv.FormatUint(rand.Uint64N(1<<20)+1<<20, 36) // #nosec G404 -- identifiant de dossier, sans enjeu de secret
}

func pushSessionAlive(id string) bool {
	_, err := tmuxRun("", "has-session", "-t", "="+pushSessionName(id))
	return err == nil
}

func readPushStatus(id string) (PushRunStatus, error) {
	var status PushRunStatus
	statusFile, err := openTrusted(pushRunFile(id, pushRunStatusFile))
	if err != nil {
		return status, err
	}
	defer statusFile.Close()
	content, err := io.ReadAll(io.LimitReader(statusFile, pushRunStatusMax+1))
	if err != nil {
		return status, err
	}
	if len(content) > pushRunStatusMax {
		return status, errors.New("status too large")
	}
	return status, json.Unmarshal(content, &status)
}

func writePushStatus(status PushRunStatus) error {
	content, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return rWriteAtomic(cfgDir(), pushRunFile(status.ID, pushRunStatusFile), content, 0o600)
}

// loadPushRun : l'état d'une poussée, avec « interrompue » si sa session a disparu avant la fin.
func loadPushRun(id string) (PushRunStatus, error) {
	status, err := readPushStatus(id)
	if err != nil {
		return status, err
	}
	status.Interrupted = !status.Done && !pushSessionAlive(id)
	return status, nil
}

func listPushRuns() []PushRunStatus {
	runs := []PushRunStatus{}
	entries, err := readDirTrusted(pushRunsRoot())
	if err != nil {
		return runs
	}
	for _, entry := range entries {
		if !entry.IsDir() || !pushRunIDRe.MatchString(entry.Name()) {
			continue
		}
		if status, loadErr := loadPushRun(entry.Name()); loadErr == nil {
			runs = append(runs, status)
		}
	}
	slices.SortFunc(runs, func(first, second PushRunStatus) int { return int(first.Started - second.Started) })
	return runs
}

func isPushRunActive(status PushRunStatus) bool { return !status.Done && !status.Interrupted }

func hasActivePushRun(dir string) bool {
	return slices.ContainsFunc(listPushRuns(), func(status PushRunStatus) bool { return status.Dir == dir && isPushRunActive(status) })
}

var errPushNoTmux = terr("srv.gitpushrun.noTmux")

// startPushRun écrit l'état initial puis lance la session tmux qui pousse vers chaque remote, l'un après l'autre.
func startPushRun(dir string, remotes, tags []string, forceTags bool) (PushRunStatus, error) {
	branch, _ := gitOut1(dir, "symbolic-ref", "--short", "-q", "HEAD")
	if !onPath("tmux") {
		return PushRunStatus{}, errPushNoTmux
	}
	status := PushRunStatus{ID: newPushRunID(), Dir: dir, Branch: branch, Tags: tags, ForceTags: forceTags, Started: time.Now().UnixMilli()}
	for _, remote := range remotes {
		status.Remotes = append(status.Remotes, PushRemoteState{Name: remote, State: pushStatePending})
	}
	if err := mkdirTrusted(pushRunDir(status.ID), 0o700); err != nil {
		return status, err
	}
	if err := writePushStatus(status); err != nil {
		return status, err
	}
	self, err := os.Executable()
	if err == nil {
		_, err = tmuxRun("", "new-session", "-d", "-x", pushRunPaneCols, "-y", pushRunPaneRows, "-s", pushSessionName(status.ID), shJoin([]string{self, "_push", status.ID}))
	}
	if err != nil {
		rRemoveAll(cfgDir(), pushRunDir(status.ID))
		return status, err
	}
	return status, nil
}

// readPushOutput : au plus pushRunOutputChunk octets du journal à partir de offset, et le décalage suivant.
func readPushOutput(id string, offset int64) ([]byte, int64) {
	logFile, err := openTrusted(pushRunFile(id, pushRunLogFile))
	if err != nil {
		return []byte{}, offset
	}
	defer logFile.Close()
	if _, err := logFile.Seek(offset, io.SeekStart); err != nil {
		return []byte{}, offset
	}
	chunk, _ := io.ReadAll(io.LimitReader(logFile, pushRunOutputChunk))
	return chunk, offset + int64(len(chunk))
}

func routesGitPushRuns(mux *http.ServeMux) {
	requireKnownRun := func(responseWriter http.ResponseWriter, request *http.Request) (PushRunStatus, bool) {
		id := request.PathValue("id")
		if !pushRunIDRe.MatchString(id) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_id", L("srv.http_util.idInvalide"))
			return PushRunStatus{}, false
		}
		status, err := loadPushRun(id)
		if err != nil || !knownRepo(status.Dir) {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
			return status, false
		}
		return status, true
	}
	mux.HandleFunc("POST /v1/git/push-runs", func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload struct {
			Dir       string
			Remotes   []string
			Tags      []string
			ForceTags bool
		}
		if !decodeBody(responseWriter, request, &payload, pushRunBodyMax) {
			return
		}
		if !knownRepo(payload.Dir) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.dossierInvalide"))
			return
		}
		knownRemotes := gitRemoteNames(payload.Dir)
		isValidSelection := len(payload.Remotes) > 0 && !slices.ContainsFunc(payload.Remotes, func(name string) bool { return !slices.Contains(knownRemotes, name) })
		if !isValidSelection {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.gitpushrun.badRemotes"))
			return
		}
		knownTags := gitTagNames(payload.Dir)
		if slices.ContainsFunc(payload.Tags, func(name string) bool { return !slices.Contains(knownTags, name) }) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.gitpushrun.badTags"))
			return
		}
		if branch, _ := gitOut1(payload.Dir, "symbolic-ref", "--short", "-q", "HEAD"); branch == "" {
			writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.gitpush.detached"))
			return
		}
		if hasActivePushRun(payload.Dir) {
			writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.gitpushrun.alreadyRunning"))
			return
		}
		status, err := startPushRun(payload.Dir, payload.Remotes, payload.Tags, payload.ForceTags)
		if errors.Is(err, errPushNoTmux) {
			writeErr(responseWriter, http.StatusNotImplemented, "not_implemented", err.Error())
			return
		}
		if err != nil {
			writeErr(responseWriter, http.StatusBadGateway, "push_failed", err.Error())
			return
		}
		writeJSON(responseWriter, http.StatusCreated, map[string]any{"run": status})
	})
	mux.HandleFunc("GET /v1/git/push-runs", func(responseWriter http.ResponseWriter, request *http.Request) {
		runs := slices.DeleteFunc(listPushRuns(), func(status PushRunStatus) bool { return !knownRepo(status.Dir) })
		writeJSON(responseWriter, http.StatusOK, map[string]any{"runs": runs})
	})
	mux.HandleFunc("GET /v1/git/push-runs/{id}/output", func(responseWriter http.ResponseWriter, request *http.Request) {
		status, isKnown := requireKnownRun(responseWriter, request)
		if !isKnown {
			return
		}
		offset, _ := strconv.ParseInt(request.URL.Query().Get("offset"), 10, 64)
		chunk, next := readPushOutput(status.ID, max(offset, 0))
		if status.Done {
			gitMu.Lock()
			delete(gitCache, status.Dir) // l'avance sur les remotes a changé : relue au prochain passage
			gitMu.Unlock()
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"out": chunk, "next": next, "run": status})
	})
	mux.HandleFunc("DELETE /v1/git/push-runs/{id}", func(responseWriter http.ResponseWriter, request *http.Request) {
		status, isKnown := requireKnownRun(responseWriter, request)
		if !isKnown {
			return
		}
		if isPushRunActive(status) {
			writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.gitpushrun.stillRunning"))
			return
		}
		if err := rRemoveAll(cfgDir(), pushRunDir(status.ID)); err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
}
