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
	for _, name := range got {
		if !nameRe.MatchString(name) || seen[name] {
			t.Fatalf("invalid or duplicate %q in %v", name, got)
		}
		seen[name] = true
	}
}

func TestDrawNamesAvoidsTaken(t *testing.T) {
	first, err := drawNames(32, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	taken := []string{}
	for index := 0; index < 100; index++ {
		more, _ := drawNames(1, taken, 7+int64(index))
		taken = append(taken, strings.ToUpper(more[0]))
	}
	taken = append(taken, first...)
	got, err := drawNames(20, taken, 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range got {
		for _, tk := range taken {
			if strings.EqualFold(name, tk) {
				t.Fatalf("%q is taken", name)
			}
		}
	}
}

func TestDrawNamesDeterministic(t *testing.T) {
	values, _ := drawNames(10, nil, 42)
	localValues, _ := drawNames(10, nil, 42)
	if !reflect.DeepEqual(values, localValues) {
		t.Fatalf("%v != %v", values, localValues)
	}
}

func TestDrawNamesZero(t *testing.T) {
	for _, count := range []int{0, -3} {
		got, err := drawNames(count, nil, 1)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("n=%d: %v, %v", count, got, err)
		}
	}
	if got, _ := drawNames(100, nil, 1); len(got) != 32 {
		t.Fatalf("cap: %d", len(got))
	}
}
