package main

import (
	"strings"
	"testing"
)

func TestMcpMask(t *testing.T) {
	if got := mcpMaskURL("https://u:p@h.example/x?key=SECRET"); strings.Contains(got, "SECRET") || strings.Contains(got, "u:p") {
		t.Errorf("url : %s", got)
	}
	got := mcpMaskArgs([]string{"-y", "srv", "--api-key", "SECRET", "--token=TOP", "--port", "80"})
	if strings.Contains(got, "SECRET") || strings.Contains(got, "TOP") || !strings.Contains(got, "--port 80") {
		t.Errorf("args : %s", got)
	}
}
