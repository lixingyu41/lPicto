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

func TestListAlbumsUsesCachedStatsUntilRefresh(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("a/one.jpg", "a", model.MediaTypeImage, 100, 100)); err != nil {
		t.Fatal(err)
	}
	album, err := database.CreateAlbum(ctx, AlbumCreate{
		Name:           "缓存统计",
		FolderRelPaths: []string{""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if album.AssetCount != 1 || album.StatsUpdatedAt == 0 {
		t.Fatalf("created album stats = count %d updated %d, want cached count 1", album.AssetCount, album.StatsUpdatedAt)
	}

	if _, _, _, err := database.UpsertAsset(ctx, testAlbumAsset("b/two.jpg", "b", model.MediaTypeImage, 100, 100)); err != nil {
		t.Fatal(err)
	}
	albums, err := database.ListAlbums(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := albumByID(albums, album.ID).AssetCount; got != 1 {
		t.Fatalf("cached album count after asset insert = %d, want 1 before refresh", got)
	}
	if err := database.RefreshAlbumStats(ctx); err != nil {
		t.Fatal(err)
	}
	albums, err = database.ListAlbums(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := albumByID(albums, album.ID).AssetCount; got != 2 {
		t.Fatalf("cached album count after refresh = %d, want 2", got)
	}
}

func TestLibraryAlbumIDFilters(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, asset := range []AssetUpsert{
		testAlbumAsset("a/one.jpg", "a", model.MediaTypeImage, 100, 100),
		testAlbumAsset("b/two.jpg", "b", model.MediaTypeImage, 100, 100),
		testAlbumAsset("c/three.jpg", "c", model.MediaTypeImage, 100, 100),
	} {
		if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
			t.Fatal(err)
		}
	}
	albumA, err := database.CreateAlbum(ctx, AlbumCreate{
		Name: "A",
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
	albumB, err := database.CreateAlbum(ctx, AlbumCreate{
		Name: "B",
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
	albumA2, err := database.CreateAlbum(ctx, AlbumCreate{
		Name: "A2",
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

	page, err := database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename", VisibleOnly: true, AlbumIDs: []int64{albumA.ID, albumB.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if got := albumRelPaths(page.Items); len(got) != 2 || got[0] != "a/one.jpg" || got[1] != "b/two.jpg" {
		t.Fatalf("multi album filter = %#v, want a/one.jpg and b/two.jpg", got)
	}

	page, err = database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename", VisibleOnly: true, AlbumIDs: []int64{albumA.ID, albumA2.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if got := albumRelPaths(page.Items); len(got) != 1 || got[0] != "a/one.jpg" {
		t.Fatalf("overlapping album filter = %#v, want a/one.jpg once", got)
	}

	page, err = database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename", VisibleOnly: true, AlbumUnassigned: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := albumRelPaths(page.Items); len(got) != 1 || got[0] != "c/three.jpg" {
		t.Fatalf("unassigned album filter = %#v, want c/three.jpg", got)
	}

	page, err = database.ListAlbumAssets(ctx, albumA.ID, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename", VisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := albumRelPaths(page.Items); len(got) != 1 || got[0] != "a/one.jpg" {
		t.Fatalf("single album compatibility = %#v, want a/one.jpg", got)
	}
}

func TestLibraryImageSizeNeighbors(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	large := testAlbumAsset("large.jpg", "", model.MediaTypeImage, 100, 100)
	large.Size = 300
	medium := testAlbumAsset("medium.jpg", "", model.MediaTypeImage, 100, 100)
	medium.Size = 200
	small := testAlbumAsset("small.jpg", "", model.MediaTypeImage, 100, 100)
	small.Size = 100
	video := testAlbumAsset("video.mp4", "", model.MediaTypeVideo, 100, 100)
	video.Size = 250
	if _, _, _, err := database.UpsertAsset(ctx, large); err != nil {
		t.Fatal(err)
	}
	mediumID, _, _, err := database.UpsertAsset(ctx, medium)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.UpsertAsset(ctx, small); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.UpsertAsset(ctx, video); err != nil {
		t.Fatal(err)
	}

	rating := 0
	neighbors, err := database.Neighbors(ctx, NeighborOptions{
		Context: "library",
		AssetID: mediumID,
		Type:    model.MediaTypeImage,
		Sort:    "size_desc",
		Rating:  &rating,
		Limit:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors.Previous) != 1 || neighbors.Previous[0].RelPath != "large.jpg" {
		t.Fatalf("previous = %#v, want large.jpg", albumRelPaths(neighbors.Previous))
	}
	if len(neighbors.Next) != 1 || neighbors.Next[0].RelPath != "small.jpg" {
		t.Fatalf("next = %#v, want small.jpg", albumRelPaths(neighbors.Next))
	}
}

func albumByID(albums []model.Album, id int64) model.Album {
	for _, album := range albums {
		if album.ID == id {
			return album
		}
	}
	return model.Album{}
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
	items, err := database.PendingWork(ctx)
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
