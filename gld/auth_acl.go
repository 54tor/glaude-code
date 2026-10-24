package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	"GET /configuration/": rightPublic, "GET /skills": rightPublic, "GET /skills/": rightPublic, "GET /espace/": rightPublic, "GET /assets/": rightPublic,
	"GET /icons/": rightPublic, "GET /favicon.ico": rightPublic, "GET /manifest.webmanifest": rightPublic,

	// Connexion et profil.
	"POST /auth/login": rightPublic, "POST /auth/mfa/totp": rightPublic, "POST /auth/mfa/recovery": rightPublic,
	"POST /auth/mfa/webauthn/begin": rightPublic, "POST /auth/mfa/webauthn/finish": rightPublic, "POST /auth/logout": rightPublic,
	"GET /auth/me": rightPublic, "POST /auth/totp/begin": rightPublic, "POST /auth/totp/confirm": rightPublic,
	"POST /auth/webauthn/register/begin": rightPublic, "POST /auth/webauthn/register/finish": rightPublic,
	"POST /auth/factor/remove": rightPublic, "POST /auth/recovery/regenerate": rightPublic, "POST /auth/password": rightPublic,
	"GET /auth/sessions": rightPublic, "POST /auth/sessions/revoke": rightPublic,

	// Administration des comptes et des droits.
	"GET /admin/users": rightAdmin, "POST /admin/users": rightAdmin, "POST /admin/users/disable": rightAdmin,
	"POST /admin/users/reset-mfa": rightAdmin, "POST /admin/users/password": rightAdmin, "POST /admin/acl": rightAdmin,
	"GET /admin/events": rightAdmin, "GET /admin/homes": rightAdmin, "GET /admin/sessions": rightAdmin, "POST /admin/sessions/revoke": rightAdmin,

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
	// /snippet-draft ne fait qu'un brouillon (même appel de modèle que /memory-draft) ; écrire un snippet, c'est écrire
	// une commande slash de Claude Code (commands/*.md), donc config_write.
	"POST /snippet-draft": RightMemWrite,
	"GET /notes":          RightMemRead, "POST /notes": RightMemWrite,
	"GET /trash": RightMemRead, "GET /trash-file": RightMemRead, "POST /trash-restore": RightMemWrite, "POST /trash-purge": RightMemWrite,
	"GET /skills-list": RightMemRead, "GET /skill": RightMemRead,

	// Lecture du home et état d'interface par home.
	"GET /config": RightMemRead, "GET /config-projects": RightMemRead, "GET /config-effective": RightMemRead,
	"GET /config-mcp": RightMemRead, "GET /slash-commands": RightMemRead,
	"GET /git-status": RightMemRead, "GET /git-changes": RightMemRead, "GET /git-remotes": RightMemRead,
	"GET /cleanup-warning": RightMemRead, "GET /tabs": RightMemRead, "GET /keepwarm": RightMemRead,
	"POST /tabs-save": RightMemWrite, "POST /tabs-rm": RightMemWrite, "POST /vgroups": RightMemWrite,

	// Réglages et skills : font exécuter du code par Claude Code.
	"POST /config-edit": RightConfigWrite, "GET /config-raw": RightConfigWrite, "POST /config-raw": RightConfigWrite,
	"POST /config-restore": RightConfigWrite, "POST /config-perm-import": RightConfigWrite, "POST /config-mcp-status": RightConfigWrite,
	"POST /config-perm-explain": RightConfigWrite,
	"POST /snippet-save":        RightConfigWrite, "POST /snippet-rm": RightConfigWrite, "POST /snippets-import": RightConfigWrite,
	"POST /skill-save": RightConfigWrite, "POST /skill-copy": RightConfigWrite, "POST /skill-rm": RightConfigWrite,
	"POST /skill-draft": RightConfigWrite,

	// Compte et usages.
	"GET /account": RightUsageRead, "POST /account-refresh": RightUsageRead,

	// Transcriptions et état des sessions. /info renvoie les premiers messages d'une session : contenu, pas usage.
	"GET /api": RightSessionsRead, "GET /conv": RightSessionsRead, "GET /conv-text": RightSessionsRead,
	"GET /conv-image": RightSessionsRead, "GET /info": RightSessionsRead,
	"GET /session-state": RightSessionsRead, "GET /session-live": RightSessionsRead, "GET /session-screen": RightSessionsRead,
	"GET /session-loaded": RightSessionsRead, "GET /session-trash": RightSessionsRead, "GET /sessions-stamp": RightSessionsRead,
	"GET /session-send-info": RightSessionsRead, "GET /search": RightSessionsRead,
	// Renvoie des extraits de commandes tirés des transcriptions.
	"GET /config-perm-suggest": RightSessionsRead,

	// Exécution.
	"POST /chat": RightExec, "POST /chat-stop": RightExec, "POST /new-session": RightExec,
	"GET /fs-tree": RightFsRead, "GET /fs-file": RightFsRead, "GET /fs-trash": RightFsRead,
	"GET /fs-index": RightFsRead, "POST /fs-files": RightFsRead, "GET /fs-watch": RightFsRead,
	"POST /fs-save": RightFsWrite, "POST /fs-new": RightFsWrite, "POST /fs-rename": RightFsWrite, "POST /fs-rm": RightFsWrite,
	"POST /fs-trash-restore": RightFsWrite, "POST /fs-trash-purge": RightFsWrite,
	"GET /term-info": RightExec, "POST /term-open": RightExec, "GET /term-ws": RightExec,
	"POST /session-send": RightExec, "POST /session-key": RightExec, "POST /session-close": RightExec,
	"GET /trust-pending": RightExec, "POST /trust-answer": RightExec, "POST /open": RightExec, "POST /git-push": RightExec,
	"GET /ls": RightExec, "POST /dirs": RightExec, "POST /image-upload": RightExec,
	// Agissent sur les sessions : fermeture, suppression de transcriptions, tours de modèle.
	"POST /rm": RightExec, "POST /rename": RightExec, "POST /reassign": RightExec, "POST /keepwarm": RightExec,
	"POST /session-trash-add": RightExec, "POST /session-trash-restore": RightExec, "POST /session-trash-purge": RightExec,
}

// Routes de sondage : ne prolongent pas la session, sans quoi un onglet ouvert ne l'expirerait jamais.
var passiveRoutes = map[string]bool{
	"GET /api": true, "GET /sessions-stamp": true, "GET /session-live": true, "GET /session-state": true,
	"GET /session-screen": true, "GET /conv": true, "GET /snippet-list": true, "GET /memory-list": true,
	"GET /memory-health": true, "GET /memory-loaded": true, "GET /trash": true, "GET /keepwarm": true,
	"GET /git-status": true, "GET /account": true, "GET /trust-pending": true, "GET /cleanup-warning": true,
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
	case pat == "POST /homes-select":
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
	case pat == "POST /homes-sync-plan" || pat == "POST /homes-sync-apply":
		var req struct {
			From, To string
			Picks    []SyncPick
		}
		if err := peekJSON(r, &req); err != nil {
			return peekErr(err)
		}
		// Le plan renvoie des aperçus de la destination ; appliquer un réglage, une skill ou une commande y fait exécuter du code.
		destNeeds := []Right{RightMemSync, RightMemWrite}
		if pat == "POST /homes-sync-plan" {
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
	if pat == "POST /trash-restore" {
		var req struct{ ID string }
		if err := peekJSON(r, &req); err != nil {
			return peekErr(err)
		}
		if trashNeedsConfig(homeFrom(r), req.ID) {
			return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] && m[RightConfigWrite] })
		}
	}
	return rightsErr(g, id, func(m map[Right]bool) bool { return m[need] })
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
	b, err := os.ReadFile(filepath.Join(dir, "gld-web-trash", id, "meta.json"))
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
	watchedRoutes  = map[string]bool{"GET /term-ws": true, "POST /chat": true}
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
// contexte), le proxy d'un home enfant coupe l'amont et le client, /chat arrête le tour en cours.
func withAuthWatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g := gateFrom(r)
		if g == nil || g.p == nil || !watchedRoutes[g.pat] {
			next.ServeHTTP(w, r)
			return
		}
		// /chat relayé à un enfant : la coupure ne lui parvient que comme un client parti, et un tour continue alors (onglet
		// fermé). La passerelle lui demande donc l'arrêt explicitement, avec l'identifiant lu ici (l'enfant n'en lit pas plus).
		var chatID, child string
		if g.pat == "POST /chat" && homeFrom(r) != "" {
			var req struct{ ID string }
			if errors.Is(peekJSON(r, &req), errBodyTooBig) {
				gateDeny(w, 413, "srv.http_util.requeteTropGrosse")
				return
			}
			chatID, child = req.ID, homeFrom(r)
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

// stopChildChat envoie POST /chat-stop à l'enfant du home, par son proxy (en-tête X-Gld-Child compris).
func stopChildChat(home, id string) {
	p, err := homeProxy(home)
	if err != nil {
		journal("auth: arrêt du tour %s sur %s : %v", id, home, err)
		return
	}
	b, _ := json.Marshal(map[string]string{"ID": id})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "/chat-stop", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
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
