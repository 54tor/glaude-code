package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Cibles de fuzzing : `go test -run '^$' -fuzz '^FuzzX$' -fuzztime 60s .` ; sans -fuzz, seules les graines
// (et le corpus de testdata/fuzz) tournent, comme des tests ordinaires.

func decodeStd(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		t.Fatalf("relecture de %q : %v", data, err)
	}
	return value
}

// parseOrdered accepte exactement les documents JSON valides, donne le même contenu que encoding/json (dernière clé
// en double gagnante comprise), et formatOrdered s'y relit à l'identique.
func FuzzParseOrdered(fuzzer *testing.F) {
	for _, text := range []string{`{}`, `[]`, `{"b":1,"a":[true,null,"x"]}`, `{"a":1,"a":2}`, `1e400`, `"é\ud800"`,
		`{"a":{"b":{"c":[1.50,-0,1E2]}}}`, `{"<>&":" "}`, ` [1 , 2 ] `, `{"a":1}x`, `{"a" 1}`, `[1,]`, "\"\xff\""} {
		fuzzer.Add([]byte(text))
	}
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		value, err := parseOrdered(data)
		if (err == nil) != json.Valid(data) {
			t.Fatalf("parseOrdered(%q) : err=%v, json.Valid=%v", data, err, json.Valid(data))
		}
		if err != nil {
			return
		}
		out := formatOrdered(value)
		if !reflect.DeepEqual(decodeStd(t, []byte(out)), decodeStd(t, data)) {
			t.Fatalf("contenu changé : %q -> %q", data, out)
		}
		v2, err := parseOrdered([]byte(out))
		if err != nil || formatOrdered(v2) != out {
			t.Fatalf("aller-retour instable : %q -> %q (%v)", data, out, err)
		}
	})
}

// Masquer puis restaurer redonne le document d'origine ; le texte masqué ne contient la valeur d'aucune clé de secret.
func FuzzMaskRestore(fuzzer *testing.F) {
	for _, text := range []string{`{"env":{"API_KEY":"abc","B":"1"}}`, `{"token":["x","y"],"n":{"password":""}}`,
		`{"apiKeyHelper":"/bin/k","a":[{"auth":"z"}]}`, `{"secret":"••••••••"}`, `{"k":1}`} {
		fuzzer.Add([]byte(text))
	}
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		value, err := parseOrdered(data)
		if err != nil {
			return
		}
		want := formatOrdered(value)
		localValue := maskSecrets(value, "")
		var walk func(value any, key string)
		walk = func(value any, key string) {
			switch value := value.(type) {
			case *omap:
				for _, key := range value.keys {
					walk(value.m[key], key)
				}
			case []any:
				for _, localValue := range value {
					walk(localValue, key)
				}
			case string:
				if secretKey.MatchString(key) && value != "" && value != maskText {
					t.Fatalf("valeur de %q non masquée : %q", key, value)
				}
			}
		}
		walk(localValue, "")
		got, err := restoreMasked(localValue, value, "") // restaure m sur place
		if err != nil {
			t.Fatalf("restoreMasked(%q) : %v", data, err)
		}
		if formatOrdered(got) != want {
			t.Fatalf("aller-retour du masque : %q -> %q", want, formatOrdered(got))
		}
	})
}

// maskURL ne laisse jamais passer un mot de passe, ni un nom d'utilisateur en http(s).
func FuzzMaskURL(fuzzer *testing.F) {
	for _, text := range []string{"https://u:p@h/x", "https://ghp_x@github.com/a", "ssh://git:pw@h/a", "git@h:a/b.git",
		"https://a:p^w%@h/x", "HTTPS://u:p@h", "http://%41:%42@h", "file:///x", "https://u:p@[::1]:80/x?q=1#f", "ftp://a:b@c"} {
		fuzzer.Add(text)
	}
	fuzzer.Fuzz(func(t *testing.T, raw string) {
		got := maskURL(raw)
		uRL, err := url.Parse(raw)
		if err != nil {
			if index := strings.Index(raw, "://"); index >= 0 {
				auth := strings.SplitN(got[index+3:], "/", 2)[0]
				if strings.Contains(auth, "@") {
					t.Fatalf("userinfo gardé sur une URL illisible : %q -> %q", raw, got)
				}
			}
			return
		}
		if uRL.User == nil {
			if got != raw {
				t.Fatalf("URL sans identifiants modifiée : %q -> %q", raw, got)
			}
			return
		}
		localURL, err := url.Parse(got)
		if err != nil {
			t.Fatalf("maskURL(%q) = %q illisible : %v", raw, got, err)
		}
		if localURL.User != nil {
			if _, has := localURL.User.Password(); has {
				t.Fatalf("mot de passe gardé : %q -> %q", raw, got)
			}
			if text := strings.ToLower(localURL.Scheme); text == "http" || text == "https" {
				t.Fatalf("nom gardé en http(s) : %q -> %q", raw, got)
			}
		}
	})
}

// Les sorties de git status, même tronquées ou corrompues, ne font jamais paniquer ; le nombre de fichiers lus est celui
// compté par parseGitStatus.
func FuzzParseGit(fuzzer *testing.F) {
	fuzzer.Add("# branch.head main\x001 .M N... 1 1 1 a b f.go\x002 R. N... 1 1 1 a b R100 n\x00o\x00? x\x00")
	fuzzer.Add("2 R. N... 1 1 1 a b R100 dernier")
	fuzzer.Add("u UU N... 1 1 1 1 a b c m\x00# branch.ab +x -\x00")
	fuzzer.Fuzz(func(t *testing.T, out string) {
		cs := parseGitChanges(out)
		gitState := parseGitStatus(out)
		if len(cs) > gitState.Changed+gitState.Untrack {
			t.Fatalf("%d fichiers lus, %d comptés : %q", len(cs), gitState.Changed+gitState.Untrack, out)
		}
	})
}

// Une clé de mémoire acceptée désigne toujours un fichier sous son dossier de base, jamais au-dessus.
func FuzzMemResolve(fuzzer *testing.F) {
	use(fuzzer.TempDir())
	for _, text := range []string{"g/CLAUDE.md", "g/rules/a.md", "m/-p/MEMORY.md", "m/../x.md", "g/rules/../../x.md",
		"r/-p/a.md", "p/-p/CLAUDE.md", "m/-p/.md", "g/rules/a/b.md"} {
		fuzzer.Add(text)
	}
	fuzzer.Fuzz(func(t *testing.T, key string) {
		base, rel, err := memResolve(key)
		if err != nil {
			return
		}
		if base == "" || !filepath.IsLocal(rel) {
			t.Fatalf("memResolve(%q) = %q, %q : hors du dossier", key, base, rel)
		}
	})
}

// Un chemin accepté pour la synchronisation reste sous le dossier visé.
func FuzzSyncRelOK(fuzzer *testing.F) {
	for _, text := range []string{"a/b.md", "../x", "a/../../b", ".cache/x", "a/.hidden", "a//b", "/abs", "a\\..\\b", "é/x"} {
		fuzzer.Add(text)
	}
	fuzzer.Fuzz(func(t *testing.T, rel string) {
		if syncRelOK(rel) && !filepath.IsLocal(rel) {
			t.Fatalf("syncRelOK(%q) accepte un chemin qui sort", rel)
		}
	})
}

// Une ligne de transcript arbitraire ne fait jamais paniquer l'extraction des messages ni la lecture du titre.
func FuzzLineMsgs(fuzzer *testing.F) {
	for _, text := range []string{
		`{"type":"user","message":{"content":"bonjour"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"x"},{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"r"}]}]}}`,
		`{"type":"user","isMeta":true}`, `{"type":"assistant","message":{"content":null}}`, `{"type":"user","message":"x"}`,
	} {
		fuzzer.Add([]byte(text))
	}
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		lineMsgs(data, true)
		lineMsgs(data, false)
	})
}

// missingPart (analyse d'une commande de hook) ne panique sur aucune commande.
func FuzzMissingPart(fuzzer *testing.F) {
	for _, text := range []string{`"$HOME/x" && ~/y || z`, `'a b' "c`, `cd /; ls | grep`, `${HOME}/a.sh`, `""`} {
		fuzzer.Add(text)
	}
	fuzzer.Fuzz(func(t *testing.T, cmd string) { missingPart(cmd) })
}
