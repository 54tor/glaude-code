package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopbackHostOnlyRejectsRebindingHosts(t *testing.T) {
	handler := loopbackHostOnly(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) { responseWriter.WriteHeader(http.StatusOK) }))
	hostsExpected := map[string]int{"127.0.0.1:6060": 200, "localhost:6060": 200, "[::1]:6060": 200, "evil.example.com:6060": 403, "10.0.0.5:6060": 403}
	for host, statusExpected := range hostsExpected {
		request := httptest.NewRequest("GET", "/debug/pprof/", nil)
		request.Host = host
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != statusExpected {
			t.Errorf("%s : valeurAttendue %d, valeurObtenue %d", host, statusExpected, recorder.Code)
		}
	}
}
