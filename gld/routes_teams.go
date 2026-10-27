package main

import (
	"encoding/json"
	"io"
	"net/http"
	"unicode/utf8"
)

// routesTeams : équipes d'agents (catalogue, lancement, suivi, messages). Fines : la logique est dans teams_*.go.
func routesTeams(mux *http.ServeMux) {
	reply := func(w http.ResponseWriter, code int, v map[string]any) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, code, v)
	}
	fail := func(w http.ResponseWriter, err error) {
		reply(w, 200, map[string]any{"ok": false, "out": err.Error()})
	}
	bad := func(w http.ResponseWriter) {
		reply(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
	}
	// body décode le JSON ; name (si non nil) reçoit un nom d'équipe déjà validé.
	body := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v) != nil {
			bad(w)
			return false
		}
		return true
	}
	okName := func(w http.ResponseWriter, name string) bool {
		if !teamNameRe.MatchString(name) {
			reply(w, 400, map[string]any{"ok": false, "out": L("srv.teams_run.nomInvalide")})
			return false
		}
		return true
	}
	author := func(r *http.Request) string {
		g := gateFrom(r)
		switch {
		case g == nil || g.p == nil:
			return "local"
		case g.p.Token:
			return "token"
		}
		return g.p.User.Name
	}

	mux.HandleFunc("GET /teams-catalog", func(w http.ResponseWriter, r *http.Request) {
		c, err := loadCatalog(cfgDir())
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true, "agents": c.Agents, "teams": c.Teams})
	})
	mux.HandleFunc("POST /teams-compose", func(w http.ResponseWriter, r *http.Request) {
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
		reply(w, 200, map[string]any{"ok": true, "lead": lead, "mates": mates, "tooMany": tooMany})
	})
	mux.HandleFunc("GET /teams", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"ok": true, "teams": teamList(cfgDir())})
	})
	mux.HandleFunc("GET /team", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if !okName(w, name) {
			return
		}
		v, err := teamView(cfgDir(), name)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true, "view": v})
	})
	mux.HandleFunc("GET /team-screen", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if !okName(w, name) {
			return
		}
		text, err := teamScreen(cfgDir(), name, r.URL.Query().Get("agent"))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true, "text": text})
	})
	mux.HandleFunc("POST /teams-launch", func(w http.ResponseWriter, r *http.Request) {
		var req LaunchReq
		if !body(w, r, &req) {
			return
		}
		run, err := launchTeam(cfgDir(), req)
		journal("teams-launch : %d équipe(s), objectif de %d caractères : %v", len(req.TeamIDs), utf8.RuneCountInString(req.Goal), errOr(err))
		if err != nil {
			reply(w, 200, map[string]any{"ok": false, "out": err.Error(), "run": run})
			return
		}
		reply(w, 200, map[string]any{"ok": true, "run": run})
	})
	mux.HandleFunc("POST /team-send", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name, To, Text string }
		if !body(w, r, &req) || !okName(w, req.Name) {
			return
		}
		queued, err := teamSend(cfgDir(), req.Name, author(r), req.To, req.Text)
		journal("team-send %s -> %s (%d caractères) : %v", req.Name, req.To, utf8.RuneCountInString(req.Text), errOr(err))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true, "queued": queued})
	})
	mux.HandleFunc("POST /team-key", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name, Agent, Key string }
		if !body(w, r, &req) || !okName(w, req.Name) {
			return
		}
		err := teamKey(cfgDir(), req.Name, req.Agent, req.Key)
		journal("team-key %s -> %s : %v", req.Name, req.Agent, errOr(err))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /team-close", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		if !body(w, r, &req) || !okName(w, req.Name) {
			return
		}
		err := teamClose(cfgDir(), req.Name)
		journal("team-close %s : %v", req.Name, errOr(err))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /teams-agent-save", func(w http.ResponseWriter, r *http.Request) {
		var a Agent
		if !body(w, r, &a) {
			return
		}
		a, err := saveAgent(cfgDir(), a)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true, "agent": a})
	})
	mux.HandleFunc("POST /teams-team-save", func(w http.ResponseWriter, r *http.Request) {
		var t Team
		if !body(w, r, &t) {
			return
		}
		t, err := saveTeam(cfgDir(), t)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, map[string]any{"ok": true, "team": t})
	})
	rm := func(del func(string, string) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var req struct{ ID string }
			if !body(w, r, &req) {
				return
			}
			if err := del(cfgDir(), req.ID); err != nil {
				fail(w, err)
				return
			}
			reply(w, 200, map[string]any{"ok": true})
		}
	}
	mux.HandleFunc("POST /teams-agent-rm", rm(removeAgent))
	mux.HandleFunc("POST /teams-team-rm", rm(removeTeam))
}
