package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestProcessingProgressCountsActiveAssets(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "a.jpg", ParentRelPath: "", Filename: "a.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "a",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusPending,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "b.mp4", ParentRelPath: "", Filename: "b.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 20, Mtime: 20, ImportedAt: 20, TimelineAt: 20, CacheKey: "b",
		ThumbStatus: model.StatusNotRequired, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusProcessing, VideoProxyStatus: model.StatusNotRequired,
	})
	deletedID, _, _, err := database.UpsertAsset(ctx, AssetUpsert{
		RelPath: "deleted.jpg", ParentRelPath: "", Filename: "deleted.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 30, Mtime: 30, ImportedAt: 30, TimelineAt: 30, CacheKey: "deleted",
		ThumbStatus: model.StatusError, PreviewStatus: model.StatusError,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkDeleted(ctx, "deleted.jpg", 40); err != nil {
		t.Fatal(err)
	}
	_ = deletedID

	progress, err := database.ProcessingProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if progress.AssetTotal != 2 || progress.ImageTotal != 1 || progress.VideoTotal != 1 {
		t.Fatalf("asset totals = %#v", progress)
	}
	if progress.Thumb.Total != 1 || progress.Thumb.Ready != 1 {
		t.Fatalf("thumb counts = %#v", progress.Thumb)
	}
	if progress.Preview.Total != 1 || progress.Preview.Pending != 1 {
		t.Fatalf("preview counts = %#v", progress.Preview)
	}
	if progress.VideoPoster.Total != 1 || progress.VideoPoster.Processing != 1 {
		t.Fatalf("poster counts = %#v", progress.VideoPoster)
	}
	if progress.VideoProxy.Total != 1 || progress.VideoProxy.NotRequired != 1 {
		t.Fatalf("proxy counts = %#v", progress.VideoProxy)
	}
}

func insertProgressAsset(t *testing.T, database *DB, asset AssetUpsert) {
	t.Helper()
	if _, _, _, err := database.UpsertAsset(context.Background(), asset); err != nil {
		t.Fatal(err)
	}
}
