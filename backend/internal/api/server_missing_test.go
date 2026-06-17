package api

import (
	"context"
	"database/sql"
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
	asset, err := database.GetAssetIncludingDeleted(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if asset.DeletedAt == nil {
		t.Fatalf("DeletedAt = nil, want set")
	}
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}
