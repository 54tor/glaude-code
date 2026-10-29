package main

import "testing"

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v0.7.1", "v0.7.2", true}, {"v0.7.1", "v0.10.0", true}, {"v0.7.1", "v0.7.1", false},
		{"v0.8.0", "v0.7.9", false}, {"dev", "v9.9.9", false}, {"v0.7.1", "nightly", false},
	} {
		if got := newerVersion(c.cur, c.latest); got != c.want {
			t.Errorf("newerVersion(%q,%q)=%v", c.cur, c.latest, got)
		}
	}
}
