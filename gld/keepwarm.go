package main

// « Maintenir au chaud » : le cache des prompts d'une session expire une heure après son dernier appel (HOT côté interface). Quand il
// reste moins de 5 minutes, gld envoie lui-même un court message à la session, sans action de l'utilisateur : lire le cache coûte
// une fraction de ce que coûte le reconstruire. Chaque ping est un vrai tour (il apparaît dans la conversation) : il est compté, et
// le maintien s'arrête seul après `Max` pings, dès que la session est froide, ou dès qu'elle est fermée.
//
// Seule une session ouverte dans tmux est pinguée : `claude -p --resume` n'a ni les mêmes outils ni le même prompt système que le
// mode interactif, et ne relit pas son cache (mesuré : 0 token lu, 268 709 réécrits, 102 s après un tour -p).

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
	Dir      string `json:"dir"`
	On       bool   `json:"on"`
	Pings    int    `json:"pings"`    // depuis l'activation
	Max      int    `json:"max"`      // arrêt automatique après ce nombre de pings
	LastPing int64  `json:"lastPing"` // secondes Unix
	Note     string `json:"note,omitempty"`
}

var (
	warmMu    sync.Mutex
	warmState = map[string]*WarmState{}
	warmOnce  sync.Once
	warmPing  = func(id string) error { return sendToSession(id, pingPrompt) } // remplaçable dans les tests
)

func warmFile() string { return filepath.Join(cfgDir(), "gld-web-keepwarm.json") }

func warmLoad() {
	b, err := readTrusted(warmFile())
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
	if err := rWriteAtomic(cfgDir(), warmFile(), b, 0o600); err != nil {
		journal("keepwarm-save : %v", err)
	}
}

// sessionMtime : date du dernier message d'une session connue.
func sessionMtime(id string) (float64, bool) {
	for _, g := range sessions() {
		for _, s := range g.Sessions {
			if s.ID == id {
				return s.Mtime, true
			}
		}
	}
	return 0, false
}

func closedNote() string { return L("srv.keepwarm.closedNote") }

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
	mt := map[string]float64{}
	for _, g := range sessions() {
		for _, s := range g.Sessions {
			mt[s.ID] = s.Mtime
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
			stop(L("srv.keepwarm.notFound"))
			continue
		}
		left := cacheTTL - now.Sub(time.Unix(int64(m), 0))
		if left <= 0 {
			stop(L("srv.keepwarm.alreadyCold"))
			continue
		}
		if left >= warmBelow || now.Unix()-s.LastPing < 240 {
			continue
		}
		if open == nil {
			continue
		}
		if !open[id] {
			stop(closedNote())
			continue
		}
		if !idle[id] {
			continue // elle travaille : son activité garde le cache chaud
		}
		if s.Pings >= s.Max {
			stop(L("srv.keepwarm.pingLimit"))
			continue
		}
		err := warmPing(id)
		journal("keepwarm %s : %v", id, errOr(err))
		warmMu.Lock()
		s.LastPing = now.Unix()
		if err != nil {
			s.Note = L("srv.keepwarm.pingFailed", "A1", cut(err.Error(), 200))
		} else {
			s.Pings++
			s.Note = ""
		}
		warmSave()
		warmMu.Unlock()
	}
}

func keepWarmLoop(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-t.C:
			warmTick(time.Now())
		}
	}
}

func routesKeepWarm(mux *http.ServeMux) {
	warmOnce.Do(func() {
		warmMu.Lock()
		warmLoad()
		warmMu.Unlock()
	})
	mux.HandleFunc("GET /v1/keepwarm", func(w http.ResponseWriter, r *http.Request) {
		warmMu.Lock()
		defer warmMu.Unlock()
		writeJSON(w, 200, map[string]any{"items": warmState, "ttl": int(cacheTTL / time.Second), "below": int(warmBelow / time.Second)})
	})
	mux.HandleFunc("PUT /v1/keepwarm", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Dir, ID string
			On      bool
			Max     int
			Reheat  bool // session au cache froid : accepte d'en payer la reconstruction par un premier ping
		}
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if _, ok := transcript(req.Dir, req.ID); !ok {
			writeErr(w, http.StatusNotFound, "not_found", L("srv.chat.sessionInvalide"))
			return
		}
		reheated := false
		if req.On { // hors verrou : `claude agents` prend du temps
			if open := liveSessions(); open != nil && !open[req.ID] {
				writeErr(w, http.StatusConflict, "conflict", L("srv.keepwarm.sessionFermeeRouvrezLa"))
				return
			}
			// Reconstruire un cache froid coûte cher : le premier ping n'est envoyé que sur demande explicite (Reheat).
			if m, known := sessionMtime(req.ID); req.Reheat && known && time.Since(time.Unix(int64(m), 0)) >= cacheTTL {
				err := warmPing(req.ID) // le premier tour reconstruit le cache, le maintien prend ensuite le relais
				journal("keepwarm-reheat %s : %v", req.ID, errOr(err))
				if err != nil {
					writeErr(w, http.StatusBadGateway, "upstream_failed", L("srv.keepwarm.pingFailed", "A1", cut(err.Error(), 200)))
					return
				}
				reheated = true
			}
		}
		warmMu.Lock()
		defer warmMu.Unlock()
		s := warmState[req.ID]
		if !req.On {
			if s != nil {
				s.On, s.Note = false, ""
				warmSave()
			}
			writeJSON(w, 200, map[string]any{"message": L("srv.keepwarm.maintienAuChaudArrete")})
			return
		}
		if req.Max <= 0 {
			req.Max = warmDefaultMx
		}
		req.Max = min(req.Max, warmMaxPings)
		ns := &WarmState{Dir: req.Dir, On: true, Max: req.Max}
		if reheated {
			ns.Pings, ns.LastPing = 1, time.Now().Unix()
		}
		warmState[req.ID] = ns
		warmSave()
		writeJSON(w, 200, map[string]any{"message": L("srv.keepwarm.maintienAuChaudActive")})
	})
}
