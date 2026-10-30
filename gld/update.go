package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
}

var (
	updMu   sync.Mutex
	updAt   time.Time
	updInfo updateInfo
)

// latestRelease : dernière release publique ; remplaçable en test.
var latestRelease = func(ctx context.Context) (tag, url string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", releaseAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gld")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var d struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&d) != nil {
		return "", "", io.ErrUnexpectedEOF
	}
	return d.Tag, d.URL, nil
}

// newerVersion : vrai si latest (vX.Y.Z) dépasse cur ; « dev » ou une version illisible n'est jamais périmée.
func newerVersion(cur, latest string) bool {
	a, b := verParts(cur), verParts(latest)
	if a == nil || b == nil {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return b[i] > a[i]
		}
	}
	return false
}

func verParts(v string) []int {
	p := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	if len(p) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, s := range p {
		s, _, _ = strings.Cut(s, "-")
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// checkUpdate : l'échec réseau est mis en cache comme un succès (sans « newer ») pour ne pas marteler GitHub (60 requêtes/h sans jeton).
func checkUpdate(ctx context.Context, force bool) updateInfo {
	updMu.Lock()
	defer updMu.Unlock()
	ttl := 6 * time.Hour
	if force {
		ttl = 30 * time.Second // plancher : un clic répété ne doit pas vider le quota GitHub
	}
	if !updAt.IsZero() && time.Since(updAt) < ttl {
		return updInfo
	}
	info := updateInfo{Current: version}
	if tag, url, err := latestRelease(ctx); err == nil {
		info.Latest, info.URL, info.Newer = tag, url, newerVersion(version, tag)
	}
	updAt, updInfo = time.Now(), info
	return info
}

func routesUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, checkUpdate(r.Context(), r.URL.Query().Get("force") == "1"))
	})
}
