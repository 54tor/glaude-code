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
)

func imagesDir() string { return filepath.Join(cfgDir(), imgPrefix) }

// imageExt : l'extension d'une image d'après son contenu (jamais d'après ce que dit le navigateur) ; vide si ce n'en est pas une.
// Seuls les formats raster courants passent : pas de SVG, qui peut porter du script.
func imageExt(b []byte) string {
	switch http.DetectContentType(b[:min(len(b), 512)]) {
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
func saveImage(b []byte, sid string) (string, error) {
	if len(b) == 0 || len(b) > imgMax {
		return "", terr("srv.images.imageVideOuTrop")
	}
	ext := imageExt(b)
	if ext == "" {
		return "", terr("srv.images.formatNonReconnuPng")
	}
	return saveBlob(b, ext, sid)
}

// fileExt : l'extension d'un fichier joint d'après son nom, réduite à quelques caractères alphanumériques (jamais un chemin).
func fileExt(name string) string {
	e := strings.ToLower(filepath.Ext(filepath.Base(name)))
	if len(e) < 2 || len(e) > 9 {
		return ".bin"
	}
	for _, c := range e[1:] {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return ".bin"
		}
	}
	return e
}

// saveBlob range des octets dans le dossier privé sous un nom aléatoire, préfixé par l'id de session quand il est connu (dropSessionFiles) (le nom d'origine ne sert qu'à l'extension).
func saveBlob(b []byte, ext, sid string) (string, error) {
	if !idRe.MatchString(sid) {
		sid = "" // sans session (nouvelle session, ou id invalide) : le fichier n'est retiré que par la purge de 14 jours
	} else {
		sid += "-"
	}
	dir := imagesDir()
	if err := rMkdirAll(cfgDir(), dir, 0o700); err != nil {
		return "", err
	}
	if ents, err := readDirTrusted(dir); err == nil {
		for _, e := range ents {
			if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() && time.Since(fi.ModTime()) > imgKeep {
				rRemove(cfgDir(), filepath.Join(dir, e.Name()))
			}
		}
	}
	var r [4]byte
	rand.Read(r[:])
	p := filepath.Join(dir, sid+time.Now().Format("20060102-150405")+"-"+hex.EncodeToString(r[:])+ext)
	f, err := rOpenFile(cfgDir(), p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		rRemove(cfgDir(), p)
		return "", err
	}
	return p, nil
}

// dropSessionFiles retire les images et fichiers joints déposés pour ces sessions (préfixe « <id>- »).
func dropSessionFiles(ids []string) {
	dir := imagesDir()
	ents, err := readDirTrusted(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		for _, id := range ids {
			if idRe.MatchString(id) && strings.HasPrefix(e.Name(), id+"-") {
				rRemove(cfgDir(), filepath.Join(dir, e.Name()))
				break
			}
		}
	}
}

// lineAt lit la ligne de transcript qui commence à l'octet o (au plus imgLine octets).
func lineAt(path string, o int64) ([]byte, error) {
	f, err := openTrusted(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if o < 0 {
		return nil, terr("srv.images.decalageNegatif")
	}
	if _, err := f.Seek(o, io.SeekStart); err != nil {
		return nil, err
	}
	line, err := bufio.NewReaderSize(io.LimitReader(f, imgLine), 1<<20).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return line, nil
}

// convImage extrait le k-ième bloc d'une ligne de transcript (décalage o) si c'est une image encodée en base64.
func convImage(path string, o int64, k int) ([]byte, string, error) {
	bad := terr("srv.images.imageIntrouvable")
	if k < 0 || k > 1000 {
		return nil, "", bad
	}
	line, err := lineAt(path, o)
	if err != nil {
		return nil, "", bad
	}
	var l struct {
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
	if json.Unmarshal(line, &l) != nil || (l.Type != "user" && l.Type != "assistant") || k >= len(l.Message.Content) {
		return nil, "", bad
	}
	b := l.Message.Content[k]
	if b.Type != "image" || b.Source.Type != "base64" {
		return nil, "", bad
	}
	data, err := base64.StdEncoding.DecodeString(b.Source.Data)
	if err != nil || imageExt(data) == "" {
		return nil, "", bad
	}
	return data, http.DetectContentType(data[:min(len(data), 512)]), nil
}

// uploadName : nom d'un fichier déposé (saveBlob) ; ni séparateur ni point de tête, donc jamais un chemin.
var uploadName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,200}$`)

// uploadImage ouvre une image déposée par son seul nom, dans le dossier privé : os.Root refuse tout lien symbolique qui en sortirait.
// Le type vient du contenu (imageExt), pas du nom.
func uploadImage(name string) (*os.File, string, error) {
	bad := terr("srv.images.imageIntrouvable")
	if !uploadName.MatchString(name) {
		return nil, "", bad
	}
	r, err := os.OpenRoot(imagesDir())
	if err != nil {
		return nil, "", bad
	}
	defer r.Close()
	f, err := r.Open(name)
	if err != nil {
		return nil, "", bad
	}
	st, err := f.Stat()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	if err != nil || !st.Mode().IsRegular() || st.Size() > imgMax || imageExt(head[:n]) == "" {
		f.Close()
		return nil, "", bad
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, "", bad
	}
	return f, http.DetectContentType(head[:n]), nil
}

func routesImages(mux *http.ServeMux) {
	mux.HandleFunc("GET /conv-upload", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if _, ok := transcript(q.Get("dir"), q.Get("id")); !ok {
			http.NotFound(w, r)
			return
		}
		f, typ, err := uploadImage(q.Get("name"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		h := w.Header()
		h.Set("Content-Type", typ)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("Cache-Control", "private, max-age=3600")
		io.Copy(w, f)
	})
	mux.HandleFunc("POST /image-upload", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, imgMax+1))
		var p string
		if err == nil {
			p, err = saveImage(b, r.URL.Query().Get("id"))
		}
		journal("image-upload (%d octets) : %v", len(b), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "path": p, "out": L("srv.images.imageAjoutee")})
	})
	mux.HandleFunc("POST /file-upload", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, fileMax+1))
		var p string
		if err == nil && (len(b) == 0 || len(b) > fileMax) {
			err = terr("srv.images.imageVideOuTrop")
		}
		if err == nil {
			name, _ := url.QueryUnescape(r.Header.Get("X-File-Name"))
			p, err = saveBlob(b, fileExt(name), r.URL.Query().Get("id"))
		}
		journal("file-upload (%d octets) : %v", len(b), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "path": p, "out": L("srv.images.imageAjoutee")})
	})
	mux.HandleFunc("GET /conv-image", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		p, ok := transcript(q.Get("dir"), q.Get("id"))
		o, e1 := strconv.ParseInt(q.Get("o"), 10, 64)
		k, e2 := strconv.Atoi(q.Get("k"))
		if !ok || e1 != nil || e2 != nil {
			http.NotFound(w, r)
			return
		}
		data, typ, err := convImage(p, o, k)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", typ)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("Cache-Control", "private, max-age=3600")
		w.Write(data)
	})
}
