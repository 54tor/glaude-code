package main

import "testing"

func TestNewWaiting(t *testing.T) {
	got := newWaiting(map[string]bool{"a": true}, []string{"a", "b"})
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("newWaiting = %v", got)
	}
}
