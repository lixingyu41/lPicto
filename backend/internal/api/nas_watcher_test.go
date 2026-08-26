package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lpicto/backend/internal/config"
	"lpicto/backend/internal/storage"
)

func TestMapNASWatcherPathUsesConfiguredRootAndRejectsTraversal(t *testing.T) {
	store, err := storage.New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{NASWatcherRoots: map[string]string{"VID": "nas/VID"}}, store: store}
	root, relPath, ok := server.mapNASWatcherPath("vid", "folder/example.mp4")
	if !ok || root != "nas/VID" || relPath != "nas/VID/folder/example.mp4" {
		t.Fatalf("mapped root=%q path=%q ok=%v", root, relPath, ok)
	}
	if _, _, ok := server.mapNASWatcherPath("VID", "../outside.mp4"); ok {
		t.Fatal("traversal path was accepted")
	}
	if _, _, ok := server.mapNASWatcherPath("OTHER", "example.mp4"); ok {
		t.Fatal("unknown root was accepted")
	}
	if root, ok := server.mapNASWatcherRoot("vid"); !ok || root != "nas/VID" {
		t.Fatalf("mapped recovery root=%q ok=%v", root, ok)
	}
}

func TestNASWatcherNestedFileExcludesRootFiles(t *testing.T) {
	if nasWatcherNestedFile("root.mp4") {
		t.Fatal("root-level media was accepted")
	}
	if !nasWatcherNestedFile("folder/video.mp4") || !nasWatcherNestedFile("folder/sub/photo.jpg") {
		t.Fatal("nested media was rejected")
	}
}

func TestNASWatcherHeartbeatIsOptionalAndAuthenticated(t *testing.T) {
	server := &Server{
		cfg:        config.Config{NASWatcherToken: "secret"},
		nasWatcher: newNASWatcherIntegration(true, 90*time.Second),
	}
	unauthorized := httptest.NewRecorder()
	server.nasWatcherEvents(unauthorized, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"instanceId":"test","events":[]}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"instanceId":"FN-LXY","events":[]}`))
	request.Header.Set("Authorization", "Bearer secret")
	request.RemoteAddr = "192.168.2.50:12345"
	response := httptest.NewRecorder()
	server.nasWatcherEvents(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("heartbeat status = %d body=%s", response.Code, response.Body.String())
	}
	status := server.nasWatcher.status()
	if !status.Connected || status.InstanceID != "FN-LXY" || status.RemoteAddress != "192.168.2.50" {
		t.Fatalf("status = %#v", status)
	}
}

func TestNASWatcherDisabledDoesNotAffectApplication(t *testing.T) {
	server := &Server{cfg: config.Config{}, nasWatcher: newNASWatcherIntegration(false, 90*time.Second)}
	response := httptest.NewRecorder()
	server.nasWatcherEvents(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled status = %d", response.Code)
	}
	if task := server.nasWatcherSystemTask(); task.Status != "skipped" || len(task.Actions) != 0 {
		t.Fatalf("disabled task = %#v", task)
	}
}
