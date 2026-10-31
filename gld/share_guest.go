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
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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
	guestQuietHeader = "X-Gld-Guest-Quiet" // vérification pour /v1/guest/sessions : l'enfant ne touche pas LastSeen
	// Posés par la garde sur la requête invitée qu'elle sert elle-même (guestServe), après avoir retiré toute copie du client.
	// L'invitation ne prend pas X-Gld-Guest, qui porte déjà le secret relayé à l'enfant.
	authorHeader  = "X-Gld-Author"
	inviteHeader  = "X-Gld-Invite"
	invitePat     = "GET /invite/{home}/{token}"
	invitePostPat = "POST /invite/{home}/{token}"
	guestAddMax   = 10
	guestBodyMax  = sendMax + 1<<10
)

// guestStatics : l'interface invité se charge par navigation, sans en-tête ; servie si un cookie invité est valide (en mode
// jeton, l'hôte les refuserait à l'invité). Contenu public identique pour l'hôte.
var guestStatics = map[string]bool{
	"GET /guest": true, "GET /guest/{handle}": true, "GET /assets/": true, "GET /icons/": true, "GET /favicon.ico": true, "GET /site.webmanifest": true,
}

var guestRoutes = map[string]bool{
	"GET /v1/sessions/{id}/conversation": true, "GET /v1/sessions/{id}/conversation/text": true, "GET /v1/sessions/{id}/conversation/tools": true,
	"GET /v1/sessions/{id}/conversation/images/{img}": true,
	"GET /v1/sessions/{id}/live":                      true, "GET /v1/sessions/{id}/state": true, "GET /v1/sessions/{id}/tasks/{task}/output": true, "GET /v1/sessions/{id}/workflows": true,
	"GET /v1/fs/tree": true, "GET /v1/fs/entries/content": true,
	"GET /v1/sessions/{id}/queue": true, "POST /v1/sessions/{id}/queue": true, "DELETE /v1/sessions/{id}/queue/{qid}": true,
	"GET /v1/guest/me": true, "DELETE /v1/guest/me": true,
	"GET /guest": true, "GET /assets/": true, "GET /icons/": true, "GET /favicon.ico": true, "GET /site.webmanifest": true,
}

type guestRelayKey struct{}
type guestQuietKey struct{}

var guestOpenShares = openShares // les tests comptent les lectures

func guestCookieName(handle string) string { return guestCookiePrefix + handle }

func validHandle(handle string) bool {
	if len(handle) != 16 {
		return false
	}
	for _, character := range handle {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

var guestAdds = struct {
	sync.Mutex
	by map[string]*tryWin
}{by: map[string]*tryWin{}}

func guestAddTry(invite string) bool {
	guestAdds.Lock()
	defer guestAdds.Unlock()
	window := guestAdds.by[invite]
	if window == nil {
		window = &tryWin{}
		guestAdds.by[invite] = window
	}
	if window.at(time.Now()).n >= guestAddMax {
		return false
	}
	window.n++
	return true
}

// guestCSRF : dérivé du secret, rendu à la page par /v1/guest/me ; une autre origine ne peut ni le lire ni le calculer.
func guestCSRF(secret string) string {
	digest := sha256.Sum256([]byte("gld-guest-csrf\x00" + secret))
	return hex.EncodeToString(digest[:])
}

func guestDeny(responseWriter http.ResponseWriter, code int, key string) {
	responseWriter.Header().Set("Cache-Control", "no-store")
	writeErr(responseWriter, code, statusCode(code), L(key))
}

type noStoreWriter struct {
	http.ResponseWriter
	done bool
	code int
}

func (writer *noStoreWriter) WriteHeader(status int) {
	if !writer.done {
		writer.done, writer.code = true, status
		writer.Header().Set("Cache-Control", "no-store")
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *noStoreWriter) Write(content []byte) (int, error) {
	if !writer.done {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(content)
}

func (writer *noStoreWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// Flush : la garde enveloppe aussi des réponses hôte, dont des flux (chat) qui exigent http.Flusher.
func (writer *noStoreWriter) Flush() {
	if !writer.done {
		writer.WriteHeader(http.StatusOK)
	}
	if flusher, canFlush := writer.ResponseWriter.(http.Flusher); canFlush {
		flusher.Flush()
	}
}

// Les juges du cookie rendent isCertain=false quand ils ne peuvent pas répondre (magasin absent, fichier illisible) : seul un
// « secret invalide » certain efface le cookie, sinon une panne passagère perdrait pour de bon un lien à usage unique.
// Le secret d'un cookie authentifie une seule invitation.
func guestShare(secret string, touch bool) (share Share, invitation Invite, found, isCertain bool) {
	if secret == "" {
		return Share{}, Invite{}, false, true
	}
	if shareSt == nil {
		return Share{}, Invite{}, false, false
	}
	if !touch {
		share, invitation, found = shareSt.ByCookieQuiet(secret)
	} else {
		share, invitation, found = shareSt.ByCookie(secret)
	}
	return share, invitation, found, true
}

var guestReadHomes = func() ([]byte, error) { return readTrusted(homesFile()) } // les tests simulent une lecture

// guestHomes : lu une seule fois par jugement (readHomesFile rend une liste vide sur toute erreur, ce qui ferait croire à un
// home inconnu). Fichier absent : aucun home, réponse certaine ; illisible ou corrompu : pas de réponse.
func guestHomes() ([]Home, bool) {
	content, err := guestReadHomes()
	if errors.Is(err, os.ErrNotExist) {
		return nil, true
	}
	var parsed struct{ Homes []Home }
	if err != nil || json.Unmarshal(content, &parsed) != nil {
		return nil, false
	}
	return parsed.Homes, true
}

// guestKnown : la passerelle ne possède pas les partages d'un home enfant ; elle lit son fichier pour savoir si le cookie
// mérite le relais (sinon : requête hôte). L'enfant revalide le secret dans sa propre mémoire, qui fait foi.
func guestKnown(home, secret string) (Share, Invite, bool, bool) {
	if secret == "" {
		return Share{}, Invite{}, false, true
	}
	homes, readable := guestHomes()
	if !readable {
		return Share{}, Invite{}, false, false
	}
	homeIndex := slices.IndexFunc(homes, func(candidate Home) bool { return candidate.Name == home })
	if homeIndex < 0 {
		return Share{}, Invite{}, false, true
	}
	store, err := guestOpenShares(homes[homeIndex].Dir)
	if err != nil {
		return Share{}, Invite{}, false, false
	}
	share, invitation, found := store.ByCookieQuiet(secret)
	return share, invitation, found, true
}

// guestPre : le cookie de la poignée handle, jugé par la passerelle (home enfant : d'après son fichier). Le partage trouvé doit
// avoir cette poignée : un secret recopié sous le nom d'un autre cookie ne vaut rien.
// touch : seule une requête invitée gardée met LastSeen à jour.
func guestPre(handle, cookieValue string, touch bool) (home, secret string, share Share, invitation Invite, found, isCertain bool) {
	if !validHandle(handle) {
		return "", "", Share{}, Invite{}, false, true
	}
	home, secret = guestSplit(cookieValue)
	if home == "" {
		share, invitation, found, isCertain = guestShare(secret, touch)
	} else {
		share, invitation, found, isCertain = guestKnown(home, secret)
	}
	return home, secret, share, invitation, found && shareHandle(share.Session) == handle, isCertain
}

func guestSplit(cookieValue string) (home, secret string) {
	if separatorIndex := strings.LastIndexByte(cookieValue, '.'); separatorIndex >= 0 {
		home, _ = url.QueryUnescape(cookieValue[:separatorIndex])
		secret = cookieValue[separatorIndex+1:]
	}
	return home, secret
}

// guestCookies : les cookies invités de la requête, un par nom, au plus guestListMax ; ceux du home principal (en mémoire)
// d'abord, puis au plus guestChildMax de homes enfants. Borne le travail qu'un client impose avant toute garde.
func guestCookies(request *http.Request) []*http.Cookie {
	var mainCookies, childCookies []*http.Cookie
	seen := map[string]bool{}
	for _, cookie := range request.Cookies() {
		if !strings.HasPrefix(cookie.Name, guestCookiePrefix) || seen[cookie.Name] {
			continue
		}
		if len(seen) == guestListMax {
			break
		}
		seen[cookie.Name] = true
		if home, _ := guestSplit(cookie.Value); home == "" {
			mainCookies = append(mainCookies, cookie)
		} else if len(childCookies) < guestChildMax {
			childCookies = append(childCookies, cookie)
		}
	}
	return append(mainCookies, childCookies...)
}

func guestClear(responseWriter http.ResponseWriter, request *http.Request, name string) {
	http.SetCookie(responseWriter, &http.Cookie{Name: name, Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestHTTPS(request), SameSite: http.SameSiteStrictMode})
}

// guestStatic : la page /guest/<poignée> juge d'abord le cookie de cette poignée (une lecture), puis le balayage plafonné.
// Les pages (no-store) effacent les cookies examinés et certainement invalides, sans quoi des partages terminés masqueraient
// un partage vivant ; les fichiers publics, gardés un an par navigateur et CDN, ne portent jamais d'effacement.
func guestStatic(mux *http.ServeMux, responseWriter http.ResponseWriter, request *http.Request) (served, cleared bool) {
	if request.Method != http.MethodGet || request.URL.RawPath != "" || path.Clean(request.URL.Path) != request.URL.Path {
		return false, false
	}
	_, pattern := mux.Handler(request)
	if !guestStatics[pattern] {
		return false, false
	}
	isPage := pattern == "GET /guest" || pattern == "GET /guest/{handle}"
	judge := func(handle string, cookie *http.Cookie) bool {
		_, _, _, _, found, isCertain := guestPre(handle, cookie.Value, false)
		if !found && isCertain && isPage {
			guestClear(responseWriter, request, cookie.Name)
			cleared = true
		}
		return found
	}
	judgedName := ""
	if handle := strings.TrimPrefix(request.URL.Path, "/guest/"); pattern == "GET /guest/{handle}" && validHandle(handle) {
		if cookie, err := request.Cookie(guestCookieName(handle)); err == nil {
			if judge(handle, cookie) {
				return true, cleared
			}
			judgedName = cookie.Name
		}
	}
	for _, cookie := range guestCookies(request) {
		if cookie.Name != judgedName && judge(strings.TrimPrefix(cookie.Name, guestCookiePrefix), cookie) {
			return true, cleared
		}
	}
	return false, cleared
}

// guestPattern : le motif du mux, seulement s'il est dans la liste blanche et que le chemin est canonique (ni « // », ni
// encodage, ni « / » final ajouté) : le mux redirigerait ces variantes, la garde les refuse.
func guestPattern(mux *http.ServeMux, request *http.Request) string {
	if request.URL.RawPath != "" || path.Clean(request.URL.Path) != request.URL.Path {
		return ""
	}
	_, pattern := mux.Handler(request)
	if !guestRoutes[pattern] {
		return ""
	}
	if _, routePath, _ := strings.Cut(pattern, " "); !strings.HasSuffix(routePath, "/") && !strings.Contains(routePath, "{") && routePath != request.URL.Path {
		return ""
	}
	return pattern
}

func guestCSRFOK(request *http.Request, secret string) bool {
	return request.Method == http.MethodGet || subtle.ConstantTimeCompare([]byte(request.Header.Get(csrfHeader)), []byte(guestCSRF(secret))) == 1
}

func guestGate(mux *http.ServeMux, next http.Handler) http.Handler {
	crossOriginProtection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if len(request.Header.Values(authorHeader)) > 0 || len(request.Header.Values(inviteHeader)) > 0 {
			request = request.Clone(request.Context())
			request.Header.Del(authorHeader)
			request.Header.Del(inviteHeader)
		}
		_, pattern := mux.Handler(request)
		if pattern == invitePat || pattern == invitePostPat {
			crossOriginProtection.Handler(http.HandlerFunc(guestInvite)).ServeHTTP(responseWriter, request)
			return
		}
		if childToken != "" { // enfant : seule la passerelle parle (childAuthorized), le secret arrive en en-tête
			secret := request.Header.Get(guestHeader)
			if secret == "" {
				next.ServeHTTP(responseWriter, request)
				return
			}
			// La passerelle n'a pas jugé ce client comme hôte : un secret refusé ici ne retombe jamais sur l'hôte.
			share, invitation, found, isCertain := guestShare(secret, request.Header.Get(guestQuietHeader) != "1")
			if !isCertain { // pas un refus du secret : la passerelle ne doit pas effacer le cookie
				guestDeny(responseWriter, 503, "srv.share.indisponible")
				return
			}
			handle := request.Header.Get(shareHeader)
			if !found || shareHandle(share.Session) != handle {
				guestDeny(responseWriter, 403, "srv.share_guest.refuse")
				return
			}
			crossOriginProtection.Handler(guestServe(mux, share, invitation, secret, handle)).ServeHTTP(responseWriter, request)
			return
		}
		if pattern == "GET /v1/guest/sessions" {
			guestList(responseWriter, request)
			return
		}
		handle := request.Header.Get(shareHeader)
		// <img> ne peut pas porter d'en-tête : pour les images de la conversation seulement, la poignée peut venir de ?share= (l'en-tête l'emporte).
		fromQuery := handle == "" && request.Method == http.MethodGet && pattern == "GET /v1/sessions/{id}/conversation/images/{img}"
		if fromQuery {
			handle = request.URL.Query().Get("share")
		}
		if !validHandle(handle) {
			served, cleared := guestStatic(mux, responseWriter, request)
			if cleared {
				responseWriter = &noStoreWriter{ResponseWriter: responseWriter}
			}
			if served {
				mux.ServeHTTP(responseWriter, request)
				return
			}
			next.ServeHTTP(responseWriter, request)
			return
		}
		cookie, err := request.Cookie(guestCookieName(handle))
		if err != nil {
			next.ServeHTTP(responseWriter, request)
			return
		}
		home, secret, share, invitation, found, isCertain := guestPre(handle, cookie.Value, true)
		if found {
			if fromQuery { // la suite (garde de l'enfant comprise) voit la requête comme si l'en-tête avait été envoyé
				request = request.Clone(request.Context())
				query := request.URL.Query()
				query.Del("share")
				request.URL.RawQuery = query.Encode()
				request.Header.Set(shareHeader, handle)
			}
			if home == "" {
				crossOriginProtection.Handler(guestServe(mux, share, invitation, secret, handle)).ServeHTTP(responseWriter, request)
			} else {
				crossOriginProtection.Handler(guestRelay(mux, home, secret, handle)).ServeHTTP(responseWriter, request)
			}
			return
		}
		// Partage terminé ou cookie altéré : l'hôte qui a ouvert un lien dans son navigateur ne doit pas rester bloqué. Sans
		// réponse certaine, le cookie reste et la requête passe comme requête hôte.
		if isCertain {
			guestClear(responseWriter, request, cookie.Name)
			responseWriter = &noStoreWriter{ResponseWriter: responseWriter}
		}
		next.ServeHTTP(responseWriter, request)
	})
}

// guestRelay : passerelle, partage d'un home enfant ; liste blanche et CSRF jugés ici, le reste par l'enfant.
func guestRelay(mux *http.ServeMux, home, secret, handle string) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		pattern := guestPattern(mux, request)
		if pattern == "" || !guestCSRFOK(request, secret) {
			guestDeny(responseWriter, 403, "srv.share_guest.refuse")
			return
		}
		if pattern == "DELETE /v1/guest/me" { // ici : un enfant qui refuse le secret (fin non écrite) ne doit pas l'empêcher
			guestLeave(responseWriter, request, handle)
			return
		}
		proxy, err := homeProxy(home)
		if err != nil {
			journal("homes %s : %v", home, err)
			guestDeny(responseWriter, 503, "srv.homes.indisponible")
			return
		}
		proxy.ServeHTTP(&noStoreWriter{ResponseWriter: responseWriter}, request.WithContext(context.WithValue(request.Context(), guestRelayKey{}, secret)))
	})
}

func guestServe(mux *http.ServeMux, share Share, invitation Invite, secret, handle string) http.Handler {
	redactor := newPathRedactor(skillsHome(), decodeDir(share.Dir), guestAlias(invitation.ID), hostIdentityTerms()...)
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		pattern := guestPattern(mux, request)
		if pattern == "" || !guestCSRFOK(request, secret) {
			guestDeny(responseWriter, 403, "srv.share_guest.refuse")
			return
		}
		if pattern == "DELETE /v1/guest/me" {
			guestLeave(responseWriter, request, handle)
			return
		}
		if pattern == "GET /v1/guest/me" {
			responseWriter.Header().Set("Cache-Control", "no-store")
			writeJSON(responseWriter, 200, map[string]any{"id": share.Session, "dir": redactor.applyString(share.Dir), "mode": invitation.Mode, "csrf": guestCSRF(secret), "handle": handle, "title": share.Title,
				"invite": invitation.ID, "label": invitation.label()})
			return
		}
		if (pattern == "GET /v1/fs/tree" || pattern == "GET /v1/fs/entries/content") && guestSecretPath(request.URL.Query().Get("path")) {
			guestDeny(responseWriter, 403, "srv.share_guest.refuse")
			return
		}
		if pattern == "GET /v1/fs/entries/content" {
			if relativePath := path.Clean(request.URL.Query().Get("path")); relativePath == ".." || strings.HasPrefix(relativePath, "../") || path.IsAbs(relativePath) || !guestCited(share)[relativePath] {
				guestDeny(responseWriter, 403, "srv.share_guest.refuse")
				return
			}
		}
		forwarded := request.Clone(request.Context())
		query := forwarded.URL.Query()
		query.Set("id", share.Session)
		query.Set("dir", share.Dir)
		if pattern == "GET /v1/fs/tree" || pattern == "GET /v1/fs/entries/content" { // l'IDE attend le chemin réel, sh.Dir est le nom du dossier de transcripts
			absoluteDir := decodeDir(share.Dir)
			if absoluteDir == "" {
				guestDeny(responseWriter, 403, "srv.share_guest.refuse")
				return
			}
			if !guestInside(absoluteDir, query.Get("path")) {
				guestDeny(responseWriter, 403, "srv.share_guest.refuse")
				return
			}
			query.Set("dir", absoluteDir)
		}
		forwarded.URL.RawQuery = query.Encode()
		guestSessionPath(forwarded.URL, share.Session)
		forwarded.Header.Set(authorHeader, "guest")
		forwarded.Header.Set(inviteHeader, invitation.ID)
		if pattern == "GET /v1/fs/tree" {
			filteredTree := &guestTree{ResponseWriter: responseWriter}
			mux.ServeHTTP(filteredTree, forwarded)
			filteredTree.flush(&noStoreWriter{ResponseWriter: responseWriter}, guestCited(share), strings.Trim(path.Clean("/"+request.URL.Query().Get("path")), "/"))
			return
		}
		switch pattern {
		case "POST /v1/sessions/{id}/queue":
			// Corps reconstruit : seul le texte vient du client, la session est celle du partage.
			var body struct{ Text string }
			content, err := io.ReadAll(io.LimitReader(request.Body, guestBodyMax+1))
			if err != nil || len(content) > guestBodyMax || json.Unmarshal(content, &body) != nil {
				guestDeny(responseWriter, 400, "srv.http_util.requeteInvalide")
				return
			}
			if queueSt == nil {
				guestDeny(responseWriter, 503, "srv.queue.indisponible")
				return
			}
			if invitation.Mode != "rw" {
				guestDeny(responseWriter, 403, "srv.share_guest.lectureSeule")
				return
			}
			if !guestAddTry(invitation.ID) {
				responseWriter.Header().Set("Retry-After", "60")
				guestDeny(responseWriter, 429, "srv.share_guest.tropDeMessages")
				return
			}
			rebuiltBody, _ := json.Marshal(map[string]string{"text": body.Text})
			forwarded.Body, forwarded.ContentLength = io.NopCloser(bytes.NewReader(rebuiltBody)), int64(len(rebuiltBody))
		case "DELETE /v1/sessions/{id}/queue/{qid}":
			if queueSt == nil {
				guestDeny(responseWriter, 503, "srv.queue.indisponible")
				return
			}
			queueItemID := path.Base(forwarded.URL.Path)
			for _, queueItem := range queueSt.List(share.Session) {
				if queueItem.ID == queueItemID && (queueItem.Author != "guest" || queueItem.Guest != invitation.ID) {
					guestDeny(responseWriter, 403, "srv.share_guest.refuse")
					return
				}
			}
		}
		guestWriter := guestRedactedWriter(&noStoreWriter{ResponseWriter: responseWriter}, redactor)
		if pattern == "GET /v1/sessions/{id}/live" {
			serveGuestLive(mux, guestWriter, forwarded)
			return
		}
		if pattern == "GET /v1/sessions/{id}/queue" {
			serveGuestQueue(mux, responseWriter, forwarded, redactor)
			return
		}
		mux.ServeHTTP(guestWriter, forwarded)
	})
}

// guestSessionPath réécrit le segment {id} de /v1/sessions/{id}/... avec la session du partage : l'identifiant du client
// n'est jamais lu. guestPattern a déjà refusé RawPath et chemins non canoniques, donc Path suffit.
func guestSessionPath(requestURL *url.URL, session string) {
	const sessionsPrefix = "/v1/sessions/"
	if rest, hasPrefix := strings.CutPrefix(requestURL.Path, sessionsPrefix); hasPrefix {
		_, tail, _ := strings.Cut(rest, "/")
		requestURL.Path = sessionsPrefix + session
		if tail != "" || strings.HasSuffix(rest, "/") {
			requestURL.Path += "/" + tail
		}
	}
}

// guestLeave : efface le cookie de ce partage sans toucher au partage ; l'issue d'un hôte qui a ouvert son propre lien.
func guestLeave(responseWriter http.ResponseWriter, request *http.Request, handle string) {
	guestClear(responseWriter, request, guestCookieName(handle))
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.WriteHeader(http.StatusNoContent)
}

// hostCredential : la requête porte un accès hôte valide (jeton, appareil jumelé, compte ayant exec sur le home du partage) ;
// jugée par la passerelle, seule à voir les cookies. Un compte sans exec sur ce home est un collègue : il peut être l'invité.
func hostCredential(request *http.Request, home string) bool {
	if hostToken != "" {
		if cookie, tokenCookieErr := request.Cookie(tokenCookie); tokenCookieErr == nil && sameToken(cookie.Value, hostToken) {
			return true
		}
		if cookie, deviceCookieErr := request.Cookie(devCookie); deviceCookieErr == nil && deviceOK(cookie.Value) {
			return true
		}
	}
	if _, decision := reqMode(request); decision.on && decision.st != nil {
		if principal, _ := gatePrincipal(decision.st, request, true); principal != nil {
			if principal.Token {
				return true
			}
			homeID, isKnownHome := mainHomeID, true
			if home != mainHomeID {
				homeID, isKnownHome = homeIDFor(home)
			}
			rights, rightsReadErr := decision.st.Rights(principal.User.ID, homeID)
			return !isKnownHome || rightsReadErr != nil || rights[RightExec] // droits illisibles ou home inconnu : on ne consomme pas le jeton
		}
	}
	return false
}

// guestInvite : /invite/{home}/{token} ; « main » désigne le home du processus principal. Le GET n'affiche qu'un bouton :
// un aperçu de lien (messagerie, antivirus de courriel) ne doit pas consommer le jeton à usage unique. Le POST l'échange ;
// chaque essai compte dans la même limite que le jeton hôte (par adresse et pour tout le serveur), un succès est rendu.
func guestInvite(responseWriter http.ResponseWriter, request *http.Request) {
	refused := func(code int, key string) {
		gatePage(responseWriter, code, gateData{Title: L("srv.share_guest.titre"), Text: L(key)})
	}
	home, token, hasSeparator := strings.Cut(strings.TrimPrefix(request.URL.Path, "/invite/"), "/")
	if !hasSeparator || home == "" || token == "" || strings.Contains(token, "/") {
		refused(403, "srv.share_guest.lienInvalide")
		return
	}
	gateway := childToken == ""
	if gateway && hostCredential(request, home) {
		refused(200, "srv.share_guest.vousEtesHote")
		return
	}
	if request.Method != http.MethodPost {
		gatePage(responseWriter, 200, gateData{Title: L("srv.share_guest.titre"), Text: L("srv.share_guest.invitation"), Action: request.URL.EscapedPath(), Button: L("srv.share_guest.rejoindre")})
		return
	}
	if gateway {
		clientAddress := clientIP(request)
		if !tokenTry(clientAddress) {
			responseWriter.Header().Set("Retry-After", "60")
			refused(429, "srv.token.tooMany")
			return
		}
		if home != mainHomeID {
			proxy, err := homeProxy(home)
			if err != nil {
				refused(403, "srv.share_guest.lienInvalide")
				return
			}
			recorder := &noStoreWriter{ResponseWriter: responseWriter}
			proxy.ServeHTTP(recorder, request)
			if recorder.code == http.StatusSeeOther {
				tokenRefund(clientAddress)
			}
			return
		}
	}
	if shareSt == nil {
		refused(403, "srv.share_guest.lienInvalide")
		return
	}
	for _, cookie := range guestCookies(request) { // lien déjà échangé, rouvert dans le navigateur autorisé : retour à la session
		_, secret := guestSplit(cookie.Value)
		if session, reopened := shareSt.Reopened(token, secret); reopened {
			responseWriter.Header().Set("Cache-Control", "no-store")
			http.Redirect(responseWriter, request, "/guest/"+shareHandle(session), http.StatusSeeOther)
			return
		}
	}
	secret, share, _, redeemed := shareSt.Redeem(token)
	if !redeemed {
		refused(403, "srv.share_guest.lienInvalide")
		return
	}
	prefix := ""
	if gateway {
		tokenRefund(clientIP(request))
	} else {
		prefix = url.QueryEscape(home)
	}
	secure := requestHTTPS(request) || (!gateway && request.Header.Get(guestHTTPSHeader) == "1")
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.Header().Set("Referrer-Policy", "no-referrer")
	handle := shareHandle(share.Session)
	http.SetCookie(responseWriter, &http.Cookie{Name: guestCookieName(handle), Value: prefix + "." + secret, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.Redirect(responseWriter, request, "/guest/"+handle, http.StatusSeeOther)
}

// Ces routes ne servent un invité que par la garde ; montées pour l'exhaustivité de routeRights et pour l'hôte.
func routesGuest(mux *http.ServeMux) {
	mux.HandleFunc(invitePat, guestInvite)
	mux.HandleFunc(invitePostPat, guestInvite)
	mux.HandleFunc("DELETE /v1/guest/me", func(responseWriter http.ResponseWriter, request *http.Request) {
		guestDeny(responseWriter, 403, "srv.share_guest.refuse")
	})
	mux.HandleFunc("GET /v1/guest/me", func(responseWriter http.ResponseWriter, request *http.Request) {
		guestDeny(responseWriter, 403, "srv.share_guest.refuse")
	})
	mux.HandleFunc("GET /v1/guest/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		guestDeny(responseWriter, 403, "srv.share_guest.refuse")
	})
}

type guestItem struct {
	Handle string `json:"handle"`
	Title  string `json:"title"`
	Mode   string `json:"mode"`
}

// guestList : passerelle seulement ; les partages valides de ce navigateur, sans secret ni session ni dossier. Seul un cookie
// certainement invalide est effacé (jugé ici, ou refusé par l'enfant avec la réponse de guestDeny) ; toute autre issue (fichier
// illisible, enfant injoignable, 403 des gardes Host ou childAuthorized) omet le partage et garde le cookie.
func guestList(responseWriter http.ResponseWriter, request *http.Request) {
	items := []guestItem{}
	for _, cookie := range guestCookies(request) {
		handle := strings.TrimPrefix(cookie.Name, guestCookiePrefix)
		home, secret, share, invitation, found, isCertain := guestPre(handle, cookie.Value, false)
		if !found {
			if isCertain {
				guestClear(responseWriter, request, cookie.Name)
			}
			continue
		}
		listedShare := guestItem{Handle: handle, Title: share.Title, Mode: invitation.Mode}
		if home != "" {
			var status int
			if listedShare, status = guestChildMe(request, home, secret, handle); status == http.StatusForbidden {
				guestClear(responseWriter, request, cookie.Name)
			}
			if status != http.StatusOK {
				continue
			}
		}
		items = append(items, listedShare)
	}
	if len(items) == 0 {
		guestDeny(responseWriter, 403, "srv.share_guest.refuse")
		return
	}
	responseWriter.Header().Set("Cache-Control", "no-store")
	writeJSON(responseWriter, 200, items)
}

type bufWriter struct {
	h    http.Header
	code int
	b    bytes.Buffer
}

func (writer *bufWriter) Header() http.Header { return writer.h }
func (writer *bufWriter) WriteHeader(status int) {
	if writer.code == 0 {
		writer.code = status
	}
}
func (writer *bufWriter) Write(data []byte) (int, error) {
	writer.WriteHeader(http.StatusOK)
	if writer.b.Len()+len(data) > 4<<10 {
		return 0, io.ErrShortWrite
	}
	return writer.b.Write(data)
}

// guestChildMe : l'enfant revalide le secret dans sa mémoire (qui fait foi) par son /v1/guest/me, relayé comme une requête
// invitée ; la passerelle ne garde que poignée, titre et mode.
func guestChildMe(request *http.Request, home, secret, handle string) (guestItem, int) {
	proxy, err := homeProxy(home)
	if err != nil {
		journal("homes %s : %v", home, err)
		return guestItem{}, http.StatusServiceUnavailable
	}
	subrequest := request.Clone(context.WithValue(context.WithValue(request.Context(), guestRelayKey{}, secret), guestQuietKey{}, true))
	subrequest.Method, subrequest.URL = http.MethodGet, &url.URL{Path: "/v1/guest/me"}
	subrequest.Body, subrequest.ContentLength = http.NoBody, 0
	subrequest.Header.Set(shareHeader, handle)
	subrequest.Header.Del("Accept-Encoding") // corps lu ici : le transport décompresse seul s'il a négocié lui-même
	buffered := &bufWriter{h: http.Header{}}
	proxy.ServeHTTP(buffered, subrequest)
	var listedShare guestItem
	if buffered.code == http.StatusForbidden {
		var denial struct{ Error *struct{ Code string } }
		if json.Unmarshal(buffered.b.Bytes(), &denial) != nil || denial.Error == nil {
			return listedShare, http.StatusBadGateway // pas un jugement du secret par la garde invitée de l'enfant
		}
	}
	if buffered.code != http.StatusOK {
		return listedShare, buffered.code
	}
	if json.Unmarshal(buffered.b.Bytes(), &listedShare) != nil || listedShare.Handle != handle {
		return guestItem{}, http.StatusBadGateway
	}
	return listedShare, http.StatusOK
}

// guestSecretPath : défense en profondeur ; la confinement au dossier du partage est faite par ideRel/os.Root (absolu, « .. » et liens refusés). Un segment .git, .env*, clé ou certificat ferme le chemin à l'invité (lecture seule du dossier du partage).
func guestSecretPath(requestPath string) bool {
	for _, segment := range strings.Split(requestPath, "/") {
		lowered := strings.ToLower(segment)
		if lowered == ".git" || strings.HasPrefix(lowered, ".env") || strings.HasPrefix(lowered, "id_rsa") || strings.HasPrefix(lowered, "id_") ||
			slices.Contains([]string{".ssh", ".aws", ".kube", ".docker", ".netrc", ".npmrc", ".pgpass"}, lowered) ||
			strings.HasSuffix(lowered, ".pem") || strings.HasSuffix(lowered, ".key") || strings.HasSuffix(lowered, ".p12") || strings.HasSuffix(lowered, ".pfx") ||
			strings.Contains(lowered, "credentials") || strings.Contains(lowered, "secret") {
			return true
		}
	}
	return false
}

// guestCited : les chemins (relatifs au dossier du partage) que le transcript cite en absolu (lus, écrits ou mentionnés par la
// session), donc déjà visibles dans la conversation. Un invité ne lit que ceux-là ; l'arbre n'affiche qu'eux et leurs dossiers.
// Un chemin avec espace n'est pas reconnu : le refus est le bon défaut.
func guestCited(share Share) map[string]bool {
	cited := map[string]bool{}
	transcriptPath, found := transcript(share.Dir, share.Session)
	if !found {
		return cited
	}
	absoluteDir := decodeDir(share.Dir)
	if absoluteDir == "" {
		return cited
	}
	quotedDir, _ := json.Marshal(strings.TrimSuffix(absoluteDir, "/"))
	citedPattern := regexp.MustCompile(regexp.QuoteMeta(string(quotedDir[1:len(quotedDir)-1])) + `/([A-Za-z0-9._@+~/-]+)`)
	_ = scan(transcriptPath, func(transcriptLine []byte) {
		for _, match := range citedPattern.FindAllSubmatch(transcriptLine, -1) {
			if relativePath := path.Clean(strings.TrimRight(string(match[1]), "./")); relativePath != "." && relativePath != ".." && !strings.HasPrefix(relativePath, "../") {
				cited[relativePath] = true
			}
		}
	})
	return cited
}

// citedDir : un dossier est visible s'il contient un chemin cité.
func citedDir(cited map[string]bool, relativeDir string) bool {
	for citedPath := range cited {
		if relativeDir == "" || strings.HasPrefix(citedPath, relativeDir+"/") {
			return true
		}
	}
	return false
}

// guestTree filtre la réponse de /v1/fs/tree : fichiers cités, dossiers qui en contiennent.
type guestTree struct {
	http.ResponseWriter
	buf  bytes.Buffer
	code int
}

func (tree *guestTree) WriteHeader(status int)            { tree.code = status }
func (tree *guestTree) Write(content []byte) (int, error) { return tree.buf.Write(content) }

func (tree *guestTree) flush(responseWriter http.ResponseWriter, cited map[string]bool, dir string) {
	var listing struct {
		Entries   []ideEntry `json:"entries"`
		Truncated bool       `json:"truncated"`
	}
	if tree.code >= 300 || json.Unmarshal(tree.buf.Bytes(), &listing) != nil {
		if tree.code == 0 || tree.code < 300 {
			tree.code = 403
		}
		guestDeny(responseWriter, tree.code, "srv.share_guest.refuse")
		return
	}
	keep := listing.Entries[:0]
	for _, entry := range listing.Entries {
		entryPath := path.Join(dir, entry.Name)
		if entry.Type == "dir" && citedDir(cited, entryPath) || entry.Type == "file" && cited[entryPath] {
			keep = append(keep, entry)
		}
	}
	listing.Entries = keep
	writeJSON(responseWriter, 200, listing)
}

// guestInside : le chemin, une fois les liens résolus, reste sous le dossier du partage (ideLstat refuse déjà les liens ; cette
// vérification tient même si ce garde change).
func guestInside(absoluteDir, relativePath string) bool {
	base, err := filepath.EvalSymlinks(absoluteDir)
	if err != nil {
		return false
	}
	full, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(relativePath)))
	if err != nil {
		return false
	}
	back, err := filepath.Rel(base, full)
	return err == nil && back != ".." && !strings.HasPrefix(back, ".."+string(filepath.Separator))
}
