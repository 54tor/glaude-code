package main

import (
	"os"
	"os/exec"
)

// runInOwnGroup : sans groupe de processus sous Windows, seule l'annulation par défaut (claude tué) s'applique.
func runInOwnGroup(*exec.Cmd) {}

func interruptGroup(process *os.Process) error { return process.Signal(os.Interrupt) }
