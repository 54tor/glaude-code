package main

import (
	"net/http"
	"sort"
)

// canSeeConfigSecrets : sans garde (mode ouvert ou jeton) tout est visible ; avec, il faut config_write sur le home de la requête.
func canSeeConfigSecrets(request *http.Request) bool { return callerHas(request, RightConfigWrite) }

// healthFor : configHealth ; sans config_write, les alertes de hook et de permission (qui citent commande ou règle) sont retirées.
func healthFor(files []ConfigFile, raw map[string]any, strict bool) []CfgIssue {
	issues := configHealth(files, raw)
	if !strict {
		return issues
	}
	visibleIssues := issues[:0]
	for _, issue := range issues {
		if issue.Kind != "hook" && issue.Kind != "permission" {
			visibleIssues = append(visibleIssues, issue)
		}
	}
	return visibleIssues
}

// routesConfig : la configuration de Claude Code (config*.go) : lecture, analyses, modifications permises, sauvegardes.
func routesConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/config", func(responseWriter http.ResponseWriter, request *http.Request) {
		strict := !canSeeConfigSecrets(request)
		files, raw := allConfigsFor(strict)
		type plugin struct {
			ID        string `json:"id"`
			Enabled   bool   `json:"enabled"`
			Installed bool   `json:"installed"`
		}
		var plugins []plugin
		installed := installedPlugins()
		if enabledValue, isSet := lookup(raw["u"], "enabledPlugins"); isSet {
			if enabledMap := asMap(enabledValue); enabledMap != nil {
				for _, pluginID := range enabledMap.keys {
					isEnabled, _ := enabledMap.m[pluginID].(bool)
					plugins = append(plugins, plugin{pluginID, isEnabled, installed[pluginID]})
				}
			}
		}
		sort.Slice(plugins, func(leftIndex, rightIndex int) bool { return plugins[leftIndex].ID < plugins[rightIndex].ID })
		projects := liveProjects()
		writeJSON(responseWriter, 200, map[string]any{
			"files": nz(files), "hooks": nz(hooksFor(raw, strict)), "issues": nz(healthFor(files, raw, strict)), "backups": listBackups(),
			"plugins": nz(plugins), "projects": nz(projects),
		})
	})
	mux.HandleFunc("GET /v1/config/projects", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"projects": projectConfigsFor(!canSeeConfigSecrets(request))})
	})
	mux.HandleFunc("GET /v1/config/effective", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		if dir != "" && !safeDir(dir) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		strict := !canSeeConfigSecrets(request)
		_, raw := allConfigsFor(strict)
		writeJSON(responseWriter, 200, map[string]any{"rows": nz(effectiveFor(raw, dir, strict))})
	})
	mux.HandleFunc("GET /v1/config/mcp", func(responseWriter http.ResponseWriter, request *http.Request) {
		servers := mcpServersFor(!canSeeConfigSecrets(request))
		if servers == nil {
			servers = []McpServer{}
		}
		writeJSON(responseWriter, 200, map[string]any{"servers": servers})
	})
	mux.HandleFunc("POST /v1/config/mcp/checks", func(responseWriter http.ResponseWriter, request *http.Request) {
		status, err := mcpStatus()
		if err != nil {
			writeErr(responseWriter, http.StatusServiceUnavailable, "unavailable", L("srv.routes_config.claudeMcpList", "A1", err.Error()))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"status": status})
	})
	mux.HandleFunc("PUT /v1/config/projects/mcp", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Dir string }
		if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		servers, err := projectMcp(body.Dir)
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"servers": servers})
	})
	mux.HandleFunc("GET /v1/config/permissions/suggestions", func(responseWriter http.ResponseWriter, request *http.Request) {
		_, raw := allConfigs()
		var have []string
		for _, doc := range raw {
			allowValue, _ := lookup(doc, "permissions", "allow")
			have = append(have, strList(allowValue)...)
		}
		suggestions := permSuggestions(have)
		if suggestions == nil {
			suggestions = []PermSuggest{}
		}
		writeJSON(responseWriter, 200, map[string]any{"suggestions": suggestions})
	})
	mux.HandleFunc("POST /v1/config/edits", func(responseWriter http.ResponseWriter, request *http.Request) {
		var edit CfgEdit
		if !decodeBody(responseWriter, request, &edit, 1<<14) {
			return
		}
		if len(edit.Path) == 0 {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		before, after, err := configEdit(edit)
		if !edit.Dry {
			journal("config-edit %s %s %v : %v", edit.File, edit.Op, edit.Path, errOr(err)) // jamais la valeur : elle peut être un secret
		}
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		message := L("srv.config.savedBackup")
		if edit.Dry {
			message = L("srv.config.preview")
		} else if before == after {
			message = L("srv.config.noChange")
		}
		writeJSON(responseWriter, 200, map[string]any{"out": message, "before": before, "after": after})
	})
	mux.HandleFunc("POST /v1/config/restorations", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Name string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		err := restoreBackup(body.Name)
		journal("config-restore %s : %v", body.Name, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"out": L("srv.routes_config.sauvegardeRestauree")})
	})
}

// nz : une liste vide s'écrit « [] » en JSON, jamais « null » (le navigateur ne la parcourrait plus).
func nz[Item any](items []Item) []Item {
	if items == nil {
		return []Item{}
	}
	return items
}

// routesConfigPerm : import des règles de permission (configperm_io.go) ; l'export se fait côté navigateur, à partir des réglages affichés.
func routesConfigPerm(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/config/permissions/imports", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			File, Text, Mode string
			Dry              bool
		}
		if !decodeBody(responseWriter, request, &body, 2<<20) {
			return
		}
		before, after, warns, summary, err := permImport(body.File, body.Text, body.Mode, body.Dry)
		if !body.Dry {
			journal("config-perm-import %s %s : %s : %v", body.File, body.Mode, summary, errOr(err))
		}
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		message := summary
		if !body.Dry {
			message = L("srv.config.importedBackup", "A1", summary)
		}
		writeJSON(responseWriter, 200, map[string]any{"out": message, "before": before, "after": after, "warnings": nz(warns)})
	})
}

// routesConfigRaw : édition du JSON brut d'un fichier de réglages (configraw.go).
func routesConfigRaw(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/config/raw", func(responseWriter http.ResponseWriter, request *http.Request) {
		text, err := configRawRead(request.URL.Query().Get("file"))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("PUT /v1/config/raw", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			File, Text string
			Dry        bool
		}
		if !decodeBody(responseWriter, request, &body, 2<<20) {
			return
		}
		before, after, warns, err := configRawEdit(body.File, body.Text, body.Dry)
		if !body.Dry {
			journal("config-raw %s (%d octets) : %v", body.File, len(body.Text), errOr(err)) // jamais le contenu : il peut contenir des secrets
		}
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		message := L("srv.config.savedBackup")
		if body.Dry {
			message = L("srv.config.preview")
		} else if before == after {
			message = L("srv.config.noChange")
		}
		writeJSON(responseWriter, 200, map[string]any{"out": message, "before": before, "after": after, "warnings": nz(warns)})
	})
}
