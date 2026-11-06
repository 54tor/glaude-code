//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// runInOwnGroup : claude et tout ce qu'il lance (Bash, serveurs MCP) forment un groupe ; l'annulation du contexte (délai,
// droit retiré) tue le groupe entier, sans laisser d'orphelin.
func runInOwnGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// interruptGroup : le groupe s'il existe (tour lancé par runInOwnGroup), sinon le seul processus.
func interruptGroup(process *os.Process) error {
	if syscall.Kill(-process.Pid, syscall.SIGINT) == nil {
		return nil
	}
	return process.Signal(os.Interrupt)
}
