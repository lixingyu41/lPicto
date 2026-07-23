package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"lpicto/backend/internal/model"
)

var ErrEmptyAssetTag = errors.New("asset tag is empty")

type AssetTag struct {
	ID        int64
	Name      string
	CreatedAt int64
}

type TagSummary struct {
	ID         int64
	Name       string
	AssetCount int
	CreatedAt  int64
}

func NormalizeAssetTag(value string) string {
	return strings.TrimSpace(value)
}

func (d *DB) ListAssetTags(ctx context.Context, assetID int64) ([]AssetTag, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT tag.id, tag.name, EXTRACT(EPOCH FROM asset_tag.created_at)::BIGINT
FROM asset_tag
JOIN tag ON tag.id = asset_tag.tag_id
JOIN media_asset ON media_asset.id = asset_tag.asset_id
WHERE asset_tag.asset_id = ? AND media_asset.deleted_at IS NULL
ORDER BY lower(tag.name), tag.name`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([]AssetTag, 0)
	for rows.Next() {
		var tag AssetTag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (d *DB) AssetTagsByAssetIDs(ctx context.Context, assetIDs []int64) (map[int64][]AssetTag, error) {
	result := make(map[int64][]AssetTag, len(assetIDs))
	if len(assetIDs) == 0 {
		return result, nil
	}
	rows, err := d.conn.QueryContext(ctx, `
SELECT asset_tag.asset_id, tag.id, tag.name, EXTRACT(EPOCH FROM asset_tag.created_at)::BIGINT
FROM asset_tag
JOIN tag ON tag.id = asset_tag.tag_id
WHERE asset_tag.asset_id IN (`+queryPlaceholders(len(assetIDs))+`)
ORDER BY asset_tag.asset_id, lower(tag.name), tag.name`, int64Values(assetIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID int64
		var tag AssetTag
		if err := rows.Scan(&assetID, &tag.ID, &tag.Name, &tag.CreatedAt); err != nil {
			return nil, err
		}
		result[assetID] = append(result[assetID], tag)
	}
	return result, rows.Err()
}

func int64Values(values []int64) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func (d *DB) AddAssetTag(ctx context.Context, assetID int64, name string) (AssetTag, error) {
	name = NormalizeAssetTag(name)
	if name == "" {
		return AssetTag{}, ErrEmptyAssetTag
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return AssetTag{}, err
	}
	defer tx.Rollback()

	var assetExists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM media_asset WHERE id = ? AND deleted_at IS NULL
)`, assetID).Scan(&assetExists); err != nil {
		return AssetTag{}, err
	}
	if !assetExists {
		return AssetTag{}, sql.ErrNoRows
	}

	var tag AssetTag
	if err := tx.QueryRowContext(ctx, `
INSERT INTO tag (name)
VALUES (?)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id, name`, name).Scan(&tag.ID, &tag.Name); err != nil {
		return AssetTag{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO asset_tag (asset_id, tag_id)
VALUES (?, ?)
ON CONFLICT (asset_id, tag_id) DO NOTHING`, assetID, tag.ID); err != nil {
		return AssetTag{}, err
	}
	if err := tx.QueryRowContext(ctx, `
SELECT tag.id, tag.name, EXTRACT(EPOCH FROM asset_tag.created_at)::BIGINT
FROM asset_tag
JOIN tag ON tag.id = asset_tag.tag_id
WHERE asset_tag.asset_id = ? AND asset_tag.tag_id = ?`, assetID, tag.ID).Scan(&tag.ID, &tag.Name, &tag.CreatedAt); err != nil {
		return AssetTag{}, err
	}
	if err := tx.Commit(); err != nil {
		return AssetTag{}, err
	}
	return tag, nil
}

func (d *DB) DeleteAssetTag(ctx context.Context, assetID int64, name string) error {
	name = NormalizeAssetTag(name)
	if name == "" {
		return ErrEmptyAssetTag
	}
	_, err := d.conn.ExecContext(ctx, `
DELETE FROM asset_tag
USING tag
WHERE asset_tag.tag_id = tag.id
  AND asset_tag.asset_id = ?
  AND tag.name = ?`, assetID, name)
	return err
}

func (d *DB) ListAssetsByTag(ctx context.Context, name string, opts AssetListOptions) (model.Page[model.Asset], error) {
	name = NormalizeAssetTag(name)
	if name == "" {
		return model.Page[model.Asset]{}, ErrEmptyAssetTag
	}
	opts.ManualTag = name
	return d.listAssets(ctx, opts, false)
}

func (d *DB) ListTags(ctx context.Context) ([]TagSummary, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT tag.id, tag.name, COUNT(asset_tag.asset_id)::INT, COALESCE(EXTRACT(EPOCH FROM MIN(asset_tag.created_at))::BIGINT, 0)
FROM tag
LEFT JOIN asset_tag ON asset_tag.tag_id = tag.id
GROUP BY tag.id, tag.name
ORDER BY lower(tag.name), tag.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TagSummary, 0)
	for rows.Next() {
		var item TagSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.AssetCount, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) CreateTag(ctx context.Context, name string) (TagSummary, error) {
	name = NormalizeAssetTag(name)
	if name == "" {
		return TagSummary{}, ErrEmptyAssetTag
	}
	var id int64
	if err := d.conn.QueryRowContext(ctx, `
INSERT INTO tag (name)
VALUES (?)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id`, name).Scan(&id); err != nil {
		return TagSummary{}, err
	}
	return d.GetTag(ctx, id)
}

func (d *DB) GetTag(ctx context.Context, id int64) (TagSummary, error) {
	var item TagSummary
	err := d.conn.QueryRowContext(ctx, `
SELECT tag.id, tag.name, COUNT(asset_tag.asset_id)::INT, COALESCE(EXTRACT(EPOCH FROM MIN(asset_tag.created_at))::BIGINT, 0)
FROM tag
LEFT JOIN asset_tag ON asset_tag.tag_id = tag.id
WHERE tag.id = ?
GROUP BY tag.id, tag.name`, id).Scan(&item.ID, &item.Name, &item.AssetCount, &item.CreatedAt)
	return item, err
}

func (d *DB) RenameTag(ctx context.Context, id int64, name string) (TagSummary, error) {
	name = NormalizeAssetTag(name)
	if name == "" {
		return TagSummary{}, ErrEmptyAssetTag
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return TagSummary{}, err
	}
	defer tx.Rollback()
	var existingID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM tag WHERE name = ? AND id <> ?`, name, id).Scan(&existingID)
	if err == nil {
		if err := mergeTagIDs(ctx, tx, existingID, []int64{id}); err != nil {
			return TagSummary{}, err
		}
		if err := tx.Commit(); err != nil {
			return TagSummary{}, err
		}
		return d.GetTag(ctx, existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TagSummary{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE tag SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return TagSummary{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return TagSummary{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return TagSummary{}, err
	}
	return d.GetTag(ctx, id)
}

func (d *DB) DeleteTag(ctx context.Context, id int64) error {
	result, err := d.conn.ExecContext(ctx, `DELETE FROM tag WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) MergeTags(ctx context.Context, targetID int64, sourceIDs []int64, targetName string) (TagSummary, error) {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return TagSummary{}, err
	}
	defer tx.Rollback()
	if strings.TrimSpace(targetName) != "" {
		targetName = NormalizeAssetTag(targetName)
		if targetName == "" {
			return TagSummary{}, ErrEmptyAssetTag
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tag SET name = ? WHERE id = ?`, targetName, targetID); err != nil {
			return TagSummary{}, err
		}
	}
	if err := mergeTagIDs(ctx, tx, targetID, sourceIDs); err != nil {
		return TagSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return TagSummary{}, err
	}
	return d.GetTag(ctx, targetID)
}

func mergeTagIDs(ctx context.Context, tx *sqlTx, targetID int64, sourceIDs []int64) error {
	if targetID <= 0 {
		return sql.ErrNoRows
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tag WHERE id = ?)`, targetID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	for _, sourceID := range uniqueInt64s(sourceIDs) {
		if sourceID == targetID {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO asset_tag (asset_id, tag_id, created_at)
SELECT asset_id, ?, created_at
FROM asset_tag
WHERE tag_id = ?
ON CONFLICT (asset_id, tag_id) DO NOTHING`, targetID, sourceID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tag WHERE id = ?`, sourceID); err != nil {
			return err
		}
	}
	return nil
}
