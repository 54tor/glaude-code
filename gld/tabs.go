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
	Busy    bool   `json:"busy,omitempty"` // travaillait à la sauvegarde : le redémarrage l'a coupée en plein tour
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

const tabSaveNameMax = 80

var tabsMu sync.Mutex

func tabsFile() string { return filepath.Join(cfgDir(), "gld-web-tabs.json") }

// openSessions : les sessions interactives ouvertes (`claude agents`), hors tâches de fond (remplaçable dans les tests).
var openSessions = func() []string {
	agentsJSON, _ := claudeAgents()
	var agents []struct{ SessionId, Kind string }
	json.Unmarshal(agentsJSON, &agents)
	var ids []string
	for _, agent := range agents {
		if agent.Kind == "interactive" && idRe.MatchString(agent.SessionId) {
			ids = append(ids, agent.SessionId)
		}
	}
	return ids
}

func liveTabs() []TabSession {
	tabs := []TabSession{}
	for _, sessionID := range openSessions() {
		matches, _ := filepath.Glob(filepath.Join(root, "*", sessionID+".jsonl"))
		if len(matches) == 0 {
			continue
		}
		fileInfo, err := os.Stat(matches[0])
		if err != nil {
			continue
		}
		session := info(matches[0], fileInfo)
		if session.autoReview() || session.team != "" { // ni revue automatique ni équipier : ils appartiennent à leur session parente
			continue
		}
		status, _ := sessionStatus(sessionID)
		tabs = append(tabs, TabSession{sessionID, session.Dir, session.Cwd, session.Title, status == "busy"})
	}
	slices.SortFunc(tabs, func(left, right TabSession) int { return strings.Compare(left.ID, right.ID) })
	return tabs
}

// loadTabs : les sauvegardes ; un fichier absent est une liste vide, un fichier illisible une erreur (on ne réécrit jamais par-dessus).
func loadTabs() ([]TabSave, error) {
	content, err := readTrusted(tabsFile())
	if err != nil {
		return []TabSave{}, nil
	}
	return readTabs(content)
}

func readTabs(content []byte) ([]TabSave, error) {
	saves := []TabSave{}
	if err := json.Unmarshal(content, &saves); err != nil {
		return nil, terr("srv.tabs.fichierIllisible", "A1", err.Error())
	}
	// Les sauvegardes d'avant le filtre de liveTabs contiennent encore des revues automatiques et des équipiers : on les écarte à la lecture.
	for saveIndex := range saves {
		saves[saveIndex].Sessions = slices.DeleteFunc(saves[saveIndex].Sessions, func(tab TabSession) bool { return Session{Title: tab.Title}.autoReview() || isTeammate(tab) })
	}
	saves = slices.DeleteFunc(saves, func(save TabSave) bool { return len(save.Sessions) == 0 })
	return saves, nil
}

// isTeammate : la session sauvegardée est celle d'un équipier (transcript introuvable : non).
func isTeammate(tab TabSession) bool {
	if !idRe.MatchString(tab.ID) || strings.ContainsAny(tab.Dir, "/\\") || tab.Dir == "" || tab.Dir == ".." {
		return false
	}
	transcriptPath := filepath.Join(root, tab.Dir, tab.ID+".jsonl")
	fileInfo, err := os.Stat(transcriptPath)
	return err == nil && info(transcriptPath, fileInfo).team != ""
}

func writeTabs(saves []TabSave) error {
	content, _ := json.MarshalIndent(saves, "", "  ")
	return rWriteAtomic(cfgDir(), tabsFile(), content, 0o600)
}

func sameIDs(left, right []TabSession) bool {
	return slices.EqualFunc(left, right, func(leftTab, rightTab TabSession) bool {
		return leftTab.ID == rightTab.ID && leftTab.Busy == rightTab.Busy
	})
}

// saveTabs enregistre les sessions ouvertes. Une sauvegarde automatique est sautée si rien n'a changé
// depuis la précédente ; seules celles de la dernière heure (keepAuto) sont gardées.
func saveTabs(name string, auto bool) (TabSave, bool, error) {
	name = strings.TrimSpace(spaceRe.ReplaceAllString(name, " "))
	if utf8.RuneCountInString(name) > tabSaveNameMax {
		return TabSave{}, false, terr("srv.tabs.nomTropLong")
	}
	openTabs := liveTabs()
	if len(openTabs) == 0 {
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
		for _, previous := range all {
			if previous.Auto {
				if sameIDs(previous.Sessions, openTabs) {
					return previous, false, nil
				}
				break
			}
		}
	}
	save := TabSave{name, auto, time.Now().Format(time.RFC3339Nano), openTabs}
	err = writeTabs(pruneAuto(append([]TabSave{save}, all...)))
	if !auto {
		journal("tabs-save « %s » %d session(s) : %v", name, len(openTabs), errOr(err))
	}
	return save, true, err
}

// pruneAuto retire les sauvegardes automatiques de plus d'une heure avant la plus récente (liste du plus récent au plus ancien).
func pruneAuto(saves []TabSave) []TabSave {
	var newest time.Time
	kept := []TabSave{}
	for _, save := range saves {
		if save.Auto {
			savedAt, err := time.Parse(time.RFC3339Nano, save.At)
			if newest.IsZero() {
				newest = savedAt
			} else if err != nil || newest.Sub(savedAt) > keepAuto {
				continue
			}
		}
		kept = append(kept, save)
	}
	return kept
}

func removeTabs(at string) error {
	tabsMu.Lock()
	defer tabsMu.Unlock()
	all, err := loadTabs()
	if err != nil {
		return err
	}
	next := slices.DeleteFunc(slices.Clone(all), func(save TabSave) bool { return save.At == at })
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
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
			saveTabs("", true)
		}
	}
}
