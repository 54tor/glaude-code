package main

// Suppression complète d'une session, en Go : même logique que purger_session + solder de
// bin/gld-lib.sh, sans bash, jq ni coreutils (donc utilisable sous Windows).
//
// Un transcript n'est que la partie visible : Claude écrit aussi dans file-history/, session-env/,
// tasks/, teams/, jobs/, uploads/, debug/, paste-cache/, shell-snapshots/ et history.jsonl.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var snapRe = regexp.MustCompile(`snapshot-[a-zA-Z]+-[0-9]+-[a-zA-Z0-9]+\.sh`)

// liveSessions : ids des sessions vivantes (remplaçable dans les tests).
var liveSessions = alive

// cfgDir : racine de la configuration Claude (le parent de projects/). Rien n'est effacé hors d'ici.
func cfgDir() string { return filepath.Dir(root) }

// grace : un fichier touché depuis moins de GLD_GRACE minutes (60 par défaut) n'est jamais un reste.
func grace() time.Duration {
	n, err := strconv.Atoi(env("GLD_GRACE", os.Getenv("CC_GRACE")))
	if err != nil || n < 0 {
		n = 60
	}
	return time.Duration(n) * time.Minute
}

// safeRemove efface un chemin, avec garde-fou : rien hors de la config Claude, jamais un memory/
// (la mémoire longue durée de Claude, qui survit aux sessions).
func safeRemove(p string) error {
	cfg := cfgDir()
	rel, err := filepath.Rel(cfg, p)
	if err != nil || rel == "." || strings.Contains(rel, "..") {
		return fmt.Errorf("refus d'effacer hors de %s : %s", cfg, p)
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "memory" {
			return fmt.Errorf("chemin protégé, non effacé : %s", p)
		}
	}
	return os.RemoveAll(p)
}

// satellites : tout ce qu'une session possède AILLEURS que dans son transcript.
// file-history, session-env, uploads, debug portent l'id entier ; tasks, teams, jobs les huit premiers caractères.
func satellites(id string) []string {
	cfg, short := cfgDir(), id[:8]
	l := []string{
		filepath.Join(cfg, "file-history", id),
		filepath.Join(cfg, "session-env", id),
		filepath.Join(cfg, "uploads", id),
		filepath.Join(cfg, "debug", id+".txt"),
		filepath.Join(cfg, "tasks", "session-"+short),
		filepath.Join(cfg, "teams", "session-"+short),
	}
	// jobs/ : huit caractères pourraient désigner une autre session, state.json tranche.
	// Un job sans state.json lisible appartient à celle-ci par défaut.
	job := filepath.Join(cfg, "jobs", short)
	if fi, err := os.Stat(job); err == nil && fi.IsDir() {
		var st struct {
			SessionId string `json:"sessionId"`
		}
		if b, err := os.ReadFile(filepath.Join(job, "state.json")); err == nil {
			json.Unmarshal(b, &st)
		}
		if st.SessionId == "" || st.SessionId == id {
			l = append(l, job)
		}
	}
	return l
}

// snapshotsCited : instantanés de shell cités par une session (transcript et sous-agents),
// à relever AVANT de l'effacer : leur nom ne se déduit de rien d'autre.
func snapshotsCited(dir, id string, into map[string]bool) {
	files := []string{filepath.Join(dir, id+".jsonl")}
	filepath.WalkDir(filepath.Join(dir, id), func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	for _, f := range files {
		scan(f, func(b []byte) {
			if bytes.Contains(b, []byte("snapshot-")) {
				for _, m := range snapRe.FindAll(b, -1) {
					into[string(m)] = true
				}
			}
		})
	}
}

// hexTokens : jetons de 16 caractères hexadécimaux, comme `grep -oE '[0-9a-f]{16}'` (découpage
// aligné dans une suite plus longue). Les hachages de collage sont cités ainsi dans les transcripts.
func hexTokens(b []byte, into map[string]bool) {
	isHex := func(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' }
	for i := 0; i < len(b); {
		if !isHex(b[i]) {
			i++
			continue
		}
		j := i
		for j < len(b) && isHex(b[j]) {
			j++
		}
		for k := i; k+16 <= j; k += 16 {
			into[string(b[k:k+16])] = true
		}
		i = j
	}
}

// lineHashes : hachages de collage (pastedContents[].contentHash) d'une ligne d'history.jsonl.
func lineHashes(b []byte) []string {
	var l struct {
		PastedContents json.RawMessage `json:"pastedContents"`
	}
	if json.Unmarshal(b, &l) != nil || len(l.PastedContents) == 0 {
		return nil
	}
	type pc struct {
		ContentHash string `json:"contentHash"`
	}
	var out []string
	var m map[string]pc
	var a []pc
	if json.Unmarshal(l.PastedContents, &m) == nil {
		for _, v := range m {
			a = append(a, v)
		}
	} else if json.Unmarshal(l.PastedContents, &a) != nil {
		return nil
	}
	for _, v := range a {
		if v.ContentHash != "" {
			out = append(out, v.ContentHash)
		}
	}
	return out
}

// purgeSessions supprime transcript, sous-agents et satellites des sessions, puis solde les restes
// partagés. Les sessions vivantes sont épargnées. Renvoie les comptes et un compte rendu.
func purgeSessions(items []item) (n, spared int, notes []string, err error) {
	live := liveSessions()
	var deleted []string
	snaps := map[string]bool{}
	var fails []string
	for _, it := range items {
		if live[it.ID] {
			spared++
			continue
		}
		dir := filepath.Join(root, it.Dir)
		snapshotsCited(dir, it.ID, snaps)
		targets := append([]string{filepath.Join(dir, it.ID+".jsonl"), filepath.Join(dir, it.ID)}, satellites(it.ID)...)
		for _, p := range targets {
			if _, e := os.Lstat(p); e != nil {
				continue
			}
			if e := safeRemove(p); e != nil {
				fails = append(fails, e.Error())
			}
		}
		deleted = append(deleted, it.ID)
		n++
	}
	if n > 0 {
		notes = solder(deleted, snaps)
	}
	if len(fails) > 0 {
		err = fmt.Errorf("%d échec(s) : %s", len(fails), strings.Join(fails, " ; "))
	}
	return
}

// solder : lignes d'history.jsonl, collages et instantanés de shell des sessions effacées. Un collage ou
// un instantané ne part que si plus rien ne le cite (deux sessions peuvent partager le même contenu)
// et s'il n'est pas tout frais.
func solder(deleted []string, snaps map[string]bool) []string {
	cfg := cfgDir()
	var notes []string
	hist := filepath.Join(cfg, "history.jsonl")
	hashes := map[string]bool{}
	var kept [][]byte // lignes d'history.jsonl restantes

	pats := make([][]byte, len(deleted))
	for i, id := range deleted {
		pats[i] = []byte(`"sessionId":"` + id + `"`)
	}
	removed := 0
	if b, e := os.ReadFile(hist); e == nil && len(b) > 0 {
		for _, ln := range bytes.SplitAfter(b, []byte("\n")) {
			drop := false
			for _, p := range pats {
				if bytes.Contains(ln, p) {
					drop = true
					break
				}
			}
			if drop {
				removed++
				for _, h := range lineHashes(ln) {
					hashes[h] = true
				}
			} else if len(ln) > 0 {
				kept = append(kept, ln)
			}
		}
		if removed > 0 { // fichier temporaire puis renommage : jamais de troncature si on est interrompu
			tmp := hist + ".cc.tmp"
			mode := os.FileMode(0o600)
			if st, e := os.Stat(hist); e == nil {
				mode = st.Mode()
			}
			if e := os.WriteFile(tmp, bytes.Join(kept, nil), mode); e == nil {
				if e := os.Rename(tmp, hist); e != nil {
					os.Remove(tmp)
					removed = 0
				}
			} else {
				removed = 0
			}
		}
	}
	if removed > 0 {
		notes = append(notes, fmt.Sprintf("  history.jsonl   : %d ligne(s) de prompt retirée(s)", removed))
	}
	if len(hashes) == 0 && len(snaps) == 0 {
		return notes
	}

	// Tout ce que les données restantes citent encore.
	refs := map[string]bool{}
	filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		scan(p, func(b []byte) {
			hexTokens(b, refs)
			if bytes.Contains(b, []byte("snapshot-")) {
				for _, m := range snapRe.FindAll(b, -1) {
					refs[string(m)] = true
				}
			}
		})
		return nil
	})
	for _, ln := range kept {
		for _, h := range lineHashes(ln) {
			refs[h] = true
		}
	}

	old := func(p string) bool {
		st, e := os.Stat(p)
		return e == nil && time.Since(st.ModTime()) > grace()
	}
	np, ns := 0, 0
	for h := range hashes {
		if refs[h] {
			continue
		}
		if p := filepath.Join(cfg, "paste-cache", h+".txt"); old(p) && safeRemove(p) == nil {
			np++
		}
	}
	for s := range snaps {
		if refs[s] {
			continue
		}
		if p := filepath.Join(cfg, "shell-snapshots", s); old(p) && safeRemove(p) == nil {
			ns++
		}
	}
	if np > 0 {
		notes = append(notes, fmt.Sprintf("  paste-cache     : %d collage(s) effacé(s)", np))
	}
	if ns > 0 {
		notes = append(notes, fmt.Sprintf("  shell-snapshots : %d instantané(s) effacé(s)", ns))
	}
	return notes
}
