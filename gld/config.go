package main

// Configuration de Claude Code : réglages utilisateur (~/.claude/settings.json), de projet (.claude/settings.json) et
// locaux (.claude/settings.local.json). Lecture, masquage des secrets, et JSON ordonné : une écriture ne doit jamais
// réordonner ni reformater le fichier de l'utilisateur.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --- JSON ordonné ------------------------------------------------------------------------------------

// omap : un objet JSON qui garde l'ordre de ses clés.
type omap struct {
	keys []string
	m    map[string]any
}

func newOmap() *omap { return &omap{m: map[string]any{}} }

func (orderedMap *omap) get(key string) (any, bool) {
	value, found := orderedMap.m[key]
	return value, found
}

func (orderedMap *omap) set(key string, value any) {
	if _, exists := orderedMap.m[key]; !exists {
		orderedMap.keys = append(orderedMap.keys, key)
	}
	orderedMap.m[key] = value
}

func (orderedMap *omap) del(key string) {
	if _, exists := orderedMap.m[key]; !exists {
		return
	}
	delete(orderedMap.m, key)
	for keyIndex, existingKey := range orderedMap.keys {
		if existingKey == key {
			orderedMap.keys = append(orderedMap.keys[:keyIndex], orderedMap.keys[keyIndex+1:]...)
			break
		}
	}
}

// parseOrdered décode un document JSON en conservant l'ordre des clés (nombres gardés tels quels : json.Number).
func parseOrdered(content []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	value, err := decodeValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, trailingErr := decoder.Token(); trailingErr != io.EOF {
		return nil, terr("srv.config.texteApresLaFin")
	}
	return value, nil
}

// maxJSONDepth borne la récursion : un fichier de réglages hostile ne doit pas épuiser la pile (erreur fatale, non rattrapable).
const maxJSONDepth = 64

// maxConfigBytes borne la taille d'un fichier de réglages ou .mcp.json avant analyse.
const maxConfigBytes = 4 << 20

var errConfigTooBig = terr("srv.config.fichierTropGros")

// readConfigFile lit un fichier de réglages en refusant ceux qui dépassent maxConfigBytes.
func readConfigFile(path string) ([]byte, error) {
	content, err := readTrusted(path)
	if err == nil && len(content) > maxConfigBytes {
		return nil, errConfigTooBig
	}
	return content, err
}

func decodeValue(decoder *json.Decoder, depth int) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	if depth >= maxJSONDepth {
		return nil, terr("srv.config.imbricationTropProfonde")
	}
	if delimiter == '{' {
		object := newOmap()
		for decoder.More() {
			keyToken, memberErr := decoder.Token()
			if memberErr != nil {
				return nil, memberErr
			}
			memberValue, memberErr := decodeValue(decoder, depth+1)
			if memberErr != nil {
				return nil, memberErr
			}
			object.set(keyToken.(string), memberValue)
		}
		_, closeErr := decoder.Token()
		return object, closeErr
	}
	array := []any{}
	for decoder.More() {
		element, elementErr := decodeValue(decoder, depth+1)
		if elementErr != nil {
			return nil, elementErr
		}
		array = append(array, element)
	}
	_, err = decoder.Token()
	return array, err
}

func encodeCompact(value any, output *bytes.Buffer) {
	switch typedValue := value.(type) {
	case *omap:
		output.WriteByte('{')
		for keyIndex, key := range typedValue.keys {
			if keyIndex > 0 {
				output.WriteByte(',')
			}
			encodeCompact(key, output)
			output.WriteByte(':')
			encodeCompact(typedValue.m[key], output)
		}
		output.WriteByte('}')
	case []any:
		output.WriteByte('[')
		for elementIndex, element := range typedValue {
			if elementIndex > 0 {
				output.WriteByte(',')
			}
			encodeCompact(element, output)
		}
		output.WriteByte(']')
	default:
		encoder := json.NewEncoder(output)
		encoder.SetEscapeHTML(false)
		encoder.Encode(typedValue)
		output.Truncate(output.Len() - 1) // le « \n » de Encode
	}
}

// formatOrdered : le document en JSON indenté de deux espaces, comme Claude Code l'écrit ; sans « \n » final.
func formatOrdered(document any) string {
	var compact bytes.Buffer
	encodeCompact(document, &compact)
	var indented bytes.Buffer
	json.Indent(&indented, compact.Bytes(), "", "  ")
	return indented.String()
}

// --- fichiers de réglages -----------------------------------------------------------------------------

// ConfigFile : un fichier de réglages. id : « u » (utilisateur), « p/<dossier> » (projet), « l/<dossier> » (local).
type ConfigFile struct {
	ID     string `json:"id"`
	Scope  string `json:"scope"` // user, project, local
	Dir    string `json:"dir,omitempty"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Error  string `json:"error,omitempty"`
	Data   any    `json:"data,omitempty"` // contenu, secrets masqués
}

// configPath traduit un identifiant en chemin, après validation stricte (le projet doit exister).
func configPath(id string) (string, error) {
	switch {
	case id == "u":
		return filepath.Join(cfgDir(), "settings.json"), nil
	case strings.HasPrefix(id, "p/"), strings.HasPrefix(id, "l/"):
		dir := id[2:]
		if !safeDir(dir) {
			return "", terr("srv.memmeta.projetInvalide")
		}
		cwd := projectCwd(dir)
		dirInfo, err := os.Stat(cwd) // #nosec G703 -- chemin géré par gld par conception, confiné par rootfs.go sous GLD_RUN_AS
		projectMissing := cwd == "" || err != nil || !dirInfo.IsDir()
		if projectMissing {
			return "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
		name := "settings.json"
		if id[0] == 'l' {
			name = "settings.local.json"
		}
		return filepath.Join(cwd, ".claude", name), nil
	}
	return "", terr("srv.config.fichierDeReglagesInvalide")
}

var secretKey = regexp.MustCompile(`(?i)(key|token|secret|passw|credential|auth|cookie|bearer)`)

// Contextes de masquage strict (appelant sans config_write) : env et commandes de hooks sont des secrets par défaut.
var credInRule = regexp.MustCompile(`(?i)(authorization|bearer|passw|secret|token|://[^/\s:@]+:[^@\s]*@)`)

// maskSecrets remplace par des points la valeur texte de toute clé qui ressemble à un secret ; le drapeau se propage
// aux objets et tableaux imbriqués sous cette clé.
func maskSecrets(value any, key string) any {
	return maskWalk(value, key, secretKey.MatchString(key), false, false, false)
}

// maskSettings : maskSecrets, et si strict (appelant sans config_write) env, commandes de hooks et règles de
// permission contenant des identifiants sont masqués aussi.
func maskSettings(value any, strict bool) any {
	return maskWalk(value, "", false, strict, false, false)
}

func maskWalk(value any, key string, isSecret, strict, inHooks, inPermissions bool) any {
	switch typedValue := value.(type) {
	case *omap:
		maskedObject := newOmap()
		for _, childKey := range typedValue.keys {
			isEnv := childKey == "env"
			isHookCommand := inHooks && childKey == "command"
			childIsSecret := isSecret || secretKey.MatchString(childKey) || (strict && (isEnv || isHookCommand))
			childInHooks := inHooks || childKey == "hooks"
			childInPermissions := inPermissions || childKey == "permissions"
			maskedObject.set(childKey, maskWalk(typedValue.m[childKey], childKey, childIsSecret, strict, childInHooks, childInPermissions))
		}
		return maskedObject
	case []any:
		maskedArray := make([]any, len(typedValue))
		for elementIndex, element := range typedValue {
			maskedArray[elementIndex] = maskWalk(element, key, isSecret, strict, inHooks, inPermissions)
		}
		return maskedArray
	case string:
		isCredentialRule := strict && inPermissions && credInRule.MatchString(typedValue)
		if typedValue != "" && (isSecret || isCredentialRule) {
			return maskText
		}
	}
	return value
}

// readConfig lit un fichier de réglages ; un fichier absent est vide, pas une erreur.
func readConfig(id string) (ConfigFile, any) {
	configFile := ConfigFile{ID: id}
	switch id[0] {
	case 'u':
		configFile.Scope, configFile.Label = "user", "~/.claude/settings.json"
	case 'p':
		configFile.Scope, configFile.Dir = "project", id[2:]
	default:
		configFile.Scope, configFile.Dir = "local", id[2:]
	}
	path, err := configPath(id)
	if err != nil {
		configFile.Error = err.Error()
		return configFile, nil
	}
	configFile.Path = path
	if configFile.Dir != "" {
		configFile.Label = ".claude/" + filepath.Base(path)
	}
	content, err := readConfigFile(path)
	if err != nil {
		if errors.Is(err, errConfigTooBig) {
			configFile.Exists, configFile.Error = true, err.Error()
		}
		return configFile, nil
	}
	configFile.Exists = true
	document, err := parseOrdered(content)
	if err != nil {
		configFile.Error = L("srv.configraw.jsonInvalide", "A1", err.Error())
		return configFile, nil
	}
	if _, isObject := document.(*omap); !isObject {
		configFile.Error = "le fichier doit contenir un objet JSON"
		return configFile, nil
	}
	return configFile, document
}

// allConfigs : le fichier utilisateur, puis, pour chaque projet dont le dossier existe, ses fichiers de projet et local
// quand ils existent. raw garde les secrets (pour les analyses), data les masque (pour l'affichage).
func allConfigs() (files []ConfigFile, raw map[string]any) { return allConfigsFor(false) }

// allConfigsFor : allConfigs, avec le masquage strict pour un appelant sans config_write.
func allConfigsFor(strict bool) (files []ConfigFile, raw map[string]any) {
	raw = map[string]any{}
	addConfig := func(id string, onlyExisting bool) {
		configFile, document := readConfig(id)
		if onlyExisting && !configFile.Exists {
			return
		}
		if document != nil {
			raw[id] = document
			configFile.Data = maskSettings(document, strict)
		}
		files = append(files, configFile)
	}
	addConfig("u", false)
	for _, liveProject := range liveProjects() {
		addConfig("p/"+liveProject.Dir, true)
		addConfig("l/"+liveProject.Dir, true)
	}
	return files, raw
}

// --- accès aux valeurs --------------------------------------------------------------------------------

func asMap(value any) *omap {
	object, _ := value.(*omap)
	return object
}

// lookup : la valeur à un chemin de clés (objets seulement).
func lookup(doc any, path ...string) (any, bool) {
	current := doc
	for _, key := range path {
		object := asMap(current)
		if object == nil {
			return nil, false
		}
		value, found := object.get(key)
		if !found {
			return nil, false
		}
		current = value
	}
	return current, true
}

func strList(value any) []string {
	list, isList := value.([]any)
	if !isList {
		return nil
	}
	var texts []string
	for _, element := range list {
		if text, isString := element.(string); isString {
			texts = append(texts, text)
		}
	}
	return texts
}

// MarshalJSON : l'ordre des clés est conservé dans les réponses de l'API.
func (orderedMap *omap) MarshalJSON() ([]byte, error) {
	var encoded bytes.Buffer
	encodeCompact(orderedMap, &encoded)
	return encoded.Bytes(), nil
}
