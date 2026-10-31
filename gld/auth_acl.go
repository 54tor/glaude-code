package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Valeurs propres au garde, hors allRights : Grant les refuse, aucune ligne home_acl ne peut les porter.
const (
	rightPublic Right = "public" // servie sans session ; les routes /v1/auth/* vérifient elles-mêmes la session
	rightAny    Right = "any"    // session complète, sans droit sur un home (la route filtre elle-même)
	rightAdmin  Right = "admin"
)

// routeRights : chaque motif enregistré dans le mux, tel quel. Un motif absent est refusé.
var routeRights = map[string]Right{
	// Interface et ressources statiques.
	"GET /{$}": rightPublic, "GET /accueil": rightPublic, "GET /toutes": rightPublic, "GET /en-cours": rightPublic, "GET /dossier/": rightPublic,
	"GET /groupe/": rightPublic, "GET /projet/": rightPublic, "GET /snippets": rightPublic, "GET /snippets/": rightPublic,
	"GET /memoire": rightPublic, "GET /memoire/": rightPublic, "GET /onglets": rightPublic, "GET /configuration": rightPublic,
	"GET /configuration/": rightPublic, "GET /skills": rightPublic, "GET /skills/": rightPublic, "GET /extensions": rightPublic, "GET /plugins": rightPublic, "GET /sauvegarde": rightPublic, "GET /equipes": rightPublic, "GET /equipes/": rightPublic, "GET /revues": rightPublic, "GET /espace/": rightPublic, "GET /reglages/": rightPublic, "GET /assets/": rightPublic,
	"GET /icons/": rightPublic, "GET /favicon.ico": rightPublic, "GET /site.webmanifest": rightPublic, "GET /sw.js": rightPublic,
	"GET /apple-touch-icon.png": rightPublic, "GET /apple-touch-icon-precomposed.png": rightPublic,
	"GET /favicon-96x96.png": rightPublic, "GET /web-app-manifest-192x192.png": rightPublic, "GET /web-app-manifest-512x512.png": rightPublic,
	"GET /guest": rightPublic, "GET /guest/{handle}": rightPublic, "GET /v1/guest/sessions": rightPublic, "GET /v1/guest/me": rightPublic, "GET /invite/{home}/{token}": rightPublic, "POST /invite/{home}/{token}": rightPublic, "DELETE /v1/guest/me": rightPublic,

	// Connexion et profil.
	"POST /v1/auth/sessions": rightPublic, "POST /v1/auth/mfa/totp": rightPublic, "POST /v1/auth/mfa/recovery": rightPublic,
	"POST /v1/auth/mfa/webauthn/challenges": rightPublic, "POST /v1/auth/mfa/webauthn/assertions": rightPublic, "DELETE /v1/auth/sessions/current": rightPublic,
	"GET /v1/auth/me": rightPublic, "POST /v1/auth/totp/enrollments": rightPublic, "PUT /v1/auth/totp/enrollments/current": rightPublic,
	"POST /v1/auth/webauthn/registrations": rightPublic, "PUT /v1/auth/webauthn/registrations/current": rightPublic,
	"POST /v1/auth/factors/{factor}/removals": rightPublic, "PUT /v1/auth/recovery-codes": rightPublic, "PUT /v1/auth/password": rightPublic,
	"GET /v1/auth/alerts": rightPublic, "GET /v1/auth/sessions": rightPublic, "POST /v1/auth/sessions/{sid}/revocations": rightPublic, "POST /v1/auth/sessions/revocations": rightPublic,

	// Administration des comptes et des droits.
	"GET /v1/admin/users": rightAdmin, "POST /v1/admin/users": rightAdmin, "PATCH /v1/admin/users/{user}": rightAdmin,
	"POST /v1/admin/users/{user}/mfa-resets": rightAdmin, "PUT /v1/admin/users/{user}/password": rightAdmin, "PUT /v1/admin/acl": rightAdmin,
	"GET /v1/admin/events": rightAdmin, "GET /v1/admin/homes": rightAdmin, "GET /v1/admin/sessions": rightAdmin, "POST /v1/admin/sessions/revocations": rightAdmin,

	// Jumelage d'un appareil par QR code (pair.go).
	"POST /v1/pairing/codes": rightAny, "GET /pair": rightPublic, "POST /pair": rightPublic, "GET /v1/devices": rightAny, "DELETE /v1/devices/{device}": rightAny,

	// Homes.
	"GET /v1/homes": rightAny, "PUT /v1/homes/current": rightAny, "PUT /v1/homes": rightAdmin,
	"POST /v1/homes/sync-plans": RightMemSync, "POST /v1/homes/syncs": RightMemSync,

	// Mémoire, règles, snippets, notes, corbeille, recherche.
	"GET /v1/memory/files": RightMemRead, "GET /v1/memory/search": RightMemRead, "GET /v1/memory/files/{key}": RightMemRead, "GET /v1/memory/health": RightMemRead,
	"GET /v1/memory/matches": RightMemRead, "GET /v1/memory/loaded": RightMemRead,
	"PUT /v1/memory/files/{key}": RightMemWrite, "PATCH /v1/memory/files/{key}": RightMemWrite, "DELETE /v1/memory/files": RightMemWrite,
	"POST /v1/memory/drafts": RightMemWrite, "POST /v1/memory/repairs": RightMemWrite, "POST /v1/memory/fixes": RightMemWrite,
	"POST /v1/memory/token-counts": RightMemWrite, "POST /v1/memory/lints": RightMemWrite, "POST /v1/memory/merges": RightMemWrite,
	"GET /v1/snippets": RightMemRead, "GET /v1/snippets/{key}": RightMemRead, "GET /v1/snippets/export": RightMemRead,
	// POST /v1/snippets/drafts ne fait qu'un brouillon (même appel de modèle que POST /v1/memory/drafts) ; écrire un snippet, c'est écrire
	// une commande slash de Claude Code (commands/*.md), donc config_write.
	"POST /v1/snippets/drafts": RightMemWrite, "POST /v1/snippets/merges": RightMemWrite,
	"GET /v1/notes": RightMemRead, "PUT /v1/notes": RightMemWrite,
	"GET /v1/notes/sent": RightMemRead, "POST /v1/notes/sent": RightMemWrite, "DELETE /v1/notes/sent": RightMemWrite,
	"GET /v1/trash/memory": RightMemRead, "GET /v1/trash/memory/{id}/file": RightMemRead, "PATCH /v1/trash/memory/{id}": RightMemWrite, "DELETE /v1/trash/memory": RightMemWrite,
	"GET /v1/skills": RightMemRead, "GET /v1/skills/catalog": RightMemRead, "GET /v1/skills/updates": RightMemRead, "GET /v1/skills/{key}": RightMemRead, "GET /v1/plugins": RightMemRead, "GET /v1/marketplaces": RightMemRead,

	// Lecture du home et état d'interface par home.
	"GET /v1/config": RightMemRead, "GET /v1/config/projects": RightMemRead, "GET /v1/config/effective": RightMemRead,
	"GET /v1/config/mcp": RightMemRead, "GET /v1/slash-commands": RightMemRead,
	"GET /v1/git/status": RightMemRead, "GET /v1/git/changes": RightMemRead, "GET /v1/git/remotes": RightMemRead,
	"GET /v1/update": RightMemRead, "GET /v1/cleanup-warning": RightMemRead, "GET /v1/tabs": RightMemRead, "GET /v1/keepwarm": RightMemRead,
	"GET /v1/builtin-rules": RightMemRead, "PUT /v1/builtin-rules": RightConfigWrite,
	"PUT /v1/tabs": RightMemWrite, "DELETE /v1/tabs/{tab}": RightMemWrite, "PUT /v1/vgroups": RightMemWrite,

	// Réglages et skills : font exécuter du code par Claude Code.
	"POST /v1/config/edits": RightConfigWrite, "GET /v1/config/raw": RightConfigWrite, "PUT /v1/config/raw": RightConfigWrite,
	"POST /v1/config/restorations": RightConfigWrite, "POST /v1/config/permissions/imports": RightConfigWrite, "POST /v1/config/mcp/checks": RightConfigWrite, "PUT /v1/config/projects/mcp": RightConfigWrite,
	"POST /v1/config/permissions/explanations": RightConfigWrite,
	"PUT /v1/snippets/{key}":                   RightConfigWrite, "DELETE /v1/snippets": RightConfigWrite, "PATCH /v1/snippets/{key}": RightConfigWrite, "POST /v1/snippets/imports": RightConfigWrite,
	"GET /v1/backup/export": RightConfigWrite, "POST /v1/backup/imports": RightConfigWrite, "POST /v1/backup/imports/{id}/applications": RightConfigWrite, "DELETE /v1/backup/imports/{id}": RightConfigWrite,
	"PUT /v1/skills/{key}": RightConfigWrite, "POST /v1/skills/copies": RightConfigWrite, "DELETE /v1/skills": RightConfigWrite,
	"POST /v1/skills/drafts": RightConfigWrite, "POST /v1/skills/installations": RightConfigWrite,
	"PUT /v1/plugins/{plugin}": RightConfigWrite, "DELETE /v1/plugins/{plugin}": RightConfigWrite, "PATCH /v1/plugins/{plugin}": RightConfigWrite, "POST /v1/plugins/{plugin}/updates": RightConfigWrite,
	"POST /v1/marketplaces": RightConfigWrite, "DELETE /v1/marketplaces/{name}": RightConfigWrite, "POST /v1/marketplaces/{name}/updates": RightConfigWrite, "POST /v1/marketplaces/updates": RightConfigWrite,

	// Compte et usages.
	"GET /v1/account": RightUsageRead, "POST /v1/account/refreshes": RightUsageRead,

	// Transcriptions et état des sessions. /info renvoie les premiers messages d'une session : contenu, pas usage.
	"GET /v1/state": RightSessionsRead, "GET /v1/sessions/{id}/conversation": RightSessionsRead, "GET /v1/sessions/{id}/conversation/text": RightSessionsRead, "GET /v1/sessions/{id}/conversation/tools": RightSessionsRead,
	"GET /v1/sessions/{id}/conversation/images/{img}": RightSessionsRead, "GET /v1/sessions/{id}/conversation/uploads/{up}": RightSessionsRead, "GET /v1/sessions/{id}/conversation/document": RightSessionsRead, "GET /v1/info": RightSessionsRead,
	"GET /v1/sessions/{id}/state": RightSessionsRead, "GET /v1/sessions/{id}/live": RightSessionsRead, "GET /v1/sessions/{id}/tasks/{task}/output": RightSessionsRead, "GET /v1/sessions/{id}/workflows": RightSessionsRead, "GET /v1/sessions/{id}/screen": RightSessionsRead,
	"GET /v1/sessions/{id}/loaded": RightSessionsRead, "GET /v1/sessions/{id}/loaded/files/{file}": RightSessionsRead, "GET /v1/trash/sessions": RightSessionsRead, "GET /v1/state/stamp": RightSessionsRead,
	"GET /v1/sessions/send-info": RightSessionsRead, "GET /v1/sessions": RightSessionsRead, "GET /v1/sessions/{id}/queue": RightSessionsRead,
	// Renvoie des extraits de commandes tirés des transcriptions.
	"GET /v1/config/permissions/suggestions": RightSessionsRead,

	// Exécution.
	"POST /v1/sessions/{id}/chat": RightExec, "DELETE /v1/sessions/{id}/chat": RightExec, "POST /v1/sessions": RightExec,
	"GET /v1/fs/tree": RightFsRead, "GET /v1/fs/entries/content": RightFsRead, "GET /v1/fs/trash": RightFsRead,
	"GET /v1/fs/index": RightFsRead, "POST /v1/fs/files": RightFsRead, "GET /v1/fs/watch": RightFsRead,
	"PUT /v1/fs/entries/content": RightFsWrite, "POST /v1/fs/entries": RightFsWrite, "PATCH /v1/fs/entries": RightFsWrite, "DELETE /v1/fs/entries": RightFsWrite,
	"PATCH /v1/fs/trash/{entry}": RightFsWrite, "DELETE /v1/fs/trash": RightFsWrite,
	"GET /v1/terminal": RightExec, "POST /v1/terminal": RightExec, "GET /v1/terminal/ws": RightExec,
	"POST /v1/sessions/{id}/messages": RightExec, "POST /v1/sessions/{id}/queue": RightExec, "DELETE /v1/sessions/{id}/queue/{qid}": RightExec, "PATCH /v1/sessions/{id}/queue/{qid}": RightExec, "POST /v1/invites": RightExec, "PUT /v1/invites/{invite}/token": RightExec, "PATCH /v1/invites/{invite}": RightExec, "DELETE /v1/invites/{invite}": RightExec, "DELETE /v1/shares/{share}": RightExec, "GET /v1/shares": RightExec, "POST /v1/sessions/{id}/keystrokes": RightExec, "DELETE /v1/sessions/{id}/process": RightExec,
	"GET /v1/trust/pending": RightExec, "POST /v1/trust/answers": RightExec, "PUT /v1/sessions/{id}/process": RightExec, "PUT /v1/projects/{dir}/process": RightExec, "POST /v1/git/pushes": RightExec,
	"GET /v1/directories": RightExec, "POST /v1/directories": RightExec, "DELETE /v1/directories": RightExec, "POST /v1/uploads/images": RightExec, "POST /v1/uploads/files": RightExec,
	// Agissent sur les sessions : fermeture, suppression de transcriptions, tours de modèle.
	"DELETE /v1/sessions": RightExec, "DELETE /v1/sessions/{id}/conversation": RightExec, "PATCH /v1/sessions/{id}": RightExec, "PATCH /v1/sessions": RightExec, "PUT /v1/keepwarm": RightExec,
	"GET /v1/teams/catalog": RightSessionsRead, "POST /v1/teams/compositions": RightSessionsRead, "GET /v1/teams": RightSessionsRead,
	"GET /v1/teams/{team}/run": RightSessionsRead, "GET /v1/teams/{team}/run/screen": RightSessionsRead,
	"POST /v1/teams/{team}/runs": RightExec, "POST /v1/teams/{team}/run/messages": RightExec, "POST /v1/teams/{team}/run/keystrokes": RightExec, "DELETE /v1/teams/{team}/run": RightExec,
	"PUT /v1/agents/{agent}": RightConfigWrite, "DELETE /v1/agents/{agent}": RightConfigWrite,
	"PUT /v1/teams/{team}": RightConfigWrite, "DELETE /v1/teams/{team}": RightConfigWrite,
	"POST /v1/trash/sessions": RightExec, "PATCH /v1/trash/sessions": RightExec, "DELETE /v1/trash/sessions": RightExec,
}

// Routes de sondage : ne prolongent pas la session, sans quoi un onglet ouvert ne l'expirerait jamais.
var passiveRoutes = map[string]bool{
	"GET /v1/state": true, "GET /v1/state/stamp": true, "GET /v1/sessions/{id}/live": true, "GET /v1/sessions/{id}/state": true,
	"GET /v1/sessions/{id}/screen": true, "GET /v1/sessions/{id}/conversation": true, "GET /v1/snippets": true, "GET /v1/memory/files": true,
	"GET /v1/memory/health": true, "GET /v1/memory/loaded": true, "GET /v1/trash/memory": true, "GET /v1/keepwarm": true,
	"GET /v1/update": true, "GET /v1/git/status": true, "GET /v1/teams/{team}/run": true, "GET /v1/teams": true, "GET /v1/account": true, "GET /v1/trust/pending": true, "GET /v1/cleanup-warning": true,
}

// tokenPrincipal : principal d'une requête Bearer ; nil tant que les jetons ne sont pas branchés.
var tokenPrincipal = func(*http.Request) *Principal { return nil }

// gateActive : le garde est monté devant le mux (mode multi-utilisateur), selon la décision prise pour cette requête.
func gateActive(request *http.Request) bool {
	_, decision := reqMode(request)
	return decision.on || decision.err != nil
}

type gateInfo struct {
	p     *Principal
	s     *authStore
	local bool // servie par la passerelle, jamais relayée à un home enfant
	pat   string
	need  Right
}

type gateKey struct{}

func gateFrom(request *http.Request) *gateInfo {
	gate, _ := request.Context().Value(gateKey{}).(*gateInfo)
	return gate
}

func (gate *gateInfo) rights(homeID string) (map[Right]bool, error) {
	if gate.p.Token {
		grantedRights := map[Right]bool{}
		for _, right := range allRights {
			grantedRights[right] = true
		}
		return grantedRights, nil
	}
	return gate.s.Rights(gate.p.User.ID, homeID)
}

// homeAllowed : sans garde, tout home est visible ; avec, seulement ceux où l'utilisateur a au moins un droit.
func (gate *gateInfo) homeAllowed(homeID string) bool {
	if gate == nil {
		return true
	}
	if gate.p == nil || homeID == "" {
		return false
	}
	grantedRights, err := gate.rights(homeID)
	return err == nil && anyRight(grantedRights)
}

func anyRight(grantedRights map[Right]bool) bool {
	for _, granted := range grantedRights {
		if granted {
			return true
		}
	}
	return false
}

func gateDeny(responseWriter http.ResponseWriter, code int, key string) {
	responseWriter.Header().Set("Cache-Control", "no-store")
	authMsg(responseWriter, code, L(key))
}

func gatePrincipal(store *authStore, request *http.Request, passive bool) (principal *Principal, bearer bool) {
	if strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
		if principal = tokenPrincipal(request); principal == nil || !principal.Token {
			return nil, true
		}
		return principal, true
	}
	cookie, err := request.Cookie(sessCookie)
	if err != nil {
		return nil, false
	}
	loadSession := store.Session
	if passive {
		loadSession = store.SessionPassive
	}
	principal, err = loadSession(cookie.Value)
	if err != nil {
		if !errors.Is(err, errNoSession) {
			journal("auth: session: %v", err)
		}
		return nil, false
	}
	if !principal.MFA {
		return nil, false
	}
	return principal, false
}

// pathSeg : le i-ième segment (0 = « v1 ») du chemin, décodé comme le fait le mux ; les PathValue ne sont posées qu'au
// dispatch, après le garde.
func pathSeg(request *http.Request, segmentIndex int) string {
	parts := strings.Split(strings.TrimPrefix(request.URL.EscapedPath(), "/"), "/")
	if segmentIndex >= len(parts) {
		return ""
	}
	segment, err := url.PathUnescape(parts[segmentIndex])
	if err != nil {
		return ""
	}
	return segment
}

const peekMax = 1 << 16 // la limite de lecture des handlers concernés

var errBodyTooBig = errors.New("body too large")

// peekJSON lit le corps pour décider, puis le remet en place, entier, pour le handler. Au-delà de peekMax, errBodyTooBig :
// décider sur un début de corps et relayer la suite serait juger une autre requête que celle servie.
func peekJSON(request *http.Request, destination any) error {
	body, err := io.ReadAll(io.LimitReader(request.Body, peekMax+1))
	if len(body) > peekMax {
		request.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), request.Body), request.Body}
		return errBodyTooBig
	}
	request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, destination)
}

func peekErr(err error) (int, string) {
	if errors.Is(err, errBodyTooBig) {
		return http.StatusRequestEntityTooLarge, "srv.http_util.requeteTropGrosse"
	}
	return http.StatusBadRequest, "srv.http_util.requeteInvalide"
}

// authGate applique routeRights. Le motif vient du mux qui servira la requête : une variante de chemin
// (double barre, encodage, barre finale) reçoit le droit de la route vers laquelle le mux la dirige.
func authGate(mux *http.ServeMux, store *authStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		_, pattern := mux.Handler(request)
		if pattern == "" { // 404 ou 405 : répondu ici, pour ne rien relayer à un home enfant
			http.NotFound(responseWriter, request)
			return
		}
		neededRight, knownRoute := routeRights[pattern]
		if !knownRoute {
			gateDeny(responseWriter, http.StatusForbidden, "srv.auth.droitRefuse")
			return
		}
		gate := &gateInfo{s: store, local: neededRight == rightPublic || neededRight == rightAny || neededRight == rightAdmin, pat: pattern, need: neededRight}
		if neededRight != rightPublic {
			principal, bearer := gatePrincipal(store, request, passiveRoutes[pattern])
			if principal == nil && bearer && !tokenTry(clientIP(request)) { // même plafond qu'en mode ouvert : un Bearer faux se devinait sans frein
				responseWriter.Header().Set("Retry-After", "60")
				gateDeny(responseWriter, http.StatusTooManyRequests, "srv.token.tooMany")
				return
			}
			if principal == nil {
				gateDeny(responseWriter, http.StatusUnauthorized, "srv.auth.sessionRequise")
				return
			}
			if !bearer && !csrfOK(request, principal) {
				gateDeny(responseWriter, http.StatusForbidden, "srv.auth.csrf")
				return
			}
			gate.p = principal
			if code, key := gateCheck(gate, request, pattern, neededRight); code != 0 {
				if code == http.StatusInternalServerError {
					journal("auth: gate %s: %s", pattern, key)
					key = "srv.auth.erreur"
				}
				gateDeny(responseWriter, code, key)
				return
			}
		}
		next.ServeHTTP(responseWriter, request.WithContext(context.WithValue(request.Context(), gateKey{}, gate)))
	})
}

// gateCheck renvoie 0 si la requête est permise, sinon le statut et la clé du message.
func gateCheck(gate *gateInfo, request *http.Request, pattern string, neededRight Right) (int, string) {
	switch {
	case neededRight == rightAdmin:
		if gate.p.Token || gate.p.User == nil || !gate.p.User.Admin {
			return http.StatusForbidden, "srv.auth.droitRefuse"
		}
		return 0, ""
	case pattern == "PUT /v1/homes/current":
		var requestBody struct{ Name string }
		if err := peekJSON(request, &requestBody); err != nil {
			return peekErr(err)
		}
		homeID, homeKnown := homeIDFor(requestBody.Name)
		if !homeKnown {
			return http.StatusForbidden, "srv.auth.droitRefuse"
		}
		return rightsErr(gate, homeID, func(grantedRights map[Right]bool) bool { return anyRight(grantedRights) })
	case neededRight == rightAny:
		return 0, ""
	case pattern == "POST /v1/homes/sync-plans" || pattern == "POST /v1/homes/syncs":
		var requestBody struct {
			From, To string
			Picks    []SyncPick
		}
		if err := peekJSON(request, &requestBody); err != nil {
			return peekErr(err)
		}
		// Le plan renvoie des aperçus de la destination ; appliquer un réglage, une skill ou une commande y fait exécuter du code.
		destNeeds := []Right{RightMemSync, RightMemWrite}
		if pattern == "POST /v1/homes/sync-plans" {
			destNeeds = append(destNeeds, RightMemRead)
		}
		for _, pick := range requestBody.Picks {
			if syncPickNeedsConfig(pick.ID) {
				destNeeds = append(destNeeds, RightConfigWrite)
				break
			}
		}
		from, fromKnown := homeIDFor(requestBody.From)
		to, toKnown := homeIDFor(requestBody.To)
		if !fromKnown || !toKnown {
			return http.StatusForbidden, "srv.auth.droitRefuse"
		}
		if code, key := rightsErr(gate, from, func(grantedRights map[Right]bool) bool {
			return grantedRights[RightMemSync] && grantedRights[RightMemRead]
		}); code != 0 {
			return code, key
		}
		return rightsErr(gate, to, func(grantedRights map[Right]bool) bool {
			for _, destNeed := range destNeeds {
				if !grantedRights[destNeed] {
					return false
				}
			}
			return true
		})
	}
	homeID, homeKnown := homeIDFor(homeFrom(request))
	if !homeKnown {
		return http.StatusForbidden, "srv.auth.droitRefuse"
	}
	if pattern == "PATCH /v1/trash/memory/{id}" {
		if trashNeedsConfig(homeFrom(request), pathSeg(request, 3)) {
			return rightsErr(gate, homeID, func(grantedRights map[Right]bool) bool {
				return grantedRights[neededRight] && grantedRights[RightConfigWrite]
			})
		}
	}
	// Le home enfant ne voit aucune passerelle (ideCodeDenied n'y décide rien) : config_write sur les chemins exécutables se tranche ici.
	if fsCodeWrite(request, pattern) {
		return rightsErr(gate, homeID, func(grantedRights map[Right]bool) bool {
			return grantedRights[neededRight] && grantedRights[RightConfigWrite]
		})
	}
	if pattern == "POST /v1/memory/merges" || pattern == "POST /v1/snippets/merges" {
		var requestBody struct{ Launch bool }
		if err := peekJSON(request, &requestBody); err != nil {
			return peekErr(err)
		}
		// Lancer ouvre une session claude avec les arguments de l'appelant : même droit que l'ouverture d'une session.
		if requestBody.Launch {
			return rightsErr(gate, homeID, func(grantedRights map[Right]bool) bool { return grantedRights[neededRight] && grantedRights[RightExec] })
		}
	}
	// Les brouillons lisent côté serveur les fichiers désignés par l'appelant et renvoient la sortie du modèle : sans mem_read,
	// ils contourneraient le droit de lecture.
	if pattern == "POST /v1/memory/drafts" || pattern == "POST /v1/skills/drafts" {
		return rightsErr(gate, homeID, func(grantedRights map[Right]bool) bool {
			return grantedRights[neededRight] && grantedRights[RightMemRead]
		})
	}
	if pattern == "PUT /v1/vgroups" {
		var submittedStore Store
		if err := peekJSON(request, &submittedStore); err != nil {
			return peekErr(err)
		}
		if vgroupsAddsDirs(homeFrom(request), submittedStore) {
			return rightsErr(gate, homeID, func(grantedRights map[Right]bool) bool { return grantedRights[neededRight] && grantedRights[RightExec] })
		}
	}
	return rightsErr(gate, homeID, func(grantedRights map[Right]bool) bool { return grantedRights[neededRight] })
}

// fsCodeWrite : la requête d'écriture de l'IDE touche un chemin exécutable (ideCodePath). Corps illisible ou trop gros : oui.
func fsCodeWrite(request *http.Request, pattern string) bool {
	query := request.URL.Query()
	switch pattern {
	case "PUT /v1/fs/entries/content":
		return ideCodePath(query.Get("path"))
	case "DELETE /v1/fs/entries":
		return ideCodePath(strings.Join(query["item"], "/"))
	case "POST /v1/fs/entries":
		var requestBody struct{ Path string }
		return peekJSON(request, &requestBody) != nil || ideCodePath(requestBody.Path)
	case "PATCH /v1/fs/entries":
		var requestBody struct{ From, To string }
		return peekJSON(request, &requestBody) != nil || ideCodePath(requestBody.From) || ideCodePath(requestBody.To)
	case "PATCH /v1/fs/trash/{entry}":
		return ideCodeRestore(query.Get("dir"), pathSeg(request, 3))
	}
	return false
}

// vgroupsAddsDirs : les dossiers enregistrés servent de racines à l'IDE, et POST /v1/directories (exec) est le seul moyen
// d'en ajouter. PUT /v1/vgroups renvoie l'ensemble : tout dossier absent du magasin actuel est un ajout.
// Magasin illisible ou home inconnu : ajout, donc exec.
func vgroupsAddsDirs(home string, submittedStore Store) bool {
	dir := cfgDir()
	if home != "" {
		homeEntry, homeFound := homeByName(home)
		if !homeFound {
			return true
		}
		dir = homeEntry.Dir
	}
	var currentStore Store
	if storeContent, err := readTrustedIn(dir, filepath.Join(dir, "gld-web-groups.json")); err == nil {
		json.Unmarshal(storeContent, &currentStore)
	}
	known := map[string]bool{}
	for _, registeredDir := range currentStore.Dirs {
		known[filepath.Clean(registeredDir)] = true
	}
	for _, project := range currentStore.Projects {
		for _, registeredDir := range project.Dirs {
			known[filepath.Clean(registeredDir)] = true
		}
	}
	for _, registeredDir := range submittedStore.Dirs {
		if !known[filepath.Clean(registeredDir)] {
			return true
		}
	}
	for _, project := range submittedStore.Projects {
		for _, registeredDir := range project.Dirs {
			if !known[filepath.Clean(registeredDir)] {
				return true
			}
		}
	}
	return false
}

// memKeyPrefixes : les clés de mémoire (memResolve), qui ne mènent jamais à commands/, skills/ ni aux réglages.
var memKeyPrefixes = []string{"g/CLAUDE.md", "g/rules/", "g/gldrules/", "m/", "r/", "p/"}

// trashNeedsConfig : restaurer un snippet (s/…) ou un skill (k/…) recrée une commande slash ou un skill exécutables.
// Élément introuvable, illisible ou de forme inconnue : config_write aussi.
func trashNeedsConfig(home, trashID string) bool {
	dir := cfgDir()
	if home != "" {
		homeEntry, homeFound := homeByName(home)
		if !homeFound {
			return true
		}
		dir = homeEntry.Dir
	}
	if trashID == "" || trashID == "." || trashID == ".." || strings.ContainsAny(trashID, `/\`) {
		return true
	}
	metaContent, err := readTrustedIn(dir, filepath.Join(dir, "gld-web-trash", trashID, "meta.json")) // la garde ne lit jamais hors du home
	var trashed Trashed
	if err != nil || json.Unmarshal(metaContent, &trashed) != nil || trashed.ID != trashID {
		return true
	}
	for _, keyPrefix := range memKeyPrefixes {
		if trashed.Key == keyPrefix || (strings.HasSuffix(keyPrefix, "/") && strings.HasPrefix(trashed.Key, keyPrefix)) {
			return false
		}
	}
	return true
}

func rightsErr(gate *gateInfo, homeID string, isAllowed func(map[Right]bool) bool) (int, string) {
	grantedRights, err := gate.rights(homeID)
	switch {
	case errors.Is(err, errTamper):
		return http.StatusForbidden, "srv.auth.tamper"
	case err != nil:
		return http.StatusInternalServerError, err.Error()
	case !isAllowed(grantedRights):
		return http.StatusForbidden, "srv.auth.droitRefuse"
	}
	return 0, ""
}

// Flux longs : le garde ne juge qu'à l'ouverture, une session révoquée ou un droit retiré doit aussi couper un flux déjà ouvert.
var (
	authWatchEvery = 5 * time.Second
	watchedRoutes  = map[string]bool{"GET /v1/terminal/ws": true, "POST /v1/sessions/{id}/chat": true, "GET /v1/fs/watch": true}
	errAuthRevoked = errors.New("auth: access revoked")
)

// authWS relit la session (expirée, révoquée, compte désactivé) et le droit sur le home de la requête. Sans garde, ou pour le
// jeton (fixe pour la vie du processus), rien à revérifier.
func authWS(request *http.Request, right Right) error {
	gate := gateFrom(request)
	if gate == nil || gate.p == nil || gate.p.Token {
		return nil
	}
	cookie, err := request.Cookie(sessCookie)
	if err != nil {
		return errAuthRevoked
	}
	principal, err := gate.s.SessionPassive(cookie.Value)
	if err != nil || !principal.MFA || principal.User == nil || gate.p.User == nil || principal.User.ID != gate.p.User.ID {
		return errAuthRevoked
	}
	if right == rightAny || right == rightPublic {
		return nil
	}
	homeID, homeKnown := homeIDFor(homeFrom(request))
	if !homeKnown {
		return errAuthRevoked
	}
	grantedRights, err := gate.s.Rights(principal.User.ID, homeID)
	if err != nil || !grantedRights[right] {
		return errAuthRevoked
	}
	return nil
}

// withAuthWatch annule le contexte d'un flux long dès que authWS échoue : le WebSocket local se ferme (sa lecture suit ce
// contexte), le proxy d'un home enfant coupe l'amont et le client, POST .../chat arrête le tour en cours.
func withAuthWatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		gate := gateFrom(request)
		if gate == nil || gate.p == nil || !watchedRoutes[gate.pat] {
			next.ServeHTTP(responseWriter, request)
			return
		}
		// POST .../chat relayé à un enfant : la coupure ne lui parvient que comme un client parti, et un tour continue alors (onglet
		// fermé). La passerelle lui demande donc l'arrêt explicitement, avec l'identifiant lu ici (l'enfant n'en lit pas plus).
		var chatID, child string
		if gate.pat == "POST /v1/sessions/{id}/chat" && homeFrom(request) != "" {
			chatID, child = pathSeg(request, 2), homeFrom(request)
		}
		watchContext, cancel := context.WithCancelCause(request.Context())
		defer cancel(nil)
		request = request.WithContext(watchContext)
		go func() {
			ticker := time.NewTicker(authWatchEvery)
			defer ticker.Stop()
			for {
				select {
				case <-watchContext.Done():
					return
				case <-ticker.C:
					if authWS(request, gate.need) != nil {
						journal("auth: flux %s coupé (session ou droit retiré)", gate.pat)
						cancel(errAuthRevoked)
						if chatID != "" {
							stopChildChat(child, chatID)
						}
						return
					}
				}
			}
		}()
		next.ServeHTTP(responseWriter, request)
	})
}

// chatStopPath : l'identifiant vient d'un segment décodé (%2F possible) et la requête part avec l'autorité de la passerelle.
func chatStopPath(chatID string) (string, bool) {
	if !idRe.MatchString(chatID) {
		return "", false
	}
	return "/v1/sessions/" + chatID + "/chat", true
}

// stopChildChat envoie DELETE /v1/sessions/{id}/chat à l'enfant du home, par son proxy (en-tête X-Gld-Child compris).
func stopChildChat(home, chatID string) {
	path, pathValid := chatStopPath(chatID)
	if !pathValid {
		journal("auth: arrêt du tour sur %s ignoré : identifiant invalide", home)
		return
	}
	proxy, err := homeProxy(home)
	if err != nil {
		journal("auth: arrêt du tour %s sur %s : %v", chatID, home, err)
		return
	}
	stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopRequest, _ := http.NewRequestWithContext(stopContext, "DELETE", path, nil) // #nosec G704 -- chemin construit par chatStopPath, appel interne via le proxy
	discard := &discardWriter{h: http.Header{}}
	proxy.ServeHTTP(discard, stopRequest)
	journal("auth: arrêt du tour %s sur %s demandé : %d", chatID, home, discard.code)
}

type discardWriter struct {
	h    http.Header
	code int
}

func (writer *discardWriter) Header() http.Header            { return writer.h }
func (writer *discardWriter) Write(data []byte) (int, error) { return len(data), nil }
func (writer *discardWriter) WriteHeader(statusCode int)     { writer.code = statusCode }

// callerHas : l'appelant a ce droit sur le home de la requête. Sans garde ni passerelle (mode ouvert ou jeton) tout est permis ;
// un home enfant lit les droits que sa passerelle a posés dans rightsHeader (absent : mode ouvert côté passerelle).
func callerHas(request *http.Request, right Right) bool {
	if gate := gateFrom(request); gate != nil {
		if gate.p == nil {
			return false
		}
		homeID, homeKnown := homeIDFor(homeFrom(request))
		if !homeKnown {
			return false
		}
		grantedRights, err := gate.rights(homeID)
		return err == nil && grantedRights[right]
	}
	if childToken != "" {
		if !childAuthorized(request) {
			return false
		}
		if rightsValues := request.Header.Values(rightsHeader); len(rightsValues) > 0 {
			return slices.Contains(strings.Split(rightsValues[0], ","), string(right))
		}
	}
	return true
}
