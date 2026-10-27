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
	if got, err := q.Remove("s", a.ID); err != nil || got.Text != "un" {
		t.Fatalf("Remove = %+v %v", got, err)
	}
	if _, err := q.Remove("s", a.ID); err == nil {
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
	oSend, oOpen, oAsk, oWait, oScreen, oShare := sendFn, openFn, askingFn, waiting, screenFn, shareSt
	shareSt = &shareStore{m: map[string]*Share{}}
	for _, id := range open {
		shareSt.m[id] = &Share{Session: id, Mode: "rw"}
	}
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
	screenFn = func(string) (string, error) { return "", nil }
	sentMu.Lock()
	notBefore = map[string]time.Time{}
	sentMu.Unlock()
	t.Cleanup(func() {
		sendFn, openFn, askingFn, waiting, screenFn, shareSt = oSend, oOpen, oAsk, oWait, oScreen, oShare
	})
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

func TestQueueRemoveByAuthor(t *testing.T) {
	dir := t.TempDir()
	q, _ := openQueue(dir)
	q.Add("s", "guest", "a")
	q.Add("s", "host", "b")
	q.Add("t", "guest", "c")
	if n, err := q.RemoveByAuthor("s", "guest", false); n != 1 || err != nil {
		t.Fatalf("retirés : %d", n)
	}
	if l := q.List("s"); len(l) != 1 || l[0].Author != "host" {
		t.Fatalf("s : %+v", l)
	}
	if len(q.List("t")) != 1 {
		t.Fatal("autre session touchée")
	}
	q2, _ := openQueue(dir)
	if len(q2.List("s")) != 1 {
		t.Fatal("non persisté")
	}
}

func TestQueueRemoveRefusesSending(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	a, _ := q.Add("s", "host", "un")
	if err := q.MarkSending("s", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Remove("s", a.ID); !errors.Is(err, errQueueBusy) {
		t.Fatalf("Remove d'un item en cours d'envoi = %v", err)
	}
	if len(q.List("s")) != 1 {
		t.Fatal("l'item en cours d'envoi a disparu")
	}
	q.Done("s", a.ID) // le drain, lui, le retire une fois l'envoi fini
	if len(q.List("s")) != 0 {
		t.Fatal("Done n'a pas retiré l'item")
	}
	if _, err := q.Remove("s", a.ID); !errors.Is(err, errQueueGone) {
		t.Fatalf("Remove absent = %v", err)
	}
}

func TestDrainGuestLabel(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	q.Add("s", "guest", "/exit")
	drainOnce(q, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|"+guestLabel+"/exit" {
		t.Fatalf("sent=%q", *sent)
	}
	q.Add("s", "guest", "/clear")
	q.Add("s", "host", "/compact")
	drainOnce(q, time.Now().Add(5*time.Second))
	drainOnce(q, time.Now().Add(10*time.Second))
	if len(*sent) != 3 || (*sent)[1] != "s|"+followPrefix+guestLabel+"/clear" || (*sent)[2] != "s|/compact" {
		t.Fatalf("sent=%q", *sent)
	}
}

func TestQueueGuestTextLimit(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	max := sendMax - len(guestLabel) - len(followPrefix)
	if _, err := q.Add("s", "guest", strings.Repeat("x", max+1)); err != errQueueText {
		t.Fatalf("un octet de trop accepté : %v", err)
	}
	big := strings.Repeat("x", max)
	if _, err := q.Add("s", "guest", big); err != nil {
		t.Fatal(err)
	}
	q.Add("s", "host", "suite")
	drainOnce(q, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|"+followPrefix+guestLabel+big || len((*sent)[0])-2 > sendMax {
		t.Fatalf("envoi = %d octets", len((*sent)[0]))
	}
	if _, err := q.Add("s", "host", strings.Repeat("x", sendMax)); err != nil {
		t.Fatal("hôte limité comme un invité")
	}
}

func TestDrainFreshScreenCheck(t *testing.T) {
	dialog := "Bash command\n  rm -rf x\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel"
	for name, scr := range map[string]struct {
		text string
		err  error
	}{"question": {dialog, nil}, "erreur": {"", errors.New("tmux")}} {
		q, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
		screenFn = func(string) (string, error) { return scr.text, scr.err }
		q.Add("s", "guest", "oui")
		now := time.Now()
		drainOnce(q, now)
		if len(*sent) != 0 || q.List("s")[0].State != "queued" {
			t.Fatalf("%s : envoi %v list=%+v", name, *sent, q.List("s"))
		}
		screenFn = func(string) (string, error) { return "> ", nil }
		drainOnce(q, now.Add(9*time.Second))
		if len(*sent) != 0 {
			t.Fatalf("%s : nouvel essai avant 10 s", name)
		}
		drainOnce(q, now.Add(11*time.Second))
		if len(*sent) != 1 {
			t.Fatalf("%s : pas envoyé une fois l'écran libre", name)
		}
	}
}

func TestQueueOpenNullFile(t *testing.T) {
	for _, body := range []string{"null", `{"s":null}`} {
		dir := t.TempDir()
		os.WriteFile(dir+"/queue.json", []byte(body), 0o600)
		q, err := openQueue(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Add("s", "host", "x"); err != nil || len(q.List("s")) != 1 {
			t.Fatalf("%s : %v", body, err)
		}
	}
}

func drainShareEnv(t *testing.T, mode string) *shareStore {
	t.Helper()
	old := shareSt
	t.Cleanup(func() { shareSt = old })
	shareSt = nil
	if mode == "" {
		return nil
	}
	s, _ := newShares(t)
	if _, err := s.Create(shSess, "dir", ""); err != nil {
		t.Fatal(err)
	}
	if mode != "rw" {
		s.SetMode(shSess, mode)
	}
	shareSt = s
	return s
}

func TestDrainDropsGuestItemWhenShareReadOnly(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	drainShareEnv(t, "ro")
	q.Add(shSess, "guest", "x")
	drainOnce(q, time.Now())
	if len(*sent) != 0 || len(q.List(shSess)) != 0 {
		t.Fatalf("sent=%v list=%v", *sent, q.List(shSess))
	}
}

func TestDrainDropsGuestItemWhenShareMissing(t *testing.T) {
	for _, mode := range []string{"", "none"} {
		q, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
		if mode == "none" {
			drainShareEnv(t, "rw")
			shareSt.End(shSess)
		} else {
			drainShareEnv(t, "")
		}
		q.Add(shSess, "guest", "x")
		drainOnce(q, time.Now())
		if len(*sent) != 0 || len(q.List(shSess)) != 0 {
			t.Fatalf("%q sent=%v list=%v", mode, *sent, q.List(shSess))
		}
	}
}

func TestDrainGuestItemSentWhenShareRw(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	drainShareEnv(t, "rw")
	q.Add(shSess, "guest", "x")
	drainOnce(q, time.Now())
	if len(*sent) != 1 || (*sent)[0] != shSess+"|"+guestLabel+"x" {
		t.Fatalf("sent=%v", *sent)
	}
}

func TestDrainHostItemUnaffectedByShareMode(t *testing.T) {
	for _, mode := range []string{"", "ro"} {
		q, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
		drainShareEnv(t, mode)
		q.Add(shSess, "host", "x")
		drainOnce(q, time.Now())
		if len(*sent) != 1 || (*sent)[0] != shSess+"|x" {
			t.Fatalf("%q sent=%v", mode, *sent)
		}
	}
}

func TestDrainDropsGuestItemWhenModeFlipsBeforeSend(t *testing.T) {
	q, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	s := drainShareEnv(t, "rw")
	screenFn = func(string) (string, error) { s.SetMode(shSess, "ro"); return "", nil }
	q.Add(shSess, "guest", "x")
	drainOnce(q, time.Now())
	if len(*sent) != 0 || len(q.List(shSess)) != 0 {
		t.Fatalf("sent=%v list=%v", *sent, q.List(shSess))
	}
}
