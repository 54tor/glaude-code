// Rédaction d'un fichier de mémoire par un petit modèle (Haiku par défaut), sans le contexte de Claude Code :
// ni outils, ni compétences, ni MCP, ni réglages. Le résultat est un brouillon : rien n'est écrit ici.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

var writeModels = []string{"haiku", "sonnet", "opus", "fable"}

const memSystem = `Tu rédiges des fichiers de mémoire Markdown pour Claude Code.
Format : un en-tête YAML entre deux lignes « --- » avec name (kebab-case), description (une ligne, sert à décider de la pertinence) et metadata.type (user, feedback, project ou reference), puis le fait lui-même, concis. Pour feedback et project, ajoute une ligne **Why:** puis une ligne **How to apply:**.
Garde la langue des références. Réponds uniquement avec le contenu complet du fichier, sans bloc de code ni commentaire.`

// claudeSystem : pour un CLAUDE.md (instructions lues à chaque session), pas d'en-tête YAML.
const claudeSystem = `Tu rédiges un fichier CLAUDE.md : les instructions que Claude Code lit au début de chaque session.
Markdown concis : quelques titres, des listes de règles courtes et actionnables, regroupées par thème ; pas d'en-tête YAML, pas de répétition, pas de remplissage.
Garde ce que contient déjà le fichier existant (sauf s'il contredit les références, auquel cas les références l'emportent) et la langue des sources.
Réponds uniquement avec le contenu complet du fichier, sans bloc de code ni commentaire.`

// cmdSystem : mise en forme d'une commande personnalisée de Claude Code (un snippet).
const cmdSystem = `Tu mets en forme une commande personnalisée de Claude Code : un fichier Markdown de prompt, invoqué par /nom.
Format : un en-tête YAML entre deux lignes « --- » avec description (une ligne : ce que fait la commande) et, seulement si la commande prend des arguments, argument-hint (ex. [fichier] [niveau]) ; puis le prompt lui-même, clair et à l'impératif, structuré si c'est utile (étapes, contraintes, format de sortie attendu). Mets $ARGUMENTS (ou $1, $2…) là où l'utilisateur donne ses arguments.
N'invente aucune exigence absente de la consigne ou du texte d'origine. Garde la langue de la source. Réponds uniquement avec le contenu complet du fichier, sans bloc de code ni commentaire.`

// snippetDraft : la commande mise en forme (ou rédigée) par le modèle, à partir du texte de l'éditeur et d'une consigne.
func snippetDraft(model, instruction, text string) (string, float64, error) {
	instruction, text = strings.TrimSpace(instruction), strings.TrimSpace(text)
	switch {
	case !slices.Contains(writeModels, model):
		return "", 0, errors.New("modèle inconnu")
	case instruction == "" && text == "":
		return "", 0, errors.New("rien à mettre en forme")
	case len(instruction) > 8<<10 || len(text) > snippetMax:
		return "", 0, errors.New("consigne ou texte trop long")
	}
	if instruction == "" {
		instruction = "Mets ce texte en forme comme une commande Claude Code."
	}
	prompt := "Consigne : " + instruction + "\n"
	if text != "" {
		prompt += "\nTexte actuel de la commande :\n~~~~\n" + text + "\n~~~~\n"
	}
	return memDraft(model, cmdSystem, prompt)
}

// isClaudeMd : la clé désigne un CLAUDE.md (global ou de projet), pas une entrée de mémoire.
func isClaudeMd(key string) bool {
	return key == "g/CLAUDE.md" || (strings.HasPrefix(key, "p/") && strings.HasSuffix(key, "CLAUDE.md"))
}

// memWritePrompt : la consigne, le fichier à réécrire (s'il y en a un) et les références, toutes des clés validées.
func memWritePrompt(instruction, key string, refs []string) (string, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || len(instruction) > 8<<10 || len(refs) > 20 {
		return "", errors.New("consigne vide ou trop longue, ou trop de références")
	}
	var b strings.Builder
	b.WriteString("Consigne : " + instruction + "\n")
	quote := func(title, k string) error {
		base, rel, err := memResolve(k)
		if err != nil {
			return err
		}
		t, err := memRead(k)
		if err != nil {
			return err
		}
		b.WriteString("\n" + title + " (" + base + string(os.PathSeparator) + rel + ") :\n~~~~\n" + t + "\n~~~~\n")
		return nil
	}
	if key != "" {
		// Un CLAUDE.md à créer n'existe pas encore : il n'y a rien à citer.
		if _, err := memRead(key); errors.Is(err, os.ErrNotExist) {
			if _, _, err := memResolve(key); err != nil {
				return "", err
			}
			b.WriteString("\n(Le fichier n'existe pas encore : écris-le en entier.)\n")
		} else if err := quote("Fichier à réécrire", key); err != nil {
			return "", err
		}
	}
	for _, k := range refs {
		if k == key {
			continue
		}
		if err := quote("Référence", k); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

var fenceRe = regexp.MustCompile("(?s)^```[a-z]*\n(.*)\n```$")

// memWrite demande le texte au modèle ; il est renvoyé tel quel (moins un éventuel bloc de code englobant).
var memDraft = func(model, system, prompt string) (string, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := withPath(exec.CommandContext(ctx, "claude", "-p", "--model", model, "--tools", "", "--disable-slash-commands",
		"--strict-mcp-config", "--setting-sources", "", "--no-session-persistence", "--system-prompt", system,
		"--output-format", "json"))
	cmd.Stdin = strings.NewReader(prompt) // sur stdin : un argument est limité à 128 Kio (MAX_ARG_STRLEN), un brouillon peut citer 20 Mio
	cmd.Dir = os.TempDir()                // aucun CLAUDE.md de projet
	out, err := cmd.Output()
	var r struct {
		Result  string
		IsError bool    `json:"is_error"`
		Cost    float64 `json:"total_cost_usd"`
	}
	if json.Unmarshal(out, &r) != nil {
		if err == nil {
			err = errors.New("réponse illisible")
		}
		return "", 0, err
	}
	if r.IsError {
		return "", r.Cost, errors.New(cut(r.Result, 300))
	}
	t := strings.TrimSpace(r.Result)
	if m := fenceRe.FindStringSubmatch(t); m != nil {
		t = m[1]
	}
	return t + "\n", r.Cost, nil
}

func memWriteDraft(model, instruction, key string, refs []string) (string, float64, error) {
	if !slices.Contains(writeModels, model) {
		return "", 0, errors.New("modèle inconnu")
	}
	prompt, err := memWritePrompt(instruction, key, refs)
	if err != nil {
		return "", 0, err
	}
	system := memSystem
	if isClaudeMd(key) {
		system = claudeSystem
	}
	return memDraft(model, system, prompt)
}

var nameRe = regexp.MustCompile(`(?m)^name:\s*["']?([^"'\n]+?)["']?\s*$`)
var descRe = regexp.MustCompile(`(?m)^description:\s*["']?([^\n]+?)["']?\s*$`)

// memIndex ajoute à MEMORY.md la ligne d'une nouvelle entrée de mémoire de projet, si elle n'y est pas déjà.
func memIndex(key, text string) error {
	if !strings.HasPrefix(key, "m/") {
		return nil
	}
	i := strings.LastIndex(key, "/")
	file, ik := key[i+1:], key[:i+1]+"MEMORY.md"
	if file == "MEMORY.md" {
		return nil
	}
	idx, _ := memRead(ik)
	if strings.Contains(idx, "]("+file+")") {
		return nil
	}
	title := strings.TrimSuffix(file, ".md")
	if m := nameRe.FindStringSubmatch(text); m != nil {
		title = m[1]
	}
	line := "- [" + title + "](" + file + ")"
	if m := descRe.FindStringSubmatch(text); m != nil {
		line += " — " + m[1]
	}
	if idx != "" && !strings.HasSuffix(idx, "\n") {
		idx += "\n"
	}
	return memWrite(ik, idx+line+"\n", false)
}
