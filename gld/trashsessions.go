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
func trashSessions(items []item) (movedCount, spared int, err error) {
	live, err := liveCheck()
	if err != nil {
		return 0, 0, err
	}
	for _, sessionItem := range items {
		if live[sessionItem.ID] {
			spared++
			continue
		}
		transcriptPath, found := transcript(sessionItem.Dir, sessionItem.ID)
		if !found {
			continue
		}
		fileInfo, _ := os.Stat(transcriptPath)
		meta := TrashedSession{Dir: sessionItem.Dir, ID: sessionItem.ID, At: float64(time.Now().Unix())}
		if fileInfo != nil {
			session := info(transcriptPath, fileInfo)
			meta.Title, meta.Cwd, meta.KB = session.Title, session.Cwd, session.KB
		}
		trashEntryDir := trashSessionPath(sessionItem.ID)
		if mkdirErr := rMkdirAll(cfgDir(), trashEntryDir, 0o700); mkdirErr != nil {
			return movedCount, spared, mkdirErr
		}
		// Le transcript d'abord, la fiche ensuite : une fiche sans transcript serait une entrée fantôme, impossible à purger.
		moved := filepath.Join(trashEntryDir, sessionItem.ID+".jsonl")
		if moveErr := rRename(cfgDir(), transcriptPath, moved); moveErr != nil {
			err = errors.Join(err, moveErr)
			continue
		}
		metaJSON, _ := json.Marshal(meta)
		if metaWriteErr := rWriteFile(cfgDir(), filepath.Join(trashEntryDir, "meta.json"), metaJSON, 0o600); metaWriteErr != nil {
			rRename(cfgDir(), moved, transcriptPath)
			return movedCount, spared, metaWriteErr
		}
		if subagentsDir := filepath.Join(root, sessionItem.Dir, sessionItem.ID); dirExists(subagentsDir) {
			if subagentsMoveErr := rRename(cfgDir(), subagentsDir, filepath.Join(trashEntryDir, "sub")); subagentsMoveErr != nil {
				err = errors.Join(err, subagentsMoveErr)
			}
		}
		movedCount++
	}
	return movedCount, spared, err
}

func dirExists(path string) bool {
	fileInfo, err := os.Stat(path)
	return err == nil && fileInfo.IsDir()
}

func readTrashMeta(id string) (TrashedSession, bool) {
	var meta TrashedSession
	if !idRe.MatchString(id) {
		return meta, false
	}
	metaJSON, err := readTrusted(filepath.Join(trashSessionPath(id), "meta.json"))
	if err != nil || json.Unmarshal(metaJSON, &meta) != nil || meta.ID != id || !safeDir(meta.Dir) {
		return meta, false
	}
	return meta, true
}

// listTrashedSessions : le contenu de la corbeille, le plus récemment supprimé d'abord. Les sessions plus vieilles que
// trashKeep n'y figurent plus ; leur purge revient à expireTrashedSessions (une lecture ne supprime rien).
func listTrashedSessions() []TrashedSession {
	trashedSessions := []TrashedSession{}
	for _, meta := range trashedMetas() {
		if !expired(meta) {
			trashedSessions = append(trashedSessions, meta)
		}
	}
	sort.Slice(trashedSessions, func(leftIndex, rightIndex int) bool {
		return trashedSessions[leftIndex].At > trashedSessions[rightIndex].At
	})
	return trashedSessions
}

func trashedMetas() []TrashedSession {
	dirEntries, _ := readDirTrusted(sessionTrashDir())
	var metas []TrashedSession
	for _, dirEntry := range dirEntries {
		if meta, found := readTrashMeta(dirEntry.Name()); found {
			metas = append(metas, meta)
		}
	}
	return metas
}

func expired(meta TrashedSession) bool { return time.Since(time.Unix(int64(meta.At), 0)) > trashKeep }

// expireTrashedSessions purge pour de bon les sessions restées plus de trashKeep dans la corbeille.
func expireTrashedSessions() (int, error) {
	var expiredIDs []string
	for _, meta := range trashedMetas() {
		if expired(meta) {
			expiredIDs = append(expiredIDs, meta.ID)
		}
	}
	if len(expiredIDs) == 0 {
		return 0, nil
	}
	purgedCount, err := purgeTrashed(expiredIDs)
	journal("trash-expire %d session(s) : %v", purgedCount, errOr(err))
	return purgedCount, err
}

// expireTrashLoop : l'expiration au démarrage, puis à chaque période, jusqu'à l'arrêt du serveur.
func expireTrashLoop(every time.Duration) {
	expire := func() {
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		expireTrashedSessions()
	}
	expire()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
			expire()
		}
	}
}

// restoreTrashed remet une session à sa place ; refuse d'écraser un transcript du même nom.
func restoreTrashed(id string) error {
	meta, found := readTrashMeta(id)
	if !found {
		return terr("srv.trashsessions.sessionIntrouvableDansLa")
	}
	dir := filepath.Join(root, meta.Dir)
	transcriptPath := filepath.Join(dir, id+".jsonl")
	if _, err := os.Lstat(transcriptPath); err == nil { // #nosec G703 -- chemin géré par gld par conception, confiné par rootfs.go sous GLD_RUN_AS
		return terr("srv.trashsessions.unTranscriptDeCe")
	}
	if err := rMkdirAll(cfgDir(), dir, 0o755); err != nil {
		return err
	}
	trashEntryDir := trashSessionPath(id)
	if err := rRename(cfgDir(), filepath.Join(trashEntryDir, id+".jsonl"), transcriptPath); err != nil {
		return err
	}
	if subagentsDir := filepath.Join(trashEntryDir, "sub"); dirExists(subagentsDir) {
		if err := rRename(cfgDir(), subagentsDir, filepath.Join(dir, id)); err != nil {
			return err // le transcript est revenu ; les sous-agents restent dans la corbeille plutôt que d'être effacés
		}
	}
	return rRemoveAll(cfgDir(), trashEntryDir)
}

// purgeTrashed supprime pour de bon : les fichiers reviennent à leur place le temps de la purge complète (purgeSessions retire
// aussi les fichiers associés et les lignes de history.jsonl), puis la corbeille est vidée de ces entrées.
func purgeTrashed(trashIDs []string) (int, error) {
	// Avant toute restauration : si purgeSessions renonçait ensuite, les sessions resteraient restaurées hors corbeille.
	live, err := liveCheck()
	if err != nil {
		return 0, err
	}
	var items []item
	for _, id := range trashIDs {
		meta, found := readTrashMeta(id)
		if !found || live[id] {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(trashSessionPath(id), id+".jsonl")); statErr != nil {
			rRemoveAll(cfgDir(), trashSessionPath(id)) // fiche sans transcript : rien à remettre en place
			continue
		}
		if restoreErr := restoreTrashed(id); restoreErr != nil {
			continue
		}
		items = append(items, item{meta.Dir, id})
	}
	if len(items) == 0 {
		return 0, nil
	}
	purgedCount, _, _, err := purgeSessions(items)
	return purgedCount, err
}

func routesTrashSessions(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/trash/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"sessions": listTrashedSessions(), "days": int(trashKeep / (24 * time.Hour))})
	})
	mux.HandleFunc("POST /v1/trash/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		items, _, itemsValid := readItems(responseWriter, request)
		if !itemsValid {
			return
		}
		items = withChildren(items)
		if request.URL.Query().Get("close") == "1" {
			closeAndWait(items)
		}
		sessionsMu.Lock()
		movedCount, spared, err := trashSessions(items)
		sessionsMu.Unlock()
		journal("trash %d session(s) : %d déplacée(s), %d épargnée(s) %v : %v", len(items), movedCount, spared, ids(items), errOr(err))
		summary := rmSummary(movedCount, spared)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", summary+"\n"+err.Error())
			return
		}
		if movedCount == 0 && spared > 0 {
			writeErr(responseWriter, http.StatusConflict, "conflict", summary)
			return
		}
		writeJSON(responseWriter, 201, map[string]any{"moved": movedCount, "spared": spared, "message": summary})
	})
	// ?item=<id> répété : sessions à remettre en place.
	mux.HandleFunc("PATCH /v1/trash/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		var restoreRequest struct{ Trashed *bool }
		trashIDs := request.URL.Query()["item"]
		if !decodeBody(responseWriter, request, &restoreRequest, 1<<10) {
			return
		}
		if restoreRequest.Trashed == nil || *restoreRequest.Trashed || len(trashIDs) == 0 || len(trashIDs) > itemMax {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		restoredCount := 0
		var err error
		sessionsMu.Lock()
		for _, id := range trashIDs {
			if !idRe.MatchString(id) {
				continue
			}
			if restoreErr := restoreTrashed(id); restoreErr != nil {
				err = errors.Join(err, restoreErr)
			} else {
				restoredCount++
			}
		}
		sessionsMu.Unlock()
		journal("trash-restore %d session(s) : %v", restoredCount, errOr(err))
		summary := L("srv.trashsessions.restored", "A1", restoredCount)
		if err != nil {
			summary += "\n" + err.Error()
		}
		if restoredCount == 0 {
			writeErr(responseWriter, http.StatusConflict, "conflict", summary)
			return
		}
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", summary)
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"restored": restoredCount, "message": summary})
	})
	// ?item=<id> répété : ces sessions ; ?all=1 : toute la corbeille. Ni l'un ni l'autre n'est pas une demande de tout vider.
	mux.HandleFunc("DELETE /v1/trash/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		trashIDs := request.URL.Query()["item"]
		purgeAll := request.URL.Query().Get("all") == "1"
		if (len(trashIDs) == 0) == !purgeAll || len(trashIDs) > itemMax {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		expireTrashedSessions()
		if purgeAll {
			dirEntries, _ := readDirTrusted(sessionTrashDir())
			for _, dirEntry := range dirEntries {
				trashIDs = append(trashIDs, dirEntry.Name())
			}
		}
		validIDs := trashIDs[:0]
		for _, id := range trashIDs {
			if idRe.MatchString(id) {
				validIDs = append(validIDs, id)
			}
		}
		purgedCount, err := purgeTrashed(validIDs)
		journal("trash-purge %d session(s) : %v", purgedCount, errOr(err))
		summary := L("srv.trashsessions.purged", "A1", purgedCount)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", summary+"\n"+err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"purged": purgedCount, "message": summary})
	})
}
