package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForHealthWaitsForServiceStartup(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	processor := Processor{
		BaseURL:             server.URL,
		HealthWaitTimeout:   time.Second,
		HealthRetryInterval: time.Millisecond,
	}
	if err := processor.waitForHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("health requests = %d, want 3", requests.Load())
	}
}

func TestMediaAnalysisErrorClassification(t *testing.T) {
	mediaErrors := []string{
		"ffmpeg returned non-zero exit status 69",
		"cannot identify image file",
		"moov atom not found",
		"no such file",
	}
	for _, message := range mediaErrors {
		if !isMediaAnalysisError(errors.New(message)) {
			t.Fatalf("media error not classified: %q", message)
		}
	}
	transientErrors := []string{
		"Server disconnected without sending a response",
		"model response does not contain a JSON object",
		"connection refused",
		"request timeout",
	}
	for _, message := range transientErrors {
		if isMediaAnalysisError(errors.New(message)) {
			t.Fatalf("transient error classified as media error: %q", message)
		}
	}
}

func TestPlaybackInterruptionResponseIsRecoverable(t *testing.T) {
	if !isPlaybackInterruptionResponse(http.StatusConflict, []byte(`{"detail":"AI analysis paused for media playback"}`)) {
		t.Fatal("playback interruption was not recognized")
	}
	if isPlaybackInterruptionResponse(http.StatusInternalServerError, []byte(`{"detail":"Server disconnected without sending a response."}`)) {
		t.Fatal("service failure was incorrectly treated as playback interruption")
	}
}
