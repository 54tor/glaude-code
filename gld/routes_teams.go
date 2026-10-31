package main

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const teamAuthorMaxRunes = 64

// routesTeams : équipes d'agents (catalogue, lancement, suivi, messages). Fines : la logique est dans teams_*.go.
// principalName : le nom d'auteur d'un principal (« token » pour un jeton Bearer).
func principalName(gate *gateInfo) string {
	switch {
	case gate == nil || gate.p == nil:
		return "local"
	case gate.p.Token:
		return "token"
	}
	return gate.p.User.Name
}

// teamAuthor : l'auteur vient du principal authentifié, jamais du corps. Sur un home enfant (sans garde), la passerelle
// transmet le nom dans userHeader ; il n'est cru qu'avec le jeton d'enfant valide (childToken vide = processus principal,
// où un client pourrait forger l'en-tête).
func teamAuthor(request *http.Request) string {
	if childToken != "" && childAuthorized(request) {
		author := strings.Map(func(character rune) rune {
			if unicode.IsControl(character) {
				return -1
			}
			return character
		}, request.Header.Get(userHeader))
		if runes := []rune(author); len(runes) > teamAuthorMaxRunes {
			author = string(runes[:teamAuthorMaxRunes])
		}
		if author != "" {
			return author
		}
		return "local"
	}
	return principalName(gateFrom(request))
}

func routesTeams(mux *http.ServeMux) {
	reply := func(responseWriter http.ResponseWriter, code int, payload any) {
		responseWriter.Header().Set("Cache-Control", "no-store")
		writeJSON(responseWriter, code, payload)
	}
	fail := func(responseWriter http.ResponseWriter, err error) {
		writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
	}
	decodeTeamBody := func(responseWriter http.ResponseWriter, request *http.Request, target any) bool {
		return decodeBody(responseWriter, request, target, 1<<16)
	}
	checkTeamName := func(responseWriter http.ResponseWriter, name string) bool {
		if !teamNameRe.MatchString(name) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_name", L("srv.teams_run.nomInvalide"))
			return false
		}
		return true
	}
	mux.HandleFunc("GET /v1/teams/catalog", func(responseWriter http.ResponseWriter, request *http.Request) {
		catalog, err := loadCatalog(cfgDir())
		if err != nil {
			fail(responseWriter, err)
			return
		}
		reply(responseWriter, 200, map[string]any{"agents": catalog.Agents, "teams": catalog.Teams})
	})
	mux.HandleFunc("POST /v1/teams/compositions", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ TeamIDs []string }
		if !decodeTeamBody(responseWriter, request, &body) {
			return
		}
		catalog, err := loadCatalog(cfgDir())
		if err != nil {
			fail(responseWriter, err)
			return
		}
		lead, mates, tooMany, err := composeTeams(catalog, body.TeamIDs)
		if err != nil {
			fail(responseWriter, err)
			return
		}
		reply(responseWriter, 200, map[string]any{"lead": lead, "mates": mates, "tooMany": tooMany})
	})
	mux.HandleFunc("GET /v1/teams", func(responseWriter http.ResponseWriter, request *http.Request) {
		reply(responseWriter, 200, map[string]any{"teams": teamList(cfgDir())})
	})
	mux.HandleFunc("GET /v1/teams/{team}/run", func(responseWriter http.ResponseWriter, request *http.Request) {
		name := request.PathValue("team")
		if !checkTeamName(responseWriter, name) {
			return
		}
		view, err := teamView(cfgDir(), name)
		if err != nil {
			fail(responseWriter, err)
			return
		}
		reply(responseWriter, 200, view)
	})
	mux.HandleFunc("GET /v1/teams/{team}/run/screen", func(responseWriter http.ResponseWriter, request *http.Request) {
		name := request.PathValue("team")
		if !checkTeamName(responseWriter, name) {
			return
		}
		text, err := teamScreen(cfgDir(), name, request.URL.Query().Get("agent"))
		if err != nil {
			fail(responseWriter, err)
			return
		}
		reply(responseWriter, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("POST /v1/teams/{team}/runs", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body LaunchReq
		if !decodeTeamBody(responseWriter, request, &body) {
			return
		}
		if len(body.TeamIDs) == 0 {
			body.TeamIDs = []string{request.PathValue("team")}
		}
		run, err := launchTeam(cfgDir(), body)
		journal("teams-launch : %d équipe(s), objectif de %d caractères : %v", len(body.TeamIDs), utf8.RuneCountInString(body.Goal), errOr(err))
		if err != nil {
			status, code := http.StatusBadRequest, "bad_request"
			if len(run.Conflicts) > 0 {
				status, code = http.StatusConflict, "conflict"
			}
			reply(responseWriter, status, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}, "run": run})
			return
		}
		reply(responseWriter, 201, run)
	})
	mux.HandleFunc("POST /v1/teams/{team}/run/messages", func(responseWriter http.ResponseWriter, request *http.Request) {
		name := request.PathValue("team")
		var body struct{ To, Text string }
		if !checkTeamName(responseWriter, name) || !decodeTeamBody(responseWriter, request, &body) {
			return
		}
		queued, err := teamSend(cfgDir(), name, teamAuthor(request), body.To, body.Text)
		journal("team-send %s -> %s (%d caractères) : %v", name, body.To, utf8.RuneCountInString(body.Text), errOr(err))
		if err != nil {
			fail(responseWriter, err)
			return
		}
		reply(responseWriter, 200, map[string]any{"queued": queued})
	})
	mux.HandleFunc("POST /v1/teams/{team}/run/keystrokes", func(responseWriter http.ResponseWriter, request *http.Request) {
		name := request.PathValue("team")
		var body struct{ Agent, Key, Expect string }
		if !checkTeamName(responseWriter, name) || !decodeTeamBody(responseWriter, request, &body) {
			return
		}
		err := teamKey(cfgDir(), name, body.Agent, body.Key, body.Expect)
		journal("team-key %s -> %s : %v", name, body.Agent, errOr(err))
		if err != nil {
			fail(responseWriter, err)
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1/teams/{team}/run", func(responseWriter http.ResponseWriter, request *http.Request) {
		name := request.PathValue("team")
		if !checkTeamName(responseWriter, name) {
			return
		}
		err := teamClose(cfgDir(), name)
		journal("team-close %s : %v", name, errOr(err))
		if err != nil {
			fail(responseWriter, err)
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	// « new » à la place de l'id crée l'entrée : l'id est attribué par le serveur.
	mux.HandleFunc("PUT /v1/agents/{agent}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var agent Agent
		if !decodeTeamBody(responseWriter, request, &agent) {
			return
		}
		agent.ID = request.PathValue("agent")
		code := 200
		if agent.ID == "new" {
			agent.ID, code = "", 201
		}
		agent, err := saveAgent(cfgDir(), agent)
		if err != nil {
			fail(responseWriter, err)
			return
		}
		reply(responseWriter, code, agent)
	})
	mux.HandleFunc("PUT /v1/teams/{team}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var team Team
		if !decodeTeamBody(responseWriter, request, &team) {
			return
		}
		team.ID = request.PathValue("team")
		code := 200
		if team.ID == "new" {
			team.ID, code = "", 201
		}
		team, err := saveTeam(cfgDir(), team)
		if err != nil {
			fail(responseWriter, err)
			return
		}
		reply(responseWriter, code, team)
	})
	removeHandler := func(key string, removeEntry func(string, string) error) http.HandlerFunc {
		return func(responseWriter http.ResponseWriter, request *http.Request) {
			if err := removeEntry(cfgDir(), request.PathValue(key)); err != nil {
				fail(responseWriter, err)
				return
			}
			responseWriter.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("DELETE /v1/agents/{agent}", removeHandler("agent", removeAgent))
	mux.HandleFunc("DELETE /v1/teams/{team}", removeHandler("team", removeTeam))
}
