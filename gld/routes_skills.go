package main

import (
	"net/http"
	"sort"
	"strings"
)

const pluginListLimit = 40

// routesSkills : les skills de Claude Code (skills.go), leurs plugins et marketplaces.
func routesSkills(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/skills", func(responseWriter http.ResponseWriter, request *http.Request) {
		projects := pickerProjects()
		sort.Slice(projects, func(leftIndex, rightIndex int) bool {
			return projects[leftIndex].Project < projects[rightIndex].Project
		})
		writeJSON(responseWriter, 200, map[string]any{"skills": listSkills(), "projects": projects})
	})
	mux.HandleFunc("GET /v1/skills/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		text, files, err := readSkill(request.PathValue("key"))
		if err != nil {
			writeErr(responseWriter, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": text, "files": nz(files)})
	})
	mux.HandleFunc("PUT /v1/skills/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Text   string
			Create bool
		}
		if !decodeBody(responseWriter, request, &body, skillMaxBytes+4096) {
			return
		}
		key := request.PathValue("key")
		err := saveSkill(key, body.Text, body.Create)
		journal("skill-save %s (%d octets, création=%v) : %v", key, len(body.Text), body.Create, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_sessions.enregistre")})
	})
	mux.HandleFunc("DELETE /v1/skills", func(responseWriter http.ResponseWriter, request *http.Request) {
		keys := request.URL.Query()["item"]
		if len(keys) == 0 {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(keys) > itemMax {
			writeErr(responseWriter, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		var bulkReply bulk
		for _, key := range keys {
			err := removeSkill(key)
			journal("skill-rm %s (copie à la corbeille) : %v", key, errOr(err))
			if err != nil {
				bulkReply.fail(key, http.StatusConflict, "conflict", err.Error())
				continue
			}
			bulkReply.ok(key)
		}
		bulkReply.reply(responseWriter, L("srv.routes_skills.supprimeCopieALa"))
	})
	mux.HandleFunc("POST /v1/skills/copies", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Key, To string }
		if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		copiedKey, err := copySkill(body.Key, body.To)
		journal("skill-copy %s vers %s : %v", body.Key, body.To, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 201, map[string]any{"message": L("srv.routes_skills.copie"), "key": copiedKey})
	})
	// Rédaction ou révision par un petit modèle : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /v1/skills/drafts", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		text, cost, err := skillDraft(body.Model, body.Instruction, body.Key, body.Refs)
		journal("skill-draft %s %q réf=%v : %v (%.4f $)", body.Model, body.Key, body.Refs, errOr(err), cost)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.brouillonPret"), "text": text, "cost": cost})
	})
	mux.HandleFunc("GET /v1/skills/catalog", func(responseWriter http.ResponseWriter, request *http.Request) {
		hits, err := skillsFind(request.Context(), request.URL.Query().Get("q"))
		if err != nil {
			writeErr(responseWriter, http.StatusBadGateway, "unavailable", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"hits": hits})
	})
	mux.HandleFunc("GET /v1/skills/updates", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"states": checkSkillUpdates(request.Context(), request.URL.Query().Get("force") == "1")})
	})
	mux.HandleFunc("POST /v1/skills/installations", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Action, Source, Skill, Scope, Project string }
		if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		output, err := skillsAct(request.Context(), body.Action, body.Source, body.Skill, body.Scope, body.Project)
		journal("skill-npx %s %s %s (%s %s) : %v", body.Action, body.Source, body.Skill, body.Scope, body.Project, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusUnprocessableEntity, "action_failed", strings.TrimSpace(output+"\n"+err.Error()))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": output})
	})
	mux.HandleFunc("GET /v1/plugins", func(responseWriter http.ResponseWriter, request *http.Request) {
		installed, available, err := listPlugins(request.URL.Query().Get("q"), pluginListLimit)
		if err != nil {
			writeErr(responseWriter, http.StatusBadGateway, "unavailable", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"installed": installed, "available": available})
	})
	// Une opération du CLI de plugins (plugins.go) par méthode ; portée et projet : corps ou, pour DELETE, requête.
	plugin := func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Scope, Project string
			Enabled        *bool
		}
		query := request.URL.Query()
		if request.Method == http.MethodDelete {
			body.Scope, body.Project = query.Get("scope"), query.Get("project")
		} else if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		verb := ""
		switch {
		case request.Method == http.MethodPut:
			verb = "install"
		case request.Method == http.MethodDelete:
			verb = "uninstall"
		case request.Method == http.MethodPatch && body.Enabled != nil:
			verb = "disable"
			if *body.Enabled {
				verb = "enable"
			}
		case request.Method == http.MethodPost:
			verb = "update"
		}
		if verb == "" {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		pluginID := request.PathValue("plugin")
		output, err := pluginAct(verb, pluginID, body.Scope, body.Project)
		journal("plugin-act %s %s (%s %s) : %v", verb, pluginID, body.Scope, body.Project, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusUnprocessableEntity, "action_failed", strings.TrimSpace(output+"\n"+err.Error()))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": output})
	}
	mux.HandleFunc("PUT /v1/plugins/{plugin}", plugin)
	mux.HandleFunc("DELETE /v1/plugins/{plugin}", plugin)
	mux.HandleFunc("PATCH /v1/plugins/{plugin}", plugin)
	mux.HandleFunc("POST /v1/plugins/{plugin}/updates", plugin)
	mux.HandleFunc("GET /v1/marketplaces", func(responseWriter http.ResponseWriter, request *http.Request) {
		marketplaces, err := listMarketplaces()
		if err != nil {
			writeErr(responseWriter, http.StatusBadGateway, "unavailable", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"marketplaces": marketplaces})
	})
	market := func(responseWriter http.ResponseWriter, action, source, name string) {
		output, err := marketplaceAct(action, source, name)
		journal("marketplace-act %s %s %s : %v", action, source, name, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusUnprocessableEntity, "action_failed", strings.TrimSpace(output+"\n"+err.Error()))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": output})
	}
	mux.HandleFunc("POST /v1/marketplaces", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Source string }
		if decodeBody(responseWriter, request, &body, 1<<12) {
			market(responseWriter, "add", body.Source, "")
		}
	})
	mux.HandleFunc("DELETE /v1/marketplaces/{name}", func(responseWriter http.ResponseWriter, request *http.Request) {
		market(responseWriter, "remove", "", request.PathValue("name"))
	})
	mux.HandleFunc("POST /v1/marketplaces/{name}/updates", func(responseWriter http.ResponseWriter, request *http.Request) {
		market(responseWriter, "update", "", request.PathValue("name"))
	})
	mux.HandleFunc("POST /v1/marketplaces/updates", func(responseWriter http.ResponseWriter, request *http.Request) {
		market(responseWriter, "update", "", "")
	})
}
