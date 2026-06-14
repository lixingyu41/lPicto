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
