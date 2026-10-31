package main

// Édition du fichier de réglages en JSON brut, avec validation. Les valeurs de secrets sont affichées masquées ; un
// masque laissé tel quel à son emplacement est remplacé par la valeur d'origine à l'enregistrement, de sorte qu'on peut
// tout modifier sans jamais voir ni perdre un secret.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	maskText        = "••••••••"
	rawEditMaxBytes = 1 << 20
)

// syntaxError ajoute la ligne et la colonne à une erreur de syntaxe JSON.
func syntaxError(text string, err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		line, column := 1, 1
		for byteIndex, character := range text {
			if int64(byteIndex) >= syntaxErr.Offset {
				break
			}
			if character == '\n' {
				line, column = line+1, 1
			} else {
				column++
			}
		}
		return terr("srv.configraw.jsonInvalideLigneColonne", "A1", line, "A2", column, "A3", syntaxErr)
	}
	return terr("srv.configraw.jsonInvalide", "A1", err.Error())
}

// restoreMasked remplace, dans edited, chaque masque resté à sa place par la valeur de original au même chemin.
func restoreMasked(edited, original any, path string) (any, error) {
	switch typedValue := edited.(type) {
	case *omap:
		originalMap := asMap(original)
		for _, key := range typedValue.keys {
			var originalValue any
			if originalMap != nil {
				originalValue = originalMap.m[key]
			}
			restored, err := restoreMasked(typedValue.m[key], originalValue, path+"."+key)
			if err != nil {
				return nil, err
			}
			typedValue.m[key] = restored
		}
	case []any:
		originalList, _ := original.([]any)
		for elementIndex := range typedValue {
			var originalValue any
			if elementIndex < len(originalList) {
				originalValue = originalList[elementIndex]
			}
			restored, err := restoreMasked(typedValue[elementIndex], originalValue, fmt.Sprintf("%s[%d]", path, elementIndex))
			if err != nil {
				return nil, err
			}
			typedValue[elementIndex] = restored
		}
	case string:
		if typedValue == maskText {
			if originalText, isString := original.(string); isString {
				return originalText, nil
			}
			return nil, terr("srv.configraw.valeurMasqueeSansOriginal", "A1", strings.TrimPrefix(path, "."))
		}
	}
	return edited, nil
}

// configRawRead : le fichier en JSON indenté, secrets masqués (un fichier absent donne « {} »).
func configRawRead(id string) (string, error) {
	path, err := configPath(id)
	if err != nil {
		return "", err
	}
	content, err := readConfigFile(path)
	if errors.Is(err, errConfigTooBig) {
		return "", err
	}
	if err != nil || strings.TrimSpace(string(content)) == "" {
		return "{}\n", nil
	}
	document, err := parseOrdered(content)
	if err != nil {
		return "", syntaxError(string(content), err)
	}
	return formatOrdered(maskSecrets(document, "")) + "\n", nil
}

// loadDoc lit le fichier de réglages actuel (absent : document vide) après avoir refusé un lien symbolique.
func loadDoc(id string) (path string, original []byte, exists bool, doc any, err error) {
	if path, err = configPath(id); err != nil {
		return
	}
	if err = notSymlink(path); err != nil {
		return
	}
	var readErr error
	original, readErr = readTrusted(path)
	exists = readErr == nil
	doc = newOmap()
	if exists && strings.TrimSpace(string(original)) != "" {
		if doc, err = parseOrdered(original); err != nil {
			err = terr("srv.configraw.leFichierActuelEst", "A1", err.Error())
		}
	}
	return
}

// commitDoc compare le document actuel et le nouveau, renvoie leurs textes (secrets masqués, pour l'aperçu) et les
// avertissements de santé du nouveau ; sauf si dryRun, sauvegarde l'ancien fichier puis écrit le nouveau.
func commitDoc(id, path string, original []byte, exists bool, originalDoc, edited any, dryRun bool) (before, after string, warns []CfgIssue, err error) {
	before = formatOrdered(maskSecrets(originalDoc, "")) + "\n"
	after = formatOrdered(maskSecrets(edited, "")) + "\n"
	warns = configHealth(nil, map[string]any{id: edited})
	if dryRun {
		return
	}
	content := formatOrdered(edited) + "\n"
	if exists && string(original) == content {
		return
	}
	if exists {
		if err = saveBackup(id, original); err != nil {
			err = terr("srv.homesync.sauvegardeImpossible", "A1", err.Error())
			return
		}
	}
	err = writeConfig(path, []byte(content))
	return
}

// configRawEdit valide et enregistre un fichier de réglages saisi en JSON brut. Renvoie les textes avant et après (secrets
// masqués, pour l'aperçu) et les avertissements de santé du nouveau contenu ; n'écrit rien si dryRun.
func configRawEdit(id, text string, dryRun bool) (before, after string, warns []CfgIssue, err error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	path, original, exists, originalDoc, err := loadDoc(id)
	if err != nil {
		return
	}
	if len(text) > rawEditMaxBytes {
		err = terr("srv.configperm_io.fichierTropGros")
		return
	}
	edited, parseErr := parseOrdered([]byte(text))
	if parseErr != nil {
		err = syntaxError(text, parseErr)
		return
	}
	if asMap(edited) == nil {
		err = terr("srv.configraw.leFichierDoitContenir")
		return
	}
	if edited, err = restoreMasked(edited, originalDoc, ""); err != nil {
		return
	}
	return commitDoc(id, path, original, exists, originalDoc, edited, dryRun)
}
