package api

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
)

func TestBuildAssetDeletePlanDeletesSameStemFiles(t *testing.T) {
	server, root := testDeletePlanServer(t)
	writeTestFile(t, root, "a.mp4")
	writeTestFile(t, root, "a.nfo")
	writeTestFile(t, root, "a.zh.srt")
	writeTestFile(t, root, "a.xml")
	writeTestFile(t, root, "a.jpg")
	writeTestFile(t, root, "b.mp4")

	plan, err := server.buildAssetDeletePlan(testDeleteAsset(1, "a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	if plan.mode != assetDeleteModeFiles {
		t.Fatalf("mode = %q, want files", plan.mode)
	}
	want := []string{"a.jpg", "a.mp4", "a.nfo", "a.xml", "a.zh.srt"}
	if got := deleteRelPaths(plan.files); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
}

func TestBuildAssetDeletePlanDeletesSingleMediaFolder(t *testing.T) {
	server, root := testDeletePlanServer(t)
	writeTestFile(t, root, "dir/a.mp4")
	writeTestFile(t, root, "dir/a.nfo")
	writeTestFile(t, root, "dir/readme.txt")

	plan, err := server.buildAssetDeletePlan(testDeleteAsset(1, "dir/a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	if plan.mode != assetDeleteModeFolder {
		t.Fatalf("mode = %q, want folder", plan.mode)
	}
	if plan.folder == nil || plan.folder.relPath != "dir" {
		t.Fatalf("folder = %#v, want dir", plan.folder)
	}
	want := []string{"dir/a.mp4", "dir/a.nfo", "dir/readme.txt"}
	if got := deleteRelPaths(plan.folderContents); !reflect.DeepEqual(got, want) {
		t.Fatalf("contents = %#v, want %#v", got, want)
	}
}

func TestBuildAssetDeletePlanDoesNotDeleteRootFolder(t *testing.T) {
	server, root := testDeletePlanServer(t)
	writeTestFile(t, root, "a.mp4")

	plan, err := server.buildAssetDeletePlan(testDeleteAsset(1, "a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	if plan.mode != assetDeleteModeFiles {
		t.Fatalf("mode = %q, want files", plan.mode)
	}
	if len(plan.warnings) == 0 {
		t.Fatalf("warnings empty, want root warning")
	}
}

func TestBuildAssetDeletePlanDowngradesNestedMediaFolder(t *testing.T) {
	server, root := testDeletePlanServer(t)
	writeTestFile(t, root, "dir/a.mp4")
	writeTestFile(t, root, "dir/sub/b.jpg")

	plan, err := server.buildAssetDeletePlan(testDeleteAsset(1, "dir/a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	if plan.mode != assetDeleteModeFiles {
		t.Fatalf("mode = %q, want files", plan.mode)
	}
	want := []string{"dir/a.mp4"}
	if got := deleteRelPaths(plan.files); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
}

func TestAssetDeleteTokenChangesWhenPlanChanges(t *testing.T) {
	server, root := testDeletePlanServer(t)
	writeTestFile(t, root, "dir/a.mp4")

	first, err := server.buildAssetDeletePlan(testDeleteAsset(1, "dir/a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "dir/a.nfo")
	second, err := server.buildAssetDeletePlan(testDeleteAsset(1, "dir/a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	if first.token == second.token {
		t.Fatalf("token did not change after adding sidecar")
	}
}

func testDeletePlanServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.New(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{store: store}, root
}

func writeTestFile(t *testing.T, root string, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testDeleteAsset(id int64, rel string, mediaType string) model.Asset {
	return model.Asset{
		ID:            id,
		RelPath:       rel,
		ParentRelPath: storage.ParentRelPath(rel),
		Filename:      filepath.Base(rel),
		MediaType:     mediaType,
	}
}

func deleteRelPaths(items []assetDeleteEntry) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if item.kind == "folder" {
			continue
		}
		paths = append(paths, item.relPath)
	}
	sort.Strings(paths)
	return paths
}

func TestPurgeAssetRecordsDeletesRelatedDataAndCache(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	assetID, _, _, err := database.UpsertAsset(ctx, db.AssetUpsert{
		RelPath: "record.jpg", ParentRelPath: "", Filename: "record.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "record-cache",
		ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().ExecContext(ctx, `
INSERT INTO asset_ai_result(asset_id,input_cache_key,status,description)
VALUES($1,$2,'failed','old')`, assetID, "record-cache"); err != nil {
		t.Fatal(err)
	}

	store, err := storage.New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thumbPath, err := store.CachePath("thumbs", "record-cache", "webp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumbPath, []byte("thumb"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		db:                 database,
		store:              store,
		videoProxyStates:   map[string]*videoProxyRuntime{},
		videoSegmentStates: map[string]*videoSegmentRuntime{},
	}
	result, err := server.purgeAssetRecords(ctx, []int64{assetID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DeletedAssetIDs) != 1 || result.DeletedAssetIDs[0] != assetID {
		t.Fatalf("deleted asset ids = %#v", result.DeletedAssetIDs)
	}
	if _, err := os.Stat(thumbPath); !os.IsNotExist(err) {
		t.Fatalf("thumbnail still exists: %v", err)
	}
	var count int
	if err := database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_ai_result WHERE asset_id=$1`, assetID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("AI result count = %d, want 0", count)
	}
	if _, err := database.GetAssetIncludingDeleted(ctx, assetID); err != sql.ErrNoRows {
		t.Fatalf("asset lookup error = %v, want sql.ErrNoRows", err)
	}
}
