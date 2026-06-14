package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRelPathSafety(t *testing.T) {
	got, err := NormalizeRelPath(`2024\IMG_001.jpg`)
	if err != nil {
		t.Fatalf("NormalizeRelPath returned error: %v", err)
	}
	if got != "2024/IMG_001.jpg" {
		t.Fatalf("rel path = %q", got)
	}
	if _, err := NormalizeRelPath("../secret.jpg"); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestPhotoPathStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	store, err := New(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PhotoPath("../../secret.jpg"); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestSymlinkEscapeDetection(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.jpg")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.jpg")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	inside, _, err := SymlinkTargetWithinRoot(root, link)
	if err != nil {
		t.Fatal(err)
	}
	if inside {
		t.Fatal("expected symlink target outside root")
	}
}

func TestCacheKeyChangesWithMtime(t *testing.T) {
	first := CacheKey("a/b.jpg", 10, 100)
	second := CacheKey("a/b.jpg", 10, 101)
	if first == second {
		t.Fatal("cache key did not change")
	}
}

func TestFolderParentPath(t *testing.T) {
	if got := ParentRelPath("2024/05/IMG.jpg"); got != "2024/05" {
		t.Fatalf("parent = %q", got)
	}
	if got := ParentRelPath("IMG.jpg"); got != "" {
		t.Fatalf("root parent = %q", got)
	}
}
