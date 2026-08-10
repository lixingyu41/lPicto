package db

import (
	"context"
	"database/sql"
	"time"
)

type CacheEntry struct {
	CacheKey     string
	Kind         string
	RelativePath string
	AssetID      *int64
	SizeBytes    int64
	LastAccessed int64
	PinnedUntil  *int64
	State        string
}

type CacheKindUsage struct {
	Kind      string
	SizeBytes int64
	FileCount int
}

type SourceIOBatch struct {
	ID         int64
	Reason     string
	Priority   int
	State      string
	ItemCount  int
	BytesRead  int64
	StartedAt  int64
	FinishedAt *int64
	Error      string
}

func (d *DB) UpsertCacheEntry(ctx context.Context, entry CacheEntry) error {
	var assetID any
	if entry.AssetID != nil {
		assetID = *entry.AssetID
	}
	_, err := d.Conn().ExecContext(ctx, `
INSERT INTO cache_entry(cache_key, kind, relative_path, asset_id, size_bytes, last_accessed_at, pinned_until, state, updated_at)
VALUES($1,$2,$3,$4,$5,now(),CASE WHEN $6::bigint > 0 THEN to_timestamp($6) ELSE NULL END,$7,now())
ON CONFLICT(kind, cache_key, relative_path) DO UPDATE SET
  asset_id=EXCLUDED.asset_id,
  size_bytes=EXCLUDED.size_bytes,
  last_accessed_at=now(),
  pinned_until=EXCLUDED.pinned_until,
  state=EXCLUDED.state,
  updated_at=now()`,
		entry.CacheKey, entry.Kind, entry.RelativePath, assetID, entry.SizeBytes, valueOrZero(entry.PinnedUntil), entry.State)
	return err
}

func (d *DB) TouchCacheEntry(ctx context.Context, kind, cacheKey, relativePath string) error {
	_, err := d.Conn().ExecContext(ctx, `
UPDATE cache_entry SET last_accessed_at=now(), updated_at=now()
WHERE kind=$1 AND cache_key=$2 AND relative_path=$3
  AND last_accessed_at < now() - interval '5 minutes'`, kind, cacheKey, relativePath)
	return err
}

func (d *DB) DeleteCacheEntry(ctx context.Context, kind, cacheKey, relativePath string) error {
	_, err := d.Conn().ExecContext(ctx,
		`DELETE FROM cache_entry WHERE kind=$1 AND cache_key=$2 AND relative_path=$3`,
		kind, cacheKey, relativePath)
	return err
}

func (d *DB) DeleteCacheEntryByPath(ctx context.Context, kind, relativePath string) error {
	_, err := d.Conn().ExecContext(ctx,
		`DELETE FROM cache_entry WHERE kind=$1 AND relative_path=$2`,
		kind, relativePath)
	return err
}

func (d *DB) DeleteCacheEntriesByCacheKey(ctx context.Context, kind, cacheKey string) error {
	_, err := d.Conn().ExecContext(ctx,
		`DELETE FROM cache_entry WHERE kind=$1 AND cache_key=$2`,
		kind, cacheKey)
	return err
}

func (d *DB) DeleteOrphanAIStageCacheEntries(ctx context.Context) error {
	_, err := d.Conn().ExecContext(ctx, `
DELETE FROM cache_entry entry
WHERE entry.kind='ai-staging'
  AND NOT EXISTS (
    SELECT 1 FROM asset_ai_stage stage
    WHERE stage.cache_key=entry.cache_key
  )`)
	return err
}

func (d *DB) CacheEntryAccessTimes(ctx context.Context) (map[string]int64, error) {
	rows, err := d.Conn().QueryContext(ctx, `
SELECT kind,relative_path,EXTRACT(EPOCH FROM last_accessed_at)::bigint
FROM cache_entry`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var kind, relativePath string
		var accessedAt int64
		if err := rows.Scan(&kind, &relativePath, &accessedAt); err != nil {
			return nil, err
		}
		result[kind+"\x00"+relativePath] = accessedAt
	}
	return result, rows.Err()
}

func (d *DB) CacheUsageByKind(ctx context.Context) ([]CacheKindUsage, error) {
	rows, err := d.Conn().QueryContext(ctx, `
SELECT kind, COALESCE(SUM(size_bytes),0), COUNT(*)
FROM cache_entry WHERE state='ready' GROUP BY kind ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []CacheKindUsage
	for rows.Next() {
		var item CacheKindUsage
		if err := rows.Scan(&item.Kind, &item.SizeBytes, &item.FileCount); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) EvictableCacheEntries(ctx context.Context, kinds []string, limit int) ([]CacheEntry, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := d.Conn().QueryContext(ctx, `
SELECT cache_key, kind, relative_path, asset_id, size_bytes,
       EXTRACT(EPOCH FROM last_accessed_at)::bigint, EXTRACT(EPOCH FROM pinned_until)::bigint, state
FROM cache_entry
WHERE kind = ANY($1) AND state='ready' AND (pinned_until IS NULL OR pinned_until < now())
ORDER BY last_accessed_at ASC, size_bytes DESC LIMIT $2`, kinds, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []CacheEntry
	for rows.Next() {
		var item CacheEntry
		var assetID, pinned sql.NullInt64
		if err := rows.Scan(&item.CacheKey, &item.Kind, &item.RelativePath, &assetID, &item.SizeBytes, &item.LastAccessed, &pinned, &item.State); err != nil {
			return nil, err
		}
		if assetID.Valid {
			item.AssetID = &assetID.Int64
		}
		if pinned.Valid {
			item.PinnedUntil = &pinned.Int64
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) BeginSourceIOBatch(ctx context.Context, reason string, priority int) (int64, error) {
	var id int64
	err := d.Conn().QueryRowContext(ctx, `
INSERT INTO source_io_batch(reason,priority,state) VALUES($1,$2,'running') RETURNING id`,
		reason, priority).Scan(&id)
	return id, err
}

func (d *DB) FinishSourceIOBatch(ctx context.Context, id int64, state string, itemCount int, bytesRead int64, message string) error {
	_, err := d.Conn().ExecContext(ctx, `
UPDATE source_io_batch SET state=$2,item_count=$3,bytes_read=$4,error_message=NULLIF($5,''),
finished_at=now() WHERE id=$1`, id, state, itemCount, bytesRead, message)
	return err
}

func (d *DB) LatestSourceIOBatch(ctx context.Context) (*SourceIOBatch, error) {
	var item SourceIOBatch
	var finished sql.NullTime
	var message sql.NullString
	var started time.Time
	err := d.Conn().QueryRowContext(ctx, `
SELECT id,reason,priority,state,item_count,bytes_read,started_at,finished_at,error_message
FROM source_io_batch ORDER BY started_at DESC LIMIT 1`).Scan(
		&item.ID, &item.Reason, &item.Priority, &item.State, &item.ItemCount, &item.BytesRead,
		&started, &finished, &message)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.StartedAt = started.Unix()
	if finished.Valid {
		value := finished.Time.Unix()
		item.FinishedAt = &value
	}
	item.Error = message.String
	return &item, nil
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
