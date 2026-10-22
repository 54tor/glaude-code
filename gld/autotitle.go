package main

// Titre automatique d'une session : Haiku résume le début de la conversation en quelques mots.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const titleSystem = "Tu donnes un titre court (3 à 7 mots, sans guillemets ni point final) à la conversation reçue, " +
	"dans la langue de l'utilisateur. Réponds uniquement par le titre."

// titleRun demande le titre à Haiku (remplaçable dans les tests).
var titleRun = func(text string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := helperCmd(ctx, "haiku", titleSystem)
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.Output()
	var r struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(out, &r) != nil {
		if err == nil {
			err = errors.New("réponse illisible")
		}
		return "", err
	}
	if r.IsError {
		return "", errors.New(cut(r.Result, 300))
	}
	return r.Result, nil
}

// autoTitle propose un titre d'après les premiers échanges du transcript.
func autoTitle(path string) (string, error) {
	var b strings.Builder
	n := 0
	for _, m := range conv(path, false) {
		if m.T == "" {
			continue
		}
		b.WriteString(m.R + " : " + cut(m.T, 600) + "\n\n")
		if n++; n >= 6 {
			break
		}
	}
	if b.Len() == 0 {
		return "", errors.New("conversation vide")
	}
	t, err := titleRun(b.String())
	if err != nil {
		return "", err
	}
	t = strings.Trim(spaceRe.ReplaceAllString(t, " "), " \"«».")
	if t == "" {
		return "", errors.New("titre vide")
	}
	return cut(t, 80), nil
}
