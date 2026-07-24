package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestResetMediaLibraryClearsMediaDataAndPreservesSettings(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.Conn().ExecContext(ctx, `INSERT INTO scan_library(public_id,name) VALUES('keep-library','保留图库')`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = database.UpsertAsset(ctx, AssetUpsert{
		RelPath: "reset.jpg", Filename: "reset.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "reset-cache",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Conn().ExecContext(ctx, `INSERT INTO smart_collections(name,rule_json,created_at,updated_at) VALUES('旧集合','{}',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Conn().ExecContext(ctx, `INSERT INTO app_settings(key,value) VALUES('system_collection_counts','{}') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}

	result, err := database.ResetMediaLibrary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedAssets != 1 {
		t.Fatalf("deleted assets = %d, want 1", result.DeletedAssets)
	}
	for _, table := range []string{"media_asset", "folder", "tag", "albums", "album_groups", "smart_collections", "scan_runs", "system_task_state"} {
		var count int
		if err = database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
	var libraries int
	if err = database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM scan_library WHERE public_id='keep-library'`).Scan(&libraries); err != nil {
		t.Fatal(err)
	}
	if libraries != 1 {
		t.Fatalf("preserved libraries = %d, want 1", libraries)
	}
	var cacheSetting int
	if err = database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE key='video_proxy_cache_ttl_seconds'`).Scan(&cacheSetting); err != nil {
		t.Fatal(err)
	}
	if cacheSetting != 1 {
		t.Fatal("video cache setting was removed")
	}
}
