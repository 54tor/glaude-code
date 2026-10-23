//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// GLD_RUN_AS n'existe pas sous Windows : les sessions tournent sous le compte de gld, ce qu'un avertissement rappelle.

func applyRunAs(*exec.Cmd, ...string) {}

func runAsOn() bool { return false }

func runAsHome() string { return "" }

func tmuxDetached() bool { return false }

const runAsChildEnv = "GLD_RUN_AS_CHILD"

func checkRunAsSeparation() error {
	if os.Getenv("GLD_RUN_AS") != "" {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.runAsWindows", nil))
	}
	return nil
}

func runCanRead(string, string) int { return 2 }

func runSpec() int { return 2 }

func writeSpecAsTarget(string, []byte) (string, error) { return "", errors.ErrUnsupported }
