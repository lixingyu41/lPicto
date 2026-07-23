package db

import (
	"context"
	"database/sql"

	"lpicto/backend/internal/util"
)

const SystemTaskAIHealth = "ai_health_check"

type SystemTaskState struct {
	TaskKey        string
	Status         string
	LastStartedAt  *int64
	LastFinishedAt *int64
	LastSuccessAt  *int64
	Message        *string
}

type MediaJobTaskState struct {
	Status     string
	AssetID    *int64
	RelPath    *string
	StartedAt  *int64
	FinishedAt *int64
	Message    *string
}

type TaskFailure struct {
	AssetID int64
	RelPath string
	Reason  string
}

func (d *DB) BeginSystemTask(ctx context.Context, taskKey string) error {
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
INSERT INTO system_task_state(task_key,status,last_started_at,last_finished_at,message,updated_at)
VALUES (?, 'running', ?, NULL, NULL, ?)
ON CONFLICT(task_key) DO UPDATE SET
  status='running',last_started_at=excluded.last_started_at,last_finished_at=NULL,message=NULL,updated_at=excluded.updated_at`,
		taskKey, now, now)
	return err
}

func (d *DB) EnsureSystemTaskRunning(ctx context.Context, taskKey string) error {
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
INSERT INTO system_task_state(task_key,status,last_started_at,last_finished_at,message,updated_at)
VALUES (?, 'running', ?, NULL, NULL, ?)
ON CONFLICT(task_key) DO UPDATE SET
  status='running',
  last_started_at=CASE
    WHEN system_task_state.status='running' AND system_task_state.last_started_at IS NOT NULL
      THEN system_task_state.last_started_at
    ELSE excluded.last_started_at
  END,
  last_finished_at=NULL,
  message=NULL,
  updated_at=excluded.updated_at`,
		taskKey, now, now)
	return err
}

func (d *DB) FinishSystemTask(ctx context.Context, taskKey string, status string, message string) error {
	now := util.UnixNow()
	var successAt any
	if status == "success" {
		successAt = now
	}
	_, err := d.conn.ExecContext(ctx, `
INSERT INTO system_task_state(task_key,status,last_started_at,last_finished_at,last_success_at,message,updated_at)
VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?)
ON CONFLICT(task_key) DO UPDATE SET
  status=excluded.status,
  last_finished_at=excluded.last_finished_at,
  last_success_at=COALESCE(excluded.last_success_at,system_task_state.last_success_at),
  message=excluded.message,
  updated_at=excluded.updated_at`,
		taskKey, status, now, now, successAt, message, now)
	return err
}

func (d *DB) SystemTaskState(ctx context.Context, taskKey string) (*SystemTaskState, error) {
	row := d.conn.QueryRowContext(ctx, `
SELECT task_key,status,last_started_at,last_finished_at,last_success_at,message
FROM system_task_state WHERE task_key=?`, taskKey)
	var state SystemTaskState
	var started, finished, success sql.NullInt64
	var message sql.NullString
	if err := row.Scan(&state.TaskKey, &state.Status, &started, &finished, &success, &message); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	state.LastStartedAt = int64Ptr(started)
	state.LastFinishedAt = int64Ptr(finished)
	state.LastSuccessAt = int64Ptr(success)
	state.Message = stringPtr(message)
	return &state, nil
}

func (d *DB) LatestMediaJobTask(ctx context.Context, jobType string) (*MediaJobTaskState, error) {
	row := d.conn.QueryRowContext(ctx, `
SELECT mj.status,
  mj.asset_id,
  (SELECT fi.rel_path
   FROM file_instance fi
   WHERE fi.asset_id=mj.asset_id AND fi.missing=false
   ORDER BY fi.id
   LIMIT 1),
  EXTRACT(EPOCH FROM COALESCE(mj.started_at,mj.created_at))::BIGINT,
  CASE WHEN mj.finished_at IS NULL THEN NULL ELSE EXTRACT(EPOCH FROM mj.finished_at)::BIGINT END,
  mj.error_text
FROM media_job mj
WHERE mj.job_type=?
ORDER BY COALESCE(mj.finished_at,mj.started_at,mj.created_at) DESC,mj.id DESC
LIMIT 1`, jobType)
	var state MediaJobTaskState
	var assetID, started, finished sql.NullInt64
	var relPath, message sql.NullString
	if err := row.Scan(&state.Status, &assetID, &relPath, &started, &finished, &message); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	state.AssetID = int64Ptr(assetID)
	state.RelPath = stringPtr(relPath)
	state.StartedAt = int64Ptr(started)
	state.FinishedAt = int64Ptr(finished)
	state.Message = stringPtr(message)
	return &state, nil
}

func (d *DB) MediaJobFailures(ctx context.Context, jobType string, limit int) ([]TaskFailure, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := d.conn.QueryContext(ctx, `
SELECT mj.asset_id,
  COALESCE((SELECT fi.rel_path FROM file_instance fi WHERE fi.asset_id=mj.asset_id ORDER BY fi.missing,fi.id LIMIT 1), ''),
  COALESCE(mj.error_text, '')
FROM media_job mj
WHERE mj.job_type=? AND mj.status='error'
ORDER BY COALESCE(mj.finished_at,mj.started_at,mj.created_at) DESC,mj.id DESC
LIMIT ?`, jobType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	failures := make([]TaskFailure, 0)
	for rows.Next() {
		var failure TaskFailure
		if err := rows.Scan(&failure.AssetID, &failure.RelPath, &failure.Reason); err != nil {
			return nil, err
		}
		failures = append(failures, failure)
	}
	return failures, rows.Err()
}

func (d *DB) AIAnalysisFailures(ctx context.Context, limit int) ([]TaskFailure, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := d.conn.QueryContext(ctx, `
SELECT r.asset_id,
  COALESCE((SELECT fi.rel_path FROM file_instance fi WHERE fi.asset_id=r.asset_id ORDER BY fi.missing,fi.id LIMIT 1), ''),
  COALESCE(r.error_text, '')
FROM asset_ai_result r
JOIN media_asset ma ON ma.id=r.asset_id AND ma.deleted_at IS NULL
WHERE r.status='failed'
ORDER BY r.updated_at DESC,r.asset_id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	failures := make([]TaskFailure, 0)
	for rows.Next() {
		var failure TaskFailure
		if err := rows.Scan(&failure.AssetID, &failure.RelPath, &failure.Reason); err != nil {
			return nil, err
		}
		failures = append(failures, failure)
	}
	return failures, rows.Err()
}
