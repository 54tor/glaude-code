package main

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func lowArgon(t *testing.T) {
	m, tt, p := argonMemKiB, argonTime, argonThreads
	argonMemKiB, argonTime, argonThreads = 1024, 1, 1
	t.Cleanup(func() { argonMemKiB, argonTime, argonThreads = m, tt, p })
}

func TestPasswordRoundTrip(t *testing.T) {
	lowArgon(t)
	h, err := hashPassword("correct horse battery")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("hash=%q err=%v", h, err)
	}
	if !checkPassword(h, "correct horse battery") || checkPassword(h, "wrong horse battery") {
		t.Fatal("round trip")
	}
	argonMemKiB, argonTime = 2048, 2
	if !checkPassword(h, "correct horse battery") {
		t.Fatal("params must come from the hash")
	}
	if checkPassword("garbage", "x") || checkPassword("$argon2id$v=19$m=0,t=1,p=1$AA$AA", "x") {
		t.Fatal("malformed hash accepted")
	}
}

func TestPasswordRules(t *testing.T) {
	cases := []struct {
		pw  string
		err error
	}{
		{"elevenchars", errPasswordShort},
		{strings.Repeat("a", 1025), errPasswordLong},
		{"password1234", errPasswordCommon},
		{"PASSWORD1234", errPasswordCommon},
		{"twelve\x00chars!", errPasswordNUL},
		{"éééééééééééé", nil},
		{"twelve chars", nil},
	}
	for _, c := range cases {
		if err := validPassword(c.pw); !errors.Is(err, c.err) {
			t.Errorf("%q: got %v want %v", c.pw, err, c.err)
		}
	}
}

func TestNormName(t *testing.T) {
	if normName("Éa") != normName("Éa") {
		t.Fatal("NFC/NFD differ")
	}
	if got := normName(" ＡＬＩＣＥ "); got != "alice" {
		t.Fatal(got)
	}
}

func TestDummyCheckTiming(t *testing.T) {
	lowArgon(t)
	argonMemKiB, argonTime = 16*1024, 2
	h, _ := hashPassword("correct horse battery")
	best := func(f func()) time.Duration {
		d := time.Hour
		for i := 0; i < 3; i++ {
			s := time.Now()
			f()
			if e := time.Since(s); e < d {
				d = e
			}
		}
		return d
	}
	real := best(func() { checkPassword(h, "wrong horse battery") })
	dummy := best(dummyCheck)
	if dummy*3 < real || real*3 < dummy {
		t.Fatalf("real=%v dummy=%v", real, dummy)
	}
}

func TestPasswordHashBounds(t *testing.T) {
	lowArgon(t)
	good, _ := hashPassword("correct horse battery")
	salt, key := "AAAAAAAAAAAAAAAAAAAAAA", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	mk := func(ver, params, s, k string) string { return "$argon2id$" + ver + "$" + params + "$" + s + "$" + k }
	bad := []string{
		mk("v=19", "m=4294967295,t=1,p=1", salt, key),
		mk("v=19", "m=1024,t=4294967295,p=1", salt, key),
		mk("v=19", "m=1024,t=1,p=255", salt, key),
		mk("v=19", "m=1048577,t=1,p=1", salt, key),
		mk("v=19", "m=1024,t=11,p=1", salt, key),
		mk("v=19", "m=1024,t=1,p=17", salt, key),
		mk("v=19", "m=1024,t=1,p=1", "", key),
		mk("v=19", "m=1024,t=1,p=1", "AAAA", key),
		mk("v=19", "m=1024,t=1,p=1", salt, strings.Repeat("A", 4000)),
		mk("v=19", "m=1024,t=1,p=1", salt, "AAAA"),
		mk("v=19", "m=64,t=1,p=1garbage", salt, key),
		mk("v=19", "m=+64,t=1,p=1", salt, key),
		mk("v=19", "m= 64,t=1,p=1", salt, key),
		mk("v=19", "m=64,t=1", salt, key),
		mk("v=19", "t=1,m=64,p=1", salt, key),
		mk("v=19", "m=64,t=1,p=1,x=1", salt, key),
		mk("v=18", "m=1024,t=1,p=1", salt, key),
		mk("v=19garbage", "m=1024,t=1,p=1", salt, key),
	}
	start := time.Now()
	for _, h := range bad {
		if checkPassword(h, "x") {
			t.Errorf("accepted %q", h)
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("too slow: %v", d)
	}
	if !checkPassword(good, "correct horse battery") {
		t.Fatal("good hash rejected")
	}
}

func TestArgonConcurrencyCapped(t *testing.T) {
	var cur, peak atomic.Int64
	old := argonIDKey
	argonIDKey = func(pw, salt []byte, tm, m uint32, p uint8, l uint32) []byte {
		n := cur.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(20 * time.Millisecond)
		cur.Add(-1)
		return make([]byte, l)
	}
	t.Cleanup(func() { argonIDKey = old })
	h, _ := hashPassword("x")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			switch i % 3 {
			case 0:
				hashPassword("x")
			case 1:
				checkPassword(h, "x")
			default:
				dummyCheck()
			}
		})
	}
	wg.Wait()
	if p := peak.Load(); p > int64(cap(argonSem)) || p < 2 {
		t.Fatalf("calculs simultanés : %d", p)
	}
}
