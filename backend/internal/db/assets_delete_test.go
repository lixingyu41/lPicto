package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"lpicto/backend/internal/model"
)

func TestPurgeAssetIDsDeletesAssetAndCascadesFileInstances(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	assetID, _, _, err := database.UpsertAsset(ctx, AssetUpsert{
		RelPath: "purge.jpg", ParentRelPath: "", Filename: "purge.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "purge-cache",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().ExecContext(ctx, `UPDATE file_instance SET missing = true WHERE asset_id = $1`, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetAssetRecordIncludingDeleted(ctx, assetID); err != nil {
		t.Fatalf("missing asset record lookup failed: %v", err)
	}
	purged, err := database.PurgeAssetIDs(ctx, []int64{assetID})
	if err != nil {
		t.Fatal(err)
	}
	if len(purged) != 1 || purged[0].ID != assetID || purged[0].CacheKey != "purge-cache" {
		t.Fatalf("purged = %#v", purged)
	}
	if _, err := database.GetAssetIncludingDeleted(ctx, assetID); err != sql.ErrNoRows {
		t.Fatalf("asset lookup error = %v, want sql.ErrNoRows", err)
	}
	var fileInstances int
	if err := database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM file_instance WHERE asset_id = $1`, assetID).Scan(&fileInstances); err != nil {
		t.Fatal(err)
	}
	if fileInstances != 0 {
		t.Fatalf("file instances = %d, want 0", fileInstances)
	}
}

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

func TestReclassifyAssetAsAudioClearsVisualWork(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	assetID, _, _, err := database.UpsertAsset(ctx, AssetUpsert{
		RelPath: "audio-only.mp4", Filename: "audio-only.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "audio-only",
		ThumbStatus: model.StatusError, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusError, VideoProxyStatus: model.StatusNotRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReclassifyAssetAsAudio(ctx, assetID, "audio/mp4", true); err != nil {
		t.Fatal(err)
	}
	asset, err := database.GetAsset(ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if asset.MediaType != model.MediaTypeAudio || asset.ThumbStatus != model.StatusNotRequired || asset.VideoPosterStatus != model.StatusNotRequired || !asset.BrowserPlayable {
		t.Fatalf("reclassified asset = %#v", asset)
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

func TestMarkMissingUnderKeepsRecordsAndMatchesPathBoundary(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, relPath := range []string{"dir/a.jpg", "dir/sub/b.jpg", "directory/c.jpg"} {
		if _, _, _, err := database.UpsertAsset(ctx, AssetUpsert{
			RelPath: relPath, ParentRelPath: ParentFolderRel(relPath), Filename: filepath.Base(relPath), Ext: "jpg", MediaType: model.MediaTypeImage,
			Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: strings.ReplaceAll(relPath, "/", "-"),
			ThumbStatus: model.StatusReady, PreviewStatus: model.StatusNotRequired,
			VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
		}); err != nil {
			t.Fatal(err)
		}
	}
	marked, err := database.MarkMissingUnder(ctx, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if marked != 2 {
		t.Fatalf("marked = %d, want 2", marked)
	}
	var missingDir, missingOther int
	if err := database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM file_instance WHERE rel_path LIKE 'dir/%' AND missing`).Scan(&missingDir); err != nil {
		t.Fatal(err)
	}
	if err := database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM file_instance WHERE rel_path LIKE 'directory/%' AND missing`).Scan(&missingOther); err != nil {
		t.Fatal(err)
	}
	if missingDir != 2 || missingOther != 0 {
		t.Fatalf("missing dir=%d directory=%d, want 2 and 0", missingDir, missingOther)
	}
	active, err := database.ActiveRelPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 3 {
		t.Fatalf("active records = %d, want 3", len(active))
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
	if thumb != model.StatusPending || preview != model.StatusNotRequired || poster != model.StatusPending || proxy != model.StatusNotRequired {
		t.Fatalf("browser image statuses = %q %q %q %q", thumb, preview, poster, proxy)
	}
	_, preview, _, _ = AssetStatuses(model.MediaTypeImage, false, true)
	if preview != model.StatusPending {
		t.Fatalf("non-browser image preview = %q, want pending", preview)
	}
	thumb, preview, poster, proxy = AssetStatuses(model.MediaTypeVideo, false, true)
	if thumb != model.StatusPending || preview != model.StatusNotRequired || poster != model.StatusPending || proxy != model.StatusNotRequired {
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
	thumb, preview, poster, proxy = AssetStatuses(model.MediaTypeAudio, false, true)
	if thumb != model.StatusNotRequired || preview != model.StatusNotRequired || poster != model.StatusNotRequired || proxy != model.StatusNotRequired {
		t.Fatalf("audio statuses = %q %q %q %q, want all not_required", thumb, preview, poster, proxy)
	}
}

func TestResetBackgroundVideoProxyWorkClearsStaleStatuses(t *testing.T) {
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
	asset.VideoProxyStatus = model.StatusProcessing
	unplayableID, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ResetBackgroundVideoProxyWork(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := database.PendingWork(ctx)
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
	unplayable, err := database.GetAsset(ctx, unplayableID)
	if err != nil {
		t.Fatal(err)
	}
	if unplayable.VideoProxyStatus != model.StatusNotRequired {
		t.Fatalf("unplayable proxy status = %q, want not_required", unplayable.VideoProxyStatus)
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
	items, err := database.PendingWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("pending work = %#v, want none", items)
	}
}
