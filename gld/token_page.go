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
<link rel="manifest" href="/manifest.webmanifest"><link rel="apple-touch-icon" href="/icons/apple-touch-icon.png">
<link rel="icon" href="/favicon.ico" sizes="any"><meta name="theme-color" content="#5a9a3e">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:17px -apple-system,system-ui,sans-serif;background:#111418;color:#e8eaed;padding:env(safe-area-inset-top) 1rem env(safe-area-inset-bottom)}
main{width:100%;max-width:22rem;text-align:center}p{color:#a0a4ab;line-height:1.5}.err{color:#f87171}
form{display:grid;gap:.75rem;margin-top:1.25rem}input{font:inherit;font-size:16px;min-height:44px;padding:0 .75rem;border:1px solid #3a3f47;border-radius:.6rem;background:#1b1f25;color:inherit}
button{font:inherit;font-weight:600;min-height:44px;padding:.75rem 1.75rem;border:0;border-radius:.6rem;background:#5a9a3e;color:#fff}
@media (prefers-color-scheme:light){body{background:#fff;color:#111418}p{color:#555}input{background:#fff;border-color:#ccc}}</style></head>
<body><main><h1>{{.Title}}</h1><p>{{.Text}}</p>{{if .Err}}<p class="err" role="alert">{{.Err}}</p>{{end}}
{{if .Action}}<form method="post" action="{{.Action}}">{{if .Hidden}}<input type="hidden" name="c" value="{{.Hidden}}">{{end}}
{{if .Field}}<input type="password" name="token" autocomplete="current-password" aria-label="{{.Field}}" placeholder="{{.Field}}" required autofocus>{{end}}
<button>{{.Button}}</button></form>{{end}}</main></body></html>
`))

type gateData struct {
	Lang, Title, Text, Err, Action, Hidden, Field, Button string
}

func gatePage(w http.ResponseWriter, code int, d gateData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; manifest-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(code)
	d.Lang = uiLang()
	gateTmpl.Execute(w, d)
}

// wantsPage : une navigation du navigateur (barre d'adresse, PWA), pas un appel de l'application.
func wantsPage(r *http.Request) bool {
	return r.Method == "GET" && (r.Header.Get("Sec-Fetch-Mode") == "navigate" || strings.Contains(r.Header.Get("Accept"), "text/html"))
}

func tokenPage(w http.ResponseWriter, code int, errKey string) {
	d := gateData{Title: "Glaude Code", Text: L("srv.token.text"), Action: "/token-login", Field: L("srv.token.field"), Button: L("srv.token.button")}
	if errKey != "" {
		d.Err = L(errKey)
	}
	gatePage(w, code, d)
}

// Essais du jeton par le formulaire : tokenMaxTries échecs par minute pour tout le serveur, puis 429 jusqu'à la minute
// suivante. Les appareils déjà connectés (cookie) ne passent pas par là.
// ponytail: compteur global, par adresse si un attaquant bloque le formulaire des autres.
const tokenMaxTries = 10

var tokenTries struct {
	sync.Mutex
	n     int
	since time.Time
}

func resetTokenTries() {
	tokenTries.Lock()
	tokenTries.n, tokenTries.since = 0, time.Time{}
	tokenTries.Unlock()
}

// tokenLogin : POST /token-login, formulaire de la page du jeton.
func tokenLogin(w http.ResponseWriter, r *http.Request, token string) {
	if pairCOP.Check(r) != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	now := authNow()
	tokenTries.Lock()
	if now.Sub(tokenTries.since) > time.Minute {
		tokenTries.n, tokenTries.since = 0, now
	}
	blocked := tokenTries.n >= tokenMaxTries
	tokenTries.Unlock()
	if blocked {
		w.Header().Set("Retry-After", "60")
		tokenPage(w, http.StatusTooManyRequests, "srv.token.tooMany")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<12)
	if !sameToken(r.PostFormValue("token"), token) {
		tokenTries.Lock()
		tokenTries.n++
		tokenTries.Unlock()
		journal("token: jeton refusé depuis %s", clientIP(r))
		tokenPage(w, http.StatusUnauthorized, "srv.token.bad")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: token, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
