package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func backupUpload(h http.Handler, field string, data []byte) *httptest.ResponseRecorder {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile(field, "claude-config.zip")
	fw.Write(data)
	mw.Close()
	q := httptest.NewRequest("POST", "/v1/backup/imports", &b)
	q.Host = testHost
	q.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, q)
	return w
}

func TestRoutesBackupFlow(t *testing.T) {
	h, _, _ := web(t)
	exportEnv(t)

	q := httptest.NewRequest("GET", "/v1/backup/export?cats=skills,settings", nil)
	q.Host = testHost
	w := httptest.NewRecorder()
	h.ServeHTTP(w, q)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Content-Disposition") == "" {
		t.Fatalf("export : %d %v", w.Code, w.Header())
	}
	if _, ok := unzipMap(t, w.Body.Bytes())["home/skills/deploy/SKILL.md"]; !ok {
		t.Fatal("skill absent de l'archive")
	}
	zipB := w.Body.Bytes()

	v1err(t, h, "GET", "/v1/backup/export?cats=", "", 400, "")
	v1err(t, h, "GET", "/v1/backup/export?cats=nope", "", 400, "")

	if w := backupUpload(h, "file", []byte("pas un zip")); w.Code != 400 {
		t.Fatalf("zip corrompu : %d %s", w.Code, w.Body)
	}
	if w := backupUpload(h, "autre", zipB); w.Code != 400 {
		t.Fatalf("champ absent : %d", w.Code)
	}

	w = backupUpload(h, "file", zipB)
	var plan BackupPlan
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &plan) != nil || plan.ID == "" || len(plan.Items) == 0 {
		t.Fatalf("import : %d %s", w.Code, w.Body)
	}
	defer dropStage(plan.ID)

	v1err(t, h, "POST", "/v1/backup/imports/"+plan.ID+"/applications", "{", 400, "")
	code, m, raw := call(t, h, "POST", "/v1/backup/imports/"+plan.ID+"/applications", `{"picks":[]}`)
	if code != 200 || m["results"] == nil {
		t.Fatalf("application : %d %s", code, raw)
	}
	v1err(t, h, "POST", "/v1/backup/imports/"+plan.ID+"/applications", `{"picks":[]}`, 404, "")

	w = backupUpload(h, "file", zipB)
	json.Unmarshal(w.Body.Bytes(), &plan)
	status(t, h, "DELETE", "/v1/backup/imports/"+plan.ID, "", 204)
	status(t, h, "DELETE", "/v1/backup/imports/"+plan.ID, "", 204)
	v1err(t, h, "POST", "/v1/backup/imports/"+plan.ID+"/applications", `{"picks":[]}`, 404, "")
}

func TestRoutesBackupTooManyPicks(t *testing.T) {
	h, _, _ := web(t)
	exportEnv(t)
	b, _, _ := exportBackup([]string{"skills"})
	var plan BackupPlan
	json.Unmarshal(backupUpload(h, "file", b).Body.Bytes(), &plan)
	defer dropStage(plan.ID)
	picks := make([]SyncPick, 2001)
	body, _ := json.Marshal(map[string]any{"picks": picks})
	v1err(t, h, "POST", "/v1/backup/imports/"+plan.ID+"/applications", string(body), 400, "")
}

func TestRoutesBackupExportServerError(t *testing.T) {
	h, _, _ := web(t)
	exportEnv(t)
	op := pluginRaw
	defer func() { pluginRaw = op }()
	pluginRaw = func() ([]byte, error) { return nil, errors.New("boom") }
	v1err(t, h, "GET", "/v1/backup/export?cats=plugins", "", 500, "")
}
