package main

// Écoute hors de localhost (option --listen / GLD_WEB_LISTEN) : l'interface peut lire, supprimer et lancer des
// commandes (claude) pour le compte de l'utilisateur. Sur une adresse non locale, un jeton est donc exigé pour
// toute requête ; sans option, gld n'écoute que 127.0.0.1 et ne demande rien.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
)

const tokenCookie = "gld_token"

// isLoopback : « localhost » ou une adresse de boucle locale (127.0.0.0/8, ::1).
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	parsedIP := net.ParseIP(host)
	return parsedIP != nil && parsedIP.IsLoopback()
}

// newToken : 24 octets aléatoires, en hexadécimal.
func newToken() string {
	randomBytes := make([]byte, 24)
	rand.Read(randomBytes)
	return hex.EncodeToString(randomBytes)
}

// checkToken : un jeton fourni par l'utilisateur doit être assez long pour résister à l'essai systématique.
func checkToken(token string) error {
	if len(token) < 16 {
		return terr("srv.listen.gldWebTokenCaracteres")
	}
	return nil
}

// takeToken lit GLD_WEB_TOKEN (un jeton neuf hors localhost s'il manque, generated vaut alors true), puis retire les jetons
// de l'environnement : claude, tmux et les onglets lancés n'en héritent pas (un `env` exécuté par Claude finirait dans un
// transcript). En local, "" : pas de jeton.
func takeToken(local bool) (token string, generated bool, err error) {
	token = os.Getenv("GLD_WEB_TOKEN")
	os.Unsetenv("GLD_WEB_TOKEN")
	os.Unsetenv(childTokenEnv)
	if childToken != "" {
		if hideErr := hideEnviron(); hideErr != nil {
			journal("hideEnviron : %v", hideErr)
		}
	}
	if token != "" {
		return token, false, checkToken(token)
	}
	if !local {
		return newToken(), true, nil
	}
	return "", false, nil
}

// tokenShown : le jeton tel qu'imprimé dans l'adresse à ouvrir. Celui que fournit l'utilisateur n'est jamais écrit : stderr
// finit dans des journaux (systemd les conserve), et il le connaît déjà.
func tokenShown(token string, generated bool) string {
	if generated {
		return token
	}
	return "$GLD_WEB_TOKEN"
}

// wrongTokenPresented : la requête porte un jeton (cookie, Bearer ou ?token=) que withToken vient de refuser.
// Sans cela, seul le formulaire était limité : Bearer, cookie et ?token= se devinaient à la vitesse du réseau.
func wrongTokenPresented(request *http.Request) bool {
	if _, err := request.Cookie(tokenCookie); err == nil {
		return true
	}
	if _, hasBearer := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer "); hasBearer {
		return true
	}
	return request.URL.Query().Has("token")
}

func sameToken(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

// withToken : exige le jeton, en cookie (posé par ?token=…, puis la requête est redirigée sans le jeton dans l'adresse)
// ou en en-tête « Authorization: Bearer … » (curl).
func withToken(token string, next http.Handler) http.Handler {
	setDevKey(token)
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if cookie, cookieErr := request.Cookie(tokenCookie); cookieErr == nil && sameToken(cookie.Value, token) {
			next.ServeHTTP(responseWriter, request)
			return
		}
		if deviceCookie, deviceCookieErr := request.Cookie(devCookie); deviceCookieErr == nil && deviceOK(deviceCookie.Value) {
			next.ServeHTTP(responseWriter, request)
			return
		}
		if (request.Method == "GET" || request.Method == "HEAD") && publicAsset(request.URL.Path) {
			next.ServeHTTP(responseWriter, request)
			return
		}
		if request.Method == "POST" && request.URL.Path == "/token-login" {
			tokenLogin(responseWriter, request, token)
			return
		}
		if request.URL.Path == "/pair" { // confirmation et échange d'un code de jumelage, avant tout jeton
			pairEntry(responseWriter, request, nil)
			return
		}
		if bearerToken, hasBearer := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer "); hasBearer && sameToken(bearerToken, token) {
			next.ServeHTTP(responseWriter, request)
			return
		}
		if query := request.URL.Query(); sameToken(query.Get("token"), token) && query.Has("token") {
			http.SetCookie(responseWriter, &http.Cookie{Name: tokenCookie, Value: token, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: requestHTTPS(request), SameSite: http.SameSiteStrictMode})
			query.Del("token")
			redirectURL := url.URL{Path: "/" + strings.TrimLeft(request.URL.Path, "/"), RawQuery: query.Encode()} // jamais « //hôte » : redirection locale seulement
			http.Redirect(responseWriter, request, redirectURL.String(), http.StatusSeeOther)
			return
		}
		if _, staleCookieErr := request.Cookie(tokenCookie); staleCookieErr == nil {
			// Jeton changé depuis : sans effacement, chaque requête de l'interface rouvrirait un essai et bloquerait l'adresse,
			// /token-login compris.
			http.SetCookie(responseWriter, &http.Cookie{Name: tokenCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestHTTPS(request), SameSite: http.SameSiteStrictMode})
		}
		if wrongTokenPresented(request) && !tokenTry(clientIP(request)) { // chaque jeton faux présenté compte, comme au formulaire
			responseWriter.Header().Set("Retry-After", "60")
			responseWriter.Header().Set("Cache-Control", "no-store")
			if strings.HasPrefix(request.URL.Path, "/v1/") {
				authMsg(responseWriter, http.StatusTooManyRequests, L("srv.token.tooMany"))
				return
			}
			http.Error(responseWriter, L("srv.token.tooMany"), http.StatusTooManyRequests)
			return
		}
		// Navigateur ou PWA sans cookie : saisir le jeton plutôt qu'un 401 en texte. La racine sert toujours la page : iOS relit
		// ses métadonnées sans Accept text/html (feuille de partage, écran d'accueil) et, sur un 401, dessine une lettre en guise d'icône.
		if wantsPage(request) || (request.Method == "GET" && request.URL.Path == "/" && request.URL.RawQuery == "") {
			tokenPage(responseWriter, http.StatusOK, "")
			return
		}
		responseWriter.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(request.URL.Path, "/v1/") {
			authMsg(responseWriter, http.StatusUnauthorized, L("srv.listen.tokenRequired"))
			return
		}
		http.Error(responseWriter, L("srv.listen.tokenRequired"), http.StatusUnauthorized)
	})
}

// publicAsset : manifeste et icônes, chargés par le navigateur sans cookie (manifeste, icône d'écran d'accueil d'iOS) ;
// fichiers fixes de l'interface, sans donnée. Le chemin doit être déjà propre : « /icons/../x » ne passe pas.
// rootIcons : icônes du manifeste et favicon PNG, à la racine comme les génère RealFaviconGenerator.
var rootIcons = []string{"/favicon-96x96.png", "/web-app-manifest-192x192.png", "/web-app-manifest-512x512.png"}

func publicAsset(requestPath string) bool {
	if slices.Contains(rootIcons, requestPath) {
		return true
	}
	return path.Clean(requestPath) == requestPath && (requestPath == "/site.webmanifest" || requestPath == "/favicon.ico" || requestPath == "/apple-touch-icon.png" || requestPath == "/apple-touch-icon-precomposed.png" || strings.HasPrefix(requestPath, "/icons/"))
}

// withTokenOpen : en mode ouvert, withToken tel quel. En mode multi-utilisateur, le login remplace le jeton pour le navigateur :
// ni cookie gld_token ni ?token=, le jeton ne vaut plus qu'en Bearer (tokenForChildren), jugé par le garde.
func withTokenOpen(token string, next http.Handler) http.Handler {
	tokenHandler := withToken(token, next)
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		request, decision := reqMode(request)
		if decision.on || decision.err != nil { // erreur : pas de jeton à juger, le handler refuse le service
			next.ServeHTTP(responseWriter, request)
			return
		}
		tokenHandler.ServeHTTP(responseWriter, request)
	})
}

// tokenForChildren : principal du jeton, en en-tête Authorization seulement ; sans jeton configuré, aucun Bearer n'est valable.
// Un jeton généré (écoute hors localhost sans GLD_WEB_TOKEN) ne vaut pas non plus : imprimé au démarrage, il donnerait
// tous les droits sans mot de passe ni second facteur. Seul un jeton fixé par l'opérateur est accepté.
func tokenForChildren(token string, generated bool) func(*http.Request) *Principal {
	return func(request *http.Request) *Principal {
		bearerToken, hasBearer := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if token == "" || generated || !hasBearer || !sameToken(bearerToken, token) {
			return nil
		}
		return &Principal{Token: true}
	}
}

// tlsPair : HTTPS servi par gld quand un certificat et sa clé sont donnés ; l'un sans l'autre est une erreur, pas un repli en HTTP.
func tlsPair(cert, key string) (bool, error) {
	if (cert == "") != (key == "") {
		return false, terr("srv.listen.tlsPair")
	}
	return cert != "", nil
}

// urlsFor : les adresses à ouvrir pour une écoute sur host:port (toutes les interfaces si host est « 0.0.0.0 » ou « :: ») ;
// sans jeton, l'adresse nue.
func urlsFor(scheme, host, port, token string) []string {
	hosts := []string{host}
	if parsedIP := net.ParseIP(host); parsedIP != nil && parsedIP.IsUnspecified() {
		hosts = nil
		addrs, _ := net.InterfaceAddrs()
		for _, address := range addrs {
			if network, isIPNet := address.(*net.IPNet); isIPNet && !network.IP.IsLoopback() && network.IP.To4() != nil {
				hosts = append(hosts, network.IP.String())
			}
		}
	}
	var urls []string
	for _, hostName := range hosts {
		link := scheme + "://" + net.JoinHostPort(hostName, port) + "/"
		if token != "" {
			link += "?token=" + token
		}
		urls = append(urls, link)
	}
	return urls
}
