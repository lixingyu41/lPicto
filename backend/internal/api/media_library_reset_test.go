package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearDirectoryContentsKeepsRootAndRemovesEveryEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "thumbs", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "thumbs", "ab", "one.webp"), []byte("1234"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "temporary.part"), []byte("12"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, bytes, err := clearDirectoryContents(root)
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 || bytes != 6 {
		t.Fatalf("files = %d bytes = %d, want 2 and 6", files, bytes)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache root still contains %d entries", len(entries))
	}
}
