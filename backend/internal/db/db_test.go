package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrationInitializesDatabase(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, testDatabaseURL(t, ctx), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM folder WHERE rel_path = ''`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("root folder count = %d", count)
	}
	if err := database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM scan_library`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("scan library count = %d, want 0", count)
	}
}

func TestOpenMarksInterruptedScanRuns(t *testing.T) {
	ctx := context.Background()
	databaseURL := testDatabaseURL(t, ctx)
	migrationsDir := filepath.Join("..", "..", "migrations")
	database, err := Open(ctx, databaseURL, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.StartScanRun(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(ctx, databaseURL, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	run, err := database.LastScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.Status != "interrupted" || run.FinishedAt == nil {
		t.Fatalf("last scan run = %#v, want interrupted with finished_at", run)
	}
}

func testDatabaseURL(t *testing.T, ctx context.Context) string {
	t.Helper()
	databaseURL, adminURL, databaseName, err := createTestDatabase(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = dropTestDatabase(context.Background(), adminURL, databaseName)
	})
	return databaseURL
}
