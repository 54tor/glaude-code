package main

// Aperçu des commits que la branche courante a en plus sur chaque remote, avant de pousser.

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const (
	unpushedCommitsMax = 50
	logFieldSeparator  = "\x1f"
)

type UnpushedCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
}

type RemoteUnpushed struct {
	Name    string           `json:"name"`
	Tracked bool             `json:"tracked"` // remote suivi par la branche courante : coché par défaut dans le dialogue
	Total   int              `json:"total"`
	Commits []UnpushedCommit `json:"commits"`
}

// parseUnpushedLog lit `git log --format=%h<US>%s<US>%cI` : un commit par ligne.
func parseUnpushedLog(logOutput string) []UnpushedCommit {
	commits := []UnpushedCommit{}
	for _, logLine := range strings.Split(logOutput, "\n") {
		fields := strings.Split(logLine, logFieldSeparator)
		if len(fields) != 3 {
			continue
		}
		commits = append(commits, UnpushedCommit{Hash: fields[0], Subject: fields[1], Date: fields[2]})
	}
	return commits
}

// gitUnpushed liste, par remote, les commits de HEAD absents de toutes ses références suivies (refs/remotes/<remote>/*).
// Un remote sans la branche voit donc ses commits manquants plutôt qu'une liste vide.
func gitUnpushed(dir string) []RemoteUnpushed {
	remoteNames := gitRemoteNames(dir)
	branch, _ := gitOut1(dir, "symbolic-ref", "--short", "-q", "HEAD")
	trackedRemote := ""
	if branch != "" {
		trackedRemote, _ = gitOut1(dir, "config", "--get", "branch."+branch+".remote")
	}
	unpushed := make([]RemoteUnpushed, 0, len(remoteNames))
	for _, remoteName := range remoteNames {
		notOnRemote := []string{"HEAD", "--not", "--remotes=" + remoteName}
		countOutput, _ := gitOut1(dir, append([]string{"rev-list", "--count"}, notOnRemote...)...)
		total, _ := strconv.Atoi(countOutput)
		logArgs := append([]string{"log", "-n", strconv.Itoa(unpushedCommitsMax), "--format=%h" + logFieldSeparator + "%s" + logFieldSeparator + "%cI"}, notOnRemote...)
		logOutput, _ := gitOut1(dir, logArgs...)
		unpushed = append(unpushed, RemoteUnpushed{Name: remoteName, Tracked: remoteName == trackedRemote, Total: total, Commits: parseUnpushedLog(logOutput)})
	}
	return unpushed
}

func gitRemoteNames(dir string) []string {
	remoteNames := []string{}
	for _, remote := range gitRemotes(dir) {
		if !slices.Contains(remoteNames, remote.Name) {
			remoteNames = append(remoteNames, remote.Name)
		}
	}
	return remoteNames
}

func routesGitUnpushed(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/git/unpushed", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		if !knownRepo(dir) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.dossierInvalide"))
			return
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"remotes": gitUnpushed(dir)})
	})
}
