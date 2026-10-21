// gld-web — sessions Claude groupées par dossier de projet, dans le navigateur.
// Suppression via gld-lib.sh (même séquence que gld-rm, qui refuse les sessions vivantes).
package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

//go:embed page.html
var page []byte

// Même séquence que gld-rm, sans resoudre_projet (qui exige que le cwd existe encore).
const rmScript = `. "$HOME/.local/bin/gld-lib.sh"; cc_tmp
sessions_vivantes | grep -Fq -- "$2" && { echo "session vivante, refusée" >&2; exit 1; }
snapshots_cites "$1/$2.jsonl" "$1/$2" > "$GLD_TMP/snapshots"
purger_session "$1" "$2" >/dev/null; echo "$2" > "$GLD_TMP/supprimees"
solder "$GLD_TMP/supprimees" "$GLD_TMP/snapshots"`

var (
	root  string
	port  = env("GLD_WEB_PORT", "8799")
	idRe  = regexp.MustCompile(`^[0-9a-f-]{36}$`)
	hosts = map[string]bool{}
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type line struct {
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// promptOf : ce que l'utilisateur a tapé (hors bruit d'amorçage et résultats d'outils).
func promptOf(l *line) string {
	var s string
	var parts []struct{ Text string }
	c := l.Message.Content
	if json.Unmarshal(c, &s) != nil && json.Unmarshal(c, &parts) == nil {
		var t []string
		for _, p := range parts {
			t = append(t, p.Text)
		}
		s = strings.Join(t, " ")
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") || strings.HasPrefix(strings.ToLower(s), "caveat:") {
		return ""
	}
	return s
}

// scan appelle fn pour chaque ligne du transcript (les lignes peuvent être énormes).
func scan(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		b, err := r.ReadBytes('\n')
		if len(b) > 0 {
			fn(b)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

type Session struct {
	ID    string  `json:"id"`
	Dir   string  `json:"dir"`
	Title string  `json:"title"`
	Named bool    `json:"named"`
	Cwd   string  `json:"cwd"`
	Mtime float64 `json:"mtime"`
	KB    int64   `json:"kb"`
}

func info(path string, st os.FileInfo) Session {
	var title, cwd, first string
	_ = scan(path, func(b []byte) {
		wantTitle := bytes.Contains(b, []byte(`"custom-title"`))
		wantCwd := cwd == "" && bytes.Contains(b, []byte(`"cwd"`))
		wantFirst := first == "" && bytes.Contains(b, []byte(`"type":"user"`))
		if !wantTitle && !wantCwd && !wantFirst {
			return
		}
		var l line
		if json.Unmarshal(b, &l) != nil {
			return
		}
		if wantTitle && l.CustomTitle != "" {
			title = l.CustomTitle
		}
		if wantCwd {
			cwd = l.Cwd
		}
		if wantFirst && l.Type == "user" {
			first = promptOf(&l)
		}
	})
	s := Session{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Dir: filepath.Base(filepath.Dir(path)),
		Named: title != "", Cwd: cwd, Mtime: float64(st.ModTime().UnixNano()) / 1e9, KB: st.Size() / 1024}
	if s.Title = title; title == "" {
		if s.Title = cut(first, 100); first == "" {
			s.Title = "(sans titre)"
		}
	}
	return s
}

type Group struct {
	Project  string    `json:"project"`
	Sessions []Session `json:"sessions"`
}

func sessions() []Group {
	groups := map[string][]Session{}
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil || st.Size() == 0 {
			continue
		}
		s := info(p, st)
		k := s.Cwd
		if k == "" {
			k = s.Dir
		}
		groups[k] = append(groups[k], s)
	}
	out := []Group{}
	for k, v := range groups {
		sort.Slice(v, func(i, j int) bool { return v[i].Mtime > v[j].Mtime })
		out = append(out, Group{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out
}

type Detail struct {
	Prompts   []string `json:"prompts"` // les 3 derniers
	N         int      `json:"n"`
	Branch    string   `json:"branch"`
	Models    []string `json:"models"`
	T0        string   `json:"t0"`
	T1        string   `json:"t1"`
	Subagents int      `json:"subagents"`
}

func detail(path string) Detail {
	d := Detail{Prompts: []string{}, Models: []string{}}
	models := map[string]bool{}
	_ = scan(path, func(b []byte) {
		var l line
		if json.Unmarshal(b, &l) != nil {
			return
		}
		if d.T0 == "" {
			d.T0 = l.Timestamp
		}
		if l.Timestamp != "" {
			d.T1 = l.Timestamp
		}
		if l.GitBranch != "" {
			d.Branch = l.GitBranch
		}
		switch l.Type {
		case "user":
			if p := promptOf(&l); p != "" {
				d.N++
				d.Prompts = append(d.Prompts, cut(p, 300))
				if len(d.Prompts) > 3 {
					d.Prompts = d.Prompts[1:]
				}
			}
		case "assistant":
			if m := l.Message.Model; m != "" && !models[m] {
				models[m] = true
				d.Models = append(d.Models, m)
			}
		}
	})
	sort.Strings(d.Models)
	filepath.WalkDir(strings.TrimSuffix(path, ".jsonl"), func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
			d.Subagents++
		}
		return nil
	})
	return d
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// transcript valide le couple dir/id reçu du navigateur et renvoie le fichier.
func transcript(dir, id string) (string, bool) {
	if strings.ContainsAny(dir, "/\\") || dir == "" || dir == ".." || !idRe.MatchString(id) {
		return "", false
	}
	p := filepath.Join(root, dir, id+".jsonl")
	st, err := os.Stat(p)
	return p, err == nil && st.Mode().IsRegular()
}

func main() {
	home, _ := os.UserHomeDir()
	root = filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "projects")
	hosts["127.0.0.1:"+port], hosts["localhost:"+port] = true, true

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, sessions()) })
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		p, ok := transcript(r.URL.Query().Get("dir"), r.URL.Query().Get("id"))
		if !ok {
			writeJSON(w, 404, map[string]any{})
			return
		}
		writeJSON(w, 200, detail(p))
	})
	mux.HandleFunc("POST /rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, ID string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		if _, ok := transcript(req.Dir, req.ID); !ok {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "id invalide"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "bash", "-c", rmScript, "gld-web", filepath.Join(root, req.Dir), req.ID).CombinedOutput()
		if len(out) > 500 {
			out = out[len(out)-500:]
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": string(out)})
	})

	// Anti DNS-rebinding : seul le Host local est servi.
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "\narrêt.")
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	fmt.Fprintln(os.Stderr, "http://127.0.0.1:"+port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
