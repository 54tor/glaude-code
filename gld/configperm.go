package main

// Règles de permission proposées d'après ce que vos sessions récentes ont lancé : les commandes Bash simples qui
// reviennent souvent et qui ne sont pas encore autorisées. Rien n'est ajouté sans votre clic.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// mcpPrefix : clé interne des serveurs MCP dans le décompte (l'octet nul ne peut pas figurer dans une commande Bash).
const mcpPrefix = "\x00mcp:"

const (
	cmdUsageTTL           = 60 * time.Second
	recentSessionsScanned = 40
	transcriptTailBytes   = 3 << 20
	usageExampleMaxLen    = 100
	minSuggestCount       = 3
	maxSuggestions        = 25
)

type PermSuggest struct {
	Rule     string `json:"rule"`
	Count    int    `json:"count"`
	ReadOnly bool   `json:"readOnly"` // commande de lecture connue
	Example  string `json:"example"`
}

var (
	// jamais proposées : elles exécutent ou modifient beaucoup trop de choses pour une règle générale (une règle
	// Bash(cmd:*) autorise TOUTES les options de cmd), ou lancent une autre commande (env, find -exec, xargs…).
	neverSuggest = map[string]bool{"rm": true, "sudo": true, "dd": true, "mkfs": true, "chmod": true, "chown": true, "curl": true, "wget": true, "ssh": true, "scp": true, "rsync": true,
		"eval": true, "sh": true, "bash": true, "zsh": true, "python": true, "python3": true, "node": true, "perl": true, "ruby": true, "xargs": true, "kill": true, "pkill": true, "mv": true, "cp": true, "docker": true, "kubectl": true,
		"env": true, "nice": true, "timeout": true, "nohup": true, "time": true, "command": true, "exec": true, "awk": true, "sed": true, "find": true, "tee": true, "source": true, "watch": true, "ionice": true, "setsid": true, "stdbuf": true, "script": true}
	// en lecture seule quelles que soient les options, car une règle « cmd:* » les autorise toutes. Pas de « git diff », « git log »
	// ni « git show » (--output écrit un fichier, --ext-diff et --textconv lancent un programme), ni « file » (-C écrit), ni « date » (-s),
	// ni les tests, builds et scripts (ils exécutent du code) : ceux-là sont « à vérifier ».
	readOnlyCmds = map[string]bool{"ls": true, "cat": true, "head": true, "tail": true, "wc": true, "grep": true, "pwd": true, "stat": true, "diff": true, "which": true, "git status": true}
	twoWords     = map[string]bool{"git": true, "go": true, "npm": true, "make": true, "cargo": true, "gh": true, "systemctl": true, "npx": true, "pnpm": true, "yarn": true}
)

// bashPrefix : le préfixe sur lequel bâtir une règle pour une commande simple ; "" si elle est composée (&&, |, redirection…).
func bashPrefix(command string) string {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, "|&;<>`\n") || strings.Contains(command, "$(") {
		return ""
	}
	fields := strings.Fields(command)
	if len(fields) == 0 || strings.Contains(fields[0], "=") {
		return ""
	}
	if strings.HasPrefix(fields[0], ".") || strings.ContainsAny(fields[0], "/$(){}*?~\"'") { // script, variable, sous-shell : pas de règle sur un nom de commande
		return ""
	}
	first := filepath.Base(fields[0])
	if twoWords[first] {
		if len(fields) < 2 || strings.HasPrefix(fields[1], "-") { // « git -c x=y … » : une règle sur « git » seul autoriserait tout git
			return ""
		}
		return first + " " + fields[1]
	}
	return first
}

var shellKeyword = map[string]bool{"for": true, "while": true, "until": true, "if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true, "case": true, "esac": true, "function": true, "{": true, "}": true, "(": true, ")": true, "!": true, "cd": true, "export": true, "set": true, "unset": true, "echo": true, "printf": true, "true": true, "false": true, "test": true, "[": true, "[[": true}

// unquoted : le texte sans ce qui est entre guillemets.
func unquoted(text string) string {
	var result strings.Builder
	var openQuote rune
	for _, character := range text {
		switch {
		case openQuote != 0:
			if character == openQuote {
				openQuote = 0
			}
		case character == '\'' || character == '"':
			openQuote = character
		default:
			result.WriteRune(character)
		}
	}
	return result.String()
}

// bashSegments : les commandes simples d'une commande composée (&&, ||, ;, |, sauts de ligne hors guillemets), comme Claude Code les
// évalue une à une. Une commande avec here-document, substitution ou redirection vers un fichier n'est pas découpée : elle donne nil.
// Les mots-clés du shell (for, if…), cd, echo et autres commandes sans effet à autoriser sont écartés.
func bashSegments(command string) []string {
	hasUnsplittablePart := strings.Contains(command, "<<") || strings.Contains(command, "$(") || strings.Contains(command, "`")
	if hasUnsplittablePart {
		return nil
	}
	var segments []string
	var current strings.Builder
	var quote rune
	flushSegment := func() {
		if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
			segments = append(segments, trimmed)
		}
		current.Reset()
	}
	runes := []rune(command)
	for runeIndex := 0; runeIndex < len(runes); runeIndex++ {
		character := runes[runeIndex]
		hasNext := runeIndex+1 < len(runes)
		isDoubledOperator := hasNext && (character == '&' || character == '|') && runes[runeIndex+1] == character
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			}
			current.WriteRune(character)
		case character == '\'' || character == '"':
			quote = character
			current.WriteRune(character)
		case character == '\\' && hasNext:
			current.WriteRune(character)
			runeIndex++
			current.WriteRune(runes[runeIndex])
		case isDoubledOperator:
			runeIndex++
			flushSegment()
		case character == ';' || character == '|' || character == '\n':
			flushSegment()
		default:
			current.WriteRune(character)
		}
	}
	flushSegment()
	var commands []string
	for _, segment := range segments {
		// « 2>&1 », « 2>/dev/null » et « >/dev/null » ne comptent pas comme des écritures de fichier.
		cleaned := strings.NewReplacer("2>&1", "", "2>/dev/null", "", ">/dev/null", "", "&>/dev/null", "").Replace(segment)
		if strings.ContainsAny(unquoted(cleaned), "<>&") {
			continue // redirection vers un fichier, arrière-plan : une règle sur la commande seule ne le couvre pas
		}
		fields := strings.Fields(cleaned)
		for len(fields) > 0 && (fields[0] == "do" || fields[0] == "then" || fields[0] == "else") { // « do ls » : la commande suit le mot-clé
			fields = fields[1:]
		}
		if len(fields) == 0 || shellKeyword[fields[0]] || strings.Contains(fields[0], "=") {
			continue
		}
		commands = append(commands, strings.Join(fields, " "))
	}
	return commands
}

// tailLines : les lignes complètes des derniers maxBytes octets d'un fichier.
func tailLines(path string, maxBytes int64) [][]byte {
	file, err := openTrusted(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil {
		return nil
	}
	offset := max(fileInfo.Size()-maxBytes, 0)
	if _, seekErr := file.Seek(offset, io.SeekStart); seekErr != nil {
		return nil
	}
	content, _ := io.ReadAll(file)
	lines := splitLines(content)
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:] // la première est coupée
	}
	return lines
}

// usageCounts : le décompte des commandes Bash et des serveurs MCP des 40 sessions les plus récentes (les 3 derniers Mo de
// chacune). La lecture coûte plusieurs secondes : le résultat est gardé 60 s, le filtre par règles existantes se refait à chaque appel.
var (
	cmdUsageMu    sync.Mutex
	cmdUsageAt    time.Time
	cmdUsageRoot  string // le cache ne vaut que pour le dossier de transcripts lu
	cmdUsageCount map[string]int
	cmdUsageEx    map[string]string
)

func usageCounts() (map[string]int, map[string]string) {
	cmdUsageMu.Lock()
	defer cmdUsageMu.Unlock()
	cacheIsFresh := cmdUsageCount != nil && cmdUsageRoot == root && time.Since(cmdUsageAt) < cmdUsageTTL
	if cacheIsFresh {
		return cmdUsageCount, cmdUsageEx
	}
	type recentFile struct {
		path    string
		modTime int64
	}
	var files []recentFile
	dirs, _ := readDirTrusted(root)
	for _, dirEntry := range dirs {
		transcriptPaths, _ := filepath.Glob(filepath.Join(root, dirEntry.Name(), "*.jsonl"))
		for _, transcriptPath := range transcriptPaths {
			if fileInfo, err := os.Stat(transcriptPath); err == nil {
				files = append(files, recentFile{transcriptPath, fileInfo.ModTime().UnixNano()})
			}
		}
	}
	sort.Slice(files, func(left, right int) bool {
		return files[left].modTime > files[right].modTime
	})
	if len(files) > recentSessionsScanned {
		files = files[:recentSessionsScanned]
	}
	counts := map[string]int{}
	example := map[string]string{}
	for _, recent := range files {
		for _, transcriptLine := range tailLines(recent.path, transcriptTailBytes) {
			mentionsTool := strings.Contains(string(transcriptLine), `"name":"Bash"`) || strings.Contains(string(transcriptLine), `"name":"mcp__`)
			if !mentionsTool {
				continue
			}
			var lineEntry struct {
				Message struct {
					Content []struct {
						Type, Name string
						Input      struct{ Command string }
					}
				}
			}
			if json.Unmarshal(transcriptLine, &lineEntry) != nil {
				continue
			}
			for _, contentBlock := range lineEntry.Message.Content {
				// outil MCP (mcp__serveur__outil) : la règle porte sur le serveur entier, sans quoi chaque outil redemande
				serverName, _, hasToolSuffix := strings.Cut(strings.TrimPrefix(contentBlock.Name, "mcp__"), "__")
				isToolUse := contentBlock.Type == "tool_use"
				isMcpTool := isToolUse && strings.HasPrefix(contentBlock.Name, "mcp__") && hasToolSuffix && serverName != ""
				if isMcpTool {
					counts[mcpPrefix+serverName]++
					if example[mcpPrefix+serverName] == "" {
						example[mcpPrefix+serverName] = contentBlock.Name
					}
					continue
				}
				if isToolUse && contentBlock.Name == "Bash" {
					segments := bashSegments(contentBlock.Input.Command)
					if len(segments) == 0 {
						segments = []string{contentBlock.Input.Command} // commande simple, ou que bashPrefix saura refuser
					}
					seen := map[string]bool{} // une commande qui répète le même préfixe ne compte qu'une fois
					for _, segment := range segments {
						if prefix := bashPrefix(segment); prefix != "" && !seen[prefix] {
							seen[prefix] = true
							counts[prefix]++
							if example[prefix] == "" {
								example[prefix] = cut(contentBlock.Input.Command, usageExampleMaxLen)
							}
						}
					}
				}
			}
		}
	}
	cmdUsageCount, cmdUsageEx, cmdUsageAt, cmdUsageRoot = counts, example, time.Now(), root
	return counts, example
}

// permSuggestions regarde les commandes Bash des 40 sessions les plus récentes (les 3 derniers Mo de chacune).
func permSuggestions(existing []string) []PermSuggest {
	counts, example := usageCounts()
	existingRules := map[string]bool{}
	for _, rule := range existing {
		existingRules[rule] = true
	}
	var suggestions []PermSuggest
	for prefix, count := range counts {
		if serverName, isMcp := strings.CutPrefix(prefix, mcpPrefix); isMcp {
			mcpRule := "mcp__" + serverName
			alreadyAllowed := existingRules[mcpRule] || existingRules[mcpRule+"__*"] || existingRules["mcp__*"]
			if count >= minSuggestCount && !alreadyAllowed {
				suggestions = append(suggestions, PermSuggest{Rule: mcpRule, Count: count, Example: example[prefix]})
			}
			continue
		}
		first := strings.Fields(prefix)[0]
		bashRule := "Bash(" + prefix + ":*)"
		alreadyAllowed := existingRules[bashRule] || existingRules["Bash("+first+":*)"] || existingRules["Bash"]
		if count < minSuggestCount || neverSuggest[first] || alreadyAllowed {
			continue
		}
		suggestions = append(suggestions, PermSuggest{Rule: bashRule, Count: count, ReadOnly: readOnlyCmds[prefix], Example: example[prefix]})
	}
	sort.Slice(suggestions, func(left, right int) bool {
		if suggestions[left].Count != suggestions[right].Count {
			return suggestions[left].Count > suggestions[right].Count
		}
		return suggestions[left].Rule < suggestions[right].Rule
	})
	if len(suggestions) > maxSuggestions {
		suggestions = suggestions[:maxSuggestions]
	}
	return suggestions
}
