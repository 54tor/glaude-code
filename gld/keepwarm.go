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
	// warmMinPingGapSeconds : écart minimal entre deux pings d'une même session.
	warmMinPingGapSeconds = 240
	warmErrorMaxLength    = 200
	keepWarmBodyMax       = 1 << 10
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
	content, err := readTrusted(warmFile())
	if err != nil {
		return
	}
	states := map[string]*WarmState{}
	if json.Unmarshal(content, &states) != nil {
		return
	}
	for sessionID, state := range states {
		isValidState := idRe.MatchString(sessionID) && state != nil && state.Max > 0 && state.Max <= warmMaxPings
		if isValidState {
			warmState[sessionID] = state
		}
	}
}

func warmSave() {
	encoded, _ := json.MarshalIndent(warmState, "", "  ")
	if err := rWriteAtomic(cfgDir(), warmFile(), encoded, 0o600); err != nil {
		journal("keepwarm-save : %v", err)
	}
}

// sessionMtime : date du dernier message d'une session connue.
func sessionMtime(sessionID string) (float64, bool) {
	for _, group := range sessions() {
		for _, session := range group.Sessions {
			if session.ID == sessionID {
				return session.Mtime, true
			}
		}
	}
	return 0, false
}

func closedNote() string { return L("srv.keepwarm.closedNote") }

// warmTick : un passage sur les sessions maintenues. now est injecté pour les tests.
func warmTick(now time.Time) {
	warmMu.Lock()
	var sessionIDs []string
	for sessionID, state := range warmState {
		if state.On {
			sessionIDs = append(sessionIDs, sessionID)
		}
	}
	warmMu.Unlock()
	if len(sessionIDs) == 0 {
		return
	}
	modifiedTimes := map[string]float64{}
	for _, group := range sessions() {
		for _, session := range group.Sessions {
			modifiedTimes[session.ID] = session.Mtime
		}
	}
	open := liveSessions() // nil : on ne sait pas, on ne pingue pas une session peut-être ouverte
	idle := map[string]bool{}
	for _, sessionID := range waiting() {
		idle[sessionID] = true
	}
	for _, sessionID := range sessionIDs {
		warmMu.Lock()
		state := warmState[sessionID]
		stillOn := state != nil && state.On // arrêtée pendant les pings précédents de ce passage
		warmMu.Unlock()
		if !stillOn {
			continue
		}
		modifiedTime, known := modifiedTimes[sessionID]
		stop := func(note string) {
			warmMu.Lock()
			state.On, state.Note = false, note
			warmSave()
			warmMu.Unlock()
		}
		if !known {
			stop(L("srv.keepwarm.notFound"))
			continue
		}
		remaining := cacheTTL - now.Sub(time.Unix(int64(modifiedTime), 0))
		if remaining <= 0 {
			stop(L("srv.keepwarm.alreadyCold"))
			continue
		}
		pingedRecently := now.Unix()-state.LastPing < warmMinPingGapSeconds
		if remaining >= warmBelow || pingedRecently {
			continue
		}
		if open == nil {
			continue
		}
		if !open[sessionID] {
			stop(closedNote())
			continue
		}
		if !idle[sessionID] {
			continue // elle travaille : son activité garde le cache chaud
		}
		if state.Pings >= state.Max {
			stop(L("srv.keepwarm.pingLimit"))
			continue
		}
		err := warmPing(sessionID)
		journal("keepwarm %s : %v", sessionID, errOr(err))
		warmMu.Lock()
		state.LastPing = now.Unix()
		if err != nil {
			state.Note = L("srv.keepwarm.pingFailed", "A1", cut(err.Error(), warmErrorMaxLength))
		} else {
			state.Pings++
			state.Note = ""
		}
		warmSave()
		warmMu.Unlock()
	}
}

func keepWarmLoop(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
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
	mux.HandleFunc("GET /v1/keepwarm", func(responseWriter http.ResponseWriter, request *http.Request) {
		warmMu.Lock()
		defer warmMu.Unlock()
		writeJSON(responseWriter, http.StatusOK, map[string]any{"items": warmState, "ttl": int(cacheTTL / time.Second), "below": int(warmBelow / time.Second)})
	})
	mux.HandleFunc("PUT /v1/keepwarm", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Dir, ID string
			On      bool
			Max     int
			Reheat  bool // session au cache froid : accepte d'en payer la reconstruction par un premier ping
		}
		if !decodeBody(responseWriter, request, &body, keepWarmBodyMax) {
			return
		}
		if _, exists := transcript(body.Dir, body.ID); !exists {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.chat.sessionInvalide"))
			return
		}
		reheated := false
		if body.On { // hors verrou : `claude agents` prend du temps
			if open := liveSessions(); open != nil && !open[body.ID] {
				writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.keepwarm.sessionFermeeRouvrezLa"))
				return
			}
			// Reconstruire un cache froid coûte cher : le premier ping n'est envoyé que sur demande explicite (Reheat).
			modifiedTime, known := sessionMtime(body.ID)
			isCold := known && time.Since(time.Unix(int64(modifiedTime), 0)) >= cacheTTL
			if body.Reheat && isCold {
				err := warmPing(body.ID) // le premier tour reconstruit le cache, le maintien prend ensuite le relais
				journal("keepwarm-reheat %s : %v", body.ID, errOr(err))
				if err != nil {
					writeErr(responseWriter, http.StatusBadGateway, "upstream_failed", L("srv.keepwarm.pingFailed", "A1", cut(err.Error(), warmErrorMaxLength)))
					return
				}
				reheated = true
			}
		}
		warmMu.Lock()
		defer warmMu.Unlock()
		state := warmState[body.ID]
		if !body.On {
			if state != nil {
				state.On, state.Note = false, ""
				warmSave()
			}
			writeJSON(responseWriter, http.StatusOK, map[string]any{"message": L("srv.keepwarm.maintienAuChaudArrete")})
			return
		}
		if body.Max <= 0 {
			body.Max = warmDefaultMx
		}
		body.Max = min(body.Max, warmMaxPings)
		newState := &WarmState{Dir: body.Dir, On: true, Max: body.Max}
		if reheated {
			newState.Pings, newState.LastPing = 1, time.Now().Unix()
		}
		warmState[body.ID] = newState
		warmSave()
		writeJSON(responseWriter, http.StatusOK, map[string]any{"message": L("srv.keepwarm.maintienAuChaudActive")})
	})
}
