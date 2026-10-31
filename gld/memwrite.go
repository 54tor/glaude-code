// Rédaction d'un fichier de mémoire par un petit modèle (Haiku par défaut), sans le contexte de Claude Code :
// ni outils, ni compétences, ni MCP, ni réglages. Le résultat est un brouillon : rien n'est écrit ici.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

var writeModels = []string{"haiku", "sonnet", "opus", "fable"}

func memSystem() string { return L("prompt.memSystem") }

// claudeSystem : pour un CLAUDE.md (instructions lues à chaque session), pas d'en-tête YAML.
func claudeSystem() string { return L("prompt.claudeSystem") }

// agentsSystem : même consigne pour un AGENTS.md, lu par les agents de code en général.
func agentsSystem() string {
	return strings.NewReplacer("CLAUDE.md", "AGENTS.md", L("prompt.agentsFrom"), L("prompt.agentsTo")).Replace(claudeSystem())
}

// cmdSystem : mise en forme d'une commande personnalisée de Claude Code (un snippet).
func cmdSystem() string { return L("prompt.cmdSystem") }

// snippetDraft : la commande mise en forme (ou rédigée) par le modèle, à partir du texte de l'éditeur et d'une consigne.
func snippetDraft(model, instruction, text string) (string, float64, error) {
	instruction, text = strings.TrimSpace(instruction), strings.TrimSpace(text)
	switch {
	case !slices.Contains(writeModels, model):
		return "", 0, terr("srv.memwrite.modeleInconnu")
	case instruction == "" && text == "":
		return "", 0, terr("srv.memwrite.rienAMettreEn")
	case len(instruction) > 8<<10 || len(text) > snippetMax:
		return "", 0, terr("srv.memwrite.consigneOuTexteTrop")
	}
	if instruction == "" {
		instruction = L("prompt.cmdDefault")
	}
	prompt := L("prompt.instruction", "A1", instruction) + "\n"
	if text != "" {
		prompt += "\n" + L("prompt.currentCommand") + "\n~~~~\n" + text + "\n~~~~\n"
	}
	return memDraft(model, cmdSystem(), prompt)
}

// isClaudeMd : la clé désigne un fichier du projet ou global (CLAUDE.md, AGENTS.md, README.md, PLAN.md…), pas une entrée de mémoire :
// du Markdown libre, sans en-tête YAML.
func isClaudeMd(key string) bool {
	return key == "g/CLAUDE.md" || strings.HasPrefix(key, "p/")
}

// docSystem : pour la documentation d'un projet (README, plan, conventions, outillage), ni instructions de Claude ni mémoire.
func docSystem() string { return L("prompt.docSystem") }

// memWritePrompt : la consigne, le fichier à réécrire (s'il y en a un) et les références, toutes des clés validées.
func memWritePrompt(instruction, key string, refs []string) (string, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || len(instruction) > 8<<10 || len(refs) > 20 {
		return "", terr("srv.memwrite.consigneVideOuTrop")
	}
	var prompt strings.Builder
	prompt.WriteString(L("prompt.instruction", "A1", instruction) + "\n")
	quote := func(title, quotedKey string) error {
		base, rel, err := memResolve(quotedKey)
		if err != nil {
			return err
		}
		quotedText, err := memRead(quotedKey)
		if err != nil {
			return err
		}
		prompt.WriteString("\n" + L("prompt.quote", "A1", title, "A2", base+string(os.PathSeparator)+rel) + "\n~~~~\n" + quotedText + "\n~~~~\n")
		return nil
	}
	if key != "" {
		// Un CLAUDE.md à créer n'existe pas encore : il n'y a rien à citer.
		if _, readErr := memRead(key); errors.Is(readErr, os.ErrNotExist) {
			if _, _, resolveErr := memResolve(key); resolveErr != nil {
				return "", resolveErr
			}
			prompt.WriteString("\n" + L("prompt.fileMissing") + "\n")
		} else if quoteErr := quote(L("prompt.fileToRewrite"), key); quoteErr != nil {
			return "", quoteErr
		}
	}
	for _, referenceKey := range refs {
		if referenceKey == key {
			continue
		}
		if quoteErr := quote(L("prompt.reference"), referenceKey); quoteErr != nil {
			return "", quoteErr
		}
	}
	return prompt.String(), nil
}

var fenceRe = regexp.MustCompile("(?s)^```[a-z]*\n(.*)\n```$")

// memWrite demande le texte au modèle ; il est renvoyé tel quel (moins un éventuel bloc de code englobant).
var memDraft = func(model, system, prompt string) (string, float64, error) {
	draftContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := helperCmd(draftContext, model, system)
	cmd.Stdin = strings.NewReader(prompt) // sur stdin : un argument est limité à 128 Kio (MAX_ARG_STRLEN), un brouillon peut citer 20 Mio
	output, err := helperOutput(draftContext, cmd)
	var reply struct {
		Result  string
		IsError bool    `json:"is_error"`
		Cost    float64 `json:"total_cost_usd"`
	}
	if json.Unmarshal(output, &reply) != nil {
		if err == nil {
			err = terr("srv.memwrite.reponseIllisible")
		}
		return "", 0, err
	}
	if reply.IsError {
		return "", reply.Cost, errors.New(cut(reply.Result, 300))
	}
	draftText := strings.TrimSpace(reply.Result)
	if fenceMatch := fenceRe.FindStringSubmatch(draftText); fenceMatch != nil {
		draftText = fenceMatch[1]
	}
	return draftText + "\n", reply.Cost, nil
}

func memWriteDraft(model, instruction, key string, refs []string) (string, float64, error) {
	if !slices.Contains(writeModels, model) {
		return "", 0, terr("srv.memwrite.modeleInconnu")
	}
	prompt, err := memWritePrompt(instruction, key, refs)
	if err != nil {
		return "", 0, err
	}
	system := memSystem()
	if isClaudeMd(key) {
		system = claudeSystem()
		switch {
		case strings.HasSuffix(key, "AGENTS.md"):
			system = agentsSystem()
		case !isClaudeFile(key):
			system = docSystem()
		}
	}
	text, cost, err := memDraft(model, system, prompt)
	if err == nil && system == memSystem() {
		if original, readErr := memRead(key); readErr == nil {
			text = keepHeader(original, text)
		}
	}
	return text, cost, err
}

// keepHeader : un modèle qui « condense » une entrée de mémoire oublie parfois son en-tête YAML (name, description, type),
// et le fichier perd alors son nom, sa description et sa ligne d'index. Si l'original en avait un et pas le brouillon,
// on remet l'original en tête.
func keepHeader(original, draft string) string {
	header, _, hasHeader := frontMatter(original)
	if !hasHeader {
		return draft
	}
	if _, _, draftHasHeader := frontMatter(draft); draftHasHeader {
		return draft
	}
	return "---\n" + header + "\n---\n\n" + strings.TrimLeft(draft, "\n")
}

// memIndex ajoute à MEMORY.md la ligne d'une nouvelle entrée de mémoire de projet, si elle n'y est pas déjà.
func memIndex(key, text string) error {
	if !strings.HasPrefix(key, "m/") {
		return nil
	}
	slashIndex := strings.LastIndex(key, "/")
	file, indexKey := key[slashIndex+1:], key[:slashIndex+1]+"MEMORY.md"
	if file == "MEMORY.md" {
		return nil
	}
	indexText, _ := memRead(indexKey)
	if strings.Contains(indexText, "]("+file+")") {
		return nil
	}
	title := strings.TrimSuffix(file, ".md")
	if name := metaString(text, "name"); name != "" {
		title = name
	}
	indexEntry := "- [" + title + "](" + file + ")"
	if description := metaString(text, "description"); description != "" {
		indexEntry += " — " + description
	}
	if indexText != "" && !strings.HasSuffix(indexText, "\n") {
		indexText += "\n"
	}
	return memWrite(indexKey, indexText+indexEntry+"\n", false)
}
