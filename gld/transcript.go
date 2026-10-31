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
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	sessionTitleMaxRunes  = 100
	previewMaxRunes       = 300
	toolResultMaxRunes    = 1500
	recentPromptCount     = 3
	decodeCacheMax        = 5000
	detailCacheMax        = 64
	sessionIDPrefixLength = 8
)

type line struct {
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
	IsMeta      bool   `json:"isMeta"` // texte injecté par Claude Code (contenu d'un skill…), jamais tapé par l'utilisateur
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	Timestamp   string `json:"timestamp"`
	TeamName    string `json:"teamName"`
	AgentName   string `json:"agentName"`
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
func promptOf(transcriptLine *line) string {
	var promptText string
	var parts []struct{ Text string }
	content := transcriptLine.Message.Content
	if json.Unmarshal(content, &promptText) != nil && json.Unmarshal(content, &parts) == nil {
		var textParts []string
		for _, part := range parts {
			textParts = append(textParts, part.Text)
		}
		promptText = strings.Join(textParts, " ")
	}
	// Texte collé dans Claude Code : remplacé par une mention, un prompt fait seulement d'un collage ne sert pas de titre.
	pasted := L("srv.transcript.pasted")
	promptText = strings.TrimSpace(pastedRe.ReplaceAllString(promptText, " "+pasted+" "))
	isPastedOnly := promptText == pasted
	isMarkup := strings.HasPrefix(promptText, "<")
	isCaveat := strings.HasPrefix(strings.ToLower(promptText), "caveat:")
	if isPastedOnly || isMarkup || isCaveat {
		return ""
	}
	return spaceRe.ReplaceAllString(promptText, " ")
}

// pastedRe : un bloc collé, fermé ou non (un titre tronqué peut le couper).
var pastedRe = regexp.MustCompile(`(?s)<pasted_content id="[^"]*">.*?(</pasted_content(?: id="[^"]*")?>|$)`)

// scan appelle visitLine pour chaque ligne du transcript (les lignes peuvent être énormes).
func scan(path string, visitLine func([]byte)) error {
	file, openErr := openTrusted(path)
	if openErr != nil {
		return openErr
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, jsonLineBufferSize)
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			visitLine(lineBytes)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func cut(text string, maxRunes int) string {
	if runes := []rune(text); len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return text
}

type Session struct {
	Provider string  `json:"provider"` // fournisseur de la session (providers.go) ; les identifiants restent propres à chacun
	ID       string  `json:"id"`
	Dir      string  `json:"dir"`
	Title    string  `json:"title"`
	Named    bool    `json:"named"`
	Cwd      string  `json:"cwd"`
	Mtime    float64 `json:"mtime"`
	Ask      float64 `json:"ask,omitempty"` // dernier message de l'utilisateur : clé de tri des listes (le mtime bouge aussi à chaque réponse)
	KB       int64   `json:"kb"`
	Tok      Tokens  `json:"tok"`
	Parent   string  `json:"parent,omitempty"` // session qui a lancé celle-ci (sous-agent d'un plan) ; vide si inconnue ou disparue
	team     string  // « session-<8 premiers caractères de l'ID parent> », résolu en Parent par sessions()
}

// infoState : ce qu'on sait d'un transcript, et jusqu'où on l'a lu. Un transcript ne fait que grandir : à chaque
// rafraîchissement (toutes les 15 s) on ne relit que ce qui a été ajouté, au lieu de plusieurs Go.
type infoState struct {
	size              int64
	mtime             time.Time
	off               int64 // octets déjà lus, toujours en fin de ligne complète
	title, cwd, first string
	team, agent       string    // sessions lancées par une équipe : le parent et le nom du rôle (aucun titre n'est écrit pour elles)
	tok               Tokens    // total de la session
	lastMsg           time.Time // horodatage du dernier message user/assistant : le mtime bouge aussi pour un titre ou un résumé
	lastAsk           time.Time // horodatage du dernier vrai message utilisateur
	lastID            string    // un même message s'écrit sur plusieurs lignes qui répètent son usage : seul le dernier compte
	last              Tokens
}

var (
	infoMu    sync.Mutex
	infoCache = map[string]infoState{}
	// Au-delà, les entrées des fichiers disparus sont évincées plutôt que tout vider : vider forçait la relecture complète
	// de chaque transcript à chaque passage dès 5 000 sessions.
	infoCacheMax = 50_000
)

// forgetInfo : à appeler quand un transcript est réécrit sur place (le début a pu changer).
func forgetInfo(path string) {
	infoMu.Lock()
	delete(infoCache, path)
	infoMu.Unlock()
}

func scanInfo(path string, state infoState) infoState {
	file, openErr := openTrusted(path)
	if openErr != nil {
		return state
	}
	defer file.Close()
	if _, seekErr := file.Seek(state.off, io.SeekStart); seekErr != nil {
		return state
	}
	reader := bufio.NewReaderSize(file, jsonLineBufferSize)
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			state.visit(lineBytes)
			if readErr == nil { // une dernière ligne sans « \n » est peut-être inachevée : on la relira
				state.off += int64(len(lineBytes))
			}
		}
		if readErr != nil {
			return state
		}
	}
}

func (state *infoState) visit(lineBytes []byte) {
	wantTitle := bytes.Contains(lineBytes, []byte(`"custom-title"`))
	wantCwd := state.cwd == "" && bytes.Contains(lineBytes, []byte(`"cwd"`))
	wantTeam := state.team == "" && bytes.Contains(lineBytes, []byte(`"teamName"`))
	wantFirst := state.first == "" && bytes.Contains(lineBytes, []byte(`"type":"user"`))
	wantUsage := bytes.Contains(lineBytes, []byte(`"usage"`)) && bytes.Contains(lineBytes, []byte(`"type":"assistant"`))
	wantTimestamp := bytes.Contains(lineBytes, []byte(`"timestamp"`)) && (bytes.Contains(lineBytes, []byte(`"type":"user"`)) || bytes.Contains(lineBytes, []byte(`"type":"assistant"`)))
	if !wantTitle && !wantCwd && !wantTeam && !wantFirst && !wantUsage && !wantTimestamp {
		return
	}
	var parsedLine line
	if json.Unmarshal(lineBytes, &parsedLine) != nil {
		return
	}
	if wantTimestamp && (parsedLine.Type == "user" || parsedLine.Type == "assistant") {
		if messageTime, err := time.Parse(time.RFC3339Nano, parsedLine.Timestamp); err == nil && messageTime.After(state.lastMsg) {
			state.lastMsg = messageTime
		}
		if messageTime, err := time.Parse(time.RFC3339Nano, parsedLine.Timestamp); err == nil && parsedLine.Type == "user" && !parsedLine.IsMeta && messageTime.After(state.lastAsk) && promptOf(&parsedLine) != "" {
			state.lastAsk = messageTime
		}
	}
	if wantTitle && parsedLine.CustomTitle != "" {
		state.title = parsedLine.CustomTitle
	}
	if wantCwd {
		state.cwd = parsedLine.Cwd
	}
	if wantTeam {
		state.team, state.agent = parsedLine.TeamName, parsedLine.AgentName
	}
	if wantFirst && parsedLine.Type == "user" && !parsedLine.IsMeta {
		state.first = promptOf(&parsedLine)
	}
	if messageUsage := parsedLine.Message.Usage; wantUsage && parsedLine.Type == "assistant" && messageUsage != nil {
		if parsedLine.Message.ID != "" && parsedLine.Message.ID == state.lastID {
			state.tok = Tokens{state.tok.In - state.last.In, state.tok.Out - state.last.Out, state.tok.CR - state.last.CR, state.tok.CW - state.last.CW}
		}
		state.last = Tokens{messageUsage.In, messageUsage.Out, messageUsage.CR, messageUsage.CW}
		state.lastID = parsedLine.Message.ID
		state.tok = Tokens{state.tok.In + messageUsage.In, state.tok.Out + messageUsage.Out, state.tok.CR + messageUsage.CR, state.tok.CW + messageUsage.CW}
	}
}

func info(path string, fileInfo os.FileInfo) Session {
	infoMu.Lock()
	state, cached := infoCache[path]
	infoMu.Unlock()
	isTruncated := fileInfo.Size() < state.off
	isRewrittenSameSize := state.size == fileInfo.Size() && !state.mtime.Equal(fileInfo.ModTime())
	if !cached || isTruncated || isRewrittenSameSize {
		state = infoState{} // inconnu, tronqué ou réécrit à taille égale : lecture complète
	}
	hasChanged := state.size != fileInfo.Size() || !state.mtime.Equal(fileInfo.ModTime())
	if hasChanged {
		state = scanInfo(path, state)
		state.size, state.mtime = fileInfo.Size(), fileInfo.ModTime()
		infoMu.Lock()
		if len(infoCache) > infoCacheMax { // on ne garde que les transcripts qui existent encore : une entrée pèse quelques centaines d'octets
			for cachedPath := range infoCache {
				if _, err := os.Stat(cachedPath); err != nil {
					delete(infoCache, cachedPath)
				}
			}
			if len(infoCache) > infoCacheMax {
				clear(infoCache)
			}
		}
		infoCache[path] = state
		infoMu.Unlock()
	}
	title, cwd, first := state.title, state.cwd, state.first
	modTime := fileInfo.ModTime()
	if !state.lastMsg.IsZero() && state.lastMsg.Before(modTime) {
		modTime = state.lastMsg
	}
	session := Session{Provider: claudeID, ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Dir: filepath.Base(filepath.Dir(path)),
		Named: title != "", Cwd: cwd, team: state.team, Mtime: float64(modTime.UnixNano()) / nanosecondsPerSecond, KB: fileInfo.Size() / bytesPerKilobyte, Tok: state.tok}
	if !state.lastAsk.IsZero() {
		session.Ask = float64(state.lastAsk.UnixNano()) / nanosecondsPerSecond
	}
	switch {
	case title != "":
		session.Title = title
	case state.agent != "":
		session.Title = state.agent
	case first != "":
		session.Title = cut(first, sessionTitleMaxRunes)
	default:
		session.Title = "(sans titre)"
	}
	return session
}

// autoReviews : débuts de prompt des revues de sécurité lancées par des skills (même liste que AUTO, ui/src/lib/sessionsView.ts).
// Ces sessions ne sont ni sauvegardées pour la restauration ni rouvertes par gld-matin.
var autoReviews = []string{"Review this change for security vulnerabilities", "You previously flagged these candidate vulnerabilities", "Perform a read-only security review of this codebase"}

func (session Session) autoReview() bool {
	return !session.Named && slices.ContainsFunc(autoReviews, func(prefix string) bool { return strings.HasPrefix(session.Title, prefix) })
}

type Group struct {
	Project  string    `json:"project"`
	Missing  bool      `json:"missing"` // le dossier du projet n'existe plus (ou cwd inconnu)
	Sessions []Session `json:"sessions"`
}

// decodeCache : décoder un nom coûte un ReadDir par niveau et une regex par entrée ; avec un millier de dossiers de projets
// (la plupart sans transcript), memList, listSnippets et memHealth y passaient 2 s à chaque relecture (toutes les 15 s).
// Le résultat, trouvé ou non, vaut decodeTTL ; le cwd lu dans un transcript garde la priorité (projectCwd, sessions).
var (
	decodeMu    sync.Mutex
	decodeCache = map[string]decoded{}
	decodeTTL   = 2 * time.Minute
)

type decoded struct {
	path string
	at   time.Time
}

func decodeDir(name string) string {
	decodeMu.Lock()
	cachedEntry, found := decodeCache[name]
	decodeMu.Unlock()
	if found && time.Since(cachedEntry.at) < decodeTTL {
		return cachedEntry.path
	}
	decodedPath := decodeDirWalk(name)
	decodeMu.Lock()
	if len(decodeCache) > decodeCacheMax {
		clear(decodeCache)
	}
	decodeCache[name] = decoded{decodedPath, time.Now()}
	decodeMu.Unlock()
	return decodedPath
}

// skipRoots : racines que decodeDirWalk ne descend jamais. Pseudo-systèmes de fichiers (/proc, /sys, boucles de liens
// symboliques) et montages réseau (/mnt, /media) : un ReadDir y coûte des secondes, ou ne finit pas.
var skipRoots = map[string]bool{"/proc": true, "/sys": true, "/dev": true, "/run": true, "/mnt": true, "/media": true}

// decodeDirWalk retrouve le chemin d'un dossier de transcripts (/ . _ espace… → « - », donc lossy)
// en descendant l'arborescence réelle : seuls les dossiers dont le nom encodé colle sont suivis.
func decodeDirWalk(name string) string {
	var walk func(dirPath, remainingName string) string
	walk = func(dirPath, remainingName string) string {
		entries, err := readDirTrusted(dirPath)
		if err != nil {
			return ""
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			encodedName := nonAlnum.ReplaceAllString(entry.Name(), "-")
			childPath := filepath.Join(dirPath, entry.Name())
			if skipRoots[childPath] {
				continue
			}
			if remainingName == encodedName {
				return childPath
			}
			if strings.HasPrefix(remainingName, encodedName+"-") {
				if foundPath := walk(childPath, remainingName[len(encodedName)+1:]); foundPath != "" {
					return foundPath
				}
			}
		}
		return ""
	}
	if runtime.GOOS == "windows" { // « C:\Users\moi » s'encode « C--Users-moi » : lecteur, puis « : » et « \ » → « -- »
		for driveLetter := 'A'; driveLetter <= 'Z'; driveLetter++ {
			if prefix := string(driveLetter) + "--"; strings.HasPrefix(name, prefix) {
				if foundPath := walk(string(driveLetter)+`:\`, strings.TrimPrefix(name, prefix)); foundPath != "" {
					return foundPath
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
	var allSessions []Session
	dirCwd := map[string]string{} // cwd connu d'un dossier de transcripts, via l'une de ses sessions
	for _, transcriptPath := range files {
		fileInfo, err := os.Stat(transcriptPath)
		if err != nil || fileInfo.Size() == 0 {
			continue
		}
		session := info(transcriptPath, fileInfo)
		if session.Cwd != "" && dirCwd[session.Dir] == "" {
			dirCwd[session.Dir] = session.Cwd
		}
		allSessions = append(allSessions, session)
	}
	byPrefix := map[string]string{} // dossier + « session-xxxxxxxx » -> ID complet
	for _, session := range allSessions {
		if len(session.ID) >= sessionIDPrefixLength {
			byPrefix[session.Dir+"/session-"+session.ID[:sessionIDPrefixLength]] = session.ID
		}
	}
	for _, session := range allSessions {
		if session.team != "" {
			if parentID := byPrefix[session.Dir+"/"+session.team]; parentID != session.ID {
				session.Parent = parentID
			}
		}
		if session.Cwd == "" { // session sans cwd : on la range avec ses voisines, sinon par décodage du nom du dossier
			groupCwd, seen := dirCwd[session.Dir]
			if !seen {
				groupCwd = decodeDir(session.Dir)
				dirCwd[session.Dir] = groupCwd
			}
			session.Cwd = groupCwd
		}
		groupKey := session.Cwd
		if groupKey == "" {
			groupKey = session.Dir
		}
		groups[groupKey] = append(groups[groupKey], session)
	}
	projectGroups := []Group{}
	for groupKey, groupSessions := range groups {
		sort.Slice(groupSessions, func(leftIndex, rightIndex int) bool {
			return askOr(groupSessions[leftIndex]) > askOr(groupSessions[rightIndex])
		})
		projectInfo, err := os.Stat(groupKey)
		projectGroups = append(projectGroups, Group{groupKey, err != nil || !projectInfo.IsDir(), groupSessions})
	}
	sort.Slice(projectGroups, func(leftIndex, rightIndex int) bool {
		return projectGroups[leftIndex].Project < projectGroups[rightIndex].Project
	})
	return projectGroups
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
	fileInfo, err := os.Stat(path)
	if err != nil {
		return Detail{Prompts: []string{}, Models: []string{}}
	}
	detailMu.Lock()
	cachedEntry, found := detailCache[path]
	detailMu.Unlock()
	if found && cachedEntry.size == fileInfo.Size() && cachedEntry.mtime.Equal(fileInfo.ModTime()) {
		return cachedEntry.d
	}
	computed := computeDetail(path)
	detailMu.Lock()
	if len(detailCache) > detailCacheMax {
		clear(detailCache)
	}
	detailCache[path] = struct {
		size  int64
		mtime time.Time
		d     Detail
	}{fileInfo.Size(), fileInfo.ModTime(), computed}
	detailMu.Unlock()
	return computed
}

var (
	userTag      = []byte(`"type":"user"`)
	assistantTag = []byte(`"type":"assistant"`)
	toolResTag   = []byte(`"tool_use_id"`)
	textTag      = []byte(`"type":"text"`)
)

func computeDetail(path string) Detail {
	sessionDetail := Detail{Prompts: []string{}, Models: []string{}}
	models := map[string]bool{}
	_ = scan(path, func(lineBytes []byte) {
		// Seules les lignes d'échange comptent ; un résultat d'outil (souvent énorme) n'est pas un prompt : on ne le décode pas.
		isUserLine := bytes.Contains(lineBytes, userTag)
		isExchangeLine := isUserLine || bytes.Contains(lineBytes, assistantTag)
		isToolResultOnly := isUserLine && bytes.Contains(lineBytes, toolResTag) && !bytes.Contains(lineBytes, textTag)
		if !isExchangeLine || isToolResultOnly {
			return
		}
		var parsedLine line
		if json.Unmarshal(lineBytes, &parsedLine) != nil {
			return
		}
		if sessionDetail.T0 == "" {
			sessionDetail.T0 = parsedLine.Timestamp
		}
		if parsedLine.Timestamp != "" {
			sessionDetail.T1 = parsedLine.Timestamp
		}
		if parsedLine.GitBranch != "" {
			sessionDetail.Branch = parsedLine.GitBranch
		}
		switch parsedLine.Type {
		case "user":
			if prompt := promptOf(&parsedLine); prompt != "" {
				sessionDetail.N++
				sessionDetail.Prompts = append(sessionDetail.Prompts, cut(prompt, previewMaxRunes))
				if len(sessionDetail.Prompts) > recentPromptCount {
					sessionDetail.Prompts = sessionDetail.Prompts[1:]
				}
			}
		case "assistant":
			if model := parsedLine.Message.Model; model != "" && !models[model] {
				models[model] = true
				sessionDetail.Models = append(sessionDetail.Models, model)
			}
		}
	})
	sort.Strings(sessionDetail.Models)
	filepath.WalkDir(strings.TrimSuffix(path, ".jsonl"), func(walkedPath string, walkedEntry os.DirEntry, err error) error {
		if err == nil && !walkedEntry.IsDir() && strings.HasSuffix(walkedPath, ".jsonl") {
			sessionDetail.Subagents++
		}
		return nil
	})
	return sessionDetail
}

// --- lecture d'une conversation ---------------------------------------------

type Msg struct {
	R  string `json:"r"` // user | assistant | tool | doc (fichier écrit : T = chemin)
	T  string `json:"t"`
	Ts string `json:"ts,omitempty"`
	// O/N : décalage de la ligne du transcript et rang du message dans cette ligne. Clé stable : quand on
	// charge des messages plus anciens, ceux déjà affichés gardent la leur (pas de réutilisation de nœuds).
	O int64 `json:"o,omitempty"`
	N int   `json:"n,omitempty"`
	// Img : type d'une image du message (le texte est alors vide) ; K : rang du bloc dans le message, pour la relire (GET /v1/sessions/{id}/conversation/images/{img}).
	Img string `json:"img,omitempty"`
	K   int    `json:"k,omitempty"`
	// Cut/Len : texte coupé à l'octet Cut d'un texte de Len octets ; la suite se lit par morceaux (GET /v1/sessions/{id}/conversation/text).
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
func toolArg(toolInput json.RawMessage) string {
	var fields map[string]any
	if json.Unmarshal(toolInput, &fields) == nil {
		for _, key := range []string{"command", "file_path", "pattern", "path", "url", "query", "description", "prompt"} {
			if value, isString := fields[key].(string); isString && value != "" {
				return cut(value, previewMaxRunes)
			}
		}
	}
	return cut(string(toolInput), previewMaxRunes)
}

// askSummary : les questions d'un AskUserQuestion, une par ligne, options à la suite, au lieu du JSON brut tronqué.
func askSummary(toolInput json.RawMessage) string {
	var ask struct {
		Questions []struct {
			Question string
			Header   string
			Options  []struct{ Label string }
		}
	}
	if json.Unmarshal(toolInput, &ask) != nil || len(ask.Questions) == 0 {
		return toolArg(toolInput)
	}
	var lines []string
	for _, question := range ask.Questions {
		line := "• " + question.Question
		if question.Header != "" {
			line = "• [" + question.Header + "] " + question.Question
		}
		var labels []string
		for _, option := range question.Options {
			labels = append(labels, option.Label)
		}
		if len(labels) > 0 {
			line += "\n   → " + strings.Join(labels, " | ")
		}
		lines = append(lines, line)
	}
	return cut(strings.Join(lines, "\n"), toolResultMaxRunes)
}

// flat aplatit le contenu d'un tool_result (texte ou liste de blocs).
func flat(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []blk
	var blockTexts []string
	if json.Unmarshal(content, &blocks) == nil {
		for _, block := range blocks {
			blockTexts = append(blockTexts, block.Text)
		}
	}
	return strings.Join(blockTexts, "\n")
}

// lineMsgs extrait d'une ligne de transcript le texte des échanges, plus (tools) les appels d'outils et leurs résultats tronqués.
func lineMsgs(lineBytes []byte, tools bool) []Msg { return lineMsgsMode(lineBytes, tools, false) }

// lineMsgsMode : avec anchors (outils masqués), chaque appel d'outil laisse un message vide de rôle « tool » : l'interface en fait
// une ancre repliée à la place des outils. Les résultats d'outil n'en laissent pas (un appel et son résultat comptent pour un).
func lineMsgsMode(lineBytes []byte, tools, anchors bool) []Msg {
	if bytes.Contains(lineBytes, []byte(`"isMeta":true`)) {
		return nil
	}
	isUserLine := bytes.Contains(lineBytes, []byte(`"type":"user"`))
	if !isUserLine && !bytes.Contains(lineBytes, []byte(`"type":"assistant"`)) {
		return nil
	}
	var parsedLine struct {
		Type      string
		Timestamp string
		Message   struct{ Content json.RawMessage }
	}
	if json.Unmarshal(lineBytes, &parsedLine) != nil || (parsedLine.Type != "user" && parsedLine.Type != "assistant") {
		return nil
	}
	var blocks []blk
	var plainContent string
	if json.Unmarshal(parsedLine.Message.Content, &plainContent) == nil {
		blocks = []blk{{Type: "text", Text: plainContent}}
	} else if json.Unmarshal(parsedLine.Message.Content, &blocks) != nil {
		return nil
	}
	var messages []Msg
	for blockIndex, block := range blocks {
		switch block.Type {
		case "image":
			if block.Source.MediaType != "" {
				messages = append(messages, Msg{R: parsedLine.Type, Img: block.Source.MediaType, K: blockIndex, Ts: parsedLine.Timestamp})
			}
		case "text":
			trimmedText := strings.TrimSpace(reminder.ReplaceAllString(block.Text, ""))
			isCommandEcho := strings.HasPrefix(trimmedText, "<command-") || strings.HasPrefix(trimmedText, "<local-command")
			isCaveat := strings.HasPrefix(strings.ToLower(trimmedText), "caveat:")
			if trimmedText == "" || isCommandEcho || isCaveat {
				continue
			}
			messages = append(messages, Msg{R: parsedLine.Type, T: trimmedText, Ts: parsedLine.Timestamp})
			if parsedLine.Type == "assistant" {
				for _, docPath := range mentionedDocs(trimmedText) {
					messages = append(messages, Msg{R: "doc", T: docPath, Ts: parsedLine.Timestamp})
				}
			}
		case "tool_use":
			if docPath := writtenPath(block.Name, block.Input); docPath != "" {
				messages = append(messages, Msg{R: "doc", T: docPath, Ts: parsedLine.Timestamp})
			}
			if tools {
				callText := toolArg(block.Input)
				if block.Name == "AskUserQuestion" {
					callText = "\n" + askSummary(block.Input)
				}
				messages = append(messages, Msg{R: "tool", T: block.Name + "  " + callText, Ts: parsedLine.Timestamp})
			} else if anchors {
				messages = append(messages, Msg{R: "tool", Ts: parsedLine.Timestamp})
			}
		case "tool_result":
			if tools {
				messages = append(messages, Msg{R: "tool", T: "↳ " + cut(strings.TrimSpace(flat(block.Content)), toolResultMaxRunes), Ts: parsedLine.Timestamp})
			}
		}
	}
	return messages
}

// withOffset numérote les messages d'une ligne (voir Msg.O / Msg.N).
func withOffset(messages []Msg, lineOffset int64) []Msg {
	for messageIndex := range messages {
		messages[messageIndex].O, messages[messageIndex].N = lineOffset, messageIndex
	}
	return messages
}

// conv : toute la conversation d'un transcript.
func conv(path string, tools bool) []Msg {
	messages := []Msg{}
	var lineOffset int64
	_ = scan(path, func(lineBytes []byte) {
		messages = append(messages, withOffset(lineMsgs(lineBytes, tools), lineOffset)...)
		lineOffset += int64(len(lineBytes))
	})
	return messages
}

// transcript valide le couple dir/id reçu du navigateur et renvoie le fichier.
func transcript(dir, id string) (string, bool) {
	if strings.ContainsAny(dir, "/\\") || dir == "" || dir == ".." || !idRe.MatchString(id) {
		return "", false
	}
	transcriptPath := filepath.Join(root, dir, id+".jsonl")
	fileInfo, err := os.Stat(transcriptPath)
	return transcriptPath, err == nil && fileInfo.Mode().IsRegular()
}

// withEmptyDirs : les dossiers enregistrés à la main qui n'ont aucune session apparaissent comme des dossiers vides, pour qu'on puisse
// y lancer un prompt. Un dossier qui a déjà des sessions n'est pas répété.
func withEmptyDirs(groups []Group, dirs []string) []Group {
	have := map[string]bool{}
	for _, group := range groups {
		have[group.Project] = true
	}
	for _, registeredDir := range dirs {
		if have[registeredDir] {
			continue
		}
		dirInfo, err := os.Stat(registeredDir)
		groups = append(groups, Group{registeredDir, err != nil || !dirInfo.IsDir(), []Session{}})
	}
	sort.Slice(groups, func(leftIndex, rightIndex int) bool { return groups[leftIndex].Project < groups[rightIndex].Project })
	return groups
}

// askOr : clé de tri d'une session, son dernier message utilisateur, à défaut son mtime.
func askOr(session Session) float64 {
	if session.Ask > 0 {
		return session.Ask
	}
	return session.Mtime
}
