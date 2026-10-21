//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// execClaude remplace ce processus par claude : l'onglet se ferme avec lui, sans processus intermédiaire.
func execClaude(c *exec.Cmd) int {
	err := syscall.Exec(c.Path, c.Args, c.Env)
	os.Stderr.WriteString("gld-web : " + err.Error() + "\n")
	return 1
}

// consoleAttr : sans objet hors Windows (le lanceur « console » n'y existe pas).
func consoleAttr(*exec.Cmd) {}
