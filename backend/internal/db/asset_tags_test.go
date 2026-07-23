package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestAssetTagsAddListDeleteAndRejectEmpty(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	assetID, _, _, err := database.UpsertAsset(ctx, testSearchAsset("tagged.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	tag, err := database.AddAssetTag(ctx, assetID, "  favorite  ")
	if err != nil {
		t.Fatal(err)
	}
	if tag.Name != "favorite" {
		t.Fatalf("tag name = %q, want favorite", tag.Name)
	}
	if _, err := database.AddAssetTag(ctx, assetID, "favorite"); err != nil {
		t.Fatal(err)
	}
	tags, err := database.ListAssetTags(ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "favorite" {
		t.Fatalf("tags = %#v, want one favorite", tags)
	}
	if _, err := database.AddAssetTag(ctx, assetID, " \t "); !errors.Is(err, ErrEmptyAssetTag) {
		t.Fatalf("empty add error = %v, want ErrEmptyAssetTag", err)
	}
	if err := database.DeleteAssetTag(ctx, assetID, " favorite "); err != nil {
		t.Fatal(err)
	}
	tags, err = database.ListAssetTags(ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("tags after delete = %#v, want empty", tags)
	}
}

func TestListAssetsByTagFiltersManualTags(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	videoA, _, _, err := database.UpsertAsset(ctx, testSearchAsset("a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	videoB, _, _, err := database.UpsertAsset(ctx, testSearchAsset("b.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	image, _, _, err := database.UpsertAsset(ctx, testSearchAsset("c.jpg", model.MediaTypeImage))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.AddAssetTag(ctx, videoA, "manual"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.AddAssetTag(ctx, image, "manual"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.AddAssetTag(ctx, videoB, "other"); err != nil {
		t.Fatal(err)
	}

	page, err := database.ListAssetsByTag(ctx, " manual ", AssetListOptions{Page: 1, PageSize: 10, Sort: "filename", VisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].RelPath != "a.mp4" || page.Items[1].RelPath != "c.jpg" {
		t.Fatalf("tagged assets = %#v, want a.mp4 and c.jpg", page.Items)
	}
	page, err = database.ListAssetsByTag(ctx, " manual ", AssetListOptions{Page: 1, PageSize: 10, Sort: "filename", VisibleOnly: true, Type: model.MediaTypeVideo})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RelPath != "a.mp4" {
		t.Fatalf("tagged video assets = %#v, want a.mp4 only", page.Items)
	}
	if _, err := database.ListAssetsByTag(ctx, " ", AssetListOptions{Page: 1, PageSize: 10}); !errors.Is(err, ErrEmptyAssetTag) {
		t.Fatalf("empty filter error = %v, want ErrEmptyAssetTag", err)
	}
}
