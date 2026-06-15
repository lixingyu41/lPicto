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
	NFOScanned        bool
	Error             *string
}

type AssetListOptions struct {
	Page        int
	PageSize    int
	Type        string
	Sort        string
	Query       string
	FolderID    *int64
	From        *int64
	To          *int64
	FolderRel   *string
	Recursive   bool
	VisibleOnly bool
	NFOQuery    string
	MinWidth    *int
	MaxWidth    *int
	MinHeight   *int
	MaxHeight   *int
	MinDuration *float64
	MaxDuration *float64
	MinSize     *int64
	MaxSize     *int64
	Orientation string
}

type NeighborOptions struct {
	Context     string
	AssetID     int64
	Type        string
	Sort        string
	Query       string
	FolderID    *int64
	From        *int64
	To          *int64
	Limit       int
	Recursive   bool
	NFOQuery    string
	MinWidth    *int
	MaxWidth    *int
	MinHeight   *int
	MaxHeight   *int
	MinDuration *float64
	MaxDuration *float64
	MinSize     *int64
	MaxSize     *int64
	Orientation string
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
	var existingID int64
	var existingSize int64
	var existingMtime int64
	var existingCacheKey string
	var deletedAt sql.NullInt64
	var existingNFOJSON sql.NullString
	var existingNFOSearchText sql.NullString
	err := d.conn.QueryRowContext(ctx, `SELECT id, size, mtime, cache_key, deleted_at, nfo_json, nfo_search_text FROM assets WHERE rel_path = ?`, p.RelPath).Scan(&existingID, &existingSize, &existingMtime, &existingCacheKey, &deletedAt, &existingNFOJSON, &existingNFOSearchText)
	if errors.Is(err, sql.ErrNoRows) {
		result, err := d.conn.ExecContext(ctx, `
INSERT INTO assets (
  rel_path, parent_rel_path, filename, ext, media_type, mime_type, size, mtime,
  width, height, duration, taken_at, imported_at, timeline_at, cache_key,
  browser_playable, scan_status, thumb_status, preview_status, video_poster_status,
  video_proxy_status, metadata_json, nfo_json, nfo_search_text, error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.RelPath, p.ParentRelPath, p.Filename, p.Ext, p.MediaType, nullString(p.MimeType), p.Size, p.Mtime,
			nullInt(p.Width), nullInt(p.Height), nullFloat(p.Duration), nullInt64(p.TakenAt), p.ImportedAt, p.TimelineAt, p.CacheKey,
			boolInt(p.BrowserPlayable), model.StatusOK, p.ThumbStatus, p.PreviewStatus, p.VideoPosterStatus,
			p.VideoProxyStatus, nullString(p.MetadataJSON), nullString(p.NFOJSON), nullString(p.NFOSearchText), nullString(p.Error), now, now)
		if err != nil {
			return AssetUpsertResult{}, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return AssetUpsertResult{}, err
		}
		return AssetUpsertResult{ID: id, Added: true}, nil
	}
	if err != nil {
		return AssetUpsertResult{}, err
	}
	if existingSize == p.Size && existingMtime == p.Mtime && !deletedAt.Valid {
		if p.NFOScanned {
			if nfoColumnsEqual(existingNFOJSON, p.NFOJSON) && nfoColumnsEqual(existingNFOSearchText, p.NFOSearchText) {
				return AssetUpsertResult{ID: existingID}, nil
			}
			_, err = d.conn.ExecContext(ctx, `UPDATE assets SET nfo_json = ?, nfo_search_text = ?, updated_at = ? WHERE id = ?`,
				nullString(p.NFOJSON), nullString(p.NFOSearchText), now, existingID)
			if err != nil {
				return AssetUpsertResult{}, err
			}
			return AssetUpsertResult{ID: existingID, Updated: true}, nil
		}
		return AssetUpsertResult{ID: existingID}, nil
	}
	if p.NFOScanned {
		_, err = d.conn.ExecContext(ctx, `
UPDATE assets SET
  parent_rel_path = ?, filename = ?, ext = ?, media_type = ?, mime_type = ?,
  size = ?, mtime = ?, width = ?, height = ?, duration = ?, taken_at = ?,
  timeline_at = ?, cache_key = ?, browser_playable = ?, scan_status = ?,
  thumb_status = ?, preview_status = ?, video_poster_status = ?, video_proxy_status = ?,
  metadata_json = ?, nfo_json = ?, nfo_search_text = ?, error = ?, deleted_at = NULL, updated_at = ?
WHERE id = ?`,
			p.ParentRelPath, p.Filename, p.Ext, p.MediaType, nullString(p.MimeType),
			p.Size, p.Mtime, nullInt(p.Width), nullInt(p.Height), nullFloat(p.Duration), nullInt64(p.TakenAt),
			p.TimelineAt, p.CacheKey, boolInt(p.BrowserPlayable), model.StatusOK,
			p.ThumbStatus, p.PreviewStatus, p.VideoPosterStatus, p.VideoProxyStatus,
			nullString(p.MetadataJSON), nullString(p.NFOJSON), nullString(p.NFOSearchText), nullString(p.Error), now, existingID)
	} else {
		_, err = d.conn.ExecContext(ctx, `
UPDATE assets SET
  parent_rel_path = ?, filename = ?, ext = ?, media_type = ?, mime_type = ?,
  size = ?, mtime = ?, width = ?, height = ?, duration = ?, taken_at = ?,
  timeline_at = ?, cache_key = ?, browser_playable = ?, scan_status = ?,
  thumb_status = ?, preview_status = ?, video_poster_status = ?, video_proxy_status = ?,
  metadata_json = ?, error = ?, deleted_at = NULL, updated_at = ?
WHERE id = ?`,
			p.ParentRelPath, p.Filename, p.Ext, p.MediaType, nullString(p.MimeType),
			p.Size, p.Mtime, nullInt(p.Width), nullInt(p.Height), nullFloat(p.Duration), nullInt64(p.TakenAt),
			p.TimelineAt, p.CacheKey, boolInt(p.BrowserPlayable), model.StatusOK,
			p.ThumbStatus, p.PreviewStatus, p.VideoPosterStatus, p.VideoProxyStatus,
			nullString(p.MetadataJSON), nullString(p.Error), now, existingID)
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

func (d *DB) AssetHasNFO(ctx context.Context, relPath string) (bool, error) {
	var value sql.NullString
	err := d.conn.QueryRowContext(ctx, `SELECT nfo_json FROM assets WHERE rel_path = ?`, relPath).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value.Valid && strings.TrimSpace(value.String) != "", nil
}

func (d *DB) GetAsset(ctx context.Context, id int64) (model.Asset, error) {
	row := d.conn.QueryRowContext(ctx, assetSelectSQL()+` WHERE id = ? AND deleted_at IS NULL`, id)
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

func (d *DB) LibraryAnchors(ctx context.Context, opts AssetListOptions) (LibraryAnchorResult, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	where, args := assetFilterSQL(opts, false)
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.PageSize)
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
	_, err = d.conn.ExecContext(ctx, `UPDATE assets SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, deletedAt, deletedAt, asset.ID)
	if err != nil {
		return nil, err
	}
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
	updateArgs = append(updateArgs, deletedAt, deletedAt)
	for i, id := range ids {
		placeholders[i] = "?"
		updateArgs = append(updateArgs, id)
	}
	_, err = d.conn.ExecContext(ctx, `UPDATE assets SET deleted_at = ?, updated_at = ? WHERE deleted_at IS NULL AND id IN (`+strings.Join(placeholders, ",")+`)`, updateArgs...)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (d *DB) SetAssetWorkStatus(ctx context.Context, assetID int64, field string, status string, message *string) error {
	if !validStatusField(field) {
		return fmt.Errorf("invalid status field %s", field)
	}
	now := util.UnixNow()
	if message == nil {
		_, err := d.conn.ExecContext(ctx, fmt.Sprintf(`UPDATE assets SET %s = ?, error = NULL, updated_at = ? WHERE id = ?`, field), status, now, assetID)
		return err
	}
	_, err := d.conn.ExecContext(ctx, fmt.Sprintf(`UPDATE assets SET %s = ?, error = ?, updated_at = ? WHERE id = ?`, field), status, *message, now, assetID)
	return err
}

func (d *DB) ResetAssetThumbnail(ctx context.Context, assetID int64) error {
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
UPDATE assets
SET thumb_status = ?, video_poster_status = ?, error = NULL, updated_at = ?
WHERE id = ? AND deleted_at IS NULL`,
		model.StatusPending, model.StatusNotRequired, now, assetID)
	return err
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
		NFOQuery: opts.NFOQuery, MinWidth: opts.MinWidth, MaxWidth: opts.MaxWidth, MinHeight: opts.MinHeight, MaxHeight: opts.MaxHeight,
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
	if opts.MinWidth != nil {
		where = append(where, "width IS NOT NULL AND width >= ?")
		args = append(args, *opts.MinWidth)
	}
	if opts.MaxWidth != nil {
		where = append(where, "width IS NOT NULL AND width <= ?")
		args = append(args, *opts.MaxWidth)
	}
	if opts.MinHeight != nil {
		where = append(where, "height IS NOT NULL AND height >= ?")
		args = append(args, *opts.MinHeight)
	}
	if opts.MaxHeight != nil {
		where = append(where, "height IS NOT NULL AND height <= ?")
		args = append(args, *opts.MaxHeight)
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
				lower, upper := descendantPathBounds(*opts.FolderRel)
				where = append(where, "(parent_rel_path = ? OR (parent_rel_path >= ? AND parent_rel_path < ?))")
				args = append(args, *opts.FolderRel, lower, upper)
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

func validStatusField(field string) bool {
	switch field {
	case "thumb_status", "preview_status", "video_poster_status", "video_proxy_status":
		return true
	default:
		return false
	}
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
		if proxyEnabled && !browserPlayable {
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
