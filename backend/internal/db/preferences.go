package db

import (
	"context"
	"strings"

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

func NormalizeRating(value int) int {
	if value < 0 || value > 5 {
		return 0
	}
	return value
}

func ValidRating(value int) bool {
	return value >= 0 && value <= 5
}

func (d *DB) GetAssetPreference(ctx context.Context, assetID int64) (model.AssetPreference, error) {
	row := d.conn.QueryRowContext(ctx, `
SELECT assets.id, COALESCE(asset_preferences.rotation, 0), COALESCE(asset_preferences.rating, 0), COALESCE(asset_preferences.updated_at, assets.updated_at)
FROM assets
LEFT JOIN asset_preferences ON asset_preferences.asset_id = assets.id
WHERE assets.id = ? AND assets.deleted_at IS NULL`, assetID)
	var pref model.AssetPreference
	err := row.Scan(&pref.AssetID, &pref.Rotation, &pref.Rating, &pref.UpdatedAt)
	pref.Rotation = NormalizeRotation(pref.Rotation)
	pref.Rating = NormalizeRating(pref.Rating)
	return pref, err
}

func (d *DB) SetAssetRotation(ctx context.Context, assetID int64, rotation int) (model.AssetPreference, error) {
	rotation = NormalizeRotation(rotation)
	return d.SetAssetPreferences(ctx, assetID, &rotation, nil)
}

func (d *DB) SetAssetRating(ctx context.Context, assetID int64, rating int) (model.AssetPreference, error) {
	rating = NormalizeRating(rating)
	return d.SetAssetPreferences(ctx, assetID, nil, &rating)
}

func (d *DB) SetAssetPreferences(ctx context.Context, assetID int64, rotation *int, rating *int) (model.AssetPreference, error) {
	if rotation == nil && rating == nil {
		return d.GetAssetPreference(ctx, assetID)
	}
	now := util.UnixNow()
	columns := []string{"asset_id", "updated_at"}
	selects := []string{"id", "?"}
	args := []any{now}
	updates := []string{"updated_at = excluded.updated_at"}
	if rotation != nil {
		normalized := NormalizeRotation(*rotation)
		columns = append(columns, "rotation")
		selects = append(selects, "?")
		args = append(args, normalized)
		updates = append(updates, "rotation = excluded.rotation")
	}
	if rating != nil {
		normalized := NormalizeRating(*rating)
		columns = append(columns, "rating")
		selects = append(selects, "?")
		args = append(args, normalized)
		updates = append(updates, "rating = excluded.rating")
	}
	args = append(args, assetID)
	query := `
INSERT INTO asset_preferences (` + strings.Join(columns, ", ") + `)
SELECT ` + strings.Join(selects, ", ") + ` FROM assets WHERE id = ? AND deleted_at IS NULL
ON CONFLICT(asset_id) DO UPDATE SET ` + strings.Join(updates, ", ")
	_, err := d.conn.ExecContext(ctx, query, args...)
	if err != nil {
		return model.AssetPreference{}, err
	}
	return d.GetAssetPreference(ctx, assetID)
}
