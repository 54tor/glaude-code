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
	"GET /{$}": rightPublic, "GET /toutes": rightPublic, "GET /en-cours": rightPublic, "GET /dossier/": rightPublic,
	"GET /groupe/": rightPublic, "GET /projet/": rightPublic, "GET /snippets": rightPublic, "GET /snippets/": rightPublic,
	"GET /memoire": rightPublic, "GET /memoire/": rightPublic, "GET /onglets": rightPublic, "GET /configuration": rightPublic,
	"GET /configuration/": rightPublic, "GET /skills": rightPublic, "GET /skills/": rightPublic, "GET /extensions": rightPublic, "GET /equipes": rightPublic, "GET /equipes/": rightPublic, "GET /revues": rightPublic, "GET /espace/": rightPublic, "GET /reglages/": rightPublic, "GET /assets/": rightPublic,
	"GET /icons/": rightPublic, "GET /favicon.ico": rightPublic, "GET /manifest.webmanifest": rightPublic, "GET /sw.js": rightPublic,
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
	"GET /v1/trash/memory": RightMemRead, "GET /v1/trash/memory/{id}/file": RightMemRead, "PATCH /v1/trash/memory/{id}": RightMemWrite, "DELETE /v1/trash/memory": RightMemWrite,
	"GET /v1/skills": RightMemRead, "GET /v1/skills/catalog": RightMemRead, "GET /v1/skills/updates": RightMemRead, "GET /v1/skills/{key}": RightMemRead, "GET /v1/plugins": RightMemRead, "GET /v1/marketplaces": RightMemRead,

	// Lecture du home et état d'interface par home.
	"GET /v1/config": RightMemRead, "GET /v1/config/projects": RightMemRead, "GET /v1/config/effective": RightMemRead,
	"GET /v1/config/mcp": RightMemRead, "GET /v1/slash-commands": RightMemRead,
	"GET /v1/git/status": RightMemRead, "GET /v1/git/changes": RightMemRead, "GET /v1/git/remotes": RightMemRead,
	"GET /v1/cleanup-warning": RightMemRead, "GET /v1/tabs": RightMemRead, "GET /v1/keepwarm": RightMemRead,
	"PUT /v1/tabs": RightMemWrite, "DELETE /v1/tabs/{tab}": RightMemWrite, "PUT /v1/vgroups": RightMemWrite,

	// Réglages et skills : font exécuter du code par Claude Code.
	"POST /v1/config/edits": RightConfigWrite, "GET /v1/config/raw": RightConfigWrite, "PUT /v1/config/raw": RightConfigWrite,
	"POST /v1/config/restorations": RightConfigWrite, "POST /v1/config/permissions/imports": RightConfigWrite, "POST /v1/config/mcp/checks": RightConfigWrite, "PUT /v1/config/projects/mcp": RightConfigWrite,
	"POST /v1/config/permissions/explanations": RightConfigWrite,
	"PUT /v1/snippets/{key}":                   RightConfigWrite, "DELETE /v1/snippets": RightConfigWrite, "PATCH /v1/snippets/{key}": RightConfigWrite, "POST /v1/snippets/imports": RightConfigWrite,
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
	"GET /v1/git/status": true, "GET /v1/teams/{team}/run": true, "GET /v1/teams": true, "GET /v1/account": true, "GET /v1/trust/pending": true, "GET /v1/cleanup-warning": true,
}

// tokenPrincipal : principal d'une requête Bearer ; nil tant que les jetons ne sont pas branchés.
var tokenPrincipal = func(*http.Request) *Principal { return nil }

// gateActive : le garde est monté devant le mux (mode multi-utilisateur), selon la décision prise pour cette requête.
func gateActive(r *http.Request) bool {
	_, d := reqMode(r)
	return d.on || d.err != nil
}

type gateInfo struct {
	p     *Principal
	s     *authStore
	local bool // servie par la passerelle, jamais relayée à un home enfant
	pat   string
	need  Right
}

type gateKey struct{}

func gateFrom(r *http.Request) *gateInfo {
	g, _ := r.Context().Value(gateKey{}).(*gateInfo)
	return g
}

func (g *gateInfo) rights(homeID string) (map[Right]bool, error) {
	if g.p.Token {
		m := map[Right]bool{}
		for _, r := range allRights {
			m[r] = true
		}
		return m, nil
	}
	return g.s.Rights(g.p.User.ID, homeID)
}

// homeAllowed : sans garde, tout home est visible ; avec, seulement ceux où l'utilisateur a au moins un droit.
func (g *gateInfo) homeAllowed(homeID string) bool {
	if g == nil {
		return true
	}
	if g.p == nil || homeID == "" {
		return false
	}
	m, err := g.rights(homeID)
	return err == nil && anyRight(m)
}

func anyRight(m map[Right]bool) bool {
	for _, v := range m {
		if v {
			return true
		}
	}
	return false
}

func gateDeny(w http.ResponseWriter, code int, key string) {
	w.Header().Set("Cache-Control", "no-store")
	authMsg(w, code, L(key))
}

func gatePrincipal(s *authStore, r *http.Request, passive bool) (p *Principal, bearer bool) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		if p = tokenPrincipal(r); p == nil || !p.Token {
			return nil, true
		}
		return p, true
	}
	c, err := r.Cookie(sessCookie)
	if err != nil {
		return nil, false
	}
	get := s.Session
	if passive {
		get = s.SessionPassive
	}
	p, err = get(c.Value)
	if err != nil {
		if !errors.Is(err, errNoSession) {
			journal("auth: session: %v", err)
		}
		return nil, false
	}
	if !p.MFA {
		return nil, false
	}
	return p, false
}

// pathSeg : le i-ième segment (0 = « v1 ») du chemin, décodé comme le fait le mux ; les PathValue ne sont posées qu'au
// dispatch, après le garde.
func pathSeg(r *http.Request, i int) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	if i >= len(parts) {
		return ""
	}
	v, err := url.PathUnescape(parts[i])
	if err != nil {
		return ""
	}
	return v
}

const peekMax = 1 << 16 // la limite de lecture des handlers concernés

var errBodyTooBig = errors.New("body too large")

// peekJSON lit le corps pour décider, puis le remet en place, entier, pour le handler. Au-delà de peekMax, errBodyTooBig :
// décider sur un début de corps et relayer la suite serait juger une autre requête que celle servie.
func peekJSON(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, peekMax+1))
	if len(b) > peekMax {
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(b), r.Body), r.Body}
		return errBodyTooBig
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(b))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func peekErr(err error) (int, string) {
	if errors.Is(err, errBodyTooBig) {
		return 413, "srv.http_util.requeteTropGrosse"
	}
	return 400, "srv.http_util.requeteInvalide"
}

// authGate applique routeRights. Le motif vient du mux qui servira la requête : une variante de chemin
// (double barre, encodage, barre finale) reçoit le droit de la route vers laquelle le mux la dirige.
func authGate(mux *http.ServeMux, s *authStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pat := mux.Handler(r)
		if pat == "" { // 404 ou 405 : répondu ici, pour ne rien relayer à un home enfant
			http.NotFound(w, r)
			return
		}
		need, ok := routeRights[pat]
		if !ok {
			gateDeny(w, 403, "srv.auth.droitRefuse")
			return
		}
		g := &gateInfo{s: s, local: need == rightPublic || need == rightAny || need == rightAdmin, pat: pat, need: need}
		if need != rightPublic {
			p, bearer := gatePrincipal(s, r, passiveRoutes[pat])
			if p == nil {
				gateDeny(w, 401, "srv.auth.sessionRequise")
				return
			}
			if !bearer && !csrfOK(r, p) {
				gateDeny(w, 403, "srv.auth.csrf")
				return
			}
			g.p = p
			if code, key := gateCheck(g, r, pat, need); code != 0 {
				if code == 500 {
					journal("auth: gate %s: %s", pat, key)
					key = "srv.auth.erreur"
				}
				gateDeny(w, code, key)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), gateKey{}, g)))
	})
}

// gateCheck renvoie 0 si la requête est permise, sinon le statut et la clé du message.
func gateCheck(g *gateInfo, r *http.Request, pat string, need Right) (int, string) {
	switch {
	case need == rightAdmin:
		if g.p.Token || g.p.User == nil || !g.p.User.Admin {
			return 403, "srv.auth.droitRefuse"
		}
		return 0, ""
	case pat == "PUT /v1/homes/current":
		var req struct{ Name string }
		if err := peekJSON(r, &req); err != nil {
			return peekErr(err)
		}
		id, ok := homeIDFor(req.Name)
		if !ok {
			return 403, "srv.auth.droitRefuse"
		}
		return rightsErr(g, id, func(m map[Right]bool) bool { return anyRight(m) })
	case need == rightAny:
		return 0, ""
	case pat == "POST /v1/homes/sync-plans" || pat == "POST /v1/homes/syncs":
		var req struct {
			From, To string
			Picks    []SyncPick
		}
		if err := peekJSON(r, &req); err != nil {
			return peekErr(err)
		}
		// Le plan renvoie des aperçus de la destination ; appliquer un réglage, une skill ou une commande y fait exécuter du code.
		destNeeds := []Right{RightMemSync, RightMemWrite}
		if pat == "POST /v1/homes/sync-plans" {
			destNeeds = append(destNeeds, RightMemRead)
		}
		for _, p := range req.Picks {
			if syncPickNeedsConfig(p.ID) {
				destNeeds = append(destNeeds, RightConfigWrite)
				break
			}
		}
		from, ok1 := homeIDFor(req.From)
		to, ok2 := homeIDFor(req.To)
		if !ok1 || !ok2 {
			return 403, "srv.auth.droitRefuse"
		}
		if code, key := rightsErr(g, from, func(m map[Right]bool) bool { return m[RightMemSync] && m[RightMemRead] }); code != 0 {
			return code, key
		}
		return rightsErr(g, to, func(m map[Right]bool) bool {
			for _, n := range destNeeds {
				if !m[n] {
					return false
				}
			}
			return true
		})
	}
	id, ok := homeIDFor(homeFrom(r))
	if !ok {
		return 403, "srv.auth.droitRefuse"
	}
	if pat == "PATCH /v1/trash/memory/{id}" {
		if trashNeedsConfig(homeFrom(r), pathSeg(r, 3)) {
			return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] && m[RightConfigWrite] })
		}
	}
	if pat == "POST /v1/memory/merges" || pat == "POST /v1/snippets/merges" {
		var req struct{ Launch bool }
		if err := peekJSON(r, &req); err != nil {
			return peekErr(err)
		}
		// Lancer ouvre une session claude avec les arguments de l'appelant : même droit que l'ouverture d'une session.
		if req.Launch {
			return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] && m[RightExec] })
		}
	}
	// Les brouillons lisent côté serveur les fichiers désignés par l'appelant et renvoient la sortie du modèle : sans mem_read,
	// ils contourneraient le droit de lecture.
	if pat == "POST /v1/memory/drafts" || pat == "POST /v1/skills/drafts" {
		return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] && m[RightMemRead] })
	}
	if pat == "PUT /v1/vgroups" {
		var st Store
		if err := peekJSON(r, &st); err != nil {
			return peekErr(err)
		}
		if vgroupsAddsDirs(homeFrom(r), st) {
			return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] && m[RightExec] })
		}
	}
	return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] })
}

// vgroupsAddsDirs : les dossiers enregistrés servent de racines à l'IDE, et POST /v1/directories (exec) est le seul moyen
// d'en ajouter. PUT /v1/vgroups renvoie l'ensemble : tout dossier absent du magasin actuel est un ajout.
// Magasin illisible ou home inconnu : ajout, donc exec.
func vgroupsAddsDirs(home string, st Store) bool {
	dir := cfgDir()
	if home != "" {
		h, ok := homeByName(home)
		if !ok {
			return true
		}
		dir = h.Dir
	}
	var old Store
	if b, err := readTrustedIn(dir, filepath.Join(dir, "gld-web-groups.json")); err == nil {
		json.Unmarshal(b, &old)
	}
	known := map[string]bool{}
	for _, d := range old.Dirs {
		known[filepath.Clean(d)] = true
	}
	for _, p := range old.Projects {
		for _, d := range p.Dirs {
			known[filepath.Clean(d)] = true
		}
	}
	for _, d := range st.Dirs {
		if !known[filepath.Clean(d)] {
			return true
		}
	}
	for _, p := range st.Projects {
		for _, d := range p.Dirs {
			if !known[filepath.Clean(d)] {
				return true
			}
		}
	}
	return false
}

// memKeyPrefixes : les clés de mémoire (memResolve), qui ne mènent jamais à commands/, skills/ ni aux réglages.
var memKeyPrefixes = []string{"g/CLAUDE.md", "g/rules/", "m/", "r/", "p/"}

// trashNeedsConfig : restaurer un snippet (s/…) ou un skill (k/…) recrée une commande slash ou un skill exécutables.
// Élément introuvable, illisible ou de forme inconnue : config_write aussi.
func trashNeedsConfig(home, id string) bool {
	dir := cfgDir()
	if home != "" {
		h, ok := homeByName(home)
		if !ok {
			return true
		}
		dir = h.Dir
	}
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return true
	}
	b, err := readTrustedIn(dir, filepath.Join(dir, "gld-web-trash", id, "meta.json")) // la garde ne lit jamais hors du home
	var t Trashed
	if err != nil || json.Unmarshal(b, &t) != nil || t.ID != id {
		return true
	}
	for _, p := range memKeyPrefixes {
		if t.Key == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(t.Key, p)) {
			return false
		}
	}
	return true
}

func rightsErr(g *gateInfo, homeID string, ok func(map[Right]bool) bool) (int, string) {
	m, err := g.rights(homeID)
	switch {
	case errors.Is(err, errTamper):
		return 403, "srv.auth.tamper"
	case err != nil:
		return 500, err.Error()
	case !ok(m):
		return 403, "srv.auth.droitRefuse"
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
func authWS(r *http.Request, right Right) error {
	g := gateFrom(r)
	if g == nil || g.p == nil || g.p.Token {
		return nil
	}
	c, err := r.Cookie(sessCookie)
	if err != nil {
		return errAuthRevoked
	}
	p, err := g.s.SessionPassive(c.Value)
	if err != nil || !p.MFA || p.User == nil || g.p.User == nil || p.User.ID != g.p.User.ID {
		return errAuthRevoked
	}
	if right == rightAny || right == rightPublic {
		return nil
	}
	id, ok := homeIDFor(homeFrom(r))
	if !ok {
		return errAuthRevoked
	}
	m, err := g.s.Rights(p.User.ID, id)
	if err != nil || !m[right] {
		return errAuthRevoked
	}
	return nil
}

// withAuthWatch annule le contexte d'un flux long dès que authWS échoue : le WebSocket local se ferme (sa lecture suit ce
// contexte), le proxy d'un home enfant coupe l'amont et le client, POST .../chat arrête le tour en cours.
func withAuthWatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g := gateFrom(r)
		if g == nil || g.p == nil || !watchedRoutes[g.pat] {
			next.ServeHTTP(w, r)
			return
		}
		// POST .../chat relayé à un enfant : la coupure ne lui parvient que comme un client parti, et un tour continue alors (onglet
		// fermé). La passerelle lui demande donc l'arrêt explicitement, avec l'identifiant lu ici (l'enfant n'en lit pas plus).
		var chatID, child string
		if g.pat == "POST /v1/sessions/{id}/chat" && homeFrom(r) != "" {
			chatID, child = pathSeg(r, 2), homeFrom(r)
		}
		ctx, cancel := context.WithCancelCause(r.Context())
		defer cancel(nil)
		r = r.WithContext(ctx)
		go func() {
			t := time.NewTicker(authWatchEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if authWS(r, g.need) != nil {
						journal("auth: flux %s coupé (session ou droit retiré)", g.pat)
						cancel(errAuthRevoked)
						if chatID != "" {
							stopChildChat(child, chatID)
						}
						return
					}
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// chatStopPath : l'identifiant vient d'un segment décodé (%2F possible) et la requête part avec l'autorité de la passerelle.
func chatStopPath(id string) (string, bool) {
	if !idRe.MatchString(id) {
		return "", false
	}
	return "/v1/sessions/" + id + "/chat", true
}

// stopChildChat envoie DELETE /v1/sessions/{id}/chat à l'enfant du home, par son proxy (en-tête X-Gld-Child compris).
func stopChildChat(home, id string) {
	path, ok := chatStopPath(id)
	if !ok {
		journal("auth: arrêt du tour sur %s ignoré : identifiant invalide", home)
		return
	}
	p, err := homeProxy(home)
	if err != nil {
		journal("auth: arrêt du tour %s sur %s : %v", id, home, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "DELETE", path, nil)
	w := &discardWriter{h: http.Header{}}
	p.ServeHTTP(w, req)
	journal("auth: arrêt du tour %s sur %s demandé : %d", id, home, w.code)
}

type discardWriter struct {
	h    http.Header
	code int
}

func (d *discardWriter) Header() http.Header         { return d.h }
func (d *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardWriter) WriteHeader(c int)           { d.code = c }
