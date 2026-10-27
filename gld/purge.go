package main

// Suppression complète d'une session, en Go, sans bash, jq ni coreutils (donc utilisable sous Windows).
// Sert à l'interface comme aux commandes gld-rm et gld-trier (trier.go, rm.go).
//
// Un transcript n'est que la partie visible : Claude écrit aussi dans file-history/, session-env/,
// tasks/, teams/, jobs/, uploads/, debug/, paste-cache/, shell-snapshots/ et history.jsonl.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var snapRe = regexp.MustCompile(`snapshot-[a-zA-Z]+-[0-9]+-[a-zA-Z0-9]+\.sh`)

// sessionsMu sérialise les mutations de sessions (suppression, corbeille, restauration, réassignation, expiration) : deux
// requêtes simultanées ne doivent pas déplacer ou purger les mêmes fichiers, ni réécrire history.jsonl en même temps.
// Il se prend dans les handlers et la boucle de fond, jamais dans les fonctions internes, qui s'appellent entre elles
// (purgeTrashed → restoreTrashed → purgeSessions).
var sessionsMu sync.Mutex

// liveSessions : ids des sessions vivantes, nil si indéterminé (remplaçable dans les tests).
var liveSessions = alive

var (
	errOpen = terr("srv.purge.sessionDejaOuverteEcris")
	errLive = terr("srv.purge.impossibleDeSavoirQuelles")
)

// liveCheck : les sessions vivantes, ou errLive. À utiliser avant toute action qui écrit ou supprime dans une session.
func liveCheck() (map[string]bool, error) {
	forgetAgents()
	if m := liveSessions(); m != nil {
		return m, nil
	}
	return nil, errLive
}

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
// histMode : les droits de history.jsonl, lus par Lstat ; un lien ou un fichier spécial ne prête jamais les siens.
func histMode(st os.FileInfo) os.FileMode {
	if st.Mode().IsRegular() {
		return st.Mode().Perm()
	}
	return 0o600
}

func safeRemove(p string) error {
	cfg := cfgDir()
	rel, err := filepath.Rel(cfg, p)
	if err != nil || rel == "." || strings.Contains(rel, "..") {
		return terr("srv.purge.refusDEffacerHors", "A1", cfg, "A2", p)
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "memory" {
			return terr("srv.purge.cheminProtegeNonEfface", "A1", p)
		}
	}
	return rRemoveAll(cfg, p)
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

// purgeOne efface le transcript, les sous-agents et les satellites d'une session (sans vérifier qu'elle est
// vivante : c'est à l'appelant), et relève dans snaps les instantanés de shell qu'elle citait. Renvoie le nombre
// d'emplacements effacés et les échecs.
func purgeOne(dir, id string, snaps map[string]bool) (n int, fails []string) {
	snapshotsCited(dir, id, snaps)
	targets := append([]string{filepath.Join(dir, id+".jsonl"), filepath.Join(dir, id)}, satellites(id)...)
	for _, p := range targets {
		if _, e := os.Lstat(p); e != nil {
			continue
		}
		if e := safeRemove(p); e != nil {
			fails = append(fails, e.Error())
		} else {
			n++
		}
	}
	return
}

// purgeSessions supprime transcript, sous-agents et satellites des sessions, puis solde les restes
// partagés. Les sessions vivantes sont épargnées. Renvoie les comptes et un compte rendu.
func purgeSessions(items []item) (n, spared int, notes []string, err error) {
	live, err := liveCheck()
	if err != nil {
		return 0, 0, nil, err
	}
	var deleted []string
	snaps := map[string]bool{}
	var fails []string
	for _, it := range items {
		if live[it.ID] {
			spared++
			continue
		}
		_, f := purgeOne(filepath.Join(root, it.Dir), it.ID, snaps)
		fails = append(fails, f...)
		deleted = append(deleted, it.ID)
		n++
	}
	if n > 0 {
		notes = solder(deleted, snaps)
	}
	if len(fails) > 0 {
		err = terr("srv.purge.echecS", "A1", len(fails), "A2", strings.Join(fails, " ; "))
	}
	return
}

// pruneHistory retire d'history.jsonl les lignes de ces sessions. Renvoie leur nombre, les hachages de collage
// qu'elles citaient, les lignes conservées et d'éventuelles remarques.
func pruneHistory(deleted []string) (removed int, hashes map[string]bool, kept [][]byte, notes []string) {
	cfg := cfgDir()
	hist := filepath.Join(cfg, "history.jsonl")
	hashes = map[string]bool{}

	pats := make([][]byte, len(deleted))
	for i, id := range deleted {
		pats[i] = []byte(`"sessionId":"` + id + `"`)
	}
	st0, _ := os.Stat(hist) // pour détecter une session ouverte qui y ajouterait une ligne pendant la réécriture
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
			// ponytail: fenêtre résiduelle entre ce contrôle et le renommage ; Claude Code n'offre aucun verrou
			if st1, e := os.Lstat(hist); e != nil || st0 == nil || st1.Size() != st0.Size() || !st1.ModTime().Equal(st0.ModTime()) {
				removed = 0
				notes = append(notes, L("srv.purge.historyChanged"))
			} else if e := rWriteAtomic(cfg, hist, bytes.Join(kept, nil), histMode(st1)); e != nil {
				removed = 0
			}
		}
	}
	return
}

// solder : lignes d'history.jsonl, collages et instantanés de shell des sessions effacées. Un collage ou
// un instantané ne part que si plus rien ne le cite (deux sessions peuvent partager le même contenu)
// et s'il n'est pas tout frais.
func solder(deleted []string, snaps map[string]bool) []string {
	cfg := cfgDir()
	removed, hashes, kept, notes := pruneHistory(deleted)
	if removed > 0 {
		notes = append(notes, L("srv.purge.historyRemoved", "A1", removed))
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
		notes = append(notes, L("srv.purge.pasteCache", "A1", np))
	}
	if ns > 0 {
		notes = append(notes, L("srv.purge.shellSnapshots", "A1", ns))
	}
	return notes
}
