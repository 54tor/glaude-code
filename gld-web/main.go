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
	"unicode/utf8"
)

// Interface React/shadcn compilée (ui/dist, versionnée : `go build` n'a pas besoin de node).
//
//go:embed all:ui/dist
var uiFS embed.FS

var (
	root  string
	port  = env("GLD_WEB_PORT", env("CC_WEB_PORT", "8799")) // CC_* : anciens noms, encore lus
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
	// Texte collé dans Claude Code : remplacé par une mention, un prompt fait seulement d'un collage ne sert pas de titre.
	s = strings.TrimSpace(pastedRe.ReplaceAllString(s, " [texte collé] "))
	if s == "[texte collé]" || strings.HasPrefix(s, "<") || strings.HasPrefix(strings.ToLower(s), "caveat:") {
		return ""
	}
	return spaceRe.ReplaceAllString(s, " ")
}

// pastedRe : un bloc collé, fermé ou non (un titre tronqué peut le couper).
var pastedRe = regexp.MustCompile(`(?s)<pasted_content id="[^"]*">.*?(</pasted_content(?: id="[^"]*")?>|$)`)

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

// lineMsgs extrait d'une ligne de transcript le texte des échanges, plus (tools) les appels d'outils et leurs résultats tronqués.
func lineMsgs(b []byte, tools bool) []Msg {
	isU := bytes.Contains(b, []byte(`"type":"user"`))
	if !isU && !bytes.Contains(b, []byte(`"type":"assistant"`)) {
		return nil
	}
	var l struct {
		Type      string
		Timestamp string
		Message   struct{ Content json.RawMessage }
	}
	if json.Unmarshal(b, &l) != nil || (l.Type != "user" && l.Type != "assistant") {
		return nil
	}
	var bs []blk
	var s string
	if json.Unmarshal(l.Message.Content, &s) == nil {
		bs = []blk{{Type: "text", Text: s}}
	} else if json.Unmarshal(l.Message.Content, &bs) != nil {
		return nil
	}
	var out []Msg
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
	return out
}

// conv : toute la conversation d'un transcript.
func conv(path string, tools bool) []Msg {
	out := []Msg{}
	_ = scan(path, func(b []byte) { out = append(out, lineMsgs(b, tools)...) })
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

// checkPrompt : un message vise une seule session, fermée (sinon deux processus écriraient le même transcript).
func checkPrompt(id, prompt string) error {
	switch {
	case prompt == "":
		return nil
	case id == "":
		return errors.New("un message ne s'envoie qu'à une session précise")
	case len(prompt) > 32<<10 || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0):
		return errors.New("message trop long ou invalide")
	}
	live, err := liveCheck()
	if err != nil {
		return err
	}
	if live[id] {
		return errOpen
	}
	return nil
}

// checkResumePrompt : comme checkPrompt, et jamais sous Windows, où le message passerait par cmd.exe
// (qui ne sait pas l'échapper sûrement).
func checkResumePrompt(id, prompt string) error {
	if prompt != "" && runtime.GOOS == "windows" {
		return errors.New("reprise avec un message non prise en charge sous Windows")
	}
	return checkPrompt(id, prompt)
}

// ids : les identifiants courts de sessions, pour le journal.
func ids(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID[:8]
	}
	return out
}

func errOr(err error) string {
	if err != nil {
		return "échec : " + err.Error()
	}
	return "ok"
}

// claudeArgs valide les options de claude (seulement des drapeaux simples : --x, -x, --x=valeur, jamais du shell)
// et les traduit en arguments de gld-matin.
func claudeArgs(raw string) ([]string, error) {
	fs := strings.Fields(raw)
	for _, f := range fs {
		if !flagRe.MatchString(f) {
			return nil, errors.New("option refusée : " + f)
		}
	}
	if len(fs) == 0 {
		return nil, nil
	}
	return []string{"--args", strings.Join(fs, " ")}, nil
}

var flagRe = regexp.MustCompile(`^--?[a-zA-Z][a-zA-Z0-9-]*(=[A-Za-z0-9._:/-]+)?$`)

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

func withPath(c *exec.Cmd) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return c
	}
	home, _ := os.UserHomeDir()
	local := filepath.Join(home, ".local/bin")
	c.Env = append(os.Environ(), "PATH="+local+":"+os.Getenv("PATH"))
	// exec.Command a déjà cherché le programme dans le PATH du serveur, où ~/.local/bin peut manquer
	// (service, lanceur de bureau) : on le reprend ici, avec la même priorité que le PATH ci-dessus.
	if name := c.Args[0]; !strings.ContainsRune(name, '/') {
		if fi, err := os.Stat(filepath.Join(local, name)); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			c.Path, c.Err = filepath.Join(local, name), nil
		}
	}
	return c
}

// matin lance gld-matin et renvoie sa sortie ; sans sortie (introuvable, tué…), l'erreur elle-même, pour qu'un échec dise pourquoi.
func matin(ctx context.Context, args ...string) (string, error) {
	out, err := withPath(exec.CommandContext(ctx, "gld-matin", args...)).CombinedOutput()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		return "gld-matin : " + err.Error(), err
	}
	return string(out), err
}

// alive : ids des sessions ouvertes, ou nil si `claude agents --json` a échoué (on ne sait pas).
func alive() map[string]bool {
	out, err := withPath(exec.Command("claude", "agents", "--json")).Output()
	var a []struct{ SessionId string }
	if err != nil || json.Unmarshal(out, &a) != nil {
		return nil // inconnu : liveCheck fait refuser les actions qui en dépendent
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x.SessionId] = true
	}
	return m
}

// waiting : ids des sessions ouvertes qui attendent (interactive « idle », en arrière-plan « blocked »).
// Une demande d'autorisation en cours n'est pas distinguée d'un tour terminé.
var waiting = func() []string {
	out, err := withPath(exec.Command("claude", "agents", "--json")).Output()
	var a []struct{ SessionId, Status, State string }
	ids := []string{}
	if err != nil || json.Unmarshal(out, &a) != nil {
		return ids
	}
	for _, x := range a {
		if x.Status == "idle" || x.State == "blocked" {
			ids = append(ids, x.SessionId)
		}
	}
	return ids
}

var cwdRe = regexp.MustCompile(`"cwd":("(?:[^"\\]|\\.)*")`)

// remapCwd : old devient nw. Ses sous-dossiers ne suivent que si sub (l'ancien dossier a disparu : projet
// renommé ou déplacé) et s'ils ne sont pas déjà dans nw — sinon ce sont de vrais chemins, laissés tels quels.
func remapCwd(v, old, nw string, sub bool) string {
	sep := string(filepath.Separator)
	if v == old {
		return nw
	}
	if sub && strings.HasPrefix(v, old+sep) && v != nw && !strings.HasPrefix(v, nw+sep) {
		return nw + v[len(old):]
	}
	return v
}

// rewriteCwd remplace le cwd (voir remapCwd) dans un transcript, en gardant la date de modification.
func rewriteCwd(path, old, nw string, sub bool) error {
	cwdKey := []byte(`"cwd":`)
	remap := func(m []byte) []byte {
		var v string
		if json.Unmarshal(m[len(cwdKey):], &v) != nil {
			return m
		}
		nv := remapCwd(v, old, nw, sub)
		if nv == v {
			return m
		}
		q, _ := json.Marshal(nv)
		return append(append([]byte{}, cwdKey...), q...)
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
		if bytes.Contains(b, cwdKey) {
			b = cwdRe.ReplaceAllFunc(b, remap)
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
	live, err := liveCheck()
	if err != nil {
		return 0, err
	}
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
				_, gone := os.Stat(old)
				files := []string{jl}
				filepath.WalkDir(filepath.Join(src, it.ID), func(p string, e os.DirEntry, err error) error {
					if err == nil && !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
						files = append(files, p)
					}
					return nil
				})
				for _, f := range files {
					if err := rewriteCwd(f, old, to, gone != nil); err != nil {
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

	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: handler()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "\narrêt.")
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	migrateData()
	go autosaveTabs(2 * time.Minute)
	fmt.Fprintln(os.Stderr, "http://127.0.0.1:"+port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// handler : toutes les routes, derrière les gardes Host et CSRF.
func handler() http.Handler {
	home, _ := os.UserHomeDir()
	mux := http.NewServeMux()
	// Sous Windows le registre peut associer .js à text/plain, ce qui casse les modules : on fixe les types.
	for ext, typ := range map[string]string{".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".woff2": "font/woff2", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".webmanifest": "application/manifest+json"} {
		mime.AddExtensionType(ext, typ)
	}
	dist, _ := fs.Sub(uiFS, "ui/dist")
	files := http.FileServerFS(dist)
	// La page de l'interface, servie aussi pour ses chemins (routage côté navigateur).
	index, _ := fs.ReadFile(dist, "index.html")
	page := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	}
	for _, p := range []string{"GET /{$}", "GET /toutes", "GET /en-cours", "GET /dossier/", "GET /groupe/", "GET /snippets", "GET /snippets/", "GET /memoire", "GET /memoire/", "GET /onglets"} {
		mux.HandleFunc(p, page)
	}
	mux.Handle("GET /assets/", files)
	// Icônes et manifeste (ui/public) : favicon, icônes d'application.
	mux.Handle("GET /icons/", files)
	mux.Handle("GET /favicon.ico", files)
	mux.Handle("GET /manifest.webmanifest", files)
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		live := []string{}
		for id := range liveSessions() {
			live = append(live, id)
		}
		groupsMu.Lock()
		st := loadStore()
		groupsMu.Unlock()
		writeJSON(w, 200, map[string]any{"home": home, "root": root, "groups": sessions(), "live": live, "waiting": waiting(), "vgroups": st.Groups, "favs": st.Favs})
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
		journal("rm %d session(s) : %d supprimée(s), %d épargnée(s) %v", len(items), n, spared, ids(items))
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
		journal("reassign %v vers %s : %v", ids(items), to, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": fmt.Sprintf("%d session(s) réassignée(s) vers %s", n, to)})
	})
	// Recherche dans le texte des conversations (au moins 2 caractères).
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if n := utf8.RuneCountInString(strings.TrimSpace(q)); n < 2 || n > 200 {
			writeJSON(w, 400, map[string]any{"hits": []Hit{}})
			return
		}
		writeJSON(w, 200, map[string]any{"hits": search(q)})
	})
	mux.HandleFunc("POST /rename", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, ID, Title string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		p, ok := transcript(req.Dir, req.ID)
		title := strings.TrimSpace(spaceRe.ReplaceAllString(req.Title, " "))
		if !ok || title == "" || utf8.RuneCountInString(title) > 200 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "titre ou session invalide"})
			return
		}
		// Une session ouverte réécrit son propre titre : le nôtre serait écrasé.
		live, lerr := liveCheck()
		if lerr != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": lerr.Error()})
			return
		}
		if live[req.ID] {
			writeJSON(w, 200, map[string]any{"ok": false, "out": "session ouverte : renomme-la avec /rename dans Claude, ou ferme-la d'abord"})
			return
		}
		err := setTitle(p, req.ID, title)
		journal("rename %s « %s » : %v", req.ID, title, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Renommée : " + title})
	})
	// Snippets = commandes Claude Code (commands.go) : personnelles (~/.claude/commands, ou GLD_WEB_SNIPPETS) et de projet.
	snipDir = env("GLD_WEB_SNIPPETS", os.Getenv("CC_WEB_SNIPPETS"))
	mux.HandleFunc("GET /snippet-list", func(w http.ResponseWriter, r *http.Request) {
		l, scopes := listSnippets()
		writeJSON(w, 200, map[string]any{"dir": userCmdDir(), "files": l, "scopes": scopes})
	})
	mux.HandleFunc("GET /snippet", func(w http.ResponseWriter, r *http.Request) {
		t, err := readSnippet(r.URL.Query().Get("key"))
		if errors.Is(err, os.ErrNotExist) { // nouveau snippet pas encore enregistré
			writeJSON(w, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"text": t})
	})
	mux.HandleFunc("POST /snippet-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Text string
			Create    bool // nouveau snippet : refusé s'il existe déjà
		}
		if json.NewDecoder(io.LimitReader(r.Body, snippetMax+1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		err := saveSnippet(req.Key, req.Text, req.Create)
		journal("snippet-save %s (%d octets) : %v", req.Key, len(req.Text), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Snippet enregistré"})
	})
	mux.HandleFunc("POST /snippet-draft", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model, Instruction, Text string }
		if json.NewDecoder(io.LimitReader(r.Body, snippetMax+16<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		text, cost, err := snippetDraft(req.Model, req.Instruction, req.Text)
		journal("snippet-draft %s (%d caractères) : %v (%.4f $)", req.Model, len(req.Text), errOr(err), cost)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Mise en forme prête", "text": text, "cost": cost})
	})
	mux.HandleFunc("POST /snippet-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		err := removeSnippet(req.Key)
		journal("snippet-rm %s (copie à la corbeille) : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Snippet supprimé"})
	})
	// Export : les snippets d'un scope en archive zip ; import : fichiers .md ou archives .zip (multipart).
	mux.HandleFunc("GET /snippets-export", func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		if scope != "" && !safeDir(scope) {
			http.Error(w, "scope invalide", 400)
			return
		}
		b, n, err := exportSnippets(scope)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		name := "commandes-personnelles.zip"
		if scope != "" {
			name = "commandes" + scope + ".zip"
		}
		journal("snippets-export %q : %d fichier(s)", scope, n)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Write(b)
	})
	mux.HandleFunc("POST /snippets-import", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		if r.ParseMultipartForm(16<<20) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "envoi invalide ou trop gros (16 Mo au plus)"})
			return
		}
		scope := r.FormValue("scope")
		if scope != "" && !safeDir(scope) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "scope invalide"})
			return
		}
		var files []ImportFile
		for _, fh := range r.MultipartForm.File["files"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(f)
			f.Close()
			files = append(files, ImportFile{fh.Filename, b})
		}
		done, skipped, err := importSnippets(scope, files, r.FormValue("overwrite") == "1")
		journal("snippets-import %q : %d importé(s), %d déjà présent(s) : %v", scope, len(done), len(skipped), errOr(err))
		out := fmt.Sprintf("%d importé(s)", len(done))
		if len(skipped) > 0 {
			out += fmt.Sprintf(", %d déjà présent(s), laissé(s) tel(s) quel(s) : %s", len(skipped), strings.Join(skipped, ", "))
		}
		if err != nil {
			out += "\n" + err.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
	})
	// Mémoire : liste, lecture, enregistrement et suppression des seuls fichiers reconnus (memory.go).
	mux.HandleFunc("GET /memory-list", func(w http.ResponseWriter, r *http.Request) {
		g, p, t := memList()
		writeJSON(w, 200, map[string]any{"global": g, "projects": p, "targets": t})
	})
	mux.HandleFunc("GET /memory-file", func(w http.ResponseWriter, r *http.Request) {
		t, err := memRead(r.URL.Query().Get("key"))
		if errors.Is(err, os.ErrNotExist) { // nouvelle entrée pas encore enregistrée : pas une erreur
			writeJSON(w, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"text": t})
	})
	mux.HandleFunc("POST /memory-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Text string
			Index     bool // nouvelle entrée : l'ajouter aussi à MEMORY.md
		}
		if json.NewDecoder(io.LimitReader(r.Body, memMax+1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		err := memSave(req.Key, req.Text)
		if err == nil && req.Index {
			err = memIndex(req.Key, req.Text)
		}
		journal("memory-save %s (%d octets) : %v", req.Key, len(req.Text), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Enregistré"})
	})
	mux.HandleFunc("POST /memory-move", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, To string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		dst, err := memMove(req.Key, req.To)
		journal("memory-move %s vers %s : %v", req.Key, dst, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Déplacé", "key": dst})
	})
	// Brouillon rédigé par un petit modèle (memwrite.go) : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /memory-draft", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		text, cost, err := memWriteDraft(req.Model, req.Instruction, req.Key, req.Refs)
		journal("memory-draft %s %q réf=%v : %v (%.4f $)", req.Model, req.Key, req.Refs, errOr(err), cost)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Brouillon prêt", "text": text, "cost": cost})
	})
	mux.HandleFunc("POST /memory-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		err := memRemove(req.Key)
		journal("memory-rm %s (copie à la corbeille) : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Supprimé"})
	})
	// Sessions ouvertes sauvegardées (tabs.go) : liste, sauvegarde manuelle (nom facultatif), suppression.
	mux.HandleFunc("GET /tabs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"saves": loadTabs()})
	})
	mux.HandleFunc("POST /tabs-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		s, _, err := saveTabs(req.Name, false)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": fmt.Sprintf("%d session(s) sauvegardée(s)", len(s.Sessions))})
	})
	mux.HandleFunc("POST /tabs-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ At string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		if err := removeTabs(req.At); err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Sauvegarde supprimée"})
	})
	// Fusion de fichiers de mémoire par Claude : le prompt est construit ici, à partir des seules clés validées ;
	// launch l'ouvre dans une nouvelle session (gld-matin --prompt), sinon il est seulement renvoyé pour être copié.
	mux.HandleFunc("POST /memory-merge", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys   []string
			Launch bool
			Args   string
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		prompt, cwd, err := mergePrompt(req.Keys)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		if !req.Launch {
			writeJSON(w, 200, map[string]any{"ok": true, "out": "Prompt copié", "prompt": prompt})
			return
		}
		args, err := claudeArgs(req.Args)
		if err == nil && runtime.GOOS == "windows" {
			err = errors.New("lancement non pris en charge sous Windows : copie le prompt")
		}
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error(), "prompt": prompt})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		out, err := matin(ctx, append(args, "--prompt", prompt, cwd)...)
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out, "prompt": prompt})
	})
	// Conversation dans le navigateur (chat.go).
	mux.HandleFunc("POST /chat", chatHandler)
	mux.HandleFunc("POST /chat-stop", chatStopHandler)
	mux.HandleFunc("POST /new-session", newSessionHandler)
	// Corbeille de la zone Mémoire (trash.go).
	mux.HandleFunc("GET /trash", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": trashList()})
	})
	mux.HandleFunc("GET /trash-file", func(w http.ResponseWriter, r *http.Request) {
		_, text, err := trashItem(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("POST /trash-restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		t, err := trashRestore(req.ID)
		journal("trash-restore %s → %s : %v", req.ID, t.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Restauré : " + t.Path, "key": t.Key})
	})
	mux.HandleFunc("POST /trash-purge", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID  string
			All bool // vider toute la corbeille : jamais implicite
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		if req.ID == "" && !req.All {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "précise un élément, ou all: true pour tout vider"})
			return
		}
		err := trashPurge(req.ID)
		journal("trash-purge %q : %v", req.ID, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Corbeille vidée"})
	})
	// Groupes virtuels et favoris : le navigateur envoie l'ensemble, le serveur le valide et le remplace.
	mux.HandleFunc("POST /vgroups", func(w http.ResponseWriter, r *http.Request) {
		var st Store
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&st) != nil || st.Groups == nil || st.Favs == nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		groupsMu.Lock()
		err := saveStore(st)
		groupsMu.Unlock()
		journal("vgroups %d groupe(s), %d favori(s) : %v", len(st.Groups), len(st.Favs), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Groupes enregistrés"})
	})
	// Rouvre dans un onglet de terminal via gld-matin : une session (id) ou les récentes du projet.
	// Le cwd vient du transcript côté serveur, jamais du navigateur.
	mux.HandleFunc("POST /open", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, ID, Args, Prompt string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		var p string
		args, err := claudeArgs(req.Args)
		if err == nil {
			err = checkResumePrompt(req.ID, req.Prompt)
		}
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		if req.ID != "" {
			var ok bool
			if p, ok = transcript(req.Dir, req.ID); !ok {
				writeJSON(w, 400, map[string]any{"ok": false, "out": "id invalide"})
				return
			}
			args = append(args, "--id", req.ID)
			if req.Prompt != "" { // reprise avec un message : transmis tel quel, gld-matin l'échappe pour le shell
				args = append(args, "--prompt", req.Prompt)
			}
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
		out, err := matin(ctx, append(args, cwd)...)
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
	})

	// Anti DNS-rebinding : seul le Host local est servi. Anti CSRF : http.CrossOriginProtection
	// (bibliothèque standard) refuse toute requête modifiante venue d'une autre origine.
	guarded := http.NewCrossOriginProtection().Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		guarded.ServeHTTP(w, r)
	})
}
