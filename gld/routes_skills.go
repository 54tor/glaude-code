package main

import (
	"net/http"
	"sort"
	"strings"
)

// routesSkills : les skills de Claude Code (skills.go), leurs plugins et marketplaces.
func routesSkills(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/skills", func(w http.ResponseWriter, r *http.Request) {
		ps := liveProjects()
		sort.Slice(ps, func(i, j int) bool { return ps[i].Project < ps[j].Project })
		writeJSON(w, 200, map[string]any{"skills": listSkills(), "projects": ps})
	})
	mux.HandleFunc("GET /v1/skills/{key}", func(w http.ResponseWriter, r *http.Request) {
		text, files, err := readSkill(r.PathValue("key"))
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"text": text, "files": nz(files)})
	})
	mux.HandleFunc("PUT /v1/skills/{key}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text   string
			Create bool
		}
		if !decodeBody(w, r, &req, skillMaxBytes+4096) {
			return
		}
		key := r.PathValue("key")
		err := saveSkill(key, req.Text, req.Create)
		journal("skill-save %s (%d octets, création=%v) : %v", key, len(req.Text), req.Create, errOr(err))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_sessions.enregistre")})
	})
	mux.HandleFunc("DELETE /v1/skills", func(w http.ResponseWriter, r *http.Request) {
		keys := r.URL.Query()["item"]
		if len(keys) == 0 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(keys) > itemMax {
			writeErr(w, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		var b bulk
		for _, key := range keys {
			err := removeSkill(key)
			journal("skill-rm %s (copie à la corbeille) : %v", key, errOr(err))
			if err != nil {
				b.fail(key, http.StatusConflict, "conflict", err.Error())
				continue
			}
			b.ok(key)
		}
		b.reply(w, L("srv.routes_skills.supprimeCopieALa"))
	})
	mux.HandleFunc("POST /v1/skills/copies", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, To string }
		if !decodeBody(w, r, &req, 1<<12) {
			return
		}
		dst, err := copySkill(req.Key, req.To)
		journal("skill-copy %s vers %s : %v", req.Key, req.To, errOr(err))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"message": L("srv.routes_skills.copie"), "key": dst})
	})
	// Rédaction ou révision par un petit modèle : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /v1/skills/drafts", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if !decodeBody(w, r, &req, 1<<16) {
			return
		}
		text, cost, err := skillDraft(req.Model, req.Instruction, req.Key, req.Refs)
		journal("skill-draft %s %q réf=%v : %v (%.4f $)", req.Model, req.Key, req.Refs, errOr(err), cost)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.brouillonPret"), "text": text, "cost": cost})
	})
	mux.HandleFunc("GET /v1/skills/catalog", func(w http.ResponseWriter, r *http.Request) {
		hits, err := skillsFind(r.Context(), r.URL.Query().Get("q"))
		if err != nil {
			writeErr(w, http.StatusBadGateway, "unavailable", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"hits": hits})
	})
	mux.HandleFunc("GET /v1/skills/updates", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"states": checkSkillUpdates(r.Context(), r.URL.Query().Get("force") == "1")})
	})
	mux.HandleFunc("POST /v1/skills/installations", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Action, Source, Skill, Scope, Project string }
		if !decodeBody(w, r, &req, 1<<12) {
			return
		}
		out, err := skillsAct(r.Context(), req.Action, req.Source, req.Skill, req.Scope, req.Project)
		journal("skill-npx %s %s %s (%s %s) : %v", req.Action, req.Source, req.Skill, req.Scope, req.Project, errOr(err))
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "action_failed", strings.TrimSpace(out+"\n"+err.Error()))
			return
		}
		writeJSON(w, 200, map[string]any{"message": out})
	})
	mux.HandleFunc("GET /v1/plugins", func(w http.ResponseWriter, r *http.Request) {
		inst, av, err := listPlugins(r.URL.Query().Get("q"), 40)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "unavailable", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"installed": inst, "available": av})
	})
	// Une opération du CLI de plugins (plugins.go) par méthode ; portée et projet : corps ou, pour DELETE, requête.
	plugin := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Scope, Project string
			Enabled        *bool
		}
		q := r.URL.Query()
		if r.Method == http.MethodDelete {
			req.Scope, req.Project = q.Get("scope"), q.Get("project")
		} else if !decodeBody(w, r, &req, 1<<12) {
			return
		}
		verb := ""
		switch {
		case r.Method == http.MethodPut:
			verb = "install"
		case r.Method == http.MethodDelete:
			verb = "uninstall"
		case r.Method == http.MethodPatch && req.Enabled != nil:
			verb = map[bool]string{true: "enable", false: "disable"}[*req.Enabled]
		case r.Method == http.MethodPost:
			verb = "update"
		}
		if verb == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		id := r.PathValue("plugin")
		out, err := pluginAct(verb, id, req.Scope, req.Project)
		journal("plugin-act %s %s (%s %s) : %v", verb, id, req.Scope, req.Project, errOr(err))
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "action_failed", strings.TrimSpace(out+"\n"+err.Error()))
			return
		}
		writeJSON(w, 200, map[string]any{"message": out})
	}
	mux.HandleFunc("PUT /v1/plugins/{plugin}", plugin)
	mux.HandleFunc("DELETE /v1/plugins/{plugin}", plugin)
	mux.HandleFunc("PATCH /v1/plugins/{plugin}", plugin)
	mux.HandleFunc("POST /v1/plugins/{plugin}/updates", plugin)
	mux.HandleFunc("GET /v1/marketplaces", func(w http.ResponseWriter, r *http.Request) {
		m, err := listMarketplaces()
		if err != nil {
			writeErr(w, http.StatusBadGateway, "unavailable", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"marketplaces": m})
	})
	market := func(w http.ResponseWriter, act, source, name string) {
		out, err := marketplaceAct(act, source, name)
		journal("marketplace-act %s %s %s : %v", act, source, name, errOr(err))
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "action_failed", strings.TrimSpace(out+"\n"+err.Error()))
			return
		}
		writeJSON(w, 200, map[string]any{"message": out})
	}
	mux.HandleFunc("POST /v1/marketplaces", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Source string }
		if decodeBody(w, r, &req, 1<<12) {
			market(w, "add", req.Source, "")
		}
	})
	mux.HandleFunc("DELETE /v1/marketplaces/{name}", func(w http.ResponseWriter, r *http.Request) {
		market(w, "remove", "", r.PathValue("name"))
	})
	mux.HandleFunc("POST /v1/marketplaces/{name}/updates", func(w http.ResponseWriter, r *http.Request) {
		market(w, "update", "", r.PathValue("name"))
	})
	mux.HandleFunc("POST /v1/marketplaces/updates", func(w http.ResponseWriter, r *http.Request) {
		market(w, "update", "", "")
	})
}
