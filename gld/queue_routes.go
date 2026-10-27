package main

// Routes de la file de messages et boucle qui la vide dans les sessions ouvertes (tmux).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Identique à FOLLOW_PREFIX de gld/ui/src/lib/queue.ts (un test le vérifie).
const followPrefix = "[Message en file d'attente : d'autres messages suivent. Ne t'arrête pas pour poser une question : note tes éventuelles questions et pose-les à la fin, une fois tous les messages traités.]\n\n"

// Étiquette obligatoire des messages invités : le texte collé ne commence jamais par « / », « ! » ou « # » (commande Claude Code)
// et l'auteur reste visible dans la conversation.
const guestLabel = "[Invité] "

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
		m := map[string]bool{}
		p, _ := sendable()
		for id := range p {
			m[id] = true
		}
		return m
	}
	askingFn = askingSessions
)

var (
	sentMu    sync.Mutex
	notBefore = map[string]time.Time{}
)

// markSent : un envoi vient d'avoir lieu (file ou hôte au clavier) ; la session doit avoir le temps de démarrer un tour.
func markSent(id string) {
	sentMu.Lock()
	notBefore[id] = time.Now().Add(queueSettle)
	sentMu.Unlock()
}

func drainOnce(q *queueStore, now time.Time) {
	sess := q.Sessions()
	if len(sess) == 0 {
		return
	}
	open, idle, asking := openFn(), toSet(waiting()), toSet(askingFn())
	for _, id := range sess {
		sentMu.Lock()
		wait := now.Before(notBefore[id])
		sentMu.Unlock()
		if wait || !open[id] || !idle[id] || asking[id] {
			continue
		}
		head, ok := q.Head(id)
		if !ok || q.MarkSending(id, head.ID) != nil {
			continue
		}
		text := head.Text
		if head.Author == "guest" {
			text = guestLabel + text // Add borne le texte invité : étiquette et préfixe tiennent toujours dans sendMax
		}
		// Un texte hôte proche de sendMax ne supporte pas le préfixe : il part sans, sinon sendToSession le refuserait indéfiniment.
		if len(q.List(id)) > 1 && !strings.HasPrefix(text, followPrefix) && len(followPrefix)+len(text) <= sendMax {
			text = followPrefix + text
		}
		if err := sendFn(id, text); err != nil {
			q.Requeue(id, head.ID)
			sentMu.Lock()
			notBefore[id] = now.Add(queueRetry)
			sentMu.Unlock()
			journal("queue %s : %v", id, err)
			continue
		}
		q.Done(id, head.ID)
		sentMu.Lock()
		notBefore[id] = now.Add(queueSettle)
		sentMu.Unlock()
	}
}

func toSet(l []string) map[string]bool {
	m := make(map[string]bool, len(l))
	for _, s := range l {
		m[s] = true
	}
	return m
}

func queueLoop(ctx context.Context, q *queueStore) {
	t := time.NewTicker(queueTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			drainOnce(q, time.Now())
		}
	}
}

func routesQueue(mux *http.ServeMux) {
	q, err := openQueue(queueDir())
	if err != nil {
		journal("queue : ouverture impossible : %v", err) // un fichier illisible n'est jamais écrasé : la file reste indisponible
		q = nil
	}
	queueSt = q
	fail := func(w http.ResponseWriter, code int, key string) {
		writeJSON(w, code, map[string]any{"ok": false, "out": L(key)})
	}
	body := func(w http.ResponseWriter, r *http.Request) (struct{ ID, Item, Text string }, bool) {
		var req struct{ ID, Item, Text string }
		if json.NewDecoder(io.LimitReader(r.Body, sendMax+1<<10)).Decode(&req) != nil || !idRe.MatchString(req.ID) {
			fail(w, 400, "srv.http_util.requeteInvalide")
			return req, false
		}
		if queueSt == nil {
			fail(w, 503, "srv.queue.indisponible")
			return req, false
		}
		return req, true
	}
	mux.HandleFunc("GET /queue", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if !idRe.MatchString(id) {
			fail(w, 400, "srv.http_util.requeteInvalide")
			return
		}
		if queueSt == nil {
			fail(w, 503, "srv.queue.indisponible")
			return
		}
		writeJSON(w, 200, map[string]any{"items": queueSt.List(id)})
	})
	mux.HandleFunc("POST /queue-add", func(w http.ResponseWriter, r *http.Request) {
		req, ok := body(w, r)
		if !ok {
			return
		}
		// X-Gld-Author est posé par la garde invité (qui écrase toute valeur du client) : le corps ne peut pas choisir l'auteur.
		author := "host"
		if r.Header.Get("X-Gld-Author") == "guest" {
			author = "guest"
		}
		it, err := queueSt.Add(req.ID, author, req.Text)
		switch err {
		case nil:
			writeJSON(w, 200, map[string]any{"ok": true, "item": it})
		case errQueueFull:
			fail(w, 409, "srv.queue.pleine")
		case errQueueText:
			fail(w, 400, "srv.queue.texteRefuse")
		default:
			fail(w, 500, "srv.queue.indisponible")
		}
	})
	mux.HandleFunc("POST /queue-rm", func(w http.ResponseWriter, r *http.Request) {
		req, ok := body(w, r)
		if !ok {
			return
		}
		it, err := queueSt.Remove(req.ID, req.Item)
		if errors.Is(err, errQueueBusy) {
			fail(w, 409, "srv.queue.envoiEnCours")
			return
		}
		if err != nil {
			fail(w, 404, "srv.queue.introuvable")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "text": it.Text})
	})
	mux.HandleFunc("POST /queue-retry", func(w http.ResponseWriter, r *http.Request) {
		req, ok := body(w, r)
		if !ok {
			return
		}
		if !queueSt.Retry(req.ID, req.Item) {
			fail(w, 404, "srv.queue.introuvable")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
