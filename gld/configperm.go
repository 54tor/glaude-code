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
func bashPrefix(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, "|&;<>`\n") || strings.Contains(cmd, "$(") {
		return ""
	}
	f := strings.Fields(cmd)
	if len(f) == 0 || strings.Contains(f[0], "=") {
		return ""
	}
	if strings.HasPrefix(f[0], ".") || strings.ContainsAny(f[0], "/$(){}*?~\"'") { // script, variable, sous-shell : pas de règle sur un nom de commande
		return ""
	}
	first := filepath.Base(f[0])
	if twoWords[first] {
		if len(f) < 2 || strings.HasPrefix(f[1], "-") { // « git -c x=y … » : une règle sur « git » seul autoriserait tout git
			return ""
		}
		return first + " " + f[1]
	}
	return first
}

var shellKeyword = map[string]bool{"for": true, "while": true, "until": true, "if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true, "case": true, "esac": true, "function": true, "{": true, "}": true, "(": true, ")": true, "!": true, "cd": true, "export": true, "set": true, "unset": true, "echo": true, "printf": true, "true": true, "false": true, "test": true, "[": true, "[[": true}

// unquoted : le texte sans ce qui est entre guillemets.
func unquoted(s string) string {
	var b strings.Builder
	var q rune
	for _, c := range s {
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '\'' || c == '"':
			q = c
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// bashSegments : les commandes simples d'une commande composée (&&, ||, ;, |, sauts de ligne hors guillemets), comme Claude Code les
// évalue une à une. Une commande avec here-document, substitution ou redirection vers un fichier n'est pas découpée : elle donne nil.
// Les mots-clés du shell (for, if…), cd, echo et autres commandes sans effet à autoriser sont écartés.
func bashSegments(cmd string) []string {
	if strings.Contains(cmd, "<<") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "`") {
		return nil
	}
	var segs []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			segs = append(segs, t)
		}
		cur.Reset()
	}
	rs := []rune(cmd)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			cur.WriteRune(c)
		case c == '\'' || c == '"':
			quote = c
			cur.WriteRune(c)
		case c == '\\' && i+1 < len(rs):
			cur.WriteRune(c)
			i++
			cur.WriteRune(rs[i])
		case c == '&' && i+1 < len(rs) && rs[i+1] == '&', c == '|' && i+1 < len(rs) && rs[i+1] == '|':
			i++
			flush()
		case c == ';' || c == '|' || c == '\n':
			flush()
		default:
			cur.WriteRune(c)
		}
	}
	flush()
	var out []string
	for _, sg := range segs {
		// « 2>&1 », « 2>/dev/null » et « >/dev/null » ne comptent pas comme des écritures de fichier.
		clean := strings.NewReplacer("2>&1", "", "2>/dev/null", "", ">/dev/null", "", "&>/dev/null", "").Replace(sg)
		if strings.ContainsAny(unquoted(clean), "<>&") {
			continue // redirection vers un fichier, arrière-plan : une règle sur la commande seule ne le couvre pas
		}
		f := strings.Fields(clean)
		for len(f) > 0 && (f[0] == "do" || f[0] == "then" || f[0] == "else") { // « do ls » : la commande suit le mot-clé
			f = f[1:]
		}
		if len(f) == 0 || shellKeyword[f[0]] || strings.Contains(f[0], "=") {
			continue
		}
		out = append(out, strings.Join(f, " "))
	}
	return out
}

// tailLines : les lignes complètes des derniers n octets d'un fichier.
func tailLines(path string, n int64) [][]byte {
	f, err := openTrusted(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := max(st.Size()-n, 0)
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	b, _ := io.ReadAll(f)
	lines := splitLines(b)
	if off > 0 && len(lines) > 0 {
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
	if cmdUsageCount != nil && cmdUsageRoot == root && time.Since(cmdUsageAt) < 60*time.Second {
		return cmdUsageCount, cmdUsageEx
	}
	type fileT struct {
		p string
		t int64
	}
	var files []fileT
	dirs, _ := readDirTrusted(root)
	for _, d := range dirs {
		ms, _ := filepath.Glob(filepath.Join(root, d.Name(), "*.jsonl"))
		for _, p := range ms {
			if st, err := os.Stat(p); err == nil {
				files = append(files, fileT{p, st.ModTime().UnixNano()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].t > files[j].t })
	if len(files) > 40 {
		files = files[:40]
	}
	counts := map[string]int{}
	example := map[string]string{}
	for _, f := range files {
		for _, l := range tailLines(f.p, 3<<20) {
			if !strings.Contains(string(l), `"name":"Bash"`) && !strings.Contains(string(l), `"name":"mcp__`) {
				continue
			}
			var m struct {
				Message struct {
					Content []struct {
						Type, Name string
						Input      struct{ Command string }
					}
				}
			}
			if json.Unmarshal(l, &m) != nil {
				continue
			}
			for _, c := range m.Message.Content {
				// outil MCP (mcp__serveur__outil) : la règle porte sur le serveur entier, sans quoi chaque outil redemande
				if srv, _, ok := strings.Cut(strings.TrimPrefix(c.Name, "mcp__"), "__"); c.Type == "tool_use" && strings.HasPrefix(c.Name, "mcp__") && ok && srv != "" {
					counts[mcpPrefix+srv]++
					if example[mcpPrefix+srv] == "" {
						example[mcpPrefix+srv] = c.Name
					}
					continue
				}
				if c.Type == "tool_use" && c.Name == "Bash" {
					cmds := bashSegments(c.Input.Command)
					if len(cmds) == 0 {
						cmds = []string{c.Input.Command} // commande simple, ou que bashPrefix saura refuser
					}
					seen := map[string]bool{} // une commande qui répète le même préfixe ne compte qu'une fois
					for _, seg := range cmds {
						if p := bashPrefix(seg); p != "" && !seen[p] {
							seen[p] = true
							counts[p]++
							if example[p] == "" {
								example[p] = cut(c.Input.Command, 100)
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
	have := map[string]bool{}
	for _, r := range existing {
		have[r] = true
	}
	var out []PermSuggest
	for p, n := range counts {
		if srv, ok := strings.CutPrefix(p, mcpPrefix); ok {
			if rule := "mcp__" + srv; n >= 3 && !have[rule] && !have[rule+"__*"] && !have["mcp__*"] {
				out = append(out, PermSuggest{Rule: rule, Count: n, Example: example[p]})
			}
			continue
		}
		first := strings.Fields(p)[0]
		rule := "Bash(" + p + ":*)"
		if n < 3 || neverSuggest[first] || have[rule] || have["Bash("+first+":*)"] || have["Bash"] {
			continue
		}
		out = append(out, PermSuggest{Rule: rule, Count: n, ReadOnly: readOnlyCmds[p], Example: example[p]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Rule < out[j].Rule
	})
	if len(out) > 25 {
		out = out[:25]
	}
	return out
}
