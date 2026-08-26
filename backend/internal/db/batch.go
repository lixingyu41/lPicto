package db

import (
	"context"
	"database/sql"
	"strings"

	"lpicto/backend/internal/util"
)

func (d *DB) AddTagToAssets(ctx context.Context, assetIDs []int64, name string) ([]int64, error) {
	name = NormalizeAssetTag(name)
	if name == "" {
		return nil, ErrEmptyAssetTag
	}
	ids := uniqueInt64s(assetIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var tagID int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO tag (name)
VALUES (?)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id`, name).Scan(&tagID); err != nil {
		return nil, err
	}
	updated := make([]int64, 0, len(ids))
	for _, assetID := range ids {
		result, err := tx.ExecContext(ctx, `
INSERT INTO asset_tag (asset_id, tag_id)
SELECT id, ?
FROM media_asset
WHERE id = ? AND deleted_at IS NULL
ON CONFLICT (asset_id, tag_id) DO NOTHING`, tagID, assetID)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			updated = append(updated, assetID)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (d *DB) SetAssetsRating(ctx context.Context, assetIDs []int64, rating int) ([]int64, error) {
	rating = NormalizeRating(rating)
	return d.setAssetPreferenceBatch(ctx, assetIDs, nil, &rating)
}

func (d *DB) SetAssetsRotation(ctx context.Context, assetIDs []int64, rotation int) ([]int64, error) {
	rotation = NormalizeRotation(rotation)
	return d.setAssetPreferenceBatch(ctx, assetIDs, &rotation, nil)
}

func (d *DB) setAssetPreferenceBatch(ctx context.Context, assetIDs []int64, rotation *int, rating *int) ([]int64, error) {
	ids := uniqueInt64s(assetIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := util.UnixNow()
	updated := make([]int64, 0, len(ids))
	for _, assetID := range ids {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM media_asset WHERE id = ? AND deleted_at IS NULL)`, assetID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		columns := []string{"asset_id", "updated_at"}
		values := []string{"?", "?"}
		args := []any{assetID, now}
		updates := []string{"updated_at = excluded.updated_at"}
		if rotation != nil {
			columns = append(columns, "rotation")
			values = append(values, "?")
			args = append(args, NormalizeRotation(*rotation))
			updates = append(updates, "rotation = excluded.rotation")
		}
		if rating != nil {
			columns = append(columns, "rating")
			values = append(values, "?")
			args = append(args, NormalizeRating(*rating))
			updates = append(updates, "rating = excluded.rating")
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO asset_preferences (`+strings.Join(columns, ", ")+`)
VALUES (`+strings.Join(values, ", ")+`)
ON CONFLICT(asset_id) DO UPDATE SET `+strings.Join(updates, ", "), args...); err != nil {
			return nil, err
		}
		updated = append(updated, assetID)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (d *DB) SetAssetsHidden(ctx context.Context, assetIDs []int64, hidden bool) ([]int64, error) {
	ids := uniqueInt64s(assetIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, hidden)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := d.conn.QueryContext(ctx, `
UPDATE media_asset
SET hidden = ?, updated_at = now()
WHERE deleted_at IS NULL AND id IN (`+strings.Join(placeholders, ",")+`)
RETURNING id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	updated := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		updated = append(updated, id)
	}
	return updated, rows.Err()
}

func (d *DB) MarkDeletedAssetIDs(ctx context.Context, assetIDs []int64, deletedAt int64) ([]DeletedAsset, error) {
	ids := uniqueInt64s(assetIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := d.conn.QueryContext(ctx, `
SELECT id, rel_path, cache_key, media_type
FROM assets
WHERE deleted_at IS NULL AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DeletedAsset, 0, len(ids))
	updateIDs := make([]any, 0, len(ids))
	for rows.Next() {
		var item DeletedAsset
		if err := rows.Scan(&item.ID, &item.RelPath, &item.CacheKey, &item.MediaType); err != nil {
			return nil, err
		}
		items = append(items, item)
		updateIDs = append(updateIDs, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(updateIDs) == 0 {
		return nil, nil
	}
	updatePlaceholders := make([]string, len(updateIDs))
	updateArgs := []any{unixTime(deletedAt), unixTime(deletedAt)}
	for i, id := range updateIDs {
		updatePlaceholders[i] = "?"
		updateArgs = append(updateArgs, id)
	}
	if _, err := d.conn.ExecContext(ctx, `
UPDATE media_asset
SET deleted = true, deleted_at = ?, sha256 = NULL, updated_at = ?
WHERE deleted = false AND id IN (`+strings.Join(updatePlaceholders, ",")+`)`, updateArgs...); err != nil {
		return nil, err
	}
	if _, err := d.conn.ExecContext(ctx, `
UPDATE file_instance
SET missing = true
WHERE asset_id IN (`+strings.Join(updatePlaceholders, ",")+`)`, updateIDs...); err != nil {
		return nil, err
	}
	return items, nil
}

func (d *DB) PurgeAssetIDs(ctx context.Context, assetIDs []int64) ([]DeletedAsset, error) {
	ids := uniqueInt64s(assetIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT id, rel_path, cache_key, media_type
FROM asset_records
WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	items := make([]DeletedAsset, 0, len(ids))
	deleteIDs := make([]any, 0, len(ids))
	for rows.Next() {
		var item DeletedAsset
		if err := rows.Scan(&item.ID, &item.RelPath, &item.CacheKey, &item.MediaType); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
		deleteIDs = append(deleteIDs, item.ID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(deleteIDs) == 0 {
		return nil, nil
	}
	deletePlaceholders := make([]string, len(deleteIDs))
	for i := range deleteIDs {
		deletePlaceholders[i] = "?"
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_asset WHERE id IN (`+strings.Join(deletePlaceholders, ",")+`)`, deleteIDs...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return uniqueDeletedAssetsDB(items), nil
}

func uniqueDeletedAssetsDB(items []DeletedAsset) []DeletedAsset {
	seen := make(map[int64]struct{}, len(items))
	result := make([]DeletedAsset, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		result = append(result, item)
	}
	return result
}

func ErrNoRowsIfEmptyAssetIDs(ids []int64) error {
	if len(uniqueInt64s(ids)) == 0 {
		return sql.ErrNoRows
	}
	return nil
}
