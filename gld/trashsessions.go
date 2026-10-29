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
		if e := rMkdirAll(cfgDir(), dst, 0o700); e != nil {
			return n, spared, e
		}
		// Le transcript d'abord, la fiche ensuite : une fiche sans transcript serait une entrée fantôme, impossible à purger.
		moved := filepath.Join(dst, it.ID+".jsonl")
		if e := rRename(cfgDir(), p, moved); e != nil {
			err = errors.Join(err, e)
			continue
		}
		b, _ := json.Marshal(meta)
		if e := rWriteFile(cfgDir(), filepath.Join(dst, "meta.json"), b, 0o600); e != nil {
			rRename(cfgDir(), moved, p)
			return n, spared, e
		}
		if sub := filepath.Join(root, it.Dir, it.ID); dirExists(sub) {
			if e := rRename(cfgDir(), sub, filepath.Join(dst, "sub")); e != nil {
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
	if !idRe.MatchString(id) {
		return m, false
	}
	b, err := readTrusted(filepath.Join(trashSessionPath(id), "meta.json"))
	if err != nil || json.Unmarshal(b, &m) != nil || m.ID != id || !safeDir(m.Dir) {
		return m, false
	}
	return m, true
}

// listTrashedSessions : le contenu de la corbeille, le plus récemment supprimé d'abord. Les sessions plus vieilles que
// trashKeep n'y figurent plus ; leur purge revient à expireTrashedSessions (une lecture ne supprime rien).
func listTrashedSessions() []TrashedSession {
	out := []TrashedSession{}
	for _, m := range trashedMetas() {
		if !expired(m) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

func trashedMetas() []TrashedSession {
	ents, _ := readDirTrusted(sessionTrashDir())
	var out []TrashedSession
	for _, e := range ents {
		if m, ok := readTrashMeta(e.Name()); ok {
			out = append(out, m)
		}
	}
	return out
}

func expired(m TrashedSession) bool { return time.Since(time.Unix(int64(m.At), 0)) > trashKeep }

// expireTrashedSessions purge pour de bon les sessions restées plus de trashKeep dans la corbeille.
func expireTrashedSessions() (int, error) {
	var old []string
	for _, m := range trashedMetas() {
		if expired(m) {
			old = append(old, m.ID)
		}
	}
	if len(old) == 0 {
		return 0, nil
	}
	n, err := purgeTrashed(old)
	journal("trash-expire %d session(s) : %v", n, errOr(err))
	return n, err
}

// expireTrashLoop : l'expiration au démarrage, puis à chaque période, jusqu'à l'arrêt du serveur.
func expireTrashLoop(every time.Duration) {
	expire := func() {
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		expireTrashedSessions()
	}
	expire()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-t.C:
			expire()
		}
	}
}

// restoreTrashed remet une session à sa place ; refuse d'écraser un transcript du même nom.
func restoreTrashed(id string) error {
	m, ok := readTrashMeta(id)
	if !ok {
		return terr("srv.trashsessions.sessionIntrouvableDansLa")
	}
	dir := filepath.Join(root, m.Dir)
	dst := filepath.Join(dir, id+".jsonl")
	if _, err := os.Lstat(dst); err == nil {
		return terr("srv.trashsessions.unTranscriptDeCe")
	}
	if err := rMkdirAll(cfgDir(), dir, 0o755); err != nil {
		return err
	}
	src := trashSessionPath(id)
	if err := rRename(cfgDir(), filepath.Join(src, id+".jsonl"), dst); err != nil {
		return err
	}
	if sub := filepath.Join(src, "sub"); dirExists(sub) {
		if err := rRename(cfgDir(), sub, filepath.Join(dir, id)); err != nil {
			return err // le transcript est revenu ; les sous-agents restent dans la corbeille plutôt que d'être effacés
		}
	}
	return rRemoveAll(cfgDir(), src)
}

// purgeTrashed supprime pour de bon : les fichiers reviennent à leur place le temps de la purge complète (purgeSessions retire
// aussi les fichiers associés et les lignes de history.jsonl), puis la corbeille est vidée de ces entrées.
func purgeTrashed(ids []string) (int, error) {
	// Avant toute restauration : si purgeSessions renonçait ensuite, les sessions resteraient restaurées hors corbeille.
	live, err := liveCheck()
	if err != nil {
		return 0, err
	}
	var items []item
	for _, id := range ids {
		m, ok := readTrashMeta(id)
		if !ok || live[id] {
			continue
		}
		if _, err := os.Stat(filepath.Join(trashSessionPath(id), id+".jsonl")); err != nil {
			rRemoveAll(cfgDir(), trashSessionPath(id)) // fiche sans transcript : rien à remettre en place
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
	mux.HandleFunc("GET /v1/trash/sessions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"sessions": listTrashedSessions(), "days": int(trashKeep / (24 * time.Hour))})
	})
	mux.HandleFunc("POST /v1/trash/sessions", func(w http.ResponseWriter, r *http.Request) {
		items, _, ok := readItems(w, r)
		if !ok {
			return
		}
		items = withChildren(items)
		if r.URL.Query().Get("close") == "1" {
			closeAndWait(items)
		}
		sessionsMu.Lock()
		n, spared, err := trashSessions(items)
		sessionsMu.Unlock()
		journal("trash %d session(s) : %d déplacée(s), %d épargnée(s) %v : %v", len(items), n, spared, ids(items), errOr(err))
		out := rmSummary(n, spared)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", out+"\n"+err.Error())
			return
		}
		if n == 0 && spared > 0 {
			writeErr(w, http.StatusConflict, "conflict", out)
			return
		}
		writeJSON(w, 201, map[string]any{"moved": n, "spared": spared, "message": out})
	})
	// ?item=<id> répété : sessions à remettre en place.
	mux.HandleFunc("PATCH /v1/trash/sessions", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Trashed *bool }
		ids := r.URL.Query()["item"]
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if req.Trashed == nil || *req.Trashed || len(ids) == 0 || len(ids) > itemMax {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		n := 0
		var err error
		sessionsMu.Lock()
		for _, id := range ids {
			if !idRe.MatchString(id) {
				continue
			}
			if e := restoreTrashed(id); e != nil {
				err = errors.Join(err, e)
			} else {
				n++
			}
		}
		sessionsMu.Unlock()
		journal("trash-restore %d session(s) : %v", n, errOr(err))
		out := L("srv.trashsessions.restored", "A1", n)
		if err != nil {
			out += "\n" + err.Error()
		}
		if n == 0 {
			writeErr(w, http.StatusConflict, "conflict", out)
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", out)
			return
		}
		writeJSON(w, 200, map[string]any{"restored": n, "message": out})
	})
	// ?item=<id> répété : ces sessions ; ?all=1 : toute la corbeille. Ni l'un ni l'autre n'est pas une demande de tout vider.
	mux.HandleFunc("DELETE /v1/trash/sessions", func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query()["item"]
		all := r.URL.Query().Get("all") == "1"
		if (len(ids) == 0) == !all || len(ids) > itemMax {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		expireTrashedSessions()
		if all {
			ents, _ := readDirTrusted(sessionTrashDir())
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
		out := L("srv.trashsessions.purged", "A1", n)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", out+"\n"+err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"purged": n, "message": out})
	})
}
