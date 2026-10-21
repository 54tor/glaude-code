//go:build !windows

package main

import "errors"

// openTerminal n'est utilisé que sous Windows : ailleurs, bin/gld-matin s'en charge.
func openTerminal(cwd, title, id string, flags []string) error {
	return errors.New("non pris en charge sur cette plateforme")
}
