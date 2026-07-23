package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestFilenameSortKeyLatinizesCommonScripts(t *testing.T) {
	cases := map[string]string{
		"北京.jpg":    "bj.jpg",
		"中文.jpg":    "zw.jpg",
		"かな.mp4":   "kn.mp4",
		"안녕.jpg":    "an.jpg",
		"쭌힣.jpg":    "jh.jpg",
		"Éclair.jpg": "eclair.jpg",
	}
	for name, want := range cases {
		if got := filenameSortKey(name); got != want {
			t.Fatalf("filenameSortKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFilenameSortAndAnchorsUseLatinizedKeys(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, rel := range []string{
		"中文.jpg",
		"北京.jpg",
		"かな.jpg",
		"안녕.jpg",
		"Éclair.jpg",
	} {
		if _, _, _, err := database.UpsertAsset(ctx, testSearchAsset(rel, model.MediaTypeImage)); err != nil {
			t.Fatal(err)
		}
	}

	page, err := database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename_asc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := assetRelPaths(page.Items); len(got) != 5 || got[0] != "안녕.jpg" || got[1] != "北京.jpg" || got[2] != "Éclair.jpg" || got[3] != "かな.jpg" || got[4] != "中文.jpg" {
		t.Fatalf("filename sorted rels = %#v, want latinized A/B/E/K/Z order", got)
	}
	if page.Items[0].FilenameSortKey != "an.jpg" || page.Items[1].FilenameSortKey != "bj.jpg" {
		t.Fatalf("filename sort keys = %q, %q, want an.jpg, bj.jpg", page.Items[0].FilenameSortKey, page.Items[1].FilenameSortKey)
	}

	anchors, err := database.LibraryAnchors(ctx, AssetListOptions{PageSize: 10, Sort: "filename_asc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := anchorLabels(anchors.Items); len(got) != 5 || got[0] != "A" || got[1] != "B" || got[2] != "E" || got[3] != "K" || got[4] != "Z" {
		t.Fatalf("filename anchors = %#v, want A/B/E/K/Z", got)
	}
}
