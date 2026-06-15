package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrationInitializesDatabase(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "lpicto.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE rel_path = ''`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("root folder count = %d", count)
	}
}

func TestOpenMarksInterruptedScanRuns(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "lpicto.db")
	migrationsDir := filepath.Join("..", "..", "migrations")
	database, err := Open(ctx, dbPath, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.StartScanRun(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(ctx, dbPath, migrationsDir)
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
