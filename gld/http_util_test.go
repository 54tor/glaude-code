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
	var got struct{ Error struct{ Code, Message string } }
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
