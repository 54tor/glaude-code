package main

// Appels à claude et gld-matin : options acceptées, sessions ouvertes ou en attente.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"
)

// checkPrompt : un message vise une seule session, fermée (sinon deux processus écriraient le même transcript).
func checkPrompt(id, prompt string) error {
	switch {
	case prompt == "":
		return nil
	case id == "":
		return errors.New("un message ne s'envoie qu'à une session précise")
	case len(prompt) > 32<<10 || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0):
		return errors.New("message trop long ou invalide")
	}
	live, err := liveCheck()
	if err != nil {
		return err
	}
	if live[id] {
		return errOpen
	}
	return nil
}

// checkResumePrompt : comme checkPrompt, et jamais sous Windows, où le message passerait par cmd.exe
// (qui ne sait pas l'échapper sûrement).
func checkResumePrompt(id, prompt string) error {
	if prompt != "" && runtime.GOOS == "windows" {
		return errors.New("reprise avec un message non prise en charge sous Windows")
	}
	return checkPrompt(id, prompt)
}

// claudeArgs valide les options de claude (seulement des drapeaux simples : --x, -x, --x=valeur, jamais du shell)
// et les traduit en arguments de gld-matin.
func claudeArgs(raw string) ([]string, error) {
	fs := strings.Fields(raw)
	for _, f := range fs {
		if !flagRe.MatchString(f) {
			return nil, errors.New("option refusée : " + f)
		}
	}
	if len(fs) == 0 {
		return nil, nil
	}
	return []string{"--args", strings.Join(fs, " ")}, nil
}

var flagRe = regexp.MustCompile(`^--?[a-zA-Z][a-zA-Z0-9-]*(=[A-Za-z0-9._:/-]+)?$`)

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

func withPath(c *exec.Cmd) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return c
	}
	home, _ := os.UserHomeDir()
	local := filepath.Join(home, ".local/bin")
	c.Env = append(os.Environ(), "PATH="+local+":"+os.Getenv("PATH"))
	// exec.Command a déjà cherché le programme dans le PATH du serveur, où ~/.local/bin peut manquer
	// (service, lanceur de bureau) : on le reprend ici, avec la même priorité que le PATH ci-dessus.
	if name := c.Args[0]; !strings.ContainsRune(name, '/') {
		if fi, err := os.Stat(filepath.Join(local, name)); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			c.Path, c.Err = filepath.Join(local, name), nil
		}
	}
	return c
}

// matin exécute gld-matin (cli) en direct et renvoie sa sortie ; l'erreur s'y ajoute, pour qu'un échec dise pourquoi
// (remplaçable dans les tests).
var matin = func(ctx context.Context, args ...string) (string, error) {
	var buf strings.Builder
	err := cliMatin(ctx, args, &buf)
	if err != nil {
		buf.WriteString("gld-matin : " + err.Error() + "\n")
	}
	return buf.String(), err
}

// alive : ids des sessions ouvertes, ou nil si `claude agents --json` a échoué (on ne sait pas).
func alive() map[string]bool {
	out, err := withPath(exec.Command("claude", "agents", "--json")).Output()
	var a []struct{ SessionId string }
	if err != nil || json.Unmarshal(out, &a) != nil {
		return nil // inconnu : liveCheck fait refuser les actions qui en dépendent
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x.SessionId] = true
	}
	return m
}

// waiting : ids des sessions ouvertes qui attendent (interactive « idle », en arrière-plan « blocked »).
// Une demande d'autorisation en cours n'est pas distinguée d'un tour terminé.
var waiting = func() []string {
	out, err := withPath(exec.Command("claude", "agents", "--json")).Output()
	var a []struct{ SessionId, Status, State string }
	ids := []string{}
	if err != nil || json.Unmarshal(out, &a) != nil {
		return ids
	}
	for _, x := range a {
		if x.Status == "idle" || x.State == "blocked" {
			ids = append(ids, x.SessionId)
		}
	}
	return ids
}
