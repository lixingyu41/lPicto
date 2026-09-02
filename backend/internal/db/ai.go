package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// AIMaxAttempts is the initial attempt plus three automatic retries. A final
// failure remains visible with its diagnostic output instead of looping forever.
const AIMaxAttempts = 4

var (
	ErrEmptyAITag = errors.New("AI tag is empty")
	ErrAITagLimit = errors.New("AI tag limit reached")
)

type AITag struct {
	Tag           string       `json:"tag"`
	Confidence    float64      `json:"confidence"`
	CategoryKey   string       `json:"categoryKey,omitempty"`
	CategoryLabel string       `json:"categoryLabel,omitempty"`
	SubjectKey    string       `json:"subjectKey,omitempty"`
	SubjectLabel  string       `json:"subjectLabel,omitempty"`
	Facets        []AITagFacet `json:"facets,omitempty"`
}

type AITagFacet struct {
	FacetKey string   `json:"facetKey"`
	NodeID   string   `json:"nodeId"`
	NodeIDs  []string `json:"nodeIds"`
	Labels   []string `json:"labels"`
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

type AIAccessFailureCandidate struct {
	AssetID int64
	RelPath string
}

type AITagSummary struct {
	Tag         string `json:"tag"`
	Count       int64  `json:"count"`
	AICount     int64  `json:"aiCount"`
	ManualCount int64  `json:"manualCount"`
	ManualAdded bool   `json:"manualAdded"`
	ManualTagID *int64 `json:"manualTagId,omitempty"`
}

type AITagTreeNode struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Label    string `json:"label"`
	Depth    int    `json:"depth"`
	Count    int64  `json:"count"`
	FacetKey string `json:"facetKey"`
	Source   string `json:"source"`
}

type AIStatus struct {
	Total         int64   `json:"total"`
	Pending       int64   `json:"pending"`
	Processing    int64   `json:"processing"`
	Ready         int64   `json:"ready"`
	Failed        int64   `json:"failed"`
	Stale         int64   `json:"stale"`
	Queued        int     `json:"queued"`
	Active        int     `json:"active"`
	PerMinute     float64 `json:"perMinute"`
	ETASeconds    *int64  `json:"etaSeconds"`
	Staged        int     `json:"staged"`
	StagedBytes   int64   `json:"stagedBytes"`
	SourceReading bool    `json:"sourceReading"`
	PausedReason  string  `json:"pausedReason"`
}

type AIActivity struct {
	LastStartedAt  *int64
	LastFinishedAt *int64
}

func (d *DB) AIAverageSecondsPerItem(ctx context.Context) (*float64, error) {
	var average sql.NullFloat64
	err := d.conn.QueryRowContext(ctx, `
SELECT AVG(GREATEST(EXTRACT(EPOCH FROM (COALESCE(r.finished_at,now())-r.started_at)),0))
FROM asset_ai_result r
JOIN media_asset ma ON ma.id=r.asset_id
WHERE ma.deleted_at IS NULL
  AND r.input_cache_key=ma.cache_key
  AND r.started_at IS NOT NULL
  AND (r.finished_at IS NOT NULL OR r.status='processing')`).Scan(&average)
	if err != nil {
		return nil, err
	}
	if !average.Valid {
		return nil, nil
	}
	value := average.Float64
	return &value, nil
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
WHERE asset_id=? AND input_cache_key=? AND status IN ('pending','failed') RETURNING attempts`, assetID, cacheKey).Scan(&attempts)
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
	result, err := tx.ExecContext(ctx, rebindPostgres(`UPDATE asset_ai_result SET status='ready',description=?,tag_model=?,tag_model_version=?,description_model=?,description_model_version=?,taxonomy_version=?,sampled_frames=?::jsonb,palette=?::jsonb,error_text=NULL,finished_at=now(),updated_at=now() WHERE asset_id=? AND input_cache_key=? AND status='processing'`), description, tagModel, tagVersion, descriptionModel, descriptionVersion, taxonomyVersion, string(frames), string(paletteJSON), assetID, cacheKey)
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
		tag.Tag = strings.TrimSpace(tag.Tag)
		if tag.Tag == "" || strings.Contains(tag.Tag, "无法判断") {
			continue
		}
		if _, err = tx.ExecContext(ctx, rebindPostgres(`INSERT INTO asset_ai_tag(asset_id,tag,confidence,category_key,category_label,subject_key,subject_label) VALUES(?,?,?,?,?,?,?)`),
			assetID, tag.Tag, tag.Confidence, fallback(tag.CategoryKey, "other"), fallback(tag.CategoryLabel, "其他"), fallback(tag.SubjectKey, "object"), fallback(tag.SubjectLabel, "物体")); err != nil {
			return err
		}
		for _, facet := range tag.Facets {
			if strings.TrimSpace(facet.FacetKey) == "" || strings.TrimSpace(facet.NodeID) == "" || len(facet.NodeIDs) == 0 || len(facet.NodeIDs) != len(facet.Labels) {
				continue
			}
			if _, err = tx.ExecContext(ctx, rebindPostgres(`INSERT INTO asset_ai_tag_facet(asset_id,tag,facet_key,node_id,node_ids,labels) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`),
				assetID, tag.Tag, facet.FacetKey, facet.NodeID, facet.NodeIDs, facet.Labels); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (d *DB) ReplaceAITag(ctx context.Context, assetID int64, cacheKey, previousTag string, tag AITag) (AIResult, error) {
	previousTag = strings.TrimSpace(previousTag)
	tag.Tag = strings.TrimSpace(tag.Tag)
	if tag.Tag == "" || strings.Contains(tag.Tag, "无法判断") {
		return AIResult{}, ErrEmptyAITag
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return AIResult{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO asset_ai_result(asset_id,input_cache_key,status,updated_at)
VALUES($1,$2,'pending',now()) ON CONFLICT(asset_id) DO NOTHING`, assetID, cacheKey); err != nil {
		return AIResult{}, err
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM asset_ai_result WHERE asset_id=$1 FOR UPDATE`, assetID).Scan(&status); err != nil {
		return AIResult{}, err
	}
	if previousTag != "" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM asset_ai_tag WHERE asset_id=$1 AND tag=$2`, assetID, previousTag); err != nil {
			return AIResult{}, err
		}
	}
	var count int
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(BOOL_OR(tag=$2),false) FROM asset_ai_tag WHERE asset_id=$1`, assetID, tag.Tag).Scan(&count, &exists); err != nil {
		return AIResult{}, err
	}
	if count >= 10 && !exists {
		return AIResult{}, ErrAITagLimit
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO asset_ai_tag(asset_id,tag,confidence,category_key,category_label,subject_key,subject_label)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(asset_id,tag) DO UPDATE SET confidence=excluded.confidence,category_key=excluded.category_key,category_label=excluded.category_label,subject_key=excluded.subject_key,subject_label=excluded.subject_label`,
		assetID, tag.Tag, tag.Confidence, tag.CategoryKey, tag.CategoryLabel, tag.SubjectKey, tag.SubjectLabel); err != nil {
		return AIResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM asset_ai_tag_facet WHERE asset_id=$1 AND tag=$2`, assetID, tag.Tag); err != nil {
		return AIResult{}, err
	}
	for _, facet := range tag.Facets {
		if strings.TrimSpace(facet.FacetKey) == "" || strings.TrimSpace(facet.NodeID) == "" || len(facet.NodeIDs) == 0 || len(facet.NodeIDs) != len(facet.Labels) {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO asset_ai_tag_facet(asset_id,tag,facet_key,node_id,node_ids,labels) VALUES($1,$2,$3,$4,$5,$6)`,
			assetID, tag.Tag, facet.FacetKey, facet.NodeID, facet.NodeIDs, facet.Labels); err != nil {
			return AIResult{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE asset_ai_result SET updated_at=now() WHERE asset_id=$1`, assetID); err != nil {
		return AIResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return AIResult{}, err
	}
	return d.GetAIResult(ctx, assetID)
}

func (d *DB) DeleteAITag(ctx context.Context, assetID int64, tag string) (AIResult, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return AIResult{}, ErrEmptyAITag
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return AIResult{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM asset_ai_result WHERE asset_id=$1 FOR UPDATE`, assetID).Scan(&status); err != nil {
		return AIResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM asset_ai_tag WHERE asset_id=$1 AND tag=$2`, assetID, tag); err != nil {
		return AIResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE asset_ai_result SET updated_at=now() WHERE asset_id=$1`, assetID); err != nil {
		return AIResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return AIResult{}, err
	}
	return d.GetAIResult(ctx, assetID)
}

func fallback(value, defaultValue string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return defaultValue
}

func (d *DB) MarkAIFailed(ctx context.Context, assetID int64, cacheKey, message string) (bool, error) {
	var attempts int
	err := d.conn.QueryRowContext(ctx, `UPDATE asset_ai_result SET status='failed',error_text=?,finished_at=now(),updated_at=now() WHERE asset_id=? AND input_cache_key=? AND status='processing' RETURNING attempts`, message, assetID, cacheKey).Scan(&attempts)
	return attempts < AIMaxAttempts, err
}

func (d *DB) RequeueAIInterrupted(ctx context.Context, assetID int64, cacheKey string) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE asset_ai_result
SET status='pending',attempts=GREATEST(attempts-1,0),started_at=NULL,finished_at=NULL,error_text=NULL,updated_at=now()
WHERE asset_id=? AND input_cache_key=? AND status='processing'`, assetID, cacheKey)
	return err
}

func (d *DB) GetAIResult(ctx context.Context, assetID int64) (AIResult, error) {
	out := AIResult{
		Tags:          []AITag{},
		Palette:       []AIColor{},
		SampledFrames: json.RawMessage(`[]`),
	}
	var description, errorText sql.NullString
	var frames, palette []byte
	err := d.conn.QueryRowContext(ctx, `SELECT r.asset_id,r.input_cache_key,r.status,r.description,r.tag_model,r.tag_model_version,r.description_model,r.description_model_version,r.taxonomy_version,r.sampled_frames,r.palette,r.attempts,r.error_text,r.updated_at
FROM asset_ai_result r JOIN media_asset ma ON ma.id=r.asset_id WHERE r.asset_id=? AND ma.deleted_at IS NULL`, assetID).Scan(&out.AssetID, &out.InputCacheKey, &out.Status, &description, &out.TagModel, &out.TagModelVersion, &out.DescriptionModel, &out.DescriptionModelVersion, &out.TaxonomyVersion, &frames, &palette, &out.Attempts, &errorText, &out.UpdatedAt)
	if err != nil {
		return AIResult{}, err
	}
	out.Description = description.String
	out.Error = errorText.String
	if len(frames) > 0 && string(frames) != "null" {
		out.SampledFrames = append(json.RawMessage(nil), frames...)
	}
	_ = json.Unmarshal(palette, &out.Palette)
	if out.Palette == nil {
		out.Palette = []AIColor{}
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT t.tag,t.confidence,t.category_key,t.category_label,t.subject_key,t.subject_label,
f.facet_key,f.node_id,COALESCE(to_jsonb(f.node_ids),'[]'::jsonb),COALESCE(to_jsonb(f.labels),'[]'::jsonb)
FROM asset_ai_tag t LEFT JOIN asset_ai_tag_facet f ON f.asset_id=t.asset_id AND f.tag=t.tag
WHERE t.asset_id=? ORDER BY t.confidence DESC,t.tag,f.node_id`, assetID)
	if err != nil {
		return AIResult{}, err
	}
	defer rows.Close()
	tagIndexes := map[string]int{}
	for rows.Next() {
		var tag AITag
		var facetKey, nodeID sql.NullString
		var nodeIDsJSON, labelsJSON []byte
		if err := rows.Scan(&tag.Tag, &tag.Confidence, &tag.CategoryKey, &tag.CategoryLabel, &tag.SubjectKey, &tag.SubjectLabel, &facetKey, &nodeID, &nodeIDsJSON, &labelsJSON); err != nil {
			return AIResult{}, err
		}
		var nodeIDs, labels []string
		_ = json.Unmarshal(nodeIDsJSON, &nodeIDs)
		_ = json.Unmarshal(labelsJSON, &labels)
		index, exists := tagIndexes[tag.Tag]
		if !exists {
			index = len(out.Tags)
			tagIndexes[tag.Tag] = index
			out.Tags = append(out.Tags, tag)
		}
		if facetKey.Valid && nodeID.Valid {
			out.Tags[index].Facets = append(out.Tags[index].Facets, AITagFacet{FacetKey: facetKey.String, NodeID: nodeID.String, NodeIDs: nodeIDs, Labels: labels})
		}
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
	query := `SELECT r.asset_id,COALESCE(r.description,''),r.palette,t.tag,t.confidence,
t.category_key,t.category_label,t.subject_key,t.subject_label,
f.facet_key,f.node_id,COALESCE(to_jsonb(f.node_ids),'[]'::jsonb),COALESCE(to_jsonb(f.labels),'[]'::jsonb)
FROM asset_ai_result r
JOIN media_asset ma ON ma.id=r.asset_id AND ma.cache_key=r.input_cache_key
LEFT JOIN asset_ai_tag t ON t.asset_id=r.asset_id
LEFT JOIN asset_ai_tag_facet f ON f.asset_id=t.asset_id AND f.tag=t.tag
WHERE r.status='ready' AND r.asset_id IN (` + strings.Join(placeholders, ",") + `)
ORDER BY r.asset_id,t.confidence DESC,t.tag,f.node_id`
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tagIndexes := make(map[int64]map[string]int, len(assetIDs))
	for rows.Next() {
		var assetID int64
		var description string
		var palette []byte
		var tag sql.NullString
		var confidence sql.NullFloat64
		var categoryKey, categoryLabel, subjectKey, subjectLabel, facetKey, nodeID sql.NullString
		var nodeIDsJSON, labelsJSON []byte
		if err := rows.Scan(&assetID, &description, &palette, &tag, &confidence, &categoryKey, &categoryLabel, &subjectKey, &subjectLabel, &facetKey, &nodeID, &nodeIDsJSON, &labelsJSON); err != nil {
			return nil, err
		}
		var nodeIDs, labels []string
		_ = json.Unmarshal(nodeIDsJSON, &nodeIDs)
		_ = json.Unmarshal(labelsJSON, &labels)
		summary := result[assetID]
		summary.Description = description
		if len(summary.Palette) == 0 {
			_ = json.Unmarshal(palette, &summary.Palette)
		}
		if tag.Valid {
			indexes := tagIndexes[assetID]
			if indexes == nil {
				indexes = map[string]int{}
				tagIndexes[assetID] = indexes
			}
			index, exists := indexes[tag.String]
			if !exists {
				index = len(summary.Tags)
				indexes[tag.String] = index
				summary.Tags = append(summary.Tags, AITag{
					Tag: tag.String, Confidence: confidence.Float64,
					CategoryKey: categoryKey.String, CategoryLabel: categoryLabel.String,
					SubjectKey: subjectKey.String, SubjectLabel: subjectLabel.String,
				})
			}
			if facetKey.Valid && nodeID.Valid {
				summary.Tags[index].Facets = append(summary.Tags[index].Facets, AITagFacet{FacetKey: facetKey.String, NodeID: nodeID.String, NodeIDs: nodeIDs, Labels: labels})
			}
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
WHERE ma.deleted_at IS NULL AND ma.media_type IN (1,2) AND (r.asset_id IS NULL OR r.input_cache_key<>ma.cache_key OR r.status='pending')
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
	tx, err := d.raw.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM asset_ai_tag`); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO asset_ai_result(asset_id,input_cache_key,status,description,tag_model,tag_model_version,description_model,description_model_version,taxonomy_version,sampled_frames,palette,attempts,error_text,started_at,finished_at,updated_at)
SELECT id,cache_key,'pending',NULL,'','','','','','[]'::jsonb,'[]'::jsonb,0,NULL,NULL,NULL,now() FROM media_asset WHERE deleted_at IS NULL AND media_type IN (1,2)
ON CONFLICT(asset_id) DO UPDATE SET input_cache_key=excluded.input_cache_key,status='pending',description=NULL,tag_model='',tag_model_version='',description_model='',description_model_version='',taxonomy_version='',sampled_frames='[]'::jsonb,palette='[]'::jsonb,attempts=0,error_text=NULL,started_at=NULL,finished_at=NULL,updated_at=now()`)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
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
  WHERE ma.deleted_at IS NULL AND ma.media_type IN (1,2) AND (` + strings.Join(conditions, " OR ") + `)
), deleted_tags AS (
  DELETE FROM asset_ai_tag t USING selected WHERE t.asset_id=selected.id RETURNING t.asset_id
), reset AS (
  INSERT INTO asset_ai_result(asset_id,input_cache_key,status,description,tag_model,tag_model_version,description_model,description_model_version,taxonomy_version,sampled_frames,palette,attempts,error_text,started_at,finished_at,updated_at)
  SELECT id,cache_key,'pending',NULL,'','','','','','[]'::jsonb,'[]'::jsonb,0,NULL,NULL,NULL,now() FROM selected
  ON CONFLICT(asset_id) DO UPDATE SET input_cache_key=excluded.input_cache_key,status='pending',description=NULL,tag_model='',tag_model_version='',description_model='',description_model_version='',taxonomy_version='',sampled_frames='[]'::jsonb,palette='[]'::jsonb,attempts=0,error_text=NULL,started_at=NULL,finished_at=NULL,updated_at=now()
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
  WHERE ma.id=r.asset_id AND ma.deleted_at IS NULL AND ma.media_type IN (1,2) AND r.input_cache_key=ma.cache_key AND r.status='failed'
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

func (d *DB) AIAccessFailureCandidates(ctx context.Context) ([]AIAccessFailureCandidate, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT r.asset_id,fi.rel_path
FROM asset_ai_result r
JOIN media_asset ma ON ma.id=r.asset_id AND ma.deleted_at IS NULL AND ma.media_type IN (1,2) AND r.input_cache_key=ma.cache_key
JOIN LATERAL (
  SELECT rel_path FROM file_instance
  WHERE asset_id=r.asset_id AND missing=false
  ORDER BY id LIMIT 1
) fi ON true
WHERE r.status='failed' AND (
  lower(COALESCE(r.error_text,'')) LIKE '%no such file%' OR
  lower(COALESCE(r.error_text,'')) LIKE '%file not found%' OR
  lower(COALESCE(r.error_text,'')) LIKE '%cannot find the file%' OR
  lower(COALESCE(r.error_text,'')) LIKE '%permission denied%' OR
  lower(COALESCE(r.error_text,'')) LIKE '%operation not permitted%' OR
  lower(COALESCE(r.error_text,'')) LIKE '%input/output error%' OR
  lower(COALESCE(r.error_text,'')) LIKE '%stale file handle%'
)
ORDER BY r.asset_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AIAccessFailureCandidate, 0)
	for rows.Next() {
		var item AIAccessFailureCandidate
		if err := rows.Scan(&item.AssetID, &item.RelPath); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) RequeueAIFailuresByAssetIDs(ctx context.Context, assetIDs []int64) (int, error) {
	if len(assetIDs) == 0 {
		return 0, nil
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count := 0
	for _, assetID := range assetIDs {
		result, err := tx.ExecContext(ctx, `UPDATE asset_ai_result r
SET status='pending',attempts=0,error_text=NULL,started_at=NULL,finished_at=NULL,updated_at=now()
FROM media_asset ma
WHERE r.asset_id=? AND ma.id=r.asset_id AND ma.deleted_at IS NULL AND r.input_cache_key=ma.cache_key AND r.status='failed'`, assetID)
		if err != nil {
			return 0, err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr == nil {
			count += int(affected)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
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

func (d *DB) AITagTree(ctx context.Context, tagNodes []string) ([]AITagTreeNode, error) {
	where, args := assetFilterSQL(AssetListOptions{TagNodes: tagNodes}, false)
	rows, err := d.conn.QueryContext(ctx, `WITH eligible AS (
  SELECT id AS asset_id FROM assets WHERE `+where+`
),ai_nodes AS (
  SELECT eligible.asset_id,
    f.node_ids[position] AS id,
    CASE WHEN position=1 THEN '' ELSE f.node_ids[position-1] END AS parent_id,
    f.labels[position] AS label,
    position AS depth,
    CASE
      WHEN position=1 THEN replace(f.node_ids[1],'ai:','')
      WHEN position=2 THEN replace(f.node_ids[2],'ai:','')
      ELSE f.facet_key
    END AS facet_key
  FROM asset_ai_tag_facet f
  JOIN asset_ai_result air ON air.asset_id=f.asset_id
  JOIN media_asset ma ON ma.id=f.asset_id
  LEFT JOIN eligible ON eligible.asset_id=f.asset_id
  CROSS JOIN LATERAL generate_subscripts(f.node_ids,1) AS positions(position)
  WHERE air.status='ready' AND air.input_cache_key=ma.cache_key AND ma.deleted_at IS NULL
),manual_nodes AS (
  SELECT eligible.asset_id,'manual:'||tag.name AS id,'manual' AS parent_id,tag.name AS label,2 AS depth,'manual' AS facet_key
  FROM tag LEFT JOIN asset_tag ON asset_tag.tag_id=tag.id LEFT JOIN eligible ON eligible.asset_id=asset_tag.asset_id
)
SELECT id,parent_id,label,depth,facet_key,COUNT(DISTINCT asset_id),'ai' AS source
FROM ai_nodes GROUP BY id,parent_id,label,depth,facet_key
UNION ALL
SELECT 'manual','','自标',1,'manual',COUNT(DISTINCT asset_id),'manual' FROM manual_nodes
UNION ALL
SELECT id,parent_id,label,depth,facet_key,COUNT(DISTINCT asset_id),'manual'
FROM manual_nodes GROUP BY id,parent_id,label,depth,facet_key
ORDER BY depth,label,id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AITagTreeNode
	for rows.Next() {
		var item AITagTreeNode
		if err := rows.Scan(&item.ID, &item.ParentID, &item.Label, &item.Depth, &item.FacetKey, &item.Count, &item.Source); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) AIStatus(ctx context.Context) (AIStatus, error) {
	var out AIStatus
	err := d.conn.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE r.status='pending' OR r.asset_id IS NULL),COUNT(*) FILTER(WHERE r.status='processing'),COUNT(*) FILTER(WHERE r.status='ready' AND r.input_cache_key=ma.cache_key),COUNT(*) FILTER(WHERE r.status='failed' AND r.input_cache_key=ma.cache_key),COUNT(*) FILTER(WHERE r.asset_id IS NOT NULL AND r.input_cache_key<>ma.cache_key),COALESCE((COUNT(*) FILTER(WHERE r.status='ready' AND r.finished_at>now()-interval '1 hour'))/GREATEST(EXTRACT(EPOCH FROM (now()-MIN(r.finished_at) FILTER(WHERE r.status='ready' AND r.finished_at>now()-interval '1 hour')))/60.0,1.0/60.0),0)
FROM media_asset ma LEFT JOIN asset_ai_result r ON r.asset_id=ma.id WHERE ma.deleted_at IS NULL AND ma.media_type IN (1,2)`).Scan(&out.Total, &out.Pending, &out.Processing, &out.Ready, &out.Failed, &out.Stale, &out.PerMinute)
	return out, err
}
