package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
)

// routesConfig : la configuration de Claude Code (config*.go) : lecture, analyses, modifications permises, sauvegardes.
func routesConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		files, raw := allConfigs()
		type plugin struct {
			ID        string `json:"id"`
			Enabled   bool   `json:"enabled"`
			Installed bool   `json:"installed"`
		}
		var plugins []plugin
		inst := installedPlugins()
		if v, ok := lookup(raw["u"], "enabledPlugins"); ok {
			if o := asMap(v); o != nil {
				for _, k := range o.keys {
					on, _ := o.m[k].(bool)
					plugins = append(plugins, plugin{k, on, inst[k]})
				}
			}
		}
		sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })
		ps := configProjects()
		writeJSON(w, 200, map[string]any{
			"files": nz(files), "hooks": nz(hooksOf(raw)), "issues": nz(configHealth(files, raw)), "backups": listBackups(),
			"plugins": nz(plugins), "projects": nz(ps),
		})
	})
	mux.HandleFunc("GET /config-projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"projects": projectConfigs()})
	})
	mux.HandleFunc("GET /config-effective", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		if dir != "" && !safeDir(dir) {
			writeJSON(w, 400, map[string]any{})
			return
		}
		_, raw := allConfigs()
		writeJSON(w, 200, map[string]any{"rows": nz(effective(raw, dir))})
	})
	mux.HandleFunc("GET /config-mcp", func(w http.ResponseWriter, r *http.Request) {
		s := mcpServers()
		if s == nil {
			s = []McpServer{}
		}
		writeJSON(w, 200, map[string]any{"servers": s})
	})
	mux.HandleFunc("POST /config-mcp-status", func(w http.ResponseWriter, r *http.Request) {
		st, err := mcpStatus()
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": L("srv.routes_config.claudeMcpList", "A1", err.Error())})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "status": st})
	})
	mux.HandleFunc("POST /project-mcp", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		st, err := projectMcp(req.Dir)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "servers": st})
	})
	mux.HandleFunc("GET /config-perm-suggest", func(w http.ResponseWriter, r *http.Request) {
		_, raw := allConfigs()
		var have []string
		for _, doc := range raw {
			v, _ := lookup(doc, "permissions", "allow")
			have = append(have, strList(v)...)
		}
		s := permSuggestions(have)
		if s == nil {
			s = []PermSuggest{}
		}
		writeJSON(w, 200, map[string]any{"suggestions": s})
	})
	mux.HandleFunc("POST /config-edit", func(w http.ResponseWriter, r *http.Request) {
		var e CfgEdit
		if json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&e) != nil || len(e.Path) == 0 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		before, after, err := configEdit(e)
		if !e.Dry {
			journal("config-edit %s %s %v : %v", e.File, e.Op, e.Path, errOr(err)) // jamais la valeur : elle peut être un secret
		}
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		out := L("srv.config.savedBackup")
		if e.Dry {
			out = L("srv.config.preview")
		} else if before == after {
			out = L("srv.config.noChange")
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out, "before": before, "after": after})
	})
	mux.HandleFunc("POST /config-restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := restoreBackup(req.Name)
		journal("config-restore %s : %v", req.Name, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_config.sauvegardeRestauree")})
	})
}

// nz : une liste vide s'écrit « [] » en JSON, jamais « null » (le navigateur ne la parcourrait plus).
func nz[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// routesConfigPerm : import des règles de permission (configperm_io.go) ; l'export se fait côté navigateur, à partir des réglages affichés.
func routesConfigPerm(mux *http.ServeMux) {
	mux.HandleFunc("POST /config-perm-import", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			File, Text, Mode string
			Dry              bool
		}
		if json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		before, after, warns, summary, err := permImport(req.File, req.Text, req.Mode, req.Dry)
		if !req.Dry {
			journal("config-perm-import %s %s : %s : %v", req.File, req.Mode, summary, errOr(err))
		}
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		out := summary
		if !req.Dry {
			out = L("srv.config.importedBackup", "A1", summary)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out, "before": before, "after": after, "warnings": nz(warns)})
	})
}

// routesConfigRaw : édition du JSON brut d'un fichier de réglages (configraw.go).
func routesConfigRaw(mux *http.ServeMux) {
	mux.HandleFunc("GET /config-raw", func(w http.ResponseWriter, r *http.Request) {
		text, err := configRawRead(r.URL.Query().Get("file"))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "text": text})
	})
	mux.HandleFunc("POST /config-raw", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			File, Text string
			Dry        bool
		}
		if json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		before, after, warns, err := configRawEdit(req.File, req.Text, req.Dry)
		if !req.Dry {
			journal("config-raw %s (%d octets) : %v", req.File, len(req.Text), errOr(err)) // jamais le contenu : il peut contenir des secrets
		}
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		out := L("srv.config.savedBackup")
		if req.Dry {
			out = L("srv.config.preview")
		} else if before == after {
			out = L("srv.config.noChange")
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out, "before": before, "after": after, "warnings": nz(warns)})
	})
}
