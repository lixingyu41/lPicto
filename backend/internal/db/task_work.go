package db

import (
	"context"
	"fmt"
	"strings"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/util"
)

type taskWorkSpec struct {
	field     string
	mediaType string
	taskType  string
}

func taskWorkDefinition(taskType string) (taskWorkSpec, bool) {
	switch taskType {
	case "thumb":
		return taskWorkSpec{field: "thumb_status", taskType: "thumb"}, true
	case "preview":
		return taskWorkSpec{field: "preview_status", mediaType: model.MediaTypeImage, taskType: "preview"}, true
	case "video_poster":
		return taskWorkSpec{field: "video_poster_status", mediaType: model.MediaTypeVideo, taskType: "video_poster"}, true
	default:
		return taskWorkSpec{}, false
	}
}

func (d *DB) WorkProgress(ctx context.Context, taskType string, roots []string) (WorkStatusCounts, error) {
	if taskType == "storyboard" {
		return d.storyboardProgress(ctx, roots)
	}
	spec, ok := taskWorkDefinition(taskType)
	if !ok {
		return WorkStatusCounts{}, fmt.Errorf("unsupported task type %s", taskType)
	}
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return WorkStatusCounts{}, err
	}
	return d.statusCounts(ctx, spec.field, spec.mediaType, where, args)
}

func (d *DB) WorkItemsForRoots(ctx context.Context, taskType string, roots []string) ([]WorkItem, error) {
	if taskType == "storyboard" {
		return d.storyboardWorkItemsForRoots(ctx, roots, []string{model.StatusPending, model.StatusProcessing})
	}
	return d.workItemsForRoots(ctx, taskType, roots, []string{model.StatusPending, model.StatusProcessing})
}

// ContinueWorkForRoots restores failed work to pending and returns every
// unfinished item in the requested scope. The queue manager deduplicates by
// task type and asset ID, so stopped/processing rows can be submitted safely.
func (d *DB) ContinueWorkForRoots(ctx context.Context, taskType string, roots []string) ([]WorkItem, error) {
	if _, err := d.RetryFailedWorkForRoots(ctx, taskType, roots); err != nil {
		return nil, err
	}
	return d.WorkItemsForRoots(ctx, taskType, roots)
}

func (d *DB) workItemsForRoots(ctx context.Context, taskType string, roots []string, statuses []string) ([]WorkItem, error) {
	spec, ok := taskWorkDefinition(taskType)
	if !ok {
		return nil, fmt.Errorf("unsupported task type %s", taskType)
	}
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return nil, err
	}
	if spec.mediaType != "" {
		where += " AND media_type = ?"
		args = append(args, spec.mediaType)
	}
	if len(statuses) == 0 {
		return []WorkItem{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(statuses)), ",")
	query := fmt.Sprintf(`SELECT id,media_type FROM assets WHERE %s AND %s IN (%s) ORDER BY timeline_at DESC, id DESC`, where, spec.field, placeholders)
	for _, status := range statuses {
		args = append(args, status)
	}
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkItem, 0)
	for rows.Next() {
		var id int64
		var mediaType string
		if err := rows.Scan(&id, &mediaType); err != nil {
			return nil, err
		}
		itemType := spec.taskType
		if taskType == "thumb" && mediaType == model.MediaTypeVideo {
			itemType = "video_poster"
		}
		items = append(items, WorkItem{Type: itemType, AssetID: id})
	}
	return items, rows.Err()
}

func (d *DB) RetryFailedWorkForRoots(ctx context.Context, taskType string, roots []string) ([]WorkItem, error) {
	if taskType == "storyboard" {
		return d.retryFailedStoryboardWorkForRoots(ctx, roots)
	}
	spec, ok := taskWorkDefinition(taskType)
	if !ok {
		return nil, fmt.Errorf("unsupported task type %s", taskType)
	}
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return nil, err
	}
	if spec.mediaType != "" {
		where += " AND media_type = ?"
		args = append(args, spec.mediaType)
	}
	queryArgs := []any{model.StatusPending, unixTime(util.UnixNow())}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, model.StatusError)
	rows, err := d.conn.QueryContext(ctx, fmt.Sprintf(`
WITH reset AS (
  UPDATE media_asset SET %s=?,error_text=NULL,updated_at=?
  WHERE id IN (SELECT id FROM assets WHERE %s) AND %s=?
  RETURNING id
)
SELECT reset.id,assets.media_type FROM reset JOIN assets ON assets.id=reset.id`, spec.field, where, spec.field), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkItem, 0)
	for rows.Next() {
		var id int64
		var mediaType string
		if err := rows.Scan(&id, &mediaType); err != nil {
			return nil, err
		}
		itemType := spec.taskType
		if taskType == "thumb" && mediaType == model.MediaTypeVideo {
			itemType = "video_poster"
		}
		items = append(items, WorkItem{Type: itemType, AssetID: id})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, item := range items {
		_, _ = d.conn.ExecContext(ctx, `UPDATE media_job SET status=?,error_text=NULL,started_at=NULL,finished_at=NULL WHERE asset_id=? AND job_type=?`, model.StatusPending, item.AssetID, item.Type)
	}
	return items, nil
}

func (d *DB) ResetWorkForRoots(ctx context.Context, taskType string, roots []string) (int, error) {
	if taskType == "storyboard" {
		return d.resetStoryboardWorkForRoots(ctx, roots)
	}
	spec, ok := taskWorkDefinition(taskType)
	if !ok {
		return 0, fmt.Errorf("unsupported task type %s", taskType)
	}
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return 0, err
	}
	if spec.mediaType != "" {
		where += " AND media_type = ?"
		args = append(args, spec.mediaType)
	}
	now := util.UnixNow()
	queryArgs := []any{model.StatusPending, unixTime(now)}
	queryArgs = append(queryArgs, args...)
	readyField := readyColumnForStatus(spec.field)
	setClause := fmt.Sprintf("%s=?, error_text=NULL, updated_at=?", spec.field)
	if readyField != "" {
		setClause += fmt.Sprintf(", %s=false", readyField)
	}
	query := fmt.Sprintf(`UPDATE media_asset SET %s WHERE id IN (SELECT id FROM assets WHERE %s)`, setClause, where)
	result, err := d.conn.ExecContext(ctx, query, queryArgs...)
	if err != nil {
		return 0, err
	}
	_, _ = d.conn.ExecContext(ctx, `UPDATE media_job SET status=?, error_text=NULL, started_at=NULL, finished_at=NULL WHERE job_type=? AND asset_id IN (SELECT id FROM assets WHERE `+where+`)`, append([]any{model.StatusPending, spec.taskType}, args...)...)
	count, _ := result.RowsAffected()
	return int(count), nil
}

func (d *DB) RequeueProcessingWork(ctx context.Context, taskType string) error {
	if taskType == "storyboard" {
		_, err := d.conn.ExecContext(ctx, `UPDATE media_job SET status=?,finished_at=NULL WHERE job_type='storyboard' AND status=?`, model.StatusPending, model.StatusProcessing)
		return err
	}
	spec, ok := taskWorkDefinition(taskType)
	if !ok {
		return fmt.Errorf("unsupported task type %s", taskType)
	}
	_, err := d.conn.ExecContext(ctx, fmt.Sprintf(`UPDATE media_asset SET %s=? WHERE %s=?`, spec.field, spec.field), model.StatusPending, model.StatusProcessing)
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, `UPDATE media_job SET status=?, finished_at=NULL WHERE job_type=? AND status=?`, model.StatusPending, spec.taskType, model.StatusProcessing)
	return err
}

func (d *DB) MetadataProgress(ctx context.Context, roots []string) (WorkStatusCounts, error) {
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return WorkStatusCounts{}, err
	}
	var counts WorkStatusCounts
	err = d.conn.QueryRowContext(ctx, `
SELECT COUNT(*),
  COALESCE(SUM(CASE WHEN COALESCE(mj.status,'pending')='ready' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN COALESCE(mj.status,'pending')='pending' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN mj.status='processing' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN mj.status='error' THEN 1 ELSE 0 END),0), 0
FROM assets a LEFT JOIN media_job mj ON mj.asset_id=a.id AND mj.job_type='metadata'
WHERE `+strings.ReplaceAll(where, "rel_path", "a.rel_path"), args...).Scan(&counts.Total, &counts.Ready, &counts.Pending, &counts.Processing, &counts.Error, &counts.NotRequired)
	return counts, err
}

func (d *DB) MetadataWorkPathsForRoots(ctx context.Context, roots []string) ([]string, error) {
	if _, err := d.RetryFailedMetadataPathsForRoots(ctx, roots); err != nil {
		return nil, err
	}
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return nil, err
	}
	rows, err := d.conn.QueryContext(ctx, `
SELECT a.rel_path FROM assets a
LEFT JOIN media_job mj ON mj.asset_id=a.id AND mj.job_type='metadata'
WHERE `+strings.ReplaceAll(where, "rel_path", "a.rel_path")+`
  AND COALESCE(mj.status,'pending') IN ('pending','processing')
ORDER BY a.timeline_at DESC,a.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		paths = append(paths, rel)
	}
	return paths, rows.Err()
}

// RepairVideoDisplayMetadataDimensions corrects old video records whose stored
// dimensions ignored a quarter-turn display matrix. The original ffprobe JSON
// is sufficient, so this does not read source media or rebuild cached posters.
func (d *DB) RepairVideoDisplayMetadataDimensions(ctx context.Context) (int64, error) {
	result, err := d.conn.ExecContext(ctx, `
WITH video_streams AS (
  SELECT ma.id,ma.width AS display_width,ma.height AS display_height,stream
  FROM media_asset ma
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(ma.metadata_json->'streams','[]'::jsonb)) AS stream
  WHERE ma.media_type=2 AND ma.deleted_at IS NULL AND stream->>'codec_type'='video'
), rotated_streams AS (
  SELECT id,display_width,display_height,stream,
    NULLIF(COALESCE(
      (SELECT side_data->>'rotation'
       FROM jsonb_array_elements(COALESCE(stream->'side_data_list','[]'::jsonb)) AS side_data
       WHERE side_data->'rotation' IS NOT NULL LIMIT 1),
      stream->'tags'->>'rotate'
    ),'')::double precision AS rotation
  FROM video_streams
), repairs AS (
  SELECT DISTINCT ON (rs.id) rs.id,
    (rs.stream->>'height')::int AS target_width,
    (rs.stream->>'width')::int AS target_height
  FROM rotated_streams rs
  WHERE rs.rotation IS NOT NULL
    AND ((ROUND(rs.rotation)::int % 360 + 360) % 360) IN (90,270)
    AND NULLIF(rs.stream->>'width','') IS NOT NULL
    AND NULLIF(rs.stream->>'height','') IS NOT NULL
    AND EXISTS (SELECT 1 FROM file_instance fi WHERE fi.asset_id=rs.id AND fi.missing=false)
)
UPDATE media_asset ma SET
  width=repairs.target_width,
  height=repairs.target_height,
  aspect_ratio=repairs.target_width::double precision / NULLIF(repairs.target_height,0),
  updated_at=now()
FROM repairs
WHERE ma.id=repairs.id
  AND (ma.width IS DISTINCT FROM repairs.target_width OR ma.height IS DISTINCT FROM repairs.target_height)`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *DB) RetryFailedMetadataPathsForRoots(ctx context.Context, roots []string) ([]string, error) {
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return nil, err
	}
	rows, err := d.conn.QueryContext(ctx, `
WITH reset AS (
  UPDATE media_job mj SET status='pending',error_text=NULL,started_at=NULL,finished_at=NULL
  FROM assets a
  WHERE mj.asset_id=a.id AND mj.job_type='metadata' AND mj.status='error' AND `+strings.ReplaceAll(where, "rel_path", "a.rel_path")+`
  RETURNING a.rel_path
)
SELECT rel_path FROM reset ORDER BY rel_path`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		paths = append(paths, rel)
	}
	return paths, rows.Err()
}

func (d *DB) ResetMetadataForRoots(ctx context.Context, roots []string) (int, error) {
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return 0, err
	}
	result, err := d.conn.ExecContext(ctx, `
INSERT INTO media_job(asset_id,job_type,status,error_text,started_at,finished_at)
SELECT id,'metadata','pending',NULL,NULL,NULL FROM assets WHERE `+where+`
ON CONFLICT(asset_id,job_type) DO UPDATE SET status='pending',error_text=NULL,started_at=NULL,finished_at=NULL`, args...)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

func (d *DB) SetMetadataJobStatus(ctx context.Context, assetID int64, status string, message *string) error {
	return d.upsertMediaJob(ctx, assetID, "metadata_status", status, message, util.UnixNow())
}

func (d *DB) SetStoryboardJobStatus(ctx context.Context, assetID int64, status string, message *string) error {
	return d.upsertMediaJob(ctx, assetID, "storyboard_status", status, message, util.UnixNow())
}

func (d *DB) StoryboardJobStatus(ctx context.Context, assetID int64) (string, error) {
	var status string
	err := d.conn.QueryRowContext(ctx, `SELECT status FROM media_job WHERE asset_id=? AND job_type='storyboard'`, assetID).Scan(&status)
	return status, err
}

// EnsureStoryboardPending creates missing historical work and optionally resets
// a changed media version. It returns true only when the item should be queued.
func (d *DB) EnsureStoryboardPending(ctx context.Context, assetID int64, reset bool) (bool, error) {
	if reset {
		if err := d.SetStoryboardJobStatus(ctx, assetID, model.StatusPending, nil); err != nil {
			return false, err
		}
		return true, nil
	}
	result, err := d.conn.ExecContext(ctx, `
INSERT INTO media_job(asset_id,job_type,status,error_text,started_at,finished_at)
VALUES(?,'storyboard','pending',NULL,NULL,NULL)
ON CONFLICT(asset_id,job_type) DO NOTHING`, assetID)
	if err != nil {
		return false, err
	}
	if inserted, _ := result.RowsAffected(); inserted > 0 {
		return true, nil
	}
	status, err := d.StoryboardJobStatus(ctx, assetID)
	return status == model.StatusPending || status == model.StatusProcessing, err
}

func (d *DB) storyboardProgress(ctx context.Context, roots []string) (WorkStatusCounts, error) {
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return WorkStatusCounts{}, err
	}
	var counts WorkStatusCounts
	err = d.conn.QueryRowContext(ctx, `
SELECT COUNT(*),
  COALESCE(SUM(CASE WHEN mj.status='ready' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN COALESCE(mj.status,'pending')='pending' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN mj.status='processing' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN mj.status='error' THEN 1 ELSE 0 END),0),0
FROM assets a LEFT JOIN media_job mj ON mj.asset_id=a.id AND mj.job_type='storyboard'
WHERE `+strings.ReplaceAll(where, "rel_path", "a.rel_path")+` AND a.media_type=?`, append(args, model.MediaTypeVideo)...).Scan(
		&counts.Total, &counts.Ready, &counts.Pending, &counts.Processing, &counts.Error, &counts.NotRequired)
	return counts, err
}

func (d *DB) storyboardWorkItemsForRoots(ctx context.Context, roots []string, statuses []string) ([]WorkItem, error) {
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return nil, err
	}
	if len(statuses) == 0 {
		return []WorkItem{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(statuses)), ",")
	queryArgs := append([]any(nil), args...)
	queryArgs = append(queryArgs, model.MediaTypeVideo)
	for _, status := range statuses {
		queryArgs = append(queryArgs, status)
	}
	rows, err := d.conn.QueryContext(ctx, `
SELECT a.id FROM assets a LEFT JOIN media_job mj ON mj.asset_id=a.id AND mj.job_type='storyboard'
WHERE `+strings.ReplaceAll(where, "rel_path", "a.rel_path")+` AND a.media_type=?
  AND COALESCE(mj.status,'pending') IN (`+placeholders+`)
ORDER BY a.timeline_at DESC,a.id DESC`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkItem, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, WorkItem{Type: "storyboard", AssetID: id})
	}
	return items, rows.Err()
}

func (d *DB) retryFailedStoryboardWorkForRoots(ctx context.Context, roots []string) ([]WorkItem, error) {
	items, err := d.storyboardWorkItemsForRoots(ctx, roots, []string{model.StatusError})
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := d.SetStoryboardJobStatus(ctx, item.AssetID, model.StatusPending, nil); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (d *DB) resetStoryboardWorkForRoots(ctx context.Context, roots []string) (int, error) {
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return 0, err
	}
	result, err := d.conn.ExecContext(ctx, `
INSERT INTO media_job(asset_id,job_type,status,error_text,started_at,finished_at)
SELECT id,'storyboard','pending',NULL,NULL,NULL FROM assets
WHERE `+where+` AND media_type=?
ON CONFLICT(asset_id,job_type) DO UPDATE SET status='pending',error_text=NULL,started_at=NULL,finished_at=NULL`, append(args, model.MediaTypeVideo)...)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

func (d *DB) AssetCacheKeys(ctx context.Context) (map[string]struct{}, error) {
	rows, err := d.conn.QueryContext(ctx, `SELECT cache_key FROM media_asset WHERE deleted_at IS NULL AND cache_key<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := map[string]struct{}{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys[key] = struct{}{}
	}
	return keys, rows.Err()
}

func (d *DB) SourceHealthSampleForRoots(ctx context.Context, roots []string) (string, error) {
	where, args, err := taskRootsWhere(roots)
	if err != nil {
		return "", err
	}
	var rel string
	err = d.conn.QueryRowContext(ctx, `SELECT rel_path FROM assets WHERE `+where+` ORDER BY id LIMIT 1`, args...).Scan(&rel)
	return rel, err
}

func taskRootsWhere(roots []string) (string, []any, error) {
	if len(roots) == 0 {
		return "deleted_at IS NULL", nil, nil
	}
	return assetRootsWhere(roots)
}
