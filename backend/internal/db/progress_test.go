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
		ThumbStatus: model.StatusProcessing, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusPending,
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
	if progress.Thumb.Total != 2 || progress.Thumb.Ready != 1 || progress.Thumb.Processing != 1 {
		t.Fatalf("thumb counts = %#v", progress.Thumb)
	}
	if progress.Preview.Total != 1 || progress.Preview.Pending != 1 {
		t.Fatalf("preview counts = %#v", progress.Preview)
	}
	if progress.VideoPoster.Total != 1 || progress.VideoPoster.NotRequired != 1 {
		t.Fatalf("poster counts = %#v", progress.VideoPoster)
	}
	if progress.VideoProxy.Total != 1 || progress.VideoProxy.Pending != 1 {
		t.Fatalf("proxy counts = %#v", progress.VideoProxy)
	}
	if progress.Transcode.Total != 2 || progress.Transcode.Pending != 2 {
		t.Fatalf("transcode counts = %#v", progress.Transcode)
	}
}

func TestProcessingProgressForRootsCountsScopedAssets(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "family/a.jpg", ParentRelPath: "family", Filename: "a.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "family-a",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusPending,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "family/trip/b.mp4", ParentRelPath: "family/trip", Filename: "b.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 20, Mtime: 20, ImportedAt: 20, TimelineAt: 20, CacheKey: "family-b",
		ThumbStatus: model.StatusPending, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusProcessing,
	})
	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "work/c.jpg", ParentRelPath: "work", Filename: "c.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 30, Mtime: 30, ImportedAt: 30, TimelineAt: 30, CacheKey: "work-c",
		ThumbStatus: model.StatusError, PreviewStatus: model.StatusError,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "special%/d.jpg", ParentRelPath: "special%", Filename: "d.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 40, Mtime: 40, ImportedAt: 40, TimelineAt: 40, CacheKey: "special-d",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "specialx/e.jpg", ParentRelPath: "specialx", Filename: "e.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 50, Mtime: 50, ImportedAt: 50, TimelineAt: 50, CacheKey: "special-e",
		ThumbStatus: model.StatusPending, PreviewStatus: model.StatusPending,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})

	progress, err := database.ProcessingProgressForRoots(ctx, []string{"family"})
	if err != nil {
		t.Fatal(err)
	}
	if progress.AssetTotal != 2 || progress.ImageTotal != 1 || progress.VideoTotal != 1 {
		t.Fatalf("family totals = %#v", progress)
	}
	if progress.Thumb.Total != 2 || progress.Thumb.Ready != 1 || progress.Thumb.Pending != 1 {
		t.Fatalf("family thumb counts = %#v", progress.Thumb)
	}
	if progress.Transcode.Total != 2 || progress.Transcode.Pending != 1 || progress.Transcode.Processing != 1 {
		t.Fatalf("family transcode counts = %#v", progress.Transcode)
	}

	special, err := database.ProcessingProgressForRoots(ctx, []string{"special%"})
	if err != nil {
		t.Fatal(err)
	}
	if special.AssetTotal != 1 || special.Thumb.Ready != 1 {
		t.Fatalf("special root escaped counts = %#v", special)
	}

	empty, err := database.ProcessingProgressForRoots(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.AssetTotal != 0 || empty.Thumb.Total != 0 {
		t.Fatalf("empty roots counts = %#v, want zero", empty)
	}
	emptyCount, err := database.AssetCountForRoots(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if emptyCount != 0 {
		t.Fatalf("empty root asset count = %d, want 0", emptyCount)
	}
}

func TestResetAssetThumbnailsForRootsResetsScopedStatuses(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "family/a.jpg", ParentRelPath: "family", Filename: "a.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "family-a",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "family/b.mp4", ParentRelPath: "family", Filename: "b.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 20, Mtime: 20, ImportedAt: 20, TimelineAt: 20, CacheKey: "family-b",
		ThumbStatus: model.StatusError, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	insertProgressAsset(t, database, AssetUpsert{
		RelPath: "work/c.jpg", ParentRelPath: "work", Filename: "c.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 30, Mtime: 30, ImportedAt: 30, TimelineAt: 30, CacheKey: "work-c",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})

	reset, err := database.ResetAssetThumbnailsForRoots(ctx, []string{"family"})
	if err != nil {
		t.Fatal(err)
	}
	if reset != 2 {
		t.Fatalf("reset count = %d, want 2", reset)
	}
	family, err := database.ProcessingProgressForRoots(ctx, []string{"family"})
	if err != nil {
		t.Fatal(err)
	}
	if family.Thumb.Pending != 2 || family.Thumb.Ready != 0 || family.Thumb.Error != 0 {
		t.Fatalf("family thumb counts = %#v, want pending only", family.Thumb)
	}
	work, err := database.ProcessingProgressForRoots(ctx, []string{"work"})
	if err != nil {
		t.Fatal(err)
	}
	if work.Thumb.Ready != 1 || work.Thumb.Pending != 0 {
		t.Fatalf("work thumb counts = %#v, want untouched", work.Thumb)
	}
	var pendingJobs int
	err = database.conn.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM media_job mj
JOIN assets a ON a.id = mj.asset_id
WHERE a.rel_path LIKE 'family/%' AND mj.job_type = 'thumb' AND mj.status = 'pending'`).Scan(&pendingJobs)
	if err != nil {
		t.Fatal(err)
	}
	if pendingJobs != 2 {
		t.Fatalf("pending thumb jobs = %d, want 2", pendingJobs)
	}
}

func insertProgressAsset(t *testing.T, database *DB, asset AssetUpsert) {
	t.Helper()
	if _, _, _, err := database.UpsertAsset(context.Background(), asset); err != nil {
		t.Fatal(err)
	}
}
