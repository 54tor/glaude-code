package main

// Import et export des règles de permission : un fragment de settings.json (« {"permissions": {"allow": […]}} ») qu'on
// peut garder, partager ou coller dans un autre fichier de réglages.

import (
	"strings"
)

var permKinds = []string{"allow", "ask", "deny"}

// importKinds : les listes qu'un fragment peut porter. Les dossiers supplémentaires sont des chemins, pas des règles.
var importKinds = append(append([]string{}, permKinds...), "additionalDirectories")

const permMax = 2000 // règles acceptées par import

// parsePermImport lit un fragment exporté : soit {"permissions": {allow, ask, deny}}, soit directement {allow, ask, deny}.
// Le mode par défaut (defaultMode) n'est jamais importé : c'est un réglage de confiance, pas une règle.
func parsePermImport(text string) (map[string][]string, error) {
	if len(text) > 1<<20 {
		return nil, terr("srv.configperm_io.fichierTropGros")
	}
	v, err := parseOrdered([]byte(text))
	if err != nil {
		return nil, syntaxError(text, err)
	}
	root := asMap(v)
	if root == nil {
		return nil, terr("srv.configperm_io.unObjetJsonEst")
	}
	if p, ok := root.get("permissions"); ok {
		if root = asMap(p); root == nil {
			return nil, terr("srv.configperm_io.permissionsDoitEtreUn")
		}
	}
	out := map[string][]string{}
	total := 0
	for _, k := range root.keys {
		known := false
		for _, pk := range importKinds {
			known = known || pk == k
		}
		if !known {
			continue // defaultMode… : ignoré
		}
		list, ok := root.m[k].([]any)
		if !ok {
			return nil, terr("srv.configperm_io.doitEtreUneListe", "A1", k)
		}
		seen := map[string]bool{}
		rules := []string{}
		for i, e := range list {
			s, ok := e.(string)
			s = strings.TrimSpace(s)
			max := 300
			if k == "additionalDirectories" {
				max = 500
			}
			if !ok || s == "" || len(s) > max || strings.ContainsAny(s, "\n\r\x00") {
				return nil, terr("srv.configperm_io.lEntreeNN", "A1", k, "A2", i+1)
			}
			if !seen[s] {
				seen[s] = true
				rules = append(rules, s)
			}
		}
		total += len(rules)
		out[k] = rules
	}
	if len(out) == 0 {
		return nil, terr("srv.configperm_io.aucuneListeAllowAsk")
	}
	if total > permMax {
		return nil, terr("srv.configperm_io.tropDeReglesAu", "A1", total, "A2", permMax)
	}
	return out, nil
}

// permImport applique un fragment aux permissions d'un fichier de réglages. mode « merge » : ajoute les règles qui manquent ;
// « replace » : remplace les listes présentes dans le fragment (les autres listes ne bougent pas).
func permImport(id, text, mode string, dry bool) (before, after string, warns []CfgIssue, summary string, err error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if mode != "merge" && mode != "replace" {
		err = terr("srv.configperm_io.modeDImportInconnu")
		return
	}
	imp, err := parsePermImport(text)
	if err != nil {
		return
	}
	path, orig, exists, origDoc, err := loadDoc(id)
	if err != nil {
		return
	}
	// on travaille sur une copie : le document d'origine sert à l'aperçu
	cur, perr := parseOrdered([]byte(formatOrdered(origDoc)))
	if perr != nil {
		err = perr
		return
	}
	doc := asMap(cur)
	added, removed := 0, 0
	for _, kind := range importKinds {
		rules, has := imp[kind]
		if !has {
			continue
		}
		var existing []string
		if v, ok := lookup(doc, "permissions", kind); ok {
			existing = strList(v)
		}
		var next []string
		switch mode {
		case "merge":
			have := map[string]bool{}
			for _, r := range existing {
				have[r] = true
			}
			next = existing
			for _, r := range rules {
				if !have[r] {
					next = append(next, r)
					added++
				}
			}
		default:
			next = rules
			have := map[string]bool{}
			for _, r := range rules {
				have[r] = true
			}
			old := map[string]bool{}
			for _, r := range existing {
				old[r] = true
				if !have[r] {
					removed++
				}
			}
			for _, r := range rules {
				if !old[r] {
					added++
				}
			}
		}
		setRules(doc, kind, next)
	}
	summary = L("srv.configperm_io.summary", "A1", added, "A2", removed)
	before, after, warns, err = commitDoc(id, path, orig, exists, origDoc, doc, dry)
	return
}

// setRules écrit la liste d'une catégorie ; une liste vide retire la clé (et « permissions » si elle devient vide).
func setRules(doc *omap, kind string, rules []string) {
	perms := asMap(func() any { v, _ := doc.get("permissions"); return v }())
	if perms == nil {
		if len(rules) == 0 {
			return
		}
		perms = newOmap()
		doc.set("permissions", perms)
	}
	if len(rules) == 0 {
		perms.del(kind)
		if len(perms.keys) == 0 {
			doc.del("permissions")
		}
		return
	}
	list := make([]any, len(rules))
	for i, r := range rules {
		list[i] = r
	}
	perms.set(kind, list)
}
