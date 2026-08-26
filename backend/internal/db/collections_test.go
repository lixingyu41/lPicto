package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"lpicto/backend/internal/model"
)

func TestMissingCollectionExcludesDeletedAssets(t *testing.T) {
	database := &DB{}
	source, where, _, ok := database.systemCollectionFilter(SystemCollectionMissing, AssetListOptions{})
	if !ok {
		t.Fatal("missing collection filter should exist")
	}
	if source != "asset_records" {
		t.Fatalf("source = %q, want asset_records", source)
	}
	for _, condition := range []string{"missing = true", "is_live = true"} {
		if !strings.Contains(where, condition) {
			t.Fatalf("where = %q, missing %q", where, condition)
		}
	}
	if strings.Contains(where, "OR deleted_at") {
		t.Fatalf("where = %q, deleted assets must not re-enter missing collection", where)
	}
}

func TestAllCollectionUsesBaseLiveAssetFilter(t *testing.T) {
	database := &DB{}
	source, where, _, ok := database.systemCollectionFilter(SystemCollectionAll, AssetListOptions{})
	if !ok {
		t.Fatal("all collection filter should exist")
	}
	if source != "assets" {
		t.Fatalf("source = %q, want assets", source)
	}
	for _, condition := range []string{"is_live = true", "hidden = false"} {
		if !strings.Contains(where, condition) {
			t.Fatalf("where = %q, missing %q", where, condition)
		}
	}
}

func TestAIReadyCollectionRequiresCurrentCompletedResult(t *testing.T) {
	database := &DB{}
	source, where, _, ok := database.systemCollectionFilter(SystemCollectionAIReady, AssetListOptions{})
	if !ok {
		t.Fatal("AI ready collection filter should exist")
	}
	if source != "assets" {
		t.Fatalf("source = %q, want assets", source)
	}
	for _, condition := range []string{"air.status='ready'", "air.input_cache_key=assets.cache_key"} {
		if !strings.Contains(where, condition) {
			t.Fatalf("where = %q, missing %q", where, condition)
		}
	}
}

func TestStoryboardReadyCollectionRequiresCompletedVideoStoryboard(t *testing.T) {
	database := &DB{}
	source, where, _, ok := database.systemCollectionFilter(SystemCollectionStoryboardReady, AssetListOptions{})
	if !ok {
		t.Fatal("storyboard ready collection filter should exist")
	}
	if source != "assets" {
		t.Fatalf("source = %q, want assets", source)
	}
	for _, condition := range []string{"media_type='video'", "mj.job_type='storyboard'", "mj.status='ready'"} {
		if !strings.Contains(where, condition) {
			t.Fatalf("where = %q, missing %q", where, condition)
		}
	}
}

func TestDuplicateCollectionKeepsHashGroupsContiguous(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, testDatabaseURL(t, ctx), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	tests := []struct {
		relPath    string
		timelineAt int64
		size       int64
		hash       string
	}{
		{relPath: "a-new.jpg", timelineAt: 100, size: 100, hash: strings.Repeat("a", 64)},
		{relPath: "a-old.jpg", timelineAt: 10, size: 100, hash: strings.Repeat("a", 64)},
		{relPath: "b-new.jpg", timelineAt: 90, size: 200, hash: strings.Repeat("b", 64)},
		{relPath: "b-old.jpg", timelineAt: 80, size: 200, hash: strings.Repeat("b", 64)},
	}
	ids := make([]int64, 0, len(tests))
	for _, item := range tests {
		asset := testSearchAsset(item.relPath, model.MediaTypeImage)
		asset.Size = item.size
		asset.TimelineAt = item.timelineAt
		id, _, _, err := database.UpsertAsset(ctx, asset)
		if err != nil {
			t.Fatal(err)
		}
		if err := database.SetAssetSHA256Hex(ctx, id, item.hash); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	page, err := database.ListSystemCollectionAssets(ctx, SystemCollectionDuplicates, AssetListOptions{
		Page: 1, PageSize: 10, Sort: "timeline_desc", Group: "folder",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(page.Items))
	for _, asset := range page.Items {
		got = append(got, asset.RelPath)
	}
	want := []string{"a-new.jpg", "a-old.jpg", "b-new.jpg", "b-old.jpg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("duplicate order = %v, want %v", got, want)
	}

	neighbors, err := database.SystemCollectionNeighbors(ctx, SystemCollectionDuplicates, ids[1], AssetListOptions{Sort: "timeline_desc", Group: "folder"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors.Previous) != 1 || neighbors.Previous[0].ID != ids[0] || len(neighbors.Next) != 2 || neighbors.Next[0].ID != ids[2] || neighbors.Next[1].ID != ids[3] {
		t.Fatalf("duplicate neighbors previous=%v next=%v", assetRelPaths(neighbors.Previous), assetRelPaths(neighbors.Next))
	}
	position, err := database.SystemCollectionAssetPosition(ctx, SystemCollectionDuplicates, ids[1], AssetListOptions{PageSize: 2, Sort: "timeline_desc", Group: "folder"})
	if err != nil {
		t.Fatal(err)
	}
	if position.Index != 1 || position.Page != 1 || position.Total != 4 {
		t.Fatalf("duplicate position = %+v, want index 1 page 1 total 4", position)
	}

	candidates, err := database.DuplicateDeleteCandidateIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantCandidates := []int64{ids[1], ids[3]}
	if len(candidates) != len(wantCandidates) {
		t.Fatalf("duplicate candidates = %v, want %v", candidates, wantCandidates)
	}
	for i := range candidates {
		if candidates[i] != wantCandidates[i] {
			t.Fatalf("duplicate candidates = %v, want %v", candidates, wantCandidates)
		}
	}

	counts, err := database.RefreshSystemCollectionCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[SystemCollectionDuplicates] != 4 {
		t.Fatalf("duplicate count = %d, want 4", counts[SystemCollectionDuplicates])
	}
	cached, ok, err := database.GetSystemCollectionCounts(ctx)
	if err != nil || !ok {
		t.Fatalf("cached counts ok = %v, err = %v", ok, err)
	}
	if cached[SystemCollectionDuplicates] != 4 {
		t.Fatalf("cached duplicate count = %d, want 4", cached[SystemCollectionDuplicates])
	}
}

func TestDuplicateCollectionUsesPersistedHashesAndDropsPurgedRecords(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, testDatabaseURL(t, ctx), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ids := make([]int64, 0, 2)
	for _, relPath := range []string{"copy-a.jpg", "copy-b.jpg"} {
		asset := testSearchAsset(relPath, model.MediaTypeImage)
		asset.Size = 12345
		id, _, _, err := database.UpsertAsset(ctx, asset)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	page, err := database.ListSystemCollectionAssets(ctx, SystemCollectionDuplicates, AssetListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("unhashed duplicate items = %d, want 0", len(page.Items))
	}

	hash := strings.Repeat("c", 64)
	for _, id := range ids {
		if err := database.SetAssetSHA256Hex(ctx, id, hash); err != nil {
			t.Fatal(err)
		}
	}
	page, err = database.ListSystemCollectionAssets(ctx, SystemCollectionDuplicates, AssetListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("persisted duplicate items = %d, want 2", len(page.Items))
	}
	marked, err := database.MarkDeletedAssetIDs(ctx, []int64{ids[1]}, 123456)
	if err != nil {
		t.Fatal(err)
	}
	if len(marked) != 1 || marked[0].ID != ids[1] {
		t.Fatalf("marked deleted assets = %#v, want id %d", marked, ids[1])
	}
	var storedHash []byte
	if err := database.conn.QueryRowContext(ctx, `SELECT sha256 FROM media_asset WHERE id = ?`, ids[1]).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if len(storedHash) != 0 {
		t.Fatalf("deleted asset hash length = %d, want 0", len(storedHash))
	}
	page, err = database.ListSystemCollectionAssets(ctx, SystemCollectionDuplicates, AssetListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("duplicate items after file deletion = %d, want 0", len(page.Items))
	}

	deleted, err := database.PurgeAssetIDs(ctx, []int64{ids[1]})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].ID != ids[1] {
		t.Fatalf("purged assets = %#v, want id %d", deleted, ids[1])
	}
	page, err = database.ListSystemCollectionAssets(ctx, SystemCollectionDuplicates, AssetListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("duplicate items after purge = %d, want 0", len(page.Items))
	}
}
