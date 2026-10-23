package main

// Corbeille des sessions (~/.claude/gld-web-trash/sessions/<id>/) : « supprimer » y déplace le transcript et le dossier de
// sous-agents, sans rien effacer d'autre (fichiers associés, history.jsonl). La suppression définitive, elle, passe par
// purgeSessions : tout part. Au-delà de trashKeep, une session est purgée pour de bon.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

const trashKeep = 30 * 24 * time.Hour

type TrashedSession struct {
	Dir   string  `json:"dir"`
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Cwd   string  `json:"cwd"`
	At    float64 `json:"at"` // mise à la corbeille, secondes Unix
	KB    int64   `json:"kb"`
}

func sessionTrashDir() string { return filepath.Join(trashDir(), "sessions") }

func trashSessionPath(id string) string { return filepath.Join(sessionTrashDir(), id) }

// trashSessions déplace les sessions dans la corbeille. Les sessions ouvertes sont épargnées (comme pour la suppression).
func trashSessions(items []item) (n, spared int, err error) {
	live, err := liveCheck()
	if err != nil {
		return 0, 0, err
	}
	for _, it := range items {
		if live[it.ID] {
			spared++
			continue
		}
		p, ok := transcript(it.Dir, it.ID)
		if !ok {
			continue
		}
		st, _ := os.Stat(p)
		meta := TrashedSession{Dir: it.Dir, ID: it.ID, At: float64(time.Now().Unix())}
		if st != nil {
			s := info(p, st)
			meta.Title, meta.Cwd, meta.KB = s.Title, s.Cwd, s.KB
		}
		dst := trashSessionPath(it.ID)
		if e := os.MkdirAll(dst, 0o700); e != nil {
			return n, spared, e
		}
		b, _ := json.Marshal(meta)
		if e := os.WriteFile(filepath.Join(dst, "meta.json"), b, 0o600); e != nil {
			return n, spared, e
		}
		if e := os.Rename(p, filepath.Join(dst, it.ID+".jsonl")); e != nil {
			err = errors.Join(err, e)
			continue
		}
		if sub := filepath.Join(root, it.Dir, it.ID); dirExists(sub) {
			if e := os.Rename(sub, filepath.Join(dst, "sub")); e != nil {
				err = errors.Join(err, e)
			}
		}
		n++
	}
	return n, spared, err
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func readTrashMeta(id string) (TrashedSession, bool) {
	var m TrashedSession
	b, err := os.ReadFile(filepath.Join(trashSessionPath(id), "meta.json"))
	if err != nil || json.Unmarshal(b, &m) != nil || m.ID != id || !idRe.MatchString(id) {
		return m, false
	}
	return m, true
}

// listTrashedSessions : le contenu de la corbeille, le plus récemment supprimé d'abord. Les sessions plus vieilles que
// trashKeep sont purgées au passage.
func listTrashedSessions() []TrashedSession {
	ents, _ := os.ReadDir(sessionTrashDir())
	out := []TrashedSession{}
	var old []string
	for _, e := range ents {
		m, ok := readTrashMeta(e.Name())
		if !ok {
			continue
		}
		if time.Since(time.Unix(int64(m.At), 0)) > trashKeep {
			old = append(old, m.ID)
			continue
		}
		out = append(out, m)
	}
	if len(old) > 0 {
		purgeTrashed(old)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

// restoreTrashed remet une session à sa place ; refuse d'écraser un transcript du même nom.
func restoreTrashed(id string) error {
	m, ok := readTrashMeta(id)
	if !ok {
		return errors.New("session introuvable dans la corbeille")
	}
	dir := filepath.Join(root, m.Dir)
	if m.Dir == "" || filepath.Base(m.Dir) != m.Dir {
		return errors.New("dossier d'origine invalide")
	}
	dst := filepath.Join(dir, id+".jsonl")
	if _, err := os.Lstat(dst); err == nil {
		return errors.New("un transcript de ce nom existe déjà")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src := trashSessionPath(id)
	if err := os.Rename(filepath.Join(src, id+".jsonl"), dst); err != nil {
		return err
	}
	if sub := filepath.Join(src, "sub"); dirExists(sub) {
		os.Rename(sub, filepath.Join(dir, id))
	}
	return os.RemoveAll(src)
}

// purgeTrashed supprime pour de bon : les fichiers reviennent à leur place le temps de la purge complète (purgeSessions retire
// aussi les fichiers associés et les lignes de history.jsonl), puis la corbeille est vidée de ces entrées.
func purgeTrashed(ids []string) (int, error) {
	var items []item
	for _, id := range ids {
		m, ok := readTrashMeta(id)
		if !ok {
			continue
		}
		if err := restoreTrashed(id); err != nil {
			continue
		}
		items = append(items, item{m.Dir, id})
	}
	if len(items) == 0 {
		return 0, nil
	}
	n, _, _, err := purgeSessions(items)
	return n, err
}

func routesTrashSessions(mux *http.ServeMux) {
	mux.HandleFunc("GET /session-trash", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"sessions": listTrashedSessions(), "days": int(trashKeep / (24 * time.Hour))})
	})
	mux.HandleFunc("POST /session-trash-add", func(w http.ResponseWriter, r *http.Request) {
		items, _, ok := readItems(w, r)
		if !ok {
			return
		}
		if r.URL.Query().Get("close") == "1" {
			closeAndWait(items)
		}
		n, spared, err := trashSessions(items)
		journal("trash %d session(s) : %d déplacée(s), %d épargnée(s) %v : %v", len(items), n, spared, ids(items), errOr(err))
		out := rmSummary(n, spared)
		if err != nil {
			out += "\n" + err.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil && (n > 0 || spared == 0), "out": out})
	})
	mux.HandleFunc("POST /session-trash-restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ IDs []string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req) != nil || len(req.IDs) == 0 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		n := 0
		var err error
		for _, id := range req.IDs {
			if !idRe.MatchString(id) {
				continue
			}
			if e := restoreTrashed(id); e != nil {
				err = errors.Join(err, e)
			} else {
				n++
			}
		}
		journal("trash-restore %d session(s) : %v", n, errOr(err))
		out := "Restaurée(s) : " + strconv.Itoa(n)
		if err != nil {
			out += "\n" + err.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil && n > 0, "out": out})
	})
	// ids absent ou vide : toute la corbeille.
	mux.HandleFunc("POST /session-trash-purge", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ IDs []string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req)
		ids := req.IDs
		if len(ids) == 0 {
			ents, _ := os.ReadDir(sessionTrashDir())
			for _, e := range ents {
				ids = append(ids, e.Name())
			}
		}
		clean := ids[:0]
		for _, id := range ids {
			if idRe.MatchString(id) {
				clean = append(clean, id)
			}
		}
		n, err := purgeTrashed(clean)
		journal("trash-purge %d session(s) : %v", n, errOr(err))
		out := "Supprimée(s) définitivement : " + strconv.Itoa(n)
		if err != nil {
			out += "\n" + err.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
	})
}
