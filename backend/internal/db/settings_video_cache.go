package db

import (
	"context"
	"strconv"
)

const (
	SettingVideoProxyCacheTTLSeconds = "video_proxy_cache_ttl_seconds"
	SettingVideoProxyCacheMaxBytes   = "video_proxy_cache_max_bytes"
)

type VideoProxyCacheSettings struct {
	TTLSeconds int64
	MaxBytes   int64
}

func (d *DB) GetVideoProxyCacheSettings(ctx context.Context, fallback VideoProxyCacheSettings) (VideoProxyCacheSettings, error) {
	settings := fallback
	rows, err := d.conn.QueryContext(ctx, `
SELECT key, value
FROM app_settings
WHERE key IN (?, ?)`, SettingVideoProxyCacheTTLSeconds, SettingVideoProxyCacheMaxBytes)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case SettingVideoProxyCacheTTLSeconds:
			settings.TTLSeconds = parsed
		case SettingVideoProxyCacheMaxBytes:
			settings.MaxBytes = parsed
		}
	}
	return settings, rows.Err()
}

func (d *DB) SetVideoProxyCacheSettings(ctx context.Context, settings VideoProxyCacheSettings) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := upsertAppSetting(ctx, tx, SettingVideoProxyCacheTTLSeconds, strconv.FormatInt(settings.TTLSeconds, 10)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := upsertAppSetting(ctx, tx, SettingVideoProxyCacheMaxBytes, strconv.FormatInt(settings.MaxBytes, 10)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func upsertAppSetting(ctx context.Context, tx *sqlTx, key string, value string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO app_settings (key, value, updated_at)
VALUES (?, ?, now())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value, updated_at = now()`, key, value)
	return err
}
