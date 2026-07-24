package db

import "context"

type MediaLibraryResetResult struct {
	DeletedAssets int64
}

func (d *DB) ResetMediaLibrary(ctx context.Context) (MediaLibraryResetResult, error) {
	var result MediaLibraryResetResult
	if err := d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_asset`).Scan(&result.DeletedAssets); err != nil {
		return MediaLibraryResetResult{}, err
	}
	tx, err := d.raw.BeginTx(ctx, nil)
	if err != nil {
		return MediaLibraryResetResult{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
TRUNCATE TABLE
  media_asset,
  folder,
  tag,
  albums,
  album_groups,
  smart_collections,
  scan_runs,
  system_task_state
RESTART IDENTITY CASCADE`); err != nil {
		return MediaLibraryResetResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM app_settings WHERE key='system_collection_counts'`); err != nil {
		return MediaLibraryResetResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return MediaLibraryResetResult{}, err
	}
	return result, nil
}
