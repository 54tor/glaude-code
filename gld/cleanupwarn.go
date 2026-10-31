package main

// Claude Code supprime au démarrage les transcripts plus vieux que `cleanupPeriodDays` (30 jours par défaut, réglage de
// ~/.claude/settings.json). Cette route prévient quand des sessions vont y passer dans les 24 prochaines heures.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	maxCleanupDays     = 100000 // au-delà, le calcul en durée déborderait ; 100 000 jours valent « jamais »
	defaultCleanupDays = 30
	cleanupWarn        = 24 * time.Hour
	maxExpiringListed  = 200
)

type Expiring struct {
	Dir     string  `json:"dir"`
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Cwd     string  `json:"cwd"`
	Expires float64 `json:"expires"` // secondes Unix de la suppression prévue
}

// cleanupDays : la durée de conservation de Claude Code, en jours.
func cleanupDays() int {
	_, doc := readConfig("u") // le réglage est utilisateur seul : inutile de lire les projets et leur mémoire
	value, found := lookup(doc, "cleanupPeriodDays")
	if !found {
		return defaultCleanupDays
	}
	number, isNumber := value.(json.Number)
	if !isNumber {
		return defaultCleanupDays
	}
	configuredDays, err := number.Float64()
	if err != nil || configuredDays < 1 {
		return defaultCleanupDays
	}
	return int(min(configuredDays, maxCleanupDays))
}

// expiringSessions : les sessions qui seront supprimées dans moins de cleanupWarn (ou qui sont déjà au-delà de la limite),
// la plus proche d'abord.
func expiringSessions(now time.Time, days int) []Expiring {
	retention := time.Duration(days) * dayDuration
	var expiring []Expiring
	for _, group := range sessions() {
		for _, session := range group.Sessions {
			fileInfo, err := os.Stat(filepath.Join(root, session.Dir, session.ID+".jsonl")) // Claude Code se fie à la date du fichier
			if err != nil {
				continue
			}
			expiresAt := fileInfo.ModTime().Add(retention)
			if expiresAt.Sub(now) <= cleanupWarn {
				expiring = append(expiring, Expiring{session.Dir, session.ID, session.Title, session.Cwd, float64(expiresAt.Unix())})
			}
		}
	}
	sort.Slice(expiring, func(left, right int) bool {
		return expiring[left].Expires < expiring[right].Expires
	})
	return expiring
}

func routesCleanupWarn(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/cleanup-warning", func(responseWriter http.ResponseWriter, request *http.Request) {
		days := cleanupDays()
		list := expiringSessions(time.Now(), days)
		if len(list) > maxExpiringListed {
			list = list[:maxExpiringListed]
		}
		if list == nil {
			list = []Expiring{}
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"days": days, "sessions": list})
	})
}
