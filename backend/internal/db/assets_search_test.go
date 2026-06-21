package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestUpsertAssetNFOPreserveAndOverwrite(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := testSearchAsset("a.mp4", model.MediaTypeVideo)
	asset.NFOJSON = stringTestPtr(`{"filename":"a.nfo"}`)
	asset.NFOSearchText = stringTestPtr("old actor")
	asset.NFOScanned = true
	id, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	hasNFO, err := database.AssetHasNFO(ctx, "a.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !hasNFO {
		t.Fatal("expected asset to have nfo")
	}

	asset.NFOJSON = nil
	asset.NFOSearchText = nil
	asset.NFOScanned = false
	if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetAsset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.NFOSearchText == nil || *got.NFOSearchText != "old actor" {
		t.Fatalf("nfo search after normal scan = %#v, want preserved", got.NFOSearchText)
	}

	asset.NFOScanned = true
	if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetAsset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.NFOJSON != nil || got.NFOSearchText != nil {
		t.Fatalf("nfo after rebuild clear = %#v / %#v, want nil", got.NFOJSON, got.NFOSearchText)
	}
}

func TestUpsertAssetUpdatesTimelineWhenOnlyNFOTimeChanges(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := testSearchAsset("dated.mp4", model.MediaTypeVideo)
	asset.TimelineAt = 100
	asset.NFOJSON = stringTestPtr(`{"groups":[{"title":"基本","items":[{"key":"premiered","label":"首映时间","value":"2024-05-01","copyable":false}]}]}`)
	asset.NFOSearchText = stringTestPtr("dated")
	asset.NFOScanned = true
	id, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}

	asset.NFOJSON = nil
	asset.NFOSearchText = nil
	asset.NFOScanned = false
	asset.TimelineAt = 1714521600
	_, _, updated, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("expected timeline-only update")
	}
	got, err := database.GetAsset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimelineAt != 1714521600 {
		t.Fatalf("timeline_at = %d, want 1714521600", got.TimelineAt)
	}
	if got.NFOSearchText == nil || *got.NFOSearchText != "dated" {
		t.Fatalf("nfo search text = %#v, want preserved", got.NFOSearchText)
	}
}

func TestSearchAssetsFiltersNFOAndRanges(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	wide := testSearchAsset("wide.jpg", model.MediaTypeImage)
	wide.Width = intTestPtr(1920)
	wide.Height = intTestPtr(1080)
	wide.Size = 6 * 1024 * 1024
	wide.TimelineAt = 200
	wide.NFOJSON = stringTestPtr(`{"filename":"wide.nfo"}`)
	wide.NFOSearchText = stringTestPtr("alice tt123 favorite 2024 example title")
	wide.NFOScanned = true
	if _, _, _, err := database.UpsertAsset(ctx, wide); err != nil {
		t.Fatal(err)
	}
	portrait := testSearchAsset("portrait.jpg", model.MediaTypeImage)
	portrait.Width = intTestPtr(800)
	portrait.Height = intTestPtr(1200)
	portrait.Size = 1024
	portrait.TimelineAt = 300
	portrait.NFOJSON = stringTestPtr(`{"filename":"portrait.nfo"}`)
	portrait.NFOSearchText = stringTestPtr("alice portrait")
	portrait.NFOScanned = true
	if _, _, _, err := database.UpsertAsset(ctx, portrait); err != nil {
		t.Fatal(err)
	}
	video := testSearchAsset("clip.mp4", model.MediaTypeVideo)
	video.Width = intTestPtr(1280)
	video.Height = intTestPtr(720)
	video.Duration = floatTestPtr(180)
	video.Size = 20 * 1024 * 1024
	video.TimelineAt = 400
	if _, _, _, err := database.UpsertAsset(ctx, video); err != nil {
		t.Fatal(err)
	}

	from := int64(100)
	to := int64(250)
	minWidth := 1000
	maxHeight := 1100
	minSize := int64(5 * 1024 * 1024)
	page, err := database.SearchAssets(ctx, AssetListOptions{
		Page: 1, PageSize: 20, Type: model.MediaTypeImage, NFOQuery: "tt123", From: &from, To: &to,
		MinWidth: &minWidth, MaxHeight: &maxHeight, MinSize: &minSize, Orientation: "landscape", VisibleOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RelPath != "wide.jpg" {
		t.Fatalf("image search = %#v, want wide.jpg", page.Items)
	}

	minDuration := 120.0
	maxDuration := 240.0
	page, err = database.SearchAssets(ctx, AssetListOptions{
		Page: 1, PageSize: 20, Type: model.MediaTypeVideo, MinDuration: &minDuration, MaxDuration: &maxDuration, VisibleOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RelPath != "clip.mp4" {
		t.Fatalf("video search = %#v, want clip.mp4", page.Items)
	}
}

func TestSearchAssetsFiltersNFOFieldsAndAnyAxisResolution(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	wide := testSearchAsset("wide-fhd.mp4", model.MediaTypeVideo)
	wide.Width = intTestPtr(1920)
	wide.Height = intTestPtr(1080)
	wide.NFOJSON = stringTestPtr(`{"filename":"wide.nfo","fields":{},"groups":[{"title":"演员","items":[{"key":"actor","label":"演员","value":"Alice","copyable":true}]},{"title":"ID","items":[{"key":"uniqueid:imdb","label":"IMDB","value":"tt123","copyable":true}]},{"title":"标记","items":[{"key":"tag","label":"标签","value":"Action","copyable":false}]},{"title":"基本","items":[{"key":"title","label":"标题","value":"Sample Title","copyable":false},{"key":"year","label":"年份","value":"2024","copyable":false}]}]}`)
	wide.NFOSearchText = stringTestPtr("alice tt123 action sample title")
	wide.NFOScanned = true
	if _, _, _, err := database.UpsertAsset(ctx, wide); err != nil {
		t.Fatal(err)
	}

	portrait := testSearchAsset("portrait-fhd.mp4", model.MediaTypeVideo)
	portrait.Width = intTestPtr(1080)
	portrait.Height = intTestPtr(1920)
	portrait.NFOJSON = stringTestPtr(`{"filename":"portrait.nfo","fields":{},"groups":[{"title":"演员","items":[{"key":"actor","label":"演员","value":"Bob","copyable":true}]},{"title":"ID","items":[{"key":"uniqueid:tmdb","label":"TMDB","value":"tm456","copyable":true}]}]}`)
	portrait.NFOSearchText = stringTestPtr("bob tm456")
	portrait.NFOScanned = true
	if _, _, _, err := database.UpsertAsset(ctx, portrait); err != nil {
		t.Fatal(err)
	}

	widthMin := 1900
	widthMax := 2000
	heightMin := 1000
	heightMax := 1100
	page, err := database.SearchAssets(ctx, AssetListOptions{
		Page: 1, PageSize: 20, MinWidth: &widthMin, MaxWidth: &widthMax, MinHeight: &heightMin, MaxHeight: &heightMax, MatchAnyAxis: true, VisibleOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || !hasRelPath(page.Items, "wide-fhd.mp4") || !hasRelPath(page.Items, "portrait-fhd.mp4") {
		t.Fatalf("any-axis resolution search = %#v, want both orientations", page.Items)
	}

	page, err = database.SearchAssets(ctx, AssetListOptions{
		Page: 1, PageSize: 20, MinWidth: &widthMin, MaxWidth: &widthMax, MinHeight: &heightMin, MaxHeight: &heightMax, MatchAnyAxis: true, Orientation: "landscape", VisibleOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RelPath != "wide-fhd.mp4" {
		t.Fatalf("landscape resolution search = %#v, want wide-fhd.mp4", page.Items)
	}

	page, err = database.SearchAssets(ctx, AssetListOptions{
		Page: 1, PageSize: 20, NFOActor: "alice", NFOID: "tt123", NFOTag: "action", NFOTitle: "sample", NFOYear: "2024", VisibleOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RelPath != "wide-fhd.mp4" {
		t.Fatalf("nfo field search = %#v, want wide-fhd.mp4", page.Items)
	}

	options, err := database.NFOOptions(ctx, NFOOptionOptions{Field: "actor", Query: "ali", Limit: 10, VisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 || options[0] != "Alice" {
		t.Fatalf("actor options = %#v, want Alice", options)
	}

	options, err = database.NFOOptions(ctx, NFOOptionOptions{Field: "actor", Query: "missing", Limit: 10, VisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if options == nil || len(options) != 0 {
		t.Fatalf("empty actor options = %#v, want non-nil empty slice", options)
	}
}

func testSearchAsset(relPath string, mediaType string) AssetUpsert {
	return AssetUpsert{
		RelPath: relPath, ParentRelPath: ParentFolderRel(relPath), Filename: filepath.Base(relPath),
		Ext: filepath.Ext(relPath)[1:], MediaType: mediaType, Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10,
		CacheKey: relPath + "-cache", ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	}
}

func intTestPtr(value int) *int {
	return &value
}

func floatTestPtr(value float64) *float64 {
	return &value
}

func stringTestPtr(value string) *string {
	return &value
}

func hasRelPath(items []model.Asset, relPath string) bool {
	for _, item := range items {
		if item.RelPath == relPath {
			return true
		}
	}
	return false
}
