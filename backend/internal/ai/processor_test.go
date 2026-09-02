package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/storage"
)

func TestWaitForHealthWaitsForServiceStartup(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","service":"lpicto-ai","protocolVersion":1}`))
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

func TestComputeNodeFailureClassification(t *testing.T) {
	if !isComputeNodeFailure(http.StatusServiceUnavailable, nil) {
		t.Fatal("503 should be treated as a compute-node failure")
	}
	if !isComputeNodeFailure(http.StatusInternalServerError, []byte(`{"detail":"connection refused"}`)) {
		t.Fatal("model connection failure should be treated as a compute-node failure")
	}
	if isComputeNodeFailure(http.StatusInternalServerError, []byte(`{"detail":"cannot identify image file"}`)) {
		t.Fatal("media failure should not be treated as a compute-node failure")
	}
}

func TestExternalAnalyzeRequestContainsOnlyStagedFrames(t *testing.T) {
	cacheRoot := t.TempDir()
	stagePath := filepath.Join("ai-staging", "asset.stage.d")
	stageRoot := filepath.Join(cacheRoot, stagePath)
	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageRoot, "meta.json"), []byte(`{"ratios":[0.25,0.75]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"00.jpg", "01.jpg"} {
		if err := os.WriteFile(filepath.Join(stageRoot, name), []byte("jpeg"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	processor := Processor{Stager: &Stager{Store: storage.Store{CacheRoot: cacheRoot}}}
	stage := &db.AIStage{StagePath: filepath.ToSlash(stagePath), SizeBytes: 64}
	request, err := processor.newAnalyzeHTTPRequest(context.Background(), ComputeNode{BaseURL: "http://192.168.2.82:8090", External: true}, analyzeRequest{AssetID: 7, RelPath: "private/original.jpg", MediaType: "image", CacheKey: "key"}, stage)
	if err != nil {
		t.Fatal(err)
	}
	if request.URL.Path != "/analyze-bundle" {
		t.Fatalf("request path = %q", request.URL.Path)
	}
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	var metadata analyzeRequest
	if err := json.Unmarshal([]byte(request.FormValue("metadata")), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.StagedPath != "" || len(metadata.SampleRatios) != 2 {
		t.Fatalf("metadata = %#v", metadata)
	}
	if files := request.MultipartForm.File["frames"]; len(files) != 2 {
		t.Fatalf("frame files = %d, want 2", len(files))
	}
}
