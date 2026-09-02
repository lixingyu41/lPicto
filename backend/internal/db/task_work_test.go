package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"lpicto/backend/internal/media"
	"lpicto/backend/internal/model"
)

func TestContinueIncludesPendingAndFailedWork(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	pendingID := insertTaskWorkAsset(t, database, AssetUpsert{
		RelPath: "PIC/pending.jpg", ParentRelPath: "PIC", Filename: "pending.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "pending",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusPending,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	failedID := insertTaskWorkAsset(t, database, AssetUpsert{
		RelPath: "PIC/failed.jpg", ParentRelPath: "PIC", Filename: "failed.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 20, Mtime: 20, ImportedAt: 20, TimelineAt: 20, CacheKey: "failed",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusError,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})

	continuable, err := database.ContinueWorkForRoots(ctx, "preview", []string{"PIC"})
	if err != nil {
		t.Fatal(err)
	}
	if len(continuable) != 2 {
		t.Fatalf("continue items = %#v", continuable)
	}
	got := map[int64]bool{}
	for _, item := range continuable {
		got[item.AssetID] = true
	}
	if !got[pendingID] || !got[failedID] {
		t.Fatalf("continue items = %#v", continuable)
	}
	var status string
	if err := database.Conn().QueryRowContext(ctx, `SELECT preview_status FROM media_asset WHERE id=$1`, failedID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != model.StatusPending {
		t.Fatalf("failed preview status = %q, want pending", status)
	}
}

func TestMetadataContinueIncludesPendingAndFailedWork(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	pendingID := insertTaskWorkAsset(t, database, AssetUpsert{
		RelPath: "VID/pending.mp4", ParentRelPath: "VID", Filename: "pending.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "metadata-pending",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusReady, VideoProxyStatus: model.StatusNotRequired,
	})
	failedID := insertTaskWorkAsset(t, database, AssetUpsert{
		RelPath: "VID/failed.mp4", ParentRelPath: "VID", Filename: "failed.mp4", Ext: "mp4", MediaType: model.MediaTypeVideo,
		Size: 20, Mtime: 20, ImportedAt: 20, TimelineAt: 20, CacheKey: "metadata-failed",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusNotRequired,
		VideoPosterStatus: model.StatusReady, VideoProxyStatus: model.StatusNotRequired,
	})
	for id, status := range map[int64]string{pendingID: model.StatusPending, failedID: model.StatusError} {
		if _, err := database.Conn().ExecContext(ctx, `INSERT INTO media_job(asset_id,job_type,status,error_text) VALUES($1,'metadata',$2,'old error') ON CONFLICT(asset_id,job_type) DO UPDATE SET status=excluded.status,error_text=excluded.error_text`, id, status); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := database.MetadataWorkPathsForRoots(ctx, []string{"VID"})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("metadata continue paths = %#v", paths)
	}
	var status string
	var errorText *string
	if err := database.Conn().QueryRowContext(ctx, `SELECT status,error_text FROM media_job WHERE asset_id=$1 AND job_type='metadata'`, failedID).Scan(&status, &errorText); err != nil {
		t.Fatal(err)
	}
	if status != model.StatusPending || errorText != nil {
		t.Fatalf("failed metadata row = status %q, error %#v", status, errorText)
	}
}

func TestRepairVideoDisplayMetadataDimensionsOnlyUpdatesStaleQuarterTurns(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, testDatabaseURL(t, ctx), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	rotatedMetadata := `{"streams":[{"codec_type":"video","width":3840,"height":2160,"side_data_list":[{"rotation":-90}]}]}`
	unrotatedMetadata := `{"streams":[{"codec_type":"video","width":1920,"height":1080}]}`
	var staleID int64
	for _, item := range []struct {
		path     string
		width    int
		height   int
		metadata string
	}{
		{path: "VID/stale.mov", width: 3840, height: 2160, metadata: rotatedMetadata},
		{path: "VID/repaired.mov", width: 2160, height: 3840, metadata: rotatedMetadata},
		{path: "VID/landscape.mp4", width: 1920, height: 1080, metadata: unrotatedMetadata},
	} {
		asset := testSearchAsset(item.path, model.MediaTypeVideo)
		asset.Width = &item.width
		asset.Height = &item.height
		asset.MetadataJSON = &item.metadata
		id := insertTaskWorkAsset(t, database, asset)
		if item.path == "VID/stale.mov" {
			staleID = id
		}
	}

	updated, err := database.RepairVideoDisplayMetadataDimensions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("updated records = %d, want 1", updated)
	}
	stale, err := database.GetAsset(ctx, staleID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Width == nil || stale.Height == nil || *stale.Width != 2160 || *stale.Height != 3840 {
		t.Fatalf("repaired dimensions = %v x %v, want 2160 x 3840", stale.Width, stale.Height)
	}
}

func TestRepairEmbeddedMediaAuthorsMergesWithNFOAuthor(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, testDatabaseURL(t, ctx), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	metadata := `{"format":{"tags":{"ARTIST":"Video Creator"}}}`
	nfo := `{"filename":"movie.nfo","fields":{"作者":"NFO Author"},"groups":[{"title":"创作","items":[{"key":"writer","label":"作者","value":"NFO Author","copyable":true}]}]}`
	asset := testSearchAsset("VID/authored.mp4", model.MediaTypeVideo)
	asset.MetadataJSON = &metadata
	asset.NFOJSON = &nfo
	asset.NFOSearchText = stringTestPtr("nfo author")
	id := insertTaskWorkAsset(t, database, asset)

	updated, err := database.RepairEmbeddedMediaAuthors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("updated records = %d, want 1", updated)
	}
	stored, err := database.GetAsset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	info, err := media.ParseNFOJSON(*stored.NFOJSON)
	if err != nil {
		t.Fatal(err)
	}
	if info.Fields["作者"] != "NFO Author / Video Creator" {
		t.Fatalf("stored authors = %q", info.Fields["作者"])
	}
	if stored.NFOSearchText == nil || !strings.Contains(*stored.NFOSearchText, "video creator") {
		t.Fatalf("search text = %#v", stored.NFOSearchText)
	}
	updated, err = database.RepairEmbeddedMediaAuthors(ctx)
	if err != nil || updated != 0 {
		t.Fatalf("second repair = %d, %v; want no-op", updated, err)
	}
}

func insertTaskWorkAsset(t *testing.T, database *DB, asset AssetUpsert) int64 {
	t.Helper()
	id, _, _, err := database.UpsertAsset(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
