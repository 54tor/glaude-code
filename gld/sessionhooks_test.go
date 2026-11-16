package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTempConfigDir(t *testing.T) string {
	t.Helper()
	previousRoot := root
	root = filepath.Join(t.TempDir(), "projects")
	t.Cleanup(func() { root = previousRoot })
	return filepath.Join(cfgDir(), "settings.json")
}

func TestSyncSessionHooksInstallsAndRemovesOnlyGldEntries(t *testing.T) {
	settingsPath := useTempConfigDir(t)
	userSettings := `{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo mien"}]}]}}`
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte(userSettings), 0o600)

	if changed, err := syncSessionHooks(true); err != nil || !changed {
		t.Fatalf("installation : changed=%v err=%v", changed, err)
	}
	installed, _ := os.ReadFile(settingsPath)
	if strings.Count(string(installed), gldEventCommandEscaped()) != len(gldEventHookEvents) || !strings.Contains(string(installed), "echo mien") {
		t.Fatalf("hooks de gld absents ou hook de l'utilisateur perdu :\n%s", installed)
	}
	if changed, _ := syncSessionHooks(true); changed {
		t.Fatal("une seconde installation ne doit rien réécrire")
	}

	if changed, err := syncSessionHooks(false); err != nil || !changed {
		t.Fatalf("désinstallation : changed=%v err=%v", changed, err)
	}
	removed, _ := os.ReadFile(settingsPath)
	if strings.Contains(string(removed), "_event") || !strings.Contains(string(removed), "echo mien") || !strings.Contains(string(removed), `"model"`) {
		t.Fatalf("la désinstallation doit ne retirer que les entrées de gld :\n%s", removed)
	}
}

func TestSyncSessionHooksNeverOverwritesUnreadableSettings(t *testing.T) {
	settingsPath := useTempConfigDir(t)
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{pas du json"), 0o600)
	if _, err := syncSessionHooks(true); err == nil {
		t.Fatal("un settings.json illisible doit être refusé")
	}
	if content, _ := os.ReadFile(settingsPath); string(content) != "{pas du json" {
		t.Fatalf("fichier modifié : %s", content)
	}
}

func TestSyncSessionHooksUninstallWithoutSettingsCreatesNothing(t *testing.T) {
	settingsPath := useTempConfigDir(t)
	if changed, err := syncSessionHooks(false); err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(settingsPath); err == nil {
		t.Fatal("la désinstallation ne doit pas créer settings.json")
	}
}

func gldEventCommandEscaped() string {
	return strings.ReplaceAll(gldEventCommand, `"`, `\"`)
}
