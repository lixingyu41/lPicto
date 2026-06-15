package db

import (
	"context"
	"database/sql"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/util"
)

type ScanFinish struct {
	TotalSeen     int
	AssetsAdded   int
	AssetsUpdated int
	AssetsDeleted int
	Errors        int
	LastError     *string
	Status        string
}

func (d *DB) StartScanRun(ctx context.Context) (int64, error) {
	now := util.UnixNow()
	result, err := d.conn.ExecContext(ctx, `INSERT INTO scan_runs (status, started_at) VALUES ('running', ?)`, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (d *DB) FinishScanRun(ctx context.Context, id int64, result ScanFinish) error {
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
UPDATE scan_runs SET status = ?, finished_at = ?, total_seen = ?, assets_added = ?, assets_updated = ?,
assets_deleted = ?, errors = ?, last_error = ? WHERE id = ?`,
		result.Status, now, result.TotalSeen, result.AssetsAdded, result.AssetsUpdated,
		result.AssetsDeleted, result.Errors, nullString(result.LastError), id)
	return err
}

func (d *DB) MarkInterruptedScanRuns(ctx context.Context) error {
	_, err := d.conn.ExecContext(ctx, `
UPDATE scan_runs
SET status = 'interrupted',
    finished_at = ?,
    last_error = '扫描已中断'
WHERE status = 'running'`,
		util.UnixNow())
	return err
}

func (d *DB) RecentScanRuns(ctx context.Context, page int, pageSize int) (model.Page[model.ScanRun], error) {
	limit := pageSize + 1
	offset := (page - 1) * pageSize
	rows, err := d.conn.QueryContext(ctx, scanRunSelectSQL()+` ORDER BY started_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return model.Page[model.ScanRun]{}, err
	}
	defer rows.Close()
	items, err := scanRunRows(rows)
	if err != nil {
		return model.Page[model.ScanRun]{}, err
	}
	hasMore := len(items) > pageSize
	if hasMore {
		items = items[:pageSize]
	}
	return model.Page[model.ScanRun]{Items: items, Page: page, PageSize: pageSize, HasMore: hasMore}, nil
}

func (d *DB) LastScanRun(ctx context.Context) (*model.ScanRun, error) {
	row := d.conn.QueryRowContext(ctx, scanRunSelectSQL()+` ORDER BY started_at DESC, id DESC LIMIT 1`)
	run, err := scanRun(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func scanRunSelectSQL() string {
	return `SELECT id, status, started_at, finished_at, total_seen, assets_added, assets_updated, assets_deleted, errors, last_error FROM scan_runs`
}

func scanRun(row interface{ Scan(dest ...any) error }) (model.ScanRun, error) {
	var run model.ScanRun
	var finished sql.NullInt64
	var lastError sql.NullString
	err := row.Scan(&run.ID, &run.Status, &run.StartedAt, &finished, &run.TotalSeen, &run.AssetsAdded, &run.AssetsUpdated, &run.AssetsDeleted, &run.Errors, &lastError)
	if err != nil {
		return model.ScanRun{}, err
	}
	run.FinishedAt = int64Ptr(finished)
	run.LastError = stringPtr(lastError)
	return run, nil
}

func scanRunRows(rows *sql.Rows) ([]model.ScanRun, error) {
	var runs []model.ScanRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
