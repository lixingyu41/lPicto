package db

import (
	"context"
	"database/sql"
	"encoding/json"
)

const SettingSystemCollectionCounts = "system_collection_counts"

func (d *DB) GetSystemCollectionCounts(ctx context.Context) (map[string]int, bool, error) {
	var raw string
	err := d.conn.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, SettingSystemCollectionCounts).Scan(&raw)
	if err == sql.ErrNoRows {
		return map[string]int{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	counts := map[string]int{}
	if err := json.Unmarshal([]byte(raw), &counts); err != nil {
		return nil, false, err
	}
	return counts, true, nil
}

func (d *DB) RefreshSystemCollectionCounts(ctx context.Context) (map[string]int, error) {
	counts := make(map[string]int, len(systemCollections))
	for _, collection := range systemCollections {
		count, err := d.CountSystemCollectionAssets(ctx, collection.SystemKind, AssetListOptions{Page: 1, PageSize: 1})
		if err != nil {
			return nil, err
		}
		counts[collection.SystemKind] = count
	}
	raw, err := json.Marshal(counts)
	if err != nil {
		return nil, err
	}
	_, err = d.conn.ExecContext(ctx, `
INSERT INTO app_settings (key, value, updated_at)
VALUES (?, ?, now())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value, updated_at = now()`, SettingSystemCollectionCounts, string(raw))
	if err != nil {
		return nil, err
	}
	return counts, nil
}
