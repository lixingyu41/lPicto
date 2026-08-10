package scanner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"lpicto/backend/internal/storage"
)

func TestCountMediaFilesForRootsReportsIncrementalProgress(t *testing.T) {
	photoRoot := t.TempDir()
	dataRoot := t.TempDir()
	for _, name := range []string{"1.jpg", "2.mp4", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(photoRoot, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.New(photoRoot, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	var progress []int
	total, err := CountMediaFilesForRootsProgress(context.Background(), store, []string{""}, func(count int) {
		progress = append(progress, count)
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if !reflect.DeepEqual(progress, []int{1, 2}) {
		t.Fatalf("progress = %#v, want [1 2]", progress)
	}
}

func TestStorageRecoveryScanDoesNotInterruptForegroundScan(t *testing.T) {
	if !isAutomaticScanRequest(scanRequest{reason: "storage_recovered"}) {
		t.Fatal("storage recovery scan must wait behind the active foreground scan")
	}
}
