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
	total, tt, octet := argonMemKiB, argonTime, argonThreads
	argonMemKiB, argonTime, argonThreads = 1024, 1, 1
	t.Cleanup(func() { argonMemKiB, argonTime, argonThreads = total, tt, octet })
}

func TestPasswordRoundTrip(t *testing.T) {
	lowArgon(t)
	header, err := hashPassword("correct horse battery")
	if err != nil || !strings.HasPrefix(header, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("hash=%q err=%v", header, err)
	}
	if !checkPassword(header, "correct horse battery") || checkPassword(header, "wrong horse battery") {
		t.Fatal("round trip")
	}
	argonMemKiB, argonTime = 2048, 2
	if !checkPassword(header, "correct horse battery") {
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
	for _, record := range cases {
		if err := validPassword(record.pw); !errors.Is(err, record.err) {
			t.Errorf("%q: got %v want %v", record.pw, err, record.err)
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
	header, _ := hashPassword("correct horse battery")
	best := func(callback func()) time.Duration {
		duration := time.Hour
		for index := 0; index < 3; index++ {
			moment := time.Now()
			callback()
			if localDuration := time.Since(moment); localDuration < duration {
				duration = localDuration
			}
		}
		return duration
	}
	real := best(func() { checkPassword(header, "wrong horse battery") })
	dummy := best(dummyCheck)
	if dummy*3 < real || real*3 < dummy {
		t.Fatalf("real=%v dummy=%v", real, dummy)
	}
}

func TestPasswordHashBounds(t *testing.T) {
	lowArgon(t)
	good, _ := hashPassword("correct horse battery")
	salt, key := "AAAAAAAAAAAAAAAAAAAAAA", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	mk := func(ver, params, text, key string) string {
		return "$argon2id$" + ver + "$" + params + "$" + text + "$" + key
	}
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
	for _, header := range bad {
		if checkPassword(header, "x") {
			t.Errorf("accepted %q", header)
		}
	}
	if duration := time.Since(start); duration > time.Second {
		t.Fatalf("too slow: %v", duration)
	}
	if !checkPassword(good, "correct horse battery") {
		t.Fatal("good hash rejected")
	}
}

func TestArgonConcurrencyCapped(t *testing.T) {
	var cur, peak atomic.Int64
	old := argonIDKey
	argonIDKey = func(pw, salt []byte, tm, total uint32, octet uint8, length uint32) []byte {
		count := cur.Add(1)
		for port := peak.Load(); count > port && !peak.CompareAndSwap(port, count); port = peak.Load() {
		}
		time.Sleep(20 * time.Millisecond)
		cur.Add(-1)
		return make([]byte, length)
	}
	t.Cleanup(func() { argonIDKey = old })
	header, _ := hashPassword("x")
	var wg sync.WaitGroup
	for index := range 16 {
		wg.Go(func() {
			switch index % 3 {
			case 0:
				hashPassword("x")
			case 1:
				checkPassword(header, "x")
			default:
				dummyCheck()
			}
		})
	}
	wg.Wait()
	if port := peak.Load(); port > int64(cap(argonSem)) || port < 2 {
		t.Fatalf("calculs simultanés : %d", port)
	}
}
