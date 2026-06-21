package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"lpicto/backend/internal/config"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
)

func TestOriginalMissingSourceMarksAssetDeleted(t *testing.T) {
	server, database, _ := testMissingSourceServer(t)
	id := testInsertAsset(t, database, "missing.jpg", "0123456789abcdefabcd", model.MediaTypeImage)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/assets/"+int64String(id)+"/original", nil)
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertAssetDeleted(t, database, id)
}

func TestCacheThumbMissingSourceMarksAssetDeleted(t *testing.T) {
	server, database, _ := testMissingSourceServer(t)
	cacheKey := "fedcba9876543210fedc"
	id := testInsertAsset(t, database, "missing.jpg", cacheKey, model.MediaTypeImage)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/cache/thumbs/"+cacheKey+".webp", nil)
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertAssetDeleted(t, database, id)
}

func TestMissingPhotoRootDoesNotMarkAssetDeleted(t *testing.T) {
	server, database, root := testMissingSourceServer(t)
	id := testInsertAsset(t, database, "missing.jpg", "aaaaaaaaaaaaaaaaaaaa", model.MediaTypeImage)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/assets/"+int64String(id)+"/original", nil)
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if _, err := database.GetAsset(context.Background(), id); err != nil {
		t.Fatalf("GetAsset err = %v, want active asset", err)
	}
}

func TestAlbumSourceFoldersStayInsideConfiguredLibraryAndKeepEmptyChildren(t *testing.T) {
	server, database, root := testMissingSourceServer(t)
	for _, rel := range []string{
		"Library/empty",
		"Nested/Deep/empty",
		"Nested/sibling",
		"Other/hidden",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SetScanLibraries(context.Background(), []db.ScanLibrary{
		{ID: "library", Name: "Library", Roots: []string{"Library"}},
		{ID: "nested", Name: "Nested", Roots: []string{"Nested/Deep"}},
	}); err != nil {
		t.Fatal(err)
	}

	rootPayload := getAlbumSourceFolders(t, server, "")
	if !sourceFolderInItems(rootPayload.Items, "Library") || !sourceFolderInItems(rootPayload.Items, "Nested/Deep") {
		t.Fatalf("root items = %#v, want configured library roots", rootPayload.Items)
	}
	if sourceFolderInItems(rootPayload.Items, "Nested") || sourceFolderInItems(rootPayload.Items, "Other") {
		t.Fatalf("root items = %#v, want non-library folders hidden", rootPayload.Items)
	}

	libraryPayload := getAlbumSourceFolders(t, server, "Library")
	if !sourceFolderInItems(libraryPayload.Items, "Library/empty") {
		t.Fatalf("Library items = %#v, want empty child folder", libraryPayload.Items)
	}

	nestedPayload := getAlbumSourceFolders(t, server, "Nested")
	if len(nestedPayload.Items) != 0 {
		t.Fatalf("Nested items = %#v, want hidden parent empty", nestedPayload.Items)
	}

	deepPayload := getAlbumSourceFolders(t, server, "Nested/Deep")
	if !sourceFolderInItems(deepPayload.Items, "Nested/Deep/empty") {
		t.Fatalf("Nested/Deep items = %#v, want empty child folder", deepPayload.Items)
	}
}

func testMissingSourceServer(t *testing.T) (http.Handler, *db.DB, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dataRoot := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dataRoot, "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	store, err := storage.New(root, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewServer(config.Config{PageSizeDefault: 100, PageSizeMax: 500}, database, store, nil, nil, events.NewBus(), logger)
	return handler, database, root
}

type testSourceFoldersResponse struct {
	Current SourceFolderDTO   `json:"current"`
	Items   []SourceFolderDTO `json:"items"`
	Warning string            `json:"warning"`
}

func getAlbumSourceFolders(t *testing.T, handler http.Handler, parentRelPath string) testSourceFoldersResponse {
	t.Helper()
	target := "/api/albums/source-folders"
	if parentRelPath != "" {
		target += "?parentRelPath=" + parentRelPath
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var payload testSourceFoldersResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func sourceFolderInItems(items []SourceFolderDTO, relPath string) bool {
	_, ok := findSourceFolder(items, relPath)
	return ok
}

func findSourceFolder(items []SourceFolderDTO, relPath string) (SourceFolderDTO, bool) {
	for _, item := range items {
		if item.RelPath == relPath {
			return item, true
		}
	}
	return SourceFolderDTO{}, false
}

func testInsertAsset(t *testing.T, database *db.DB, relPath string, cacheKey string, mediaType string) int64 {
	t.Helper()
	id, _, _, err := database.UpsertAsset(context.Background(), db.AssetUpsert{
		RelPath:           relPath,
		ParentRelPath:     "",
		Filename:          relPath,
		Ext:               filepath.Ext(relPath)[1:],
		MediaType:         mediaType,
		Size:              10,
		Mtime:             10,
		ImportedAt:        10,
		TimelineAt:        10,
		CacheKey:          cacheKey,
		BrowserPlayable:   mediaType == model.MediaTypeImage,
		ThumbStatus:       model.StatusReady,
		PreviewStatus:     model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired,
		VideoProxyStatus:  model.StatusNotRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertAssetDeleted(t *testing.T, database *db.DB, id int64) {
	t.Helper()
	if _, err := database.GetAsset(context.Background(), id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetAsset err = %v, want sql.ErrNoRows", err)
	}
	var deletedAt sql.NullTime
	err := database.Conn().QueryRowContext(context.Background(), `SELECT deleted_at FROM media_asset WHERE id = $1`, id).Scan(&deletedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !deletedAt.Valid {
		t.Fatalf("DeletedAt = nil, want set")
	}
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}
