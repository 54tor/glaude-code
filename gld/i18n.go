package main

// Langue de l'interface : le réglage « language » de ~/.claude/settings.json, résolu en fr ou en,
// et les catalogues de textes du serveur (locales/fr.json, locales/en.json).

import (
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localesFS embed.FS

// langOverride impose la langue (tests) ; vide en production.
var langOverride string

var (
	bundleOnce sync.Once
	bundle     *i18n.Bundle
	localizers map[string]*i18n.Localizer
)

func loadBundle() {
	bundle = i18n.NewBundle(language.French)
	for _, l := range []string{"fr", "en"} {
		if _, err := bundle.LoadMessageFileFS(localesFS, "locales/"+l+".json"); err != nil {
			panic("catalogue " + l + " : " + err.Error())
		}
	}
	localizers = map[string]*i18n.Localizer{"fr": i18n.NewLocalizer(bundle, "fr"), "en": i18n.NewLocalizer(bundle, "en")}
}

// normalizeLang : fr, en, other (langue connue mais non traduite) ou vide (aucun réglage).
func normalizeLang(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case s == "":
		return ""
	case s == "fr" || strings.HasPrefix(s, "fr-") || strings.HasPrefix(s, "fr_") || s == "french" || s == "français" || s == "francais":
		return "fr"
	case s == "en" || strings.HasPrefix(s, "en-") || strings.HasPrefix(s, "en_") || s == "english" || s == "anglais":
		return "en"
	}
	return "other"
}

var langCache struct {
	sync.Mutex
	mtime int64
	size  int64
	raw   string
	ok    bool
}

// userLanguageRaw : la valeur brute de « language » dans settings.json (vide si absente, illisible ou non textuelle).
// Relue seulement quand le fichier change, pour qu'un changement du réglage soit vu sans redémarrer.
func userLanguageRaw() string {
	st, err := os.Stat(filepath.Join(cfgDir(), "settings.json"))
	if err != nil {
		return ""
	}
	langCache.Lock()
	defer langCache.Unlock()
	if langCache.ok && langCache.mtime == st.ModTime().UnixNano() && langCache.size == st.Size() {
		return langCache.raw
	}
	var d struct {
		Language json.RawMessage `json:"language"`
	}
	raw := ""
	if b, err := os.ReadFile(filepath.Join(cfgDir(), "settings.json")); err == nil && json.Unmarshal(b, &d) == nil {
		var s string
		if json.Unmarshal(d.Language, &s) == nil {
			raw = strings.TrimSpace(s)
		}
	}
	langCache.mtime, langCache.size, langCache.raw, langCache.ok = st.ModTime().UnixNano(), st.Size(), raw, true
	return raw
}

// resolveLang : fr ou en d'après une valeur normalisée ; empty est la langue quand rien n'est réglé.
func resolveLang(norm, empty string) string {
	switch norm {
	case "fr", "en":
		return norm
	case "other":
		return "en"
	}
	return empty
}

// uiLang : la langue des textes que le serveur renvoie à l'interface.
func uiLang() string {
	if langOverride != "" {
		return langOverride
	}
	return resolveLang(normalizeLang(userLanguageRaw()), "fr")
}

// cliLang : comme uiLang, avec LC_ALL puis LANG quand rien n'est réglé.
func cliLang() string {
	if langOverride != "" {
		return langOverride
	}
	norm := normalizeLang(userLanguageRaw())
	if norm == "" {
		for _, k := range []string{"LC_ALL", "LANG"} {
			if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
				norm = normalizeLang(v)
				break
			}
		}
	}
	return resolveLang(norm, "fr")
}

// replyLang : la langue de réponse demandée aux modèles d'aide, celle du réglage telle qu'écrite par l'utilisateur.
func replyLang() string {
	if langOverride != "" {
		return langOverride
	}
	if raw := userLanguageRaw(); raw != "" {
		return raw
	}
	return uiLang()
}

// tr : le texte de l'identifiant dans la langue ; data["Count"] (entier) choisit la forme du pluriel.
// Un identifiant inconnu est renvoyé tel quel.
func tr(lang, id string, data map[string]any) string {
	bundleOnce.Do(loadBundle)
	l := localizers[lang]
	if l == nil {
		l = localizers["fr"]
	}
	cfg := &i18n.LocalizeConfig{MessageID: id, TemplateData: data}
	if n, ok := data["Count"].(int); ok {
		cfg.PluralCount = n
	}
	s, err := l.Localize(cfg)
	if err != nil {
		return id
	}
	return s
}

// trKeys : tous les identifiants du catalogue de la langue (contrôle de symétrie fr/en).
func trKeys(lang string) []string {
	b, err := localesFS.ReadFile("locales/" + lang + ".json")
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
