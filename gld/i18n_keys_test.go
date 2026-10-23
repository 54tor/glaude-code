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
	for _, l := range []string{"fr", "en"} {
		for _, k := range trKeys(l) {
			have[l+" "+k] = true
		}
	}
	used := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			id := m[1] + m[2] + m[3]
			used++
			for _, l := range []string{"fr", "en"} {
				if !have[l+" "+id] {
					t.Errorf("%s : %q absent du catalogue %s", f, id, l)
				}
			}
		}
	}
	if used < 250 {
		t.Fatalf("seulement %d identifiants trouvés : l'expression ne repère plus les appels", used)
	}
}
