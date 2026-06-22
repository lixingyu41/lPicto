package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestFolderGroupedLibraryOrderingPositionAndNeighbors(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ids := map[string]int64{}
	for _, item := range []struct {
		rel      string
		timeline int64
		size     int64
	}{
		{rel: "a/new.jpg", timeline: 400, size: 40},
		{rel: "a/old.jpg", timeline: 300, size: 30},
		{rel: "b/new.jpg", timeline: 500, size: 50},
		{rel: "b/old.jpg", timeline: 100, size: 10},
		{rel: "c/only.jpg", timeline: 200, size: 20},
	} {
		asset := testSearchAsset(item.rel, model.MediaTypeImage)
		asset.TimelineAt = item.timeline
		asset.Size = item.size
		id, _, _, err := database.UpsertAsset(ctx, asset)
		if err != nil {
			t.Fatal(err)
		}
		ids[item.rel] = id
	}

	page, err := database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 2, Sort: "timeline_desc", Group: assetGroupFolder})
	if err != nil {
		t.Fatal(err)
	}
	if got := assetRelPaths(page.Items); len(got) != 2 || got[0] != "b/new.jpg" || got[1] != "b/old.jpg" || !page.HasMore {
		t.Fatalf("folder grouped page 1 = %#v hasMore=%v, want b group and more", got, page.HasMore)
	}
	page, err = database.ListLibraryAssets(ctx, AssetListOptions{Page: 2, PageSize: 2, Sort: "timeline_desc", Group: assetGroupFolder})
	if err != nil {
		t.Fatal(err)
	}
	if got := assetRelPaths(page.Items); len(got) != 2 || got[0] != "a/new.jpg" || got[1] != "a/old.jpg" {
		t.Fatalf("folder grouped page 2 = %#v, want a group", got)
	}

	position, err := database.AssetPosition(ctx, ids["a/old.jpg"], AssetListOptions{PageSize: 2, Sort: "timeline_desc", Group: assetGroupFolder}, false)
	if err != nil {
		t.Fatal(err)
	}
	if position.Index != 3 || position.Page != 2 || position.Total != 5 {
		t.Fatalf("folder grouped position = %+v, want index 3 page 2 total 5", position)
	}

	neighbors, err := database.Neighbors(ctx, NeighborOptions{AssetID: ids["a/new.jpg"], Sort: "timeline_desc", Group: assetGroupFolder, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := assetRelPaths(neighbors.Previous); len(got) != 2 || got[0] != "b/old.jpg" || got[1] != "b/new.jpg" {
		t.Fatalf("folder grouped previous = %#v, want nearest b/old then b/new", got)
	}
	if got := assetRelPaths(neighbors.Next); len(got) != 2 || got[0] != "a/old.jpg" || got[1] != "c/only.jpg" {
		t.Fatalf("folder grouped next = %#v, want a/old then c/only", got)
	}
}

func TestFolderGroupedAnchorsAndAlbumAssets(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, item := range []struct {
		rel  string
		size int64
	}{
		{rel: "alpha/z.jpg", size: 20},
		{rel: "alpha/y.jpg", size: 10},
		{rel: "beta/a.jpg", size: 30},
		{rel: "root.jpg", size: 40},
	} {
		asset := testSearchAsset(item.rel, model.MediaTypeImage)
		asset.Size = item.size
		if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
			t.Fatal(err)
		}
	}

	anchors, err := database.LibraryAnchors(ctx, AssetListOptions{PageSize: 2, Sort: "filename_asc", Group: assetGroupFolder})
	if err != nil {
		t.Fatal(err)
	}
	if got := anchorLabels(anchors.Items); len(got) != 3 || got[0] != "/beta" || got[1] != "全部存储" || got[2] != "/alpha" {
		t.Fatalf("folder anchors = %#v, want beta, root, alpha", got)
	}
	if anchors.Items[1].Page != 1 || anchors.Items[2].Page != 2 {
		t.Fatalf("folder anchor pages = %+v, want root page 1 and alpha page 2", anchors.Items)
	}

	album, err := database.CreateAlbum(ctx, AlbumCreate{
		Name: "全部",
		Sources: []AlbumSourceCreate{{
			RelPath:           "",
			Recursive:         true,
			MediaTypeFilter:   AlbumMediaAll,
			OrientationFilter: AlbumOrientationAll,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := database.ListAlbumAssets(ctx, album.ID, AssetListOptions{Page: 1, PageSize: 10, Sort: "size_desc", Group: assetGroupFolder})
	if err != nil {
		t.Fatal(err)
	}
	if got := assetRelPaths(page.Items); len(got) != 4 || got[0] != "root.jpg" || got[1] != "beta/a.jpg" || got[2] != "alpha/z.jpg" || got[3] != "alpha/y.jpg" {
		t.Fatalf("folder grouped album assets = %#v, want root, beta, alpha group", got)
	}
}

func assetRelPaths(assets []model.Asset) []string {
	result := make([]string, 0, len(assets))
	for _, asset := range assets {
		result = append(result, asset.RelPath)
	}
	return result
}

func anchorLabels(anchors []LibraryAnchor) []string {
	result := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		result = append(result, anchor.Label)
	}
	return result
}
