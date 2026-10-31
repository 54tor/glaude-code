package main

// Images : dépôt d'une image collée dans un champ d'envoi (elle est rangée dans un dossier privé, son chemin part dans le message
// avec « @ ») et affichage des images que contient une conversation.

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	imgMax    = 12 << 20 // taille d'une image déposée
	fileMax   = 25 << 20 // taille d'un fichier joint
	imgLine   = 48 << 20 // ligne de transcript lue pour en extraire une image
	imgKeep   = 14 * 24 * time.Hour
	imgPrefix = "gld-web-images"
	// contentSniffBytes : http.DetectContentType ne regarde pas au-delà.
	contentSniffBytes  = 512
	extensionMinLength = 2
	extensionMaxLength = 9
	imageBlockMax      = 1000
)

func imagesDir() string { return filepath.Join(cfgDir(), imgPrefix) }

// imageExt : l'extension d'une image d'après son contenu (jamais d'après ce que dit le navigateur) ; vide si ce n'en est pas une.
// Seuls les formats raster courants passent : pas de SVG, qui peut porter du script.
func imageExt(content []byte) string {
	switch http.DetectContentType(content[:min(len(content), contentSniffBytes)]) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// saveImage range une image dans le dossier privé et renvoie son chemin ; les images de plus de 14 jours sont retirées au passage.
func saveImage(content []byte, sessionID string) (string, error) {
	if len(content) == 0 || len(content) > imgMax {
		return "", terr("srv.images.imageVideOuTrop")
	}
	ext := imageExt(content)
	if ext == "" {
		return "", terr("srv.images.formatNonReconnuPng")
	}
	return saveBlob(content, ext, sessionID)
}

// fileExt : l'extension d'un fichier joint d'après son nom, réduite à quelques caractères alphanumériques (jamais un chemin).
func fileExt(name string) string {
	extension := strings.ToLower(filepath.Ext(filepath.Base(name)))
	if len(extension) < extensionMinLength || len(extension) > extensionMaxLength {
		return ".bin"
	}
	for _, character := range extension[1:] {
		isLowercaseLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		if !isLowercaseLetter && !isDigit {
			return ".bin"
		}
	}
	return extension
}

// saveBlob range des octets dans le dossier privé sous un nom aléatoire, préfixé par l'id de session quand il est connu (dropSessionFiles) (le nom d'origine ne sert qu'à l'extension).
func saveBlob(content []byte, ext, sessionID string) (string, error) {
	if !idRe.MatchString(sessionID) {
		sessionID = "" // sans session (nouvelle session, ou id invalide) : le fichier n'est retiré que par la purge de 14 jours
	} else {
		sessionID += "-"
	}
	dir := imagesDir()
	if mkdirErr := rMkdirAll(cfgDir(), dir, 0o700); mkdirErr != nil {
		return "", mkdirErr
	}
	if entries, listErr := readDirTrusted(dir); listErr == nil {
		for _, entry := range entries {
			if fileInfo, infoErr := entry.Info(); infoErr == nil && fileInfo.Mode().IsRegular() && time.Since(fileInfo.ModTime()) > imgKeep {
				rRemove(cfgDir(), filepath.Join(dir, entry.Name()))
			}
		}
	}
	var randomSuffix [4]byte
	rand.Read(randomSuffix[:])
	filePath := filepath.Join(dir, sessionID+time.Now().Format("20060102-150405")+"-"+hex.EncodeToString(randomSuffix[:])+ext)
	file, err := rOpenFile(cfgDir(), filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, err = file.Write(content)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		rRemove(cfgDir(), filePath)
		return "", err
	}
	return filePath, nil
}

// dropSessionFiles retire les images et fichiers joints déposés pour ces sessions (préfixe « <id>- »).
func dropSessionFiles(sessionIDs []string) {
	dir := imagesDir()
	entries, err := readDirTrusted(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		for _, sessionID := range sessionIDs {
			if idRe.MatchString(sessionID) && strings.HasPrefix(entry.Name(), sessionID+"-") {
				rRemove(cfgDir(), filepath.Join(dir, entry.Name()))
				break
			}
		}
	}
}

// lineAt lit la ligne de transcript qui commence à l'octet offset (au plus imgLine octets).
func lineAt(path string, offset int64) ([]byte, error) {
	file, err := openTrusted(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if offset < 0 {
		return nil, terr("srv.images.decalageNegatif")
	}
	if _, seekErr := file.Seek(offset, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	line, err := bufio.NewReaderSize(io.LimitReader(file, imgLine), jsonLineBufferSize).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return line, nil
}

// convImage extrait le bloc blockIndex d'une ligne de transcript (décalage offset) si c'est une image encodée en base64.
func convImage(path string, offset int64, blockIndex int) ([]byte, string, error) {
	imageNotFound := terr("srv.images.imageIntrouvable")
	if blockIndex < 0 || blockIndex > imageBlockMax {
		return nil, "", imageNotFound
	}
	line, err := lineAt(path, offset)
	if err != nil {
		return nil, "", imageNotFound
	}
	var record struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type   string `json:"type"`
				Source struct {
					Type string `json:"type"`
					Data string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &record) != nil {
		return nil, "", imageNotFound
	}
	isChatMessage := record.Type == "user" || record.Type == "assistant"
	if !isChatMessage || blockIndex >= len(record.Message.Content) {
		return nil, "", imageNotFound
	}
	block := record.Message.Content[blockIndex]
	if block.Type != "image" || block.Source.Type != "base64" {
		return nil, "", imageNotFound
	}
	data, err := base64.StdEncoding.DecodeString(block.Source.Data)
	if err != nil || imageExt(data) == "" {
		return nil, "", imageNotFound
	}
	return data, http.DetectContentType(data[:min(len(data), contentSniffBytes)]), nil
}

// uploadName : nom d'un fichier déposé (saveBlob) ; ni séparateur ni point de tête, donc jamais un chemin.
var uploadName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,200}$`)

// uploadImage ouvre une image déposée par son seul nom, dans le dossier privé : os.Root refuse tout lien symbolique qui en sortirait.
// Le type vient du contenu (imageExt), pas du nom.
func uploadImage(name string) (*os.File, string, error) {
	imageNotFound := terr("srv.images.imageIntrouvable")
	if !uploadName.MatchString(name) {
		return nil, "", imageNotFound
	}
	imagesRoot, err := os.OpenRoot(imagesDir())
	if err != nil {
		return nil, "", imageNotFound
	}
	defer imagesRoot.Close()
	file, err := imagesRoot.Open(name)
	if err != nil {
		return nil, "", imageNotFound
	}
	fileInfo, err := file.Stat()
	head := make([]byte, contentSniffBytes)
	headLength, _ := io.ReadFull(file, head)
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() > imgMax || imageExt(head[:headLength]) == "" {
		file.Close()
		return nil, "", imageNotFound
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		file.Close()
		return nil, "", imageNotFound
	}
	return file, http.DetectContentType(head[:headLength]), nil
}

func routesImages(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/uploads/{up}", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, isValid := pathID(responseWriter, request, "id")
		if !isValid {
			return
		}
		if _, exists := transcript(request.URL.Query().Get("dir"), sessionID); !exists {
			notFound(responseWriter)
			return
		}
		file, contentType, err := uploadImage(request.PathValue("up"))
		if err != nil {
			notFound(responseWriter)
			return
		}
		defer file.Close()
		headers := responseWriter.Header()
		headers.Set("Content-Type", contentType)
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		headers.Set("Cache-Control", "private, max-age=3600")
		io.Copy(responseWriter, file)
	})
	mux.HandleFunc("POST /v1/uploads/images", func(responseWriter http.ResponseWriter, request *http.Request) {
		content, err := io.ReadAll(http.MaxBytesReader(responseWriter, request.Body, imgMax+1))
		switch {
		case err != nil || len(content) > imgMax:
			writeErr(responseWriter, http.StatusRequestEntityTooLarge, "too_large", L("srv.images.imageVideOuTrop"))
			return
		case len(content) == 0:
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.images.imageVideOuTrop"))
			return
		case imageExt(content) == "":
			writeErr(responseWriter, http.StatusUnsupportedMediaType, "unsupported", L("srv.images.formatNonReconnuPng"))
			return
		}
		savedPath, err := saveImage(content, request.URL.Query().Get("id"))
		journal("image-upload (%d octets) : %v", len(content), errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, http.StatusCreated, map[string]any{"path": savedPath, "message": L("srv.images.imageAjoutee")})
	})
	mux.HandleFunc("POST /v1/uploads/files", func(responseWriter http.ResponseWriter, request *http.Request) {
		content, err := io.ReadAll(http.MaxBytesReader(responseWriter, request.Body, fileMax+1))
		switch {
		case err != nil || len(content) > fileMax:
			writeErr(responseWriter, http.StatusRequestEntityTooLarge, "too_large", L("srv.images.imageVideOuTrop"))
			return
		case len(content) == 0:
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.images.imageVideOuTrop"))
			return
		}
		name, _ := url.QueryUnescape(request.Header.Get("X-File-Name"))
		savedPath, err := saveBlob(content, fileExt(name), request.URL.Query().Get("id"))
		journal("file-upload (%d octets) : %v", len(content), errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, http.StatusCreated, map[string]any{"path": savedPath, "message": L("srv.images.imageAjoutee")})
	})
	// {img} : « <décalage du message>-<rang de l'image> ».
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/images/{img}", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, isValid := pathID(responseWriter, request, "id")
		if !isValid {
			return
		}
		transcriptPath, isValid := transcript(request.URL.Query().Get("dir"), sessionID)
		offsetText, indexText, hasSeparator := strings.Cut(request.PathValue("img"), "-")
		offset, offsetErr := strconv.ParseInt(offsetText, 10, 64)
		blockIndex, indexErr := strconv.Atoi(indexText)
		if !isValid || !hasSeparator || offsetErr != nil || indexErr != nil {
			notFound(responseWriter)
			return
		}
		data, contentType, err := convImage(transcriptPath, offset, blockIndex)
		if err != nil {
			notFound(responseWriter)
			return
		}
		headers := responseWriter.Header()
		headers.Set("Content-Type", contentType)
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		headers.Set("Cache-Control", "private, max-age=3600")
		responseWriter.Write(data)
	})
}
