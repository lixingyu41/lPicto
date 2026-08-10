package storage

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOperationNotPermittedMarksSourceUnavailable(t *testing.T) {
	if !IsSourceUnavailable(errors.New("[Errno 1] Operation not permitted: /Media/nas/VID/a.mp4")) {
		t.Fatal("operation-not-permitted storage error was not recognized")
	}
}

func TestSourceProbeTimeoutMarksSourceUnavailable(t *testing.T) {
	for _, message := range []string{"source probe timed out", "source read timed out after 2s"} {
		if !IsSourceUnavailable(errors.New(message)) {
			t.Fatalf("%q was not recognized as unavailable", message)
		}
	}
}

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

func TestSamePathCaseSensitivityMatchesOS(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !samePath(`C:\Photos\A`, `c:\photos\a`) {
			t.Fatal("windows paths should compare case-insensitively")
		}
		return
	}
	if samePath("/photos/A", "/photos/a") {
		t.Fatal("non-windows paths should compare case-sensitively")
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
		{kind: "storyboards", ext: "webp"},
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
		{kind: "storyboards", ext: "webp"},
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

func TestRemoveCachePrefixDeletesOnlyMatchingVariants(t *testing.T) {
	store, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefix := "abcdef1234567890-hls-"
	matching := []string{prefix + "one", prefix + "two"}
	other := "abcdef9999999999-hls-other"
	for _, key := range append(append([]string{}, matching...), other) {
		path, err := store.CachePath("video-proxies", key, "ts")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("segment"), 0o644); err != nil {
			t.Fatal(err)
		}
		if key == matching[0] {
			if err := os.WriteFile(path+".tmp.ts", []byte("temp"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := store.RemoveCachePrefix(prefix, "video-proxies", "ts"); err != nil {
		t.Fatal(err)
	}
	for _, key := range matching {
		path, _ := store.CacheFilePath("video-proxies", key, "ts")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("matching cache still exists: %s", path)
		}
		if _, err := os.Stat(path + ".tmp.ts"); !os.IsNotExist(err) {
			t.Fatalf("matching temp cache still exists: %s", path)
		}
	}
	otherPath, _ := store.CacheFilePath("video-proxies", other, "ts")
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("unrelated cache was removed: %v", err)
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
