package main

import "testing"

func TestUpstreamLimit(t *testing.T) {
	if message := upstreamLimit.FindString("API Error: 529 overloaded"); message != "529" {
		t.Fatalf("trouvé %q", message)
	}
	if message := upstreamLimit.FindString("id 15290"); message != "" {
		t.Fatalf("faux positif %q", message)
	}
}
