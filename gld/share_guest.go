package main

// Garde invité : une requête qui désigne un partage (shareHeader) et porte son cookie valide n'atteint qu'une liste blanche
// de routes, toujours sur la session partagée. Elle passe après les gardes Host et childAuthorized, avant le jeton hôte et le
// garde des comptes (handler, main.go) : un invité n'a ni jeton ni compte. Sans en-tête, ou sans cookie valide pour la poignée
// désignée, la requête continue telle quelle comme requête hôte : un cookie invité ne verrouille jamais l'hôte.

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
	// Un cookie par partage, gld_guest_<poignée> = "<home échappé>.<secret>" : le préfixe sert au routage, le secret à
	// l'authentification. La requête désigne son partage par shareHeader ; sans lui, elle reste une requête hôte.
	guestCookiePrefix = "gld_guest_"
	shareHeader       = "X-Gld-Share"
	guestListMax      = 20
	guestChildMax     = 3 // cookies de home enfant jugés par requête : chacun coûte une lecture de fichier
	// Posés par la passerelle sur la requête relayée au gld enfant qui possède le partage (childProxy) ; jamais lus ailleurs.
	guestHeader      = "X-Gld-Guest"
	guestHTTPSHeader = "X-Gld-Guest-HTTPS"
	guestQuietHeader = "X-Gld-Guest-Quiet" // vérification pour /guest-list : l'enfant ne touche pas LastSeen
	invitePat        = "GET /invite/{home}/{token}"
	invitePostPat    = "POST /invite/{home}/{token}"
	guestAddMax      = 10
	guestBodyMax     = sendMax + 1<<10
)

// guestStatics : l'interface invité se charge par navigation, sans en-tête ; servie si un cookie invité est valide (en mode
// jeton, l'hôte les refuserait à l'invité). Contenu public identique pour l'hôte.
var guestStatics = map[string]bool{
	"GET /guest": true, "GET /guest/{handle}": true, "GET /assets/": true, "GET /icons/": true, "GET /favicon.ico": true, "GET /manifest.webmanifest": true,
}

var guestRoutes = map[string]bool{
	"GET /conv": true, "GET /conv-text": true, "GET /conv-tools": true, "GET /conv-image": true,
	"GET /session-live": true, "GET /session-state": true, "GET /session-task-output": true,
	"GET /queue": true, "POST /queue-add": true, "POST /queue-rm": true, "GET /guest-me": true, "POST /guest-leave": true,
	"GET /guest": true, "GET /assets/": true, "GET /icons/": true, "GET /favicon.ico": true, "GET /manifest.webmanifest": true,
}

type guestRelayKey struct{}
type guestQuietKey struct{}

var guestOpenShares = openShares // les tests comptent les lectures

func guestCookieName(handle string) string { return guestCookiePrefix + handle }

func validHandle(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

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

func guestShare(sec string, touch bool) (Share, bool) {
	if sec == "" || shareSt == nil {
		return Share{}, false
	}
	if !touch {
		return shareSt.ByCookieQuiet(sec)
	}
	return shareSt.ByCookie(sec)
}

// guestKnown : la passerelle ne possède pas les partages d'un home enfant ; elle lit son fichier pour savoir si le cookie
// mérite le relais (sinon : requête hôte). L'enfant revalide le secret dans sa propre mémoire, qui fait foi.
func guestKnown(home, sec string) (Share, bool) {
	h, ok := homeByName(home)
	if !ok || sec == "" {
		return Share{}, false
	}
	st, err := guestOpenShares(h.Dir)
	if err != nil {
		return Share{}, false
	}
	hs := hashSecret(sec)
	for _, sh := range st.m {
		if hashEq(sh.CookieHash, hs) {
			return *sh, true
		}
	}
	return Share{}, false
}

// guestPre : le cookie de la poignée hd, jugé par la passerelle (home enfant : d'après son fichier). Le partage trouvé doit
// avoir cette poignée : un secret recopié sous le nom d'un autre cookie ne vaut rien.
// touch : seule une requête invitée gardée met LastSeen à jour.
func guestPre(hd, val string, touch bool) (home, sec string, sh Share, ok bool) {
	if !validHandle(hd) {
		return "", "", Share{}, false
	}
	home, sec = guestSplit(val)
	if home == "" {
		sh, ok = guestShare(sec, touch)
	} else {
		sh, ok = guestKnown(home, sec)
	}
	return home, sec, sh, ok && shareHandle(sh.Session) == hd
}

func guestSplit(val string) (home, sec string) {
	if i := strings.LastIndexByte(val, '.'); i >= 0 {
		home, _ = url.QueryUnescape(val[:i])
		sec = val[i+1:]
	}
	return home, sec
}

// guestCookies : les cookies invités de la requête, un par nom, au plus guestListMax ; ceux du home principal (en mémoire)
// d'abord, puis au plus guestChildMax de homes enfants. Borne le travail qu'un client impose avant toute garde.
func guestCookies(r *http.Request) []*http.Cookie {
	var main, child []*http.Cookie
	seen := map[string]bool{}
	for _, c := range r.Cookies() {
		if !strings.HasPrefix(c.Name, guestCookiePrefix) || seen[c.Name] {
			continue
		}
		if len(seen) == guestListMax {
			break
		}
		seen[c.Name] = true
		if home, _ := guestSplit(c.Value); home == "" {
			main = append(main, c)
		} else if len(child) < guestChildMax {
			child = append(child, c)
		}
	}
	return append(main, child...)
}

func guestClear(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteStrictMode})
}

func guestStatic(mux *http.ServeMux, r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path {
		return false
	}
	if _, pat := mux.Handler(r); !guestStatics[pat] {
		return false
	}
	for _, c := range guestCookies(r) {
		if _, _, _, ok := guestPre(strings.TrimPrefix(c.Name, guestCookiePrefix), c.Value, false); ok {
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
		_, pat := mux.Handler(r)
		if pat == invitePat || pat == invitePostPat {
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
			sh, ok := guestShare(sec, r.Header.Get(guestQuietHeader) != "1")
			hd := r.Header.Get(shareHeader)
			if !ok || shareHandle(sh.Session) != hd {
				guestDeny(w, 403, "srv.share_guest.refuse")
				return
			}
			cop.Handler(guestServe(mux, sh, sec, hd)).ServeHTTP(w, r)
			return
		}
		if pat == "GET /guest-list" {
			guestList(w, r)
			return
		}
		hd := r.Header.Get(shareHeader)
		if !validHandle(hd) {
			if guestStatic(mux, r) {
				mux.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(guestCookieName(hd))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if home, sec, sh, ok := guestPre(hd, c.Value, true); ok {
			if home == "" {
				cop.Handler(guestServe(mux, sh, sec, hd)).ServeHTTP(w, r)
			} else {
				cop.Handler(guestRelay(mux, home, sec, hd)).ServeHTTP(w, r)
			}
			return
		}
		// Partage terminé ou cookie altéré : l'hôte qui a ouvert un lien dans son navigateur ne doit pas rester bloqué.
		guestClear(w, r, c.Name)
		next.ServeHTTP(w, r)
	})
}

// guestRelay : passerelle, partage d'un home enfant ; liste blanche et CSRF jugés ici, le reste par l'enfant.
func guestRelay(mux *http.ServeMux, home, sec, hd string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pat := guestPattern(mux, r)
		if pat == "" || !guestCSRFOK(r, sec) {
			guestDeny(w, 403, "srv.share_guest.refuse")
			return
		}
		if pat == "POST /guest-leave" { // ici : un enfant qui refuse le secret (fin non écrite) ne doit pas l'empêcher
			guestLeave(w, r, hd)
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

func guestServe(mux *http.ServeMux, sh Share, sec, hd string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pat := guestPattern(mux, r)
		if pat == "" || !guestCSRFOK(r, sec) {
			guestDeny(w, 403, "srv.share_guest.refuse")
			return
		}
		if pat == "POST /guest-leave" {
			guestLeave(w, r, hd)
			return
		}
		if pat == "GET /guest-me" {
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, 200, map[string]any{"id": sh.Session, "dir": sh.Dir, "mode": sh.Mode, "csrf": guestCSRF(sec), "handle": hd, "title": sh.Title})
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

// guestLeave : efface le cookie de ce partage sans toucher au partage ; l'issue d'un hôte qui a ouvert son propre lien.
func guestLeave(w http.ResponseWriter, r *http.Request, hd string) {
	guestClear(w, r, guestCookieName(hd))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// hostCredential : la requête porte un accès hôte valide (jeton, appareil jumelé, compte ayant exec sur le home du partage) ;
// jugée par la passerelle, seule à voir les cookies. Un compte sans exec sur ce home est un collègue : il peut être l'invité.
func hostCredential(r *http.Request, home string) bool {
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
			if p.Token {
				return true
			}
			id, ok := mainHomeID, true
			if home != mainHomeID {
				id, ok = homeIDFor(home)
			}
			m, err := d.st.Rights(p.User.ID, id)
			return !ok || err != nil || m[RightExec] // droits illisibles ou home inconnu : on ne consomme pas le jeton
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
	if gateway && hostCredential(r, home) {
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
	sec, sh, ok := shareSt.Redeem(tok)
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
	hd := shareHandle(sh.Session)
	http.SetCookie(w, &http.Cookie{Name: guestCookieName(hd), Value: prefix + "." + sec, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/guest/"+hd, http.StatusSeeOther)
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
	mux.HandleFunc("GET /guest-list", func(w http.ResponseWriter, r *http.Request) {
		guestDeny(w, 403, "srv.share_guest.refuse")
	})
}

type guestItem struct {
	Handle string `json:"handle"`
	Title  string `json:"title"`
	Mode   string `json:"mode"`
}

// guestList : passerelle seulement ; les partages valides de ce navigateur, sans secret ni session ni dossier. Un cookie
// invalide est effacé ; un home enfant injoignable ne fait ni lister ni effacer (le lien était à usage unique).
func guestList(w http.ResponseWriter, r *http.Request) {
	out := []guestItem{}
	for _, c := range guestCookies(r) {
		hd := strings.TrimPrefix(c.Name, guestCookiePrefix)
		home, sec, sh, ok := guestPre(hd, c.Value, false)
		if !ok {
			guestClear(w, r, c.Name)
			continue
		}
		it := guestItem{Handle: hd, Title: sh.Title, Mode: sh.Mode}
		if home != "" {
			var st int
			if it, st = guestChildMe(r, home, sec, hd); st == http.StatusForbidden {
				guestClear(w, r, c.Name)
			}
			if st != http.StatusOK {
				continue
			}
		}
		out = append(out, it)
	}
	if len(out) == 0 {
		guestDeny(w, 403, "srv.share_guest.refuse")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}

type bufWriter struct {
	h    http.Header
	code int
	b    bytes.Buffer
}

func (b *bufWriter) Header() http.Header { return b.h }
func (b *bufWriter) WriteHeader(c int) {
	if b.code == 0 {
		b.code = c
	}
}
func (b *bufWriter) Write(p []byte) (int, error) {
	b.WriteHeader(http.StatusOK)
	if b.b.Len()+len(p) > 4<<10 {
		return 0, io.ErrShortWrite
	}
	return b.b.Write(p)
}

// guestChildMe : l'enfant revalide le secret dans sa mémoire (qui fait foi) par son /guest-me, relayé comme une requête
// invitée ; la passerelle ne garde que poignée, titre et mode.
func guestChildMe(r *http.Request, home, sec, hd string) (guestItem, int) {
	p, err := homeProxy(home)
	if err != nil {
		journal("homes %s : %v", home, err)
		return guestItem{}, http.StatusServiceUnavailable
	}
	sr := r.Clone(context.WithValue(context.WithValue(r.Context(), guestRelayKey{}, sec), guestQuietKey{}, true))
	sr.Method, sr.URL = http.MethodGet, &url.URL{Path: "/guest-me"}
	sr.Body, sr.ContentLength = http.NoBody, 0
	sr.Header.Set(shareHeader, hd)
	sr.Header.Del("Accept-Encoding") // corps lu ici : le transport décompresse seul s'il a négocié lui-même
	bw := &bufWriter{h: http.Header{}}
	p.ServeHTTP(bw, sr)
	var it guestItem
	if bw.code != http.StatusOK {
		return it, bw.code
	}
	if json.Unmarshal(bw.b.Bytes(), &it) != nil || it.Handle != hd {
		return guestItem{}, http.StatusBadGateway
	}
	return it, http.StatusOK
}
