package main

// Import et export des règles de permission : un fragment de settings.json (« {"permissions": {"allow": […]}} ») qu'on
// peut garder, partager ou coller dans un autre fichier de réglages.

import (
	"slices"
	"strings"
)

var permKinds = []string{"allow", "ask", "deny"}

// importKinds : les listes qu'un fragment peut porter. Les dossiers supplémentaires sont des chemins, pas des règles.
var importKinds = append(append([]string{}, permKinds...), "additionalDirectories")

const (
	permMax            = 2000 // règles acceptées par import
	permImportMaxBytes = 1 << 20
	permRuleMaxLen     = 300
	permDirMaxLen      = 500
)

// parsePermImport lit un fragment exporté : soit {"permissions": {allow, ask, deny}}, soit directement {allow, ask, deny}.
// Le mode par défaut (defaultMode) n'est jamais importé : c'est un réglage de confiance, pas une règle.
func parsePermImport(text string) (map[string][]string, error) {
	if len(text) > permImportMaxBytes {
		return nil, terr("srv.configperm_io.fichierTropGros")
	}
	parsed, err := parseOrdered([]byte(text))
	if err != nil {
		return nil, syntaxError(text, err)
	}
	fragment := asMap(parsed)
	if fragment == nil {
		return nil, terr("srv.configperm_io.unObjetJsonEst")
	}
	if permissionsValue, found := fragment.get("permissions"); found {
		if fragment = asMap(permissionsValue); fragment == nil {
			return nil, terr("srv.configperm_io.permissionsDoitEtreUn")
		}
	}
	listsByKind := map[string][]string{}
	totalRules := 0
	for _, kind := range fragment.keys {
		if !slices.Contains(importKinds, kind) {
			continue // defaultMode… : ignoré
		}
		list, isList := fragment.m[kind].([]any)
		if !isList {
			return nil, terr("srv.configperm_io.doitEtreUneListe", "A1", kind)
		}
		seen := map[string]bool{}
		rules := []string{}
		for entryIndex, entry := range list {
			rule, isString := entry.(string)
			rule = strings.TrimSpace(rule)
			maxLength := permRuleMaxLen
			if kind == "additionalDirectories" {
				maxLength = permDirMaxLen
			}
			isInvalid := !isString || rule == "" || len(rule) > maxLength || strings.ContainsAny(rule, "\n\r\x00")
			if isInvalid {
				return nil, terr("srv.configperm_io.lEntreeNN", "A1", kind, "A2", entryIndex+1)
			}
			if !seen[rule] {
				seen[rule] = true
				rules = append(rules, rule)
			}
		}
		totalRules += len(rules)
		listsByKind[kind] = rules
	}
	if len(listsByKind) == 0 {
		return nil, terr("srv.configperm_io.aucuneListeAllowAsk")
	}
	if totalRules > permMax {
		return nil, terr("srv.configperm_io.tropDeReglesAu", "A1", totalRules, "A2", permMax)
	}
	return listsByKind, nil
}

// permImport applique un fragment aux permissions d'un fichier de réglages. mode « merge » : ajoute les règles qui manquent ;
// « replace » : remplace les listes présentes dans le fragment (les autres listes ne bougent pas).
func permImport(id, text, mode string, dryRun bool) (before, after string, warns []CfgIssue, summary string, err error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if mode != "merge" && mode != "replace" {
		err = terr("srv.configperm_io.modeDImportInconnu")
		return
	}
	imported, err := parsePermImport(text)
	if err != nil {
		return
	}
	path, original, exists, originalDoc, err := loadDoc(id)
	if err != nil {
		return
	}
	// on travaille sur une copie : le document d'origine sert à l'aperçu
	copied, parseErr := parseOrdered([]byte(formatOrdered(originalDoc)))
	if parseErr != nil {
		err = parseErr
		return
	}
	doc := asMap(copied)
	added, removed := 0, 0
	for _, kind := range importKinds {
		rules, hasKind := imported[kind]
		if !hasKind {
			continue
		}
		var existing []string
		if existingValue, found := lookup(doc, "permissions", kind); found {
			existing = strList(existingValue)
		}
		var nextRules []string
		switch mode {
		case "merge":
			existingSet := map[string]bool{}
			for _, rule := range existing {
				existingSet[rule] = true
			}
			nextRules = existing
			for _, rule := range rules {
				if !existingSet[rule] {
					nextRules = append(nextRules, rule)
					added++
				}
			}
		default:
			nextRules = rules
			importedSet := map[string]bool{}
			for _, rule := range rules {
				importedSet[rule] = true
			}
			previousSet := map[string]bool{}
			for _, rule := range existing {
				previousSet[rule] = true
				if !importedSet[rule] {
					removed++
				}
			}
			for _, rule := range rules {
				if !previousSet[rule] {
					added++
				}
			}
		}
		setRules(doc, kind, nextRules)
	}
	summary = L("srv.configperm_io.summary", "A1", added, "A2", removed)
	before, after, warns, err = commitDoc(id, path, original, exists, originalDoc, doc, dryRun)
	return
}

// setRules écrit la liste d'une catégorie ; une liste vide retire la clé (et « permissions » si elle devient vide).
func setRules(doc *omap, kind string, rules []string) {
	permissionsValue, _ := doc.get("permissions")
	permissions := asMap(permissionsValue)
	if permissions == nil {
		if len(rules) == 0 {
			return
		}
		permissions = newOmap()
		doc.set("permissions", permissions)
	}
	if len(rules) == 0 {
		permissions.del(kind)
		if len(permissions.keys) == 0 {
			doc.del("permissions")
		}
		return
	}
	list := make([]any, len(rules))
	for ruleIndex, rule := range rules {
		list[ruleIndex] = rule
	}
	permissions.set(kind, list)
}
