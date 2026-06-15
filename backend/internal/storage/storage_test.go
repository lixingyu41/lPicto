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

func TestNamedRootPathMapping(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	store, err := NewWithRoots([]RootConfig{
		{ID: "C666", Path: first},
		{ID: "D666", Path: second},
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	full, err := store.PhotoPath("D666/2024/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(second, "2024", "a.jpg")
	if full != want {
		t.Fatalf("path = %q, want %q", full, want)
	}
	rel, err := store.RelPath(full)
	if err != nil {
		t.Fatal(err)
	}
	if rel != "D666/2024/a.jpg" {
		t.Fatalf("rel = %q", rel)
	}
	if _, err := store.PhotoPath(""); err == nil {
		t.Fatal("expected virtual root to have no direct filesystem path")
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

func TestRemoveCacheDeletesVariants(t *testing.T) {
	store, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cacheKey := "abcdef1234567890"
	for _, item := range []struct {
		kind string
		ext  string
	}{
		{kind: "thumbs", ext: "webp"},
		{kind: "previews", ext: "webp"},
		{kind: "video-posters", ext: "jpg"},
		{kind: "video-proxies", ext: "mp4"},
	} {
		path, err := store.CachePath(item.kind, cacheKey, item.ext)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("cache"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".tmp."+item.ext, []byte("tmp"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RemoveCache(cacheKey); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		kind string
		ext  string
	}{
		{kind: "thumbs", ext: "webp"},
		{kind: "previews", ext: "webp"},
		{kind: "video-posters", ext: "jpg"},
		{kind: "video-proxies", ext: "mp4"},
	} {
		path, err := store.CachePath(item.kind, cacheKey, item.ext)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cache path still exists: %s", path)
		}
		if _, err := os.Stat(path + ".tmp." + item.ext); !os.IsNotExist(err) {
			t.Fatalf("tmp cache path still exists: %s", path)
		}
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
