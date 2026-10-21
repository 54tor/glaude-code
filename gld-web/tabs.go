// Sauvegarde des sessions ouvertes (« onglets ») pour les rouvrir d'un coup, après un redémarrage par exemple.
// Fichier ~/.claude/gld-web-tabs.json, lu aussi par `gld-matin --resume`.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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
	out, _ := withPath(exec.Command("claude", "agents", "--json")).Output()
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
		out = append(out, TabSession{id, s.Dir, s.Cwd, s.Title})
	}
	slices.SortFunc(out, func(a, b TabSession) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func loadTabs() []TabSave {
	t := []TabSave{}
	if b, err := os.ReadFile(tabsFile()); err == nil {
		json.Unmarshal(b, &t)
	}
	return t
}

func writeTabs(t []TabSave) error {
	b, _ := json.MarshalIndent(t, "", "  ")
	tmp := tabsFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, tabsFile())
}

func sameIDs(a, b []TabSession) bool {
	return slices.EqualFunc(a, b, func(x, y TabSession) bool { return x.ID == y.ID })
}

// saveTabs enregistre les sessions ouvertes. Une sauvegarde automatique est sautée si rien n'a changé
// depuis la précédente ; seules celles de la dernière heure (keepAuto) sont gardées.
func saveTabs(name string, auto bool) (TabSave, bool, error) {
	name = strings.TrimSpace(spaceRe.ReplaceAllString(name, " "))
	if utf8.RuneCountInString(name) > 80 {
		return TabSave{}, false, errors.New("nom trop long")
	}
	ss := liveTabs()
	if len(ss) == 0 {
		return TabSave{}, false, errors.New("aucune session ouverte")
	}
	tabsMu.Lock()
	defer tabsMu.Unlock()
	all := loadTabs()
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
	err := writeTabs(pruneAuto(append([]TabSave{s}, all...)))
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
	all := loadTabs()
	next := slices.DeleteFunc(slices.Clone(all), func(t TabSave) bool { return t.At == at })
	if len(next) == len(all) {
		return errors.New("sauvegarde introuvable")
	}
	journal("tabs-rm %s", at)
	return writeTabs(next)
}

// autosaveTabs sauvegarde les sessions ouvertes au lancement puis régulièrement, tant que gld-web tourne.
// Sans session ouverte (juste après un redémarrage), rien n'est écrit : la dernière sauvegarde reste intacte.
func autosaveTabs(every time.Duration) {
	saveTabs("", true)
	for range time.Tick(every) {
		saveTabs("", true)
	}
}
