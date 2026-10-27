package main

import "testing"

func TestUpstreamLimit(t *testing.T) {
	if m := upstreamLimit.FindString("API Error: 529 overloaded"); m != "529" {
		t.Fatalf("trouvé %q", m)
	}
	if m := upstreamLimit.FindString("id 15290"); m != "" {
		t.Fatalf("faux positif %q", m)
	}
}
