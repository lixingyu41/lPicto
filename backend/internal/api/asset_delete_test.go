package api

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
)

func TestBuildAssetDeletePlanDeletesSameStemFiles(t *testing.T) {
	server, root := testDeletePlanServer(t)
	writeTestFile(t, root, "a.mp4")
	writeTestFile(t, root, "a.nfo")
	writeTestFile(t, root, "a.zh.srt")
	writeTestFile(t, root, "a.jpg")
	writeTestFile(t, root, "b.mp4")

	plan, err := server.buildAssetDeletePlan(testDeleteAsset(1, "a.mp4", model.MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	if plan.mode != assetDeleteModeFiles {
		t.Fatalf("mode = %q, want files", plan.mode)
	}
	want := []string{"a.jpg", "a.mp4", "a.nfo", "a.zh.srt"}
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
