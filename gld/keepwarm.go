package main

// « Maintenir au chaud » : le cache des prompts d'une session expire une heure après son dernier appel (HOT côté interface). Quand il
// reste moins de 5 minutes, gld envoie lui-même un court message à la session, sans action de l'utilisateur : lire le cache coûte
// une fraction de ce que coûte le reconstruire. Chaque ping est un vrai tour (il apparaît dans la conversation) : le nombre et le
// coût en sont comptés, et le maintien s'arrête seul après `Max` pings, ou dès que la session est froide.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	cacheTTL      = 3600 * time.Second // durée du cache des prompts (même valeur que HOT dans l'interface)
	warmBelow     = 300 * time.Second  // on relance quand il reste moins que cela
	warmMaxPings  = 24
	warmDefaultMx = 6
	pingPrompt    = "Ping automatique de maintien du cache : réponds uniquement « ok »."
)

type WarmState struct {
	Dir      string  `json:"dir"`
	On       bool    `json:"on"`
	Pings    int     `json:"pings"`    // depuis l'activation
	Max      int     `json:"max"`      // arrêt automatique après ce nombre de pings
	Cost     float64 `json:"cost"`     // coût indiqué par claude pour les pings (hors sessions ouvertes dans tmux, non mesuré)
	LastPing int64   `json:"lastPing"` // secondes Unix
	Note     string  `json:"note,omitempty"`
}

var (
	warmMu    sync.Mutex
	warmState = map[string]*WarmState{}
	warmOnce  sync.Once
	warmPing  = doPing // remplaçable dans les tests
)

func warmFile() string { return filepath.Join(cfgDir(), "gld-web-keepwarm.json") }

func warmLoad() {
	b, err := os.ReadFile(warmFile())
	if err != nil {
		return
	}
	m := map[string]*WarmState{}
	if json.Unmarshal(b, &m) != nil {
		return
	}
	for id, s := range m {
		if idRe.MatchString(id) && s != nil && s.Max > 0 && s.Max <= warmMaxPings {
			warmState[id] = s
		}
	}
}

func warmSave() {
	b, _ := json.MarshalIndent(warmState, "", "  ")
	tmp := warmFile() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, warmFile())
	}
}

var modelRe = regexp.MustCompile(`^[\w.\[\]-]{1,80}$`)

// lastModel : le modèle du dernier message de Claude. Le cache dépend du modèle : un ping avec un autre ne réchaufferait rien.
func lastModel(path string) string {
	lines := tailLines(path, 1<<20)
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(string(lines[i]), `"type":"assistant"`) {
			continue
		}
		var l line
		if json.Unmarshal(lines[i], &l) == nil && l.Message.Model != "" && !strings.HasPrefix(l.Message.Model, "<") && modelRe.MatchString(l.Message.Model) {
			return l.Message.Model
		}
	}
	return ""
}

// doPing envoie le ping : par tmux si la session est ouverte dans un terminal, sinon par `claude -p --resume`.
func doPing(id, path, cwd string, open bool) (cost float64, err error) {
	if open {
		return 0, sendToSession(id, pingPrompt)
	}
	model := lastModel(path)
	if model == "" {
		return 0, errors.New("modèle de la session inconnu")
	}
	chatMu.Lock()
	if chatRun[id] != nil {
		chatMu.Unlock()
		return 0, errors.New("Claude répond déjà")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := withPath(exec.CommandContext(ctx, "claude", chatArgs([]string{"--model=" + model}, id, "", pingPrompt)...))
	cmd.Dir = cwd
	out, _ := cmd.StdoutPipe()
	if err = cmd.Start(); err != nil {
		chatMu.Unlock()
		return 0, err
	}
	chatRun[id] = cmd
	chatMu.Unlock()
	defer func() {
		chatMu.Lock()
		delete(chatRun, id)
		chatMu.Unlock()
	}()
	var failed string
	saw := relay(out, func(e chatEvent) {
		if e.Done {
			cost, failed = e.Cost, e.Error
		}
	})
	werr := cmd.Wait()
	switch {
	case failed != "":
		return cost, errors.New(failed)
	case !saw:
		if werr == nil {
			werr = errors.New("aucun résultat")
		}
		return cost, werr
	}
	return cost, nil
}

// warmTick : un passage sur les sessions maintenues. now est injecté pour les tests.
func warmTick(now time.Time) {
	warmMu.Lock()
	var ids []string
	for id, s := range warmState {
		if s.On {
			ids = append(ids, id)
		}
	}
	warmMu.Unlock()
	if len(ids) == 0 {
		return
	}
	mt, cwd := map[string]float64{}, map[string]string{}
	for _, g := range sessions() {
		for _, s := range g.Sessions {
			mt[s.ID], cwd[s.ID] = s.Mtime, s.Cwd
		}
	}
	open := liveSessions() // nil : on ne sait pas, on ne pingue pas une session peut-être ouverte
	idle := map[string]bool{}
	for _, id := range waiting() {
		idle[id] = true
	}
	for _, id := range ids {
		warmMu.Lock()
		s := warmState[id]
		on := s != nil && s.On // arrêtée pendant les pings précédents de ce passage
		warmMu.Unlock()
		if !on {
			continue
		}
		m, known := mt[id]
		stop := func(note string) {
			warmMu.Lock()
			s.On, s.Note = false, note
			warmSave()
			warmMu.Unlock()
		}
		if !known {
			stop("session introuvable : maintien arrêté")
			continue
		}
		left := cacheTTL - now.Sub(time.Unix(int64(m), 0))
		if left <= 0 {
			stop("le cache était déjà froid : maintien arrêté")
			continue
		}
		if left >= warmBelow || now.Unix()-s.LastPing < 240 {
			continue
		}
		if open == nil {
			continue
		}
		isOpen := open[id]
		if isOpen && !idle[id] {
			continue // elle travaille : son activité garde le cache chaud
		}
		if s.Pings >= s.Max {
			stop("limite de pings atteinte : maintien arrêté")
			continue
		}
		p, ok := transcript(s.Dir, id)
		if !ok {
			stop("transcript introuvable : maintien arrêté")
			continue
		}
		cost, err := warmPing(id, p, cwd[id], isOpen)
		journal("keepwarm %s ouverte=%v : %v", id, isOpen, errOr(err))
		warmMu.Lock()
		s.LastPing = now.Unix()
		if err != nil {
			s.Note = "ping en échec : " + cut(err.Error(), 200)
		} else {
			s.Pings++
			s.Cost += cost
			s.Note = ""
		}
		warmSave()
		warmMu.Unlock()
	}
}

func keepWarmLoop(every time.Duration) {
	for range time.Tick(every) {
		warmTick(time.Now())
	}
}

func routesKeepWarm(mux *http.ServeMux) {
	warmOnce.Do(func() {
		warmMu.Lock()
		warmLoad()
		warmMu.Unlock()
	})
	mux.HandleFunc("GET /keepwarm", func(w http.ResponseWriter, r *http.Request) {
		warmMu.Lock()
		defer warmMu.Unlock()
		writeJSON(w, 200, map[string]any{"items": warmState, "ttl": int(cacheTTL / time.Second), "below": int(warmBelow / time.Second)})
	})
	mux.HandleFunc("POST /keepwarm", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Dir, ID string
			On      bool
			Max     int
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		if _, ok := transcript(req.Dir, req.ID); !ok {
			writeJSON(w, 200, map[string]any{"ok": false, "out": "session invalide"})
			return
		}
		warmMu.Lock()
		defer warmMu.Unlock()
		s := warmState[req.ID]
		if !req.On {
			if s != nil {
				s.On, s.Note = false, ""
				warmSave()
			}
			writeJSON(w, 200, map[string]any{"ok": true, "out": "Maintien au chaud arrêté"})
			return
		}
		if req.Max <= 0 {
			req.Max = warmDefaultMx
		}
		req.Max = min(req.Max, warmMaxPings)
		warmState[req.ID] = &WarmState{Dir: req.Dir, On: true, Max: req.Max}
		warmSave()
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Maintien au chaud activé"})
	})
}
