package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestMediaListSortsKeepListPositionAnchorsAndNeighborsAligned(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, testDatabaseURL(t, ctx), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	low := testSearchAsset("A/alpha.jpg", model.MediaTypeImage)
	low.Size, low.Mtime, low.ImportedAt, low.TimelineAt = 100, 100, 100, 100
	low.Width, low.Height, low.Duration, low.FPS = intTestPtr(640), intTestPtr(480), floatTestPtr(10), floatTestPtr(24)
	low.Container, low.VideoCodec, low.AudioCodec = stringTestPtr("matroska"), stringTestPtr("hevc"), stringTestPtr("opus")
	low.OverallBitrate, low.HasSubtitle = int64TestPtr(1_000_000), false

	high := testSearchAsset("B/zulu.mp4", model.MediaTypeVideo)
	high.Size, high.Mtime, high.ImportedAt, high.TimelineAt = 200, 200, 200, 200
	high.Width, high.Height, high.Duration, high.FPS = intTestPtr(1920), intTestPtr(1080), floatTestPtr(20), floatTestPtr(60)
	high.Container, high.VideoCodec, high.AudioCodec = stringTestPtr("mp4"), stringTestPtr("h264"), stringTestPtr("aac")
	high.OverallBitrate, high.HasSubtitle, high.HasDanmaku = int64TestPtr(5_000_000), true, true

	empty := testSearchAsset("C/empty.jpg", model.MediaTypeImage)
	empty.Size, empty.Mtime, empty.ImportedAt, empty.TimelineAt = 50, 50, 50, 50

	lowID, _, _, err := database.UpsertAsset(ctx, low)
	if err != nil {
		t.Fatal(err)
	}
	highID, _, _, err := database.UpsertAsset(ctx, high)
	if err != nil {
		t.Fatal(err)
	}
	emptyID, _, _, err := database.UpsertAsset(ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().ExecContext(ctx, `UPDATE media_asset SET rating=1 WHERE id=$1`, lowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().ExecContext(ctx, `UPDATE media_asset SET rating=5 WHERE id=$1`, highID); err != nil {
		t.Fatal(err)
	}
	saveTestAISummary(t, ctx, database, lowID, low.CacheKey, "alpha description", "alpha")
	saveTestAISummary(t, ctx, database, highID, high.CacheKey, "zulu description", "zulu")
	summaries, err := database.AssetAISummaries(ctx, []int64{lowID, highID, emptyID})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[lowID].Description != "alpha description" ||
		len(summaries[lowID].Tags) != 1 || summaries[lowID].Tags[0].Tag != "alpha" {
		t.Fatalf("AI summaries = %#v", summaries)
	}

	sorts := []string{
		"timeline_asc", "timeline_desc", "filename_asc", "filename_desc",
		"path_asc", "path_desc", "media_type_asc", "media_type_desc",
		"resolution_asc", "resolution_desc", "duration_asc", "duration_desc",
		"imported_asc", "imported_desc", "modified_asc", "modified_desc",
		"size_asc", "size_desc", "rating_asc", "rating_desc",
		"container_asc", "container_desc", "video_codec_asc", "video_codec_desc",
		"audio_codec_asc", "audio_codec_desc", "fps_asc", "fps_desc",
		"bitrate_asc", "bitrate_desc", "subtitle_asc", "subtitle_desc",
		"danmaku_asc", "danmaku_desc", "ai_description_asc", "ai_description_desc",
		"ai_tag_asc", "ai_tag_desc",
	}
	for _, sort := range sorts {
		t.Run(sort, func(t *testing.T) {
			assertMediaOrderContracts(t, ctx, database, AssetListOptions{Page: 1, PageSize: 10, Sort: sort})
		})
	}
	for _, group := range []string{"day", "month", "year", "size", "letter", "folder"} {
		t.Run("group_"+group, func(t *testing.T) {
			assertMediaOrderContracts(t, ctx, database, AssetListOptions{Page: 1, PageSize: 10, Sort: "rating_desc", Group: group})
		})
	}

	assertRelPathOrder(t, ctx, database, "resolution_desc", []string{high.RelPath, low.RelPath, empty.RelPath})
	assertRelPathOrder(t, ctx, database, "rating_desc", []string{high.RelPath, low.RelPath, empty.RelPath})
	assertRelPathOrder(t, ctx, database, "ai_tag_asc", []string{low.RelPath, high.RelPath, empty.RelPath})
}

func assertMediaOrderContracts(t *testing.T, ctx context.Context, database *DB, opts AssetListOptions) {
	t.Helper()
	page, err := database.ListLibraryAssets(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(page.Items))
	}
	anchors, err := database.LibraryAnchors(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if anchors.Total != len(page.Items) {
		t.Fatalf("anchor total = %d, list total = %d", anchors.Total, len(page.Items))
	}
	middle := page.Items[1]
	position, err := database.AssetPosition(ctx, middle.ID, opts, true)
	if err != nil {
		t.Fatal(err)
	}
	if position.Index != 1 || position.Total != len(page.Items) {
		t.Fatalf("position = %#v, want index 1 of %d", position, len(page.Items))
	}
	neighbors, err := database.AssetFilterNeighbors(ctx, middle.ID, opts, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors.Previous) != 1 || neighbors.Previous[0].ID != page.Items[0].ID {
		t.Fatalf("previous = %#v, want %d", assetRelPaths(neighbors.Previous), page.Items[0].ID)
	}
	if len(neighbors.Next) != 1 || neighbors.Next[0].ID != page.Items[2].ID {
		t.Fatalf("next = %#v, want %d", assetRelPaths(neighbors.Next), page.Items[2].ID)
	}
}

func assertRelPathOrder(t *testing.T, ctx context.Context, database *DB, sort string, want []string) {
	t.Helper()
	page, err := database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, Sort: sort})
	if err != nil {
		t.Fatal(err)
	}
	got := assetRelPaths(page.Items)
	if len(got) != len(want) {
		t.Fatalf("%s paths = %#v, want %#v", sort, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s paths = %#v, want %#v", sort, got, want)
		}
	}
}

func saveTestAISummary(t *testing.T, ctx context.Context, database *DB, assetID int64, cacheKey, description, tag string) {
	t.Helper()
	if err := database.EnsureAIQueued(ctx, assetID, cacheKey, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.MarkAIProcessing(ctx, assetID, cacheKey); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveAIResult(ctx, assetID, cacheKey, description, "clip", "v1", "vl", "v1", "tax-v1", json.RawMessage(`[]`), []AITag{{Tag: tag, Confidence: 0.8}}, nil); err != nil {
		t.Fatal(err)
	}
}

func int64TestPtr(value int64) *int64 {
	return &value
}
