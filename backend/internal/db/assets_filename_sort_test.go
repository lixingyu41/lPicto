package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestFilenameSortKeyLatinizesCommonScripts(t *testing.T) {
	cases := map[string]string{
		"北京.jpg":     "bj.jpg",
		"中文.jpg":     "zw.jpg",
		"かな.mp4":     "kn.mp4",
		"안녕.jpg":     "an.jpg",
		"쭌힣.jpg":     "jh.jpg",
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

func TestFilenameSortUsesWholeNumericRuns(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	want := []string{
		"1.jpg", "2.jpg", "10.jpg", "100.jpg",
		"ABCD2.jpg", "ABCD10.jpg",
		"角色a 001.jpg", "角色a 002.jpg", "角色a 110.jpg",
		"角色a1.jpg", "角色a2.jpg", "角色a10.jpg", "角色a110.jpg",
	}
	ids := make(map[string]int64, len(want))
	for index := len(want) - 1; index >= 0; index-- {
		assetID, _, _, err := database.UpsertAsset(ctx, testSearchAsset(want[index], model.MediaTypeImage))
		if err != nil {
			t.Fatal(err)
		}
		ids[want[index]] = assetID
	}

	page, err := database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 20, Sort: "filename_asc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := assetRelPaths(page.Items); !stringSlicesEqual(got, want) {
		t.Fatalf("natural filename order = %#v, want %#v", got, want)
	}
	descending, err := database.ListLibraryAssets(ctx, AssetListOptions{Page: 1, PageSize: 20, Sort: "filename_desc"})
	if err != nil {
		t.Fatal(err)
	}
	wantDescending := make([]string, len(want))
	for index := range want {
		wantDescending[index] = want[len(want)-1-index]
	}
	if got := assetRelPaths(descending.Items); !stringSlicesEqual(got, wantDescending) {
		t.Fatalf("descending natural filename order = %#v, want %#v", got, wantDescending)
	}

	neighbors, err := database.AssetFilterNeighbors(ctx, ids["角色a2.jpg"], AssetListOptions{Sort: "filename_asc"}, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := assetRelPaths(neighbors.Previous); !stringSlicesEqual(got, []string{"角色a1.jpg"}) {
		t.Fatalf("previous natural neighbor = %#v, want 角色a1.jpg", got)
	}
	if got := assetRelPaths(neighbors.Next); !stringSlicesEqual(got, []string{"角色a10.jpg"}) {
		t.Fatalf("next natural neighbor = %#v, want 角色a10.jpg", got)
	}
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
