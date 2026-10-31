package main

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"strings"
)

// routesStatic : la page de l'interface, ses ressources (embarquées dans le binaire) et leurs types MIME.
func routesStatic(mux *http.ServeMux) {
	// Sous Windows le registre peut associer .js à text/plain, ce qui casse les modules : on fixe les types.
	staticContentTypes := map[string]string{
		".js":          "text/javascript; charset=utf-8",
		".css":         "text/css; charset=utf-8",
		".woff2":       "font/woff2",
		".svg":         "image/svg+xml",
		".png":         "image/png",
		".ico":         "image/x-icon",
		".webmanifest": "application/manifest+json",
	}
	for extension, contentType := range staticContentTypes {
		mime.AddExtensionType(extension, contentType)
	}
	dist, _ := fs.Sub(uiFS, "ui/dist")
	fileServer := http.FileServerFS(dist)
	// FileServer liste un dossier demandé avec « / » final : la liste des bundles n'a rien à faire en public.
	files := http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/") {
			http.NotFound(responseWriter, request)
			return
		}
		fileServer.ServeHTTP(responseWriter, request)
	})
	// La page de l'interface, servie aussi pour ses chemins (routage côté navigateur).
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil { // binaire compilé sans `make ui` : on le dit plutôt que de servir une page vide
		fmt.Fprintln(os.Stderr, "gld : interface absente du binaire (ui/dist vide) : lancer `make install`")
	}
	page := func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; base-uri 'self'; frame-ancestors 'none'")
		responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
		responseWriter.Header().Set("Cache-Control", "no-cache") // elle désigne les /assets/ de la version servie
		responseWriter.Write(index)
	}
	pagePatterns := []string{
		"GET /{$}", "GET /accueil", "GET /toutes", "GET /en-cours", "GET /dossier/", "GET /groupe/", "GET /projet/",
		"GET /snippets", "GET /snippets/", "GET /memoire", "GET /memoire/", "GET /onglets",
		"GET /configuration", "GET /configuration/", "GET /skills", "GET /skills/", "GET /extensions", "GET /plugins",
		"GET /sauvegarde", "GET /equipes", "GET /equipes/", "GET /revues", "GET /espace/", "GET /reglages/",
		"GET /guest", "GET /guest/{handle}",
	}
	for _, pattern := range pagePatterns {
		mux.HandleFunc(pattern, page)
	}
	// Noms hachés par le build : un fichier de /assets/ ne change jamais, navigateur et CDN le gardent un an.
	mux.Handle("GET /assets/", http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(responseWriter, request)
	}))
	// Icônes et manifeste (ui/public) : favicon, icônes d'application.
	mux.Handle("GET /icons/", files)
	mux.Handle("GET /favicon.ico", files)
	// Servi comme chez crawl-center (application/octet-stream) : c'est la configuration dont l'icône iOS d'écran d'accueil marche.
	mux.HandleFunc("GET /site.webmanifest", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/octet-stream")
		files.ServeHTTP(responseWriter, request)
	})
	mux.Handle("GET /favicon-96x96.png", files)
	mux.Handle("GET /web-app-manifest-192x192.png", files)
	mux.Handle("GET /web-app-manifest-512x512.png", files)
	// iOS (feuille de partage, écran d'accueil) relit la page sans cookie : la 401 n'a pas de <link>, il cherche alors ces noms
	// à la racine, puis se rabat sur le favicon de 48 px.
	touch := func(responseWriter http.ResponseWriter, request *http.Request) {
		http.ServeFileFS(responseWriter, request, dist, "icons/apple-touch-icon.png")
	}
	mux.HandleFunc("GET /apple-touch-icon.png", touch)
	mux.HandleFunc("GET /apple-touch-icon-precomposed.png", touch)
	// Service worker : à la racine pour couvrir tout le site ; jamais mis en cache lui-même, pour que sa mise à jour soit vue tout de suite.
	mux.HandleFunc("GET /sw.js", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(responseWriter, request)
	})
}
