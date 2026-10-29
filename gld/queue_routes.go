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
func guestTag(in Invite) string {
	n := inviteName(in.Name)
	if n == "" {
		n = strconv.Itoa(in.Seq)
	}
	return "[Invité " + n + "] "
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
		m := map[string]bool{}
		p, _ := sendable()
		for id := range p {
			m[id] = true
		}
		return m
	}
	askingFn = askingSessions
	screenFn = sessionScreen
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
		if !ok {
			continue
		}
		inv, drop := dropRevokedGuest(q, id, head)
		if drop || q.MarkSending(id, head.ID) != nil {
			continue
		}
		text := head.Text
		if head.Author == "guest" {
			text = guestTag(inv) + text // Add borne le texte invité : étiquette et préfixe tiennent toujours dans sendMax
		}
		// Un texte hôte proche de sendMax ne supporte pas le préfixe : il part sans, sinon sendToSession le refuserait indéfiniment.
		if len(q.List(id)) > 1 && !strings.HasPrefix(text, followPrefix) && len(followPrefix)+len(text) <= sendMax {
			text = followPrefix + text
		}
		// askingFn est gardé 3 s : un dialogue d'autorisation ouvert depuis prendrait le texte et Entrée comme réponse.
		scr, err := screenFn(id)
		if err == nil && screenAsks(scr) {
			err = errQueueBusy
		}
		if err == nil {
			if _, drop := dropRevokedGuest(q, id, head); drop {
				continue
			}
		}
		if err == nil {
			err = sendFn(id, text)
		}
		if err != nil {
			q.Requeue(id, head.ID)
			sentMu.Lock()
			notBefore[id] = now.Add(queueRetry)
			sentMu.Unlock()
			if err != errQueueBusy {
				journal("queue %s : %v", id, err)
			}
			continue
		}
		q.Done(id, head.ID)
		sentMu.Lock()
		notBefore[id] = now.Add(queueSettle)
		sentMu.Unlock()
	}
}

// dropRevokedGuest retire un message invité dont le partage ou l'invitation a disparu, ou dont l'invitation est passée en
// lecture seule (reprise après écriture ratée, bascule entre deux contrôles) ; le mode peut changer entre MarkSending et
// l'envoi, d'où le second appel. Rend l'invitation de l'auteur, d'où vient l'étiquette.
func dropRevokedGuest(q *queueStore, id string, head QItem) (Invite, bool) {
	if head.Author != "guest" {
		return Invite{}, false
	}
	if shareSt != nil {
		if sh, ok := shareSt.Get(id); ok {
			if in, ok := sh.invite(head.Guest); ok && head.Guest != "" && in.Mode == "rw" {
				return in, false
			}
		}
	}
	q.Done(id, head.ID)
	journal("file: message invité retiré (partage ou invitation absent, ou en lecture seule)")
	return Invite{}, true
}

// visibleQueue : un invité (en-têtes posés par sa garde) ne voit que les éléments de l'hôte et les siens, ni le texte, ni le
// libellé, ni l'invitation d'un autre invité.
func visibleQueue(r *http.Request, items []QItem) []QItem {
	if r.Header.Get(authorHeader) != "guest" {
		return items
	}
	me := r.Header.Get(inviteHeader)
	return slices.DeleteFunc(items, func(it QItem) bool { return it.Author == "guest" && (me == "" || it.Guest != me) })
}

// guestLabels : libellés des invitations de la session, calculés à la réponse (jamais stockés dans la file).
func guestLabels(id string) map[string]string {
	m := map[string]string{}
	if shareSt != nil {
		if sh, ok := shareSt.Get(id); ok {
			for _, in := range sh.Invites {
				m[in.ID] = in.label()
			}
		}
	}
	return m
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
	fail := func(w http.ResponseWriter, code int, slug, key string) {
		writeErr(w, code, slug, L(key))
	}
	// ready : l'identifiant de session du chemin, et la file ouverte.
	ready := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return "", false
		}
		if queueSt == nil {
			fail(w, 503, "unavailable", "srv.queue.indisponible")
			return "", false
		}
		return id, true
	}
	mux.HandleFunc("GET /v1/sessions/{id}/queue", func(w http.ResponseWriter, r *http.Request) {
		id, ok := ready(w, r)
		if !ok {
			return
		}
		type item struct {
			QItem
			Label string `json:"label,omitempty"`
		}
		labels, out := guestLabels(id), []item{}
		for _, it := range visibleQueue(r, queueSt.List(id)) {
			v := item{QItem: it}
			if it.Author == "guest" {
				v.Label = cmp.Or(labels[it.Guest], "Invité")
			}
			out = append(out, v)
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/queue", func(w http.ResponseWriter, r *http.Request) {
		id, ok := ready(w, r)
		if !ok {
			return
		}
		var req struct{ Text string }
		if !decodeBody(w, r, &req, sendMax+1<<10) {
			return
		}
		// Auteur et invitation sont posés par la garde invité, qui retire toute valeur du client : le corps ne les choisit pas.
		author := "host"
		if r.Header.Get(authorHeader) == "guest" {
			author = "guest"
		}
		it, err := queueSt.Add(id, author, r.Header.Get(inviteHeader), req.Text)
		switch err {
		case nil:
			writeJSON(w, 201, map[string]any{"item": it})
		case errQueueFull:
			fail(w, 409, "conflict", "srv.queue.pleine")
		case errQueueText:
			fail(w, 400, "bad_request", "srv.queue.texteRefuse")
		default:
			fail(w, 500, "internal", "srv.queue.indisponible")
		}
	})
	mux.HandleFunc("DELETE /v1/sessions/{id}/queue/{qid}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := ready(w, r)
		if !ok {
			return
		}
		it, err := queueSt.Remove(id, r.PathValue("qid"))
		if errors.Is(err, errQueueBusy) {
			fail(w, 409, "conflict", "srv.queue.envoiEnCours")
			return
		}
		if err != nil {
			fail(w, 404, "not_found", "srv.queue.introuvable")
			return
		}
		writeJSON(w, 200, map[string]any{"text": it.Text})
	})
	mux.HandleFunc("PATCH /v1/sessions/{id}/queue/{qid}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := ready(w, r)
		if !ok {
			return
		}
		var req struct{ State string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if req.State != "queued" {
			fail(w, 400, "bad_request", "srv.http_util.requeteInvalide")
			return
		}
		if !queueSt.Retry(id, r.PathValue("qid")) {
			fail(w, 404, "not_found", "srv.queue.introuvable")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
