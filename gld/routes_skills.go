package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
)

// routesSkills : les skills de Claude Code (skills.go).
func routesSkills(mux *http.ServeMux) {
	mux.HandleFunc("GET /skills-list", func(w http.ResponseWriter, r *http.Request) {
		ps := liveProjects()
		sort.Slice(ps, func(i, j int) bool { return ps[i].Project < ps[j].Project })
		writeJSON(w, 200, map[string]any{"skills": listSkills(), "projects": ps})
	})
	mux.HandleFunc("GET /skill", func(w http.ResponseWriter, r *http.Request) {
		text, files, err := readSkill(r.URL.Query().Get("key"))
		if err != nil {
			writeJSON(w, 200, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"text": text, "files": nz(files)})
	})
	mux.HandleFunc("POST /skill-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Text string
			Create    bool
		}
		if json.NewDecoder(io.LimitReader(r.Body, skillMaxBytes+4096)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := saveSkill(req.Key, req.Text, req.Create)
		journal("skill-save %s (%d octets, création=%v) : %v", req.Key, len(req.Text), req.Create, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.enregistre")})
	})
	mux.HandleFunc("POST /skill-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := removeSkill(req.Key)
		journal("skill-rm %s (copie à la corbeille) : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_skills.supprimeCopieALa")})
	})
	mux.HandleFunc("POST /skill-copy", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, To string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		dst, err := copySkill(req.Key, req.To)
		journal("skill-copy %s vers %s : %v", req.Key, req.To, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_skills.copie"), "key": dst})
	})
	// Rédaction ou révision par un petit modèle : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /skill-draft", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		text, cost, err := skillDraft(req.Model, req.Instruction, req.Key, req.Refs)
		journal("skill-draft %s %q réf=%v : %v (%.4f $)", req.Model, req.Key, req.Refs, errOr(err), cost)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.brouillonPret"), "text": text, "cost": cost})
	})
	mux.HandleFunc("GET /plugins", func(w http.ResponseWriter, r *http.Request) {
		inst, av, err := listPlugins(r.URL.Query().Get("q"), 40)
		if err != nil {
			writeJSON(w, 200, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"installed": inst, "available": av})
	})
	mux.HandleFunc("POST /plugin-act", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Action, ID, Scope, Project string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		out, err := pluginAct(req.Action, req.ID, req.Scope, req.Project)
		journal("plugin-act %s %s (%s %s) : %v", req.Action, req.ID, req.Scope, req.Project, errOr(err))
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
	})
	mux.HandleFunc("GET /marketplaces", func(w http.ResponseWriter, r *http.Request) {
		m, err := listMarketplaces()
		if err != nil {
			writeJSON(w, 200, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"marketplaces": m})
	})
	mux.HandleFunc("POST /marketplace-act", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Action, Source, Name string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		out, err := marketplaceAct(req.Action, req.Source, req.Name)
		journal("marketplace-act %s %s %s : %v", req.Action, req.Source, req.Name, errOr(err))
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
	})
}
