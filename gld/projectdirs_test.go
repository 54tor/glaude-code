package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncProjectDirsListsSiblingsOnEveryFolderOfAProject(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	folderA, folderB, folderOutside := t.TempDir(), t.TempDir(), t.TempDir()
	put(t, filepath.Join(cfg, "projects", encodeDirName(folderA), id1+".jsonl"), `{"cwd":"`+folderA+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", encodeDirName(folderB), id2+".jsonl"), `{"cwd":"`+folderB+`"}`+"\n", old)
	store, _ := json.Marshal(Store{Projects: []Project{{Name: "p", Dirs: []string{folderA, folderB}}}})
	put(t, groupsFile(), string(store), old)

	syncProjectDirs()
	syncProjectDirs() // idempotent

	for owner, sibling := range map[string]string{folderA: folderB, folderB: folderA} {
		content, err := os.ReadFile(filepath.Join(owner, ".claude", "settings.local.json"))
		if err != nil {
			t.Fatal(err)
		}
		if count := strings.Count(string(content), sibling); count != 1 {
			t.Errorf("valeurAttendue 1 occurrence de %s, valeurObtenue %d dans %s", sibling, count, content)
		}
		if strings.Contains(string(content), folderOutside) {
			t.Errorf("un dossier hors projet figure dans %s", content)
		}
	}
}
