package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"lpicto/backend/internal/model"
)

func TestNormalizeRotation(t *testing.T) {
	tests := map[int]int{
		0:   0,
		90:  90,
		180: 180,
		270: 270,
		360: 0,
		280: 0,
		-90: 270,
	}
	for input, want := range tests {
		if got := NormalizeRotation(input); got != want {
			t.Fatalf("NormalizeRotation(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestAlbumOrientationUsesRotation(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	assetID, _, _, err := database.UpsertAsset(ctx, AssetUpsert{
		RelPath:           "v.mp4",
		ParentRelPath:     "",
		Filename:          "v.mp4",
		Ext:               "mp4",
		MediaType:         model.MediaTypeVideo,
		Size:              100,
		Mtime:             10,
		Width:             intValue(1920),
		Height:            intValue(1080),
		ImportedAt:        10,
		TimelineAt:        10,
		CacheKey:          "a",
		ThumbStatus:       model.StatusNotRequired,
		PreviewStatus:     model.StatusNotRequired,
		VideoPosterStatus: model.StatusReady,
		VideoProxyStatus:  model.StatusNotRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	album, err := database.CreateAlbum(ctx, AlbumCreate{
		Name:              "竖屏",
		FolderRelPaths:    []string{""},
		MediaTypeFilter:   model.MediaTypeVideo,
		OrientationFilter: AlbumOrientationTall,
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := database.ListAlbumAssets(ctx, album.ID, AssetListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("portrait album before rotation len = %d, want 0", len(page.Items))
	}
	if _, err := database.SetAssetRotation(ctx, assetID, 90); err != nil {
		t.Fatal(err)
	}
	page, err = database.ListAlbumAssets(ctx, album.ID, AssetListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("portrait album after rotation len = %d, want 1", len(page.Items))
	}
}

func TestAlbumSourceRecursiveFlag(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("root.jpg", "", model.MediaTypeImage, 100, 100)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("dir/child.jpg", "dir", model.MediaTypeImage, 100, 100)); err != nil {
		t.Fatal(err)
	}
	album, err := database.CreateAlbum(ctx, AlbumCreate{
		Name: "仅本层",
		Sources: []AlbumSourceCreate{{
			RelPath:           "",
			Recursive:         false,
			MediaTypeFilter:   AlbumMediaAll,
			OrientationFilter: AlbumOrientationAll,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := database.ListAlbumAssets(ctx, album.ID, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RelPath != "root.jpg" {
		t.Fatalf("non-recursive album items = %#v, want root.jpg only", page.Items)
	}
}

func TestAlbumRepeatableSourceFilters(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("media/a.jpg", "media", model.MediaTypeImage, 1200, 800)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("media/b.mp4", "media", model.MediaTypeVideo, 800, 1200)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("media/c.mp4", "media", model.MediaTypeVideo, 1200, 800)); err != nil {
		t.Fatal(err)
	}
	album, err := database.CreateAlbum(ctx, AlbumCreate{
		Name: "重复筛选",
		Sources: []AlbumSourceCreate{
			{RelPath: "media", Recursive: true, MediaTypeFilter: model.MediaTypeImage, OrientationFilter: AlbumOrientationAll},
			{RelPath: "media", Recursive: true, MediaTypeFilter: model.MediaTypeVideo, OrientationFilter: AlbumOrientationTall},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := database.ListAlbumAssets(ctx, album.ID, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename"})
	if err != nil {
		t.Fatal(err)
	}
	if got := albumRelPaths(page.Items); len(got) != 2 || got[0] != "media/a.jpg" || got[1] != "media/b.mp4" {
		t.Fatalf("repeatable album filters = %#v, want media/a.jpg and media/b.mp4", got)
	}
}

func TestUpdateAlbumAndGroups(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("a/one.jpg", "a", model.MediaTypeImage, 100, 100)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("b/two.jpg", "b", model.MediaTypeImage, 100, 100)); err != nil {
		t.Fatal(err)
	}
	group, err := database.CreateAlbumGroup(ctx, AlbumGroupCreate{Name: "收藏"})
	if err != nil {
		t.Fatal(err)
	}
	album, err := database.CreateAlbum(ctx, AlbumCreate{
		Name:    "旧相册",
		GroupID: &group.ID,
		Sources: []AlbumSourceCreate{{
			RelPath:           "a",
			Recursive:         true,
			MediaTypeFilter:   AlbumMediaAll,
			OrientationFilter: AlbumOrientationAll,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if album.GroupID == nil || *album.GroupID != group.ID {
		t.Fatalf("album group = %v, want %d", album.GroupID, group.ID)
	}

	updated, err := database.UpdateAlbum(ctx, album.ID, AlbumCreate{
		Name: "新相册",
		Sources: []AlbumSourceCreate{{
			RelPath:           "b",
			Recursive:         true,
			MediaTypeFilter:   AlbumMediaAll,
			OrientationFilter: AlbumOrientationAll,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "新相册" || updated.GroupID != nil {
		t.Fatalf("updated album = %#v, want renamed and ungrouped", updated)
	}
	page, err := database.ListAlbumAssets(ctx, album.ID, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename"})
	if err != nil {
		t.Fatal(err)
	}
	if got := albumRelPaths(page.Items); len(got) != 1 || got[0] != "b/two.jpg" {
		t.Fatalf("updated album assets = %#v, want b/two.jpg", got)
	}
}

func TestPendingWorkDoesNotRecoverProcessingVideoProxy(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := testAlbumAsset("stale.mp4", "", model.MediaTypeVideo, 1920, 1080)
	asset.VideoProxyStatus = model.StatusProcessing
	asset.ThumbStatus = model.StatusNotRequired
	asset.PreviewStatus = model.StatusNotRequired
	asset.VideoPosterStatus = model.StatusReady
	_, _, _, err = database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	items, err := database.PendingWork(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("PendingWork processing proxy = %#v, want no background video_proxy work", items)
	}
}

func intValue(value int) *int {
	return &value
}

func testAlbumAsset(relPath string, parent string, mediaType string, width int, height int) AssetUpsert {
	return AssetUpsert{
		RelPath:           relPath,
		ParentRelPath:     parent,
		Filename:          filepath.Base(relPath),
		Ext:               strings.TrimPrefix(filepath.Ext(relPath), "."),
		MediaType:         mediaType,
		Size:              100,
		Mtime:             10,
		Width:             intValue(width),
		Height:            intValue(height),
		ImportedAt:        10,
		TimelineAt:        10,
		CacheKey:          relPath,
		ThumbStatus:       model.StatusReady,
		PreviewStatus:     model.StatusReady,
		VideoPosterStatus: model.StatusReady,
		VideoProxyStatus:  model.StatusNotRequired,
	}
}

func albumRelPaths(assets []model.Asset) []string {
	result := make([]string, 0, len(assets))
	for _, asset := range assets {
		result = append(result, asset.RelPath)
	}
	return result
}
