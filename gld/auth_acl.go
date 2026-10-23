package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Valeurs propres au garde, hors allRights : Grant les refuse, aucune ligne home_acl ne peut les porter.
const (
	rightPublic Right = "public" // servie sans session ; les routes /auth/* vérifient elles-mêmes la session
	rightAny    Right = "any"    // session complète, sans droit sur un home (la route filtre elle-même)
	rightAdmin  Right = "admin"
)

// routeRights : chaque motif enregistré dans le mux, tel quel. Un motif absent est refusé.
var routeRights = map[string]Right{
	// Interface et ressources statiques.
	"GET /{$}": rightPublic, "GET /toutes": rightPublic, "GET /en-cours": rightPublic, "GET /dossier/": rightPublic,
	"GET /groupe/": rightPublic, "GET /projet/": rightPublic, "GET /snippets": rightPublic, "GET /snippets/": rightPublic,
	"GET /memoire": rightPublic, "GET /memoire/": rightPublic, "GET /onglets": rightPublic, "GET /configuration": rightPublic,
	"GET /configuration/": rightPublic, "GET /skills": rightPublic, "GET /skills/": rightPublic, "GET /assets/": rightPublic,
	"GET /icons/": rightPublic, "GET /favicon.ico": rightPublic, "GET /manifest.webmanifest": rightPublic,

	// Connexion et profil.
	"POST /auth/login": rightPublic, "POST /auth/mfa/totp": rightPublic, "POST /auth/mfa/recovery": rightPublic,
	"POST /auth/mfa/webauthn/begin": rightPublic, "POST /auth/mfa/webauthn/finish": rightPublic, "POST /auth/logout": rightPublic,
	"GET /auth/me": rightPublic, "POST /auth/totp/begin": rightPublic, "POST /auth/totp/confirm": rightPublic,
	"POST /auth/webauthn/register/begin": rightPublic, "POST /auth/webauthn/register/finish": rightPublic,
	"POST /auth/factor/remove": rightPublic, "POST /auth/recovery/regenerate": rightPublic, "POST /auth/password": rightPublic,
	"GET /auth/sessions": rightPublic, "POST /auth/sessions/revoke": rightPublic,

	// Homes.
	"GET /homes": rightAny, "POST /homes-select": rightAny, "POST /homes-save": rightAdmin,
	"POST /homes-sync-plan": RightMemSync, "POST /homes-sync-apply": RightMemSync,

	// Mémoire, règles, snippets, notes, corbeille, recherche.
	"GET /memory-list": RightMemRead, "GET /memory-file": RightMemRead, "GET /memory-health": RightMemRead,
	"GET /memory-matches": RightMemRead, "GET /memory-loaded": RightMemRead, "GET /memory-search": RightMemRead,
	"POST /memory-save": RightMemWrite, "POST /memory-move": RightMemWrite, "POST /memory-draft": RightMemWrite,
	"POST /memory-repair": RightMemWrite, "POST /memory-paths": RightMemWrite, "POST /memory-type": RightMemWrite,
	"POST /memory-fix": RightMemWrite, "POST /memory-tokens": RightMemWrite, "POST /memory-lint": RightMemWrite,
	"POST /memory-toggle": RightMemWrite, "POST /memory-rm": RightMemWrite, "POST /memory-merge": RightMemWrite,
	"GET /snippet-list": RightMemRead, "GET /snippet": RightMemRead, "GET /snippets-export": RightMemRead,
	"POST /snippet-save": RightMemWrite, "POST /snippet-draft": RightMemWrite, "POST /snippet-rm": RightMemWrite,
	"POST /snippets-import": RightMemWrite,
	"GET /notes":            RightMemRead, "POST /notes": RightMemWrite,
	"GET /trash": RightMemRead, "GET /trash-file": RightMemRead, "POST /trash-restore": RightMemWrite, "POST /trash-purge": RightMemWrite,
	"GET /search": RightMemRead, "GET /skills-list": RightMemRead, "GET /skill": RightMemRead,
	"POST /rename": RightMemWrite, "POST /rm": RightMemWrite,

	// Lecture du home et état d'interface par home.
	"GET /config": RightMemRead, "GET /config-projects": RightMemRead, "GET /config-effective": RightMemRead,
	"GET /config-mcp": RightMemRead, "GET /config-perm-suggest": RightMemRead, "GET /slash-commands": RightMemRead,
	"GET /git-status": RightMemRead, "GET /git-changes": RightMemRead, "GET /git-remotes": RightMemRead,
	"GET /cleanup-warning": RightMemRead, "GET /tabs": RightMemRead, "GET /keepwarm": RightMemRead,
	"POST /tabs-save": RightMemWrite, "POST /tabs-rm": RightMemWrite, "POST /keepwarm": RightMemWrite,
	"POST /vgroups": RightMemWrite, "POST /reassign": RightMemWrite,
	"POST /session-trash-add": RightMemWrite, "POST /session-trash-restore": RightMemWrite, "POST /session-trash-purge": RightMemWrite,

	// Réglages et skills : font exécuter du code par Claude Code.
	"POST /config-edit": RightConfigWrite, "GET /config-raw": RightConfigWrite, "POST /config-raw": RightConfigWrite,
	"POST /config-restore": RightConfigWrite, "POST /config-perm-import": RightConfigWrite, "POST /config-mcp-status": RightConfigWrite,
	"POST /config-perm-explain": RightConfigWrite,
	"POST /skill-save":          RightConfigWrite, "POST /skill-copy": RightConfigWrite, "POST /skill-rm": RightConfigWrite,
	"POST /skill-draft": RightConfigWrite,

	// Compte et usages.
	"GET /account": RightUsageRead, "POST /account-refresh": RightUsageRead,

	// Transcriptions et état des sessions. /info renvoie les premiers messages d'une session : contenu, pas usage.
	"GET /api": RightSessionsRead, "GET /conv": RightSessionsRead, "GET /conv-text": RightSessionsRead,
	"GET /conv-image": RightSessionsRead, "GET /info": RightSessionsRead,
	"GET /session-state": RightSessionsRead, "GET /session-live": RightSessionsRead, "GET /session-screen": RightSessionsRead,
	"GET /session-loaded": RightSessionsRead, "GET /session-trash": RightSessionsRead, "GET /sessions-stamp": RightSessionsRead,
	"GET /session-send-info": RightSessionsRead,

	// Exécution.
	"POST /chat": RightExec, "POST /chat-stop": RightExec, "POST /new-session": RightExec,
	"GET /term-info": RightExec, "POST /term-open": RightExec, "GET /term-ws": RightExec,
	"POST /session-send": RightExec, "POST /session-key": RightExec, "POST /session-close": RightExec,
	"GET /trust-pending": RightExec, "POST /trust-answer": RightExec, "POST /open": RightExec, "POST /git-push": RightExec,
	"GET /ls": RightExec, "POST /dirs": RightExec, "POST /image-upload": RightExec,
}

// Routes de sondage : ne prolongent pas la session, sans quoi un onglet ouvert ne l'expirerait jamais.
var passiveRoutes = map[string]bool{
	"GET /sessions-stamp": true, "GET /session-live": true, "GET /session-state": true,
	"GET /trust-pending": true, "GET /cleanup-warning": true,
}

// tokenPrincipal : principal d'une requête Bearer ; nil tant que les jetons ne sont pas branchés.
var tokenPrincipal = func(*http.Request) *Principal { return nil }

var gateOn bool

// gateActive : le garde est monté devant le mux (mode multi-utilisateur).
func gateActive() bool { return gateOn }

type gateInfo struct {
	p     *Principal
	s     *authStore
	local bool // servie par la passerelle, jamais relayée à un home enfant
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
	writeJSON(w, code, map[string]any{"ok": false, "out": L(key)})
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

// peekJSON lit le corps pour décider, puis le remet en place pour le handler.
func peekJSON(r *http.Request, v any) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(b))
	return err == nil && json.Unmarshal(b, v) == nil
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
		g := &gateInfo{s: s, local: need == rightPublic || need == rightAny || need == rightAdmin}
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
	case pat == "POST /homes-select":
		var req struct{ Name string }
		if !peekJSON(r, &req) {
			return 400, "srv.http_util.requeteInvalide"
		}
		id, ok := homeIDFor(req.Name)
		if !ok {
			return 403, "srv.auth.droitRefuse"
		}
		return rightsErr(g, id, func(m map[Right]bool) bool { return anyRight(m) })
	case need == rightAny:
		return 0, ""
	case pat == "POST /homes-sync-plan" || pat == "POST /homes-sync-apply":
		var req struct{ From, To string }
		if !peekJSON(r, &req) {
			return 400, "srv.http_util.requeteInvalide"
		}
		from, ok1 := homeIDFor(req.From)
		to, ok2 := homeIDFor(req.To)
		if !ok1 || !ok2 {
			return 403, "srv.auth.droitRefuse"
		}
		if code, key := rightsErr(g, from, func(m map[Right]bool) bool { return m[RightMemSync] && m[RightMemRead] }); code != 0 {
			return code, key
		}
		return rightsErr(g, to, func(m map[Right]bool) bool { return m[RightMemSync] && m[RightMemWrite] })
	}
	id, ok := homeIDFor(homeFrom(r))
	if !ok {
		return 403, "srv.auth.droitRefuse"
	}
	return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] })
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
