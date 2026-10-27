package main

import (
	"errors"
	"os"
	"strings"
	"testing"
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
