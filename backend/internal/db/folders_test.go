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
		testFolderAsset("Y/root.jpg", "Y", model.MediaTypeImage),
		testFolderAsset("Y/child/nested.jpg", "Y/child", model.MediaTypeImage),
		testFolderAsset("Z/other.jpg", "Z", model.MediaTypeImage),
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

	if err := database.RefreshFolders(ctx); err != nil {
		t.Fatal(err)
	}
	fullTree, err := database.FolderTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !folderRelInTree(fullTree, "Y") || !folderRelInTree(fullTree, "Z") {
		t.Fatalf("FolderTree = %#v, want existing folders", fullTree)
	}

	emptyTree, err := database.FolderTreeWithRoots(ctx, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyTree) != 0 {
		t.Fatalf("empty roots tree = %#v, want empty", emptyTree)
	}
}

func TestRefreshFoldersHidesPendingAssetsUntilThumbReady(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := testFolderAsset("pending/deep/a.jpg", "pending/deep", model.MediaTypeImage)
	asset.ThumbStatus = model.StatusPending
	asset.PreviewStatus = model.StatusPending
	assetID, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.RefreshFolders(ctx); err != nil {
		t.Fatal(err)
	}

	tree, err := database.FolderTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if folderRelInTree(tree, "pending/deep") {
		t.Fatalf("FolderTree = %#v, want pending/deep hidden before thumb ready", tree)
	}
	folder, err := database.GetFolderByRel(ctx, "pending/deep")
	if err != nil {
		t.Fatal(err)
	}
	if folder.AssetCount != 0 || folder.RecursiveAssetCount != 0 {
		t.Fatalf("pending/deep counts = direct %d recursive %d, want 0 and 0", folder.AssetCount, folder.RecursiveAssetCount)
	}
	if folder.CoverAssetID != nil {
		t.Fatalf("pending/deep cover = %v, want nil before thumb ready", *folder.CoverAssetID)
	}
	if err := database.SetAssetWorkStatus(ctx, assetID, "thumb_status", model.StatusReady, nil); err != nil {
		t.Fatal(err)
	}
	tree, err = database.FolderTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder, ok := folderByRel(tree, "pending/deep")
	if !ok {
		t.Fatalf("FolderTree = %#v, want pending/deep after thumb ready", tree)
	}
	if folder.RecursiveAssetCount != 1 || folder.CoverAssetID == nil || *folder.CoverAssetID != assetID {
		t.Fatalf("pending/deep after ready = %#v, want one ready asset with cover", folder)
	}
}

func TestRefreshFoldersRelinksAssetsFromFileInstances(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asset := testFolderAsset("lost/deep/a.jpg", "lost/deep", model.MediaTypeImage)
	id, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.conn.ExecContext(ctx, `DELETE FROM folder WHERE rel_path <> ''`); err != nil {
		t.Fatal(err)
	}
	if err := database.RefreshFolders(ctx); err != nil {
		t.Fatal(err)
	}

	restored, err := database.GetAsset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ParentRelPath != "lost/deep" {
		t.Fatalf("parent rel path = %q, want lost/deep", restored.ParentRelPath)
	}
	tree, err := database.FolderTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !folderRelInTree(tree, "lost") || !folderRelInTree(tree, "lost/deep") {
		t.Fatalf("FolderTree = %#v, want rebuilt lost folders", tree)
	}
}

func testFolderAsset(relPath string, parent string, mediaType string) AssetUpsert {
	asset := testSearchAsset(relPath, mediaType)
	asset.ParentRelPath = parent
	return asset
}

func relPaths(assets []model.Asset) []string {
	result := make([]string, 0, len(assets))
	for _, asset := range assets {
		result = append(result, asset.RelPath)
	}
	return result
}

func folderRelInTree(folders []model.Folder, rel string) bool {
	for _, folder := range folders {
		if folder.RelPath == rel {
			return true
		}
	}
	return false
}

func folderByRel(folders []model.Folder, rel string) (model.Folder, bool) {
	for _, folder := range folders {
		if folder.RelPath == rel {
			return folder, true
		}
	}
	return model.Folder{}, false
}
