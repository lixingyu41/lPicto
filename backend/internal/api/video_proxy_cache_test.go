package api

import (
	"testing"
	"time"
)

func TestVideoProxyCacheDeletePlanUsesTTLAndCapacity(t *testing.T) {
	now := time.Unix(2000, 0)
	entries := []videoProxyCacheEntry{
		{Path: "old.mp4", CacheKey: "old", Size: 60, ModTime: now.Add(-2 * time.Hour)},
		{Path: "recent.mp4", CacheKey: "recent", Size: 60, ModTime: now.Add(-10 * time.Minute)},
		{Path: "active.mp4", CacheKey: "active", Size: 60, ModTime: now.Add(-3 * time.Hour), Active: true},
	}
	plan := videoProxyCacheDeletePlan(entries, now, videoProxyCacheSettings{TTL: time.Hour, MaxBytes: 100})
	if len(plan) != 2 {
		t.Fatalf("delete plan len = %d, want 2: %#v", len(plan), plan)
	}
	if plan[0].Path != "old.mp4" || plan[1].Path != "recent.mp4" {
		t.Fatalf("delete plan = %#v, want old then recent", plan)
	}
}
