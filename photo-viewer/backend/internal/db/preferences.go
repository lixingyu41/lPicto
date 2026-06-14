package db

import (
	"context"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/util"
)

func NormalizeRotation(value int) int {
	value = value % 360
	if value < 0 {
		value += 360
	}
	switch value {
	case 90, 180, 270:
		return value
	default:
		return 0
	}
}

func (d *DB) GetAssetPreference(ctx context.Context, assetID int64) (model.AssetPreference, error) {
	row := d.conn.QueryRowContext(ctx, `
SELECT assets.id, COALESCE(asset_preferences.rotation, 0), COALESCE(asset_preferences.updated_at, assets.updated_at)
FROM assets
LEFT JOIN asset_preferences ON asset_preferences.asset_id = assets.id
WHERE assets.id = ? AND assets.deleted_at IS NULL`, assetID)
	var pref model.AssetPreference
	err := row.Scan(&pref.AssetID, &pref.Rotation, &pref.UpdatedAt)
	pref.Rotation = NormalizeRotation(pref.Rotation)
	return pref, err
}

func (d *DB) SetAssetRotation(ctx context.Context, assetID int64, rotation int) (model.AssetPreference, error) {
	rotation = NormalizeRotation(rotation)
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
INSERT INTO asset_preferences (asset_id, rotation, updated_at)
SELECT id, ?, ? FROM assets WHERE id = ? AND deleted_at IS NULL
ON CONFLICT(asset_id) DO UPDATE SET rotation = excluded.rotation, updated_at = excluded.updated_at`,
		rotation, now, assetID)
	if err != nil {
		return model.AssetPreference{}, err
	}
	return d.GetAssetPreference(ctx, assetID)
}
