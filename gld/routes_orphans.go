package main

import (
	"errors"
	"net/http"
)

// orphanNames : les ?item=<nom> d'une requête, ou false (400 déjà écrit) si vide, trop nombreux ou invalides.
func orphanNames(responseWriter http.ResponseWriter, request *http.Request) ([]string, bool) {
	names := request.URL.Query()["item"]
	if len(names) == 0 || len(names) > itemMax {
		writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
		return nil, false
	}
	for _, name := range names {
		if !validOrphanName(name) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return nil, false
		}
	}
	return names, true
}

// applyOrphans applique action à chaque nom sous sessionsMu et répond avec le compte des réussites.
func applyOrphans(responseWriter http.ResponseWriter, names []string, label, key string, status int, action func(string) error) {
	sessionsMu.Lock()
	doneCount := 0
	var err error
	for _, name := range names {
		if actionErr := action(name); actionErr != nil {
			err = errors.Join(err, actionErr)
		} else {
			doneCount++
		}
	}
	sessionsMu.Unlock()
	journal("%s %d fichier(s) : %v", label, doneCount, errOr(err))
	summary := L(key, "A1", doneCount)
	if err != nil {
		writeErr(responseWriter, http.StatusInternalServerError, "internal", summary+"\n"+err.Error())
		return
	}
	writeJSON(responseWriter, status, map[string]any{"count": doneCount, "message": summary})
}

func routesOrphans(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/uploads/orphans", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, http.StatusOK, map[string]any{"files": listOrphans(), "days": int(trashKeep.Hours() / 24)})
	})
	mux.HandleFunc("POST /v1/uploads/orphans", func(responseWriter http.ResponseWriter, request *http.Request) {
		if names, valid := orphanNames(responseWriter, request); valid {
			applyOrphans(responseWriter, names, "orphan-trash", "srv.routes_orphans.trashed", http.StatusCreated, trashOrphan)
		}
	})
	mux.HandleFunc("PATCH /v1/uploads/orphans", func(responseWriter http.ResponseWriter, request *http.Request) {
		if names, valid := orphanNames(responseWriter, request); valid {
			applyOrphans(responseWriter, names, "orphan-restore", "srv.routes_orphans.restored", http.StatusOK, restoreOrphan)
		}
	})
	// ?item=<nom> répété : ces fichiers de la corbeille ; ?all=1 : toute la corbeille. Ni l'un ni l'autre n'est pas une demande de tout vider.
	mux.HandleFunc("DELETE /v1/uploads/orphans", func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("all") != "1" {
			if names, valid := orphanNames(responseWriter, request); valid {
				applyOrphans(responseWriter, names, "orphan-purge", "srv.routes_orphans.purged", http.StatusOK, purgeOrphan)
			}
			return
		}
		var names []string
		for _, file := range listOrphans() {
			if file.Trashed {
				names = append(names, file.Name)
			}
		}
		applyOrphans(responseWriter, names, "orphan-purge", "srv.routes_orphans.purged", http.StatusOK, purgeOrphan)
	})
}
