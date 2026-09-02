package db

import (
	"context"
	"strconv"
	"strings"
)

const (
	SettingAIAutoAnalyze  = "ai_auto_analyze"
	SettingAIManualRun    = "ai_manual_run"
	SettingAIComputeMode  = "ai_compute_mode"
	SettingAIExternalHost = "ai_external_host"
	SettingAIExternalPort = "ai_external_port"

	AIComputeModeUbuntu   = "ubuntu"
	AIComputeModeExternal = "external"
	AIComputeModeDual     = "dual"
	DefaultAIExternalPort = 18090
)

type AISettings struct {
	AutoAnalyze  bool   `json:"autoAnalyze"`
	ManualRun    bool   `json:"manualRun"`
	ComputeMode  string `json:"computeMode"`
	ExternalHost string `json:"externalHost"`
	ExternalPort int    `json:"externalPort"`
}

func (d *DB) GetAISettings(ctx context.Context) (AISettings, error) {
	settings := AISettings{AutoAnalyze: true, ComputeMode: AIComputeModeUbuntu, ExternalPort: DefaultAIExternalPort}
	rows, err := d.conn.QueryContext(ctx, `SELECT key,value FROM app_settings WHERE key IN (?,?,?,?,?)`, SettingAIAutoAnalyze, SettingAIManualRun, SettingAIComputeMode, SettingAIExternalHost, SettingAIExternalPort)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return settings, err
		}
		switch key {
		case SettingAIAutoAnalyze:
			if value, parseErr := strconv.ParseBool(raw); parseErr == nil {
				settings.AutoAnalyze = value
			}
		case SettingAIManualRun:
			if value, parseErr := strconv.ParseBool(raw); parseErr == nil {
				settings.ManualRun = value
			}
		case SettingAIComputeMode:
			if validAIComputeMode(raw) {
				settings.ComputeMode = raw
			}
		case SettingAIExternalHost:
			settings.ExternalHost = strings.TrimSpace(raw)
		case SettingAIExternalPort:
			if port, parseErr := strconv.Atoi(raw); parseErr == nil && port >= 1 && port <= 65535 {
				settings.ExternalPort = port
			}
		}
	}
	return settings, rows.Err()
}

func (d *DB) SetAIComputeSettings(ctx context.Context, mode, host string, port int) (AISettings, error) {
	if !validAIComputeMode(mode) {
		mode = AIComputeModeUbuntu
	}
	if port < 1 || port > 65535 {
		port = DefaultAIExternalPort
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return AISettings{}, err
	}
	defer tx.Rollback()
	values := map[string]string{
		SettingAIComputeMode:  mode,
		SettingAIExternalHost: strings.TrimSpace(host),
		SettingAIExternalPort: strconv.Itoa(port),
	}
	for key, value := range values {
		if err := upsertAppSetting(ctx, tx, key, value); err != nil {
			return AISettings{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AISettings{}, err
	}
	return d.GetAISettings(ctx)
}

func (d *DB) SetAISettings(ctx context.Context, autoAnalyze bool, mode, host string, port int) (AISettings, error) {
	if !validAIComputeMode(mode) {
		mode = AIComputeModeUbuntu
	}
	if port < 1 || port > 65535 {
		port = DefaultAIExternalPort
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return AISettings{}, err
	}
	defer tx.Rollback()
	values := map[string]string{
		SettingAIAutoAnalyze:  strconv.FormatBool(autoAnalyze),
		SettingAIManualRun:    "false",
		SettingAIComputeMode:  mode,
		SettingAIExternalHost: strings.TrimSpace(host),
		SettingAIExternalPort: strconv.Itoa(port),
	}
	for key, value := range values {
		if err := upsertAppSetting(ctx, tx, key, value); err != nil {
			return AISettings{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AISettings{}, err
	}
	return d.GetAISettings(ctx)
}

func validAIComputeMode(mode string) bool {
	switch mode {
	case AIComputeModeUbuntu, AIComputeModeExternal, AIComputeModeDual:
		return true
	default:
		return false
	}
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
