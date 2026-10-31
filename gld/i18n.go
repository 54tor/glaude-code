package main

// Langue de l'interface : le réglage « language » de ~/.claude/settings.json, résolu en fr ou en,
// et les catalogues de textes du serveur (locales/fr.json, locales/en.json).

import (
	"embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

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
	for _, lang := range []string{"fr", "en"} {
		if _, err := bundle.LoadMessageFileFS(localesFS, "locales/"+lang+".json"); err != nil {
			panic("catalogue " + lang + " : " + err.Error())
		}
	}
	localizers = map[string]*i18n.Localizer{
		"fr": i18n.NewLocalizer(bundle, "fr"),
		"en": i18n.NewLocalizer(bundle, "en"),
	}
}

// normalizeLang : fr, en, other (langue connue mais non traduite) ou vide (aucun réglage).
func normalizeLang(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return ""
	}
	// Premier mot seulement : « fr-FR », « fr_CA.UTF-8 », « French (Canada) ».
	first := strings.FieldsFunc(normalized, func(character rune) bool { return !unicode.IsLetter(character) })
	if len(first) == 0 {
		return "other"
	}
	switch first[0] {
	case "fr", "french", "français", "francais":
		return "fr"
	case "en", "english", "anglais":
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
	fileInfo, statErr := os.Stat(filepath.Join(cfgDir(), "settings.json"))
	if statErr != nil {
		return ""
	}
	langCache.Lock()
	defer langCache.Unlock()
	isCacheFresh := langCache.ok && langCache.mtime == fileInfo.ModTime().UnixNano() && langCache.size == fileInfo.Size()
	if isCacheFresh {
		return langCache.raw
	}
	var settings struct {
		Language json.RawMessage `json:"language"`
	}
	raw := ""
	if content, readErr := os.ReadFile(filepath.Join(cfgDir(), "settings.json")); readErr == nil && json.Unmarshal(content, &settings) == nil {
		var languageSetting string
		if json.Unmarshal(settings.Language, &languageSetting) == nil {
			raw = strings.TrimSpace(languageSetting)
		}
	}
	langCache.mtime = fileInfo.ModTime().UnixNano()
	langCache.size = fileInfo.Size()
	langCache.raw = raw
	langCache.ok = true
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

// cliMode : le processus est une commande en ligne, pas le serveur ; uiLang suit alors cliLang.
var cliMode bool

// uiLang : la langue des textes que le serveur renvoie à l'interface.
func uiLang() string {
	if langOverride != "" {
		return langOverride
	}
	if cliMode {
		return cliLang()
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
		for _, variable := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			value := os.Getenv(variable)
			isNeutralLocale := value == "" || value == "C" || value == "POSIX" || strings.HasPrefix(value, "C.")
			if isNeutralLocale {
				continue // locale neutre : elle ne dit rien de la langue de l'utilisateur
			}
			norm = normalizeLang(value)
			break
		}
	}
	return resolveLang(norm, "fr")
}

// replyLang : la langue de réponse demandée aux modèles d'aide, celle du réglage telle qu'écrite par l'utilisateur.
func replyLang() string {
	if raw := userLanguageRaw(); raw != "" && langOverride == "" {
		switch normalizeLang(raw) {
		case "fr":
			return "français"
		case "en":
			return "English"
		}
		return raw
	}
	if uiLang() == "en" {
		return "English"
	}
	return "français"
}

// tr : le texte de l'identifiant dans la langue ; data["Count"] (entier) choisit la forme du pluriel.
// Un identifiant inconnu est renvoyé tel quel.
func tr(lang, id string, data map[string]any) string {
	bundleOnce.Do(loadBundle)
	localizer := localizers[lang]
	if localizer == nil {
		localizer = localizers["fr"]
	}
	localizeConfig := &i18n.LocalizeConfig{MessageID: id, TemplateData: data}
	if count, isInt := data["Count"].(int); isInt {
		localizeConfig.PluralCount = count
	}
	text, err := localizer.Localize(localizeConfig)
	if err != nil {
		return id
	}
	return text
}

// trKeys : tous les identifiants du catalogue de la langue (contrôle de symétrie fr/en).
func trKeys(lang string) []string {
	content, err := localesFS.ReadFile("locales/" + lang + ".json")
	if err != nil {
		return nil
	}
	var catalog map[string]json.RawMessage
	if json.Unmarshal(content, &catalog) != nil {
		return nil
	}
	keys := make([]string, 0, len(catalog))
	for key := range catalog {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type msgErr struct {
	id      string
	data    map[string]any
	wrapped error
}

// Error écrit le message dans la langue courante au moment de l'affichage, pas de la création.
func (message *msgErr) Error() string {
	return tr(uiLang(), message.id, message.data)
}

func (message *msgErr) Unwrap() error {
	return message.wrapped
}

func kvMap(pairs []any) map[string]any {
	values := make(map[string]any, len(pairs)/2)
	for pairIndex := 0; pairIndex+1 < len(pairs); pairIndex += 2 {
		values[pairs[pairIndex].(string)] = pairs[pairIndex+1]
	}
	return values
}

// L : le texte de l'identifiant dans la langue de l'interface ; pairs = paires nom, valeur du modèle.
func L(id string, pairs ...any) string {
	return tr(uiLang(), id, kvMap(pairs))
}

// terr : une erreur dont le texte vient du catalogue ; la valeur d'un nom en « W… » est l'erreur enveloppée.
func terr(id string, pairs ...any) error {
	values := kvMap(pairs)
	var wrapped error
	for name, value := range values {
		if valueErr, isError := value.(error); isError && strings.HasPrefix(name, "W") {
			wrapped = valueErr
		}
	}
	return &msgErr{id: id, data: values, wrapped: wrapped}
}

// isMsg : l'erreur (ou une erreur qu'elle enveloppe) vient de ce message du catalogue.
func isMsg(err error, id string) bool {
	var message *msgErr
	return errors.As(err, &message) && message.id == id
}
