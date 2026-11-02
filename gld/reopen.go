package main

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

const reopenTimeout = 60 * time.Second

func sessionCwd(sessionItem item) string {
	transcriptPath := filepath.Join(root, sessionItem.Dir, sessionItem.ID+".jsonl")
	transcriptInfo, err := os.Stat(transcriptPath)
	if err != nil {
		return ""
	}
	return info(transcriptPath, transcriptInfo).Cwd
}

// reopenSessions relance dans un onglet les sessions fermées pour leur déplacement : dans targetDir si le déplacement a
// réussi, sinon dans leur ancien dossier. Renvoie le nombre de réouvertures en échec.
func reopenSessions(requestContext context.Context, previousCwds map[string]string, targetDir string, moved bool) int {
	failed := 0
	for sessionID, previousCwd := range previousCwds {
		cwd := previousCwd
		if moved {
			cwd = targetDir
		}
		args, argsErr := claudeArgs("")
		launchContext, cancel := context.WithTimeout(requestContext, reopenTimeout)
		_, launchErr := matin(launchContext, append(args, "--id", sessionID, cwd)...)
		cancel()
		journal("reopen %s dans %s : %v", sessionID, cwd, errOr(launchErr))
		if argsErr != nil || launchErr != nil {
			failed++
		}
	}
	return failed
}
