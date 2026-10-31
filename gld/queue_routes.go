package main

// Routes de la file de messages et boucle qui la vide dans les sessions ouvertes (tmux).

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Identique à FOLLOW_PREFIX de gld/ui/src/lib/queue.ts (un test le vérifie).
const followPrefix = "[Message en file d'attente : d'autres messages suivent. Ne t'arrête pas pour poser une question : note tes éventuelles questions et pose-les à la fin, une fois tous les messages traités.]\n\n"

// Étiquette obligatoire des messages invités, « [Invité <nom ou numéro>] » : le texte collé ne commence jamais par « / », « ! »
// ou « # » (commande Claude Code) et l'auteur reste visible dans la conversation. guestLabelMax borne la plus longue.
const guestLabelMax = len("[Invité ") + inviteNameMax*utf8.UTFMax + len("] ")

// guestTag : le nom est réassaini (fichier altéré) ; il ne peut donc ni fermer le crochet ni dépasser guestLabelMax.
func guestTag(invite Invite) string {
	label := inviteName(invite.Name)
	if label == "" {
		label = strconv.Itoa(invite.Seq)
	}
	return "[Invité " + label + "] "
}

const (
	queueSettle = 4500 * time.Millisecond
	queueTick   = 1500 * time.Millisecond
	queueRetry  = 10 * time.Second
)

// Points de substitution des tests : envoi, sessions ouvertes, sessions en question.
var (
	queueSt  *queueStore
	queueDir = cfgDir // les tests pointent ailleurs : jamais le vrai dossier de l'utilisateur
	sendFn   = sendToSession
	openFn   = func() map[string]bool {
		openSessions := map[string]bool{}
		sendableSessions, _ := sendable()
		for sessionID := range sendableSessions {
			openSessions[sessionID] = true
		}
		return openSessions
	}
	askingFn = askingSessions
	screenFn = sessionScreen
)

var (
	sentMu    sync.Mutex
	notBefore = map[string]time.Time{}
)

// markSent : un envoi vient d'avoir lieu (file ou hôte au clavier) ; la session doit avoir le temps de démarrer un tour.
func markSent(sessionID string) {
	sentMu.Lock()
	notBefore[sessionID] = time.Now().Add(queueSettle)
	sentMu.Unlock()
}

func drainOnce(queue *queueStore, now time.Time) {
	sessionIDs := queue.Sessions()
	if len(sessionIDs) == 0 {
		return
	}
	openSessionIDs, idleSessions, askingSessionIDs := openFn(), toSet(waiting()), toSet(askingFn())
	for _, sessionID := range sessionIDs {
		sentMu.Lock()
		mustWait := now.Before(notBefore[sessionID])
		sentMu.Unlock()
		if mustWait || !openSessionIDs[sessionID] || askingSessionIDs[sessionID] {
			continue
		}
		if !idleSessions[sessionID] && !idleByScreen(sessionID, now) {
			continue
		}
		head, hasHead := queue.Head(sessionID)
		if !hasHead {
			continue
		}
		invite, isDropped := dropRevokedGuest(queue, sessionID, head)
		if isDropped || queue.MarkSending(sessionID, head.ID) != nil {
			continue
		}
		text := head.Text
		if head.Author == "guest" {
			text = guestTag(invite) + text // Add borne le texte invité : étiquette et préfixe tiennent toujours dans sendMax
		}
		// Un texte hôte proche de sendMax ne supporte pas le préfixe : il part sans, sinon sendToSession le refuserait indéfiniment.
		hasFollowers := len(queue.List(sessionID)) > 1
		prefixFits := len(followPrefix)+len(text) <= sendMax
		if hasFollowers && !strings.HasPrefix(text, followPrefix) && prefixFits {
			text = followPrefix + text
		}
		// askingFn est gardé 3 s : un dialogue d'autorisation ouvert depuis prendrait le texte et Entrée comme réponse.
		screen, err := screenFn(sessionID)
		if err == nil && screenAsks(screen) {
			err = errQueueBusy
		}
		if err == nil {
			if _, isDropped := dropRevokedGuest(queue, sessionID, head); isDropped {
				continue
			}
		}
		if err == nil {
			err = sendFn(sessionID, text)
		}
		if err != nil {
			queue.Requeue(sessionID, head.ID)
			sentMu.Lock()
			notBefore[sessionID] = now.Add(queueRetry)
			sentMu.Unlock()
			if err != errQueueBusy {
				journal("queue %s : %v", sessionID, err)
			}
			continue
		}
		queue.Done(sessionID, head.ID)
		sentMu.Lock()
		notBefore[sessionID] = now.Add(queueSettle)
		sentMu.Unlock()
	}
}

// dropRevokedGuest retire un message invité dont le partage ou l'invitation a disparu, ou dont l'invitation est passée en
// lecture seule (reprise après écriture ratée, bascule entre deux contrôles) ; le mode peut changer entre MarkSending et
// l'envoi, d'où le second appel. Rend l'invitation de l'auteur, d'où vient l'étiquette.
func dropRevokedGuest(queue *queueStore, sessionID string, head QItem) (Invite, bool) {
	if head.Author != "guest" {
		return Invite{}, false
	}
	if shareSt != nil {
		if share, isShared := shareSt.Get(sessionID); isShared {
			if invite, isInvited := share.invite(head.Guest); isInvited && head.Guest != "" && invite.Mode == "rw" {
				return invite, false
			}
		}
	}
	queue.Done(sessionID, head.ID)
	journal("file: message invité retiré (partage ou invitation absent, ou en lecture seule)")
	return Invite{}, true
}

// visibleQueue : un invité (en-têtes posés par sa garde) ne voit que les éléments de l'hôte et les siens, ni le texte, ni le
// libellé, ni l'invitation d'un autre invité.
func visibleQueue(request *http.Request, items []QItem) []QItem {
	if request.Header.Get(authorHeader) != "guest" {
		return items
	}
	ownInvite := request.Header.Get(inviteHeader)
	return slices.DeleteFunc(items, func(queuedItem QItem) bool {
		return queuedItem.Author == "guest" && (ownInvite == "" || queuedItem.Guest != ownInvite)
	})
}

// guestLabels : libellés des invitations de la session, calculés à la réponse (jamais stockés dans la file).
func guestLabels(sessionID string) map[string]string {
	labels := map[string]string{}
	if shareSt != nil {
		if share, isShared := shareSt.Get(sessionID); isShared {
			for _, invite := range share.Invites {
				labels[invite.ID] = invite.label()
			}
		}
	}
	return labels
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func queueLoop(loopContext context.Context, queue *queueStore) {
	ticker := time.NewTicker(queueTick)
	defer ticker.Stop()
	for {
		select {
		case <-loopContext.Done():
			return
		case <-ticker.C:
			drainOnce(queue, time.Now())
		}
	}
}

func routesQueue(mux *http.ServeMux) {
	queue, openErr := openQueue(queueDir())
	if openErr != nil {
		journal("queue : ouverture impossible : %v", openErr) // un fichier illisible n'est jamais écrasé : la file reste indisponible
		queue = nil
	}
	queueSt = queue
	fail := func(responseWriter http.ResponseWriter, code int, slug, key string) {
		writeErr(responseWriter, code, slug, L(key))
	}
	// ready : l'identifiant de session du chemin, et la file ouverte.
	ready := func(responseWriter http.ResponseWriter, request *http.Request) (string, bool) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return "", false
		}
		if queueSt == nil {
			fail(responseWriter, 503, "unavailable", "srv.queue.indisponible")
			return "", false
		}
		return sessionID, true
	}
	mux.HandleFunc("GET /v1/sessions/{id}/queue", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := ready(responseWriter, request)
		if !found {
			return
		}
		type labeledItem struct {
			QItem
			Label string `json:"label,omitempty"`
		}
		labels, items := guestLabels(sessionID), []labeledItem{}
		for _, queuedItem := range visibleQueue(request, queueSt.List(sessionID)) {
			labeled := labeledItem{QItem: queuedItem}
			if queuedItem.Author == "guest" {
				labeled.Label = cmp.Or(labels[queuedItem.Guest], "Invité")
			}
			items = append(items, labeled)
		}
		writeJSON(responseWriter, 200, map[string]any{"items": items})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/queue", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := ready(responseWriter, request)
		if !found {
			return
		}
		var body struct{ Text string }
		if !decodeBody(responseWriter, request, &body, sendMax+1<<10) {
			return
		}
		// Auteur et invitation sont posés par la garde invité, qui retire toute valeur du client : le corps ne les choisit pas.
		author := "host"
		if request.Header.Get(authorHeader) == "guest" {
			author = "guest"
		}
		addedItem, addErr := queueSt.Add(sessionID, author, request.Header.Get(inviteHeader), body.Text)
		switch addErr {
		case nil:
			writeJSON(responseWriter, 201, map[string]any{"item": addedItem})
		case errQueueFull:
			fail(responseWriter, 409, "conflict", "srv.queue.pleine")
		case errQueueText:
			fail(responseWriter, 400, "bad_request", "srv.queue.texteRefuse")
		default:
			fail(responseWriter, 500, "internal", "srv.queue.indisponible")
		}
	})
	mux.HandleFunc("DELETE /v1/sessions/{id}/queue/{qid}", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := ready(responseWriter, request)
		if !found {
			return
		}
		removedItem, removeErr := queueSt.Remove(sessionID, request.PathValue("qid"))
		if errors.Is(removeErr, errQueueBusy) {
			fail(responseWriter, 409, "conflict", "srv.queue.envoiEnCours")
			return
		}
		if removeErr != nil {
			fail(responseWriter, 404, "not_found", "srv.queue.introuvable")
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": removedItem.Text})
	})
	mux.HandleFunc("PATCH /v1/sessions/{id}/queue/{qid}", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := ready(responseWriter, request)
		if !found {
			return
		}
		var body struct{ State string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		if body.State != "queued" {
			fail(responseWriter, 400, "bad_request", "srv.http_util.requeteInvalide")
			return
		}
		if !queueSt.Retry(sessionID, request.PathValue("qid")) {
			fail(responseWriter, 404, "not_found", "srv.queue.introuvable")
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
}
