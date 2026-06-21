package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type AssetUpsert struct {
	RelPath           string
	ParentRelPath     string
	Filename          string
	Ext               string
	MediaType         string
	MimeType          *string
	Size              int64
	Mtime             int64
	Width             *int
	Height            *int
	Duration          *float64
	TakenAt           *int64
	ImportedAt        int64
	TimelineAt        int64
	CacheKey          string
	BrowserPlayable   bool
	ThumbStatus       string
	PreviewStatus     string
	VideoPosterStatus string
	VideoProxyStatus  string
	MetadataJSON      *string
	NFOJSON           *string
	NFOSearchText     *string
	NFOSize           *int64
	NFOMtime          *int64
	NFOScanned        bool
	Error             *string
}

type AssetSignature struct {
	ID       int64
	Size     int64
	Mtime    int64
	NFOSize  *int64
	NFOMtime *int64
	HasNFO   bool
}

type AssetListOptions struct {
	Page         int
	PageSize     int
	Type         string
	Sort         string
	Query        string
	FolderID     *int64
	From         *int64
	To           *int64
	FolderRel    *string
	Recursive    bool
	VisibleOnly  bool
	NFOQuery     string
	NFOActor     string
	NFOID        string
	NFOTag       string
	NFOTitle     string
	NFOYear      string
	MinWidth     *int
	MaxWidth     *int
	MinHeight    *int
	MaxHeight    *int
	MatchAnyAxis bool
	MinDuration  *float64
	MaxDuration  *float64
	MinSize      *int64
	MaxSize      *int64
	Orientation  string
}

type NeighborOptions struct {
	Context      string
	AssetID      int64
	Type         string
	Sort         string
	Query        string
	FolderID     *int64
	From         *int64
	To           *int64
	Limit        int
	Recursive    bool
	NFOQuery     string
	NFOActor     string
	NFOID        string
	NFOTag       string
	NFOTitle     string
	NFOYear      string
	MinWidth     *int
	MaxWidth     *int
	MinHeight    *int
	MaxHeight    *int
	MatchAnyAxis bool
	MinDuration  *float64
	MaxDuration  *float64
	MinSize      *int64
	MaxSize      *int64
	Orientation  string
}

type NFOOptionOptions struct {
	Field       string
	Query       string
	Limit       int
	VisibleOnly bool
}

type Neighbors struct {
	Current  model.Asset
	Previous []model.Asset
	Next     []model.Asset
}

type WorkItem struct {
	Type    string
	AssetID int64
}

type DeletedAsset struct {
	ID        int64
	RelPath   string
	CacheKey  string
	MediaType string
}

type AssetUpsertResult struct {
	ID          int64
	Added       bool
	Updated     bool
	OldCacheKey string
}

type LibraryAnchor struct {
	Key      string
	Label    string
	Kind     string
	Page     int
	Position float64
	Value    int64
}

type LibraryAnchorResult struct {
	Items []LibraryAnchor
	Total int
}

type AssetPosition struct {
	Index    int
	Page     int
	Position float64
	Total    int
}

type libraryAnchorRow struct {
	Filename   string
	ImportedAt int64
	Size       int64
	TimelineAt int64
}

func (d *DB) UpsertAsset(ctx context.Context, p AssetUpsert) (id int64, added bool, updated bool, err error) {
	result, err := d.UpsertAssetDetailed(ctx, p)
	if err != nil {
		return 0, false, false, err
	}
	return result.ID, result.Added, result.Updated, nil
}

func (d *DB) UpsertAssetDetailed(ctx context.Context, p AssetUpsert) (AssetUpsertResult, error) {
	now := util.UnixNow()
	if p.ImportedAt == 0 {
		p.ImportedAt = now
	}
	if err := d.EnsureAssetFolders(ctx, p.RelPath); err != nil {
		return AssetUpsertResult{}, err
	}
	var existingID int64
	var existingSize int64
	var existingMtime int64
	var existingCacheKey string
	var existingTimelineAt int64
	var deletedAt sql.NullInt64
	var existingNFOJSON sql.NullString
	var existingNFOSearchText sql.NullString
	var existingNFOSize sql.NullInt64
	var existingNFOMtime sql.NullInt64
	err := d.conn.QueryRowContext(ctx, `SELECT id, size, mtime, cache_key, timeline_at, deleted_at, nfo_json, nfo_search_text, nfo_size, nfo_mtime FROM assets WHERE rel_path = ?`, p.RelPath).Scan(&existingID, &existingSize, &existingMtime, &existingCacheKey, &existingTimelineAt, &deletedAt, &existingNFOJSON, &existingNFOSearchText, &existingNFOSize, &existingNFOMtime)
	if errors.Is(err, sql.ErrNoRows) {
		tx, err := d.conn.BeginTx(ctx, nil)
		if err != nil {
			return AssetUpsertResult{}, err
		}
		folderID, err := folderIDForRel(ctx, tx, p.ParentRelPath)
		if err != nil {
			_ = tx.Rollback()
			return AssetUpsertResult{}, err
		}
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO media_asset (
  media_type, status, basename, ext, mime_type, width, height, aspect_ratio,
  duration_ms, size_bytes, file_mtime, captured_at, imported_at, sort_time,
  folder_id, metadata_json, nfo_json, nfo_search_text, cache_key, browser_playable,
  thumb_ready, preview_ready, proxy_ready, thumb_status, preview_status,
  video_poster_status, video_proxy_status, nfo_size, nfo_mtime, error_text, created_at, updated_at
) VALUES (?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id`,
			mediaTypeCode(p.MediaType), p.Filename, p.Ext, nullString(p.MimeType), nullInt(p.Width), nullInt(p.Height), aspectRatio(p.Width, p.Height),
			durationMillis(p.Duration), p.Size, unixTime(p.Mtime), unixTimePtr(p.TakenAt), unixTime(p.ImportedAt), unixTime(p.TimelineAt),
			folderID, nullString(p.MetadataJSON), nullString(p.NFOJSON), nullString(p.NFOSearchText), p.CacheKey, p.BrowserPlayable,
			p.ThumbStatus == model.StatusReady, p.PreviewStatus == model.StatusReady, p.VideoProxyStatus == model.StatusReady,
			p.ThumbStatus, p.PreviewStatus, p.VideoPosterStatus, p.VideoProxyStatus, nullInt64(p.NFOSize), unixTimePtr(p.NFOMtime), nullString(p.Error), unixTime(now), unixTime(now)).Scan(&id)
		if err != nil {
			_ = tx.Rollback()
			return AssetUpsertResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO file_instance (asset_id, library_id, rel_path, size_bytes, file_mtime, last_seen_at, missing)
VALUES (?, 1, ?, ?, ?, ?, false)`,
			id, p.RelPath, p.Size, unixTime(p.Mtime), unixTime(now)); err != nil {
			_ = tx.Rollback()
			return AssetUpsertResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssetUpsertResult{}, err
		}
		return AssetUpsertResult{ID: id, Added: true}, nil
	}
	if err != nil {
		return AssetUpsertResult{}, err
	}
	if existingSize == p.Size && existingMtime == p.Mtime && !deletedAt.Valid {
		timelineChanged := p.TimelineAt > 0 && p.TimelineAt != existingTimelineAt
		if p.NFOScanned {
			nfoSignatureChanged := !nullInt64Equal(existingNFOSize, p.NFOSize) || !nullInt64Equal(existingNFOMtime, p.NFOMtime)
			if nfoColumnsEqual(existingNFOJSON, p.NFOJSON) && nfoColumnsEqual(existingNFOSearchText, p.NFOSearchText) && !nfoSignatureChanged && !timelineChanged {
				return AssetUpsertResult{ID: existingID}, nil
			}
			_, err = d.conn.ExecContext(ctx, `UPDATE media_asset SET nfo_json = ?::jsonb, nfo_search_text = ?, nfo_size = ?, nfo_mtime = ?, sort_time = ?, updated_at = ? WHERE id = ?`,
				nullString(p.NFOJSON), nullString(p.NFOSearchText), nullInt64(p.NFOSize), unixTimePtr(p.NFOMtime), unixTime(p.TimelineAt), unixTime(now), existingID)
			if err != nil {
				return AssetUpsertResult{}, err
			}
			return AssetUpsertResult{ID: existingID, Updated: true}, nil
		}
		if timelineChanged {
			_, err = d.conn.ExecContext(ctx, `UPDATE media_asset SET sort_time = ?, updated_at = ? WHERE id = ?`, unixTime(p.TimelineAt), unixTime(now), existingID)
			if err != nil {
				return AssetUpsertResult{}, err
			}
			return AssetUpsertResult{ID: existingID, Updated: true}, nil
		}
		return AssetUpsertResult{ID: existingID}, nil
	}
	if p.NFOScanned {
		err = d.updateAssetRecord(ctx, existingID, p, now, true)
	} else {
		err = d.updateAssetRecord(ctx, existingID, p, now, false)
	}
	if err != nil {
		return AssetUpsertResult{}, err
	}
	result := AssetUpsertResult{ID: existingID, Updated: true}
	if existingCacheKey != "" && existingCacheKey != p.CacheKey {
		result.OldCacheKey = existingCacheKey
	}
	return result, nil
}

func (d *DB) updateAssetRecord(ctx context.Context, id int64, p AssetUpsert, now int64, updateNFO bool) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	folderID, err := folderIDForRel(ctx, tx, p.ParentRelPath)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if updateNFO {
		_, err = tx.ExecContext(ctx, `
UPDATE media_asset SET
  media_type = ?, basename = ?, ext = ?, mime_type = ?, size_bytes = ?, file_mtime = ?,
  width = ?, height = ?, aspect_ratio = ?, duration_ms = ?, captured_at = ?, sort_time = ?,
  cache_key = ?, browser_playable = ?, status = 0, thumb_status = ?, preview_status = ?,
  video_poster_status = ?, video_proxy_status = ?, thumb_ready = ?, preview_ready = ?, proxy_ready = ?,
  metadata_json = ?::jsonb, nfo_json = ?::jsonb, nfo_search_text = ?, nfo_size = ?, nfo_mtime = ?, error_text = ?, deleted = false,
  deleted_at = NULL, folder_id = ?, updated_at = ?
WHERE id = ?`,
			mediaTypeCode(p.MediaType), p.Filename, p.Ext, nullString(p.MimeType), p.Size, unixTime(p.Mtime),
			nullInt(p.Width), nullInt(p.Height), aspectRatio(p.Width, p.Height), durationMillis(p.Duration), unixTimePtr(p.TakenAt), unixTime(p.TimelineAt),
			p.CacheKey, p.BrowserPlayable, p.ThumbStatus, p.PreviewStatus,
			p.VideoPosterStatus, p.VideoProxyStatus, p.ThumbStatus == model.StatusReady, p.PreviewStatus == model.StatusReady, p.VideoProxyStatus == model.StatusReady,
			nullString(p.MetadataJSON), nullString(p.NFOJSON), nullString(p.NFOSearchText), nullInt64(p.NFOSize), unixTimePtr(p.NFOMtime), nullString(p.Error), folderID, unixTime(now), id)
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE media_asset SET
  media_type = ?, basename = ?, ext = ?, mime_type = ?, size_bytes = ?, file_mtime = ?,
  width = ?, height = ?, aspect_ratio = ?, duration_ms = ?, captured_at = ?, sort_time = ?,
  cache_key = ?, browser_playable = ?, status = 0, thumb_status = ?, preview_status = ?,
  video_poster_status = ?, video_proxy_status = ?, thumb_ready = ?, preview_ready = ?, proxy_ready = ?,
  metadata_json = ?::jsonb, error_text = ?, deleted = false, deleted_at = NULL, folder_id = ?, updated_at = ?
WHERE id = ?`,
			mediaTypeCode(p.MediaType), p.Filename, p.Ext, nullString(p.MimeType), p.Size, unixTime(p.Mtime),
			nullInt(p.Width), nullInt(p.Height), aspectRatio(p.Width, p.Height), durationMillis(p.Duration), unixTimePtr(p.TakenAt), unixTime(p.TimelineAt),
			p.CacheKey, p.BrowserPlayable, p.ThumbStatus, p.PreviewStatus,
			p.VideoPosterStatus, p.VideoProxyStatus, p.ThumbStatus == model.StatusReady, p.PreviewStatus == model.StatusReady, p.VideoProxyStatus == model.StatusReady,
			nullString(p.MetadataJSON), nullString(p.Error), folderID, unixTime(now), id)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE file_instance
SET rel_path = ?, size_bytes = ?, file_mtime = ?, last_seen_at = ?, missing = false
WHERE asset_id = ? AND library_id = 1`,
		p.RelPath, p.Size, unixTime(p.Mtime), unixTime(now), id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *DB) AssetHasNFO(ctx context.Context, relPath string) (bool, error) {
	value, err := d.AssetNFOJSON(ctx, relPath)
	if err != nil {
		return false, err
	}
	return value != nil && strings.TrimSpace(*value) != "", nil
}

func (d *DB) AssetNFOJSON(ctx context.Context, relPath string) (*string, error) {
	var value sql.NullString
	err := d.conn.QueryRowContext(ctx, `SELECT nfo_json FROM assets WHERE rel_path = ?`, relPath).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	return &value.String, nil
}

func (d *DB) AssetSignature(ctx context.Context, relPath string) (*AssetSignature, error) {
	var signature AssetSignature
	var nfoSize sql.NullInt64
	var nfoMtime sql.NullInt64
	err := d.conn.QueryRowContext(ctx, `
SELECT
  ma.id,
  ma.size_bytes,
  EXTRACT(EPOCH FROM ma.file_mtime)::BIGINT,
  ma.nfo_size,
  EXTRACT(EPOCH FROM ma.nfo_mtime)::BIGINT,
  ma.nfo_json IS NOT NULL
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id AND fi.missing = false
WHERE fi.rel_path = ?
  AND ma.deleted = false
  AND ma.deleted_at IS NULL`, relPath).Scan(&signature.ID, &signature.Size, &signature.Mtime, &nfoSize, &nfoMtime, &signature.HasNFO)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if nfoSize.Valid {
		signature.NFOSize = &nfoSize.Int64
	}
	if nfoMtime.Valid {
		signature.NFOMtime = &nfoMtime.Int64
	}
	return &signature, nil
}

func (d *DB) GetAsset(ctx context.Context, id int64) (model.Asset, error) {
	row := d.conn.QueryRowContext(ctx, assetSelectSQL()+` WHERE id = ? AND deleted_at IS NULL`, id)
	return scanAsset(row)
}

func (d *DB) GetAssetByCacheKey(ctx context.Context, cacheKey string) (model.Asset, error) {
	row := d.conn.QueryRowContext(ctx, assetSelectSQL()+` WHERE cache_key = ? AND deleted_at IS NULL LIMIT 1`, cacheKey)
	return scanAsset(row)
}

func (d *DB) GetAssetIncludingDeleted(ctx context.Context, id int64) (model.Asset, error) {
	row := d.conn.QueryRowContext(ctx, assetSelectSQL()+` WHERE id = ?`, id)
	return scanAsset(row)
}

func (d *DB) ListLibraryAssets(ctx context.Context, opts AssetListOptions) (model.Page[model.Asset], error) {
	return d.listAssets(ctx, opts, false)
}

func (d *DB) SearchAssets(ctx context.Context, opts AssetListOptions) (model.Page[model.Asset], error) {
	return d.listAssets(ctx, opts, true)
}

func (d *DB) NFOOptions(ctx context.Context, opts NFOOptionOptions) ([]string, error) {
	filter, ok := nfoFieldFilterSQL(opts.Field)
	if !ok {
		return nil, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}
	where := []string{"deleted_at IS NULL", "nfo_json IS NOT NULL", "(" + filter + ")"}
	var args []any
	if opts.VisibleOnly {
		where = append(where, "thumb_status = 'ready'")
	}
	if query := strings.TrimSpace(opts.Query); query != "" {
		where = append(where, "lower(trim(COALESCE(nfo_item.item_value->>'value', ''))) LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(strings.ToLower(query))+"%")
	}
	args = append(args, limit)
	query := `
SELECT value
FROM (
  SELECT DISTINCT trim(COALESCE(nfo_item.item_value->>'value', '')) AS value
  FROM assets
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(nfo_json::jsonb->'groups', '[]'::jsonb)) AS nfo_group(group_value)
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(nfo_group.group_value->'items', '[]'::jsonb)) AS nfo_item(item_value)
  WHERE ` + strings.Join(where, " AND ") + `
) options
WHERE value <> ''
ORDER BY lower(value), value
LIMIT ?`
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (d *DB) LibraryAnchors(ctx context.Context, opts AssetListOptions) (LibraryAnchorResult, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	where, args := assetFilterSQL(opts, false)
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.PageSize)
}

func (d *DB) SearchAnchors(ctx context.Context, opts AssetListOptions) (LibraryAnchorResult, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	where, args := assetFilterSQL(opts, true)
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.PageSize)
}

func (d *DB) AssetPosition(ctx context.Context, assetID int64, opts AssetListOptions, timeline bool) (AssetPosition, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	where, args := assetFilterSQL(opts, timeline)
	return d.assetPositionForFilter(ctx, assetID, where, args, opts.Sort, opts.PageSize)
}

func (d *DB) anchorsForFilter(ctx context.Context, where string, args []any, sort string, pageSize int) (LibraryAnchorResult, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	query := "SELECT filename, size, imported_at, timeline_at FROM assets WHERE " + where + " ORDER BY " + sortSQL(sort)
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return LibraryAnchorResult{}, err
	}
	defer rows.Close()
	var items []libraryAnchorRow
	for rows.Next() {
		var item libraryAnchorRow
		if err := rows.Scan(&item.Filename, &item.Size, &item.ImportedAt, &item.TimelineAt); err != nil {
			return LibraryAnchorResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return LibraryAnchorResult{}, err
	}
	if len(items) == 0 {
		return LibraryAnchorResult{}, nil
	}
	if usesUniformAnchors(sort) {
		return LibraryAnchorResult{Items: uniformAnchors(sort, items, pageSize), Total: len(items)}, nil
	}
	anchors := make([]LibraryAnchor, 0, len(items))
	seen := make(map[string]struct{})
	lastMonth := ""
	lastYear := ""
	for index, item := range items {
		key, label, kind, value := anchorParts(sort, item)
		if isTimeSort(sort) {
			year, month := dateAnchorGroups(value)
			if year != lastYear {
				kind = "year"
				lastYear = year
				lastMonth = month
			} else if month != lastMonth {
				kind = "month"
				lastMonth = month
			}
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		position := 0.0
		if len(items) > 1 {
			position = float64(index) / float64(len(items)-1)
		}
		anchors = append(anchors, LibraryAnchor{
			Key:      key,
			Label:    label,
			Kind:     kind,
			Page:     index/pageSize + 1,
			Position: position,
			Value:    value,
		})
	}
	return LibraryAnchorResult{Items: anchors, Total: len(items)}, nil
}

func (d *DB) assetPositionForFilter(ctx context.Context, assetID int64, where string, args []any, sort string, pageSize int) (AssetPosition, error) {
	query := `
SELECT item_index, total_count
FROM (
  SELECT id, ROW_NUMBER() OVER (ORDER BY ` + sortSQL(sort) + `) - 1 AS item_index, COUNT(*) OVER () AS total_count
  FROM assets
  WHERE ` + where + `
) ranked
WHERE id = ?`
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, assetID)
	var index int
	var total int
	if err := d.conn.QueryRowContext(ctx, query, queryArgs...).Scan(&index, &total); err != nil {
		return AssetPosition{}, err
	}
	position := 0.0
	if total > 1 {
		position = float64(index) / float64(total-1)
	}
	return AssetPosition{Index: index, Page: index/pageSize + 1, Position: position, Total: total}, nil
}

func usesUniformAnchors(sort string) bool {
	return sort == "" ||
		sort == "timeline_asc" ||
		sort == "timeline_desc" ||
		sort == "imported_asc" ||
		sort == "imported_desc" ||
		sort == "size" ||
		sort == "size_asc" ||
		sort == "size_desc"
}

func uniformAnchors(sort string, items []libraryAnchorRow, pageSize int) []LibraryAnchor {
	const maxAnchors = 12
	count := len(items)
	if count > maxAnchors {
		count = maxAnchors
	}
	if count <= 0 {
		return nil
	}
	anchors := make([]LibraryAnchor, 0, count)
	top := anchorScaleValue(sort, items[0])
	bottom := anchorScaleValue(sort, items[len(items)-1])
	for index := 0; index < count; index++ {
		position := 0.0
		if count > 1 {
			position = float64(index) / float64(count-1)
		}
		value := int64(float64(top) + (float64(bottom)-float64(top))*position)
		itemIndex := 0
		if len(items) > 1 {
			itemIndex = int(position * float64(len(items)-1))
		}
		label, kind := uniformAnchorLabel(sort, value)
		anchors = append(anchors, LibraryAnchor{
			Key:      fmt.Sprintf("scale:%s:%d", sort, index),
			Label:    label,
			Kind:     kind,
			Page:     itemIndex/pageSize + 1,
			Position: position,
			Value:    value,
		})
	}
	return anchors
}

func anchorScaleValue(sort string, item libraryAnchorRow) int64 {
	switch sort {
	case "imported_asc", "imported_desc":
		return item.ImportedAt
	case "size", "size_asc", "size_desc":
		return item.Size
	default:
		return item.TimelineAt
	}
}

func uniformAnchorLabel(sort string, value int64) (string, string) {
	switch sort {
	case "size", "size_asc", "size_desc":
		return formatAnchorSize(value), "size"
	default:
		return time.Unix(value, 0).Local().Format("2006-01-02"), "day"
	}
}

func (d *DB) ListTimelineAssets(ctx context.Context, opts AssetListOptions) (model.Page[model.Asset], error) {
	opts.Sort = "timeline_desc"
	return d.listAssets(ctx, opts, true)
}

func (d *DB) ListFolderAssets(ctx context.Context, folderID int64, opts AssetListOptions) (model.Page[model.Asset], error) {
	folder, err := d.getFolderRaw(ctx, folderID)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	opts.FolderRel = &folder.RelPath
	return d.listAssets(ctx, opts, false)
}

func (d *DB) FolderAnchors(ctx context.Context, folderID int64, opts AssetListOptions) (LibraryAnchorResult, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	folder, err := d.getFolderRaw(ctx, folderID)
	if err != nil {
		return LibraryAnchorResult{}, err
	}
	opts.FolderRel = &folder.RelPath
	where, args := assetFilterSQL(opts, false)
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.PageSize)
}

func (d *DB) FolderAssetPosition(ctx context.Context, folderID int64, assetID int64, opts AssetListOptions) (AssetPosition, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	folder, err := d.getFolderRaw(ctx, folderID)
	if err != nil {
		return AssetPosition{}, err
	}
	opts.FolderRel = &folder.RelPath
	where, args := assetFilterSQL(opts, false)
	return d.assetPositionForFilter(ctx, assetID, where, args, opts.Sort, opts.PageSize)
}

func (d *DB) listAssets(ctx context.Context, opts AssetListOptions, timeline bool) (model.Page[model.Asset], error) {
	where, args := assetFilterSQL(opts, timeline)
	order := sortSQL(opts.Sort)
	limit := opts.PageSize + 1
	offset := (opts.Page - 1) * opts.PageSize
	query := assetSelectSQL() + " WHERE " + where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	defer rows.Close()
	items, err := scanAssetRows(rows)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	hasMore := len(items) > opts.PageSize
	if hasMore {
		items = items[:opts.PageSize]
	}
	return model.Page[model.Asset]{Items: items, Page: opts.Page, PageSize: opts.PageSize, HasMore: hasMore}, nil
}

func (d *DB) ActiveRelPaths(ctx context.Context) (map[string]struct{}, error) {
	rows, err := d.conn.QueryContext(ctx, `SELECT rel_path FROM assets WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		result[rel] = struct{}{}
	}
	return result, rows.Err()
}

func (d *DB) ActiveRelPathsForRoots(ctx context.Context, roots []string) (map[string]struct{}, error) {
	where, args, err := assetRootsWhere(roots)
	if err != nil {
		return nil, err
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT rel_path FROM assets WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		result[rel] = struct{}{}
	}
	return result, rows.Err()
}

func (d *DB) MarkDeleted(ctx context.Context, relPath string, deletedAt int64) error {
	_, err := d.MarkDeletedWithCache(ctx, relPath, deletedAt)
	return err
}

func (d *DB) MarkDeletedWithCache(ctx context.Context, relPath string, deletedAt int64) (*DeletedAsset, error) {
	var asset DeletedAsset
	err := d.conn.QueryRowContext(ctx, `
SELECT id, rel_path, cache_key, media_type
FROM assets
WHERE rel_path = ? AND deleted_at IS NULL`, relPath).Scan(&asset.ID, &asset.RelPath, &asset.CacheKey, &asset.MediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = d.conn.ExecContext(ctx, `
UPDATE media_asset
SET deleted = true, deleted_at = ?, updated_at = ?
WHERE id = ? AND deleted = false`,
		unixTime(deletedAt), unixTime(deletedAt), asset.ID)
	if err != nil {
		return nil, err
	}
	_, _ = d.conn.ExecContext(ctx, `UPDATE file_instance SET missing = true WHERE asset_id = ?`, asset.ID)
	return &asset, nil
}

func (d *DB) MarkDeletedUnder(ctx context.Context, relPath string, deletedAt int64) ([]DeletedAsset, error) {
	where := `deleted_at IS NULL AND rel_path = ?`
	args := []any{relPath}
	if relPath != "" {
		where = `deleted_at IS NULL AND (rel_path = ? OR rel_path LIKE ? ESCAPE '\')`
		args = []any{relPath, escapeLike(relPath) + "/%"}
	}
	return d.markDeletedWhere(ctx, deletedAt, where, args...)
}

func (d *DB) MarkAllDeleted(ctx context.Context, deletedAt int64) (int64, error) {
	items, err := d.MarkAllDeletedWithCache(ctx, deletedAt)
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (d *DB) MarkAllDeletedWithCache(ctx context.Context, deletedAt int64) ([]DeletedAsset, error) {
	return d.markDeletedWhere(ctx, deletedAt, `deleted_at IS NULL`)
}

func (d *DB) markDeletedWhere(ctx context.Context, deletedAt int64, where string, args ...any) ([]DeletedAsset, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT id, rel_path, cache_key, media_type
FROM assets
WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []DeletedAsset
	var ids []any
	for rows.Next() {
		var asset DeletedAsset
		if err := rows.Scan(&asset.ID, &asset.RelPath, &asset.CacheKey, &asset.MediaType); err != nil {
			return nil, err
		}
		items = append(items, asset)
		ids = append(ids, asset.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	updateArgs := make([]any, 0, len(ids)+2)
	updateArgs = append(updateArgs, unixTime(deletedAt), unixTime(deletedAt))
	for i, id := range ids {
		placeholders[i] = "?"
		updateArgs = append(updateArgs, id)
	}
	_, err = d.conn.ExecContext(ctx, `UPDATE media_asset SET deleted = true, deleted_at = ?, updated_at = ? WHERE deleted = false AND id IN (`+strings.Join(placeholders, ",")+`)`, updateArgs...)
	if err != nil {
		return nil, err
	}
	_, _ = d.conn.ExecContext(ctx, `UPDATE file_instance SET missing = true WHERE asset_id IN (`+strings.Join(placeholders, ",")+`)`, ids...)
	return items, nil
}

func (d *DB) SetAssetWorkStatus(ctx context.Context, assetID int64, field string, status string, message *string) error {
	if !validStatusField(field) {
		return fmt.Errorf("invalid status field %s", field)
	}
	now := util.UnixNow()
	readyField := readyColumnForStatus(field)
	if readyField != "" {
		_, err := d.conn.ExecContext(ctx, fmt.Sprintf(`UPDATE media_asset SET %s = ?, %s = ?, error_text = ?, updated_at = ? WHERE id = ?`, field, readyField),
			status, status == model.StatusReady, nullString(message), unixTime(now), assetID)
		if err != nil {
			return err
		}
	} else {
		_, err := d.conn.ExecContext(ctx, fmt.Sprintf(`UPDATE media_asset SET %s = ?, error_text = ?, updated_at = ? WHERE id = ?`, field),
			status, nullString(message), unixTime(now), assetID)
		if err != nil {
			return err
		}
	}
	if err := d.upsertMediaJob(ctx, assetID, field, status, message, now); err != nil {
		return err
	}
	if status == model.StatusReady {
		return d.upsertMediaVariant(ctx, assetID, field)
	}
	return nil
}

func (d *DB) ResetAssetThumbnail(ctx context.Context, assetID int64) error {
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
UPDATE media_asset
SET thumb_status = ?, video_poster_status = ?, thumb_ready = false, error_text = NULL, updated_at = ?
WHERE id = ? AND deleted = false`,
		model.StatusPending, model.StatusNotRequired, unixTime(now), assetID)
	return err
}

func (d *DB) ResetAssetThumbnailsForRoots(ctx context.Context, roots []string) (int, error) {
	where, args, err := assetRootsWhere(roots)
	if err != nil {
		return 0, err
	}
	now := util.UnixNow()
	queryArgs := []any{model.StatusPending, model.StatusNotRequired, unixTime(now)}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, model.StatusPending)
	var count int
	err = d.conn.QueryRowContext(ctx, `
WITH reset AS (
  UPDATE media_asset
  SET thumb_status = ?, video_poster_status = ?, thumb_ready = false, error_text = NULL, updated_at = ?
  WHERE deleted = false
    AND deleted_at IS NULL
    AND id IN (SELECT id FROM assets WHERE `+where+`)
  RETURNING id
), jobs AS (
  INSERT INTO media_job (asset_id, job_type, status, error_text, started_at, finished_at)
  SELECT id, 'thumb', ?, NULL, NULL, NULL FROM reset
  ON CONFLICT(asset_id, job_type) DO UPDATE SET
    status = excluded.status,
    error_text = excluded.error_text,
    started_at = NULL,
    finished_at = NULL
  RETURNING 1
)
SELECT COUNT(*) FROM reset`, queryArgs...).Scan(&count)
	return count, err
}

func (d *DB) ThumbnailWorkForRoots(ctx context.Context, roots []string) ([]WorkItem, error) {
	where, args, err := assetRootsWhere(roots)
	if err != nil {
		return nil, err
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT id FROM assets WHERE `+where+` AND media_type IN (?, ?)`, append(args, model.MediaTypeImage, model.MediaTypeVideo)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WorkItem
	for rows.Next() {
		var item WorkItem
		item.Type = "thumb"
		if err := rows.Scan(&item.AssetID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) EnableVideoProxies(ctx context.Context) error {
	now := util.UnixNow()
	rows, err := d.conn.QueryContext(ctx, `
UPDATE media_asset
SET video_proxy_status = ?, proxy_ready = false, error_text = NULL, updated_at = ?
WHERE deleted = false
  AND deleted_at IS NULL
  AND media_type = ?
  AND video_proxy_status = ?
RETURNING id`,
		model.StatusPending, unixTime(now), mediaTypeCode(model.MediaTypeVideo), model.StatusNotRequired)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID int64
		if err := rows.Scan(&assetID); err != nil {
			return err
		}
		if err := d.upsertMediaJob(ctx, assetID, "video_proxy_status", model.StatusPending, nil, now); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (d *DB) PendingWork(ctx context.Context, videoProxyEnabled bool) ([]WorkItem, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT id, media_type, thumb_status, preview_status, video_poster_status, video_proxy_status
FROM assets
WHERE deleted_at IS NULL AND (
  thumb_status IN ('pending','processing','error') OR
  preview_status IN ('pending','processing','error') OR
  video_proxy_status IN ('pending','processing','error')
)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WorkItem
	for rows.Next() {
		var id int64
		var mediaType, thumbStatus, previewStatus, posterStatus, proxyStatus string
		if err := rows.Scan(&id, &mediaType, &thumbStatus, &previewStatus, &posterStatus, &proxyStatus); err != nil {
			return nil, err
		}
		_ = posterStatus
		if recoverableWorkStatus(thumbStatus) {
			items = append(items, WorkItem{Type: "thumb", AssetID: id})
		}
		if mediaType == model.MediaTypeImage && recoverableWorkStatus(previewStatus) {
			items = append(items, WorkItem{Type: "preview", AssetID: id})
		}
		if mediaType == model.MediaTypeVideo {
			if videoProxyEnabled && recoverableWorkStatus(proxyStatus) {
				items = append(items, WorkItem{Type: "video_proxy", AssetID: id})
			}
		}
	}
	return items, rows.Err()
}

func recoverableWorkStatus(status string) bool {
	return status == model.StatusPending || status == model.StatusProcessing || status == model.StatusError
}

func (d *DB) Neighbors(ctx context.Context, opts NeighborOptions) (Neighbors, error) {
	if opts.Limit <= 0 {
		opts.Limit = 5
	}
	current, err := d.GetAsset(ctx, opts.AssetID)
	if err != nil {
		return Neighbors{}, err
	}
	filterOpts := AssetListOptions{
		Type: opts.Type, Sort: opts.Sort, Query: opts.Query, From: opts.From, To: opts.To, VisibleOnly: true,
		NFOQuery: opts.NFOQuery, NFOActor: opts.NFOActor, NFOID: opts.NFOID, NFOTag: opts.NFOTag, NFOTitle: opts.NFOTitle, NFOYear: opts.NFOYear,
		MinWidth: opts.MinWidth, MaxWidth: opts.MaxWidth, MinHeight: opts.MinHeight, MaxHeight: opts.MaxHeight, MatchAnyAxis: opts.MatchAnyAxis,
		MinDuration: opts.MinDuration, MaxDuration: opts.MaxDuration, MinSize: opts.MinSize, MaxSize: opts.MaxSize, Orientation: opts.Orientation,
	}
	if opts.Context == "folder" {
		if opts.FolderID == nil {
			return Neighbors{}, errors.New("folderId is required")
		}
		folder, err := d.GetFolder(ctx, *opts.FolderID)
		if err != nil {
			return Neighbors{}, err
		}
		filterOpts.FolderRel = &folder.RelPath
		filterOpts.Recursive = opts.Recursive
	}
	if opts.Context == "timeline" {
		filterOpts.Sort = "timeline_desc"
	}
	where, args := assetFilterSQL(filterOpts, opts.Context == "timeline" || opts.Context == "search")
	prevCond, prevArgs, prevOrder := neighborCondition(current, filterOpts.Sort, true)
	nextCond, nextArgs, nextOrder := neighborCondition(current, filterOpts.Sort, false)
	previous, err := d.neighborSide(ctx, where, args, prevCond, prevArgs, prevOrder, opts.Limit)
	if err != nil {
		return Neighbors{}, err
	}
	next, err := d.neighborSide(ctx, where, args, nextCond, nextArgs, nextOrder, opts.Limit)
	if err != nil {
		return Neighbors{}, err
	}
	return Neighbors{Current: current, Previous: previous, Next: next}, nil
}

func (d *DB) neighborSide(ctx context.Context, where string, args []any, condition string, conditionArgs []any, order string, limit int) ([]model.Asset, error) {
	query := assetSelectSQL() + " WHERE " + where + " AND (" + condition + ") ORDER BY " + order + " LIMIT ?"
	allArgs := append([]any{}, args...)
	allArgs = append(allArgs, conditionArgs...)
	allArgs = append(allArgs, limit)
	rows, err := d.conn.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssetRows(rows)
}

func assetFilterSQL(opts AssetListOptions, timeline bool) (string, []any) {
	where := []string{"deleted_at IS NULL"}
	var args []any
	if opts.VisibleOnly {
		where = append(where, "thumb_status = 'ready'")
	}
	switch opts.Type {
	case model.MediaTypeImage, model.MediaTypeVideo:
		where = append(where, "media_type = ?")
		args = append(args, opts.Type)
	}
	if opts.Query != "" {
		where = append(where, "lower(filename) LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(strings.ToLower(opts.Query))+"%")
	}
	if opts.NFOQuery != "" {
		where = append(where, "nfo_search_text IS NOT NULL AND lower(nfo_search_text) LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(strings.ToLower(opts.NFOQuery))+"%")
	}
	if condition, conditionArgs := nfoValueSearchCondition("actor", opts.NFOActor); condition != "" {
		where = append(where, condition)
		args = append(args, conditionArgs...)
	}
	if condition, conditionArgs := nfoValueSearchCondition("id", opts.NFOID); condition != "" {
		where = append(where, condition)
		args = append(args, conditionArgs...)
	}
	if condition, conditionArgs := nfoValueSearchCondition("tag", opts.NFOTag); condition != "" {
		where = append(where, condition)
		args = append(args, conditionArgs...)
	}
	if condition, conditionArgs := nfoValueSearchCondition("title", opts.NFOTitle); condition != "" {
		where = append(where, condition)
		args = append(args, conditionArgs...)
	}
	if condition, conditionArgs := nfoValueSearchCondition("year", opts.NFOYear); condition != "" {
		where = append(where, condition)
		args = append(args, conditionArgs...)
	}
	if condition, conditionArgs := dimensionFilterSQL(opts); condition != "" {
		where = append(where, condition)
		args = append(args, conditionArgs...)
	}
	if opts.MinDuration != nil {
		where = append(where, "duration IS NOT NULL AND duration >= ?")
		args = append(args, *opts.MinDuration)
	}
	if opts.MaxDuration != nil {
		where = append(where, "duration IS NOT NULL AND duration <= ?")
		args = append(args, *opts.MaxDuration)
	}
	if opts.MinSize != nil {
		where = append(where, "size >= ?")
		args = append(args, *opts.MinSize)
	}
	if opts.MaxSize != nil {
		where = append(where, "size <= ?")
		args = append(args, *opts.MaxSize)
	}
	switch opts.Orientation {
	case "landscape":
		where = append(where, "width IS NOT NULL AND height IS NOT NULL AND width > height")
	case "portrait":
		where = append(where, "width IS NOT NULL AND height IS NOT NULL AND height > width")
	}
	if opts.FolderRel != nil {
		if opts.Recursive {
			if *opts.FolderRel != "" {
				where = append(where, "(parent_rel_path = ? OR parent_rel_path LIKE ? ESCAPE '\\')")
				args = append(args, *opts.FolderRel, descendantPathLike(*opts.FolderRel))
			}
		} else {
			where = append(where, "parent_rel_path = ?")
			args = append(args, *opts.FolderRel)
		}
	}
	if timeline {
		if opts.From != nil {
			where = append(where, "timeline_at >= ?")
			args = append(args, *opts.From)
		}
		if opts.To != nil {
			where = append(where, "timeline_at <= ?")
			args = append(args, *opts.To)
		}
	}
	return strings.Join(where, " AND "), args
}

func dimensionFilterSQL(opts AssetListOptions) (string, []any) {
	if opts.MinWidth == nil && opts.MaxWidth == nil && opts.MinHeight == nil && opts.MaxHeight == nil {
		return "", nil
	}
	primary, primaryArgs := axisDimensionFilterSQL("width", "height", opts)
	if !opts.MatchAnyAxis {
		return primary, primaryArgs
	}
	swapped, swappedArgs := axisDimensionFilterSQL("height", "width", opts)
	if primary == "" {
		return swapped, swappedArgs
	}
	if swapped == "" {
		return primary, primaryArgs
	}
	args := append([]any{}, primaryArgs...)
	args = append(args, swappedArgs...)
	return "((" + primary + ") OR (" + swapped + "))", args
}

func axisDimensionFilterSQL(widthColumn string, heightColumn string, opts AssetListOptions) (string, []any) {
	var parts []string
	var args []any
	if opts.MinWidth != nil {
		parts = append(parts, widthColumn+" IS NOT NULL AND "+widthColumn+" >= ?")
		args = append(args, *opts.MinWidth)
	}
	if opts.MaxWidth != nil {
		parts = append(parts, widthColumn+" IS NOT NULL AND "+widthColumn+" <= ?")
		args = append(args, *opts.MaxWidth)
	}
	if opts.MinHeight != nil {
		parts = append(parts, heightColumn+" IS NOT NULL AND "+heightColumn+" >= ?")
		args = append(args, *opts.MinHeight)
	}
	if opts.MaxHeight != nil {
		parts = append(parts, heightColumn+" IS NOT NULL AND "+heightColumn+" <= ?")
		args = append(args, *opts.MaxHeight)
	}
	return strings.Join(parts, " AND "), args
}

func nfoValueSearchCondition(field string, query string) (string, []any) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}
	filter, ok := nfoFieldFilterSQL(field)
	if !ok {
		return "", nil
	}
	return `nfo_json IS NOT NULL AND EXISTS (
  SELECT 1
  FROM jsonb_array_elements(COALESCE(nfo_json::jsonb->'groups', '[]'::jsonb)) AS nfo_group(group_value)
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(nfo_group.group_value->'items', '[]'::jsonb)) AS nfo_item(item_value)
  WHERE (` + filter + `)
    AND lower(COALESCE(nfo_item.item_value->>'value', '')) LIKE ? ESCAPE '\'
)`, []any{"%" + escapeLike(strings.ToLower(query)) + "%"}
}

func nfoFieldFilterSQL(field string) (string, bool) {
	switch field {
	case "actor":
		return "lower(COALESCE(nfo_group.group_value->>'title', '')) = '演员' OR lower(COALESCE(nfo_item.item_value->>'key', '')) = 'actor'", true
	case "id":
		return "lower(COALESCE(nfo_group.group_value->>'title', '')) = 'id' OR lower(COALESCE(nfo_item.item_value->>'key', '')) = 'uniqueid' OR lower(COALESCE(nfo_item.item_value->>'key', '')) LIKE 'uniqueid:%'", true
	case "tag":
		return "lower(COALESCE(nfo_group.group_value->>'title', '')) = '标记' OR lower(COALESCE(nfo_item.item_value->>'key', '')) IN ('tag', 'genre')", true
	case "title":
		return "lower(COALESCE(nfo_item.item_value->>'key', '')) IN ('title', 'originaltitle', 'sorttitle')", true
	case "year":
		return "lower(COALESCE(nfo_item.item_value->>'key', '')) = 'year'", true
	default:
		return "", false
	}
}

func sortSQL(sort string) string {
	switch sort {
	case "timeline_asc":
		return "timeline_at ASC, id ASC"
	case "filename", "filename_asc":
		return "lower(filename) ASC, id ASC"
	case "filename_desc":
		return "lower(filename) DESC, id DESC"
	case "size", "size_desc":
		return "size DESC, id DESC"
	case "size_asc":
		return "size ASC, id ASC"
	case "imported_asc":
		return "imported_at ASC, id ASC"
	case "imported_desc":
		return "imported_at DESC, id DESC"
	default:
		return "timeline_at DESC, id DESC"
	}
}

func anchorParts(sort string, item libraryAnchorRow) (string, string, string, int64) {
	switch sort {
	case "filename", "filename_asc", "filename_desc":
		label := filenameAnchorLabel(item.Filename)
		return "name:" + label, label, "letter", 0
	case "size", "size_asc", "size_desc":
		label := sizeAnchorLabel(item.Size)
		return "size:" + label, label, "size", item.Size
	case "imported_asc", "imported_desc":
		return dateAnchorParts(item.ImportedAt)
	case "timeline_asc", "timeline_desc":
		return dateAnchorParts(item.TimelineAt)
	default:
		return dateAnchorParts(item.TimelineAt)
	}
}

func dateAnchorParts(unix int64) (string, string, string, int64) {
	t := time.Unix(unix, 0).Local()
	label := t.Format("2006-01-02")
	return "date:" + label, label, "day", unix
}

func dateAnchorGroups(unix int64) (string, string) {
	t := time.Unix(unix, 0).Local()
	return t.Format("2006"), t.Format("2006-01")
}

func isTimeSort(sort string) bool {
	return sort == "timeline_asc" || sort == "timeline_desc" || sort == "imported_asc" || sort == "imported_desc" || sort == ""
}

func filenameAnchorLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "#"
	}
	r, _ := utf8.DecodeRuneInString(name)
	r = unicode.ToUpper(r)
	if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return string(r)
	}
	return string(r)
}

func sizeAnchorLabel(size int64) string {
	const mb = 1024 * 1024
	switch {
	case size >= 2000*mb:
		return "2000M+"
	case size >= 1000*mb:
		return "1000M+"
	case size >= 500*mb:
		return "500M+"
	case size >= 100*mb:
		return "100M+"
	case size >= 10*mb:
		return "10M+"
	case size >= mb:
		return "1M+"
	default:
		return "<1M"
	}
}

func formatAnchorSize(size int64) string {
	if size < 0 {
		size = 0
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case size >= 10*gb:
		return fmt.Sprintf("%dGB", size/gb)
	case size >= gb:
		return fmt.Sprintf("%.1fGB", float64(size)/float64(gb))
	case size >= 10*mb:
		return fmt.Sprintf("%dMB", size/mb)
	case size >= mb:
		return fmt.Sprintf("%.1fMB", float64(size)/float64(mb))
	case size >= 10*kb:
		return fmt.Sprintf("%dKB", size/kb)
	case size >= kb:
		return fmt.Sprintf("%.1fKB", float64(size)/float64(kb))
	default:
		return fmt.Sprintf("%dB", size)
	}
}

func neighborCondition(current model.Asset, sort string, previous bool) (string, []any, string) {
	switch sort {
	case "timeline_asc":
		if previous {
			return "(timeline_at < ? OR (timeline_at = ? AND id < ?))", []any{current.TimelineAt, current.TimelineAt, current.ID}, "timeline_at DESC, id DESC"
		}
		return "(timeline_at > ? OR (timeline_at = ? AND id > ?))", []any{current.TimelineAt, current.TimelineAt, current.ID}, "timeline_at ASC, id ASC"
	case "filename", "filename_asc":
		name := strings.ToLower(current.Filename)
		if previous {
			return "(lower(filename) < ? OR (lower(filename) = ? AND id < ?))", []any{name, name, current.ID}, "lower(filename) DESC, id DESC"
		}
		return "(lower(filename) > ? OR (lower(filename) = ? AND id > ?))", []any{name, name, current.ID}, "lower(filename) ASC, id ASC"
	case "filename_desc":
		name := strings.ToLower(current.Filename)
		if previous {
			return "(lower(filename) > ? OR (lower(filename) = ? AND id > ?))", []any{name, name, current.ID}, "lower(filename) ASC, id ASC"
		}
		return "(lower(filename) < ? OR (lower(filename) = ? AND id < ?))", []any{name, name, current.ID}, "lower(filename) DESC, id DESC"
	case "size", "size_desc":
		if previous {
			return "(size > ? OR (size = ? AND id > ?))", []any{current.Size, current.Size, current.ID}, "size ASC, id ASC"
		}
		return "(size < ? OR (size = ? AND id < ?))", []any{current.Size, current.Size, current.ID}, "size DESC, id DESC"
	case "size_asc":
		if previous {
			return "(size < ? OR (size = ? AND id < ?))", []any{current.Size, current.Size, current.ID}, "size DESC, id DESC"
		}
		return "(size > ? OR (size = ? AND id > ?))", []any{current.Size, current.Size, current.ID}, "size ASC, id ASC"
	case "imported_asc":
		if previous {
			return "(imported_at < ? OR (imported_at = ? AND id < ?))", []any{current.ImportedAt, current.ImportedAt, current.ID}, "imported_at DESC, id DESC"
		}
		return "(imported_at > ? OR (imported_at = ? AND id > ?))", []any{current.ImportedAt, current.ImportedAt, current.ID}, "imported_at ASC, id ASC"
	case "imported_desc":
		if previous {
			return "(imported_at > ? OR (imported_at = ? AND id > ?))", []any{current.ImportedAt, current.ImportedAt, current.ID}, "imported_at ASC, id ASC"
		}
		return "(imported_at < ? OR (imported_at = ? AND id < ?))", []any{current.ImportedAt, current.ImportedAt, current.ID}, "imported_at DESC, id DESC"
	default:
		if previous {
			return "(timeline_at > ? OR (timeline_at = ? AND id > ?))", []any{current.TimelineAt, current.TimelineAt, current.ID}, "timeline_at ASC, id ASC"
		}
		return "(timeline_at < ? OR (timeline_at = ? AND id < ?))", []any{current.TimelineAt, current.TimelineAt, current.ID}, "timeline_at DESC, id DESC"
	}
}

func assetSelectSQL() string {
	return `SELECT id, rel_path, parent_rel_path, filename, ext, media_type, mime_type, size, mtime,
width, height, duration, taken_at, imported_at, timeline_at, cache_key, browser_playable,
scan_status, thumb_status, preview_status, video_poster_status, video_proxy_status,
COALESCE((SELECT rotation FROM asset_preferences WHERE asset_id = assets.id), 0) AS rotation,
metadata_json, nfo_json, nfo_search_text, error, deleted_at, created_at, updated_at FROM assets`
}

func scanAsset(row interface{ Scan(dest ...any) error }) (model.Asset, error) {
	var asset model.Asset
	var mime, metadata, nfoJSON, nfoSearchText, errorText sql.NullString
	var width, height, takenAt, deletedAt sql.NullInt64
	var duration sql.NullFloat64
	var browserPlayable int
	err := row.Scan(&asset.ID, &asset.RelPath, &asset.ParentRelPath, &asset.Filename, &asset.Ext, &asset.MediaType, &mime, &asset.Size, &asset.Mtime,
		&width, &height, &duration, &takenAt, &asset.ImportedAt, &asset.TimelineAt, &asset.CacheKey, &browserPlayable,
		&asset.ScanStatus, &asset.ThumbStatus, &asset.PreviewStatus, &asset.VideoPosterStatus, &asset.VideoProxyStatus,
		&asset.Rotation, &metadata, &nfoJSON, &nfoSearchText, &errorText, &deletedAt, &asset.CreatedAt, &asset.UpdatedAt)
	if err != nil {
		return model.Asset{}, err
	}
	asset.MimeType = stringPtr(mime)
	asset.Width = intPtr(width)
	asset.Height = intPtr(height)
	asset.Duration = floatPtr(duration)
	asset.TakenAt = int64Ptr(takenAt)
	asset.MetadataJSON = stringPtr(metadata)
	asset.NFOJSON = stringPtr(nfoJSON)
	asset.NFOSearchText = stringPtr(nfoSearchText)
	asset.Error = stringPtr(errorText)
	asset.DeletedAt = int64Ptr(deletedAt)
	asset.BrowserPlayable = browserPlayable == 1
	asset.Rotation = NormalizeRotation(asset.Rotation)
	return asset, nil
}

func scanAssetRows(rows *sql.Rows) ([]model.Asset, error) {
	var items []model.Asset
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, asset)
	}
	return items, rows.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nfoColumnsEqual(existing sql.NullString, next *string) bool {
	if next == nil {
		return !existing.Valid
	}
	return existing.Valid && existing.String == *next
}

func nullInt64Equal(existing sql.NullInt64, next *int64) bool {
	if next == nil {
		return !existing.Valid
	}
	return existing.Valid && existing.Int64 == *next
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func descendantPathBounds(rel string) (string, string) {
	lower := rel + "/"
	bytes := []byte(lower)
	for i := len(bytes) - 1; i >= 0; i-- {
		if bytes[i] == 0xff {
			continue
		}
		bytes[i]++
		return lower, string(bytes[:i+1])
	}
	return lower, lower + "\x00"
}

func descendantPathLike(rel string) string {
	return escapeLike(rel) + "/%"
}

func validStatusField(field string) bool {
	switch field {
	case "thumb_status", "preview_status", "video_poster_status", "video_proxy_status":
		return true
	default:
		return false
	}
}

func mediaTypeCode(value string) int {
	switch value {
	case model.MediaTypeVideo:
		return 2
	default:
		return 1
	}
}

func unixTime(value int64) time.Time {
	return time.Unix(value, 0).UTC()
}

func unixTimePtr(value *int64) any {
	if value == nil || *value == 0 {
		return nil
	}
	return unixTime(*value)
}

func durationMillis(value *float64) any {
	if value == nil {
		return nil
	}
	return int64(*value * 1000)
}

func aspectRatio(width *int, height *int) any {
	if width == nil || height == nil || *height == 0 {
		return nil
	}
	return float64(*width) / float64(*height)
}

func folderIDForRel(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, rel string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM folder WHERE library_id = 1 AND rel_path = ?`, rel).Scan(&id)
	return id, err
}

func readyColumnForStatus(field string) string {
	switch field {
	case "thumb_status", "video_poster_status":
		return "thumb_ready"
	case "preview_status":
		return "preview_ready"
	case "video_proxy_status":
		return "proxy_ready"
	default:
		return ""
	}
}

func variantTypeForStatus(field string) (int, string, bool) {
	switch field {
	case "thumb_status":
		return 1, "thumbs", true
	case "video_poster_status":
		return 4, "thumbs", true
	case "preview_status":
		return 3, "previews", true
	case "video_proxy_status":
		return 6, "video-proxies", true
	default:
		return 0, "", false
	}
}

func variantExt(field string) string {
	if field == "video_proxy_status" {
		return "mp4"
	}
	return "webp"
}

func (d *DB) upsertMediaVariant(ctx context.Context, assetID int64, field string) error {
	variantType, dir, ok := variantTypeForStatus(field)
	if !ok {
		return nil
	}
	var cacheKey string
	if err := d.conn.QueryRowContext(ctx, `SELECT cache_key FROM media_asset WHERE id = ?`, assetID).Scan(&cacheKey); err != nil {
		return err
	}
	path := fmt.Sprintf("%s/%s.%s", dir, cacheKey, variantExt(field))
	_, err := d.conn.ExecContext(ctx, `
INSERT INTO media_variant (asset_id, variant_type, path, ready, generated_at)
VALUES (?, ?, ?, true, now())
ON CONFLICT(asset_id, variant_type) DO UPDATE SET
  path = excluded.path,
  ready = true,
  generated_at = excluded.generated_at`,
		assetID, variantType, path)
	return err
}

func (d *DB) upsertMediaJob(ctx context.Context, assetID int64, field string, status string, message *string, now int64) error {
	jobType := strings.TrimSuffix(field, "_status")
	startedAt := any(nil)
	finishedAt := any(nil)
	if status == model.StatusProcessing {
		startedAt = unixTime(now)
	}
	if status == model.StatusReady || status == model.StatusError || status == model.StatusNotRequired {
		finishedAt = unixTime(now)
	}
	_, err := d.conn.ExecContext(ctx, `
INSERT INTO media_job (asset_id, job_type, status, error_text, started_at, finished_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(asset_id, job_type) DO UPDATE SET
  status = excluded.status,
  error_text = excluded.error_text,
  started_at = COALESCE(excluded.started_at, media_job.started_at),
  finished_at = excluded.finished_at`,
		assetID, jobType, status, nullString(message), startedAt, finishedAt)
	return err
}

func ParentFolderRel(rel string) string {
	parent := path.Dir(rel)
	if parent == "." || parent == "/" {
		return ""
	}
	return parent
}

func AssetStatuses(mediaType string, browserPlayable bool, proxyEnabled bool) (thumb, preview, poster, proxy string) {
	if mediaType == model.MediaTypeImage {
		previewStatus := model.StatusNotRequired
		if !browserPlayable {
			previewStatus = model.StatusPending
		}
		return model.StatusPending, previewStatus, model.StatusNotRequired, model.StatusNotRequired
	}
	if mediaType == model.MediaTypeVideo {
		proxyStatus := model.StatusNotRequired
		if proxyEnabled {
			proxyStatus = model.StatusPending
		}
		return model.StatusPending, model.StatusNotRequired, model.StatusNotRequired, proxyStatus
	}
	return model.StatusNotRequired, model.StatusNotRequired, model.StatusNotRequired, model.StatusNotRequired
}

func BuildAssetUpsert(rel string, mediaType string) (AssetUpsert, error) {
	normalized, err := storage.NormalizeRelPath(rel)
	if err != nil {
		return AssetUpsert{}, err
	}
	return AssetUpsert{
		RelPath:       normalized,
		ParentRelPath: storage.ParentRelPath(normalized),
		Filename:      path.Base(normalized),
		MediaType:     mediaType,
	}, nil
}
