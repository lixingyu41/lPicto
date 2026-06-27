package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestMarkDeletedWithCacheReturnsCacheKey(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, _, _, err := database.UpsertAsset(ctx, AssetUpsert{
		RelPath: "a.jpg", ParentRelPath: "", Filename: "a.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "old-cache",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := database.MarkDeletedWithCache(ctx, "a.jpg", 20)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || deleted.CacheKey != "old-cache" || deleted.RelPath != "a.jpg" {
		t.Fatalf("deleted = %#v, want old-cache a.jpg", deleted)
	}
	deleted, err = database.MarkDeletedWithCache(ctx, "a.jpg", 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != nil {
		t.Fatalf("second delete = %#v, want nil", deleted)
	}
}

func TestMarkDeletedUnderReturnsNestedAssets(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, asset := range []AssetUpsert{
		{RelPath: "dir/a.jpg", ParentRelPath: "dir", Filename: "a.jpg", Ext: "jpg", MediaType: model.MediaTypeImage, CacheKey: "a"},
		{RelPath: "dir/sub/b.jpg", ParentRelPath: "dir/sub", Filename: "b.jpg", Ext: "jpg", MediaType: model.MediaTypeImage, CacheKey: "b"},
		{RelPath: "other.jpg", ParentRelPath: "", Filename: "other.jpg", Ext: "jpg", MediaType: model.MediaTypeImage, CacheKey: "other"},
	} {
		asset.Size = 10
		asset.Mtime = 10
		asset.ImportedAt = 10
		asset.TimelineAt = 10
		asset.ThumbStatus = model.StatusReady
		asset.PreviewStatus = model.StatusReady
		asset.VideoPosterStatus = model.StatusNotRequired
		asset.VideoProxyStatus = model.StatusNotRequired
		if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := database.MarkDeletedUnder(ctx, "dir", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted len = %d, want 2: %#v", len(deleted), deleted)
	}
	active, err := database.ActiveRelPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := active["other.jpg"]; !ok || len(active) != 1 {
		t.Fatalf("active = %#v, want only other.jpg", active)
	}
}

func TestUpsertAssetDetailedReportsOldCacheKey(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := AssetUpsert{
		RelPath: "a.jpg", ParentRelPath: "", Filename: "a.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "old",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	}
	if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	asset.Size = 11
	asset.CacheKey = "new"
	result, err := database.UpsertAssetDetailed(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.OldCacheKey != "old" {
		t.Fatalf("result = %#v, want updated old cache", result)
	}
}

func TestAssetStatusesSkipsPreviewForBrowserPlayableImages(t *testing.T) {
	thumb, preview, poster, proxy := AssetStatuses(model.MediaTypeImage, true, true)
	if thumb != model.StatusPending || preview != model.StatusNotRequired || poster != model.StatusNotRequired || proxy != model.StatusNotRequired {
		t.Fatalf("browser image statuses = %q %q %q %q", thumb, preview, poster, proxy)
	}
	_, preview, _, _ = AssetStatuses(model.MediaTypeImage, false, true)
	if preview != model.StatusPending {
		t.Fatalf("non-browser image preview = %q, want pending", preview)
	}
	thumb, preview, poster, proxy = AssetStatuses(model.MediaTypeVideo, false, true)
	if thumb != model.StatusPending || preview != model.StatusNotRequired || poster != model.StatusNotRequired || proxy != model.StatusPending {
		t.Fatalf("non-browser video statuses = %q %q %q %q", thumb, preview, poster, proxy)
	}
	thumb, preview, poster, proxy = AssetStatuses(model.MediaTypeVideo, true, true)
	if thumb != model.StatusPending || preview != model.StatusNotRequired || poster != model.StatusNotRequired || proxy != model.StatusNotRequired {
		t.Fatalf("browser video statuses = %q %q %q %q", thumb, preview, poster, proxy)
	}
	_, _, _, proxy = AssetStatuses(model.MediaTypeVideo, true, false)
	if proxy != model.StatusNotRequired {
		t.Fatalf("disabled video proxy status = %q, want not_required", proxy)
	}
}

func TestEnableVideoProxiesMarksOnlyUnplayableVideosPending(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := AssetUpsert{
		RelPath: "playable.mp4", ParentRelPath: "", Filename: "playable.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "playable", BrowserPlayable: true,
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	}
	playableID, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	asset.RelPath = "unplayable.mkv"
	asset.Filename = "unplayable.mkv"
	asset.Ext = "mkv"
	asset.CacheKey = "unplayable"
	asset.BrowserPlayable = false
	unplayableID, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.EnableVideoProxies(ctx); err != nil {
		t.Fatal(err)
	}
	_ = unplayableID
	items, err := database.PendingWork(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("pending work = %#v, want no background video proxy work", items)
	}
	playable, err := database.GetAsset(ctx, playableID)
	if err != nil {
		t.Fatal(err)
	}
	if playable.VideoProxyStatus != model.StatusNotRequired {
		t.Fatalf("playable proxy status = %q, want not_required", playable.VideoProxyStatus)
	}
}

func TestPendingWorkSkipsPlayableVideoProxy(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := AssetUpsert{
		RelPath: "playable.mp4", ParentRelPath: "", Filename: "playable.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "playable", BrowserPlayable: true,
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusProcessing,
	}
	if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	items, err := database.PendingWork(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("pending work = %#v, want none", items)
	}
}
