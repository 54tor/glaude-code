package main

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// routesTeams : équipes d'agents (catalogue, lancement, suivi, messages). Fines : la logique est dans teams_*.go.
// principalName : le nom d'auteur d'un principal (« token » pour un jeton Bearer).
func principalName(g *gateInfo) string {
	switch {
	case g == nil || g.p == nil:
		return "local"
	case g.p.Token:
		return "token"
	}
	return g.p.User.Name
}

// teamAuthor : l'auteur vient du principal authentifié, jamais du corps. Sur un home enfant (sans garde), la passerelle
// transmet le nom dans userHeader ; il n'est cru qu'avec le jeton d'enfant valide (childToken vide = processus principal,
// où un client pourrait forger l'en-tête).
func teamAuthor(r *http.Request) string {
	if childToken != "" && childAuthorized(r) {
		n := strings.Map(func(c rune) rune {
			if unicode.IsControl(c) {
				return -1
			}
			return c
		}, r.Header.Get(userHeader))
		if rs := []rune(n); len(rs) > 64 {
			n = string(rs[:64])
		}
		if n != "" {
			return n
		}
		return "local"
	}
	return principalName(gateFrom(r))
}

func routesTeams(mux *http.ServeMux) {
	reply := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, code, v)
	}
	fail := func(w http.ResponseWriter, err error) {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
	}
	body := func(w http.ResponseWriter, r *http.Request, v any) bool { return decodeBody(w, r, v, 1<<16) }
	okName := func(w http.ResponseWriter, name string) bool {
		if !teamNameRe.MatchString(name) {
			writeErr(w, http.StatusBadRequest, "bad_name", L("srv.teams_run.nomInvalide"))
			return false
		}
		return true
	}
	mux.HandleFunc("GET /v1/teams/catalog", func(w http.ResponseWriter, r *http.Request) {
		c, err := loadCatalog(cfgDir())
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"agents": c.Agents, "teams": c.Teams})
	})
	mux.HandleFunc("POST /v1/teams/compositions", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ TeamIDs []string }
		if !body(w, r, &req) {
			return
		}
		c, err := loadCatalog(cfgDir())
		if err != nil {
			fail(w, err)
			return
		}
		lead, mates, tooMany, err := composeTeams(c, req.TeamIDs)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"lead": lead, "mates": mates, "tooMany": tooMany})
	})
	mux.HandleFunc("GET /v1/teams", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"teams": teamList(cfgDir())})
	})
	mux.HandleFunc("GET /v1/teams/{team}/run", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("team")
		if !okName(w, name) {
			return
		}
		v, err := teamView(cfgDir(), name)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, v)
	})
	mux.HandleFunc("GET /v1/teams/{team}/run/screen", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("team")
		if !okName(w, name) {
			return
		}
		text, err := teamScreen(cfgDir(), name, r.URL.Query().Get("agent"))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("POST /v1/teams/{team}/runs", func(w http.ResponseWriter, r *http.Request) {
		var req LaunchReq
		if !body(w, r, &req) {
			return
		}
		if len(req.TeamIDs) == 0 {
			req.TeamIDs = []string{r.PathValue("team")}
		}
		run, err := launchTeam(cfgDir(), req)
		journal("teams-launch : %d équipe(s), objectif de %d caractères : %v", len(req.TeamIDs), utf8.RuneCountInString(req.Goal), errOr(err))
		if err != nil {
			st, code := http.StatusBadRequest, "bad_request"
			if len(run.Conflicts) > 0 {
				st, code = http.StatusConflict, "conflict"
			}
			reply(w, st, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}, "run": run})
			return
		}
		reply(w, 201, run)
	})
	mux.HandleFunc("POST /v1/teams/{team}/run/messages", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("team")
		var req struct{ To, Text string }
		if !okName(w, name) || !body(w, r, &req) {
			return
		}
		queued, err := teamSend(cfgDir(), name, teamAuthor(r), req.To, req.Text)
		journal("team-send %s -> %s (%d caractères) : %v", name, req.To, utf8.RuneCountInString(req.Text), errOr(err))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"queued": queued})
	})
	mux.HandleFunc("POST /v1/teams/{team}/run/keystrokes", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("team")
		var req struct{ Agent, Key, Expect string }
		if !okName(w, name) || !body(w, r, &req) {
			return
		}
		err := teamKey(cfgDir(), name, req.Agent, req.Key, req.Expect)
		journal("team-key %s -> %s : %v", name, req.Agent, errOr(err))
		if err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1/teams/{team}/run", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("team")
		if !okName(w, name) {
			return
		}
		err := teamClose(cfgDir(), name)
		journal("team-close %s : %v", name, errOr(err))
		if err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// « new » à la place de l'id crée l'entrée : l'id est attribué par le serveur.
	mux.HandleFunc("PUT /v1/agents/{agent}", func(w http.ResponseWriter, r *http.Request) {
		var a Agent
		if !body(w, r, &a) {
			return
		}
		a.ID = r.PathValue("agent")
		code := 200
		if a.ID == "new" {
			a.ID, code = "", 201
		}
		a, err := saveAgent(cfgDir(), a)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, code, a)
	})
	mux.HandleFunc("PUT /v1/teams/{team}", func(w http.ResponseWriter, r *http.Request) {
		var t Team
		if !body(w, r, &t) {
			return
		}
		t.ID = r.PathValue("team")
		code := 200
		if t.ID == "new" {
			t.ID, code = "", 201
		}
		t, err := saveTeam(cfgDir(), t)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, code, t)
	})
	rm := func(key string, del func(string, string) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if err := del(cfgDir(), r.PathValue(key)); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("DELETE /v1/agents/{agent}", rm("agent", removeAgent))
	mux.HandleFunc("DELETE /v1/teams/{team}", rm("team", removeTeam))
}
