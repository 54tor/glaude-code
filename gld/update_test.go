package main

import (
	"context"
	"testing"
	"time"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v0.7.1", "v0.7.2", true}, {"v0.7.1", "v0.10.0", true}, {"v0.7.1", "v0.7.1", false},
		{"v0.8.0", "v0.7.9", false}, {"dev", "v9.9.9", false}, {"v0.7.1", "nightly", false},
		{"2.1.314", "2.1.316", true}, {"v0.8.1-rc1", "v0.8.1", true}, {"v0.8.1", "v0.8.1-rc1", false},
	} {
		if got := newerVersion(c.cur, c.latest); got != c.want {
			t.Errorf("newerVersion(%q,%q)=%v", c.cur, c.latest, got)
		}
	}
}

func TestCheckUpdateClaude(t *testing.T) {
	oldR, oldI, oldL, oldAt := latestRelease, claudeInstalled, claudeLatest, updAt
	t.Cleanup(func() {
		latestRelease, claudeInstalled, claudeLatest, updAt = oldR, oldI, oldL, oldAt
		updInfo = updateInfo{}
	})
	latestRelease = func(context.Context) (string, string, error) { return "", "", context.Canceled }
	claudeInstalled = func(context.Context) string { return "2.1.313" }
	claudeLatest = func(context.Context) (string, error) { return "2.1.316", nil }
	updAt = time.Time{}
	if i := checkUpdate(context.Background(), false); !i.ClaudeNewer || i.ClaudeCurrent != "2.1.313" || i.ClaudeLatest != "2.1.316" {
		t.Fatalf("claude périmé non signalé : %+v", i)
	}
	claudeInstalled = func(context.Context) string { return "2.1.314" }
	updAt = time.Time{}
	if i := checkUpdate(context.Background(), false); i.ClaudeNewer {
		t.Fatalf("retard de 2 patchs signalé : %+v", i)
	}
	claudeInstalled = func(context.Context) string { return "2.0.999" }
	updAt = time.Time{}
	if i := checkUpdate(context.Background(), false); !i.ClaudeNewer {
		t.Fatalf("retard de mineure non signalé : %+v", i)
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
