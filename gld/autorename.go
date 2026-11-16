package main

// Titre automatique relancé : une session titrée par /rename automatique est re-titrée tous les autoRenameEveryInputs messages ;
// un titre posé à la main (écart avec celui que nous avons relevé) la retire du suivi, et plus rien n'y touche.

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"time"
)

const (
	autoRenameEveryInputs = 5
	autoRenamePendingFor  = 2 * time.Minute // délai laissé à un /rename envoyé pour écrire son titre
)

// AutoTitle : état d'une session suivie. Title est le dernier titre automatique relevé ; Pending, un /rename envoyé dont le titre n'est pas encore lu.
type AutoTitle struct {
	Title     string `json:"title"`
	Inputs    int    `json:"inputs"`
	Pending   bool   `json:"pending,omitempty"`
	PendingAt int64  `json:"pendingAt,omitempty"`
}

var (
	inputCountMu    sync.Mutex
	inputCountCache = map[string]inputCount{}
	periodicSending = map[string]bool{}
)

type inputCount struct {
	off   int64
	count int
}

// countInputs : messages de l'utilisateur (hors méta et résultats d'outils) du transcript, relus depuis le dernier offset.
func countInputs(path string) int {
	inputCountMu.Lock()
	cached := inputCountCache[path]
	inputCountMu.Unlock()
	file, openErr := openTrusted(path)
	if openErr != nil {
		return cached.count
	}
	defer file.Close()
	if fileInfo, statErr := file.Stat(); statErr == nil && fileInfo.Size() < cached.off {
		cached = inputCount{}
	}
	if _, seekErr := file.Seek(cached.off, io.SeekStart); seekErr != nil {
		return cached.count
	}
	reader := bufio.NewReaderSize(file, jsonLineBufferSize)
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if readErr == nil {
			cached.off += int64(len(lineBytes))
			var parsedLine line
			if json.Unmarshal(lineBytes, &parsedLine) == nil && parsedLine.Type == "user" && !parsedLine.IsMeta && promptOf(&parsedLine) != "" {
				cached.count++
			}
		}
		if readErr != nil {
			break
		}
	}
	inputCountMu.Lock()
	inputCountCache[path] = cached
	inputCountMu.Unlock()
	return cached.count
}

func currentTitle(path string) string {
	file, openErr := openTrusted(path)
	if openErr != nil {
		return ""
	}
	defer file.Close()
	fileInfo, statErr := file.Stat()
	if statErr != nil {
		return ""
	}
	return info(path, fileInfo).Title
}

func updateAutoTitles(change func(titles map[string]AutoTitle)) {
	groupsMu.Lock()
	defer groupsMu.Unlock()
	store := loadStore()
	if store.AutoTitles == nil {
		store.AutoTitles = map[string]AutoTitle{}
	}
	change(store.AutoTitles)
	if len(store.AutoTitles) == 0 {
		store.AutoTitles = nil
	}
	saveStore(store)
}

// markAutoTitled : un /rename automatique vient d'être envoyé à la session.
func markAutoTitled(sessionID, transcriptPath string) {
	inputs := countInputs(transcriptPath)
	updateAutoTitles(func(titles map[string]AutoTitle) {
		previous := titles[sessionID]
		titles[sessionID] = AutoTitle{Title: previous.Title, Inputs: inputs, Pending: true, PendingAt: time.Now().Unix()}
	})
}

// protectManualTitle : un titre posé à la main n'est plus jamais remplacé par le titre automatique.
func protectManualTitle(sessionID string) {
	updateAutoTitles(func(titles map[string]AutoTitle) { delete(titles, sessionID) })
}

func autoRenameTick() {
	groupsMu.Lock()
	tracked := loadStore().AutoTitles
	groupsMu.Unlock()
	if len(tracked) == 0 {
		return
	}
	openSessions := openFn()
	for sessionID, record := range tracked {
		transcriptPath, found := transcript("", sessionID)
		if !found || !openSessions[sessionID] {
			continue
		}
		reviewAutoTitle(sessionID, transcriptPath, record)
	}
}

func reviewAutoTitle(sessionID, transcriptPath string, record AutoTitle) {
	title := currentTitle(transcriptPath)
	if record.Pending {
		isTitleWritten := title != "" && title != record.Title
		isPendingExpired := record.PendingAt > 0 && time.Since(time.Unix(record.PendingAt, 0)) > autoRenamePendingFor
		if !isTitleWritten && !isPendingExpired {
			return
		}
		if !isTitleWritten {
			title = record.Title
		}
		record.Title, record.Pending = title, false
		updateAutoTitles(func(titles map[string]AutoTitle) { titles[sessionID] = record })
	}
	if record.Title == "" || title != record.Title {
		protectManualTitle(sessionID)
		return
	}
	if countInputs(transcriptPath)-record.Inputs < autoRenameEveryInputs {
		return
	}
	inputCountMu.Lock()
	alreadySending := periodicSending[sessionID]
	periodicSending[sessionID] = true
	inputCountMu.Unlock()
	if alreadySending {
		return
	}
	finish := func() {
		inputCountMu.Lock()
		delete(periodicSending, sessionID)
		inputCountMu.Unlock()
	}
	sendWhenIdle(sessionID, "/rename", func() { markAutoTitled(sessionID, transcriptPath); finish() }, finish)
}
