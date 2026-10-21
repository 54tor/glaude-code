//go:build windows

package main

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

const createNewConsole = 0x00000010

// openTerminal ouvre `claude [options] --resume id` dans un nouvel onglet Windows Terminal, ou à défaut dans une
// nouvelle console. claude est lancé DIRECTEMENT : aucune valeur n'est jamais confiée à cmd.exe, dont les
// métacaractères (& | < > ^ %) permettraient d'enchaîner une autre commande. Les valeurs viennent des transcripts,
// donc on les revalide ici, quel que soit l'appelant.
func openTerminal(cwd, title, id string, flags []string) error {
	if !idRe.MatchString(id) {
		return errors.New("id invalide")
	}
	for _, f := range flags {
		if !flagRe.MatchString(f) {
			return errors.New("option refusée : " + f)
		}
	}
	if strings.Contains(cwd, ";") { // séparateur de commandes de wt.exe
		return errors.New("« ; » interdit dans le chemin du projet")
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		return err
	}
	args := append(append([]string{}, flags...), "--resume", id)
	if wt, err := exec.LookPath("wt.exe"); err == nil {
		return exec.Command(wt, append([]string{"-w", "0", "new-tab", "--title", safeTitle(title), "-d", cwd, claude}, args...)...).Start()
	}
	c := exec.Command(claude, args...)
	c.Dir = cwd
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	return c.Start()
}
