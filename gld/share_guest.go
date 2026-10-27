package main

// Garde invité : un cookie invité valide ne donne accès qu'à une liste blanche de routes, toujours sur la session partagée.
// Elle passe après les gardes Host et childAuthorized, avant le jeton hôte et le garde des comptes (handler, main.go) : un
// invité n'a ni jeton ni compte. Sans cookie invité valide, la requête continue telle quelle comme requête hôte.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	guestCookie = "gld_guest" // "<home échappé>.<secret>" : le préfixe sert au routage, le secret à l'authentification
	// Posés par la passerelle sur la requête relayée au gld enfant qui possède le partage (childProxy) ; jamais lus ailleurs.
	guestHeader      = "X-Gld-Guest"
	guestHTTPSHeader = "X-Gld-Guest-HTTPS"
	invitePat        = "GET /invite/{home}/{token}"
	invitePostPat    = "POST /invite/{home}/{token}"
	guestAddMax      = 10
	guestBodyMax     = sendMax + 1<<10
)

var guestRoutes = map[string]bool{
	"GET /conv": true, "GET /conv-text": true, "GET /conv-tools": true, "GET /conv-image": true,
	"GET /session-live": true, "GET /session-state": true, "GET /session-task-output": true, "GET /session-screen": true,
	"GET /queue": true, "POST /queue-add": true, "POST /queue-rm": true, "GET /guest-me": true, "POST /guest-leave": true,
	"GET /guest": true, "GET /assets/": true, "GET /icons/": true, "GET /favicon.ico": true, "GET /manifest.webmanifest": true,
}

type guestRelayKey struct{}

var guestAdds = struct {
	sync.Mutex
	by map[string]*tryWin
}{by: map[string]*tryWin{}}

func guestAddTry(session string) bool {
	guestAdds.Lock()
	defer guestAdds.Unlock()
	w := guestAdds.by[session]
	if w == nil {
		w = &tryWin{}
		guestAdds.by[session] = w
	}
	if w.at(time.Now()).n >= guestAddMax {
		return false
	}
	w.n++
	return true
}

// guestCSRF : dérivé du secret, rendu à la page par /guest-me ; une autre origine ne peut ni le lire ni le calculer.
func guestCSRF(secret string) string {
	h := sha256.Sum256([]byte("gld-guest-csrf\x00" + secret))
	return hex.EncodeToString(h[:])
}

func guestDeny(w http.ResponseWriter, code int, key string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, code, map[string]any{"ok": false, "out": L(key)})
}

type noStoreWriter struct {
	http.ResponseWriter
	done bool
	code int
}

func (n *noStoreWriter) WriteHeader(c int) {
	if !n.done {
		n.done, n.code = true, c
		n.Header().Set("Cache-Control", "no-store")
	}
	n.ResponseWriter.WriteHeader(c)
}

func (n *noStoreWriter) Write(b []byte) (int, error) {
	if !n.done {
		n.WriteHeader(http.StatusOK)
	}
	return n.ResponseWriter.Write(b)
}

func (n *noStoreWriter) Unwrap() http.ResponseWriter { return n.ResponseWriter }

func guestShare(sec string) (Share, bool) {
	if sec == "" || shareSt == nil {
		return Share{}, false
	}
	return shareSt.ByCookie(sec)
}

// guestKnown : la passerelle ne possède pas les partages d'un home enfant ; elle lit son fichier pour savoir si le cookie
// mérite le relais (sinon : requête hôte). L'enfant revalide le secret dans sa propre mémoire, qui fait foi.
func guestKnown(home, sec string) bool {
	h, ok := homeByName(home)
	if !ok || sec == "" {
		return false
	}
	st, err := openShares(h.Dir)
	if err != nil {
		return false
	}
	hs := hashSecret(sec)
	for _, sh := range st.m {
		if hashEq(sh.CookieHash, hs) {
			return true
		}
	}
	return false
}

// guestPattern : le motif du mux, seulement s'il est dans la liste blanche et que le chemin est canonique (ni « // », ni
// encodage, ni « / » final ajouté) : le mux redirigerait ces variantes, la garde les refuse.
func guestPattern(mux *http.ServeMux, r *http.Request) string {
	if r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path {
		return ""
	}
	_, pat := mux.Handler(r)
	if !guestRoutes[pat] {
		return ""
	}
	if _, p, _ := strings.Cut(pat, " "); !strings.HasSuffix(p, "/") && p != r.URL.Path {
		return ""
	}
	return pat
}

func guestCSRFOK(r *http.Request, sec string) bool {
	return r.Method != http.MethodPost || subtle.ConstantTimeCompare([]byte(r.Header.Get(csrfHeader)), []byte(guestCSRF(sec))) == 1
}

func guestGate(mux *http.ServeMux, next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pat := mux.Handler(r); pat == invitePat || pat == invitePostPat {
			cop.Handler(http.HandlerFunc(guestInvite)).ServeHTTP(w, r)
			return
		}
		if childToken != "" { // enfant : seule la passerelle parle (childAuthorized), le secret arrive en en-tête
			sec := r.Header.Get(guestHeader)
			if sec == "" {
				next.ServeHTTP(w, r)
				return
			}
			// La passerelle n'a pas jugé ce client comme hôte : un secret refusé ici ne retombe jamais sur l'hôte.
			sh, ok := guestShare(sec)
			if !ok {
				guestDeny(w, 403, "srv.share_guest.refuse")
				return
			}
			cop.Handler(guestServe(mux, sh, sec)).ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(guestCookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		home, sec := "", ""
		if i := strings.LastIndexByte(c.Value, '.'); i >= 0 {
			home, _ = url.QueryUnescape(c.Value[:i])
			sec = c.Value[i+1:]
		}
		if home == "" {
			if sh, ok := guestShare(sec); ok {
				cop.Handler(guestServe(mux, sh, sec)).ServeHTTP(w, r)
				return
			}
		} else if guestKnown(home, sec) {
			cop.Handler(guestRelay(mux, home, sec)).ServeHTTP(w, r)
			return
		}
		// Partage terminé ou cookie altéré : l'hôte qui a ouvert un lien dans son navigateur ne doit pas rester bloqué.
		http.SetCookie(w, &http.Cookie{Name: guestCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteStrictMode})
		next.ServeHTTP(w, r)
	})
}

// guestRelay : passerelle, partage d'un home enfant ; liste blanche et CSRF jugés ici, le reste par l'enfant.
func guestRelay(mux *http.ServeMux, home, sec string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pat := guestPattern(mux, r)
		if pat == "" || !guestCSRFOK(r, sec) {
			guestDeny(w, 403, "srv.share_guest.refuse")
			return
		}
		if pat == "POST /guest-leave" { // ici : un enfant qui refuse le secret (fin non écrite) ne doit pas l'empêcher
			guestLeave(w, r)
			return
		}
		p, err := homeProxy(home)
		if err != nil {
			journal("homes %s : %v", home, err)
			guestDeny(w, 503, "srv.homes.indisponible")
			return
		}
		p.ServeHTTP(&noStoreWriter{ResponseWriter: w}, r.WithContext(context.WithValue(r.Context(), guestRelayKey{}, sec)))
	})
}

func guestServe(mux *http.ServeMux, sh Share, sec string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pat := guestPattern(mux, r)
		if pat == "" || !guestCSRFOK(r, sec) {
			guestDeny(w, 403, "srv.share_guest.refuse")
			return
		}
		if pat == "POST /guest-leave" {
			guestLeave(w, r)
			return
		}
		if pat == "GET /guest-me" {
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, 200, map[string]any{"id": sh.Session, "dir": sh.Dir, "mode": sh.Mode, "csrf": guestCSRF(sec)})
			return
		}
		r2 := r.Clone(r.Context())
		q := r2.URL.Query()
		q.Set("id", sh.Session)
		q.Set("dir", sh.Dir)
		r2.URL.RawQuery = q.Encode()
		r2.Header.Set("X-Gld-Author", "guest")
		if r.Method == http.MethodPost {
			// Corps reconstruit : seuls item et texte viennent du client, id choisi par le partage.
			var in struct{ Item, Text string }
			b, err := io.ReadAll(io.LimitReader(r.Body, guestBodyMax+1))
			if err != nil || len(b) > guestBodyMax || json.Unmarshal(b, &in) != nil {
				guestDeny(w, 400, "srv.http_util.requeteInvalide")
				return
			}
			if queueSt == nil {
				guestDeny(w, 503, "srv.queue.indisponible")
				return
			}
			switch pat {
			case "POST /queue-add":
				if sh.Mode != "rw" {
					guestDeny(w, 403, "srv.share_guest.lectureSeule")
					return
				}
				if !guestAddTry(sh.Session) {
					w.Header().Set("Retry-After", "60")
					guestDeny(w, 429, "srv.share_guest.tropDeMessages")
					return
				}
			case "POST /queue-rm":
				for _, it := range queueSt.List(sh.Session) {
					if it.ID == in.Item && it.Author != "guest" {
						guestDeny(w, 403, "srv.share_guest.refuse")
						return
					}
				}
			}
			nb, _ := json.Marshal(map[string]string{"id": sh.Session, "item": in.Item, "text": in.Text})
			r2.Body, r2.ContentLength = io.NopCloser(bytes.NewReader(nb)), int64(len(nb))
		}
		mux.ServeHTTP(&noStoreWriter{ResponseWriter: w}, r2)
	})
}

// guestLeave : efface le cookie invité sans toucher au partage ; l'issue d'un hôte qui a ouvert son propre lien.
func guestLeave(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: guestCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteStrictMode})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// hostCredential : la requête porte un accès hôte valide (jeton, appareil jumelé, session de compte) ; jugée par la
// passerelle, seule à voir les cookies.
func hostCredential(r *http.Request) bool {
	if hostToken != "" {
		if c, err := r.Cookie(tokenCookie); err == nil && sameToken(c.Value, hostToken) {
			return true
		}
		if c, err := r.Cookie(devCookie); err == nil && deviceOK(c.Value) {
			return true
		}
	}
	if _, d := reqMode(r); d.on && d.st != nil {
		if p, _ := gatePrincipal(d.st, r, true); p != nil {
			return true
		}
	}
	return false
}

// guestInvite : /invite/{home}/{token} ; « main » désigne le home du processus principal. Le GET n'affiche qu'un bouton :
// un aperçu de lien (messagerie, antivirus de courriel) ne doit pas consommer le jeton à usage unique. Le POST l'échange ;
// chaque essai compte dans la même limite que le jeton hôte (par adresse et pour tout le serveur), un succès est rendu.
func guestInvite(w http.ResponseWriter, r *http.Request) {
	refused := func(code int, key string) {
		gatePage(w, code, gateData{Title: L("srv.share_guest.titre"), Text: L(key)})
	}
	home, tok, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/invite/"), "/")
	if !ok || home == "" || tok == "" || strings.Contains(tok, "/") {
		refused(403, "srv.share_guest.lienInvalide")
		return
	}
	gateway := childToken == ""
	if gateway && hostCredential(r) {
		refused(200, "srv.share_guest.vousEtesHote")
		return
	}
	if r.Method != http.MethodPost {
		gatePage(w, 200, gateData{Title: L("srv.share_guest.titre"), Text: L("srv.share_guest.invitation"), Action: r.URL.EscapedPath(), Button: L("srv.share_guest.rejoindre")})
		return
	}
	if gateway {
		ip := clientIP(r)
		if !tokenTry(ip) {
			w.Header().Set("Retry-After", "60")
			refused(429, "srv.token.tooMany")
			return
		}
		if home != mainHomeID {
			p, err := homeProxy(home)
			if err != nil {
				refused(403, "srv.share_guest.lienInvalide")
				return
			}
			nw := &noStoreWriter{ResponseWriter: w}
			p.ServeHTTP(nw, r)
			if nw.code == http.StatusSeeOther {
				tokenRefund(ip)
			}
			return
		}
	}
	if shareSt == nil {
		refused(403, "srv.share_guest.lienInvalide")
		return
	}
	sec, _, ok := shareSt.Redeem(tok)
	if !ok {
		refused(403, "srv.share_guest.lienInvalide")
		return
	}
	prefix := ""
	if gateway {
		tokenRefund(clientIP(r))
	} else {
		prefix = url.QueryEscape(home)
	}
	secure := requestHTTPS(r) || (!gateway && r.Header.Get(guestHTTPSHeader) == "1")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.SetCookie(w, &http.Cookie{Name: guestCookie, Value: prefix + "." + sec, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/guest", http.StatusSeeOther)
}

// Ces routes ne servent un invité que par la garde ; montées pour l'exhaustivité de routeRights et pour l'hôte.
func routesGuest(mux *http.ServeMux) {
	mux.HandleFunc(invitePat, guestInvite)
	mux.HandleFunc(invitePostPat, guestInvite)
	mux.HandleFunc("POST /guest-leave", func(w http.ResponseWriter, r *http.Request) {
		guestDeny(w, 403, "srv.share_guest.refuse")
	})
	mux.HandleFunc("GET /guest-me", func(w http.ResponseWriter, r *http.Request) {
		guestDeny(w, 403, "srv.share_guest.refuse")
	})
}
