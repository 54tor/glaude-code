package main

// Titre automatique d'une session : Haiku résume le début de la conversation en quelques mots.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// pendingTitles : sessions ouvertes dont le titre automatique attend la fin du processus (id -> transcript).
var (
	pendingTitles   = map[string]string{}
	pendingTitlesMu sync.Mutex
)

func deferTitle(sessionID, path string) {
	pendingTitlesMu.Lock()
	pendingTitles[sessionID] = path
	pendingTitlesMu.Unlock()
}

// flushTitles pose le titre des sessions en attente qui ne sont plus ouvertes ; une session sans titre à ce moment-là seulement.
func flushTitles() {
	pendingTitlesMu.Lock()
	titlesToFlush := make(map[string]string, len(pendingTitles))
	for sessionID, transcriptPath := range pendingTitles {
		titlesToFlush[sessionID] = transcriptPath
	}
	pendingTitlesMu.Unlock()
	if len(titlesToFlush) == 0 {
		return
	}
	liveSessions, liveErr := liveCheck()
	if liveErr != nil {
		return
	}
	for sessionID, transcriptPath := range titlesToFlush {
		if liveSessions[sessionID] {
			continue
		}
		pendingTitlesMu.Lock()
		delete(pendingTitles, sessionID)
		pendingTitlesMu.Unlock()
		if hasTitle(transcriptPath) {
			continue
		}
		title, titleErr := autoTitle(transcriptPath)
		if titleErr == nil {
			titleErr = setTitle(transcriptPath, sessionID, title)
		}
		journal("auto-title %s « %s » : %v", sessionID, title, errOr(titleErr))
	}
}

func titleLoop(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
			flushTitles()
		}
	}
}

func titleSystem() string { return L("prompt.titleSystem", "A1", replyLang()) }

const (
	titleRunTimeout         = 60 * time.Second
	titleErrorMaxLength     = 300
	titleExcerptMaxLength   = 600
	titleMaxMessagesSampled = 6
	titleMaxLength          = 80
)

// titleRun demande le titre à Haiku (remplaçable dans les tests).
var titleRun = func(text string) (string, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), titleRunTimeout)
	defer cancel()
	cmd := helperCmd(commandContext, "haiku", titleSystem())
	cmd.Stdin = strings.NewReader(text)
	output, err := helperOutput(commandContext, cmd)
	var reply struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(output, &reply) != nil {
		if err == nil {
			err = terr("srv.memwrite.reponseIllisible")
		}
		return "", err
	}
	if reply.IsError {
		return "", errors.New(cut(reply.Result, titleErrorMaxLength))
	}
	return reply.Result, nil
}

// autoTitle propose un titre d'après les premiers échanges du transcript.
func autoTitle(path string) (string, error) {
	var excerpt strings.Builder
	sampledMessages := 0
	for _, message := range conv(path, false) {
		if message.T == "" {
			continue
		}
		excerpt.WriteString(message.R + " : " + cut(message.T, titleExcerptMaxLength) + "\n\n")
		sampledMessages++
		if sampledMessages >= titleMaxMessagesSampled {
			break
		}
	}
	if excerpt.Len() == 0 {
		return "", terr("srv.autotitle.emptyConversation")
	}
	title, err := titleRun(excerpt.String())
	if err != nil {
		return "", err
	}
	title = strings.Trim(spaceRe.ReplaceAllString(title, " "), " \"«».")
	if title == "" {
		return "", terr("srv.autotitle.emptyTitle")
	}
	return cut(title, titleMaxLength), nil
}
