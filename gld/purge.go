package main

// Suppression complète d'une session, en Go, sans bash, jq ni coreutils (donc utilisable sous Windows).
// Sert à l'interface comme aux commandes gld-rm et gld-trier (trier.go, rm.go).
//
// Un transcript n'est que la partie visible : Claude écrit aussi dans file-history/, session-env/,
// tasks/, teams/, jobs/, uploads/, debug/, paste-cache/, shell-snapshots/ et history.jsonl.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var snapRe = regexp.MustCompile(`snapshot-[a-zA-Z]+-[0-9]+-[a-zA-Z0-9]+\.sh`)

// sessionsMu sérialise les mutations de sessions (suppression, corbeille, restauration, réassignation, expiration) : deux
// requêtes simultanées ne doivent pas déplacer ou purger les mêmes fichiers, ni réécrire history.jsonl en même temps.
// Il se prend dans les handlers et la boucle de fond, jamais dans les fonctions internes, qui s'appellent entre elles
// (purgeTrashed → restoreTrashed → purgeSessions).
var sessionsMu sync.Mutex

// liveSessions : ids des sessions vivantes, nil si indéterminé (remplaçable dans les tests).
var liveSessions = alive

var (
	errOpen = terr("srv.purge.sessionDejaOuverteEcris")
	errLive = terr("srv.purge.impossibleDeSavoirQuelles")
)

// liveCheck : les sessions vivantes, ou errLive. À utiliser avant toute action qui écrit ou supprime dans une session.
func liveCheck() (map[string]bool, error) {
	forgetAgents()
	if liveIDs := liveSessions(); liveIDs != nil {
		return liveIDs, nil
	}
	return nil, errLive
}

// cfgDir : racine de la configuration Claude (le parent de projects/). Rien n'est effacé hors d'ici.
func cfgDir() string { return filepath.Dir(root) }

// grace : un fichier touché depuis moins de GLD_GRACE minutes (60 par défaut) n'est jamais un reste.
func grace() time.Duration {
	minutes, err := strconv.Atoi(env("GLD_GRACE", os.Getenv("CC_GRACE")))
	if err != nil || minutes < 0 {
		minutes = 60
	}
	return time.Duration(minutes) * time.Minute
}

// safeRemove efface un chemin, avec garde-fou : rien hors de la config Claude, jamais un memory/
// (la mémoire longue durée de Claude, qui survit aux sessions).
// histMode : les droits de history.jsonl, lus par Lstat ; un lien ou un fichier spécial ne prête jamais les siens.
func histMode(fileInfo os.FileInfo) os.FileMode {
	if fileInfo.Mode().IsRegular() {
		return fileInfo.Mode().Perm()
	}
	return 0o600
}

func safeRemove(path string) error {
	configDir := cfgDir()
	rel, err := filepath.Rel(configDir, path)
	if err != nil || rel == "." || strings.Contains(rel, "..") {
		return terr("srv.purge.refusDEffacerHors", "A1", configDir, "A2", path)
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "memory" {
			return terr("srv.purge.cheminProtegeNonEfface", "A1", path)
		}
	}
	return rRemoveAll(configDir, path)
}

// satellites : tout ce qu'une session possède AILLEURS que dans son transcript.
// file-history, session-env, uploads, debug portent l'id entier ; tasks, teams, jobs les huit premiers caractères.
func satellites(sessionID string) []string {
	if !idRe.MatchString(sessionID) {
		return nil
	}
	configDir, shortID := cfgDir(), sessionID[:8]
	paths := []string{
		filepath.Join(configDir, "file-history", sessionID),
		filepath.Join(configDir, "session-env", sessionID),
		filepath.Join(configDir, "uploads", sessionID),
		filepath.Join(configDir, "debug", sessionID+".txt"),
		filepath.Join(configDir, "tasks", "session-"+shortID),
		filepath.Join(configDir, "teams", "session-"+shortID),
	}
	// jobs/ : huit caractères pourraient désigner une autre session, state.json tranche.
	// Un job sans state.json lisible appartient à celle-ci par défaut.
	job := filepath.Join(configDir, "jobs", shortID)
	if jobInfo, statErr := os.Stat(job); statErr == nil && jobInfo.IsDir() {
		var jobState struct {
			SessionId string `json:"sessionId"`
		}
		if content, readErr := readTrusted(filepath.Join(job, "state.json")); readErr == nil {
			json.Unmarshal(content, &jobState)
		}
		if jobState.SessionId == "" || jobState.SessionId == sessionID {
			paths = append(paths, job)
		}
	}
	return paths
}

// snapshotsCited : instantanés de shell cités par une session (transcript et sous-agents),
// à relever AVANT de l'effacer : leur nom ne se déduit de rien d'autre.
func snapshotsCited(dir, sessionID string, into map[string]bool) {
	files := []string{filepath.Join(dir, sessionID+".jsonl")}
	filepath.WalkDir(filepath.Join(dir, sessionID), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	for _, file := range files {
		scan(file, func(rawLine []byte) {
			if bytes.Contains(rawLine, []byte("snapshot-")) {
				for _, match := range snapRe.FindAll(rawLine, -1) {
					into[string(match)] = true
				}
			}
		})
	}
}

// hexTokens : jetons de 16 caractères hexadécimaux, comme `grep -oE '[0-9a-f]{16}'` (découpage
// aligné dans une suite plus longue). Les hachages de collage sont cités ainsi dans les transcripts.
func hexTokens(content []byte, into map[string]bool) {
	isHex := func(character byte) bool {
		return character >= '0' && character <= '9' || character >= 'a' && character <= 'f'
	}
	for runStart := 0; runStart < len(content); {
		if !isHex(content[runStart]) {
			runStart++
			continue
		}
		runEnd := runStart
		for runEnd < len(content) && isHex(content[runEnd]) {
			runEnd++
		}
		for tokenStart := runStart; tokenStart+16 <= runEnd; tokenStart += 16 {
			into[string(content[tokenStart:tokenStart+16])] = true
		}
		runStart = runEnd
	}
}

// lineHashes : hachages de collage (pastedContents[].contentHash) d'une ligne d'history.jsonl.
func lineHashes(rawLine []byte) []string {
	var historyLine struct {
		PastedContents json.RawMessage `json:"pastedContents"`
	}
	if json.Unmarshal(rawLine, &historyLine) != nil || len(historyLine.PastedContents) == 0 {
		return nil
	}
	type pastedContent struct {
		ContentHash string `json:"contentHash"`
	}
	var hashes []string
	var byKey map[string]pastedContent
	var contents []pastedContent
	if json.Unmarshal(historyLine.PastedContents, &byKey) == nil {
		for _, content := range byKey {
			contents = append(contents, content)
		}
	} else if json.Unmarshal(historyLine.PastedContents, &contents) != nil {
		return nil
	}
	for _, content := range contents {
		if content.ContentHash != "" {
			hashes = append(hashes, content.ContentHash)
		}
	}
	return hashes
}

// purgeOne efface le transcript, les sous-agents et les satellites d'une session (sans vérifier qu'elle est
// vivante : c'est à l'appelant), et relève dans snaps les instantanés de shell qu'elle citait. Renvoie le nombre
// d'emplacements effacés et les échecs.
func purgeOne(dir, sessionID string, snaps map[string]bool) (removedCount int, fails []string) {
	snapshotsCited(dir, sessionID, snaps)
	targets := append([]string{filepath.Join(dir, sessionID+".jsonl"), filepath.Join(dir, sessionID)}, satellites(sessionID)...)
	for _, path := range targets {
		if _, lstatErr := os.Lstat(path); lstatErr != nil {
			continue
		}
		if removeErr := safeRemove(path); removeErr != nil {
			fails = append(fails, removeErr.Error())
		} else {
			removedCount++
		}
	}
	return
}

// purgeSessions supprime transcript, sous-agents et satellites des sessions, puis solde les restes
// partagés. Les sessions vivantes sont épargnées. Renvoie les comptes et un compte rendu.
func purgeSessions(items []item) (purgedCount, spared int, notes []string, err error) {
	live, err := liveCheck()
	if err != nil {
		return 0, 0, nil, err
	}
	var deleted []string
	snaps := map[string]bool{}
	var fails []string
	for _, sessionItem := range items {
		if live[sessionItem.ID] {
			spared++
			continue
		}
		_, itemFails := purgeOne(filepath.Join(root, sessionItem.Dir), sessionItem.ID, snaps)
		fails = append(fails, itemFails...)
		deleted = append(deleted, sessionItem.ID)
		purgedCount++
	}
	if purgedCount > 0 {
		dropSessionFiles(deleted)
		notes = solder(deleted, snaps)
	}
	if len(fails) > 0 {
		err = terr("srv.purge.echecS", "A1", len(fails), "A2", strings.Join(fails, " ; "))
	}
	return
}

// pruneHistory retire d'history.jsonl les lignes de ces sessions. Renvoie leur nombre, les hachages de collage
// qu'elles citaient, les lignes conservées et d'éventuelles remarques.
func pruneHistory(deleted []string) (removed int, hashes map[string]bool, kept [][]byte, notes []string) {
	configDir := cfgDir()
	historyPath := filepath.Join(configDir, "history.jsonl")
	hashes = map[string]bool{}

	patterns := make([][]byte, len(deleted))
	for patternIndex, sessionID := range deleted {
		patterns[patternIndex] = []byte(`"sessionId":"` + sessionID + `"`)
	}
	statBefore, _ := os.Stat(historyPath) // pour détecter une session ouverte qui y ajouterait une ligne pendant la réécriture
	if content, readErr := readTrusted(historyPath); readErr == nil && len(content) > 0 {
		for _, rawLine := range bytes.SplitAfter(content, []byte("\n")) {
			drop := false
			for _, pattern := range patterns {
				if bytes.Contains(rawLine, pattern) {
					drop = true
					break
				}
			}
			if drop {
				removed++
				for _, hash := range lineHashes(rawLine) {
					hashes[hash] = true
				}
			} else if len(rawLine) > 0 {
				kept = append(kept, rawLine)
			}
		}
		if removed > 0 { // fichier temporaire puis renommage : jamais de troncature si on est interrompu
			// ponytail: fenêtre résiduelle entre ce contrôle et le renommage ; Claude Code n'offre aucun verrou
			if statNow, lstatErr := os.Lstat(historyPath); lstatErr != nil || statBefore == nil || statNow.Size() != statBefore.Size() || !statNow.ModTime().Equal(statBefore.ModTime()) {
				removed = 0
				notes = append(notes, L("srv.purge.historyChanged"))
			} else if historyWriteErr := rWriteAtomic(configDir, historyPath, bytes.Join(kept, nil), histMode(statNow)); historyWriteErr != nil {
				removed = 0
			}
		}
	}
	return
}

// solder : lignes d'history.jsonl, collages et instantanés de shell des sessions effacées. Un collage ou
// un instantané ne part que si plus rien ne le cite (deux sessions peuvent partager le même contenu)
// et s'il n'est pas tout frais.
func solder(deleted []string, snaps map[string]bool) []string {
	configDir := cfgDir()
	removed, hashes, kept, notes := pruneHistory(deleted)
	if removed > 0 {
		notes = append(notes, L("srv.purge.historyRemoved", "A1", removed))
	}
	if len(hashes) == 0 && len(snaps) == 0 {
		return notes
	}

	// Tout ce que les données restantes citent encore.
	refs := map[string]bool{}
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		scan(path, func(rawLine []byte) {
			hexTokens(rawLine, refs)
			if bytes.Contains(rawLine, []byte("snapshot-")) {
				for _, match := range snapRe.FindAll(rawLine, -1) {
					refs[string(match)] = true
				}
			}
		})
		return nil
	})
	for _, rawLine := range kept {
		for _, hash := range lineHashes(rawLine) {
			refs[hash] = true
		}
	}

	isOld := func(path string) bool {
		fileInfo, statErr := os.Stat(path)
		return statErr == nil && time.Since(fileInfo.ModTime()) > grace()
	}
	pasteCount, snapshotCount := 0, 0
	for hash := range hashes {
		if refs[hash] {
			continue
		}
		if pastePath := filepath.Join(configDir, "paste-cache", hash+".txt"); isOld(pastePath) && safeRemove(pastePath) == nil {
			pasteCount++
		}
	}
	for snapshot := range snaps {
		if refs[snapshot] {
			continue
		}
		if snapshotPath := filepath.Join(configDir, "shell-snapshots", snapshot); isOld(snapshotPath) && safeRemove(snapshotPath) == nil {
			snapshotCount++
		}
	}
	if pasteCount > 0 {
		notes = append(notes, L("srv.purge.pasteCache", "A1", pasteCount))
	}
	if snapshotCount > 0 {
		notes = append(notes, L("srv.purge.shellSnapshots", "A1", snapshotCount))
	}
	return notes
}
