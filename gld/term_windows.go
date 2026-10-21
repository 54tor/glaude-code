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
func execClaude(c *exec.Cmd) int {
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		os.Stderr.WriteString("gld : " + err.Error() + "\n")
		return 1
	}
	return 0
}

// consoleAttr : une nouvelle fenêtre de console, quand Windows Terminal est absent.
func consoleAttr(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole} }
