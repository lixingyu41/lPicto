package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestFolderAssetsRecursiveOptionAndLiveCounts(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, asset := range []AssetUpsert{
		testAlbumAsset("Y/root.jpg", "Y", model.MediaTypeImage, 100, 100),
		testAlbumAsset("Y/child/nested.jpg", "Y/child", model.MediaTypeImage, 100, 100),
		testAlbumAsset("Z/other.jpg", "Z", model.MediaTypeImage, 100, 100),
	} {
		if _, _, _, err := database.UpsertAsset(ctx, asset); err != nil {
			t.Fatal(err)
		}
		if err := database.EnsureAssetFolders(ctx, asset.RelPath); err != nil {
			t.Fatal(err)
		}
	}

	folder, err := database.GetFolderByRel(ctx, "Y")
	if err != nil {
		t.Fatal(err)
	}
	if folder.AssetCount != 1 || folder.RecursiveAssetCount != 2 {
		t.Fatalf("folder counts = direct %d recursive %d, want 1 and 2", folder.AssetCount, folder.RecursiveAssetCount)
	}

	direct, err := database.ListFolderAssets(ctx, folder.ID, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename"})
	if err != nil {
		t.Fatal(err)
	}
	if got := relPaths(direct.Items); len(got) != 1 || got[0] != "Y/root.jpg" {
		t.Fatalf("direct folder assets = %#v, want Y/root.jpg", got)
	}

	recursive, err := database.ListFolderAssets(ctx, folder.ID, AssetListOptions{Page: 1, PageSize: 10, Sort: "filename", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := relPaths(recursive.Items); len(got) != 2 || got[0] != "Y/child/nested.jpg" || got[1] != "Y/root.jpg" {
		t.Fatalf("recursive folder assets = %#v, want nested and root", got)
	}

	if err := database.EnsureFolder(ctx, "Z"); err != nil {
		t.Fatal(err)
	}
	tree, err := database.FolderTreeWithRoots(ctx, []string{"Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !folderRelInTree(tree, "Z") {
		t.Fatalf("FolderTreeWithRoots did not keep empty scan root Z")
	}
}

func folderRelInTree(folders []model.Folder, rel string) bool {
	for _, folder := range folders {
		if folder.RelPath == rel {
			return true
		}
	}
	return false
}
