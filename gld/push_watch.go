package main

// Détection des sessions qui demandent ou attendent, pour le Web Push : la passerelle interroge son propre home et ceux des
// enfants déjà lancés, puis envoie les nouveautés. Les droits sont jugés à l'envoi (push_send.go), pas ici.

import (
	"encoding/json"
	"net/http"
	"time"
)

const pushChildFetchTimeout = 5 * time.Second

type pushHomeState struct {
	Waiting []string          `json:"waiting"`
	Asking  []string          `json:"asking"`
	Titles  map[string]string `json:"titles"`
}

func localPushState() pushHomeState {
	titles := map[string]string{}
	for _, group := range sessions() {
		for _, session := range group.Sessions {
			titles[session.ID] = session.Title
		}
	}
	return pushHomeState{Waiting: waiting(), Asking: askingSessions(), Titles: titles}
}

func routesPushState(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/push-state", func(responseWriter http.ResponseWriter, request *http.Request) {
		if !childAuthorized(request) {
			writeErr(responseWriter, http.StatusForbidden, "forbidden", "forbidden")
			return
		}
		writeJSON(responseWriter, http.StatusOK, localPushState())
	})
}

func toSetOf(sessionIDs []string) map[string]bool {
	set := make(map[string]bool, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		set[sessionID] = true
	}
	return set
}

// pushEventsFor : une question nouvelle notifie « asking » ; une attente nouvelle notifie « waiting » sauf si la session pose
// une question (la question prime). Le premier passage mémorise l'état sans rien notifier.
func pushEventsFor(homeID string, previous, current pushHomeState, isFirstPass bool) []pushEvent {
	if isFirstPass {
		return nil
	}
	previouslyAsking, previouslyWaiting := toSetOf(previous.Asking), toSetOf(previous.Waiting)
	currentlyAsking := toSetOf(current.Asking)
	var events []pushEvent
	for _, sessionID := range current.Asking {
		if !previouslyAsking[sessionID] {
			events = append(events, pushEvent{homeID, sessionID, current.Titles[sessionID], "asking"})
		}
	}
	for _, sessionID := range current.Waiting {
		if !previouslyWaiting[sessionID] && !currentlyAsking[sessionID] {
			events = append(events, pushEvent{homeID, sessionID, current.Titles[sessionID], "waiting"})
		}
	}
	return events
}

type runningChild struct{ homeID, port, token string }

func runningChildren() []runningChild {
	homesMu.Lock()
	defer homesMu.Unlock()
	var children []runningChild
	for name, process := range homeProcs {
		select {
		case <-process.done:
			continue
		default:
		}
		if home, found := homeByName(name); found && home.ID != "" {
			children = append(children, runningChild{home.ID, process.port, process.token})
		}
	}
	return children
}

func fetchChildPushState(child runningChild) (pushHomeState, bool) {
	request, requestErr := http.NewRequest("GET", "http://127.0.0.1:"+child.port+"/v1/push-state", nil)
	if requestErr != nil {
		return pushHomeState{}, false
	}
	request.Header.Set(childHeader, child.token)
	client := &http.Client{Timeout: pushChildFetchTimeout}
	response, fetchErr := client.Do(request)
	if fetchErr != nil {
		return pushHomeState{}, false
	}
	defer response.Body.Close()
	var state pushHomeState
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&state) != nil {
		return pushHomeState{}, false
	}
	return state, true
}

// collectPushStates : un home enfant injoignable est simplement absent du résultat, son état précédent est conservé.
func collectPushStates() map[string]pushHomeState {
	states := map[string]pushHomeState{mainHomeID: localPushState()}
	for _, child := range runningChildren() {
		if state, fetched := fetchChildPushState(child); fetched {
			states[child.homeID] = state
		}
	}
	return states
}

var pushStatesSource = collectPushStates

func watchPush(every time.Duration) {
	previousStates := map[string]pushHomeState{}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
		}
		for homeID, currentState := range pushStatesSource() {
			previousState, wasSeen := previousStates[homeID]
			for _, event := range pushEventsFor(homeID, previousState, currentState, !wasSeen) {
				pushDispatch(event)
			}
			previousStates[homeID] = currentState
		}
	}
}
