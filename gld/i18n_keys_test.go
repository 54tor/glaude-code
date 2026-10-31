package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Chaque identifiant littéral passé à L, terr, tr ou isMsg existe dans les deux catalogues.
func TestEveryCatalogKeyUsedExists(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`\b(?:L|terr)\("([A-Za-z0-9_.]+)"[,)]|\btr\([^,()]+,\s*"([A-Za-z0-9_.]+)"|\bisMsg\([^,()]+,\s*"([A-Za-z0-9_.]+)"`)
	have := map[string]bool{}
	for _, line := range []string{"fr", "en"} {
		for _, key := range trKeys(line) {
			have[line+" "+key] = true
		}
	}
	used := 0
	for _, filePath := range files {
		if strings.HasSuffix(filePath, "_test.go") {
			continue
		}
		data, _ := os.ReadFile(filePath)
		for _, values := range re.FindAllStringSubmatch(string(data), -1) {
			id := values[1] + values[2] + values[3]
			used++
			for _, line := range []string{"fr", "en"} {
				if !have[line+" "+id] {
					t.Errorf("%s : %q absent du catalogue %s", filePath, id, line)
				}
			}
		}
	}
	if used < 250 {
		t.Fatalf("seulement %d identifiants trouvés : l'expression ne repère plus les appels", used)
	}
}
