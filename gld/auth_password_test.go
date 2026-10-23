package main

import (
	"errors"
	"strings"
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
