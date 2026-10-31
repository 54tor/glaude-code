package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionsBientotSupprimees(t *testing.T) {
	handler, cfg, _ := web(t)
	// 30 jours par défaut : une session de 29,5 jours est dans la fenêtre des 24 h, une de 10 jours non.
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	old := time.Now().Add(-29*24*time.Hour - 12*time.Hour)
	os.Chtimes(path, old, old)
	_, lookup, _ := call(t, handler, "GET", "/v1/cleanup-warning", "")
	values, _ := lookup["sessions"].([]any)
	if lookup["days"] != float64(30) || len(values) != 1 {
		t.Fatalf("fenêtre de 24 h : %v", lookup)
	}
	recent := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(path, recent, recent)
	_, lookup, _ = call(t, handler, "GET", "/v1/cleanup-warning", "")
	if values, _ := lookup["sessions"].([]any); len(values) != 0 {
		t.Fatalf("session récente signalée : %v", lookup)
	}
	// réglage de l'utilisateur : 7 jours, la même session (10 jours) est déjà au-delà de la limite
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"cleanupPeriodDays": 7}`), 0o600)
	_, lookup, _ = call(t, handler, "GET", "/v1/cleanup-warning", "")
	if values, _ := lookup["sessions"].([]any); lookup["days"] != float64(7) || len(values) != 1 {
		t.Fatalf("réglage 7 jours : %v", lookup)
	}
	// une valeur énorme (réglage « jamais ») ne déclenche aucun avertissement
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"cleanupPeriodDays": 3650000}`), 0o600)
	_, lookup, _ = call(t, handler, "GET", "/v1/cleanup-warning", "")
	if values, _ := lookup["sessions"].([]any); len(values) != 0 {
		t.Fatalf("valeur énorme : %v", lookup)
	}
}
