package main

import (
	"io/fs"
	"mime"
	"net/http"
)

// routesStatic : la page de l'interface, ses ressources (embarquées dans le binaire) et leurs types MIME.
func routesStatic(mux *http.ServeMux) {
	// Sous Windows le registre peut associer .js à text/plain, ce qui casse les modules : on fixe les types.
	for ext, typ := range map[string]string{".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".woff2": "font/woff2", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".webmanifest": "application/manifest+json"} {
		mime.AddExtensionType(ext, typ)
	}
	dist, _ := fs.Sub(uiFS, "ui/dist")
	files := http.FileServerFS(dist)
	// La page de l'interface, servie aussi pour ses chemins (routage côté navigateur).
	index, _ := fs.ReadFile(dist, "index.html")
	page := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	}
	for _, p := range []string{"GET /{$}", "GET /toutes", "GET /en-cours", "GET /dossier/", "GET /groupe/", "GET /snippets", "GET /snippets/", "GET /memoire", "GET /memoire/", "GET /onglets", "GET /configuration"} {
		mux.HandleFunc(p, page)
	}
	mux.Handle("GET /assets/", files)
	// Icônes et manifeste (ui/public) : favicon, icônes d'application.
	mux.Handle("GET /icons/", files)
	mux.Handle("GET /favicon.ico", files)
	mux.Handle("GET /manifest.webmanifest", files)
}
