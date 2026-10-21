// gld-web — sessions Claude groupées par dossier de projet, dans le navigateur.
// Suppression via gld-lib.sh (même séquence que gld-rm, qui refuse les sessions vivantes).
package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
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

//go:embed style.css
var style []byte

// Bibliothèques servies depuis le binaire (la CSP interdit tout CDN). Licences dans vendor/.
//
//go:embed vendor/marked.js vendor/purify.js
var vendor embed.FS

// Même séquence que gld-rm, sans resoudre_projet (qui exige que le cwd existe encore).
// Arguments : des chemins « <dossier de transcripts>/<id> ». Un seul solder pour tout le lot.
// Les sessions vivantes sont épargnées.
const rmScript = `. "$HOME/.local/bin/gld-lib.sh"; cc_tmp
viv=$(sessions_vivantes); n=0; k=0
: > "$GLD_TMP/snapshots"; : > "$GLD_TMP/supprimees"
for a in "$@"; do
  d="${a%/*}"; id="${a##*/}"
  if printf '%s\n' "$viv" | grep -Fq -- "$id"; then k=$((k+1)); continue; fi
  snapshots_cites "$d/$id.jsonl" "$d/$id" >> "$GLD_TMP/snapshots"
  purger_session "$d" "$id" >/dev/null; echo "$id" >> "$GLD_TMP/supprimees"; n=$((n+1))
done
[ "$n" -gt 0 ] && solder "$GLD_TMP/supprimees" "$GLD_TMP/snapshots"
echo "$n supprimée(s), $k vivante(s) épargnée(s)"
[ "$n" -gt 0 ] || [ "$k" = 0 ] || exit 1`

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
	Missing  bool      `json:"missing"` // le dossier du projet n'existe plus (ou cwd inconnu)
	Sessions []Session `json:"sessions"`
}

// decodeDir retrouve le chemin d'un dossier de transcripts (/ . _ espace… → « - », donc lossy)
// en descendant l'arborescence réelle : seuls les dossiers dont le nom encodé colle sont suivis.
func decodeDir(name string) string {
	var walk func(p, rest string) string
	walk = func(p, rest string) string {
		ents, err := os.ReadDir(p)
		if err != nil {
			return ""
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			enc := nonAlnum.ReplaceAllString(e.Name(), "-")
			next := filepath.Join(p, e.Name())
			if rest == enc {
				return next
			}
			if strings.HasPrefix(rest, enc+"-") {
				if r := walk(next, rest[len(enc)+1:]); r != "" {
					return r
				}
			}
		}
		return ""
	}
	return walk("/", strings.TrimPrefix(name, "-"))
}

func sessions() []Group {
	groups := map[string][]Session{}
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	var all []Session
	dirCwd := map[string]string{} // cwd connu d'un dossier de transcripts, via l'une de ses sessions
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil || st.Size() == 0 {
			continue
		}
		s := info(p, st)
		if s.Cwd != "" && dirCwd[s.Dir] == "" {
			dirCwd[s.Dir] = s.Cwd
		}
		all = append(all, s)
	}
	for _, s := range all {
		if s.Cwd == "" { // session sans cwd : on la range avec ses voisines, sinon par décodage du nom du dossier
			c, seen := dirCwd[s.Dir]
			if !seen {
				c = decodeDir(s.Dir)
				dirCwd[s.Dir] = c
			}
			s.Cwd = c
		}
		k := s.Cwd
		if k == "" {
			k = s.Dir
		}
		groups[k] = append(groups[k], s)
	}
	out := []Group{}
	for k, v := range groups {
		sort.Slice(v, func(i, j int) bool { return v[i].Mtime > v[j].Mtime })
		fi, err := os.Stat(k)
		out = append(out, Group{k, err != nil || !fi.IsDir(), v})
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

// --- lecture d'une conversation ---------------------------------------------

type Msg struct {
	R  string `json:"r"` // user | assistant | tool
	T  string `json:"t"`
	Ts string `json:"ts,omitempty"`
}

type blk struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
}

var reminder = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// toolArg résume l'entrée d'un appel d'outil : la clé qui parle le plus, sinon le JSON compact.
func toolArg(in json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(in, &m) == nil {
		for _, k := range []string{"command", "file_path", "pattern", "path", "url", "query", "description", "prompt"} {
			if v, ok := m[k].(string); ok && v != "" {
				return cut(v, 300)
			}
		}
	}
	return cut(string(in), 300)
}

// flat aplatit le contenu d'un tool_result (texte ou liste de blocs).
func flat(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var bs []blk
	var t []string
	if json.Unmarshal(c, &bs) == nil {
		for _, b := range bs {
			t = append(t, b.Text)
		}
	}
	return strings.Join(t, "\n")
}

// conv extrait la conversation : texte des échanges, plus (tools) les appels d'outils et leurs résultats tronqués.
func conv(path string, tools bool) []Msg {
	out := []Msg{}
	_ = scan(path, func(b []byte) {
		isU := bytes.Contains(b, []byte(`"type":"user"`))
		if !isU && !bytes.Contains(b, []byte(`"type":"assistant"`)) {
			return
		}
		var l struct {
			Type      string
			Timestamp string
			Message   struct{ Content json.RawMessage }
		}
		if json.Unmarshal(b, &l) != nil || (l.Type != "user" && l.Type != "assistant") {
			return
		}
		var bs []blk
		var s string
		if json.Unmarshal(l.Message.Content, &s) == nil {
			bs = []blk{{Type: "text", Text: s}}
		} else if json.Unmarshal(l.Message.Content, &bs) != nil {
			return
		}
		for _, x := range bs {
			switch x.Type {
			case "text":
				t := strings.TrimSpace(reminder.ReplaceAllString(x.Text, ""))
				if t == "" || strings.HasPrefix(t, "<command-") || strings.HasPrefix(t, "<local-command") || strings.HasPrefix(strings.ToLower(t), "caveat:") {
					continue
				}
				out = append(out, Msg{l.Type, t, l.Timestamp})
			case "tool_use":
				if tools {
					out = append(out, Msg{"tool", x.Name + "  " + toolArg(x.Input), l.Timestamp})
				}
			case "tool_result":
				if tools {
					out = append(out, Msg{"tool", "↳ " + cut(strings.TrimSpace(flat(x.Content)), 1500), l.Timestamp})
				}
			}
		}
	})
	return out
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

type item struct{ Dir, ID string }

// readItems décode {items:[{dir,id}], to} et n'accepte que des sessions qui existent vraiment.
func readItems(w http.ResponseWriter, r *http.Request) ([]item, string, bool) {
	var req struct {
		Items []item
		To    string
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<22)).Decode(&req) != nil || len(req.Items) == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
		return nil, "", false
	}
	for _, it := range req.Items {
		if _, ok := transcript(it.Dir, it.ID); !ok {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "id invalide"})
			return nil, "", false
		}
	}
	return req.Items, req.To, true
}

var flagRe = regexp.MustCompile(`^--?[a-zA-Z][a-zA-Z0-9-]*(=[A-Za-z0-9._:/-]+)?$`)

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

func withPath(c *exec.Cmd) *exec.Cmd {
	home, _ := os.UserHomeDir()
	c.Env = append(os.Environ(), "PATH="+filepath.Join(home, ".local/bin")+":"+os.Getenv("PATH"))
	return c
}

func alive() map[string]bool {
	out, _ := withPath(exec.Command("claude", "agents", "--json")).Output()
	var a []struct{ SessionId string }
	json.Unmarshal(out, &a)
	m := map[string]bool{}
	for _, x := range a {
		m[x.SessionId] = true
	}
	return m
}

// rewriteCwd remplace le cwd (et ses sous-dossiers) dans un transcript, en gardant la date de modification.
func rewriteCwd(path, old, nw string) error {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	pre := func(s string) string { return strings.TrimSuffix(q(s+"/"), `"`) }
	reps := [][2][]byte{
		{[]byte(`"cwd":` + q(old)), []byte(`"cwd":` + q(nw))},
		{[]byte(`"cwd":` + pre(old)), []byte(`"cwd":` + pre(nw))},
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode())
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(out, 1<<20)
	r := bufio.NewReaderSize(in, 1<<20)
	for {
		b, rerr := r.ReadBytes('\n')
		for _, p := range reps {
			b = bytes.ReplaceAll(b, p[0], p[1])
		}
		w.Write(b)
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				err = rerr
			}
			break
		}
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, path); err == nil {
		err = os.Chtimes(path, st.ModTime(), st.ModTime())
	}
	return err
}

// reassign déplace des sessions vers le dossier de transcripts de `to` (un dossier existant)
// et y réécrit leur cwd. Tout est vérifié avant le premier déplacement.
func reassign(items []item, to string) (int, error) {
	if !filepath.IsAbs(to) {
		return 0, errors.New("chemin absolu requis")
	}
	to = filepath.Clean(to)
	if fi, err := os.Stat(to); err != nil || !fi.IsDir() {
		return 0, errors.New("le dossier cible n'existe pas : " + to)
	}
	dst := filepath.Join(root, nonAlnum.ReplaceAllString(to, "-"))
	live := alive()
	for _, it := range items {
		if live[it.ID] {
			return 0, errors.New("session vivante, ferme-la d'abord : " + it.ID[:8])
		}
		if filepath.Join(root, it.Dir) == dst {
			return 0, errors.New("déjà dans ce dossier : " + it.ID[:8])
		}
		for _, n := range []string{it.ID + ".jsonl", it.ID} {
			if _, err := os.Lstat(filepath.Join(dst, n)); err == nil {
				return 0, errors.New("existe déjà dans la cible : " + n)
			}
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	for _, it := range items {
		src := filepath.Join(root, it.Dir)
		jl := filepath.Join(src, it.ID+".jsonl")
		if st, err := os.Stat(jl); err == nil {
			if old := info(jl, st).Cwd; old != "" && old != to {
				files := []string{jl}
				filepath.WalkDir(filepath.Join(src, it.ID), func(p string, e os.DirEntry, err error) error {
					if err == nil && !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
						files = append(files, p)
					}
					return nil
				})
				for _, f := range files {
					if err := rewriteCwd(f, old, to); err != nil {
						return 0, err
					}
				}
			}
		}
		for _, n := range []string{it.ID + ".jsonl", it.ID} {
			if err := os.Rename(filepath.Join(src, n), filepath.Join(dst, n)); err != nil && !os.IsNotExist(err) {
				return 0, err
			}
		}
		os.Remove(src) // seulement s'il est devenu vide
	}
	return len(items), nil
}

func main() {
	home, _ := os.UserHomeDir()
	root = filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "projects")
	hosts["127.0.0.1:"+port], hosts["localhost:"+port] = true, true

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self'; frame-ancestors 'none'")
		w.Write(page)
	})
	mux.HandleFunc("GET /vendor/{f}", func(w http.ResponseWriter, r *http.Request) {
		b, err := vendor.ReadFile("vendor/" + r.PathValue("f")) // embed.FS : jamais hors de vendor/
		if err != nil || !strings.HasSuffix(r.PathValue("f"), ".js") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(b)
	})
	mux.HandleFunc("GET /style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(style)
	})
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"home": home, "groups": sessions()})
	})
	// Explorateur de dossiers pour « Réassigner » / « Déplacer » : sous-dossiers d'un chemin.
	// Un chemin disparu remonte au plus proche parent qui existe.
	mux.HandleFunc("GET /ls", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		if p == "" {
			p = home
		}
		if !filepath.IsAbs(p) {
			writeJSON(w, 400, map[string]any{"error": "chemin absolu requis"})
			return
		}
		p = filepath.Clean(p)
		for p != "/" {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				break
			}
			p = filepath.Dir(p)
		}
		ents, err := os.ReadDir(p)
		if err != nil {
			writeJSON(w, 403, map[string]any{"error": err.Error()})
			return
		}
		dirs := []string{}
		for _, e := range ents {
			isDir := e.IsDir()
			if !isDir && e.Type()&os.ModeSymlink != 0 { // lien : on ne suit que ceux qui mènent à un dossier
				fi, err := os.Stat(filepath.Join(p, e.Name()))
				isDir = err == nil && fi.IsDir()
			}
			if isDir {
				dirs = append(dirs, e.Name())
			}
		}
		sort.Strings(dirs)
		parent := ""
		if p != "/" {
			parent = filepath.Dir(p)
		}
		writeJSON(w, 200, map[string]any{"path": p, "parent": parent, "dirs": dirs})
	})
	mux.HandleFunc("GET /conv", func(w http.ResponseWriter, r *http.Request) {
		p, ok := transcript(r.URL.Query().Get("dir"), r.URL.Query().Get("id"))
		if !ok {
			writeJSON(w, 404, map[string]any{})
			return
		}
		writeJSON(w, 200, map[string]any{"msgs": conv(p, r.URL.Query().Get("tools") == "1")})
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		p, ok := transcript(r.URL.Query().Get("dir"), r.URL.Query().Get("id"))
		if !ok {
			writeJSON(w, 404, map[string]any{})
			return
		}
		writeJSON(w, 200, detail(p))
	})
	mux.HandleFunc("POST /rm", func(w http.ResponseWriter, r *http.Request) {
		items, _, ok := readItems(w, r)
		if !ok {
			return
		}
		args := []string{"-c", rmScript, "gld-web"}
		for _, it := range items {
			args = append(args, filepath.Join(root, it.Dir, it.ID))
		}
		// Contexte détaché de la requête : un navigateur qui se déconnecte ne doit pas
		// interrompre une suppression en cours (sessions à moitié purgées).
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, "bash", args...).CombinedOutput()
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": strings.TrimSpace(string(out))})
	})
	mux.HandleFunc("POST /reassign", func(w http.ResponseWriter, r *http.Request) {
		items, to, ok := readItems(w, r)
		if !ok {
			return
		}
		n, err := reassign(items, to)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": fmt.Sprintf("%d session(s) réassignée(s) vers %s", n, to)})
	})
	// Rouvre dans un onglet de terminal via gld-matin : une session (id) ou les récentes du projet.
	// Le cwd vient du transcript côté serveur, jamais du navigateur.
	mux.HandleFunc("POST /open", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, ID, Args string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		var args []string
		var p string
		// Options de claude : seulement des drapeaux simples (--x, -x, --x=valeur), jamais du shell.
		for _, f := range strings.Fields(req.Args) {
			if !flagRe.MatchString(f) {
				writeJSON(w, 400, map[string]any{"ok": false, "out": "option refusée : " + f})
				return
			}
		}
		if a := strings.Join(strings.Fields(req.Args), " "); a != "" {
			args = append(args, "--args", a)
		}
		if req.ID != "" {
			var ok bool
			if p, ok = transcript(req.Dir, req.ID); !ok {
				writeJSON(w, 400, map[string]any{"ok": false, "out": "id invalide"})
				return
			}
			args = append(args, "--id", req.ID)
		} else if strings.ContainsAny(req.Dir, "/\\") || req.Dir == "" || req.Dir == ".." {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "dossier invalide"})
			return
		} else if fs, _ := filepath.Glob(filepath.Join(root, req.Dir, "*.jsonl")); len(fs) > 0 {
			p = fs[0]
		}
		st, err := os.Stat(p)
		if err != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "out": "aucune session"})
			return
		}
		cwd := info(p, st).Cwd
		if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
			writeJSON(w, 200, map[string]any{"ok": false, "out": "dossier du projet introuvable : " + cwd})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := withPath(exec.CommandContext(ctx, "gld-matin", append(args, cwd)...)).CombinedOutput()
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": string(out)})
	})

	// Anti DNS-rebinding : seul le Host local est servi.
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "forbidden", 403)
			return
		}
		// Anti CSRF : un POST doit être du JSON (déclenche un preflight CORS, jamais accordé)
		// et, s'il porte un Origin, venir de notre propre page.
		if r.Method == http.MethodPost {
			if r.Header.Get("Content-Type") != "application/json" ||
				(r.Header.Get("Origin") != "" && !hosts[strings.TrimPrefix(r.Header.Get("Origin"), "http://")]) {
				http.Error(w, "forbidden", 403)
				return
			}
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
