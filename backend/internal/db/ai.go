package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// AIMaxAttempts is the initial attempt plus one automatic retry. Manual retry
// resets the counter and starts a new two-attempt cycle.
const AIMaxAttempts = 2

type AITag struct {
	Tag        string  `json:"tag"`
	Confidence float64 `json:"confidence"`
}

type AIColor struct {
	Hex    string  `json:"hex"`
	Weight float64 `json:"weight"`
}

type AIResult struct {
	AssetID                 int64           `json:"assetId"`
	InputCacheKey           string          `json:"inputCacheKey"`
	Status                  string          `json:"status"`
	Description             string          `json:"description"`
	Tags                    []AITag         `json:"tags"`
	Palette                 []AIColor       `json:"palette"`
	TagModel                string          `json:"tagModel"`
	TagModelVersion         string          `json:"tagModelVersion"`
	DescriptionModel        string          `json:"descriptionModel"`
	DescriptionModelVersion string          `json:"descriptionModelVersion"`
	TaxonomyVersion         string          `json:"taxonomyVersion"`
	SampledFrames           json.RawMessage `json:"sampledFrames"`
	Attempts                int             `json:"attempts"`
	Error                   string          `json:"error,omitempty"`
	UpdatedAt               time.Time       `json:"updatedAt"`
}

type AISummary struct {
	Description string
	Tags        []AITag
	Palette     []AIColor
}

type AIBackfillItem struct {
	AssetID  int64
	CacheKey string
	RelPath  string
}

type AITagSummary struct {
	Tag         string `json:"tag"`
	Count       int64  `json:"count"`
	AICount     int64  `json:"aiCount"`
	ManualCount int64  `json:"manualCount"`
	ManualAdded bool   `json:"manualAdded"`
	ManualTagID *int64 `json:"manualTagId,omitempty"`
}

type AIStatus struct {
	Total      int64   `json:"total"`
	Pending    int64   `json:"pending"`
	Processing int64   `json:"processing"`
	Ready      int64   `json:"ready"`
	Failed     int64   `json:"failed"`
	Stale      int64   `json:"stale"`
	Queued     int     `json:"queued"`
	Active     int     `json:"active"`
	PerMinute  float64 `json:"perMinute"`
	ETASeconds *int64  `json:"etaSeconds"`
}

type AIActivity struct {
	LastStartedAt  *int64
	LastFinishedAt *int64
}

func (d *DB) EnsureAIQueued(ctx context.Context, assetID int64, cacheKey string, force bool) error {
	if force {
		_, err := d.conn.ExecContext(ctx, `INSERT INTO asset_ai_result(asset_id,input_cache_key,status,attempts,error_text,updated_at)
VALUES(?,?,'pending',0,NULL,now()) ON CONFLICT(asset_id) DO UPDATE SET input_cache_key=excluded.input_cache_key,status='pending',attempts=0,error_text=NULL,updated_at=now()`, assetID, cacheKey)
		return err
	}
	_, err := d.conn.ExecContext(ctx, `INSERT INTO asset_ai_result(asset_id,input_cache_key,status,updated_at)
VALUES(?,?,'pending',now()) ON CONFLICT(asset_id) DO UPDATE SET input_cache_key=excluded.input_cache_key,status='pending',attempts=0,error_text=NULL,updated_at=now()
WHERE asset_ai_result.input_cache_key<>excluded.input_cache_key`, assetID, cacheKey)
	return err
}

func (d *DB) MarkAIProcessing(ctx context.Context, assetID int64, cacheKey string) (int, error) {
	var attempts int
	err := d.conn.QueryRowContext(ctx, `UPDATE asset_ai_result SET status='processing',attempts=attempts+1,started_at=now(),error_text=NULL,updated_at=now()
WHERE asset_id=? AND input_cache_key=? RETURNING attempts`, assetID, cacheKey).Scan(&attempts)
	return attempts, err
}

func (d *DB) SaveAIResult(ctx context.Context, assetID int64, cacheKey, description, tagModel, tagVersion, descriptionModel, descriptionVersion, taxonomyVersion string, frames json.RawMessage, tags []AITag, palette []AIColor) error {
	tx, err := d.raw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	paletteJSON, err := json.Marshal(palette)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, rebindPostgres(`UPDATE asset_ai_result SET status='ready',description=?,tag_model=?,tag_model_version=?,description_model=?,description_model_version=?,taxonomy_version=?,sampled_frames=?::jsonb,palette=?::jsonb,error_text=NULL,finished_at=now(),updated_at=now() WHERE asset_id=? AND input_cache_key=?`), description, tagModel, tagVersion, descriptionModel, descriptionVersion, taxonomyVersion, string(frames), string(paletteJSON), assetID, cacheKey)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, rebindPostgres(`DELETE FROM asset_ai_tag WHERE asset_id=?`), assetID); err != nil {
		return err
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag.Tag) == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, rebindPostgres(`INSERT INTO asset_ai_tag(asset_id,tag,confidence) VALUES(?,?,?)`), assetID, strings.TrimSpace(tag.Tag), tag.Confidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) MarkAIFailed(ctx context.Context, assetID int64, cacheKey, message string) (bool, error) {
	var attempts int
	err := d.conn.QueryRowContext(ctx, `UPDATE asset_ai_result SET status='failed',error_text=?,finished_at=now(),updated_at=now() WHERE asset_id=? AND input_cache_key=? RETURNING attempts`, message, assetID, cacheKey).Scan(&attempts)
	return attempts < AIMaxAttempts, err
}

func (d *DB) RequeueAIInterrupted(ctx context.Context, assetID int64, cacheKey string) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE asset_ai_result
SET status='pending',attempts=GREATEST(attempts-1,0),started_at=NULL,finished_at=NULL,error_text=NULL,updated_at=now()
WHERE asset_id=? AND input_cache_key=? AND status='processing'`, assetID, cacheKey)
	return err
}

func (d *DB) GetAIResult(ctx context.Context, assetID int64) (AIResult, error) {
	var out AIResult
	var description, errorText sql.NullString
	var frames, palette []byte
	err := d.conn.QueryRowContext(ctx, `SELECT r.asset_id,r.input_cache_key,r.status,r.description,r.tag_model,r.tag_model_version,r.description_model,r.description_model_version,r.taxonomy_version,r.sampled_frames,r.palette,r.attempts,r.error_text,r.updated_at
FROM asset_ai_result r JOIN media_asset ma ON ma.id=r.asset_id WHERE r.asset_id=? AND ma.deleted_at IS NULL`, assetID).Scan(&out.AssetID, &out.InputCacheKey, &out.Status, &description, &out.TagModel, &out.TagModelVersion, &out.DescriptionModel, &out.DescriptionModelVersion, &out.TaxonomyVersion, &frames, &palette, &out.Attempts, &errorText, &out.UpdatedAt)
	if err != nil {
		return AIResult{}, err
	}
	out.Description = description.String
	out.Error = errorText.String
	out.SampledFrames = append(json.RawMessage(nil), frames...)
	_ = json.Unmarshal(palette, &out.Palette)
	rows, err := d.conn.QueryContext(ctx, `SELECT tag,confidence FROM asset_ai_tag WHERE asset_id=? ORDER BY confidence DESC,tag`, assetID)
	if err != nil {
		return AIResult{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var tag AITag
		if err := rows.Scan(&tag.Tag, &tag.Confidence); err != nil {
			return AIResult{}, err
		}
		out.Tags = append(out.Tags, tag)
	}
	return out, rows.Err()
}

func (d *DB) AssetAISummaries(ctx context.Context, assetIDs []int64) (map[int64]AISummary, error) {
	result := make(map[int64]AISummary, len(assetIDs))
	if len(assetIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(assetIDs))
	args := make([]any, len(assetIDs))
	for i, id := range assetIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `SELECT r.asset_id,COALESCE(r.description,''),r.palette,t.tag,t.confidence
FROM asset_ai_result r
JOIN media_asset ma ON ma.id=r.asset_id AND ma.cache_key=r.input_cache_key
LEFT JOIN asset_ai_tag t ON t.asset_id=r.asset_id
WHERE r.status='ready' AND r.asset_id IN (` + strings.Join(placeholders, ",") + `)
ORDER BY r.asset_id,t.confidence DESC,t.tag`
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID int64
		var description string
		var palette []byte
		var tag sql.NullString
		var confidence sql.NullFloat64
		if err := rows.Scan(&assetID, &description, &palette, &tag, &confidence); err != nil {
			return nil, err
		}
		summary := result[assetID]
		summary.Description = description
		if len(summary.Palette) == 0 {
			_ = json.Unmarshal(palette, &summary.Palette)
		}
		if tag.Valid {
			summary.Tags = append(summary.Tags, AITag{Tag: tag.String, Confidence: confidence.Float64})
		}
		result[assetID] = summary
	}
	return result, rows.Err()
}

func (d *DB) AIBackfillBatch(ctx context.Context, limit int) ([]AIBackfillItem, error) {
	var pilotImages, pilotVideos, totalImages, totalVideos int
	if err := d.conn.QueryRowContext(ctx, `SELECT
COUNT(*) FILTER(WHERE ma.media_type=1 AND r.input_cache_key=ma.cache_key AND r.status='ready'),
COUNT(*) FILTER(WHERE ma.media_type=2 AND r.input_cache_key=ma.cache_key AND r.status='ready'),
COUNT(*) FILTER(WHERE ma.media_type=1),COUNT(*) FILTER(WHERE ma.media_type=2)
FROM media_asset ma LEFT JOIN asset_ai_result r ON r.asset_id=ma.id
WHERE ma.deleted_at IS NULL AND EXISTS (SELECT 1 FROM file_instance fi WHERE fi.asset_id=ma.id AND fi.missing=false)`).Scan(&pilotImages, &pilotVideos, &totalImages, &totalVideos); err != nil {
		return nil, err
	}
	pilotImageTarget := min(80, totalImages)
	pilotVideoTarget := min(20, totalVideos)
	if pilotImages < pilotImageTarget || pilotVideos < pilotVideoTarget {
		missingImages := max(0, pilotImageTarget-pilotImages)
		missingVideos := max(0, pilotVideoTarget-pilotVideos)
		rows, err := d.conn.QueryContext(ctx, `(SELECT ma.id,ma.cache_key,fi.rel_path,ma.sort_time FROM media_asset ma
JOIN LATERAL (SELECT rel_path FROM file_instance WHERE asset_id=ma.id AND missing=false ORDER BY last_seen_at DESC,id DESC LIMIT 1) fi ON true
LEFT JOIN asset_ai_result r ON r.asset_id=ma.id WHERE ma.deleted_at IS NULL AND ma.media_type=1 AND (r.asset_id IS NULL OR r.input_cache_key<>ma.cache_key OR r.status='pending') ORDER BY ma.sort_time DESC,ma.id DESC LIMIT ?)
UNION ALL (SELECT ma.id,ma.cache_key,fi.rel_path,ma.sort_time FROM media_asset ma
JOIN LATERAL (SELECT rel_path FROM file_instance WHERE asset_id=ma.id AND missing=false ORDER BY last_seen_at DESC,id DESC LIMIT 1) fi ON true
LEFT JOIN asset_ai_result r ON r.asset_id=ma.id WHERE ma.deleted_at IS NULL AND ma.media_type=2 AND (r.asset_id IS NULL OR r.input_cache_key<>ma.cache_key OR r.status='pending') ORDER BY ma.sort_time DESC,ma.id DESC LIMIT ?) ORDER BY sort_time DESC`, missingImages, missingVideos)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var items []AIBackfillItem
		for rows.Next() {
			var item AIBackfillItem
			var timeline time.Time
			if err := rows.Scan(&item.AssetID, &item.CacheKey, &item.RelPath, &timeline); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, rows.Err()
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT ma.id,ma.cache_key,fi.rel_path FROM media_asset ma
JOIN LATERAL (SELECT rel_path FROM file_instance WHERE asset_id=ma.id AND missing=false ORDER BY last_seen_at DESC,id DESC LIMIT 1) fi ON true
LEFT JOIN asset_ai_result r ON r.asset_id=ma.id
WHERE ma.deleted_at IS NULL AND (r.asset_id IS NULL OR r.input_cache_key<>ma.cache_key OR r.status='pending')
ORDER BY ma.sort_time DESC,ma.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AIBackfillItem
	for rows.Next() {
		var item AIBackfillItem
		if err := rows.Scan(&item.AssetID, &item.CacheKey, &item.RelPath); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) ResetAIProcessing(ctx context.Context) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE asset_ai_result SET status='pending',updated_at=now() WHERE status='processing'`)
	return err
}

func (d *DB) ReindexAI(ctx context.Context) (int64, error) {
	result, err := d.conn.ExecContext(ctx, `INSERT INTO asset_ai_result(asset_id,input_cache_key,status,attempts,error_text,updated_at)
SELECT id,cache_key,'pending',0,NULL,now() FROM media_asset WHERE deleted_at IS NULL
ON CONFLICT(asset_id) DO UPDATE SET input_cache_key=excluded.input_cache_key,status='pending',attempts=0,error_text=NULL,updated_at=now()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *DB) ReindexAIForLibrary(ctx context.Context, library ScanLibrary) ([]AIBackfillItem, error) {
	conditions := make([]string, 0, len(library.Roots))
	args := make([]any, 0, len(library.Roots)*2)
	for _, root := range library.Roots {
		root = strings.Trim(strings.ReplaceAll(root, `\`, "/"), "/")
		if root == "" {
			conditions = []string{"TRUE"}
			args = nil
			break
		}
		conditions = append(conditions, `(fi.rel_path=? OR fi.rel_path LIKE ? ESCAPE '\')`)
		args = append(args, root, escapeLike(root)+"/%")
	}
	if len(conditions) == 0 {
		return []AIBackfillItem{}, nil
	}
	query := `WITH selected AS (
  SELECT DISTINCT ma.id,ma.cache_key,ma.sort_time
  FROM media_asset ma
  JOIN file_instance fi ON fi.asset_id=ma.id AND fi.missing=false
  WHERE ma.deleted_at IS NULL AND (` + strings.Join(conditions, " OR ") + `)
), reset AS (
  INSERT INTO asset_ai_result(asset_id,input_cache_key,status,attempts,error_text,started_at,finished_at,updated_at)
  SELECT id,cache_key,'pending',0,NULL,NULL,NULL,now() FROM selected
  ON CONFLICT(asset_id) DO UPDATE SET input_cache_key=excluded.input_cache_key,status='pending',attempts=0,error_text=NULL,started_at=NULL,finished_at=NULL,updated_at=now()
  RETURNING asset_id,input_cache_key
)
SELECT reset.asset_id,reset.input_cache_key FROM reset JOIN selected ON selected.id=reset.asset_id ORDER BY selected.sort_time DESC,reset.asset_id DESC`
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AIBackfillItem
	for rows.Next() {
		var item AIBackfillItem
		if err := rows.Scan(&item.AssetID, &item.CacheKey); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) RetryFailedAI(ctx context.Context) ([]AIBackfillItem, error) {
	rows, err := d.conn.QueryContext(ctx, `WITH reset AS (
  UPDATE asset_ai_result r
  SET status='pending',attempts=0,error_text=NULL,started_at=NULL,finished_at=NULL,updated_at=now()
  FROM media_asset ma
  WHERE ma.id=r.asset_id AND ma.deleted_at IS NULL AND r.input_cache_key=ma.cache_key AND r.status='failed'
  RETURNING r.asset_id,ma.cache_key,ma.sort_time
)
SELECT asset_id,cache_key FROM reset ORDER BY sort_time DESC,asset_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AIBackfillItem
	for rows.Next() {
		var item AIBackfillItem
		if err := rows.Scan(&item.AssetID, &item.CacheKey); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) AIActivity(ctx context.Context) (AIActivity, error) {
	var started, finished sql.NullTime
	err := d.conn.QueryRowContext(ctx, `SELECT MAX(r.started_at),MAX(r.finished_at)
FROM asset_ai_result r JOIN media_asset ma ON ma.id=r.asset_id
WHERE ma.deleted_at IS NULL AND r.input_cache_key=ma.cache_key`).Scan(&started, &finished)
	if err != nil {
		return AIActivity{}, err
	}
	activity := AIActivity{}
	if started.Valid {
		value := started.Time.Unix()
		activity.LastStartedAt = &value
	}
	if finished.Valid {
		value := finished.Time.Unix()
		activity.LastFinishedAt = &value
	}
	return activity, nil
}

func (d *DB) AITags(ctx context.Context, query string) ([]AITagSummary, error) {
	query = strings.TrimSpace(query)
	rows, err := d.conn.QueryContext(ctx, `WITH ai_rows AS (
  SELECT ait.tag,ait.asset_id FROM asset_ai_tag ait JOIN asset_ai_result air ON air.asset_id=ait.asset_id JOIN media_asset ma ON ma.id=ait.asset_id
  WHERE air.status='ready' AND air.input_cache_key=ma.cache_key AND ma.deleted_at IS NULL
),manual_rows AS (
  SELECT tag.name AS tag,asset_tag.asset_id FROM tag LEFT JOIN asset_tag ON asset_tag.tag_id=tag.id
),names AS (
  SELECT tag FROM ai_rows UNION SELECT name FROM tag
),combined AS (
  SELECT tag,asset_id,'ai' AS tag_source FROM ai_rows
  UNION ALL
  SELECT tag,asset_id,'manual' AS tag_source FROM manual_rows WHERE asset_id IS NOT NULL
)
SELECT names.tag,
  COUNT(DISTINCT combined.asset_id),
  COUNT(DISTINCT combined.asset_id) FILTER(WHERE combined.tag_source='ai'),
  COUNT(DISTINCT combined.asset_id) FILTER(WHERE combined.tag_source='manual'),
  EXISTS(SELECT 1 FROM tag manual_tag WHERE manual_tag.name=names.tag),
  (SELECT manual_tag.id FROM tag manual_tag WHERE manual_tag.name=names.tag LIMIT 1)
FROM names LEFT JOIN combined ON combined.tag=names.tag
WHERE (?='' OR lower(names.tag) LIKE ? ESCAPE '\')
GROUP BY names.tag ORDER BY COUNT(DISTINCT combined.asset_id) DESC,lower(names.tag),names.tag LIMIT 800`, query, "%"+escapeLike(strings.ToLower(query))+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AITagSummary
	for rows.Next() {
		var item AITagSummary
		var manualTagID sql.NullInt64
		if err := rows.Scan(&item.Tag, &item.Count, &item.AICount, &item.ManualCount, &item.ManualAdded, &manualTagID); err != nil {
			return nil, err
		}
		if manualTagID.Valid {
			id := manualTagID.Int64
			item.ManualTagID = &id
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) AIStatus(ctx context.Context) (AIStatus, error) {
	var out AIStatus
	err := d.conn.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE r.status='pending' OR r.asset_id IS NULL),COUNT(*) FILTER(WHERE r.status='processing'),COUNT(*) FILTER(WHERE r.status='ready' AND r.input_cache_key=ma.cache_key),COUNT(*) FILTER(WHERE r.status='failed' AND r.input_cache_key=ma.cache_key),COUNT(*) FILTER(WHERE r.asset_id IS NOT NULL AND r.input_cache_key<>ma.cache_key),COALESCE((COUNT(*) FILTER(WHERE r.status='ready' AND r.finished_at>now()-interval '1 hour'))/GREATEST(EXTRACT(EPOCH FROM (now()-MIN(r.finished_at) FILTER(WHERE r.status='ready' AND r.finished_at>now()-interval '1 hour')))/60.0,1.0/60.0),0)
FROM media_asset ma LEFT JOIN asset_ai_result r ON r.asset_id=ma.id WHERE ma.deleted_at IS NULL`).Scan(&out.Total, &out.Pending, &out.Processing, &out.Ready, &out.Failed, &out.Stale, &out.PerMinute)
	return out, err
}
