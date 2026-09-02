package db

import (
	"context"
	"strconv"
)

const (
	SettingExternalFileAccessPaused  = "debug_external_file_access_paused"
	SettingBackgroundProcessingPause = "debug_background_processing_paused"
)

type DebugSettings struct {
	ExternalFileAccessPaused   bool `json:"externalFileAccessPaused"`
	BackgroundProcessingPaused bool `json:"backgroundProcessingPaused"`
}

func (d *DB) GetDebugSettings(ctx context.Context) (DebugSettings, error) {
	var settings DebugSettings
	rows, err := d.conn.QueryContext(ctx, `SELECT key,value FROM app_settings WHERE key IN (?,?)`, SettingExternalFileAccessPaused, SettingBackgroundProcessingPause)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return settings, err
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			continue
		}
		switch key {
		case SettingExternalFileAccessPaused:
			settings.ExternalFileAccessPaused = value
		case SettingBackgroundProcessingPause:
			settings.BackgroundProcessingPaused = value
		}
	}
	return settings, rows.Err()
}

func (d *DB) SetDebugSettings(ctx context.Context, settings DebugSettings) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertAppSetting(ctx, tx, SettingExternalFileAccessPaused, strconv.FormatBool(settings.ExternalFileAccessPaused)); err != nil {
		return err
	}
	if err := upsertAppSetting(ctx, tx, SettingBackgroundProcessingPause, strconv.FormatBool(settings.BackgroundProcessingPaused)); err != nil {
		return err
	}
	return tx.Commit()
}
