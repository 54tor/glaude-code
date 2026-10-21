package main

// Écoute hors de localhost (option --listen / GLD_WEB_LISTEN) : l'interface peut lire, supprimer et lancer des
// commandes (claude) pour le compte de l'utilisateur. Sur une adresse non locale, un jeton est donc exigé pour
// toute requête ; sans option, gld-web n'écoute que 127.0.0.1 et ne demande rien.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const tokenCookie = "gld_token"

// isLoopback : « localhost » ou une adresse de boucle locale (127.0.0.0/8, ::1).
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// newToken : 24 octets aléatoires, en hexadécimal.
func newToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// checkToken : un jeton fourni par l'utilisateur doit être assez long pour résister à l'essai systématique.
func checkToken(t string) error {
	if len(t) < 16 {
		return errors.New("GLD_WEB_TOKEN : 16 caractères au moins")
	}
	return nil
}

func sameToken(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// withToken : exige le jeton, en cookie (posé par ?token=…, puis la requête est redirigée sans le jeton dans l'adresse)
// ou en en-tête « Authorization: Bearer … » (curl).
func withToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(tokenCookie); err == nil && sameToken(c.Value, token) {
			next.ServeHTTP(w, r)
			return
		}
		if h, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && sameToken(h, token) {
			next.ServeHTTP(w, r)
			return
		}
		if q := r.URL.Query(); sameToken(q.Get("token"), token) && q.Has("token") {
			http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: token, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			q.Del("token")
			u := url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
			http.Redirect(w, r, u.String(), http.StatusSeeOther)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "Jeton requis : ouvrez l'adresse affichée au démarrage de gld-web (…/?token=…).", http.StatusUnauthorized)
	})
}

// urlsFor : les adresses à ouvrir pour une écoute sur host:port (toutes les interfaces si host est « 0.0.0.0 » ou « :: »).
func urlsFor(host, port, token string) []string {
	hosts := []string{host}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		hosts = nil
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
				hosts = append(hosts, n.IP.String())
			}
		}
	}
	var out []string
	for _, h := range hosts {
		out = append(out, "http://"+net.JoinHostPort(h, port)+"/?token="+token)
	}
	return out
}
