package main

// Réouverture de sessions dans un terminal.
// Unix : on délègue à bin/gld-matin (qui sait détecter tmux, kitty, gnome-terminal…).
// Windows : Windows Terminal (wt.exe) si présent, sinon une fenêtre cmd.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// safeTitle : un titre passé à wt.exe ne doit contenir ni « ; » (séparateur de commandes de wt)
// ni caractère de contrôle.
func safeTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == ';' {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(cut(s, 40))
}

// openTerminal ouvre `claude [options] --resume id` dans un nouvel onglet (Windows).
func openTerminal(cwd, title, id string, flags []string) error {
	cl := append(append([]string{"claude"}, flags...), "--resume", id)
	if wt, err := exec.LookPath("wt.exe"); err == nil {
		a := append([]string{"-w", "0", "new-tab", "--title", safeTitle(title), "-d", cwd, "cmd", "/k"}, cl...)
		return exec.Command(wt, a...).Start()
	}
	a := append([]string{"/c", "start", safeTitle(title), "/D", cwd, "cmd", "/k"}, cl...)
	return exec.Command("cmd", a...).Start()
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
