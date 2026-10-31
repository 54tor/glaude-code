//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

const createNewConsole = 0x00000010

// execClaude lance claude comme enfant, avec la console de l'onglet, et reprend son code de sortie.
func execClaude(command *exec.Cmd) int {
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		os.Stderr.WriteString("gld : " + err.Error() + "\n")
		return 1
	}
	return 0
}

// consoleAttr : une nouvelle fenêtre de console, quand Windows Terminal est absent.
func consoleAttr(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
}
