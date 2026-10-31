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
	queueStore, err := openQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if qItems := queueStore.List("s"); qItems == nil || len(qItems) != 0 {
		t.Fatalf("List vide = %#v", qItems)
	}
	qItem, err := queueStore.Add("s", "host", "", "un")
	if err != nil || qItem.ID == "" || qItem.State != "queued" || qItem.At == 0 {
		t.Fatalf("Add = %+v %v", qItem, err)
	}
	localQItem, _ := queueStore.Add("s", "guest", gInv, "deux")
	if qItems := queueStore.List("s"); len(qItems) != 2 || qItems[0].ID != qItem.ID || qItems[1].ID != localQItem.ID {
		t.Fatalf("List = %+v", qItems)
	}
	if localQItem, ok := queueStore.Head("s"); !ok || localQItem.ID != qItem.ID {
		t.Fatalf("Head = %+v %v", localQItem, ok)
	}
	if got, err := queueStore.Remove("s", qItem.ID); err != nil || got.Text != "un" {
		t.Fatalf("Remove = %+v %v", got, err)
	}
	if _, err := queueStore.Remove("s", qItem.ID); err == nil {
		t.Fatal("Remove deux fois")
	}
	if values := queueStore.Sessions(); len(values) != 1 || values[0] != "s" {
		t.Fatalf("Sessions = %v", values)
	}
	queueStore.Done("s", localQItem.ID)
	if len(queueStore.List("s")) != 0 || len(queueStore.Sessions()) != 0 {
		t.Fatal("Done n'a pas vidé la file")
	}
}

func TestQueueLimits(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	for index := 0; index < 50; index++ {
		if _, err := queueStore.Add("s", "host", "", "m"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := queueStore.Add("s", "host", "", "m"); !errors.Is(err, errQueueFull) {
		t.Fatalf("51e = %v", err)
	}
	q2, _ := openQueue(t.TempDir())
	for _, txt := range []string{strings.Repeat("x", sendMax+1), "\x00", "a\xffb"} {
		if _, err := q2.Add("s", "host", "", txt); !errors.Is(err, errQueueText) {
			t.Errorf("%q : %v", txt, err)
		}
	}
	if len(q2.List("s")) != 0 {
		t.Fatal("file modifiée par un refus")
	}
}

func TestQueueRestartMarksUncertain(t *testing.T) {
	dir := t.TempDir()
	queueStore, _ := openQueue(dir)
	qItem, _ := queueStore.Add("s", "host", "", "un")
	queueStore.Add("s", "host", "", "deux")
	if err := queueStore.MarkSending("s", qItem.ID); err != nil {
		t.Fatal(err)
	}
	if qItems := queueStore.List("s"); qItems[0].State != "sending" || qItems[0].SentAt == 0 {
		t.Fatalf("sending = %+v", qItems[0])
	}
	if _, ok := queueStore.Head("s"); ok {
		t.Fatal("Head malgré sending")
	}
	q2, err := openQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	if qItems := q2.List("s"); len(qItems) != 2 || qItems[0].State != "uncertain" {
		t.Fatalf("après réouverture = %+v", qItems)
	}
	if _, ok := q2.Head("s"); ok {
		t.Fatal("Head malgré uncertain")
	}
}

func TestQueueRetry(t *testing.T) {
	dir := t.TempDir()
	queueStore, _ := openQueue(dir)
	qItem, _ := queueStore.Add("s", "host", "", "un")
	if queueStore.Retry("s", qItem.ID) {
		t.Fatal("Retry sur queued")
	}
	if err := queueStore.MarkSending("s", qItem.ID); err != nil {
		t.Fatal(err)
	}
	queueStore, _ = openQueue(dir)
	if !queueStore.Retry("s", qItem.ID) {
		t.Fatal("Retry refusé")
	}
	if localQItem, ok := queueStore.Head("s"); !ok || localQItem.ID != qItem.ID || localQItem.State != "queued" {
		t.Fatalf("Head = %+v %v", localQItem, ok)
	}
}

func TestQueueMarkSendingCAS(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	qItem, _ := queueStore.Add("s", "host", "", "un")
	localQItem, _ := queueStore.Add("s", "host", "", "deux")
	if err := queueStore.MarkSending("s", qItem.ID); err != nil {
		t.Fatal(err)
	}
	if err := queueStore.MarkSending("s", qItem.ID); !errors.Is(err, errQueueGone) {
		t.Fatalf("second = %v", err)
	}
	queueStore.Remove("s", localQItem.ID)
	if err := queueStore.MarkSending("s", localQItem.ID); !errors.Is(err, errQueueGone) {
		t.Fatalf("après Remove = %v", err)
	}
}

func TestQueueMarkSendingRollsBackOnWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignore les permissions")
	}
	dir := t.TempDir()
	queueStore, _ := openQueue(dir)
	qItem, _ := queueStore.Add("s", "host", "", "un")
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := queueStore.MarkSending("s", qItem.ID); err == nil || errors.Is(err, errQueueGone) {
		t.Fatalf("erreur attendue, reçu %v", err)
	}
	if qItems := queueStore.List("s"); qItems[0].State != "queued" || qItems[0].SentAt != 0 {
		t.Fatalf("pas de rollback : %+v", qItems[0])
	}
	os.Chmod(dir, 0o700)
	q2, _ := openQueue(dir)
	if qItems := q2.List("s"); qItems[0].State != "queued" {
		t.Fatalf("réouverture = %+v", qItems[0])
	}
}

// gInv : l'invitation des partages posés par fakeDrainEnv.
const gInv = "00000000000000aa"

func fakeDrainEnv(t *testing.T, open, idle, asking []string) (*[]string, *error) {
	t.Helper()
	oSend, oOpen, oAsk, oWait, oScreen, oShare := sendFn, openFn, askingFn, waiting, screenFn, shareSt
	shareSt = &shareStore{m: map[string]*Share{}}
	for _, id := range open {
		shareSt.m[id] = &Share{Session: id, Seq: 1, Invites: []Invite{{ID: gInv, Seq: 1, Mode: "rw"}}}
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
	queueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	queueStore.Add("s", "host", "", "un")
	drainOnce(queueStore, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|un" || len(queueStore.List("s")) != 0 {
		t.Fatalf("sent=%v list=%v", *sent, queueStore.List("s"))
	}
	queueStore.Add("s", "host", "", "a")
	queueStore.Add("s", "host", "", "b")
	drainOnce(queueStore, time.Now()) // apaisement non écoulé
	if len(*sent) != 1 {
		t.Fatalf("envoi pendant l'apaisement : %v", *sent)
	}
	drainOnce(queueStore, time.Now().Add(5*time.Second))
	if len(*sent) != 2 || (*sent)[1] != "s|"+followPrefix+"a" {
		t.Fatalf("préfixe attendu : %v", *sent)
	}
}

func TestDrainWaitsWhenBusyAsking(t *testing.T) {
	for name, env := range map[string][3][]string{
		"occupée": {{"s"}, {}, nil}, "question": {{"s"}, {"s"}, {"s"}}, "fermée": {{}, {"s"}, nil},
	} {
		queueStore, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, env[0], env[1], env[2])
		queueStore.Add("s", "host", "", "x")
		drainOnce(queueStore, time.Now())
		if len(*sent) != 0 || len(queueStore.List("s")) != 1 {
			t.Fatalf("%s : envoi %v", name, *sent)
		}
	}
	localQueueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	localQueueStore.Add("s", "host", "", "x")
	markSent("s")
	drainOnce(localQueueStore, time.Now())
	if len(*sent) != 0 {
		t.Fatal("markSent n'a pas retardé le vidage")
	}
}

func TestDrainFailureKeepsItem(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	_, fail := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	*fail = errors.New("tmux")
	queueStore.Add("s", "host", "", "x")
	now := time.Now()
	drainOnce(queueStore, now)
	if qItems := queueStore.List("s"); len(qItems) != 1 || qItems[0].State != "queued" {
		t.Fatalf("list=%+v", qItems)
	}
	*fail = nil
	drainOnce(queueStore, now.Add(9*time.Second))
	if len(queueStore.List("s")) != 1 {
		t.Fatal("nouvel essai avant 10 s")
	}
	drainOnce(queueStore, now.Add(11*time.Second))
	if len(queueStore.List("s")) != 0 {
		t.Fatal("pas de nouvel essai après 10 s")
	}
}

func TestDrainSkipsWhenMarkSendingFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignore les permissions")
	}
	dir := t.TempDir()
	queueStore, _ := openQueue(dir)
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	queueStore.Add("s", "host", "", "x")
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	drainOnce(queueStore, time.Now())
	if len(*sent) != 0 {
		t.Fatalf("envoi malgré MarkSending en échec : %v", *sent)
	}
	if qItems := queueStore.List("s"); len(qItems) != 1 || qItems[0].State != "queued" {
		t.Fatalf("list=%+v", qItems)
	}
}

func TestDrainOmitsPrefixWhenTooLong(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	big := strings.Repeat("x", sendMax)
	queueStore.Add("s", "host", "", big)
	queueStore.Add("s", "host", "", "suite")
	drainOnce(queueStore, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|"+big || len(queueStore.List("s")) != 1 {
		t.Fatalf("sent=%d list=%d", len(*sent), len(queueStore.List("s")))
	}
}

func TestQueueRequeue(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	it, _ := queueStore.Add("s", "host", "", "x")
	if queueStore.Requeue("s", it.ID) {
		t.Fatal("Requeue d'un item queued")
	}
	queueStore.MarkSending("s", it.ID)
	if !queueStore.Requeue("s", it.ID) || queueStore.List("s")[0].State != "queued" {
		t.Fatal("Requeue sending -> queued")
	}
}

const qid = "11111111-1111-1111-1111-111111111111"

func queueMux(t *testing.T) *http.ServeMux {
	t.Helper()
	oldSt, oldDir := queueSt, queueDir
	dir := t.TempDir()
	queueDir = func() string { return dir }
	t.Cleanup(func() { queueSt, queueDir = oldSt, oldDir })
	mux := http.NewServeMux()
	routesQueue(mux)
	return mux
}

func post(mux *http.ServeMux, path, body string, hdr ...string) *httptest.ResponseRecorder {
	return send(mux, "POST", path, body, hdr...)
}

func send(mux *http.ServeMux, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for index := 0; index+1 < len(hdr); index += 2 {
		request.Header.Set(hdr[index], hdr[index+1])
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func TestQueueAddHandlerRejectsOversize(t *testing.T) {
	mux := queueMux(t)
	big, _ := json.Marshal(map[string]string{"text": strings.Repeat("x", sendMax+1)})
	if recorder := post(mux, "/v1/sessions/"+qid+"/queue", string(big)); recorder.Code != 400 || len(queueSt.List(qid)) != 0 {
		t.Fatalf("code=%d", recorder.Code)
	}
	if recorder := post(mux, "/v1/sessions/"+qid+"/queue", `{"text":"ok"}`, authorHeader, "guest", inviteHeader, gInv); recorder.Code != 201 || queueSt.List(qid)[0].Guest != gInv {
		t.Fatalf("code=%d", recorder.Code)
	}
	post(mux, "/v1/sessions/"+qid+"/queue", `{"text":"ok2","author":"guest"}`)
	if qItems := queueSt.List(qid); qItems[1].Author != "host" {
		t.Fatalf("auteur du corps pris en compte : %+v", qItems[1])
	}
	localRecorder := send(mux, "DELETE", "/v1/sessions/"+qid+"/queue/"+queueSt.List(qid)[1].ID, "")
	if !strings.Contains(localRecorder.Body.String(), `"text":"ok2"`) {
		t.Fatalf("rm = %s", localRecorder.Body)
	}
}

func TestSessionLiveHasPending(t *testing.T) {
	old := queueSt
	queueSt, _ = openQueue(t.TempDir())
	t.Cleanup(func() { queueSt = old })
	if st := sessionLive("", qid); st.Pending == nil || len(st.Pending) != 0 {
		t.Fatalf("pending vide = %#v", st.Pending)
	}
	queueSt.Add(qid, "host", "", "x")
	if st := sessionLive("", qid); len(st.Pending) != 1 {
		t.Fatalf("pending = %+v", st.Pending)
	}
}

func TestFollowPrefixMatchesUI(t *testing.T) {
	data, err := os.ReadFile("ui/src/lib/queue.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "FOLLOW_PREFIX = "+strconv.Quote(followPrefix)) {
		t.Fatal("followPrefix diffère de FOLLOW_PREFIX")
	}
}

func TestQueueRemoveByGuest(t *testing.T) {
	dir := t.TempDir()
	queueStore, _ := openQueue(dir)
	queueStore.Add("s", "guest", gInv, "a")
	queueStore.Add("s", "guest", "00000000000000bb", "b2")
	queueStore.Add("s", "host", "", "b")
	queueStore.Add("t", "guest", gInv, "c")
	if count, err := queueStore.RemoveByGuest("s", gInv, false); count != 1 || err != nil {
		t.Fatalf("retirés : %d", count)
	}
	if count, err := queueStore.RemoveByGuest("s", "", false); count != 1 || err != nil {
		t.Fatalf("retirés : %d", count)
	}
	if qItems := queueStore.List("s"); len(qItems) != 1 || qItems[0].Author != "host" {
		t.Fatalf("s : %+v", qItems)
	}
	if len(queueStore.List("t")) != 1 {
		t.Fatal("autre session touchée")
	}
	q2, _ := openQueue(dir)
	if len(q2.List("s")) != 1 {
		t.Fatal("non persisté")
	}
}

func TestQueueRemoveRefusesSending(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	qItem, _ := queueStore.Add("s", "host", "", "un")
	if err := queueStore.MarkSending("s", qItem.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := queueStore.Remove("s", qItem.ID); !errors.Is(err, errQueueBusy) {
		t.Fatalf("Remove d'un item en cours d'envoi = %v", err)
	}
	if len(queueStore.List("s")) != 1 {
		t.Fatal("l'item en cours d'envoi a disparu")
	}
	queueStore.Done("s", qItem.ID) // le drain, lui, le retire une fois l'envoi fini
	if len(queueStore.List("s")) != 0 {
		t.Fatal("Done n'a pas retiré l'item")
	}
	if _, err := queueStore.Remove("s", qItem.ID); !errors.Is(err, errQueueGone) {
		t.Fatalf("Remove absent = %v", err)
	}
}

func TestDrainGuestLabel(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	queueStore.Add("s", "guest", gInv, "/exit")
	drainOnce(queueStore, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|[Invité 1] /exit" {
		t.Fatalf("sent=%q", *sent)
	}
	queueStore.Add("s", "guest", gInv, "/clear")
	queueStore.Add("s", "host", "", "/compact")
	drainOnce(queueStore, time.Now().Add(5*time.Second))
	drainOnce(queueStore, time.Now().Add(10*time.Second))
	if len(*sent) != 3 || (*sent)[1] != "s|"+followPrefix+"[Invité 1] /clear" || (*sent)[2] != "s|/compact" {
		t.Fatalf("sent=%q", *sent)
	}
}

func TestQueueGuestTextLimit(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
	max := sendMax - guestLabelMax - len(followPrefix)
	if _, err := queueStore.Add("s", "guest", gInv, strings.Repeat("x", max+1)); err != errQueueText {
		t.Fatalf("un octet de trop accepté : %v", err)
	}
	big := strings.Repeat("x", max)
	if _, err := queueStore.Add("s", "guest", gInv, big); err != nil {
		t.Fatal(err)
	}
	queueStore.Add("s", "host", "", "suite")
	drainOnce(queueStore, time.Now())
	if len(*sent) != 1 || (*sent)[0] != "s|"+followPrefix+"[Invité 1] "+big || len((*sent)[0])-2 > sendMax {
		t.Fatalf("envoi = %d octets", len((*sent)[0]))
	}
	if _, err := queueStore.Add("s", "host", "", strings.Repeat("x", sendMax)); err != nil {
		t.Fatal("hôte limité comme un invité")
	}
}

func TestDrainFreshScreenCheck(t *testing.T) {
	dialog := "Bash command\n  rm -rf x\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel"
	for name, scr := range map[string]struct {
		text string
		err  error
	}{"question": {dialog, nil}, "erreur": {"", errors.New("tmux")}} {
		queueStore, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, []string{"s"}, []string{"s"}, nil)
		screenFn = func(string) (string, error) { return scr.text, scr.err }
		queueStore.Add("s", "guest", gInv, "oui")
		now := time.Now()
		drainOnce(queueStore, now)
		if len(*sent) != 0 || queueStore.List("s")[0].State != "queued" {
			t.Fatalf("%s : envoi %v list=%+v", name, *sent, queueStore.List("s"))
		}
		screenFn = func(string) (string, error) { return "> ", nil }
		drainOnce(queueStore, now.Add(9*time.Second))
		if len(*sent) != 0 {
			t.Fatalf("%s : nouvel essai avant 10 s", name)
		}
		drainOnce(queueStore, now.Add(11*time.Second))
		if len(*sent) != 1 {
			t.Fatalf("%s : pas envoyé une fois l'écran libre", name)
		}
	}
}

func TestQueueOpenNullFile(t *testing.T) {
	for _, body := range []string{"null", `{"s":null}`} {
		dir := t.TempDir()
		os.WriteFile(dir+"/queue.json", []byte(body), 0o600)
		queueStore, err := openQueue(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := queueStore.Add("s", "host", "", "x"); err != nil || len(queueStore.List("s")) != 1 {
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
	localShareStore, _ := newShares(t)
	localShareStore.m[shSess] = &Share{Session: shSess, Seq: 1, Invites: []Invite{{ID: gInv, Seq: 1, Mode: mode}}}
	shareSt = localShareStore
	return localShareStore
}

func TestDrainDropsGuestItemWhenShareReadOnly(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	drainShareEnv(t, "ro")
	queueStore.Add(shSess, "guest", gInv, "x")
	drainOnce(queueStore, time.Now())
	if len(*sent) != 0 || len(queueStore.List(shSess)) != 0 {
		t.Fatalf("sent=%v list=%v", *sent, queueStore.List(shSess))
	}
}

func TestDrainDropsGuestItemWhenShareMissing(t *testing.T) {
	for _, mode := range []string{"", "none"} {
		queueStore, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
		if mode == "none" {
			drainShareEnv(t, "rw")
			shareSt.End(shSess)
		} else {
			drainShareEnv(t, "")
		}
		queueStore.Add(shSess, "guest", gInv, "x")
		drainOnce(queueStore, time.Now())
		if len(*sent) != 0 || len(queueStore.List(shSess)) != 0 {
			t.Fatalf("%q sent=%v list=%v", mode, *sent, queueStore.List(shSess))
		}
	}
}

func TestDrainGuestItemSentWhenShareRw(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	drainShareEnv(t, "rw")
	queueStore.Add(shSess, "guest", gInv, "x")
	drainOnce(queueStore, time.Now())
	if len(*sent) != 1 || (*sent)[0] != shSess+"|[Invité 1] x" {
		t.Fatalf("sent=%v", *sent)
	}
}

func TestDrainHostItemUnaffectedByShareMode(t *testing.T) {
	for _, mode := range []string{"", "ro"} {
		queueStore, _ := openQueue(t.TempDir())
		sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
		drainShareEnv(t, mode)
		queueStore.Add(shSess, "host", "", "x")
		drainOnce(queueStore, time.Now())
		if len(*sent) != 1 || (*sent)[0] != shSess+"|x" {
			t.Fatalf("%q sent=%v", mode, *sent)
		}
	}
}

func TestDrainDropsGuestItemWhenModeFlipsBeforeSend(t *testing.T) {
	queueStore, _ := openQueue(t.TempDir())
	sent, _ := fakeDrainEnv(t, []string{shSess}, []string{shSess}, nil)
	shareStore := drainShareEnv(t, "rw")
	screenFn = func(string) (string, error) { shareStore.SetInviteMode(shSess, gInv, "ro"); return "", nil }
	queueStore.Add(shSess, "guest", gInv, "x")
	drainOnce(queueStore, time.Now())
	if len(*sent) != 0 || len(queueStore.List(shSess)) != 0 {
		t.Fatalf("sent=%v list=%v", *sent, queueStore.List(shSess))
	}
}
