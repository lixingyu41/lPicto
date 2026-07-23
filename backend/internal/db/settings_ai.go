package db

import (
	"context"
	"strconv"
)

const (
	SettingAIAutoAnalyze = "ai_auto_analyze"
	SettingAIManualRun   = "ai_manual_run"
)

type AISettings struct {
	AutoAnalyze bool `json:"autoAnalyze"`
	ManualRun   bool `json:"manualRun"`
}

func (d *DB) GetAISettings(ctx context.Context) (AISettings, error) {
	settings := AISettings{AutoAnalyze: true}
	rows, err := d.conn.QueryContext(ctx, `SELECT key,value FROM app_settings WHERE key IN (?,?)`, SettingAIAutoAnalyze, SettingAIManualRun)
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
		case SettingAIAutoAnalyze:
			settings.AutoAnalyze = value
		case SettingAIManualRun:
			settings.ManualRun = value
		}
	}
	return settings, rows.Err()
}

func (d *DB) SetAIAutoAnalyze(ctx context.Context, enabled bool) (AISettings, error) {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return AISettings{}, err
	}
	defer tx.Rollback()
	if err := upsertAppSetting(ctx, tx, SettingAIAutoAnalyze, strconv.FormatBool(enabled)); err != nil {
		return AISettings{}, err
	}
	if err := upsertAppSetting(ctx, tx, SettingAIManualRun, "false"); err != nil {
		return AISettings{}, err
	}
	if err := tx.Commit(); err != nil {
		return AISettings{}, err
	}
	return d.GetAISettings(ctx)
}

func (d *DB) SetAIManualRun(ctx context.Context, enabled bool) (AISettings, error) {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return AISettings{}, err
	}
	defer tx.Rollback()
	if err := upsertAppSetting(ctx, tx, SettingAIManualRun, strconv.FormatBool(enabled)); err != nil {
		return AISettings{}, err
	}
	if err := tx.Commit(); err != nil {
		return AISettings{}, err
	}
	return d.GetAISettings(ctx)
}

func (d *DB) AIExecutionEnabled(ctx context.Context) (bool, error) {
	settings, err := d.GetAISettings(ctx)
	return settings.AutoAnalyze || settings.ManualRun, err
}
