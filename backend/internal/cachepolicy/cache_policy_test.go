package cachepolicy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureCapacityEvictsTransientBeforeProtectedMedia(t *testing.T) {
	root := t.TempDir()
	manager := New(root, nil)
	manager.maxBytes = 12
	manager.minFree = 0
	write := func(kind, name, value string) string {
		t.Helper()
		path := filepath.Join(root, kind, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	thumb := write("thumbs", "thumb.webp", "12345678")
	transient := write("video-chunks", "chunk.bin", "abcdefgh")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(transient, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsureCapacity(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Fatalf("protected thumbnail was removed: %v", err)
	}
	if _, err := os.Stat(transient); !os.IsNotExist(err) {
		t.Fatalf("transient video chunk still exists: %v", err)
	}
}

func TestPinnedEntrySurvivesCapacitySweep(t *testing.T) {
	root := t.TempDir()
	manager := New(root, nil)
	manager.maxBytes = 1
	manager.minFree = 0
	path := filepath.Join(root, "video-chunks", "chunk.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("12345678"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := manager.Pin(path)
	defer release()
	if _, err := manager.EnsureCapacity(context.Background(), 0); err == nil {
		t.Fatal("capacity sweep unexpectedly succeeded with only a pinned entry")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pinned entry was removed: %v", err)
	}
}

func TestClearPreservesCompletedThumbnailsPostersAndStoryboards(t *testing.T) {
	root := t.TempDir()
	manager := New(root, nil)
	write := func(kind, name string) string {
		t.Helper()
		path := filepath.Join(root, kind, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("cache"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	protected := []string{
		write("thumbs", "asset.webp"),
		write("video-posters", "asset.jpg"),
		write("storyboards", "asset-000.webp"),
	}
	reclaimable := []string{
		write("video-chunks", "asset.bin"),
		write("video-proxies", "asset.ts"),
		write("audio-chunks", "asset.bin"),
		write("audio-proxies", "asset.flac"),
		write("originals", "asset.jpg"),
		write("previews", "asset.webp"),
	}
	result, err := manager.Clear(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFiles != len(reclaimable) {
		t.Fatalf("deleted files = %d, want %d", result.DeletedFiles, len(reclaimable))
	}
	for _, path := range protected {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected cache was removed: %s: %v", path, err)
		}
	}
	for _, path := range reclaimable {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("reclaimable cache still exists: %s: %v", path, err)
		}
	}
}

func TestCapacitySweepUsesLeastRecentlyUsedOrder(t *testing.T) {
	root := t.TempDir()
	manager := New(root, nil)
	manager.maxBytes = 8
	manager.minFree = 0
	write := func(name string, accessed time.Time) string {
		t.Helper()
		path := filepath.Join(root, "video-chunks", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("12345678"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, accessed, accessed); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oldest := write("old.bin", time.Now().Add(-2*time.Hour))
	newest := write("new.bin", time.Now().Add(-time.Hour))
	if _, err := manager.EnsureCapacity(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatalf("oldest cache still exists: %v", err)
	}
	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("newest cache was removed: %v", err)
	}
}

func TestPinsAreSharedByManagersForSameRoot(t *testing.T) {
	root := t.TempDir()
	first := New(root, nil)
	second := New(root, nil)
	second.maxBytes = 1
	second.minFree = 0
	path := filepath.Join(root, "video-proxies", "active.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("12345678"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := first.Pin(path)
	defer release()
	if _, err := second.EnsureCapacity(context.Background(), 0); err == nil {
		t.Fatal("capacity sweep unexpectedly removed a cache pinned by another manager")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shared pinned cache was removed: %v", err)
	}
}
