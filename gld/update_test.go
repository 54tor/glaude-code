package main

import (
	"context"
	"testing"
	"time"
)

func TestCheckUpdateClaude(t *testing.T) {
	oldR, oldI, oldL, oldAt := latestRelease, claudeInstalled, claudeLatest, updAt
	t.Cleanup(func() {
		latestRelease, claudeInstalled, claudeLatest, updAt = oldR, oldI, oldL, oldAt
		updInfo = updateInfo{}
	})
	latestRelease = func(context.Context) (string, string, error) { return "", "", context.Canceled }
	claudeInstalled = func(context.Context) string { return "2.1.314" }
	claudeLatest = func(context.Context) (string, error) { return "2.1.316", nil }
	updAt = time.Time{}
	if i := checkUpdate(context.Background(), false); !i.ClaudeNewer || i.ClaudeCurrent != "2.1.314" || i.ClaudeLatest != "2.1.316" {
		t.Fatalf("claude périmé non signalé : %+v", i)
	}
	claudeInstalled = func(context.Context) string { return "2.1.316" }
	updAt = time.Time{}
	if i := checkUpdate(context.Background(), false); i.ClaudeNewer {
		t.Fatalf("à jour signalé périmé : %+v", i)
	}
	claudeInstalled = func(context.Context) string { return "" }
	updAt = time.Time{}
	if i := checkUpdate(context.Background(), false); i.ClaudeNewer || i.ClaudeLatest != "" {
		t.Fatalf("claude absent : %+v", i)
	}
}
