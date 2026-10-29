package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrShape(t *testing.T) {
	w := httptest.NewRecorder()
	writeErr(w, http.StatusConflict, "conflict", "boom")
	var got struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 409 || got.Error.Code != "conflict" || got.Error.Message != "boom" {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
}

func TestDecodeBodyRejectsGarbage(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader("{"))
	var v struct{}
	if decodeBody(w, r, &v, 1<<10) || w.Code != 400 {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestPathID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x/{id}", func(w http.ResponseWriter, r *http.Request) {
		if id, ok := pathID(w, r, "id"); ok {
			writeJSON(w, 200, map[string]string{"id": id})
		}
	})
	for p, want := range map[string]int{"/x/11111111-1111-1111-1111-111111111111": 200, "/x/..%2f..": 400, "/x/abc": 400} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", p, rec.Code, want)
		}
	}
}
