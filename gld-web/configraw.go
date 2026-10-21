package main

// Édition du fichier de réglages en JSON brut, avec validation. Les valeurs de secrets sont affichées masquées ; un
// masque laissé tel quel à son emplacement est remplacé par la valeur d'origine à l'enregistrement, de sorte qu'on peut
// tout modifier sans jamais voir ni perdre un secret.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const maskText = "••••••••"

// syntaxError ajoute la ligne et la colonne à une erreur de syntaxe JSON.
func syntaxError(text string, err error) error {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line, col := 1, 1
		for i, c := range text {
			if int64(i) >= se.Offset {
				break
			}
			if c == '\n' {
				line, col = line+1, 1
			} else {
				col++
			}
		}
		return fmt.Errorf("JSON invalide, ligne %d, colonne %d : %v", line, col, se)
	}
	return errors.New("JSON invalide : " + err.Error())
}

// restoreMasked remplace, dans n, chaque masque resté à sa place par la valeur de o au même chemin.
func restoreMasked(n, o any, path string) (any, error) {
	switch x := n.(type) {
	case *omap:
		oo := asMap(o)
		for _, k := range x.keys {
			var ov any
			if oo != nil {
				ov = oo.m[k]
			}
			v, err := restoreMasked(x.m[k], ov, path+"."+k)
			if err != nil {
				return nil, err
			}
			x.m[k] = v
		}
	case []any:
		oa, _ := o.([]any)
		for i := range x {
			var ov any
			if i < len(oa) {
				ov = oa[i]
			}
			v, err := restoreMasked(x[i], ov, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			x[i] = v
		}
	case string:
		if x == maskText {
			if s, ok := o.(string); ok {
				return s, nil
			}
			return nil, errors.New("valeur masquée sans original à " + strings.TrimPrefix(path, ".") + " : saisissez la vraie valeur, ou remettez la clé à sa place")
		}
	}
	return n, nil
}

// configRawRead : le fichier en JSON indenté, secrets masqués (un fichier absent donne « {} »).
func configRawRead(id string) (string, error) {
	path, err := configPath(id)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return "{}\n", nil
	}
	v, err := parseOrdered(b)
	if err != nil {
		return "", syntaxError(string(b), err)
	}
	return formatOrdered(maskSecrets(v, "")) + "\n", nil
}

// loadDoc lit le fichier de réglages actuel (absent : document vide) après avoir refusé un lien symbolique.
func loadDoc(id string) (path string, orig []byte, exists bool, doc any, err error) {
	if path, err = configPath(id); err != nil {
		return
	}
	if err = notSymlink(path); err != nil {
		return
	}
	var rerr error
	orig, rerr = os.ReadFile(path)
	exists = rerr == nil
	doc = newOmap()
	if exists && strings.TrimSpace(string(orig)) != "" {
		if doc, err = parseOrdered(orig); err != nil {
			err = errors.New("le fichier actuel est illisible : " + err.Error() + " (restaurez une sauvegarde ou corrigez-le à la main)")
		}
	}
	return
}

// commitDoc compare le document actuel et le nouveau, renvoie leurs textes (secrets masqués, pour l'aperçu) et les
// avertissements de santé du nouveau ; sauf si dry, sauvegarde l'ancien fichier puis écrit le nouveau.
func commitDoc(id, path string, orig []byte, exists bool, origDoc, nv any, dry bool) (before, after string, warns []CfgIssue, err error) {
	before = formatOrdered(maskSecrets(origDoc, "")) + "\n"
	after = formatOrdered(maskSecrets(nv, "")) + "\n"
	warns = configHealth(nil, map[string]any{id: nv})
	if dry {
		return
	}
	content := formatOrdered(nv) + "\n"
	if exists && string(orig) == content {
		return
	}
	if exists {
		if err = saveBackup(id, orig); err != nil {
			err = errors.New("sauvegarde impossible : " + err.Error())
			return
		}
	}
	err = writeConfig(path, []byte(content))
	return
}

// configRawEdit valide et enregistre un fichier de réglages saisi en JSON brut. Renvoie les textes avant et après (secrets
// masqués, pour l'aperçu) et les avertissements de santé du nouveau contenu ; n'écrit rien si dry.
func configRawEdit(id, text string, dry bool) (before, after string, warns []CfgIssue, err error) {
	path, orig, exists, origDoc, err := loadDoc(id)
	if err != nil {
		return
	}
	if len(text) > 1<<20 {
		err = errors.New("fichier trop gros")
		return
	}
	nv, perr := parseOrdered([]byte(text))
	if perr != nil {
		err = syntaxError(text, perr)
		return
	}
	if asMap(nv) == nil {
		err = errors.New("le fichier doit contenir un objet JSON ({ … })")
		return
	}
	if nv, err = restoreMasked(nv, origDoc, ""); err != nil {
		return
	}
	return commitDoc(id, path, orig, exists, origDoc, nv, dry)
}
