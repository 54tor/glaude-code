package main

// Sessions à rouvrir dans un terminal (les onglets eux-mêmes : term.go, matin.go).

import (
	"regexp"
	"strings"
)

const tabTitleMaxLength = 40

var unsafeTitle = regexp.MustCompile(`[^\p{L}\p{N} ._-]+`)

// safeTitle : titre d'onglet restreint à des lettres, chiffres, espaces et « . _ - ». Il ne passe jamais par
// un interpréteur, mais « ; » est le séparateur de commandes de wt.exe : on n'en laisse rien passer.
func safeTitle(title string) string {
	return strings.TrimSpace(cut(unsafeTitle.ReplaceAllString(title, " "), tabTitleMaxLength))
}

var spaceRe = regexp.MustCompile(`\s+`)
