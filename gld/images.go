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
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	imgMax    = 12 << 20 // taille d'une image déposée
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
func saveImage(b []byte) (string, error) {
	if len(b) == 0 || len(b) > imgMax {
		return "", terr("srv.images.imageVideOuTrop")
	}
	ext := imageExt(b)
	if ext == "" {
		return "", terr("srv.images.formatNonReconnuPng")
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
	p := filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+hex.EncodeToString(r[:])+ext)
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

func routesImages(mux *http.ServeMux) {
	mux.HandleFunc("POST /image-upload", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, imgMax+1))
		var p string
		if err == nil {
			p, err = saveImage(b)
		}
		journal("image-upload (%d octets) : %v", len(b), errOr(err))
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
