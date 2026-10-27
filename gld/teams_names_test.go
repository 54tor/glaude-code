package main

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var nameRe = regexp.MustCompile(`^[a-z]{2,12}$`)

func TestDrawNamesDistinct(t *testing.T) {
	got, err := drawNames(20, nil, 1)
	if err != nil || len(got) != 20 {
		t.Fatalf("got %v, %v", got, err)
	}
	seen := map[string]bool{}
	for _, n := range got {
		if !nameRe.MatchString(n) || seen[n] {
			t.Fatalf("invalid or duplicate %q in %v", n, got)
		}
		seen[n] = true
	}
}

func TestDrawNamesAvoidsTaken(t *testing.T) {
	first, err := drawNames(32, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	taken := []string{}
	for i := 0; i < 100; i++ {
		more, _ := drawNames(1, taken, 7+int64(i))
		taken = append(taken, strings.ToUpper(more[0]))
	}
	taken = append(taken, first...)
	got, err := drawNames(20, taken, 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		for _, tk := range taken {
			if strings.EqualFold(n, tk) {
				t.Fatalf("%q is taken", n)
			}
		}
	}
}

func TestDrawNamesDeterministic(t *testing.T) {
	a, _ := drawNames(10, nil, 42)
	b, _ := drawNames(10, nil, 42)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("%v != %v", a, b)
	}
}

func TestDrawNamesZero(t *testing.T) {
	for _, n := range []int{0, -3} {
		got, err := drawNames(n, nil, 1)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("n=%d: %v, %v", n, got, err)
		}
	}
	if got, _ := drawNames(100, nil, 1); len(got) != 32 {
		t.Fatalf("cap: %d", len(got))
	}
}
