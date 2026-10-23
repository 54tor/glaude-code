package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionsBientotSupprimees(t *testing.T) {
	h, cfg, _ := web(t)
	// 30 jours par défaut : une session de 29,5 jours est dans la fenêtre des 24 h, une de 10 jours non.
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	old := time.Now().Add(-29*24*time.Hour - 12*time.Hour)
	os.Chtimes(p, old, old)
	_, m, _ := call(t, h, "GET", "/cleanup-warning", "")
	l, _ := m["sessions"].([]any)
	if m["days"] != float64(30) || len(l) != 1 {
		t.Fatalf("fenêtre de 24 h : %v", m)
	}
	recent := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(p, recent, recent)
	_, m, _ = call(t, h, "GET", "/cleanup-warning", "")
	if l, _ := m["sessions"].([]any); len(l) != 0 {
		t.Fatalf("session récente signalée : %v", m)
	}
	// réglage de l'utilisateur : 7 jours, la même session (10 jours) est déjà au-delà de la limite
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"cleanupPeriodDays": 7}`), 0o600)
	_, m, _ = call(t, h, "GET", "/cleanup-warning", "")
	if l, _ := m["sessions"].([]any); m["days"] != float64(7) || len(l) != 1 {
		t.Fatalf("réglage 7 jours : %v", m)
	}
	// une valeur énorme (réglage « jamais ») ne déclenche aucun avertissement
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"cleanupPeriodDays": 3650000}`), 0o600)
	_, m, _ = call(t, h, "GET", "/cleanup-warning", "")
	if l, _ := m["sessions"].([]any); len(l) != 0 {
		t.Fatalf("valeur énorme : %v", m)
	}
}
