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

func backupUpload(handler http.Handler, field string, data []byte) *httptest.ResponseRecorder {
	var buffer bytes.Buffer
	mw := multipart.NewWriter(&buffer)
	fw, _ := mw.CreateFormFile(field, "claude-config.zip")
	fw.Write(data)
	mw.Close()
	request := httptest.NewRequest("POST", "/v1/backup/imports", &buffer)
	request.Host = testHost
	request.Header.Set("Content-Type", mw.FormDataContentType())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestRoutesBackupFlow(t *testing.T) {
	handler, _, _ := web(t)
	exportEnv(t)

	request := httptest.NewRequest("GET", "/v1/backup/export?cats=skills,settings", nil)
	request.Host = testHost
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/zip" || recorder.Header().Get("Content-Disposition") == "" {
		t.Fatalf("export : %d %v", recorder.Code, recorder.Header())
	}
	if _, ok := unzipMap(t, recorder.Body.Bytes())["home/skills/deploy/SKILL.md"]; !ok {
		t.Fatal("skill absent de l'archive")
	}
	zipB := recorder.Body.Bytes()

	v1err(t, handler, "GET", "/v1/backup/export?cats=", "", 400, "")
	v1err(t, handler, "GET", "/v1/backup/export?cats=nope", "", 400, "")

	if recorder := backupUpload(handler, "file", []byte("pas un zip")); recorder.Code != 400 {
		t.Fatalf("zip corrompu : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := backupUpload(handler, "autre", zipB); recorder.Code != 400 {
		t.Fatalf("champ absent : %d", recorder.Code)
	}

	recorder = backupUpload(handler, "file", zipB)
	var plan BackupPlan
	if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &plan) != nil || plan.ID == "" || len(plan.Items) == 0 {
		t.Fatalf("import : %d %s", recorder.Code, recorder.Body)
	}
	defer dropStage(plan.ID)

	v1err(t, handler, "POST", "/v1/backup/imports/"+plan.ID+"/applications", "{", 400, "")
	code, lookup, raw := call(t, handler, "POST", "/v1/backup/imports/"+plan.ID+"/applications", `{"picks":[]}`)
	if code != 200 || lookup["results"] == nil {
		t.Fatalf("application : %d %s", code, raw)
	}
	v1err(t, handler, "POST", "/v1/backup/imports/"+plan.ID+"/applications", `{"picks":[]}`, 404, "")

	recorder = backupUpload(handler, "file", zipB)
	json.Unmarshal(recorder.Body.Bytes(), &plan)
	status(t, handler, "DELETE", "/v1/backup/imports/"+plan.ID, "", 204)
	status(t, handler, "DELETE", "/v1/backup/imports/"+plan.ID, "", 204)
	v1err(t, handler, "POST", "/v1/backup/imports/"+plan.ID+"/applications", `{"picks":[]}`, 404, "")
}

func TestRoutesBackupTooManyPicks(t *testing.T) {
	handler, _, _ := web(t)
	exportEnv(t)
	data, _, _ := exportBackup([]string{"skills"})
	var plan BackupPlan
	json.Unmarshal(backupUpload(handler, "file", data).Body.Bytes(), &plan)
	defer dropStage(plan.ID)
	picks := make([]SyncPick, 2001)
	body, _ := json.Marshal(map[string]any{"picks": picks})
	v1err(t, handler, "POST", "/v1/backup/imports/"+plan.ID+"/applications", string(body), 400, "")
}

func TestRoutesBackupExportServerError(t *testing.T) {
	handler, _, _ := web(t)
	exportEnv(t)
	op := pluginRaw
	defer func() { pluginRaw = op }()
	pluginRaw = func() ([]byte, error) { return nil, errors.New("boom") }
	v1err(t, handler, "GET", "/v1/backup/export?cats=plugins", "", 500, "")
}
