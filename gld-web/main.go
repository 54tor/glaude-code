// gld-web — sessions Claude groupées par dossier de projet, dans le navigateur.
// Suppression native (purge.go), sans bash : fonctionne aussi sous Windows.
package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Interface React/shadcn compilée (ui/dist, versionnée : `go build` n'a pas besoin de node).
//
//go:embed all:ui/dist
var uiFS embed.FS

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
	if runtime.GOOS == "windows" { // « C:\Users\moi » s'encode « C--Users-moi » : lecteur, puis « : » et « \ » → « -- »
		for c := 'A'; c <= 'Z'; c++ {
			if prefix := string(c) + "--"; strings.HasPrefix(name, prefix) {
				if r := walk(string(c)+`:\`, strings.TrimPrefix(name, prefix)); r != "" {
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
	if runtime.GOOS == "windows" {
		return c
	}
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
	pre := func(s string) string { return strings.TrimSuffix(q(s+string(filepath.Separator)), `"`) }
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
	// Sous Windows le registre peut associer .js à text/plain, ce qui casse les modules : on fixe les types.
	for ext, typ := range map[string]string{".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".woff2": "font/woff2", ".svg": "image/svg+xml"} {
		mime.AddExtensionType(ext, typ)
	}
	dist, _ := fs.Sub(uiFS, "ui/dist")
	files := http.FileServerFS(dist)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		files.ServeHTTP(w, r)
	})
	mux.Handle("GET /assets/", files)
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
		sep := string(filepath.Separator)
		if p == "::drives" && runtime.GOOS == "windows" { // liste des lecteurs
			drives := []string{}
			for c := 'A'; c <= 'Z'; c++ {
				if _, err := os.Stat(string(c) + `:\`); err == nil {
					drives = append(drives, string(c)+`:\`)
				}
			}
			writeJSON(w, 200, map[string]any{"path": "", "parent": "", "dirs": drives, "sep": sep})
			return
		}
		if !filepath.IsAbs(p) {
			writeJSON(w, 400, map[string]any{"error": "chemin absolu requis"})
			return
		}
		p = filepath.Clean(p)
		for {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				break
			}
			up := filepath.Dir(p)
			if up == p {
				break
			}
			p = up
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
		parent := filepath.Dir(p)
		if parent == p { // racine : sous Windows on remonte à la liste des lecteurs
			parent = ""
			if runtime.GOOS == "windows" {
				parent = "::drives"
			}
		}
		writeJSON(w, 200, map[string]any{"path": p, "parent": parent, "dirs": dirs, "sep": sep})
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
		// Pas de contexte lié à la requête : un navigateur qui se déconnecte ne doit pas
		// interrompre une suppression en cours (sessions à moitié purgées).
		n, spared, notes, err := purgeSessions(items)
		msg := fmt.Sprintf("%d supprimée(s), %d vivante(s) épargnée(s)", n, spared)
		if len(notes) > 0 {
			msg += "\n" + strings.Join(notes, "\n")
		}
		if err != nil {
			msg += "\n" + err.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil && (n > 0 || spared == 0), "out": msg})
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
		if runtime.GOOS == "windows" {
			var ss []Session
			if req.ID != "" {
				ss = []Session{info(p, st)}
			} else {
				ss = recentSessions(filepath.Dir(p), 7, 50000)
			}
			n := 0
			for _, s := range ss {
				if openTerminal(cwd, s.Title, s.ID, strings.Fields(req.Args)) == nil {
					n++
				}
			}
			writeJSON(w, 200, map[string]any{"ok": n > 0, "out": fmt.Sprintf("%d onglet(s) lancé(s).", n)})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := withPath(exec.CommandContext(ctx, "gld-matin", append(args, cwd)...)).CombinedOutput()
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": string(out)})
	})

	// Anti DNS-rebinding : seul le Host local est servi. Anti CSRF : http.CrossOriginProtection
	// (bibliothèque standard) refuse toute requête modifiante venue d'une autre origine.
	guarded := http.NewCrossOriginProtection().Handler(mux)
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "forbidden", 403)
			return
		}
		guarded.ServeHTTP(w, r)
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
