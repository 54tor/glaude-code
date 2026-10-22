package main

// Lecture des transcripts : sessions, titre, détail, messages.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type line struct {
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
	IsMeta      bool   `json:"isMeta"` // texte injecté par Claude Code (contenu d'un skill…), jamais tapé par l'utilisateur
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			In  int64 `json:"input_tokens"`
			Out int64 `json:"output_tokens"`
			CR  int64 `json:"cache_read_input_tokens"`
			CW  int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Tokens : consommation d'une session (entrée neuve, sortie, cache lu, cache écrit).
type Tokens struct {
	In  int64 `json:"in"`
	Out int64 `json:"out"`
	CR  int64 `json:"cr"`
	CW  int64 `json:"cw"`
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
	Provider string  `json:"provider"` // fournisseur de la session (providers.go) ; les identifiants restent propres à chacun
	ID       string  `json:"id"`
	Dir      string  `json:"dir"`
	Title    string  `json:"title"`
	Named    bool    `json:"named"`
	Cwd      string  `json:"cwd"`
	Mtime    float64 `json:"mtime"`
	KB       int64   `json:"kb"`
	Tok      Tokens  `json:"tok"`
}

// infoState : ce qu'on sait d'un transcript, et jusqu'où on l'a lu. Un transcript ne fait que grandir : à chaque
// rafraîchissement (toutes les 15 s) on ne relit que ce qui a été ajouté, au lieu de plusieurs Go.
type infoState struct {
	size              int64
	mtime             time.Time
	off               int64 // octets déjà lus, toujours en fin de ligne complète
	title, cwd, first string
	tok               Tokens    // total de la session
	lastMsg           time.Time // horodatage du dernier message user/assistant : le mtime bouge aussi pour un titre ou un résumé
	lastID            string    // un même message s'écrit sur plusieurs lignes qui répètent son usage : seul le dernier compte
	last              Tokens
}

var (
	infoMu    sync.Mutex
	infoCache = map[string]infoState{}
)

// forgetInfo : à appeler quand un transcript est réécrit sur place (le début a pu changer).
func forgetInfo(path string) {
	infoMu.Lock()
	delete(infoCache, path)
	infoMu.Unlock()
}

func scanInfo(path string, c infoState) infoState {
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer f.Close()
	if _, err := f.Seek(c.off, io.SeekStart); err != nil {
		return c
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		b, err := r.ReadBytes('\n')
		if len(b) > 0 {
			c.visit(b)
			if err == nil { // une dernière ligne sans « \n » est peut-être inachevée : on la relira
				c.off += int64(len(b))
			}
		}
		if err != nil {
			return c
		}
	}
}

func (c *infoState) visit(b []byte) {
	wantTitle := bytes.Contains(b, []byte(`"custom-title"`))
	wantCwd := c.cwd == "" && bytes.Contains(b, []byte(`"cwd"`))
	wantFirst := c.first == "" && bytes.Contains(b, []byte(`"type":"user"`))
	wantUsage := bytes.Contains(b, []byte(`"usage"`)) && bytes.Contains(b, []byte(`"type":"assistant"`))
	wantTS := bytes.Contains(b, []byte(`"timestamp"`)) && (bytes.Contains(b, []byte(`"type":"user"`)) || bytes.Contains(b, []byte(`"type":"assistant"`)))
	if !wantTitle && !wantCwd && !wantFirst && !wantUsage && !wantTS {
		return
	}
	var l line
	if json.Unmarshal(b, &l) != nil {
		return
	}
	if wantTS && (l.Type == "user" || l.Type == "assistant") {
		if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil && t.After(c.lastMsg) {
			c.lastMsg = t
		}
	}
	if wantTitle && l.CustomTitle != "" {
		c.title = l.CustomTitle
	}
	if wantCwd {
		c.cwd = l.Cwd
	}
	if wantFirst && l.Type == "user" && !l.IsMeta {
		c.first = promptOf(&l)
	}
	if u := l.Message.Usage; wantUsage && l.Type == "assistant" && u != nil {
		if l.Message.ID != "" && l.Message.ID == c.lastID {
			c.tok = Tokens{c.tok.In - c.last.In, c.tok.Out - c.last.Out, c.tok.CR - c.last.CR, c.tok.CW - c.last.CW}
		}
		c.last = Tokens{u.In, u.Out, u.CR, u.CW}
		c.lastID = l.Message.ID
		c.tok = Tokens{c.tok.In + u.In, c.tok.Out + u.Out, c.tok.CR + u.CR, c.tok.CW + u.CW}
	}
}

func info(path string, st os.FileInfo) Session {
	infoMu.Lock()
	c, ok := infoCache[path]
	infoMu.Unlock()
	if !ok || st.Size() < c.off || (c.size == st.Size() && !c.mtime.Equal(st.ModTime())) {
		c = infoState{} // inconnu, tronqué ou réécrit à taille égale : lecture complète
	}
	if c.size != st.Size() || !c.mtime.Equal(st.ModTime()) {
		c = scanInfo(path, c)
		c.size, c.mtime = st.Size(), st.ModTime()
		infoMu.Lock()
		if len(infoCache) > 5000 {
			clear(infoCache)
		}
		infoCache[path] = c
		infoMu.Unlock()
	}
	title, cwd, first := c.title, c.cwd, c.first
	mt := st.ModTime()
	if !c.lastMsg.IsZero() && c.lastMsg.Before(mt) {
		mt = c.lastMsg
	}
	s := Session{Provider: claudeID, ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Dir: filepath.Base(filepath.Dir(path)),
		Named: title != "", Cwd: cwd, Mtime: float64(mt.UnixNano()) / 1e9, KB: st.Size() / 1024, Tok: c.tok}
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

// detailCache : le détail d'un transcript ne se recalcule que s'il a changé (un transcript peut peser plusieurs Go).
var (
	detailMu    sync.Mutex
	detailCache = map[string]struct {
		size  int64
		mtime time.Time
		d     Detail
	}{}
)

func detail(path string) Detail {
	st, err := os.Stat(path)
	if err != nil {
		return Detail{Prompts: []string{}, Models: []string{}}
	}
	detailMu.Lock()
	c, ok := detailCache[path]
	detailMu.Unlock()
	if ok && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.d
	}
	d := computeDetail(path)
	detailMu.Lock()
	if len(detailCache) > 64 {
		clear(detailCache)
	}
	detailCache[path] = struct {
		size  int64
		mtime time.Time
		d     Detail
	}{st.Size(), st.ModTime(), d}
	detailMu.Unlock()
	return d
}

var (
	userTag      = []byte(`"type":"user"`)
	assistantTag = []byte(`"type":"assistant"`)
	toolResTag   = []byte(`"tool_use_id"`)
	textTag      = []byte(`"type":"text"`)
)

func computeDetail(path string) Detail {
	d := Detail{Prompts: []string{}, Models: []string{}}
	models := map[string]bool{}
	_ = scan(path, func(b []byte) {
		// Seules les lignes d'échange comptent ; un résultat d'outil (souvent énorme) n'est pas un prompt : on ne le décode pas.
		isU := bytes.Contains(b, userTag)
		if !isU && !bytes.Contains(b, assistantTag) || isU && bytes.Contains(b, toolResTag) && !bytes.Contains(b, textTag) {
			return
		}
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
	// O/N : décalage de la ligne du transcript et rang du message dans cette ligne. Clé stable : quand on
	// charge des messages plus anciens, ceux déjà affichés gardent la leur (pas de réutilisation de nœuds).
	O int64 `json:"o,omitempty"`
	N int   `json:"n,omitempty"`
	// Img : type d'une image du message (le texte est alors vide) ; K : rang du bloc dans le message, pour la relire (GET /conv-image).
	Img string `json:"img,omitempty"`
	K   int    `json:"k,omitempty"`
	// Cut/Len : texte coupé à l'octet Cut d'un texte de Len octets ; la suite se lit par morceaux (GET /conv-text).
	Cut int `json:"cut,omitempty"`
	Len int `json:"len,omitempty"`
}

type blk struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	Source  struct {
		MediaType string `json:"media_type"` // image : le contenu (data) n'est jamais chargé ici
	} `json:"source"`
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
	if bytes.Contains(b, []byte(`"isMeta":true`)) {
		return nil
	}
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
	for i, x := range bs {
		switch x.Type {
		case "image":
			if x.Source.MediaType != "" {
				out = append(out, Msg{R: l.Type, Img: x.Source.MediaType, K: i, Ts: l.Timestamp})
			}
		case "text":
			t := strings.TrimSpace(reminder.ReplaceAllString(x.Text, ""))
			if t == "" || strings.HasPrefix(t, "<command-") || strings.HasPrefix(t, "<local-command") || strings.HasPrefix(strings.ToLower(t), "caveat:") {
				continue
			}
			out = append(out, Msg{R: l.Type, T: t, Ts: l.Timestamp})
		case "tool_use":
			if tools {
				out = append(out, Msg{R: "tool", T: x.Name + "  " + toolArg(x.Input), Ts: l.Timestamp})
			}
		case "tool_result":
			if tools {
				out = append(out, Msg{R: "tool", T: "↳ " + cut(strings.TrimSpace(flat(x.Content)), 1500), Ts: l.Timestamp})
			}
		}
	}
	return out
}

// withOffset numérote les messages d'une ligne (voir Msg.O / Msg.N).
func withOffset(ms []Msg, off int64) []Msg {
	for i := range ms {
		ms[i].O, ms[i].N = off, i
	}
	return ms
}

// conv : toute la conversation d'un transcript.
func conv(path string, tools bool) []Msg {
	out := []Msg{}
	var off int64
	_ = scan(path, func(b []byte) {
		out = append(out, withOffset(lineMsgs(b, tools), off)...)
		off += int64(len(b))
	})
	return out
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
