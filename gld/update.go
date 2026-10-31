package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const releaseAPI = "https://api.github.com/repos/54tor/glaude-code/releases/latest"

type updateInfo struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	URL     string `json:"url"`
	Newer   bool   `json:"newer"`
	// Claude Code installé et dernier publié sur npm ; ClaudeNewer : une version plus récente existe.
	ClaudeCurrent string `json:"claudeCurrent,omitempty"`
	ClaudeLatest  string `json:"claudeLatest,omitempty"`
	ClaudeNewer   bool   `json:"claudeNewer,omitempty"`
}

var (
	updMu   sync.Mutex
	updAt   time.Time
	updInfo updateInfo
)

// latestRelease : dernière release publique ; remplaçable en test.
var latestRelease = func(requestContext context.Context) (tag, url string, err error) {
	requestContext, cancel := context.WithTimeout(requestContext, 10*time.Second)
	defer cancel()
	releaseRequest, err := http.NewRequestWithContext(requestContext, "GET", releaseAPI, nil)
	if err != nil {
		return "", "", err
	}
	releaseRequest.Header.Set("Accept", "application/vnd.github+json")
	releaseRequest.Header.Set("User-Agent", "gld")
	releaseResponse, err := http.DefaultClient.Do(releaseRequest)
	if err != nil {
		return "", "", err
	}
	defer releaseResponse.Body.Close()
	var release struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if releaseResponse.StatusCode != 200 || json.NewDecoder(io.LimitReader(releaseResponse.Body, 1<<20)).Decode(&release) != nil {
		return "", "", io.ErrUnexpectedEOF
	}
	return release.Tag, release.URL, nil
}

const claudeNpmAPI = "https://registry.npmjs.org/@anthropic-ai/claude-code/latest"

// claudeInstalled : la version de claude (« 2.1.314 (Claude Code) » devient « 2.1.314 ») ; remplaçable en test.
var claudeInstalled = func(commandContext context.Context) string {
	commandContext, cancel := context.WithTimeout(commandContext, 10*time.Second)
	defer cancel()
	versionOutput, err := withPath(exec.CommandContext(commandContext, "claude", "--version")).Output()
	if err != nil {
		return ""
	}
	versionNumber, _, _ := strings.Cut(strings.TrimSpace(string(versionOutput)), " ")
	return versionNumber
}

// claudeLatest : dernière version de Claude Code publiée sur npm ; remplaçable en test.
var claudeLatest = func(requestContext context.Context) (string, error) {
	requestContext, cancel := context.WithTimeout(requestContext, 10*time.Second)
	defer cancel()
	registryRequest, err := http.NewRequestWithContext(requestContext, "GET", claudeNpmAPI, nil)
	if err != nil {
		return "", err
	}
	registryRequest.Header.Set("User-Agent", "gld")
	registryResponse, err := http.DefaultClient.Do(registryRequest)
	if err != nil {
		return "", err
	}
	defer registryResponse.Body.Close()
	var packageInfo struct {
		Version string `json:"version"`
	}
	if registryResponse.StatusCode != 200 || json.NewDecoder(io.LimitReader(registryResponse.Body, 1<<20)).Decode(&packageInfo) != nil {
		return "", io.ErrUnexpectedEOF
	}
	return packageInfo.Version, nil
}

// newerVersion : vrai si latest (vX.Y.Z) dépasse current ; « dev » ou une version illisible n'est jamais périmée.
func newerVersion(current, latest string) bool {
	currentParts, latestParts := verParts(current), verParts(latest)
	if currentParts == nil || latestParts == nil {
		return false
	}
	for partIndex := range currentParts {
		if currentParts[partIndex] != latestParts[partIndex] {
			return latestParts[partIndex] > currentParts[partIndex]
		}
	}
	return false
}

const claudePatchTolerance = 3

// claudeFarBehind : l'installeur natif de Claude Code suit npm avec du retard, donc seul un écart de mineure ou d'au moins claudePatchTolerance patchs est signalé.
func claudeFarBehind(current, latest string) bool {
	if !newerVersion(current, latest) {
		return false
	}
	currentParts, latestParts := verParts(current), verParts(latest)
	if currentParts[0] != latestParts[0] || currentParts[1] != latestParts[1] {
		return true
	}
	return latestParts[2]-currentParts[2] >= claudePatchTolerance
}

func verParts(versionText string) []int {
	parts := strings.SplitN(strings.TrimPrefix(versionText, "v"), ".", 3)
	if len(parts) != 3 {
		return nil
	}
	numbers := make([]int, 3)
	for partIndex, part := range parts {
		part, _, _ = strings.Cut(part, "-")
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		numbers[partIndex] = number
	}
	return numbers
}

// checkUpdate : l'échec réseau est mis en cache comme un succès (sans « newer ») pour ne pas marteler GitHub (60 requêtes/h sans jeton).
func checkUpdate(requestContext context.Context, force bool) updateInfo {
	updMu.Lock()
	defer updMu.Unlock()
	ttl := 6 * time.Hour
	if force {
		ttl = 30 * time.Second // plancher : un clic répété ne doit pas vider le quota GitHub
	}
	if !updAt.IsZero() && time.Since(updAt) < ttl {
		return updInfo
	}
	checkedInfo := updateInfo{Current: version}
	if tag, url, err := latestRelease(requestContext); err == nil {
		checkedInfo.Latest, checkedInfo.URL, checkedInfo.Newer = tag, url, newerVersion(version, tag)
	}
	if installedClaude := claudeInstalled(requestContext); installedClaude != "" {
		checkedInfo.ClaudeCurrent = installedClaude
		if latestClaude, err := claudeLatest(requestContext); err == nil {
			checkedInfo.ClaudeLatest, checkedInfo.ClaudeNewer = latestClaude, claudeFarBehind(installedClaude, latestClaude)
		}
	}
	updAt, updInfo = time.Now(), checkedInfo
	return checkedInfo
}

func routesUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/update", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, checkUpdate(request.Context(), request.URL.Query().Get("force") == "1"))
	})
}
