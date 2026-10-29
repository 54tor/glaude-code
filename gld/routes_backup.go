package main

import (
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	backupUploadMax = 16 << 20
	backupPicksMax  = 2000
)

// routesBackup : export et import de la configuration (archive zip) ; l'import se fait en deux temps (plan, puis application).
func routesBackup(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/backup/export", func(w http.ResponseWriter, r *http.Request) {
		var cats []string
		for _, c := range strings.Split(r.URL.Query().Get("cats"), ",") {
			if c != "" {
				cats = append(cats, c)
			}
		}
		b, n, err := exportBackup(cats)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		journal("backup-export %s : %d fichier(s)", strings.Join(cats, ","), n)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "claude-config.zip"}))
		w.Write(b)
	})
	mux.HandleFunc("POST /v1/backup/imports", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, backupUploadMax)
		if r.ParseMultipartForm(backupUploadMax) != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_backup.envoiInvalideOuTrop"))
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_backup.envoiInvalideOuTrop"))
			return
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_backup.envoiInvalideOuTrop"))
			return
		}
		plan, err := stageBackup(b)
		if err != nil {
			journal("backup-import : refusé : %v", err)
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		journal("backup-import %s : %d élément(s), %d ignoré(s)", plan.ID, len(plan.Items), len(plan.Skipped))
		writeJSON(w, 200, plan)
	})
	mux.HandleFunc("POST /v1/backup/imports/{id}/applications", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Picks   []SyncPick
			Targets map[string]string
		}
		if !decodeBody(w, r, &req, 1<<20) {
			return
		}
		if len(req.Picks) > backupPicksMax {
			writeErr(w, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		id := r.PathValue("id")
		res, err := applyBackup(id, req.Picks, req.Targets)
		if isMsg(err, "srv.backup_import.etapeInconnue") {
			writeErr(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		journal("backup-apply %s : %d choix, %d résultat(s) : %v", id, len(req.Picks), len(res), errOr(err))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"results": res})
	})
	mux.HandleFunc("DELETE /v1/backup/imports/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		dropStage(id)
		journal("backup-drop %s", id)
		w.WriteHeader(http.StatusNoContent)
	})
}
