package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrShape(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeErr(recorder, http.StatusConflict, "conflict", "boom")
	var got struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != 409 || got.Error.Code != "conflict" || got.Error.Message != "boom" {
		t.Fatalf("code=%d body=%s", recorder.Code, recorder.Body)
	}
}

func TestDecodeBodyRejectsGarbage(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/", strings.NewReader("{"))
	var record struct{}
	if decodeBody(recorder, request, &record, 1<<10) || recorder.Code != 400 {
		t.Fatalf("code=%d", recorder.Code)
	}
}

func TestPathID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x/{id}", func(responseWriter http.ResponseWriter, request *http.Request) {
		if id, ok := pathID(responseWriter, request, "id"); ok {
			writeJSON(responseWriter, 200, map[string]string{"id": id})
		}
	})
	for path, want := range map[string]int{"/x/11111111-1111-1111-1111-111111111111": 200, "/x/..%2f..": 400, "/x/abc": 400} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
}
