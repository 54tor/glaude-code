package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeToolUseLine(docPath string) string {
	return `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"` + docPath + `"}}]}}` + "\n"
}

func TestWroteDoc(t *testing.T) {
	directory := t.TempDir()
	writtenDoc := filepath.Join(directory, "rapport.md")
	citedDoc := filepath.Join(directory, "cite.md")
	otherDoc := filepath.Join(directory, "autre.md")
	for _, docPath := range []string{writtenDoc, citedDoc, otherDoc} {
		os.WriteFile(docPath, []byte("x"), 0o600)
	}
	transcriptPath := filepath.Join(directory, "t.jsonl")
	userLine := `{"type":"user","message":{"content":"voir ` + citedDoc + `"}}` + "\n"
	bareUserLine := `{"type":"user","message":{"content":"` + otherDoc + `"}}` + "\n"
	os.WriteFile(transcriptPath, []byte(userLine+bareUserLine+writeToolUseLine(writtenDoc)), 0o600)

	if !wroteDoc(transcriptPath, writtenDoc) {
		t.Fatal("un fichier écrit par Write n'est pas reconnu")
	}
	if wroteDoc(transcriptPath, citedDoc) {
		t.Fatal("un chemin seulement cité par l'utilisateur est servi comme document")
	}
	if wroteDoc(transcriptPath, otherDoc) {
		t.Fatal("un message de l'utilisateur réduit au chemin est servi comme document écrit")
	}
	if wroteDoc(transcriptPath, filepath.Join(directory, "jamais.md")) {
		t.Fatal("un chemin absent du transcript est servi")
	}
	if wroteDoc(filepath.Join(directory, "absent.jsonl"), writtenDoc) {
		t.Fatal("transcript illisible : doit refuser")
	}
}

func TestWroteDocAssistantMention(t *testing.T) {
	directory := t.TempDir()
	mentionedDoc := filepath.Join(directory, "notes.md")
	os.WriteFile(mentionedDoc, []byte("x"), 0o600)
	transcriptPath := filepath.Join(directory, "t.jsonl")
	os.WriteFile(transcriptPath, []byte(`{"type":"assistant","message":{"content":"Rapport : `+mentionedDoc+`"}}`+"\n"), 0o600)
	if !wroteDoc(transcriptPath, mentionedDoc) {
		t.Fatal("un document cité par l'assistant n'est pas reconnu")
	}
}

func TestDocumentRouteServesOnlyWrittenDocs(t *testing.T) {
	handler, cfg, _ := web(t)
	directory := t.TempDir()
	writtenDoc := filepath.Join(directory, "rapport.md")
	unwrittenDoc := filepath.Join(directory, "secret.md")
	os.WriteFile(writtenDoc, []byte("# titre"), 0o600)
	os.WriteFile(unwrittenDoc, []byte("# secret"), 0o600)
	os.WriteFile(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), []byte(writeToolUseLine(writtenDoc)), 0o600)

	fetch := func(docPath string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/v1/sessions/"+id1+"/conversation/document?dir=-p&path="+docPath, nil)
		request.Host = testHost
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	served := fetch(writtenDoc)
	if served.Code != http.StatusOK || served.Body.String() != "# titre" {
		t.Fatalf("document écrit : %d %q", served.Code, served.Body.String())
	}
	if !strings.Contains(served.Header().Get("Content-Security-Policy"), "default-src 'none'") || served.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("en-têtes de confinement absents : %v", served.Header())
	}
	if refused := fetch(unwrittenDoc); refused.Code != http.StatusNotFound {
		t.Fatalf("document jamais écrit servi : %d", refused.Code)
	}
}

func TestDocSystem(t *testing.T) {
	noOverride(t)
	setLangT(t, "fr")
	if got := docSystem(); !strings.Contains(got, "document Markdown de projet logiciel") {
		t.Fatalf("fr : %q", got)
	}
	setLangT(t, "en")
	english := docSystem()
	if english == "" || strings.Contains(english, "document Markdown de projet logiciel") {
		t.Fatalf("en : %q", english)
	}
	if english == memSystem() || english == cmdSystem() {
		t.Fatal("docSystem doit être distinct des consignes mémoire et commande")
	}
}

func TestProjectKeyFor(t *testing.T) {
	_, cfg, _ := web(t)
	projectPath := t.TempDir()
	keyedDir := filepath.Join(cfg, "projects", "-bob-app")
	os.MkdirAll(keyedDir, 0o755)
	transcript := `{"type":"user","cwd":"` + projectPath + `","message":{"content":"x"}}` + "\n"
	os.WriteFile(filepath.Join(keyedDir, id2+".jsonl"), []byte(transcript), 0o600)

	if got := projectKeyFor(projectPath); got != "-bob-app" {
		t.Fatalf("projectKeyFor = %q", got)
	}
	if got := projectKeyFor("/home/bob/inconnu"); got != "" {
		t.Fatalf("projet inconnu : %q", got)
	}
}

func TestRemoveWorkDir(t *testing.T) {
	previous := workPath
	t.Cleanup(func() { workPath = previous })

	created := filepath.Join(t.TempDir(), "gld-web-created")
	os.MkdirAll(created, 0o700)
	workPath = created
	removeWorkDir()
	if _, err := os.Stat(created); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dossier de travail non retiré : %v", err)
	}

	configDir := t.TempDir()
	use(configDir)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	workPath = cfgDir()
	removeWorkDir()
	if _, err := os.Stat(configDir); err != nil {
		t.Fatalf("le dossier de configuration de repli ne doit jamais être supprimé : %v", err)
	}

	workPath = ""
	removeWorkDir()
}

func TestWorkspaceReopenAfterClose(t *testing.T) {
	_, _, projectDir := web(t)
	wsTest(t)
	workspace, err := ideWS(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	ideCloseAll()

	reopened := workspace.reopen()
	if reopened == nil || reopened == workspace || reopened.dir != projectDir {
		t.Fatalf("reopen = %+v", reopened)
	}
	reopened.mu.Lock()
	isClosed := reopened.closed
	reopened.mu.Unlock()
	if isClosed {
		t.Fatal("l'espace rouvert est déjà fermé")
	}

	missing := &ideWorkspace{dir: filepath.Join(projectDir, "disparu")}
	if missing.reopen() != nil {
		t.Fatal("reopen d'un dossier disparu doit rendre nil")
	}
}

func TestPeekErr(t *testing.T) {
	status, key := peekErr(errBodyTooBig)
	if status != http.StatusRequestEntityTooLarge || key != "srv.http_util.requeteTropGrosse" {
		t.Fatalf("trop gros : %d %s", status, key)
	}
	status, key = peekErr(errors.New("invalid character"))
	if status != http.StatusBadRequest || key != "srv.http_util.requeteInvalide" {
		t.Fatalf("invalide : %d %s", status, key)
	}
	status, _ = peekErr(errors.Join(errors.New("x"), errBodyTooBig))
	if status != http.StatusRequestEntityTooLarge {
		t.Fatal("errBodyTooBig enveloppée non reconnue")
	}
}

func TestDiscardWriter(t *testing.T) {
	writer := &discardWriter{h: http.Header{}}
	writer.WriteHeader(http.StatusAccepted)
	written, err := writer.Write([]byte("corps ignoré"))
	if err != nil || written != len("corps ignoré") || writer.code != http.StatusAccepted {
		t.Fatalf("Write = %d %v, code %d", written, err, writer.code)
	}
	writer.Header().Set("X", "1")
	if writer.Header().Get("X") != "1" {
		t.Fatal("Header")
	}
}

func TestTamperErr(t *testing.T) {
	tampered := &tamperErr{uid: 7, table: "passkeys"}
	if tampered.Error() != errTamper.Error() || !errors.Is(tampered, errTamper) {
		t.Fatalf("Error/Unwrap : %q", tampered.Error())
	}
	if strings.Contains(tampered.Error(), "passkeys") {
		t.Fatal("le message ne doit pas exposer la table fautive")
	}
}

func TestFreePort(t *testing.T) {
	first, err := freePort()
	if err != nil || first == "" || first == "0" {
		t.Fatalf("freePort = %q %v", first, err)
	}
	listener, listenErr := net.Listen("tcp", "127.0.0.1:"+first)
	if listenErr != nil {
		t.Fatalf("le port rendu n'est pas libre : %v", listenErr)
	}
	listener.Close()
}

func TestStartHomeProcFailsWhenContextCancelled(t *testing.T) {
	cancel := withHomeCtx(t)
	cancel()
	process, err := startHomeProc(Home{Name: "Pro", Dir: t.TempDir()})
	if err == nil || process != nil {
		t.Fatalf("démarrage malgré l'arrêt du serveur : %v %v", process, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("erreur = %v", err)
	}
}
