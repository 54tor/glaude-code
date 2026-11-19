package main

// Corps de « gld _push <id> », exécuté dans la session tmux d'une poussée : chaque remote est poussé à son tour, git sur un vrai
// terminal (ses hooks voient un tty). La sortie va au volet tmux et au journal ; status.json suit l'avancement.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func runPushSession(id string) int {
	if !pushRunIDRe.MatchString(id) {
		return 2
	}
	status, err := readPushStatus(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gld :", err)
		return 1
	}
	if err := os.Chdir(status.Dir); err != nil { // le dossier ne passe pas par tmux (-c en expanse les formats « #(…) »)
		fmt.Fprintln(os.Stderr, "gld :", err)
		return 1
	}
	logFile, err := rOpenFile(cfgDir(), pushRunFile(id, pushRunLogFile), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gld :", err)
		return 1
	}
	defer logFile.Close()
	output := io.MultiWriter(os.Stdout, logFile)
	for remoteIndex := range status.Remotes {
		status.Remotes[remoteIndex].State = pushStateRunning
		writePushStatus(status)
		exitCode := pushRemoteAndTags(status.Dir, status.Remotes[remoteIndex].Name, status.Tags, status.ForceTags, output)
		status.Remotes[remoteIndex].Exit = &exitCode
		status.Remotes[remoteIndex].State = pushStateSucceeded
		if exitCode != 0 {
			status.Remotes[remoteIndex].State = pushStateFailed
		}
		writePushStatus(status)
	}
	status.Done = true
	writePushStatus(status)
	return 0
}

// pushRemoteAndTags pousse la branche, puis les tags cochés (refs/tags/<nom>, forcés seulement sur demande explicite) ; le premier
// code non nul est rendu.
func pushRemoteAndTags(dir, remote string, tags []string, forceTags bool, output io.Writer) int {
	tagArgs := []string{"push"}
	if forceTags {
		tagArgs = append(tagArgs, "--force")
	}
	exitCode := pushToRemoteOnTerminal(dir, remote, output)
	for _, tag := range tags {
		tagExitCode := pushRefOnTerminal(dir, tagArgs, remote, "refs/tags/"+tag, output)
		if exitCode == 0 {
			exitCode = tagExitCode
		}
	}
	return exitCode
}

// pushToRemoteOnTerminal pousse HEAD vers remote ; sans amont, la branche est liée à ce remote (-u). Jamais de --force ni d'invite :
// un identifiant demandé fait échouer le push avec le message de git.
func pushToRemoteOnTerminal(dir, remote string, output io.Writer) int {
	args := []string{"push"}
	if upstream, _ := gitOut1(dir, "rev-parse", "--abbrev-ref", "@{u}"); upstream == "" {
		args = append(args, "-u")
	}
	return pushRefOnTerminal(dir, args, remote, "HEAD", output)
}

func pushRefOnTerminal(dir string, args []string, remote, ref string, output io.Writer) int {
	commandContext, cancel := context.WithTimeout(context.Background(), gitPushTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "git", append(args, "--", remote, ref)...) // #nosec G702 -- exécutable fixe, arguments en tableau, sans shell
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		if sshCommand, _ := gitOut1(dir, "config", "core.sshCommand"); sshCommand == "" {
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
		}
	}
	terminal, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintln(output, err)
		return exitCodeUnknown
	}
	defer terminal.Close()
	pty.InheritSize(os.Stdin, terminal)
	io.Copy(output, terminal) // se termine en erreur EIO quand git sort : sans conséquence
	return exitCodeOf(cmd.Wait())
}
