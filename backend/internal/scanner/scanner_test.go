package scanner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/storage"
)

func TestLibraryScansDoNotDeleteOtherRoots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scan, database := newTestScanner(t, ctx)

	writeTestFile(t, scan.Store, "Y/one.jpg")
	writeTestFile(t, scan.Store, "Z/two.jpg")

	if result := scan.RequestScanRoots("Y", []string{"Y"}); !result.Accepted {
		t.Fatalf("Y scan result = %#v", result)
	}
	waitScannerIdle(t, ctx, scan)
	assertActiveRelPaths(t, ctx, database, []string{"Y/one.jpg"})

	if result := scan.RequestScanRoots("Z", []string{"Z"}); !result.Accepted {
		t.Fatalf("Z scan result = %#v", result)
	}
	waitScannerIdle(t, ctx, scan)
	assertActiveRelPaths(t, ctx, database, []string{"Y/one.jpg", "Z/two.jpg"})
}

func TestLibraryScanDeletesOnlyInsideScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scan, database := newTestScanner(t, ctx)

	writeTestFile(t, scan.Store, "Y/keep.jpg")
	removePath := writeTestFile(t, scan.Store, "Y/remove.jpg")
	writeTestFile(t, scan.Store, "Z/keep.jpg")

	if result := scan.RequestScanRoots("all", []string{""}); !result.Accepted {
		t.Fatalf("full scan result = %#v", result)
	}
	waitScannerIdle(t, ctx, scan)

	if err := os.Remove(removePath); err != nil {
		t.Fatal(err)
	}
	if result := scan.RequestScanRoots("Y", []string{"Y"}); !result.Accepted {
		t.Fatalf("Y rescan result = %#v", result)
	}
	waitScannerIdle(t, ctx, scan)
	assertActiveRelPaths(t, ctx, database, []string{"Y/keep.jpg", "Z/keep.jpg"})
}

func TestStopThenStartRunsLatestRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	installSlowExifTool(t, 200*time.Millisecond)
	scan, database := newTestScanner(t, ctx)
	scan.Extractor = media.Extractor{CommandTimeout: 2 * time.Second}

	writeTestFile(t, scan.Store, "Y/first.jpg")
	writeTestFile(t, scan.Store, "Z/latest.jpg")

	if result := scan.RequestScanRoots("first", []string{"Y"}); !result.Accepted {
		t.Fatalf("first scan result = %#v", result)
	}
	if result := scan.RequestStop(); !result.Accepted {
		t.Fatalf("stop result = %#v", result)
	}
	if result := scan.RequestScanRoots("latest", []string{"Z"}); !result.Accepted {
		t.Fatalf("latest scan result = %#v", result)
	}
	waitScannerIdle(t, ctx, scan)
	assertActiveRelPaths(t, ctx, database, []string{"Z/latest.jpg"})
}

func newTestScanner(t *testing.T, ctx context.Context) (*Scanner, *db.DB) {
	t.Helper()
	dataRoot := t.TempDir()
	yRoot := t.TempDir()
	zRoot := t.TempDir()
	store, err := storage.NewWithRoots([]storage.RootConfig{
		{ID: "Y", Path: yRoot},
		{ID: "Z", Path: zRoot},
	}, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(ctx, filepath.Join(dataRoot, "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	scan := &Scanner{
		DB:          database,
		Store:       store,
		Extractor:   media.Extractor{CommandTimeout: 20 * time.Millisecond},
		ScanWorkers: 2,
		Logger:      slog.Default(),
	}
	scan.Start(ctx)
	return scan, database
}

func writeTestFile(t *testing.T, store storage.Store, rel string) string {
	t.Helper()
	path, err := store.PhotoPath(rel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not really a jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitScannerIdle(t *testing.T, ctx context.Context, scan *Scanner) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := scan.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !status.Running {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("scanner did not become idle: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertActiveRelPaths(t *testing.T, ctx context.Context, database *db.DB, want []string) {
	t.Helper()
	active, err := database.ActiveRelPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(active))
	for rel := range active {
		got = append(got, rel)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("active rel paths = %#v, want %#v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("active rel paths = %#v, want %#v", got, want)
		}
	}
}

func installSlowExifTool(t *testing.T, delay time.Duration) {
	t.Helper()
	bin := t.TempDir()
	delaySeconds := delay.Seconds()
	if runtime.GOOS == "windows" {
		ps1Path := filepath.Join(bin, "slow-exif.ps1")
		ps1 := "Start-Sleep -Seconds " + formatDelay(delaySeconds) + "\r\n" +
			"Write-Output '[{\"MIMEType\":\"image/jpeg\",\"ImageWidth\":1,\"ImageHeight\":1}]'\r\n"
		if err := os.WriteFile(ps1Path, []byte(ps1), 0o644); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(bin, "exiftool.bat")
		script := "@echo off\r\n" +
			"powershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0slow-exif.ps1\"\r\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		path := filepath.Join(bin, "exiftool")
		script := "#!/bin/sh\nsleep " + formatDelay(delaySeconds) + "\nprintf '[{\"MIMEType\":\"image/jpeg\",\"ImageWidth\":1,\"ImageHeight\":1}]'\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func formatDelay(seconds float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(seconds, 'f', 3, 64), "0"), ".")
}
