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
