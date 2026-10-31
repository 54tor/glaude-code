package main

// Tests issus des tests de mutation (gremlins) : chacun fige une limite ou une branche qu'aucun autre test ne vérifiait.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRechercheEtCache(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	put(t, path, `{"type":"user","message":{"content":"alpha"}}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"type":"user","message":{"content":"rien"}}`+"\n", old)
	if hits := search("alpha"); len(hits) != 1 {
		t.Fatalf("recherche : %+v", hits)
	}
	// Même date, autre taille : le cache ne doit pas resservir l'ancien texte.
	put(t, path, `{"type":"user","message":{"content":"beta, plus long"}}`+"\n", old)
	st, _ := os.Stat(path)
	os.Chtimes(path, st.ModTime(), st.ModTime())
	if hits := search("beta"); len(hits) != 1 {
		t.Fatal("texte périmé servi par le cache")
	}
	nom := strings.Repeat("é", 80)
	if saveStore(Store{Groups: []VGroup{{Name: nom, Project: "/p"}}, Favs: []string{}}) != nil ||
		saveStore(Store{Groups: []VGroup{{Name: nom + "é", Project: "/p"}}, Favs: []string{}}) == nil {
		t.Fatal("nom de groupe à la limite")
	}
}

func TestSessionsEtDetail(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	base := t.TempDir()
	proj, proj2 := filepath.Join(base, "un"), filepath.Join(base, "deux")
	cache := filepath.Join(base, "mon projet.v2", "sous")
	for _, dir := range []string{proj, proj2, cache} {
		os.MkdirAll(dir, 0o755)
	}
	pp := filepath.Join(cfg, "projects")
	head := `{"cwd":"` + proj + `"}` + "\n"
	// 2 500 octets en tout, quelle que soit la longueur du dossier temporaire (TMPDIR) : 2 Ko attendus
	put(t, filepath.Join(pp, "-p", id1+".jsonl"), head+strings.Repeat(" ", 2500-len(head)-1)+"\n", 3*time.Hour)
	put(t, filepath.Join(pp, "-p", id2+".jsonl"), `{"type":"user","message":{"content":"sans cwd"}}`+"\n", time.Hour) // rangée avec sa voisine
	put(t, filepath.Join(pp, "-p", id3+".jsonl"), `{"cwd":"`+proj2+`"}`+"\n", 2*time.Hour)
	enc := nonAlnum.ReplaceAllString(cache, "-")
	put(t, filepath.Join(pp, enc, "44444444-4444-4444-4444-444444444444.jsonl"), `{"type":"user","message":{"content":"x"}}`+"\n", old)
	put(t, filepath.Join(pp, "-q", "55555555-5555-5555-5555-555555555555.jsonl"), "", old) // vide : ignorée

	gs := sessions()
	if len(gs) != 3 || gs[0].Project > gs[1].Project || gs[1].Project > gs[2].Project {
		t.Fatalf("groupes : %+v", gs)
	}
	by := map[string]Group{}
	for _, group := range gs {
		by[group.Project] = group
	}
	localGroup := by[proj]
	if len(localGroup.Sessions) != 2 || localGroup.Sessions[0].ID != id2 || localGroup.Sessions[1].ID != id1 || len(by[proj2].Sessions) != 1 || len(by[cache].Sessions) != 1 {
		t.Fatalf("regroupement : %+v", gs)
	}
	session := localGroup.Sessions[1]
	if session.KB != 2 || !near(session.Mtime, mtimeOf(t, filepath.Join(pp, "-p", id1+".jsonl"))) {
		t.Fatalf("taille et date : %+v", session)
	}
	if decodeDir("-nexiste-pas-du-tout") != "" {
		t.Fatal("dossier inventé décodé")
	}

	// Détail : premières et dernières dates, prompts (3 derniers), modèles distincts triés, sous-agents.
	var builder strings.Builder
	for index := 1; index <= 5; index++ {
		fmt.Fprintf(&builder, `{"type":"user","timestamp":"t%d","message":{"content":"p%d"}}`+"\n", index, index)
	}
	builder.WriteString(`{"type":"user","message":{"content":[{"type":"tool_result","content":"r"}]}}` + "\n")
	builder.WriteString(`{"type":"assistant","message":{"model":"b"}}` + "\n" + `{"type":"assistant","message":{"model":"a"}}` + "\n" + `{"type":"assistant","message":{"model":"a"}}` + "\n")
	dp := filepath.Join(pp, "-p", id1+".jsonl")
	put(t, dp, builder.String(), old)
	put(t, filepath.Join(pp, "-p", id1, "subagents", "a.jsonl"), "x", old)
	put(t, filepath.Join(pp, "-p", id1, "subagents", "b.jsonl"), "x", old)
	put(t, filepath.Join(pp, "-p", id1, "subagents", "c.txt"), "x", old)
	localDetail := detail(dp)
	if localDetail.T0 != "t1" || localDetail.T1 != "t5" || localDetail.N != 5 || strings.Join(localDetail.Prompts, ",") != "p3,p4,p5" || strings.Join(localDetail.Models, ",") != "a,b" || localDetail.Subagents != 2 {
		t.Fatalf("détail : %+v", localDetail)
	}
}

func TestReassignation(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	encSrc, encDst := nonAlnum.ReplaceAllString(src, "-"), nonAlnum.ReplaceAllString(dst, "-")
	jl := filepath.Join(cfg, "projects", encSrc, id1+".jsonl")
	put(t, jl, `{"cwd":"`+src+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", encSrc, id1, "subagents", "a.jsonl"), `{"cwd":"`+src+`"}`+"\n", old)
	before := mtimeOf(t, jl)

	liveSessions = func() map[string]bool { return map[string]bool{id1: true} }
	if _, err := reassign([]item{{encSrc, id1}}, dst); err == nil || !strings.Contains(err.Error(), "session vivante, ferme-la d'abord : 11111111") {
		t.Fatal(err)
	}
	use(cfg)
	if _, err := reassign([]item{{encSrc, id1}}, src); err == nil || !strings.Contains(err.Error(), "déjà dans ce dossier : 11111111") {
		t.Fatal(err)
	}
	put(t, filepath.Join(cfg, "projects", encDst, id1+".jsonl"), "{}\n", old)
	if _, err := reassign([]item{{encSrc, id1}}, dst); err == nil || !strings.Contains(err.Error(), "existe déjà dans la cible : "+id1+".jsonl") {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(cfg, "projects", encDst, id1+".jsonl"))
	put(t, filepath.Join(cfg, "projects", encDst, id1, "subagents", "b.jsonl"), "{}\n", old) // reste d'un /cd interrompu
	if count, err := reassign([]item{{encSrc, id1}}, dst); count != 1 || err != nil {
		t.Fatal(count, err)
	}
	moved := filepath.Join(cfg, "projects", encDst, id1+".jsonl")
	data, _ := os.ReadFile(moved)
	sub, _ := os.ReadFile(filepath.Join(cfg, "projects", encDst, id1, "subagents", "a.jsonl"))
	if !strings.Contains(string(data), `"cwd":"`+dst+`"`) || !strings.Contains(string(sub), `"cwd":"`+dst+`"`) {
		t.Fatalf("cwd non réécrit :\n%s\n%s", data, sub)
	}
	if _, err := os.Stat(filepath.Join(cfg, "projects", encDst, id1, "subagents", "b.jsonl")); err != nil {
		t.Fatal("dossier fusionné : b.jsonl perdu")
	}
	if _, err := os.Stat(filepath.Join(cfg, "projects", encSrc, id1)); err == nil {
		t.Fatal("dossier source non vidé")
	}
	if !near(mtimeOf(t, moved), before) {
		t.Fatal("la date du transcript a changé")
	}
}

func TestOuvertsEtRecents(t *testing.T) {
	handler, _, _ := web(t)
	t.Setenv("FAKE_AGENTS", `[{"sessionId":"a","status":"busy","state":"blocked"},{"sessionId":"b","status":"busy","state":"running"},{"sessionId":"c","status":"idle"}]`)
	if values := waiting(); strings.Join(values, ",") != "a,c" {
		t.Fatalf("en attente : %v", values)
	}
	status(t, handler, "GET", "/v1/directories?path=::drives", "", 400) // la liste des lecteurs n'existe que sous Windows

}

func TestPurgeRestes(t *testing.T) {
	t.Setenv("GLD_GRACE", "")
	for value, want := range map[string]time.Duration{"": time.Hour, "0": 0, "5": 5 * time.Minute, "-5": time.Hour, "x": time.Hour} {
		t.Setenv("GLD_GRACE", value)
		if duration := grace(); duration != want {
			t.Fatalf("GLD_GRACE=%q : %v", value, duration)
		}
	}
	if values := lineHashes([]byte(`{"pastedContents":[{"contentHash":"x"},{"contentHash":""}]}`)); len(values) != 1 || values[0] != "x" {
		t.Fatal("collages en liste :", values)
	}
	if lineHashes([]byte(`{"pastedContents":"?"}`)) != nil {
		t.Fatal("collages illisibles")
	}

	cfg := t.TempDir()
	use(cfg)
	pp := filepath.Join(cfg, "projects", "-p")
	hist := filepath.Join(cfg, "history.jsonl")
	ss := filepath.Join(cfg, "shell-snapshots")
	// id1 : deux instantanés sur une ligne, orphelins ; id2 : un collage seul ; id3 : un instantané seul.
	put(t, filepath.Join(pp, id1+".jsonl"), `{"m":"snapshot-bash-444-d.sh snapshot-bash-555-e.sh snapshot-bash-666-f.sh"}`+"\n", old)
	put(t, filepath.Join(pp, id2+".jsonl"), "{}\n", old)
	put(t, filepath.Join(pp, id3+".jsonl"), `{"m":"snapshot-bash-888-h.sh"}`+"\n", old)
	put(t, filepath.Join(pp, "77777777-7777-7777-7777-777777777777.jsonl"), `{"m":"snapshot-bash-666-f.sh snapshot-bash-999-i.sh"}`+"\n", old) // gardée
	put(t, hist, `{"sessionId":"`+id2+`","pastedContents":{"1":{"contentHash":"dddddddddddddddd"}}}`+"\n", old)
	os.Chmod(hist, 0o644)
	for _, text := range []string{"444-d", "555-e", "666-f", "888-h", "999-i"} {
		put(t, filepath.Join(ss, "snapshot-bash-"+text+".sh"), "s", old)
	}
	put(t, filepath.Join(cfg, "paste-cache", "dddddddddddddddd.txt"), "c", old)

	if _, _, notes, err := purgeSessions([]item{{"-p", id1}}); err != nil || len(notes) != 1 || notes[0] != "  shell-snapshots : 2 instantané(s) effacé(s)" {
		t.Fatal(notes, err)
	}
	if exists(filepath.Join(ss, "snapshot-bash-444-d.sh")) || exists(filepath.Join(ss, "snapshot-bash-555-e.sh")) || !exists(filepath.Join(ss, "snapshot-bash-666-f.sh")) {
		t.Fatal("instantanés d'une même ligne")
	}
	// Sans ligne d'historique ni reste : history.jsonl n'est pas réécrit, aucun compte rendu.
	h0 := mtimeOf(t, hist)
	if _, _, notes, _ := purgeSessions([]item{{"-p", id3}}); len(notes) != 1 || !strings.Contains(notes[0], "1 instantané") {
		t.Fatal("instantané seul :", notes)
	}
	if mtimeOf(t, hist) != h0 {
		t.Fatal("history.jsonl réécrit sans raison")
	}
	if _, _, notes, _ := purgeSessions([]item{{"-p", id2}}); len(notes) != 2 || !strings.Contains(notes[1], "1 collage") {
		t.Fatal("collage seul :", notes)
	}
	if st, _ := os.Stat(hist); st.Mode().Perm() != 0o644 {
		t.Fatal("droits de history.jsonl perdus :", st.Mode())
	}
	if exists(filepath.Join(cfg, "paste-cache", "dddddddddddddddd.txt")) {
		t.Fatal("collage orphelin gardé")
	}
	put(t, filepath.Join(pp, "88888888-8888-8888-8888-888888888888.jsonl"), "{}\n", old)
	if _, _, notes, _ := purgeSessions([]item{{"-p", "88888888-8888-8888-8888-888888888888"}}); len(notes) != 0 {
		t.Fatal("compte rendu sans rien d'effacé :", notes)
	}
}

func TestDecodeDirWalkRacinesExclues(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "mon projet")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	enc := nonAlnum.ReplaceAllString(proj, "-")
	if got := decodeDirWalk(enc); got != proj {
		t.Fatalf("décodage = %q, attendu %q", got, proj)
	}
	top := "/" + strings.SplitN(strings.TrimPrefix(proj, "/"), "/", 2)[0]
	skipRoots[top] = true
	defer delete(skipRoots, top)
	if got := decodeDirWalk(enc); got != "" {
		t.Fatalf("une racine exclue (%s) a été parcourue : %q", top, got)
	}
	for _, raw := range []string{"/proc", "/sys", "/dev", "/run", "/mnt", "/media"} {
		if !skipRoots[raw] {
			t.Errorf("%s doit être exclue", raw)
		}
	}
}
