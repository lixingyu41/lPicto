package db

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestAIResultSearchTagsVersioningAndManualIsolation(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	asset := testSearchAsset("park.jpg", model.MediaTypeImage)
	id, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetAsset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAIQueued(ctx, id, stored.CacheKey, false); err != nil {
		t.Fatal(err)
	}
	if _, err := database.MarkAIProcessing(ctx, id, stored.CacheKey); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveAIResult(ctx, id, stored.CacheKey, "一名游客在绿色公园的步道旁行走，周围可见树木和草地。", "clip", "v1", "qwen", "v1", "tax-v1", json.RawMessage(`[{"ratio":0}]`), []AITag{{Tag: "公园", Confidence: 0.41}}, nil); err != nil {
		t.Fatal(err)
	}
	page, err := database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, AIDescription: "绿色公园", VisibleOnly: true})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("AI description search = %#v, %v", page.Items, err)
	}
	page, err = database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, AIDescription: "绿色公圆", VisibleOnly: true})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("fuzzy AI description search = %#v, %v", page.Items, err)
	}
	page, err = database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, AITag: "公园", VisibleOnly: true})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("AI tag search = %#v, %v", page.Items, err)
	}
	page, err = database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, CombinedQuery: "绿色公圆", VisibleOnly: true})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("combined filename and AI description search = %#v, %v", page.Items, err)
	}
	if _, err := database.AddAssetTag(ctx, id, "收藏"); err != nil {
		t.Fatal(err)
	}
	page, err = database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, CombinedTag: "收藏", VisibleOnly: true})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("combined manual tag search = %#v, %v", page.Items, err)
	}
	page, err = database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, CombinedTag: "公园", VisibleOnly: true})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("combined AI tag search = %#v, %v", page.Items, err)
	}
	second := testSearchAsset("manual-only.jpg", model.MediaTypeImage)
	secondID, _, _, err := database.UpsertAsset(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.AddAssetTag(ctx, secondID, "旅行"); err != nil {
		t.Fatal(err)
	}
	page, err = database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 10, CombinedTags: []string{"公园", "旅行"}, VisibleOnly: true})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("multi-select combined tags = %#v, %v", page.Items, err)
	}
	summaries, err := database.AITags(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var aiSummary, manualSummary *AITagSummary
	for i := range summaries {
		switch summaries[i].Tag {
		case "公园":
			aiSummary = &summaries[i]
		case "收藏":
			manualSummary = &summaries[i]
		}
	}
	if aiSummary == nil || aiSummary.AICount != 1 || aiSummary.ManualAdded {
		t.Fatalf("AI summary = %#v", aiSummary)
	}
	if manualSummary == nil || manualSummary.ManualCount != 1 || !manualSummary.ManualAdded || manualSummary.ManualTagID == nil {
		t.Fatalf("manual summary = %#v", manualSummary)
	}
	manual, err := database.ListAssetTags(ctx, id)
	if err != nil || len(manual) != 1 || manual[0].Name != "收藏" {
		t.Fatalf("manual tags changed by AI: %#v, %v", manual, err)
	}
	if err := database.DeleteTag(ctx, *manualSummary.ManualTagID); err != nil {
		t.Fatal(err)
	}
	manual, err = database.ListAssetTags(ctx, id)
	if err != nil || len(manual) != 0 {
		t.Fatalf("deleted manual tag still attached: %#v, %v", manual, err)
	}
	if err := database.EnsureAIQueued(ctx, id, "changed-cache-key", false); err != nil {
		t.Fatal(err)
	}
	result, err := database.GetAIResult(ctx, id)
	if err != nil || result.Status != "pending" || result.InputCacheKey != "changed-cache-key" {
		t.Fatalf("version invalidation = %#v, %v", result, err)
	}
}

func TestAIBackfillPilotStopsQueueingCompletedMediaType(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	image := testSearchAsset("pilot-image.jpg", model.MediaTypeImage)
	imageID, _, _, err := database.UpsertAsset(ctx, image)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		video := testSearchAsset(fmt.Sprintf("pilot-video-%02d.mp4", i), model.MediaTypeVideo)
		videoID, _, _, err := database.UpsertAsset(ctx, video)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := database.GetAsset(ctx, videoID)
		if err != nil {
			t.Fatal(err)
		}
		if err := database.EnsureAIQueued(ctx, videoID, stored.CacheKey, false); err != nil {
			t.Fatal(err)
		}
		if err := database.SaveAIResult(ctx, videoID, stored.CacheKey, "视频画面展示一项用于本地试运行的普通媒体内容。", "clip", "v1", "qwen", "v1", "tax-v1", json.RawMessage(`[{"ratio":0.5}]`), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	items, err := database.AIBackfillBatch(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AssetID != imageID || items[0].RelPath != image.RelPath {
		t.Fatalf("pilot backfill should only queue missing images, got %#v", items)
	}
}

func TestAIBackfillContinuesAfterVideoOnlyPilot(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var pendingID int64
	for i := 0; i < 21; i++ {
		video := testSearchAsset(fmt.Sprintf("video-only-%02d.mp4", i), model.MediaTypeVideo)
		videoID, _, _, err := database.UpsertAsset(ctx, video)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := database.GetAsset(ctx, videoID)
		if err != nil {
			t.Fatal(err)
		}
		if i == 20 {
			pendingID = videoID
			continue
		}
		if err := database.EnsureAIQueued(ctx, videoID, stored.CacheKey, false); err != nil {
			t.Fatal(err)
		}
		if err := database.SaveAIResult(ctx, videoID, stored.CacheKey, "视频画面展示一项用于本地试运行的普通媒体内容。", "clip", "v1", "qwen", "v1", "tax-v1", json.RawMessage(`[{"ratio":0.5}]`), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	items, err := database.AIBackfillBatch(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AssetID != pendingID {
		t.Fatalf("video-only backfill stopped after pilot: %#v", items)
	}
}

func TestAIFailureStopsRetryingAtAutomaticLimit(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	asset := testSearchAsset("retry.jpg", model.MediaTypeImage)
	assetID, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetAsset(ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAIQueued(ctx, assetID, stored.CacheKey, false); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= AIMaxAttempts; attempt++ {
		gotAttempt, err := database.MarkAIProcessing(ctx, assetID, stored.CacheKey)
		if err != nil || gotAttempt != attempt {
			t.Fatalf("attempt %d = %d, %v", attempt, gotAttempt, err)
		}
		retry, err := database.MarkAIFailed(ctx, assetID, stored.CacheKey, "damaged media")
		if err != nil {
			t.Fatal(err)
		}
		if retry != (attempt < AIMaxAttempts) {
			t.Fatalf("retry after attempt %d = %v", attempt, retry)
		}
	}
	items, err := database.RetryFailedAI(ctx)
	if err != nil || len(items) != 1 || items[0].AssetID != assetID {
		t.Fatalf("retry failed AI items = %#v, %v", items, err)
	}
	result, err := database.GetAIResult(ctx, assetID)
	if err != nil || result.Status != "pending" || result.Attempts != 0 || result.Error != "" {
		t.Fatalf("reset AI result = %#v, %v", result, err)
	}
}

func TestAIPlaybackInterruptionReturnsTaskToPendingWithoutUsingAttempt(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	asset := testSearchAsset("interrupted.jpg", model.MediaTypeImage)
	assetID, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetAsset(ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAIQueued(ctx, assetID, stored.CacheKey, false); err != nil {
		t.Fatal(err)
	}
	if attempt, err := database.MarkAIProcessing(ctx, assetID, stored.CacheKey); err != nil || attempt != 1 {
		t.Fatalf("processing attempt = %d, %v", attempt, err)
	}
	if err := database.RequeueAIInterrupted(ctx, assetID, stored.CacheKey); err != nil {
		t.Fatal(err)
	}
	result, err := database.GetAIResult(ctx, assetID)
	if err != nil || result.Status != "pending" || result.Attempts != 0 || result.Error != "" {
		t.Fatalf("interrupted AI result = %#v, %v", result, err)
	}
}

func TestAISettingsSwitchBetweenAutomaticAndManualRuns(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	settings, err := database.GetAISettings(ctx)
	if err != nil || !settings.AutoAnalyze || settings.ManualRun {
		t.Fatalf("default settings = %#v, %v", settings, err)
	}
	settings, err = database.SetAIAutoAnalyze(ctx, false)
	if err != nil || settings.AutoAnalyze || settings.ManualRun {
		t.Fatalf("disabled settings = %#v, %v", settings, err)
	}
	settings, err = database.SetAIManualRun(ctx, true)
	if err != nil || settings.AutoAnalyze || !settings.ManualRun {
		t.Fatalf("manual settings = %#v, %v", settings, err)
	}
	enabled, err := database.AIExecutionEnabled(ctx)
	if err != nil || !enabled {
		t.Fatalf("manual execution enabled = %v, %v", enabled, err)
	}
}
