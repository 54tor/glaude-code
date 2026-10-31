package main

// Pages servies hors connexion (mode jeton) : saisie du jeton et confirmation d'un jumelage. Elles portent le manifeste,
// les icônes et les métas d'iOS : « Sur l'écran d'accueil » depuis l'une d'elles installe une PWA complète, et une PWA
// qui a perdu son cookie peut se reconnecter sans repasser par Safari.

import (
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

var gateTmpl = template.Must(template.New("gate").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>{{.Title}}</title>
<link rel="manifest" href="/site.webmanifest"><link rel="apple-touch-icon" href="/icons/gld-touch-1024.png">
<link rel="icon" type="image/png" href="/icons/gld-favicon.png" sizes="96x96"><meta name="theme-color" content="#111418">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-title" content="Glaude">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:17px -apple-system,system-ui,sans-serif;background:#111418;color:#e8eaed;padding:env(safe-area-inset-top) 1rem env(safe-area-inset-bottom)}
*{box-sizing:border-box}main{width:100%;max-width:22rem;text-align:center}p{color:#a0a4ab;line-height:1.5}.err{color:#f87171}
form{display:grid;gap:.75rem;margin-top:1.25rem}input{width:100%;min-width:0;font:inherit;font-size:16px;min-height:44px;padding:0 .75rem;border:1px solid #6f7681;border-radius:.6rem;background:#1b1f25;color:inherit}
.code{font:600 1.6rem ui-monospace,monospace;letter-spacing:.15em;text-align:center}a{color:#7cc15a}
button{font:inherit;font-weight:600;min-height:44px;padding:.75rem 1.75rem;border:0;border-radius:.6rem;background:#3f7a2a;color:#fff}
@media (prefers-color-scheme:light){body{background:#fff;color:#111418}p{color:#555}a{color:#3f7a2a}input{background:#fff;border-color:#8a8a8a}}</style></head>
<body><main><h1>{{.Title}}</h1><p>{{.Text}}</p>{{if .Err}}<p class="err" role="alert">{{.Err}}</p>{{end}}
{{if .Action}}<form method="post" action="{{.Action}}">{{if .Hidden}}<input type="hidden" name="c" value="{{.Hidden}}">{{end}}
{{if .Field}}<input type="password" name="token" autocomplete="current-password" aria-label="{{.Field}}" placeholder="{{.Field}}" required autofocus>{{end}}
{{if .Code}}<input class="code" name="c" inputmode="numeric" autocomplete="one-time-code" maxlength="12" aria-label="{{.Title}}" placeholder="0000 0000" required autofocus>{{end}}
<button>{{.Button}}</button></form>{{end}}{{if .Link}}<p><a href="{{.Link}}">{{.LinkText}}</a></p>{{end}}</main></body></html>
`))

type gateData struct {
	Lang, Title, Text, Err, Action, Hidden, Field, Button, Link, LinkText string
	Code                                                                  bool // champ du code de jumelage
}

func gatePage(responseWriter http.ResponseWriter, code int, pageData gateData) {
	headers := responseWriter.Header()
	headers.Set("Content-Type", "text/html; charset=utf-8")
	headers.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; manifest-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	headers.Set("Cache-Control", "no-store")
	headers.Set("Referrer-Policy", "no-referrer")
	responseWriter.WriteHeader(code)
	pageData.Lang = uiLang()
	gateTmpl.Execute(responseWriter, pageData)
}

// wantsPage : une navigation du navigateur (barre d'adresse, PWA), pas un appel de l'application.
func wantsPage(request *http.Request) bool {
	return request.Method == "GET" && (request.Header.Get("Sec-Fetch-Mode") == "navigate" || strings.Contains(request.Header.Get("Accept"), "text/html"))
}

func tokenPage(responseWriter http.ResponseWriter, code int, errKey string) {
	pageData := gateData{Title: "Glaude Code", Text: L("srv.token.text"), Action: "/token-login", Field: L("srv.token.field"), Button: L("srv.token.button"), Link: "/pair", LinkText: L("srv.pair.title")}
	if errKey != "" {
		pageData.Err = L(errKey)
	}
	gatePage(responseWriter, code, pageData)
}

// Essais du jeton par le formulaire, par minute : tokenMaxTries échecs par adresse (clientIP, derrière un proxy déclaré
// dans GLD_TRUSTED_PROXIES), et tokenMaxGlobal pour tout le serveur contre un essai réparti sur beaucoup d'adresses.
// Les appareils déjà connectés (cookie) ne passent pas par là.
const (
	tokenMaxTries  = 10
	tokenMaxGlobal = 100
)

type tryWin struct {
	n     int
	since time.Time
}

func (window *tryWin) at(now time.Time) *tryWin {
	if now.Sub(window.since) > time.Minute {
		window.n, window.since = 0, now
	}
	return window
}

var tokenTries = struct {
	sync.Mutex
	all tryWin
	by  map[string]*tryWin
}{by: map[string]*tryWin{}}

func resetTokenTries() {
	tokenTries.Lock()
	tokenTries.all, tokenTries.by = tryWin{}, map[string]*tryWin{}
	tokenTries.Unlock()
}

// tokenTry réserve un essai, sous le même verrou que le contrôle : des requêtes simultanées ne passent pas toutes avant
// le premier échec. false : l'adresse ou le serveur a épuisé ses essais. Un essai réussi se rend par tokenRefund.
func tokenTry(clientAddress string) bool {
	now := authNow()
	tokenTries.Lock()
	defer tokenTries.Unlock()
	addressWindow := tokenTries.by[clientAddress]
	if addressWindow == nil {
		if len(tokenTries.by) >= 4096 { // ponytail: purge complète au plafond, les fenêtres ne durent qu'une minute
			tokenTries.by = map[string]*tryWin{}
		}
		addressWindow = &tryWin{}
		tokenTries.by[clientAddress] = addressWindow
	}
	addressWindow.at(now)
	tokenTries.all.at(now)
	if addressWindow.n >= tokenMaxTries || tokenTries.all.n >= tokenMaxGlobal {
		return false
	}
	addressWindow.n++
	tokenTries.all.n++
	return true
}

func tokenRefund(clientAddress string) {
	tokenTries.Lock()
	defer tokenTries.Unlock()
	if addressWindow := tokenTries.by[clientAddress]; addressWindow != nil && addressWindow.n > 0 {
		addressWindow.n--
	}
	if tokenTries.all.n > 0 {
		tokenTries.all.n--
	}
}

// tokenLogin : POST /token-login, formulaire de la page du jeton.
func tokenLogin(responseWriter http.ResponseWriter, request *http.Request, token string) {
	if pairCOP.Check(request) != nil {
		http.Error(responseWriter, "forbidden", http.StatusForbidden)
		return
	}
	clientAddress := clientIP(request)
	if !tokenTry(clientAddress) {
		responseWriter.Header().Set("Retry-After", "60")
		tokenPage(responseWriter, http.StatusTooManyRequests, "srv.token.tooMany")
		return
	}
	request.Body = http.MaxBytesReader(responseWriter, request.Body, 1<<12)
	if !sameToken(request.PostFormValue("token"), token) {
		journal("token: jeton refusé depuis %s", clientAddress)
		tokenPage(responseWriter, http.StatusUnauthorized, "srv.token.bad")
		return
	}
	tokenRefund(clientAddress)
	http.SetCookie(responseWriter, &http.Cookie{Name: tokenCookie, Value: token, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: requestHTTPS(request), SameSite: http.SameSiteStrictMode})
	http.Redirect(responseWriter, request, "/", http.StatusSeeOther)
}
