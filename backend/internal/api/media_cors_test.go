package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMediaTransferCORSHandlesSegmentPreflight(t *testing.T) {
	called := false
	handler := mediaTransferCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodOptions, "/api/assets/7/hls/segments/0.ts", nil)
	request.Header.Set("Origin", "http://192.168.2.97:18080")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || called {
		t.Fatalf("response = %d, downstream called = %v", response.Code, called)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Range, X-LPicto-Segment-Priority" {
		t.Fatalf("allow headers = %q", got)
	}
}

func TestMediaTransferCORSLeavesAPIRequestsUnchanged(t *testing.T) {
	handler := mediaTransferCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/library/assets", nil))
	if response.Code != http.StatusAccepted || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("response = %d headers = %#v", response.Code, response.Header())
	}
}
