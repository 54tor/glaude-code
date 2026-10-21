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
func bashPrefix(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, "|&;<>`\n") || strings.Contains(cmd, "$(") {
		return ""
	}
	f := strings.Fields(cmd)
	if len(f) == 0 || strings.Contains(f[0], "=") {
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

// tailLines : les lignes complètes des derniers n octets d'un fichier.
func tailLines(path string, n int64) [][]byte {
	f, err := os.Open(path)
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

// permSuggestions regarde les commandes Bash des 40 sessions les plus récentes (les 3 derniers Mo de chacune).
func permSuggestions(existing []string) []PermSuggest {
	type fileT struct {
		p string
		t int64
	}
	var files []fileT
	dirs, _ := os.ReadDir(root)
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
	have := map[string]bool{}
	for _, r := range existing {
		have[r] = true
	}
	counts := map[string]int{}
	example := map[string]string{}
	for _, f := range files {
		for _, l := range tailLines(f.p, 3<<20) {
			if !strings.Contains(string(l), `"name":"Bash"`) {
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
				if c.Type == "tool_use" && c.Name == "Bash" {
					if p := bashPrefix(c.Input.Command); p != "" {
						counts[p]++
						if example[p] == "" {
							example[p] = cut(c.Input.Command, 100)
						}
					}
				}
			}
		}
	}
	var out []PermSuggest
	for p, n := range counts {
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
