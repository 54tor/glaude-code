package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSetPaths(t *testing.T) {
	// sans front matter : il est créé ; avec : seul `paths:` change ; vide : le champ (et un front matter vide) disparaît
	out, err := setPaths("# R\n", []string{"src/**/*.{ts,tsx}"})
	if err != nil || out != "---\npaths:\n  - \"src/**/*.{ts,tsx}\"\n---\n\n# R\n" {
		t.Fatalf("création : %q %v", out, err)
	}
	m, err := parseMeta(out)
	if g, e := globsOf(m); err != nil || e != nil || !slices.Equal(g, []string{"src/**/*.{ts,tsx}"}) {
		t.Fatal(g, e, err)
	}
	out, err = setPaths("---\ndescription: d\npaths:\n  - a\n  - b\n---\ncorps\n", []string{"c/**"})
	if err != nil || out != "---\ndescription: d\npaths:\n  - \"c/**\"\n---\ncorps\n" {
		t.Fatalf("remplacement : %q %v", out, err)
	}
	if out, _ = setPaths(out, nil); out != "---\ndescription: d\n---\ncorps\n" {
		t.Fatalf("retrait : %q", out)
	}
	if out, _ = setPaths("---\npaths:\n  - a\n---\ncorps\n", nil); out != "corps\n" {
		t.Fatalf("front matter vide : %q", out)
	}
	if _, err := setPaths("x", []string{"[a"}); err == nil {
		t.Fatal("motif invalide accepté")
	}
}

func TestSetType(t *testing.T) {
	for _, c := range []struct{ in, typ, want string }{
		{"corps\n", "feedback", "---\nmetadata:\n  type: feedback\n---\n\ncorps\n"},
		{"---\nname: a\nmetadata:\n  node_type: memory\n  type: project\n---\nx\n", "user", "---\nname: a\nmetadata:\n  node_type: memory\n  type: user\n---\nx\n"},
		{"---\nname: a\ntype: project\n---\nx\n", "reference", "---\nname: a\ntype: reference\n---\nx\n"},
		{"---\nname: a\nmetadata:\n  other: 1\n---\nx\n", "user", "---\nname: a\nmetadata:\n  type: user\n  other: 1\n---\nx\n"},
		{"---\nname: a\n---\nx\n", "user", "---\nname: a\nmetadata:\n  type: user\n---\nx\n"},
		{"---\nname: a\nmetadata:\n  type: user\n---\nx\n", "", "---\nname: a\nmetadata:\n---\nx\n"},
	} {
		if got, err := setType(c.in, c.typ); err != nil || got != c.want {
			t.Errorf("setType(%q, %q) = %q, %v ; attendu %q", c.in, c.typ, got, err, c.want)
		}
	}
	if _, err := setType("x", "autre"); err == nil {
		t.Fatal("type inconnu accepté")
	}
}

func TestSplitGlobs(t *testing.T) {
	if g := splitGlobs("a/*.{ts,tsx}, b/**,,c"); !slices.Equal(g, []string{"a/*.{ts,tsx}", "b/**", "c"}) {
		t.Fatal(g)
	}
}

func TestMemoryMeta(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(proj, "src", "a.ts"), "x", old)
	put(t, filepath.Join(proj, "src", "b.go"), "x", old)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "@RTK.md et @absent.md\n", old)
	put(t, filepath.Join(cfg, "RTK.md"), "ok", old)
	put(t, filepath.Join(cfg, "rules", "ts.md"), "---\npaths:\n  - \"src/**/*.ts\"\n---\nr\n", old)
	put(t, filepath.Join(cfg, "rules", "tous.md"), "toujours\n", old)
	put(t, filepath.Join(cfg, "rules", "liens.md"), "voir [[b]] et [[disparue]]\n", old)
	put(t, filepath.Join(cfg, "rules", "vide.md"), "---\npaths: []\n---\nr\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [A](a.md) — a\n- [Mort](mort.md) — x\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "---\nname: a\nmetadata:\n  type: feedback\n---\nvoir [[b]] et [[nulle part]]\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "b.md"), "b", old)

	g, ps, _ := memList()
	by := map[string]MemFile{}
	for _, f := range append(g, ps[0].Files...) {
		by[f.Key] = f
	}
	if f := by["g/rules/ts.md"]; !slices.Equal(f.Paths, []string{"src/**/*.ts"}) || f.Always {
		t.Fatalf("règle scopée : %+v", f)
	}
	if f := by["g/rules/tous.md"]; f.Paths != nil || !f.Always || f.Tokens == 0 {
		t.Fatalf("règle générale : %+v", f)
	}
	if f := by["g/rules/vide.md"]; f.Warn == "" {
		t.Fatalf("paths vide non signalé : %+v", f)
	}
	if f := by["m/-p/a.md"]; f.Type != "feedback" {
		t.Fatalf("type : %+v", f)
	}

	files, total, _, err := memMatches("g/rules/ts.md", "-p")
	if err != nil || total != 1 || !slices.Equal(files, []string{"src/a.ts"}) {
		t.Fatal(files, total, err)
	}

	kinds := map[string]int{}
	ruleLinks := 0
	for _, i := range memHealth() {
		kinds[i.Kind]++
		if i.Kind == "lien" && i.Key == "g/rules/liens.md" {
			ruleLinks++
		}
	}
	if ruleLinks != 1 {
		t.Errorf("liens cassés d'une règle globale : %d, attendu 1", ruleLinks)
	}
	for _, k := range []string{"import", "index-ligne", "index-absent", "lien", "front-matter"} {
		if kinds[k] == 0 {
			t.Errorf("problème %q non détecté : %v", k, kinds)
		}
	}
	if rm, add, err := memRepairIndex("-p"); err != nil || rm != 1 || add != 1 {
		t.Fatal(rm, add, err)
	}
	if s, _ := memRead("m/-p/MEMORY.md"); strings.Contains(s, "mort.md") || !strings.Contains(s, "](b.md)") {
		t.Fatalf("index réparé : %q", s)
	}

	if h := memSearch("TOUJOURS"); len(h) != 1 || h["g/rules/tous.md"] == "" {
		t.Fatal(h)
	}

	// désactivation puis réactivation d'une règle
	dst, err := memToggle("g/rules/tous.md")
	if err != nil || dst != "g/rules/tous.md.off" {
		t.Fatal(dst, err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "rules", "tous.md.off")); err != nil {
		t.Fatal(err)
	}
	g, _, _ = memList()
	var off MemFile
	for _, f := range g {
		if f.Key == dst {
			off = f
		}
	}
	if !off.Off || off.Always {
		t.Fatalf("règle désactivée : %+v", off)
	}
	if dst, err = memToggle(dst); err != nil || dst != "g/rules/tous.md" {
		t.Fatal(dst, err)
	}
	if _, err := memToggle("m/-p/a.md"); err == nil {
		t.Fatal("seules les règles se désactivent")
	}

	// copie : l'original reste
	if dst, err := memTransfer("g/rules/tous.md", "-p", true); err != nil || dst != "m/-p/tous.md" {
		t.Fatal(dst, err)
	}
	if _, err := memRead("g/rules/tous.md"); err != nil {
		t.Fatal("l'original a disparu :", err)
	}
}

// La documentation du projet (README, PLAN…) est listée et lisible, mais n'est pas « chargée à chaque session » et ses
// @mentions ne sont pas des imports cassés.
func TestDocumentationDuProjet(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(proj, "CLAUDE.md"), "règles\n", old)
	for _, n := range []string{"CONVENTIONS.md", "PLAN.md", "TOOLING.md", "README.md", "AGENTS.md"} {
		put(t, filepath.Join(proj, n), "voir @quelquun et @inexistant.md\n", old)
	}
	_, ps, _ := memList()
	got := map[string]MemFile{}
	for _, f := range ps[0].Files {
		got[f.Name] = f
	}
	for _, n := range []string{"CONVENTIONS.md", "PLAN.md", "TOOLING.md", "README.md", "AGENTS.md"} {
		f, ok := got[n]
		if !ok || f.Always {
			t.Errorf("%s : listé=%v chargé=%v", n, ok, f.Always)
		}
		if s, err := memRead("p/-p/" + n); err != nil || s == "" {
			t.Errorf("%s illisible : %v", n, err)
		}
	}
	if !got["CLAUDE.md"].Always {
		t.Error("CLAUDE.md doit rester chargé")
	}
	for _, i := range memHealth() {
		if i.Kind == "import" && !strings.HasSuffix(i.Key, "CLAUDE.md") {
			t.Errorf("faux import signalé : %+v", i)
		}
	}
}

func TestKeepHeader(t *testing.T) {
	orig := "---\nname: a\ndescription: d\nmetadata:\n  type: user\n---\n\ncorps\n"
	if got := keepHeader(orig, "texte condensé\n"); got != "---\nname: a\ndescription: d\nmetadata:\n  type: user\n---\n\ntexte condensé\n" {
		t.Fatalf("%q", got)
	}
	avec := "---\nname: autre\n---\n\nx\n"
	if keepHeader(orig, avec) != avec {
		t.Fatal("un en-tête existant ne doit pas être remplacé")
	}
	if keepHeader("sans en-tête\n", "x\n") != "x\n" {
		t.Fatal("rien à restaurer sans en-tête d'origine")
	}
}

func TestSetTypeEnteteDejaInvalide(t *testing.T) {
	in := "---\nname: x\ndescription: Politique projet : aucune rétrocompatibilité\ntype: feedback\n---\n\ncorps\n"
	if _, err := parseMeta(in); err == nil {
		t.Fatal("le cas de test devait être un en-tête invalide")
	}
	got, err := setType(in, "project")
	if err != nil || !strings.Contains(got, "\ntype: project\n") || !strings.Contains(got, "description: Politique projet : aucune") {
		t.Fatalf("setType sur un en-tête invalide : %q, %v", got, err)
	}
}

func TestFixFrontMatter(t *testing.T) {
	in := "---\nname: x\ndescription: Règles : tags :latest # ok\nmetadata:\n  type: user\n---\n\ncorps\n"
	out, err := fixFrontMatter(in)
	if err != nil || !strings.Contains(out, `description: "Règles : tags :latest # ok"`) || !strings.HasSuffix(out, "\ncorps\n") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := fixFrontMatter(out); err == nil {
		t.Fatal("déjà valide : doit refuser")
	}
}

func TestMetaString(t *testing.T) {
	for text, want := range map[string]string{
		"---\ndescription:\nname: x\n---\n":                 "",
		"---\ndescription: >-\n  sur deux\n  lignes\n---\n": "sur deux lignes",
		"---\ndescription: 'entre guillemets'\n---\n":       "entre guillemets",
		"pas d'en-tête": "",
	} {
		if got := metaString(text, "description"); got != want {
			t.Errorf("metaString(%q) = %q, attendu %q", text, got, want)
		}
	}
}

func TestMdLinkIgnoreLesURL(t *testing.T) {
	if m := mdLink.FindStringSubmatch("- [Doc](https://x.y/readme.md)"); m != nil {
		t.Fatalf("URL prise pour un fichier de mémoire : %v", m)
	}
	if m := mdLink.FindStringSubmatch("- [Note](note.md)"); m == nil || m[1] != "note.md" {
		t.Fatalf("lien local manqué : %v", m)
	}
}

func TestMemWriteGardeUnFichierIllisible(t *testing.T) {
	cfg := t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	p := filepath.Join(cfg, "CLAUDE.md")
	os.WriteFile(p, []byte("\xff\xfe pas de l'UTF-8"), 0o644)
	if err := memSave("g/CLAUDE.md", "nouveau"); err == nil {
		t.Fatal("fichier illisible écrasé")
	}
	if b, _ := os.ReadFile(p); string(b) != "\xff\xfe pas de l'UTF-8" {
		t.Fatalf("contenu perdu : %q", b)
	}
}

func TestSetTypeNeToucheQueMetadata(t *testing.T) {
	text := "---\nname: x\nhooks:\n  type: command\nmetadata:\n  type: user\n---\ncorps\n"
	out, err := setType(text, "feedback")
	if err != nil || !strings.Contains(out, "hooks:\n  type: command") || !strings.Contains(out, "metadata:\n  type: feedback") {
		t.Fatalf("setType : %q %v", out, err)
	}
	out, err = setType(text, "")
	if err != nil || !strings.Contains(out, "hooks:\n  type: command") || strings.Contains(out, "type: user") {
		t.Fatalf("retrait du type : %q %v", out, err)
	}
	if out, _ := setType("---\nhooks:\n  type: command\n---\n", "user"); !strings.Contains(out, "hooks:\n  type: command") || !strings.Contains(out, "metadata:\n  type: user") {
		t.Fatalf("sans metadata : %q", out)
	}
}
