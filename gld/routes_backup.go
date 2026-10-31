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
	mux.HandleFunc("GET /v1/backup/export", func(responseWriter http.ResponseWriter, request *http.Request) {
		var categories []string
		for _, category := range strings.Split(request.URL.Query().Get("cats"), ",") {
			if category != "" {
				categories = append(categories, category)
			}
		}
		archive, fileCount, err := exportBackup(categories)
		if err != nil {
			if isMsg(err, "srv.backup_export.catsVides") || isMsg(err, "srv.backup_export.catInconnue") {
				writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			} else {
				writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			}
			return
		}
		journal("backup-export %s : %d fichier(s)", strings.Join(categories, ","), fileCount)
		responseWriter.Header().Set("Content-Type", "application/zip")
		responseWriter.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "claude-config.zip"}))
		responseWriter.Write(archive)
	})
	mux.HandleFunc("POST /v1/backup/imports", func(responseWriter http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(responseWriter, request.Body, backupUploadMax)
		if request.ParseMultipartForm(backupUploadMax) != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_backup.envoiInvalideOuTrop"))
			return
		}
		uploadedFile, _, err := request.FormFile("file")
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_backup.envoiInvalideOuTrop"))
			return
		}
		defer uploadedFile.Close()
		archive, err := io.ReadAll(uploadedFile)
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_backup.envoiInvalideOuTrop"))
			return
		}
		plan, err := stageBackup(archive)
		if err != nil {
			journal("backup-import : refusé : %v", err)
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		journal("backup-import %s : %d élément(s), %d ignoré(s)", plan.ID, len(plan.Items), len(plan.Skipped))
		writeJSON(responseWriter, 200, plan)
	})
	mux.HandleFunc("POST /v1/backup/imports/{id}/applications", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Picks   []SyncPick
			Targets map[string]string
		}
		if !decodeBody(responseWriter, request, &body, 1<<20) {
			return
		}
		if len(body.Picks) > backupPicksMax {
			writeErr(responseWriter, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		stageID := request.PathValue("id")
		results, err := applyBackup(stageID, body.Picks, body.Targets)
		if isMsg(err, "srv.backup_import.etapeInconnue") {
			writeErr(responseWriter, http.StatusNotFound, "not_found", err.Error())
			return
		}
		journal("backup-apply %s : %d choix, %d résultat(s) : %v", stageID, len(body.Picks), len(results), errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"results": results})
	})
	mux.HandleFunc("DELETE /v1/backup/imports/{id}", func(responseWriter http.ResponseWriter, request *http.Request) {
		stageID := request.PathValue("id")
		dropStage(stageID)
		journal("backup-drop %s", stageID)
		responseWriter.WriteHeader(http.StatusNoContent)
	})
}
