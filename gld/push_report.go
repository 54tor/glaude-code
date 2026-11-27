package main

// Remise automatique de l'échec d'une poussée à la session qui l'a lancée : ouverte, elle reçoit le message ; fermée ou mise à la
// corbeille, elle est restaurée puis reprise avec lui. Le statut garde « Reported » pour qu'un redémarrage de gld ne le rejoue pas.

import (
	"context"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	pushReportEvery     = 5 * time.Second
	pushReportOutputMax = 16 << 10
	pushReportLaunchMax = 60 * time.Second
)

func failedRemotes(status PushRunStatus) []PushRemoteState {
	return slices.DeleteFunc(slices.Clone(status.Remotes), func(remote PushRemoteState) bool { return remote.State != pushStateFailed })
}

func pushOutputTail(id string) string {
	chunk, _ := readPushOutput(id, 0)
	text := strings.ReplaceAll(stripANSI(string(chunk)), "\r", "")
	if len(text) > pushReportOutputMax {
		text = text[len(text)-pushReportOutputMax:]
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = text[newline+1:]
		}
	}
	return strings.TrimSpace(text)
}

func pushReportText(status PushRunStatus) string {
	var names []string
	exitCode := exitCodeUnknown
	for _, remote := range failedRemotes(status) {
		names = append(names, remote.Name)
		if remote.Exit != nil && exitCode == exitCodeUnknown {
			exitCode = *remote.Exit
		}
	}
	return L("srv.pushreport.intro", "A1", status.Branch, "A2", strings.Join(names, ", "), "A3", status.Dir, "A4", exitCode) +
		"\n\n" + pushOutputTail(status.ID) + "\n\n" +
		L("srv.pushreport.outro", "A1", status.Branch, "A2", strings.Join(names, ", "))
}

func deliverPushReport(status PushRunStatus) error {
	text := pushReportText(status)
	if liveSessions()[status.Session] {
		if waitingIDs, _ := waitingKnown(); slices.Contains(waitingIDs, status.Session) || queueSt == nil {
			return sendToSession(status.Session, text)
		}
		_, err := queueSt.Add(status.Session, "host", "", text)
		return err
	}
	if _, isTrashed := readTrashMeta(status.Session); isTrashed {
		if err := restoreTrashed(status.Session); err != nil {
			return err
		}
	}
	if err := checkResumePrompt(status.Session, text); err != nil {
		return err
	}
	transcriptPath, found := transcript(status.SessionDir, status.Session)
	if !found {
		return terr("srv.chat.sessionInvalide")
	}
	transcriptInfo, err := os.Stat(transcriptPath)
	if err != nil {
		return err
	}
	launchContext, cancel := context.WithTimeout(context.Background(), pushReportLaunchMax)
	defer cancel()
	_, err = matin(launchContext, "--id", status.Session, "--prompt", text, info(transcriptPath, transcriptInfo).Cwd)
	return err
}

// reportFailedPushes : un échec de remise est journalisé et marqué remis quand même, sinon il serait rejoué toutes les 5 s ;
// le bouton « Envoyer à Claude » du toast reste le recours.
func reportFailedPushes() {
	for _, status := range listPushRuns() {
		hasFailed := len(failedRemotes(status)) > 0
		if !status.Done || status.Reported || status.Session == "" || !hasFailed {
			continue
		}
		deliveryErr := deliverPushReport(status)
		journal("push-report %s vers la session %s : %v", status.ID, status.Session, errOr(deliveryErr))
		status.Reported = true
		writePushStatus(status)
	}
}

func watchPushReports(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
		}
		reportFailedPushes()
	}
}
