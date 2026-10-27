package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestQueueAddListRemove(t *testing.T) {
	q, err := openQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if l := q.List("s"); l == nil || len(l) != 0 {
		t.Fatalf("List vide = %#v", l)
	}
	a, err := q.Add("s", "host", "un")
	if err != nil || a.ID == "" || a.State != "queued" || a.At == 0 {
		t.Fatalf("Add = %+v %v", a, err)
	}
	b, _ := q.Add("s", "guest", "deux")
	if l := q.List("s"); len(l) != 2 || l[0].ID != a.ID || l[1].ID != b.ID {
		t.Fatalf("List = %+v", l)
	}
	if h, ok := q.Head("s"); !ok || h.ID != a.ID {
		t.Fatalf("Head = %+v %v", h, ok)
	}
	if got, ok := q.Remove("s", a.ID); !ok || got.Text != "un" {
		t.Fatalf("Remove = %+v %v", got, ok)
	}
	if _, ok := q.Remove("s", a.ID); ok {
		t.Fatal("Remove deux fois")
	}
	if s := q.Sessions(); len(s) != 1 || s[0] != "s" {
		t.Fatalf("Sessions = %v", s)
	}
	q.Done("s", b.ID)
	if len(q.List("s")) != 0 || len(q.Sessions()) != 0 {
		t.Fatal("Done n'a pas vidé la file")
	}
}

func TestQueueLimits(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	for i := 0; i < 50; i++ {
		if _, err := q.Add("s", "host", "m"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.Add("s", "host", "m"); !errors.Is(err, errQueueFull) {
		t.Fatalf("51e = %v", err)
	}
	q2, _ := openQueue(t.TempDir())
	for _, txt := range []string{strings.Repeat("x", sendMax+1), "\x00", "a\xffb"} {
		if _, err := q2.Add("s", "host", txt); !errors.Is(err, errQueueText) {
			t.Errorf("%q : %v", txt, err)
		}
	}
	if len(q2.List("s")) != 0 {
		t.Fatal("file modifiée par un refus")
	}
}

func TestQueueRestartMarksUncertain(t *testing.T) {
	dir := t.TempDir()
	q, _ := openQueue(dir)
	a, _ := q.Add("s", "host", "un")
	q.Add("s", "host", "deux")
	if err := q.MarkSending("s", a.ID); err != nil {
		t.Fatal(err)
	}
	if l := q.List("s"); l[0].State != "sending" || l[0].SentAt == 0 {
		t.Fatalf("sending = %+v", l[0])
	}
	if _, ok := q.Head("s"); ok {
		t.Fatal("Head malgré sending")
	}
	q2, err := openQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l := q2.List("s"); len(l) != 2 || l[0].State != "uncertain" {
		t.Fatalf("après réouverture = %+v", l)
	}
	if _, ok := q2.Head("s"); ok {
		t.Fatal("Head malgré uncertain")
	}
}

func TestQueueRetry(t *testing.T) {
	dir := t.TempDir()
	q, _ := openQueue(dir)
	a, _ := q.Add("s", "host", "un")
	if q.Retry("s", a.ID) {
		t.Fatal("Retry sur queued")
	}
	if err := q.MarkSending("s", a.ID); err != nil {
		t.Fatal(err)
	}
	q, _ = openQueue(dir)
	if !q.Retry("s", a.ID) {
		t.Fatal("Retry refusé")
	}
	if h, ok := q.Head("s"); !ok || h.ID != a.ID || h.State != "queued" {
		t.Fatalf("Head = %+v %v", h, ok)
	}
}

func TestQueueMarkSendingCAS(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	a, _ := q.Add("s", "host", "un")
	b, _ := q.Add("s", "host", "deux")
	if err := q.MarkSending("s", a.ID); err != nil {
		t.Fatal(err)
	}
	if err := q.MarkSending("s", a.ID); !errors.Is(err, errQueueGone) {
		t.Fatalf("second = %v", err)
	}
	q.Remove("s", b.ID)
	if err := q.MarkSending("s", b.ID); !errors.Is(err, errQueueGone) {
		t.Fatalf("après Remove = %v", err)
	}
}

func TestQueueMarkSendingRollsBackOnWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignore les permissions")
	}
	dir := t.TempDir()
	q, _ := openQueue(dir)
	a, _ := q.Add("s", "host", "un")
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := q.MarkSending("s", a.ID); err == nil || errors.Is(err, errQueueGone) {
		t.Fatalf("erreur attendue, reçu %v", err)
	}
	if l := q.List("s"); l[0].State != "queued" || l[0].SentAt != 0 {
		t.Fatalf("pas de rollback : %+v", l[0])
	}
	os.Chmod(dir, 0o700)
	q2, _ := openQueue(dir)
	if l := q2.List("s"); l[0].State != "queued" {
		t.Fatalf("réouverture = %+v", l[0])
	}
}

func fakeDrainEnv(t *testing.T, open, idle, asking []string) (*[]string, *error) {
	t.Helper()
	oSend, oOpen, oAsk, oWait := sendFn, openFn, askingFn, waiting
	var sent []string
	var fail error
	sendFn = func(id, text string) error {
		if fail != nil {
			return fail
		}
		sent = append(sent, id+"|"+text)
		return nil
	}
	openFn = func() map[string]bool { return toSet(open) }
	askingFn = func() []string { return asking }
	waiting = func() []string { return idle }
	sentMu.Lock()
	notBefore = map[string]time.Time{}
	sentMu.Unlock()
	t.Cleanup(func() { sendFn, openFn, askingFn, waiting = oSend, oOpen, oAsk, oWait })
	return &sent, &fail
}

func TestDrainSendsHeadWhenIdle(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	q.Add("s", "host", "un")
	drainOnce(q, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|un" || len(q.List("s")) != 0 {
		t.Fatalf("sent=%v list=%v", *sent, q.List("s"))
	}
	q.Add("s", "host", "a")
	q.Add("s", "host", "b")
	drainOnce(q, time.Now()) // apaisement non écoulé
	if len(*sent) != 1 {
		t.Fatalf("envoi pendant l'apaisement : %v", *sent)
	}
	drainOnce(q, time.Now().Add(5*time.Second))
	if len(*sent) != 2 || (*sent)[1] != "s|"+followPrefix+"a" {
		t.Fatalf("préfixe attendu : %v", *sent)
	}
}

func TestDrainWaitsWhenBusyAsking(t *testing.T) {
	for name, env := range map[string][3][]string{
		"occupée": {{"s"}, {}, nil}, "question": {{"s"}, {"s"}, {"s"}}, "fermée": {{}, {"s"}, nil},
	} {
		q, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, env[0], env[1], env[2])
		q.Add("s", "host", "x")
		drainOnce(q, time.Now())
		if len(*sent) != 0 || len(q.List("s")) != 1 {
			t.Fatalf("%s : envoi %v", name, *sent)
		}
	}
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	q.Add("s", "host", "x")
	markSent("s")
	drainOnce(q, time.Now())
	if len(*sent) != 0 {
		t.Fatal("markSent n'a pas retardé le vidage")
	}
}

func TestDrainFailureKeepsItem(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	_, fail := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	*fail = errors.New("tmux")
	q.Add("s", "host", "x")
	now := time.Now()
	drainOnce(q, now)
	if l := q.List("s"); len(l) != 1 || l[0].State != "queued" {
		t.Fatalf("list=%+v", l)
	}
	*fail = nil
	drainOnce(q, now.Add(9*time.Second))
	if len(q.List("s")) != 1 {
		t.Fatal("nouvel essai avant 10 s")
	}
	drainOnce(q, now.Add(11*time.Second))
	if len(q.List("s")) != 0 {
		t.Fatal("pas de nouvel essai après 10 s")
	}
}

func TestDrainSkipsWhenMarkSendingFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignore les permissions")
	}
	dir := t.TempDir()
	q, _ := openQueue(dir)
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	q.Add("s", "host", "x")
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	drainOnce(q, time.Now())
	if len(*sent) != 0 {
		t.Fatalf("envoi malgré MarkSending en échec : %v", *sent)
	}
	if l := q.List("s"); len(l) != 1 || l[0].State != "queued" {
		t.Fatalf("list=%+v", l)
	}
}

func TestDrainOmitsPrefixWhenTooLong(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	big := strings.Repeat("x", sendMax)
	q.Add("s", "host", big)
	q.Add("s", "host", "suite")
	drainOnce(q, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|"+big || len(q.List("s")) != 1 {
		t.Fatalf("sent=%d list=%d", len(*sent), len(q.List("s")))
	}
}

func TestQueueRequeue(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	it, _ := q.Add("s", "host", "x")
	if q.Requeue("s", it.ID) {
		t.Fatal("Requeue d'un item queued")
	}
	q.MarkSending("s", it.ID)
	if !q.Requeue("s", it.ID) || q.List("s")[0].State != "queued" {
		t.Fatal("Requeue sending -> queued")
	}
}

const qid = "11111111-1111-1111-1111-111111111111"

func queueMux(t *testing.T) *http.ServeMux {
	t.Helper()
	oldSt, oldDir := queueSt, queueDir
	d := t.TempDir()
	queueDir = func() string { return d }
	t.Cleanup(func() { queueSt, queueDir = oldSt, oldDir })
	mux := http.NewServeMux()
	routesQueue(mux)
	return mux
}

func post(mux *http.ServeMux, path, body string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestQueueAddHandlerRejectsOversize(t *testing.T) {
	mux := queueMux(t)
	big, _ := json.Marshal(map[string]string{"id": qid, "text": strings.Repeat("x", sendMax+1)})
	if w := post(mux, "/queue-add", string(big)); w.Code != 400 || len(queueSt.List(qid)) != 0 {
		t.Fatalf("code=%d", w.Code)
	}
	if w := post(mux, "/queue-add", `{"id":"`+qid+`","text":"ok"}`, "X-Gld-Author", "guest"); w.Code != 200 || queueSt.List(qid)[0].Author != "guest" {
		t.Fatalf("code=%d", w.Code)
	}
	post(mux, "/queue-add", `{"id":"`+qid+`","text":"ok2","author":"guest"}`)
	if l := queueSt.List(qid); l[1].Author != "host" {
		t.Fatalf("auteur du corps pris en compte : %+v", l[1])
	}
	w := post(mux, "/queue-rm", `{"id":"`+qid+`","item":"`+queueSt.List(qid)[1].ID+`"}`)
	if !strings.Contains(w.Body.String(), `"text":"ok2"`) {
		t.Fatalf("rm = %s", w.Body)
	}
}

func TestSessionLiveHasPending(t *testing.T) {
	old := queueSt
	queueSt, _ = openQueue(t.TempDir())
	t.Cleanup(func() { queueSt = old })
	if st := sessionLive("", qid); st.Pending == nil || len(st.Pending) != 0 {
		t.Fatalf("pending vide = %#v", st.Pending)
	}
	queueSt.Add(qid, "host", "x")
	if st := sessionLive("", qid); len(st.Pending) != 1 {
		t.Fatalf("pending = %+v", st.Pending)
	}
}

func TestFollowPrefixMatchesUI(t *testing.T) {
	b, err := os.ReadFile("ui/src/lib/queue.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "FOLLOW_PREFIX = "+strconv.Quote(followPrefix)) {
		t.Fatal("followPrefix diffère de FOLLOW_PREFIX")
	}
}
