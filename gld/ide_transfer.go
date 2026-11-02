package main

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
)

const ideMaxUpload = 64 << 20

func routesIDETransfer(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/fs/entries/raw", ideDownload)
	mux.HandleFunc("POST /v1/fs/entries/upload", ideUpload)
}

func ideDownload(responseWriter http.ResponseWriter, request *http.Request) {
	root, rel, targetInfo, err := ideResolve(request, false)
	if err != nil {
		ideFail(responseWriter, err)
		return
	}
	defer root.Close()
	if !targetInfo.Mode().IsRegular() {
		ideFail(responseWriter, errIdePath)
		return
	}
	file, err := root.Open(rel)
	if err != nil {
		ideFail(responseWriter, err)
		return
	}
	defer file.Close()
	if openedInfo, statErr := file.Stat(); statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(targetInfo, openedInfo) {
		ideFail(responseWriter, errIdeSymlink)
		return
	}
	header := responseWriter.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(rel)}))
	header.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(responseWriter, request, "", targetInfo.ModTime(), file)
}

func ideUpload(responseWriter http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	dir := query.Get("dir")
	root, rel, err := ideOpen(dir, query.Get("path"))
	if err != nil {
		ideFail(responseWriter, err)
		return
	}
	defer root.Close()
	if ideInTrash(rel) {
		ideFail(responseWriter, errIdePath)
		return
	}
	if ideCodeDenied(responseWriter, request, rel) {
		return
	}
	if parentErr := ideParentDir(root, rel); parentErr != nil {
		ideFail(responseWriter, parentErr)
		return
	}
	if ideExists(root, rel) {
		ideFail(responseWriter, errIdeExists)
		return
	}
	content, err := io.ReadAll(http.MaxBytesReader(responseWriter, request.Body, ideMaxUpload))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			err = errIdeTooBig
		}
		ideFail(responseWriter, err)
		return
	}
	refuseIfAppeared := func() error {
		if ideExists(root, rel) {
			return errIdeExists
		}
		return nil
	}
	if writeErr := ideAtomicWrite(root, rel, content, 0o644, refuseIfAppeared); writeErr != nil {
		ideFail(responseWriter, writeErr)
		return
	}
	ideAudit(responseWriter, request, "upload", dir, rel, int64(len(content)))
	writeJSON(responseWriter, 201, map[string]any{"path": rel})
}
