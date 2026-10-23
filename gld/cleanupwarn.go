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
	_, raw := allConfigs()
	if v, ok := lookup(raw["u"], "cleanupPeriodDays"); ok {
		if n, ok := v.(json.Number); ok {
			if f, err := n.Float64(); err == nil && f >= 1 {
				return int(min(f, maxCleanupDays))
			}
		}
	}
	return defaultCleanupDays
}

// expiringSessions : les sessions qui seront supprimées dans moins de cleanupWarn (ou qui sont déjà au-delà de la limite),
// la plus proche d'abord.
func expiringSessions(now time.Time, days int) []Expiring {
	keep := time.Duration(days) * 24 * time.Hour
	var out []Expiring
	for _, g := range sessions() {
		for _, s := range g.Sessions {
			st, err := os.Stat(filepath.Join(root, s.Dir, s.ID+".jsonl")) // Claude Code se fie à la date du fichier
			if err != nil {
				continue
			}
			exp := st.ModTime().Add(keep)
			if exp.Sub(now) <= cleanupWarn {
				out = append(out, Expiring{s.Dir, s.ID, s.Title, s.Cwd, float64(exp.Unix())})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Expires < out[j].Expires })
	return out
}

func routesCleanupWarn(mux *http.ServeMux) {
	mux.HandleFunc("GET /cleanup-warning", func(w http.ResponseWriter, r *http.Request) {
		days := cleanupDays()
		list := expiringSessions(time.Now(), days)
		if len(list) > 200 {
			list = list[:200]
		}
		if list == nil {
			list = []Expiring{}
		}
		writeJSON(w, 200, map[string]any{"days": days, "sessions": list})
	})
}
