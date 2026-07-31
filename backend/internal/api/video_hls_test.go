package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lpicto/backend/internal/config"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
)

func TestPlaybackSegmentPreemptsBalancedAndKeepsPeerPlaybackRunning(t *testing.T) {
	warmupCtx, cancelWarmup := context.WithCancelCause(context.Background())
	oldViewerCtx, cancelOldViewer := context.WithCancelCause(context.Background())
	server := &Server{videoSegmentStates: map[string]*videoSegmentRuntime{
		"warmup": {CacheKey: "warmup", SessionID: "same", Priority: videoSegmentPriorityBalanced, Transcoding: true, Cancel: cancelWarmup},
		"old":    {CacheKey: "old", SessionID: "old", Priority: videoSegmentPriorityPlayback, Transcoding: true, Cancel: cancelOldViewer},
	}}
	server.preemptVideoSegmentsLocked("playing", videoSegmentPriorityPlayback)
	if !errors.Is(context.Cause(warmupCtx), errVideoSegmentPreempted) {
		t.Fatalf("warmup cause = %v", context.Cause(warmupCtx))
	}
	if context.Cause(oldViewerCtx) != nil {
		t.Fatalf("peer playback was cancelled: %v", context.Cause(oldViewerCtx))
	}
}

func TestVideoSegmentQueueUsesPriorityThenFIFO(t *testing.T) {
	server := &Server{videoSegmentStates: map[string]*videoSegmentRuntime{
		"tail":     {CacheKey: "tail", Priority: videoSegmentPriorityBalanced, QueueOrder: 2, Queued: true, Cancel: func(error) {}},
		"neighbor": {CacheKey: "neighbor", Priority: videoSegmentPriorityBalanced, QueueOrder: 1, Queued: true, Cancel: func(error) {}},
		"critical": {CacheKey: "critical", Priority: videoSegmentPriorityCritical, QueueOrder: 3, Queued: true, Cancel: func(error) {}},
	}}
	if got := server.videoSegmentQueueHeadLocked(); got == nil || got.CacheKey != "critical" {
		t.Fatalf("queue head = %#v, want critical", got)
	}
	server.videoSegmentStates["critical"].Queued = false
	if got := server.videoSegmentQueueHeadLocked(); got == nil || got.CacheKey != "neighbor" {
		t.Fatalf("balanced queue head = %#v, want oldest neighbor", got)
	}
	server.videoSegmentStates["playing-2"] = &videoSegmentRuntime{CacheKey: "playing-2", Priority: videoSegmentPriorityPlayback, QueueOrder: 5, Queued: true, Cancel: func(error) {}}
	server.videoSegmentStates["playing-1"] = &videoSegmentRuntime{CacheKey: "playing-1", Priority: videoSegmentPriorityPlayback, QueueOrder: 4, Queued: true, Cancel: func(error) {}}
	if got := server.videoSegmentQueueHeadLocked(); got == nil || got.CacheKey != "playing-1" {
		t.Fatalf("playback queue head = %#v, want oldest playback request", got)
	}
}

func TestStopVideoSegmentSessionCancelsOnlyMatchingTasksAndKeepsCache(t *testing.T) {
	matchingCtx, cancelMatching := context.WithCancelCause(context.Background())
	otherCtx, cancelOther := context.WithCancelCause(context.Background())
	server := &Server{videoSegmentStates: map[string]*videoSegmentRuntime{
		"matching": {AssetID: 7, SessionID: "viewer-a", Queued: true, Cancel: cancelMatching},
		"other":    {AssetID: 7, SessionID: "viewer-b", Queued: true, Cancel: cancelOther},
		"cached":   {AssetID: 7, SessionID: "viewer-a"},
	}}
	if got := server.stopVideoSegmentSession(7, "viewer-a"); got != 1 {
		t.Fatalf("cancelled = %d, want 1", got)
	}
	if !errors.Is(context.Cause(matchingCtx), errVideoSegmentSessionStop) {
		t.Fatalf("matching cause = %v", context.Cause(matchingCtx))
	}
	if context.Cause(otherCtx) != nil {
		t.Fatalf("other session was cancelled: %v", context.Cause(otherCtx))
	}
	if _, ok := server.videoSegmentStates["cached"]; ok {
		t.Fatal("completed runtime should be removed when session closes")
	}
}

func TestSharedVideoSegmentSurvivesUntilLastSessionStops(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	server := &Server{videoSegmentStates: map[string]*videoSegmentRuntime{
		"shared": {
			AssetID: 7, SessionID: "viewer-a", SessionIDs: map[string]struct{}{"viewer-a": {}, "viewer-b": {}},
			Queued: true, Cancel: cancel,
		},
	}}
	if got := server.stopVideoSegmentSession(7, "viewer-a"); got != 0 {
		t.Fatalf("first stop cancelled %d tasks, want 0", got)
	}
	if context.Cause(ctx) != nil {
		t.Fatalf("shared task was cancelled while another viewer still needed it: %v", context.Cause(ctx))
	}
	if got := server.stopVideoSegmentSession(7, "viewer-b"); got != 1 {
		t.Fatalf("last stop cancelled %d tasks, want 1", got)
	}
	if !errors.Is(context.Cause(ctx), errVideoSegmentSessionStop) {
		t.Fatalf("last stop cause = %v", context.Cause(ctx))
	}
}

func TestVideoSegmentPriorityHeaderOverridesPlaylistQuery(t *testing.T) {
	request := httptest.NewRequest("GET", "/segment.ts?priority=preload", nil)
	request.Header.Set(videoSegmentPriorityHeader, "playback")
	if got := videoSegmentPriorityFromRequest(request, videoSegmentPriorityBalanced); got != videoSegmentPriorityPlayback {
		t.Fatalf("priority = %d, want playback", got)
	}
}

func TestVideoSegmentQueryPreservesPreloadPriority(t *testing.T) {
	query := videoSegmentQuery(model.Asset{CacheKey: "asset"}, VideoProxyHeartbeatRequest{
		ClientID: "browser", SessionID: "viewer",
	}, "preload")
	if !strings.Contains(query, "priority=preload") {
		t.Fatalf("query = %q, want preload priority", query)
	}
}

func TestRemoveVideoSegmentCachesStopsRuntimeAndDeletesOnlyAssetSegments(t *testing.T) {
	cacheRoot := t.TempDir()
	assetCacheKey := "abcdef0123456789"
	matchingKey := assetCacheKey + "-hls-matching"
	otherKey := "abcdef9999999999-hls-other"
	store := storage.Store{CacheRoot: cacheRoot}
	for _, key := range []string{matchingKey, otherKey} {
		path, err := store.CachePath("video-proxies", key, "ts")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("segment"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(done)
	}()
	server := &Server{
		store: store,
		videoSegmentStates: map[string]*videoSegmentRuntime{
			matchingKey: {CacheKey: matchingKey, Queued: true, Cancel: cancel, Done: done},
			otherKey:    {CacheKey: otherKey},
		},
	}
	if err := server.removeVideoSegmentCaches(assetCacheKey); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), errVideoSegmentSuperseded) {
		t.Fatalf("cancel cause = %v", context.Cause(ctx))
	}
	matchingPath, _ := store.CacheFilePath("video-proxies", matchingKey, "ts")
	if _, err := os.Stat(matchingPath); !os.IsNotExist(err) {
		t.Fatalf("matching segment still exists: %s", matchingPath)
	}
	otherPath, _ := store.CacheFilePath("video-proxies", otherKey, "ts")
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("unrelated segment was removed: %v", err)
	}
	if _, ok := server.videoSegmentStates[matchingKey]; ok {
		t.Fatal("matching runtime state still exists")
	}
	if _, ok := server.videoSegmentStates[otherKey]; !ok {
		t.Fatal("unrelated runtime state was removed")
	}
}

func TestVideoSegmentStatusPrefersPlaybackOverQueuedWarmup(t *testing.T) {
	duration := 60.0
	server := &Server{
		cfg:   config.Config{VideoSegmentSeconds: 10, VideoProxyCRF: 23},
		store: storage.Store{CacheRoot: t.TempDir()},
		videoSegmentStates: map[string]*videoSegmentRuntime{
			"warmup":  {AssetID: 7, SessionID: "viewer", SegmentIndex: 4, Priority: videoSegmentPriorityBalanced, Queued: true, UpdatedAt: time.Now()},
			"playing": {AssetID: 7, SessionID: "viewer", SegmentIndex: 1, Priority: videoSegmentPriorityPlayback, Transcoding: true, UpdatedAt: time.Now().Add(-time.Second)},
		},
	}
	dto := server.videoSegmentStatus(model.Asset{ID: 7, CacheKey: "asset", Duration: &duration}, "viewer")
	if dto.SegmentIndex != 1 || !dto.Transcoding || dto.Queued {
		t.Fatalf("status selected warmup instead of playback: %#v", dto)
	}
}

func TestVideoSegmentCountAndTailDuration(t *testing.T) {
	if got := videoSegmentCount(25.2, 10); got != 3 {
		t.Fatalf("segment count = %d, want 3", got)
	}
	if got := videoSegmentDuration(25.2, 10, 2); got < 5.19 || got > 5.21 {
		t.Fatalf("tail duration = %.3f, want about 5.2", got)
	}
}

func TestParseVideoSegmentIndex(t *testing.T) {
	got, err := parseVideoSegmentIndex("48.ts")
	if err != nil {
		t.Fatalf("parse segment failed: %v", err)
	}
	if got != 48 {
		t.Fatalf("segment = %d, want 48", got)
	}
	if _, err := parseVideoSegmentIndex("-1.ts"); err == nil {
		t.Fatal("negative segment should fail")
	}
}

func TestVideoSegmentCacheKeyIncludesSegmentIndex(t *testing.T) {
	first := videoSegmentCacheKey("assetcache", 10, 0, 23, "vaapi", 1)
	second := videoSegmentCacheKey("assetcache", 10, 0, 23, "vaapi", 2)
	if first == second {
		t.Fatalf("cache keys should differ by segment index: %q", first)
	}
}

func TestParseVideoSegmentProbeAndValidate(t *testing.T) {
	duration, packets := parseVideoSegmentProbe("nb_read_packets=300\nduration=10.031033\n")
	if duration != 10.031033 || packets != 300 {
		t.Fatalf("probe = duration %.6f, packets %d", duration, packets)
	}
	if !videoSegmentOutputValid(duration, packets, 10) {
		t.Fatal("full segment should be valid")
	}
	if videoSegmentOutputValid(0.023222, 0, 10) {
		t.Fatal("empty segment should be invalid")
	}
}

func TestVideoSegmentEditListFallbackExtensions(t *testing.T) {
	for _, path := range []string{"video.mp4", "video.MOV", "video.m4v"} {
		if !videoSegmentSupportsEditListFallback(path) {
			t.Fatalf("%q should support edit-list fallback", path)
		}
	}
	if videoSegmentSupportsEditListFallback("video.mkv") {
		t.Fatal("mkv should not use edit-list fallback")
	}
}

func TestVideoSegmentCacheSummary(t *testing.T) {
	cacheRoot := t.TempDir()
	server := &Server{
		cfg:   config.Config{VideoSegmentSeconds: 10, VideoProxyCRF: 23, FFmpegHWAccel: "vaapi"},
		store: storage.Store{CacheRoot: cacheRoot},
	}
	duration := 25.0
	asset := model.Asset{CacheKey: "abcdef0123456789", Duration: &duration, Size: 1000}
	for index, size := range []int{12, 18} {
		key := videoSegmentCacheKey(asset.CacheKey, 10, 0, 23, "vaapi", index)
		path, err := server.store.CachePath("video-proxies", key, "ts")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staleKey := videoSegmentCacheKey(asset.CacheKey, 10, 0, 24, "vaapi", 0)
	stalePath, err := server.store.CachePath("video-proxies", staleKey, "ts")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := server.videoSegmentCacheSummary(asset)
	if summary.CachedBytes != 30 || summary.CachedSegments != 2 || summary.SegmentCount != 3 || summary.EstimatedTotalBytes != 45 {
		t.Fatalf("summary = %#v", summary)
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "video-proxies")); err != nil {
		t.Fatal(err)
	}
}
