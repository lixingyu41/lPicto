package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"lpicto/backend/internal/model"
)

func TestCaptureVideoPosterRejectsInvalidTime(t *testing.T) {
	handler, database, _ := testMissingSourceServer(t)
	id := testInsertAsset(t, database, "short.mp4", "capture-invalid-time", model.MediaTypeVideo)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/assets/"+strconv.FormatInt(id, 10)+"/video-poster/capture", bytes.NewBufferString("{\"timeSeconds\":-1}"))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestCaptureVideoPosterRejectsImage(t *testing.T) {
	handler, database, _ := testMissingSourceServer(t)
	id := testInsertAsset(t, database, "still.jpg", "capture-not-video", model.MediaTypeImage)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/assets/"+strconv.FormatInt(id, 10)+"/video-poster/capture", bytes.NewBufferString("{\"timeSeconds\":0}"))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}
