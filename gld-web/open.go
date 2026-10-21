package main

// Réouverture de sessions dans un terminal.
// Unix : on délègue à bin/gld-matin (qui sait détecter tmux, kitty, gnome-terminal…).
// Windows : voir open_windows.go (claude est lancé directement, jamais via cmd.exe).

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var unsafeTitle = regexp.MustCompile(`[^\p{L}\p{N} ._-]+`)

// safeTitle : titre d'onglet restreint à des lettres, chiffres, espaces et « . _ - ». Il ne passe jamais par
// un interpréteur, mais « ; » est le séparateur de commandes de wt.exe : on n'en laisse rien passer.
func safeTitle(s string) string {
	return strings.TrimSpace(cut(unsafeTitle.ReplaceAllString(s, " "), 40))
}

var spaceRe = regexp.MustCompile(`\s+`)

// recentSessions : les sessions à rouvrir pour un dossier de transcripts, comme gld-matin —
// récentes (days), assez longues (minBytes), un seul titre par homonyme, hors sessions vivantes.
func recentSessions(dir string, days int, minBytes int64) []Session {
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	live := liveSessions()
	var out []Session
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil || st.Size() < minBytes || time.Since(st.ModTime()) > time.Duration(days)*24*time.Hour {
			continue
		}
		if s := info(p, st); !live[s.ID] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mtime > out[j].Mtime })
	seen := map[string]bool{}
	var res []Session
	for _, s := range out {
		if k := spaceRe.ReplaceAllString(s.Title, " "); !seen[k] {
			seen[k] = true
			res = append(res, s)
		}
	}
	return res
}
