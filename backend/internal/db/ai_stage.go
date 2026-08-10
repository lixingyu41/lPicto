package db

import (
	"context"
	"database/sql"
)

type AIStage struct {
	AssetID   int64
	CacheKey  string
	State     string
	StagePath string
	SizeBytes int64
	ExpiresAt *int64
	UpdatedAt int64
}

func (d *DB) UpsertAIStage(ctx context.Context, stage AIStage) error {
	_, err := d.Conn().ExecContext(ctx, `
INSERT INTO asset_ai_stage(asset_id,cache_key,state,stage_path,size_bytes,prepared_at,expires_at,error_message,updated_at)
VALUES($1,$2,$3,NULLIF($4,''),$5,CASE WHEN $3='ready' THEN now() ELSE NULL END,
       CASE WHEN $6::bigint > 0 THEN to_timestamp($6) ELSE NULL END,NULL,now())
ON CONFLICT(asset_id) DO UPDATE SET cache_key=EXCLUDED.cache_key,state=EXCLUDED.state,
stage_path=EXCLUDED.stage_path,size_bytes=EXCLUDED.size_bytes,prepared_at=EXCLUDED.prepared_at,
expires_at=EXCLUDED.expires_at,error_message=NULL,updated_at=now()`,
		stage.AssetID, stage.CacheKey, stage.State, stage.StagePath, stage.SizeBytes, valueOrZero(stage.ExpiresAt))
	return err
}

func (d *DB) AIStageForAsset(ctx context.Context, assetID int64, cacheKey string) (*AIStage, error) {
	var item AIStage
	var path sql.NullString
	var expires sql.NullTime
	err := d.Conn().QueryRowContext(ctx, `
SELECT asset_id,cache_key,state,stage_path,size_bytes,expires_at
FROM asset_ai_stage WHERE asset_id=$1 AND cache_key=$2`, assetID, cacheKey).Scan(
		&item.AssetID, &item.CacheKey, &item.State, &path, &item.SizeBytes, &expires)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.StagePath = path.String
	if expires.Valid {
		value := expires.Time.Unix()
		item.ExpiresAt = &value
	}
	return &item, nil
}

func (d *DB) MarkAIStageProcessing(ctx context.Context, assetID int64, cacheKey string) error {
	_, err := d.Conn().ExecContext(ctx, `
UPDATE asset_ai_stage SET state='processing',updated_at=now()
WHERE asset_id=$1 AND cache_key=$2`, assetID, cacheKey)
	return err
}

func (d *DB) DeleteAIStage(ctx context.Context, assetID int64) error {
	_, err := d.Conn().ExecContext(ctx, `DELETE FROM asset_ai_stage WHERE asset_id=$1`, assetID)
	return err
}

func (d *DB) DeleteAIStageByCacheKey(ctx context.Context, cacheKey string) error {
	_, err := d.Conn().ExecContext(ctx, `DELETE FROM asset_ai_stage WHERE cache_key=$1`, cacheKey)
	return err
}

func (d *DB) AIStages(ctx context.Context) ([]AIStage, error) {
	rows, err := d.Conn().QueryContext(ctx, `
SELECT asset_id,cache_key,state,stage_path,size_bytes,expires_at,
       EXTRACT(EPOCH FROM updated_at)::bigint
FROM asset_ai_stage ORDER BY asset_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AIStage
	for rows.Next() {
		var item AIStage
		var path sql.NullString
		var expires sql.NullTime
		if err := rows.Scan(&item.AssetID, &item.CacheKey, &item.State, &path, &item.SizeBytes, &expires, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.StagePath = path.String
		if expires.Valid {
			value := expires.Time.Unix()
			item.ExpiresAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) AIStageStats(ctx context.Context) (ready int, bytes int64, err error) {
	err = d.Conn().QueryRowContext(ctx, `
SELECT COUNT(*),COALESCE(SUM(stage.size_bytes),0)
FROM asset_ai_stage stage
JOIN media_asset asset ON asset.id=stage.asset_id AND asset.cache_key=stage.cache_key
LEFT JOIN asset_ai_result result ON result.asset_id=asset.id
WHERE stage.state='ready'
  AND asset.deleted_at IS NULL
  AND asset.media_type IN (1,2)
  AND EXISTS (
    SELECT 1 FROM file_instance file
    WHERE file.asset_id=asset.id AND file.missing=false
  )
  AND (
    result.asset_id IS NULL
    OR result.input_cache_key<>asset.cache_key
    OR result.status='pending'
  )`).Scan(&ready, &bytes)
	return
}
