// Sauvegarde des sessions ouvertes (« onglets ») pour les rouvrir d'un coup, après un redémarrage par exemple.
// Fichier ~/.claude/gld-web-tabs.json, lu aussi par `gld-matin --resume`.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type TabSession struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`
	Project string `json:"project"` // dossier de projet du transcript (celui que gld-matin attend)
	Title   string `json:"title"`
}

type TabSave struct {
	Name     string       `json:"name"` // vide pour une sauvegarde automatique sans nom
	Auto     bool         `json:"auto"`
	At       string       `json:"at"` // RFC 3339 à la nanoseconde, sert aussi d'identifiant
	Sessions []TabSession `json:"sessions"`
}

// Sauvegardes automatiques conservées : celles de la dernière heure d'activité, comptée depuis la plus récente
// (pas depuis maintenant : après une nuit ordinateur éteint, l'état d'avant l'arrêt doit rester). Les manuelles restent toutes.
const keepAuto = time.Hour

var tabsMu sync.Mutex

func tabsFile() string { return filepath.Join(cfgDir(), "gld-web-tabs.json") }

// openSessions : les sessions interactives ouvertes (`claude agents`), hors tâches de fond (remplaçable dans les tests).
var openSessions = func() []string {
	out, _ := claudeAgents()
	var a []struct{ SessionId, Kind string }
	json.Unmarshal(out, &a)
	var ids []string
	for _, x := range a {
		if x.Kind == "interactive" && idRe.MatchString(x.SessionId) {
			ids = append(ids, x.SessionId)
		}
	}
	return ids
}

func liveTabs() []TabSession {
	out := []TabSession{}
	for _, id := range openSessions() {
		ps, _ := filepath.Glob(filepath.Join(root, "*", id+".jsonl"))
		if len(ps) == 0 {
			continue
		}
		st, err := os.Stat(ps[0])
		if err != nil {
			continue
		}
		s := info(ps[0], st)
		if s.autoReview() {
			continue
		}
		out = append(out, TabSession{id, s.Dir, s.Cwd, s.Title})
	}
	slices.SortFunc(out, func(a, b TabSession) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// loadTabs : les sauvegardes ; un fichier absent est une liste vide, un fichier illisible une erreur (on ne réécrit jamais par-dessus).
func loadTabs() ([]TabSave, error) {
	b, err := readTrusted(tabsFile())
	if err != nil {
		return []TabSave{}, nil
	}
	return readTabs(b)
}

func readTabs(b []byte) ([]TabSave, error) {
	t := []TabSave{}
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, terr("srv.tabs.fichierIllisible", "A1", err.Error())
	}
	// Les sauvegardes d'avant le filtre de liveTabs contiennent encore des revues automatiques : on les écarte à la lecture.
	for i := range t {
		t[i].Sessions = slices.DeleteFunc(t[i].Sessions, func(s TabSession) bool { return Session{Title: s.Title}.autoReview() })
	}
	t = slices.DeleteFunc(t, func(x TabSave) bool { return len(x.Sessions) == 0 })
	return t, nil
}

func writeTabs(t []TabSave) error {
	b, _ := json.MarshalIndent(t, "", "  ")
	return rWriteAtomic(cfgDir(), tabsFile(), b, 0o600)
}

func sameIDs(a, b []TabSession) bool {
	return slices.EqualFunc(a, b, func(x, y TabSession) bool { return x.ID == y.ID })
}

// saveTabs enregistre les sessions ouvertes. Une sauvegarde automatique est sautée si rien n'a changé
// depuis la précédente ; seules celles de la dernière heure (keepAuto) sont gardées.
func saveTabs(name string, auto bool) (TabSave, bool, error) {
	name = strings.TrimSpace(spaceRe.ReplaceAllString(name, " "))
	if utf8.RuneCountInString(name) > 80 {
		return TabSave{}, false, terr("srv.tabs.nomTropLong")
	}
	ss := liveTabs()
	if len(ss) == 0 {
		return TabSave{}, false, terr("srv.tabs.aucuneSessionOuverte")
	}
	tabsMu.Lock()
	defer tabsMu.Unlock()
	all, err := loadTabs()
	if err != nil {
		if auto {
			journal("tabs-save : %v", err)
		}
		return TabSave{}, false, err
	}
	if auto {
		for _, t := range all {
			if t.Auto {
				if sameIDs(t.Sessions, ss) {
					return t, false, nil
				}
				break
			}
		}
	}
	s := TabSave{name, auto, time.Now().Format(time.RFC3339Nano), ss}
	err = writeTabs(pruneAuto(append([]TabSave{s}, all...)))
	if !auto {
		journal("tabs-save « %s » %d session(s) : %v", name, len(ss), errOr(err))
	}
	return s, true, err
}

// pruneAuto retire les sauvegardes automatiques de plus d'une heure avant la plus récente (liste du plus récent au plus ancien).
func pruneAuto(l []TabSave) []TabSave {
	var newest time.Time
	out := []TabSave{}
	for _, t := range l {
		if t.Auto {
			at, err := time.Parse(time.RFC3339Nano, t.At)
			if newest.IsZero() {
				newest = at
			} else if err != nil || newest.Sub(at) > keepAuto {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

func removeTabs(at string) error {
	tabsMu.Lock()
	defer tabsMu.Unlock()
	all, err := loadTabs()
	if err != nil {
		return err
	}
	next := slices.DeleteFunc(slices.Clone(all), func(t TabSave) bool { return t.At == at })
	if len(next) == len(all) {
		return terr("srv.tabs.sauvegardeIntrouvable")
	}
	journal("tabs-rm %s", at)
	return writeTabs(next)
}

// autosaveTabs sauvegarde les sessions ouvertes au lancement puis régulièrement, tant que gld tourne.
// Sans session ouverte (juste après un redémarrage), rien n'est écrit : la dernière sauvegarde reste intacte.
func autosaveTabs(every time.Duration) {
	saveTabs("", true)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-t.C:
			saveTabs("", true)
		}
	}
}
