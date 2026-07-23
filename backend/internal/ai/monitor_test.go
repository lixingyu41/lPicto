package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAIHealthEndpointsAndSchedule(t *testing.T) {
	var restarts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/restart":
			restarts.Add(1)
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if AIHealthCheckInterval != 30*time.Minute {
		t.Fatalf("AI health interval = %s", AIHealthCheckInterval)
	}
	if !probeAIHealth(context.Background(), server.URL) {
		t.Fatal("healthy AI service was reported unavailable")
	}
	if err := requestAIRestart(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	if restarts.Load() != 1 {
		t.Fatalf("restart requests = %d", restarts.Load())
	}
}
