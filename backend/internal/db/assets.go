package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"time"

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
	FPS               *float64
	VideoCodec        *string
	AudioCodec        *string
	Container         *string
	VideoBitrate      *int64
	AudioBitrate      *int64
	OverallBitrate    *int64
	TakenAt           *int64
	ImportedAt        int64
	TimelineAt        int64
	SHA256            []byte
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
	HasSubtitle       bool
	HasDanmaku        bool
	Error             *string
}

type AssetSignature struct {
	ID          int64
	Size        int64
	Mtime       int64
	NFOSize     *int64
	NFOMtime    *int64
	HasNFO      bool
	HasSubtitle bool
	HasDanmaku  bool
}

type AssetListOptions struct {
	Page            int
	PageSize        int
	Type            string
	Sort            string
	Group           string
	Query           string
	CombinedQuery   string
	FolderID        *int64
	From            *int64
	To              *int64
	FolderRel       *string
	FolderIDs       []int64
	Recursive       bool
	VisibleOnly     bool
	NFOQuery        string
	NFOActor        string
	NFOID           string
	NFOTag          string
	ManualTag       string
	CombinedTag     string
	CombinedTags    []string
	TagNodes        []string
	AIDescription   string
	AITag           string
	NFOTitle        string
	NFOYear         string
	MinWidth        *int
	MaxWidth        *int
	MinHeight       *int
	MaxHeight       *int
	MatchAnyAxis    bool
	MinDuration     *float64
	MaxDuration     *float64
	MinSize         *int64
	MaxSize         *int64
	Orientation     string
	Rating          *int
	AlbumUnassigned bool
	AlbumIDs        []int64
	IncludeHidden   bool
	PlayedOnly      bool
}

type NeighborOptions struct {
	Context         string
	AssetID         int64
	Type            string
	Sort            string
	Group           string
	Query           string
	CombinedQuery   string
	FolderID        *int64
	From            *int64
	To              *int64
	Limit           int
	Recursive       bool
	NFOQuery        string
	NFOActor        string
	NFOID           string
	NFOTag          string
	ManualTag       string
	CombinedTag     string
	CombinedTags    []string
	TagNodes        []string
	AIDescription   string
	AITag           string
	NFOTitle        string
	NFOYear         string
	MinWidth        *int
	MaxWidth        *int
	MinHeight       *int
	MaxHeight       *int
	MatchAnyAxis    bool
	MinDuration     *float64
	MaxDuration     *float64
	MinSize         *int64
	MaxSize         *int64
	Orientation     string
	Rating          *int
	AlbumUnassigned bool
	AlbumIDs        []int64
	IncludeHidden   bool
	VisibleOnly     bool
	PlayedOnly      bool
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
	Filename        string
	FilenameSortKey string
	ParentRelPath   string
	ImportedAt      int64
	Size            int64
	TimelineAt      int64
	LastPlayedAt    *int64
}

const assetGroupFolder = "folder"

func (d *DB) UpsertAsset(ctx context.Context, p AssetUpsert) (id int64, added bool, updated bool, err error) {
	result, err := d.UpsertAssetDetailed(ctx, p)
	if err != nil {
		return 0, false, false, err
	}
	return result.ID, result.Added, result.Updated, nil
}

func (d *DB) UpsertAssetDetailed(ctx context.Context, p AssetUpsert) (AssetUpsertResult, error) {
	now := util.UnixNow()
	sortKey := filenameSortKey(p.Filename)
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
  duration_ms, fps, video_codec, audio_codec, container, video_bitrate, audio_bitrate, overall_bitrate,
  size_bytes, file_mtime, captured_at, imported_at, sort_time,
  sha256, folder_id, metadata_json, nfo_json, nfo_search_text, cache_key, filename_sort_key, browser_playable,
  thumb_ready, preview_ready, proxy_ready, thumb_status, preview_status,
  video_poster_status, video_proxy_status, nfo_size, nfo_mtime, has_subtitle, has_danmaku, error_text, created_at, updated_at
) VALUES (
  ?, 0, ?, ?, ?, ?, ?, ?,
  ?, ?, ?, ?, ?, ?, ?, ?,
  ?, ?, ?, ?, ?, ?, ?,
  ?::jsonb, ?::jsonb, ?, ?, ?, ?,
  ?, ?, ?, ?, ?, ?, ?,
  ?, ?, ?, ?, ?, ?, ?
)
RETURNING id`,
			mediaTypeCode(p.MediaType), p.Filename, p.Ext, nullString(p.MimeType), nullInt(p.Width), nullInt(p.Height), aspectRatio(p.Width, p.Height),
			durationMillis(p.Duration), nullFloat64(p.FPS), nullString(p.VideoCodec), nullString(p.AudioCodec), nullString(p.Container),
			nullInt64(p.VideoBitrate), nullInt64(p.AudioBitrate), nullInt64(p.OverallBitrate),
			p.Size, unixTime(p.Mtime), unixTimePtr(p.TakenAt), unixTime(p.ImportedAt), unixTime(p.TimelineAt),
			nullBytes(p.SHA256), folderID, nullString(p.MetadataJSON), nullString(p.NFOJSON), nullString(p.NFOSearchText), p.CacheKey, sortKey, p.BrowserPlayable,
			p.ThumbStatus == model.StatusReady, p.PreviewStatus == model.StatusReady, p.VideoProxyStatus == model.StatusReady,
			p.ThumbStatus, p.PreviewStatus, p.VideoPosterStatus, p.VideoProxyStatus, nullInt64(p.NFOSize), unixTimePtr(p.NFOMtime), p.HasSubtitle, p.HasDanmaku, nullString(p.Error), unixTime(now), unixTime(now)).Scan(&id)
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
  media_type = ?, basename = ?, filename_sort_key = ?, ext = ?, mime_type = ?, size_bytes = ?, file_mtime = ?,
  width = ?, height = ?, aspect_ratio = ?, duration_ms = ?, captured_at = ?, sort_time = ?,
  fps = ?, video_codec = ?, audio_codec = ?, container = ?, video_bitrate = ?, audio_bitrate = ?, overall_bitrate = ?,
  sha256 = ?, cache_key = ?, browser_playable = ?, status = 0, thumb_status = ?, preview_status = ?,
  video_poster_status = ?, video_proxy_status = ?, thumb_ready = ?, preview_ready = ?, proxy_ready = ?,
  metadata_json = ?::jsonb, nfo_json = ?::jsonb, nfo_search_text = ?, nfo_size = ?, nfo_mtime = ?, error_text = ?, deleted = false,
  deleted_at = NULL, has_subtitle = ?, has_danmaku = ?, folder_id = ?, updated_at = ?
WHERE id = ?`,
			mediaTypeCode(p.MediaType), p.Filename, filenameSortKey(p.Filename), p.Ext, nullString(p.MimeType), p.Size, unixTime(p.Mtime),
			nullInt(p.Width), nullInt(p.Height), aspectRatio(p.Width, p.Height), durationMillis(p.Duration), unixTimePtr(p.TakenAt), unixTime(p.TimelineAt),
			nullFloat64(p.FPS), nullString(p.VideoCodec), nullString(p.AudioCodec), nullString(p.Container), nullInt64(p.VideoBitrate), nullInt64(p.AudioBitrate), nullInt64(p.OverallBitrate),
			nullBytes(p.SHA256), p.CacheKey, p.BrowserPlayable, p.ThumbStatus, p.PreviewStatus,
			p.VideoPosterStatus, p.VideoProxyStatus, p.ThumbStatus == model.StatusReady, p.PreviewStatus == model.StatusReady, p.VideoProxyStatus == model.StatusReady,
			nullString(p.MetadataJSON), nullString(p.NFOJSON), nullString(p.NFOSearchText), nullInt64(p.NFOSize), unixTimePtr(p.NFOMtime), nullString(p.Error), p.HasSubtitle, p.HasDanmaku, folderID, unixTime(now), id)
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE media_asset SET
  media_type = ?, basename = ?, filename_sort_key = ?, ext = ?, mime_type = ?, size_bytes = ?, file_mtime = ?,
  width = ?, height = ?, aspect_ratio = ?, duration_ms = ?, captured_at = ?, sort_time = ?,
  fps = ?, video_codec = ?, audio_codec = ?, container = ?, video_bitrate = ?, audio_bitrate = ?, overall_bitrate = ?,
  sha256 = ?, cache_key = ?, browser_playable = ?, status = 0, thumb_status = ?, preview_status = ?,
  video_poster_status = ?, video_proxy_status = ?, thumb_ready = ?, preview_ready = ?, proxy_ready = ?,
  metadata_json = ?::jsonb, error_text = ?, deleted = false, deleted_at = NULL, has_subtitle = ?, has_danmaku = ?, folder_id = ?, updated_at = ?
WHERE id = ?`,
			mediaTypeCode(p.MediaType), p.Filename, filenameSortKey(p.Filename), p.Ext, nullString(p.MimeType), p.Size, unixTime(p.Mtime),
			nullInt(p.Width), nullInt(p.Height), aspectRatio(p.Width, p.Height), durationMillis(p.Duration), unixTimePtr(p.TakenAt), unixTime(p.TimelineAt),
			nullFloat64(p.FPS), nullString(p.VideoCodec), nullString(p.AudioCodec), nullString(p.Container), nullInt64(p.VideoBitrate), nullInt64(p.AudioBitrate), nullInt64(p.OverallBitrate),
			nullBytes(p.SHA256), p.CacheKey, p.BrowserPlayable, p.ThumbStatus, p.PreviewStatus,
			p.VideoPosterStatus, p.VideoProxyStatus, p.ThumbStatus == model.StatusReady, p.PreviewStatus == model.StatusReady, p.VideoProxyStatus == model.StatusReady,
			nullString(p.MetadataJSON), nullString(p.Error), p.HasSubtitle, p.HasDanmaku, folderID, unixTime(now), id)
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
  ma.nfo_json IS NOT NULL,
  ma.has_subtitle,
  ma.has_danmaku
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id AND fi.missing = false
WHERE fi.rel_path = ?
  AND ma.deleted = false
  AND ma.deleted_at IS NULL`, relPath).Scan(&signature.ID, &signature.Size, &signature.Mtime, &nfoSize, &nfoMtime, &signature.HasNFO, &signature.HasSubtitle, &signature.HasDanmaku)
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

func (d *DB) SetAssetSidecarFlags(ctx context.Context, assetID int64, hasSubtitle bool, hasDanmaku bool) error {
	_, err := d.conn.ExecContext(ctx, `
UPDATE media_asset
SET has_subtitle = ?, has_danmaku = ?, updated_at = now()
WHERE id = ?`, hasSubtitle, hasDanmaku, assetID)
	return err
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

func (d *DB) GetAssetRecordIncludingDeleted(ctx context.Context, id int64) (model.Asset, error) {
	row := d.conn.QueryRowContext(ctx, assetSelectSQLFrom("asset_records")+` WHERE id = ?`, id)
	return scanAsset(row)
}

func (d *DB) MarkAssetPlayed(ctx context.Context, id int64) (*int64, error) {
	var playedAt int64
	err := d.conn.QueryRowContext(ctx, `
UPDATE media_asset
SET last_played_at = now()
WHERE id = ?
  AND media_type = 2
  AND deleted_at IS NULL
RETURNING EXTRACT(EPOCH FROM last_played_at)::BIGINT`, id).Scan(&playedAt)
	if err != nil {
		return nil, err
	}
	return &playedAt, nil
}

func (d *DB) ListLibraryAssets(ctx context.Context, opts AssetListOptions) (model.Page[model.Asset], error) {
	return d.listAssets(ctx, opts, false)
}

func (d *DB) SearchAssets(ctx context.Context, opts AssetListOptions) (model.Page[model.Asset], error) {
	return d.listAssets(ctx, opts, true)
}

func (d *DB) NFOOptions(ctx context.Context, opts NFOOptionOptions) ([]string, error) {
	if _, ok := nfoFieldFilterSQL(opts.Field); !ok {
		return nil, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}
	where := []string{"assets.is_live = true", "anv.field = ?"}
	args := []any{opts.Field}
	if opts.VisibleOnly {
		where = append(where, "(assets.thumb_status = 'ready' OR assets.media_type = 'audio')")
	}
	if query := strings.TrimSpace(opts.Query); query != "" {
		where = append(where, "anv.normalized_value LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(strings.ToLower(query))+"%")
	}
	args = append(args, limit)
	query := `SELECT MIN(anv.value) AS value
FROM asset_nfo_value anv
JOIN assets ON assets.id = anv.asset_id
WHERE ` + strings.Join(where, " AND ") + `
GROUP BY anv.normalized_value
ORDER BY anv.normalized_value
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
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) SearchAnchors(ctx context.Context, opts AssetListOptions) (LibraryAnchorResult, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	where, args := assetFilterSQL(opts, true)
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) AssetPosition(ctx context.Context, assetID int64, opts AssetListOptions, timeline bool) (AssetPosition, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	where, args := assetFilterSQL(opts, timeline)
	return d.assetPositionForFilter(ctx, assetID, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) anchorsForFilter(ctx context.Context, where string, args []any, sort string, group string, pageSize int) (LibraryAnchorResult, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	query := "SELECT filename, filename_sort_key, parent_rel_path, size, imported_at, timeline_at, last_played_at FROM assets WHERE " + where + " ORDER BY " + groupedSortSQL(group, sort)
	if group == assetGroupFolder {
		query = folderGroupedRankedSQL(where, sort) + "SELECT filename, filename_sort_key, parent_rel_path, size, imported_at, timeline_at, last_played_at FROM ranked ORDER BY " + folderGroupSortSQL(sort)
	}
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return LibraryAnchorResult{}, err
	}
	defer rows.Close()
	var items []libraryAnchorRow
	for rows.Next() {
		var item libraryAnchorRow
		var lastPlayedAt sql.NullInt64
		if err := rows.Scan(&item.Filename, &item.FilenameSortKey, &item.ParentRelPath, &item.Size, &item.ImportedAt, &item.TimelineAt, &lastPlayedAt); err != nil {
			return LibraryAnchorResult{}, err
		}
		item.LastPlayedAt = int64Ptr(lastPlayedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return LibraryAnchorResult{}, err
	}
	if len(items) == 0 {
		return LibraryAnchorResult{}, nil
	}
	if group == assetGroupFolder {
		return LibraryAnchorResult{Items: folderAnchors(items, pageSize), Total: len(items)}, nil
	}
	if group != "" {
		return LibraryAnchorResult{Items: groupedAnchors(items, group, sort, pageSize), Total: len(items)}, nil
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

func (d *DB) assetPositionForFilter(ctx context.Context, assetID int64, where string, args []any, sort string, group string, pageSize int) (AssetPosition, error) {
	query := `
SELECT item_index, total_count
FROM (
  SELECT id, ROW_NUMBER() OVER (ORDER BY ` + groupedSortSQL(group, sort) + `) - 1 AS item_index, COUNT(*) OVER () AS total_count
  FROM assets
  WHERE ` + where + `
) ranked
WHERE id = ?`
	if group == assetGroupFolder {
		query = folderGroupedRankedSQL(where, sort) + `
SELECT item_index, total_count
FROM (
  SELECT id, ROW_NUMBER() OVER (ORDER BY ` + folderGroupSortSQL(sort) + `) - 1 AS item_index, COUNT(*) OVER () AS total_count
  FROM ranked
) positioned
WHERE id = ?`
	}
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
	return sort != "filename" && sort != "filename_asc" && sort != "filename_desc"
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
	for index := 0; index < count; index++ {
		position := 0.0
		if count > 1 {
			position = float64(index) / float64(count-1)
		}
		itemIndex := 0
		if len(items) > 1 {
			itemIndex = int(math.Round(position * float64(len(items)-1)))
		}
		value := anchorScaleValue(sort, items[itemIndex])
		label, kind := uniformAnchorLabel(sort, value)
		anchors = append(anchors, LibraryAnchor{
			Key:      fmt.Sprintf("scale:%s:%d", sort, itemIndex),
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
	case "last_played_asc", "last_played_desc":
		if item.LastPlayedAt != nil {
			return *item.LastPlayedAt
		}
		return 0
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

func folderAnchors(items []libraryAnchorRow, pageSize int) []LibraryAnchor {
	if len(items) == 0 {
		return nil
	}
	anchors := make([]LibraryAnchor, 0)
	seen := make(map[string]struct{})
	for index, item := range items {
		if _, ok := seen[item.ParentRelPath]; ok {
			continue
		}
		seen[item.ParentRelPath] = struct{}{}
		position := 0.0
		if len(items) > 1 {
			position = float64(index) / float64(len(items)-1)
		}
		anchors = append(anchors, LibraryAnchor{
			Key:      "folder:" + item.ParentRelPath,
			Label:    folderAnchorLabel(item.ParentRelPath),
			Kind:     assetGroupFolder,
			Page:     index/pageSize + 1,
			Position: position,
			Value:    0,
		})
	}
	return anchors
}

func groupedAnchors(items []libraryAnchorRow, group string, sort string, pageSize int) []LibraryAnchor {
	anchors := make([]LibraryAnchor, 0)
	seen := make(map[string]struct{})
	for index, item := range items {
		key, label, kind, value := assetGroupAnchor(item, group, sort)
		if key == "" {
			continue
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
			Key: key, Label: label, Kind: kind, Page: index/pageSize + 1, Position: position, Value: value,
		})
	}
	return anchors
}

func assetGroupAnchor(item libraryAnchorRow, group string, sort string) (string, string, string, int64) {
	switch group {
	case "day", "month", "year":
		value := item.TimelineAt
		if strings.HasPrefix(sort, "last_played_") && item.LastPlayedAt != nil {
			value = *item.LastPlayedAt
		} else if strings.HasPrefix(sort, "imported_") {
			value = item.ImportedAt
		}
		date := time.Unix(value, 0).Local()
		label := date.Format("2006-01-02")
		if group == "month" {
			label = date.Format("2006-01")
		} else if group == "year" {
			label = date.Format("2006")
		}
		return group + ":" + label, label, group, value
	case "size":
		label := sizeAnchorLabel(item.Size)
		return "size:" + label, label, "size", item.Size
	case "letter":
		label := filenameAnchorLabel(item.FilenameSortKey)
		return "name:" + label, label, "letter", 0
	default:
		return "", "", "", 0
	}
}

func folderAnchorLabel(relPath string) string {
	if relPath == "" {
		return "全部存储"
	}
	return "/" + relPath
}

func (d *DB) ListFolderAssets(ctx context.Context, folderID int64, opts AssetListOptions) (model.Page[model.Asset], error) {
	folder, err := d.getFolderRaw(ctx, folderID)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	if opts.Recursive {
		ids, err := d.DescendantFolderIDs(ctx, folder.RelPath)
		if err != nil {
			return model.Page[model.Asset]{}, err
		}
		opts.FolderIDs = ids
	} else {
		opts.FolderRel = &folder.RelPath
	}
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
	if opts.Recursive {
		ids, err := d.DescendantFolderIDs(ctx, folder.RelPath)
		if err != nil {
			return LibraryAnchorResult{}, err
		}
		opts.FolderIDs = ids
	} else {
		opts.FolderRel = &folder.RelPath
	}
	where, args := assetFilterSQL(opts, false)
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) FolderAssetPosition(ctx context.Context, folderID int64, assetID int64, opts AssetListOptions) (AssetPosition, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	folder, err := d.getFolderRaw(ctx, folderID)
	if err != nil {
		return AssetPosition{}, err
	}
	if opts.Recursive {
		ids, err := d.DescendantFolderIDs(ctx, folder.RelPath)
		if err != nil {
			return AssetPosition{}, err
		}
		opts.FolderIDs = ids
	} else {
		opts.FolderRel = &folder.RelPath
	}
	where, args := assetFilterSQL(opts, false)
	return d.assetPositionForFilter(ctx, assetID, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) listAssets(ctx context.Context, opts AssetListOptions, timeline bool) (model.Page[model.Asset], error) {
	where, args := assetFilterSQL(opts, timeline)
	return d.ListAssetsByFilterSQL(ctx, "assets", where, args, opts)
}

func (d *DB) CountAssets(ctx context.Context, opts AssetListOptions, timeline bool) (int, error) {
	where, args := assetFilterSQL(opts, timeline)
	return d.CountAssetsByFilterSQL(ctx, "assets", where, args)
}

func (d *DB) ListAssetsByFilterSQL(ctx context.Context, source string, where string, args []any, opts AssetListOptions) (model.Page[model.Asset], error) {
	order := groupedSortSQL(opts.Group, opts.Sort)
	limit := opts.PageSize + 1
	offset := (opts.Page - 1) * opts.PageSize
	query := assetSelectSQLFrom(source) + " WHERE " + where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	if source == "assets" && opts.Group == assetGroupFolder {
		query = folderGroupedRankedSQL(where, opts.Sort) + assetSelectSQLFrom("ranked") + " ORDER BY " + folderGroupSortSQL(opts.Sort) + " LIMIT ? OFFSET ?"
	}
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

func (d *DB) CountAssetsByFilterSQL(ctx context.Context, source string, where string, args []any) (int, error) {
	var count int
	err := d.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+source+" WHERE "+where, args...).Scan(&count)
	return count, err
}

func (d *DB) SourceHealthSample(ctx context.Context, rootID string) (string, error) {
	if rootID == "" {
		return "", sql.ErrNoRows
	}
	var relPath string
	err := d.conn.QueryRowContext(ctx, `SELECT rel_path FROM assets WHERE deleted_at IS NULL AND (rel_path=? OR rel_path LIKE ?) ORDER BY id DESC LIMIT 1`, rootID, rootID+"/%").Scan(&relPath)
	return relPath, err
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

// MarkMissingRelPaths records files absent from a successful reconciliation scan.
// It intentionally keeps media_asset and its cache key intact so the existing
// thumbnail remains available in the missing/unavailable system collection.
func (d *DB) MarkMissingRelPaths(ctx context.Context, relPaths []string) (int, error) {
	const batchSize = 1000
	total := 0
	for start := 0; start < len(relPaths); start += batchSize {
		end := start + batchSize
		if end > len(relPaths) {
			end = len(relPaths)
		}
		batch := relPaths[start:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, relPath := range batch {
			placeholders[i] = "?"
			args[i] = relPath
		}
		result, err := d.conn.ExecContext(ctx, `
UPDATE file_instance fi
SET missing = true
FROM media_asset ma
WHERE ma.id = fi.asset_id
  AND ma.deleted_at IS NULL
  AND fi.missing = false
  AND fi.rel_path IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return total, err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += int(updated)
	}
	return total, nil
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
SET thumb_status = ?, video_poster_status = CASE WHEN media_type = 2 THEN ? ELSE ? END, thumb_ready = false, error_text = NULL, updated_at = ?
WHERE id = ? AND deleted = false`,
		model.StatusPending, model.StatusPending, model.StatusNotRequired, unixTime(now), assetID)
	return err
}

func (d *DB) ResetAssetThumbnailsForRoots(ctx context.Context, roots []string) (int, error) {
	where, args, err := assetRootsWhere(roots)
	if err != nil {
		return 0, err
	}
	now := util.UnixNow()
	queryArgs := []any{model.StatusPending, model.StatusPending, model.StatusNotRequired, unixTime(now)}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, model.StatusPending)
	var count int
	err = d.conn.QueryRowContext(ctx, `
WITH reset AS (
  UPDATE media_asset
  SET thumb_status = ?, video_poster_status = CASE WHEN media_type = 2 THEN ? ELSE ? END, thumb_ready = false, error_text = NULL, updated_at = ?
  WHERE deleted = false
    AND deleted_at IS NULL
    AND id IN (SELECT id FROM assets WHERE `+where+`)
  RETURNING id, media_type
), jobs AS (
  INSERT INTO media_job (asset_id, job_type, status, error_text, started_at, finished_at)
  SELECT id, CASE WHEN media_type = 2 THEN 'video_poster' ELSE 'thumb' END, ?, NULL, NULL, NULL FROM reset
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
	return d.ContinueWorkForRoots(ctx, "thumb", roots)
}

func (d *DB) ResetBackgroundVideoProxyWork(ctx context.Context) error {
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
UPDATE media_asset
SET video_proxy_status = ?, proxy_ready = false, error_text = NULL, updated_at = ?
WHERE deleted = false
  AND deleted_at IS NULL
  AND media_type = ?
  AND video_proxy_status IN (?, ?, ?)`,
		model.StatusNotRequired, unixTime(now), mediaTypeCode(model.MediaTypeVideo), model.StatusPending, model.StatusProcessing, model.StatusError)
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, `
UPDATE media_job
SET status = ?, error_text = NULL, started_at = NULL, finished_at = ?
WHERE job_type = 'video_proxy'
  AND status IN (?, ?, ?)`,
		model.StatusNotRequired, unixTime(now), model.StatusPending, model.StatusProcessing, model.StatusError)
	return err
}

func (d *DB) PendingWork(ctx context.Context) ([]WorkItem, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT id, media_type, thumb_status, preview_status, video_poster_status, video_proxy_status, browser_playable
FROM assets
WHERE deleted_at IS NULL AND (
  thumb_status IN ('pending','processing','error') OR
  preview_status IN ('pending','processing','error') OR
  video_poster_status IN ('pending','processing','error')
)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WorkItem
	for rows.Next() {
		var id int64
		var mediaType, thumbStatus, previewStatus, posterStatus, proxyStatus string
		var browserPlayable int
		if err := rows.Scan(&id, &mediaType, &thumbStatus, &previewStatus, &posterStatus, &proxyStatus, &browserPlayable); err != nil {
			return nil, err
		}
		if mediaType == model.MediaTypeVideo {
			if recoverableWorkStatus(thumbStatus) || recoverableWorkStatus(posterStatus) {
				items = append(items, WorkItem{Type: "video_poster", AssetID: id})
			}
		} else if recoverableWorkStatus(thumbStatus) {
			items = append(items, WorkItem{Type: "thumb", AssetID: id})
		}
		if mediaType == model.MediaTypeImage && recoverableWorkStatus(previewStatus) {
			items = append(items, WorkItem{Type: "preview", AssetID: id})
		}
		_ = proxyStatus
		_ = browserPlayable
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
	filterOpts := assetListOptionsFromNeighbor(opts)
	if opts.Context == "folder" {
		if opts.FolderID == nil {
			return Neighbors{}, errors.New("folderId is required")
		}
		folder, err := d.GetFolder(ctx, *opts.FolderID)
		if err != nil {
			return Neighbors{}, err
		}
		if opts.Recursive {
			ids, err := d.DescendantFolderIDs(ctx, folder.RelPath)
			if err != nil {
				return Neighbors{}, err
			}
			filterOpts.FolderIDs = ids
		} else {
			filterOpts.FolderRel = &folder.RelPath
		}
		filterOpts.Recursive = opts.Recursive
	}
	if opts.Context == "timeline" {
		filterOpts.Sort = "timeline_desc"
	}
	return d.AssetFilterNeighbors(ctx, opts.AssetID, filterOpts, opts.Context == "timeline" || opts.Context == "library", opts.Limit)
}

func assetListOptionsFromNeighbor(opts NeighborOptions) AssetListOptions {
	return AssetListOptions{
		Type: opts.Type, Sort: opts.Sort, Group: opts.Group, Query: opts.Query, CombinedQuery: opts.CombinedQuery, From: opts.From, To: opts.To, VisibleOnly: opts.VisibleOnly,
		NFOQuery: opts.NFOQuery, NFOActor: opts.NFOActor, NFOID: opts.NFOID, NFOTag: opts.NFOTag, ManualTag: opts.ManualTag, CombinedTag: opts.CombinedTag, CombinedTags: opts.CombinedTags, TagNodes: opts.TagNodes, AIDescription: opts.AIDescription, AITag: opts.AITag, NFOTitle: opts.NFOTitle, NFOYear: opts.NFOYear,
		MinWidth: opts.MinWidth, MaxWidth: opts.MaxWidth, MinHeight: opts.MinHeight, MaxHeight: opts.MaxHeight, MatchAnyAxis: opts.MatchAnyAxis,
		MinDuration: opts.MinDuration, MaxDuration: opts.MaxDuration, MinSize: opts.MinSize, MaxSize: opts.MaxSize, Orientation: opts.Orientation,
		Rating: opts.Rating, AlbumUnassigned: opts.AlbumUnassigned, AlbumIDs: opts.AlbumIDs, IncludeHidden: opts.IncludeHidden, PlayedOnly: opts.PlayedOnly,
	}
}

func (d *DB) AssetFilterNeighbors(ctx context.Context, assetID int64, filterOpts AssetListOptions, timeline bool, limit int) (Neighbors, error) {
	if limit <= 0 {
		limit = 5
	}
	current, err := d.GetAsset(ctx, assetID)
	if err != nil {
		return Neighbors{}, err
	}
	where, args := assetFilterSQL(filterOpts, timeline)
	if filterOpts.Group != "" || !legacyNeighborSort(filterOpts.Sort) {
		previous, err := d.groupedNeighborSide(ctx, where, args, filterOpts.Group, filterOpts.Sort, assetID, true, limit)
		if err != nil {
			return Neighbors{}, err
		}
		next, err := d.groupedNeighborSide(ctx, where, args, filterOpts.Group, filterOpts.Sort, assetID, false, limit)
		if err != nil {
			return Neighbors{}, err
		}
		return Neighbors{Current: current, Previous: previous, Next: next}, nil
	}
	contextName := ""
	if timeline {
		contextName = "library"
	}
	if fastMediaNeighborEligible(filterOpts, contextName) {
		previous, err := d.fastMediaNeighborSide(ctx, current, filterOpts, true, limit)
		if err != nil {
			return Neighbors{}, err
		}
		next, err := d.fastMediaNeighborSide(ctx, current, filterOpts, false, limit)
		if err != nil {
			return Neighbors{}, err
		}
		return Neighbors{Current: current, Previous: previous, Next: next}, nil
	}
	prevCond, prevArgs, prevOrder := neighborCondition(current, filterOpts.Sort, true)
	nextCond, nextArgs, nextOrder := neighborCondition(current, filterOpts.Sort, false)
	previous, err := d.neighborSide(ctx, where, args, prevCond, prevArgs, prevOrder, limit)
	if err != nil {
		return Neighbors{}, err
	}
	next, err := d.neighborSide(ctx, where, args, nextCond, nextArgs, nextOrder, limit)
	if err != nil {
		return Neighbors{}, err
	}
	return Neighbors{Current: current, Previous: previous, Next: next}, nil
}

func (d *DB) groupedNeighborSide(ctx context.Context, where string, args []any, group string, sort string, assetID int64, previous bool, limit int) ([]model.Asset, error) {
	comparator := ">"
	order := "ASC"
	if previous {
		comparator = "<"
		order = "DESC"
	}
	query := `WITH filtered AS (
  SELECT * FROM assets WHERE ` + where + `
),
ordered AS (
  SELECT filtered.*, ROW_NUMBER() OVER (ORDER BY ` + groupedSortSQL(group, sort) + `) AS item_row
  FROM filtered
), current_row AS (
  SELECT item_row FROM ordered WHERE id = ?
) ` + assetSelectSQLFrom("ordered") + `
WHERE ordered.item_row ` + comparator + ` (SELECT item_row FROM current_row)
ORDER BY ordered.item_row ` + order + `
LIMIT ?`
	if group == assetGroupFolder {
		query = folderGroupedRankedSQL(where, sort) + `,
ordered AS (
  SELECT ranked.*, ROW_NUMBER() OVER (ORDER BY ` + folderGroupSortSQL(sort) + `) AS item_row
  FROM ranked
), current_row AS (
  SELECT item_row FROM ordered WHERE id = ?
) ` + assetSelectSQLFrom("ordered") + `
WHERE ordered.item_row ` + comparator + ` (SELECT item_row FROM current_row)
ORDER BY ordered.item_row ` + order + `
LIMIT ?`
	}
	allArgs := append([]any{}, args...)
	allArgs = append(allArgs, assetID, limit)
	rows, err := d.conn.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssetRows(rows)
}

func legacyNeighborSort(sort string) bool {
	switch sort {
	case "", "timeline_asc", "timeline_desc", "filename", "filename_asc", "filename_desc", "size", "size_asc", "size_desc", "imported_asc", "imported_desc":
		return true
	default:
		return false
	}
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

func fastMediaNeighborEligible(opts AssetListOptions, contextName string) bool {
	if opts.PlayedOnly {
		return false
	}
	if contextName != "library" && contextName != "rating" {
		return false
	}
	if opts.Group == assetGroupFolder {
		return false
	}
	if opts.Type != model.MediaTypeImage && opts.Type != model.MediaTypeVideo && opts.Type != model.MediaTypeAudio {
		return false
	}
	switch opts.Sort {
	case "size", "size_desc", "size_asc":
	default:
		return false
	}
	if opts.Query != "" || opts.From != nil || opts.To != nil || opts.FolderID != nil || opts.FolderRel != nil || len(opts.FolderIDs) > 0 || opts.Recursive {
		return false
	}
	if opts.CombinedQuery != "" || opts.NFOQuery != "" || opts.NFOActor != "" || opts.NFOID != "" || opts.NFOTag != "" || opts.ManualTag != "" || opts.CombinedTag != "" || len(opts.CombinedTags) > 0 || opts.AIDescription != "" || opts.AITag != "" || opts.NFOTitle != "" || opts.NFOYear != "" {
		return false
	}
	if opts.MinWidth != nil || opts.MaxWidth != nil || opts.MinHeight != nil || opts.MaxHeight != nil || opts.MinDuration != nil || opts.MaxDuration != nil || opts.MinSize != nil || opts.MaxSize != nil {
		return false
	}
	if opts.MatchAnyAxis || normalizeAssetOrientationFilter(opts.Orientation) != "" || opts.AlbumUnassigned || len(opts.AlbumIDs) > 0 {
		return false
	}
	return opts.VisibleOnly
}

func (d *DB) fastMediaNeighborSide(ctx context.Context, current model.Asset, opts AssetListOptions, previous bool, limit int) ([]model.Asset, error) {
	condition, conditionArgs, order := fastMediaNeighborSizeCondition(current, opts.Sort, previous)
	where := []string{
		"ma.deleted_at IS NULL",
		fmt.Sprintf("ma.media_type = %d", mediaTypeCode(opts.Type)),
		"(ma.thumb_status = 'ready' OR ma.media_type = 3)",
		"EXISTS (SELECT 1 FROM file_instance fi WHERE fi.asset_id = ma.id AND fi.missing = false)",
	}
	var args []any
	if opts.Rating != nil {
		where = append(where, assetRatingSQL("ma")+" = ?")
		args = append(args, NormalizeRating(*opts.Rating))
	}
	query := `WITH candidate AS (
  SELECT ma.id AS asset_id, ROW_NUMBER() OVER (ORDER BY ` + order + `) AS rn
  FROM media_asset ma
  WHERE ` + strings.Join(where, " AND ") + `
    AND (` + condition + `)
  ORDER BY ` + order + `
  LIMIT ?
) ` + assetSelectSQL() + `
JOIN candidate ON candidate.asset_id = assets.id
ORDER BY candidate.rn`
	args = append(args, conditionArgs...)
	args = append(args, limit)
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssetRows(rows)
}

func assetFilterSQL(opts AssetListOptions, timeline bool) (string, []any) {
	where := []string{"is_live = true"}
	var args []any
	if opts.PlayedOnly {
		where = append(where, "media_type = 'video'", "last_played_at IS NOT NULL")
	}
	if !opts.IncludeHidden {
		where = append(where, "hidden = false")
	}
	if opts.VisibleOnly {
		where = append(where, "(thumb_status = 'ready' OR media_type = 'audio')")
	}
	switch opts.Type {
	case model.MediaTypeImage, model.MediaTypeVideo, model.MediaTypeAudio:
		where = append(where, "id IN (SELECT id FROM media_asset WHERE media_type = ?)")
		args = append(args, mediaTypeCode(opts.Type))
	}
	if opts.Query != "" {
		where = append(where, "lower(filename) LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(strings.ToLower(opts.Query))+"%")
	}
	if text := strings.TrimSpace(opts.CombinedQuery); text != "" {
		query := strings.ToLower(text)
		like := "%" + escapeLike(query) + "%"
		if len([]rune(text)) < 3 {
			where = append(where, `(lower(filename) LIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id AND air.status='ready' AND air.input_cache_key=assets.cache_key AND lower(air.description) LIKE ? ESCAPE '\'))`)
			args = append(args, like, like)
		} else {
			where = append(where, `(lower(filename) LIKE ? ESCAPE '\' OR ? <% lower(filename) OR EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id AND air.status='ready' AND air.input_cache_key=assets.cache_key AND (lower(air.description) LIKE ? ESCAPE '\' OR ? <% lower(air.description))))`)
			args = append(args, like, query, like, query)
		}
	}
	if opts.Rating != nil {
		where = append(where, "rating = ?")
		args = append(args, NormalizeRating(*opts.Rating))
	}
	if opts.AlbumUnassigned {
		where = append(where, "NOT "+albumMembershipExistsSQL())
	} else if len(opts.AlbumIDs) > 0 {
		where = append(where, albumMembershipExistsForAlbumIDsSQL(len(opts.AlbumIDs)))
		for _, albumID := range opts.AlbumIDs {
			args = append(args, albumID)
		}
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
	if tag := NormalizeAssetTag(opts.ManualTag); tag != "" {
		where = append(where, `EXISTS (
  SELECT 1
  FROM asset_tag
  JOIN tag ON tag.id = asset_tag.tag_id
  WHERE asset_tag.asset_id = assets.id
    AND tag.name = ?
)`)
		args = append(args, tag)
	}
	if tag := strings.TrimSpace(opts.CombinedTag); tag != "" {
		where = append(where, `(
EXISTS (SELECT 1 FROM asset_tag JOIN tag manual_tag ON manual_tag.id=asset_tag.tag_id WHERE asset_tag.asset_id=assets.id AND manual_tag.name=?)
OR EXISTS (SELECT 1 FROM asset_ai_tag ait JOIN asset_ai_result air ON air.asset_id=ait.asset_id WHERE ait.asset_id=assets.id AND ait.tag=? AND air.status='ready' AND air.input_cache_key=assets.cache_key)
)`)
		args = append(args, tag, tag)
	}
	if tags := normalizeCombinedTags(opts.CombinedTags); len(tags) > 0 {
		placeholders := queryPlaceholders(len(tags))
		where = append(where, `(
EXISTS (SELECT 1 FROM asset_tag JOIN tag manual_tag ON manual_tag.id=asset_tag.tag_id WHERE asset_tag.asset_id=assets.id AND manual_tag.name IN (`+placeholders+`))
OR EXISTS (SELECT 1 FROM asset_ai_tag ait JOIN asset_ai_result air ON air.asset_id=ait.asset_id WHERE ait.asset_id=assets.id AND ait.tag IN (`+placeholders+`) AND air.status='ready' AND air.input_cache_key=assets.cache_key)
)`)
		for _, tag := range tags {
			args = append(args, tag)
		}
		for _, tag := range tags {
			args = append(args, tag)
		}
	}
	for group, nodes := range groupTagNodes(opts.TagNodes) {
		if group == "manual" {
			names := make([]string, 0, len(nodes))
			for _, node := range nodes {
				names = append(names, strings.TrimPrefix(node, "manual:"))
			}
			where = append(where, `EXISTS (
  SELECT 1 FROM asset_tag JOIN tag ON tag.id=asset_tag.tag_id
  WHERE asset_tag.asset_id=assets.id AND tag.name IN (`+queryPlaceholders(len(names))+`)
)`)
			for _, name := range names {
				args = append(args, name)
			}
			continue
		}
		parts := make([]string, 0, len(nodes))
		for _, node := range nodes {
			parts = append(parts, `?=ANY(f.node_ids)`)
			args = append(args, node)
		}
		where = append(where, `EXISTS (
  SELECT 1 FROM asset_ai_tag_facet f
  JOIN asset_ai_result air ON air.asset_id=f.asset_id
  WHERE f.asset_id=assets.id AND air.status='ready' AND air.input_cache_key=assets.cache_key
    AND (`+strings.Join(parts, " OR ")+`)
)`)
	}
	if text := strings.TrimSpace(opts.AIDescription); text != "" {
		like := "%" + escapeLike(strings.ToLower(text)) + "%"
		if len([]rune(text)) < 3 {
			where = append(where, `EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id AND air.status='ready' AND air.input_cache_key=assets.cache_key AND lower(air.description) LIKE ? ESCAPE '\')`)
			args = append(args, like)
		} else {
			where = append(where, `EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id AND air.status='ready' AND air.input_cache_key=assets.cache_key AND (lower(air.description) LIKE ? ESCAPE '\' OR ? <% lower(air.description)))`)
			args = append(args, like, strings.ToLower(text))
		}
	}
	if tag := strings.TrimSpace(opts.AITag); tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM asset_ai_tag ait JOIN asset_ai_result air ON air.asset_id=ait.asset_id WHERE ait.asset_id=assets.id AND ait.tag=? AND air.status='ready' AND air.input_cache_key=assets.cache_key)`)
		args = append(args, tag)
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
	switch normalizeAssetOrientationFilter(opts.Orientation) {
	case "landscape":
		where = append(where, "orientation = 1")
	case "portrait":
		where = append(where, "orientation = 2")
	}
	if len(opts.FolderIDs) > 0 {
		placeholders := make([]string, len(opts.FolderIDs))
		for i, id := range opts.FolderIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where = append(where, "id IN (SELECT id FROM media_asset WHERE folder_id IN ("+strings.Join(placeholders, ",")+"))")
	} else if opts.FolderRel != nil {
		where = append(where, "parent_rel_path = ?")
		args = append(args, *opts.FolderRel)
	}
	if timeline {
		if opts.From != nil {
			where = append(where, "sort_time_value >= to_timestamp(?)")
			args = append(args, *opts.From)
		}
		if opts.To != nil {
			where = append(where, "sort_time_value <= to_timestamp(?)")
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
	if _, ok := nfoFieldFilterSQL(field); !ok {
		return "", nil
	}
	normalized := strings.ToLower(query)
	if len([]rune(query)) < 3 {
		return `EXISTS (SELECT 1 FROM asset_nfo_value anv WHERE anv.asset_id=assets.id AND anv.field=? AND anv.normalized_value LIKE ? ESCAPE '\')`, []any{field, "%" + escapeLike(normalized) + "%"}
	}
	return `EXISTS (SELECT 1 FROM asset_nfo_value anv WHERE anv.asset_id=assets.id AND anv.field=? AND (anv.normalized_value LIKE ? ESCAPE '\' OR ? <% anv.normalized_value))`, []any{field, "%" + escapeLike(normalized) + "%", normalized}
}

func nfoFieldFilterSQL(field string) (string, bool) {
	switch field {
	case "actor":
		return "lower(COALESCE(nfo_group.group_value->>'title', '')) = '演员' OR lower(COALESCE(nfo_item.item_value->>'key', '')) = 'actor'", true
	case "id":
		return "lower(COALESCE(nfo_group.group_value->>'title', '')) = 'id' OR lower(COALESCE(nfo_item.item_value->>'key', '')) = 'uniqueid' OR lower(COALESCE(nfo_item.item_value->>'key', '')) LIKE 'uniqueid:%'", true
	case "tag":
		return "lower(COALESCE(nfo_group.group_value->>'title', '')) IN ('标记', '类型') OR lower(COALESCE(nfo_item.item_value->>'key', '')) IN ('tag', 'genre')", true
	case "title":
		return "lower(COALESCE(nfo_item.item_value->>'key', '')) IN ('title', 'originaltitle', 'sorttitle')", true
	case "year":
		return "lower(COALESCE(nfo_item.item_value->>'key', '')) = 'year'", true
	default:
		return "", false
	}
}

func naturalFilenameOrderSQL(sortColumn, filenameColumn, direction, idColumn string) string {
	return sortColumn + ` COLLATE "lpicto_natural_numeric" ` + direction +
		", " + filenameColumn + " " + direction + ", " + idColumn + " " + direction
}

func naturalFilenameNeighborSQL(operator string) string {
	key := `filename_sort_key COLLATE "lpicto_natural_numeric"`
	return "(" + key + " " + operator + " ? OR (" + key + " = ? AND lower(filename) " + operator + " ?) OR (" +
		key + " = ? AND lower(filename) = ? AND id " + operator + " ?))"
}

func sortSQL(sort string) string {
	direction := "DESC"
	idDirection := "DESC"
	nulls := " NULLS LAST"
	if strings.HasSuffix(sort, "_asc") || sort == "filename" {
		direction = "ASC"
		idDirection = "ASC"
	}
	order := func(expression string) string {
		return expression + " " + direction + nulls + ", id " + idDirection
	}
	switch sort {
	case "timeline_asc":
		return "sort_time_value ASC, id ASC"
	case "filename", "filename_asc":
		return naturalFilenameOrderSQL("filename_sort_key", "lower(filename)", "ASC", "id")
	case "filename_desc":
		return naturalFilenameOrderSQL("filename_sort_key", "lower(filename)", "DESC", "id")
	case "size", "size_desc":
		return "size DESC, id DESC"
	case "size_asc":
		return "size ASC, id ASC"
	case "imported_asc":
		return "imported_at_value ASC, id ASC"
	case "imported_desc":
		return "imported_at_value DESC, id DESC"
	case "last_played_asc":
		return "last_played_at_value ASC, id ASC"
	case "last_played_desc":
		return "last_played_at_value DESC, id DESC"
	case "path_asc", "path_desc":
		return order("lower(rel_path)")
	case "media_type_asc", "media_type_desc":
		return order("media_type_value")
	case "resolution_asc", "resolution_desc":
		return order("resolution_value")
	case "duration_asc", "duration_desc":
		return order("duration_value")
	case "modified_asc", "modified_desc":
		return order("modified_at_value")
	case "rating_asc", "rating_desc":
		return order("rating")
	case "container_asc", "container_desc":
		return order("lower(container_value)")
	case "video_codec_asc", "video_codec_desc":
		return order("lower(video_codec_value)")
	case "audio_codec_asc", "audio_codec_desc":
		return order("lower(audio_codec_value)")
	case "fps_asc", "fps_desc":
		return order("fps_value")
	case "bitrate_asc", "bitrate_desc":
		return order("bitrate_value")
	case "subtitle_asc", "subtitle_desc":
		return order("has_subtitle")
	case "danmaku_asc", "danmaku_desc":
		return order("has_danmaku")
	case "ai_description_asc", "ai_description_desc":
		return order("ai_description_value")
	case "ai_tag_asc", "ai_tag_desc":
		return order("ai_tag_value")
	default:
		return "sort_time_value DESC, id DESC"
	}
}

func groupedSortSQL(group string, sort string) string {
	if group == "" || group == assetGroupFolder {
		return sortSQL(sort)
	}
	direction := "DESC"
	if strings.HasSuffix(sort, "_asc") || sort == "filename" {
		direction = "ASC"
	}
	var expression string
	switch group {
	case "day":
		expression = "date_trunc('day'," + groupTimeSQL(sort) + ")"
	case "month":
		expression = "date_trunc('month'," + groupTimeSQL(sort) + ")"
	case "year":
		expression = "date_trunc('year'," + groupTimeSQL(sort) + ")"
	case "size":
		expression = sizeGroupSQL()
	case "letter":
		expression = `CASE WHEN upper(left(filename_sort_key,1)) ~ '^[A-Z0-9[:punct:]]$' THEN upper(left(filename_sort_key,1)) ELSE '#' END`
	default:
		return sortSQL(sort)
	}
	return expression + " " + direction + " NULLS LAST, " + sortSQL(sort)
}

func groupTimeSQL(sort string) string {
	if strings.HasPrefix(sort, "last_played_") {
		return "last_played_at_value"
	}
	if strings.HasPrefix(sort, "imported_") {
		return "imported_at_value"
	}
	return "sort_time_value"
}

func sizeGroupSQL() string {
	return `CASE
WHEN size >= 2097152000 THEN 6
WHEN size >= 1048576000 THEN 5
WHEN size >= 524288000 THEN 4
WHEN size >= 104857600 THEN 3
WHEN size >= 10485760 THEN 2
WHEN size >= 1048576 THEN 1
ELSE 0 END`
}

func folderGroupedRankedSQL(where string, sort string) string {
	return `WITH filtered AS (
  SELECT * FROM assets WHERE ` + where + `
), ranked AS (
  SELECT filtered.*,
    FIRST_VALUE(timeline_at) OVER folder_window AS folder_timeline_at,
    FIRST_VALUE(imported_at) OVER folder_window AS folder_imported_at,
    FIRST_VALUE(last_played_at) OVER folder_window AS folder_last_played_at,
    FIRST_VALUE(size) OVER folder_window AS folder_size,
    FIRST_VALUE(filename_sort_key COLLATE "lpicto_natural_numeric") OVER folder_window AS folder_filename_sort_key,
    FIRST_VALUE(lower(filename)) OVER folder_window AS folder_filename,
    FIRST_VALUE(id) OVER folder_window AS folder_id
  FROM filtered
  WINDOW folder_window AS (PARTITION BY parent_rel_path ORDER BY ` + sortSQL(sort) + `)
) `
}

func folderGroupSortSQL(sort string) string {
	var groupOrder string
	switch sort {
	case "timeline_asc":
		groupOrder = "folder_timeline_at ASC, folder_id ASC"
	case "filename", "filename_asc":
		groupOrder = naturalFilenameOrderSQL("folder_filename_sort_key", "folder_filename", "ASC", "folder_id")
	case "filename_desc":
		groupOrder = naturalFilenameOrderSQL("folder_filename_sort_key", "folder_filename", "DESC", "folder_id")
	case "size", "size_desc":
		groupOrder = "folder_size DESC, folder_id DESC"
	case "size_asc":
		groupOrder = "folder_size ASC, folder_id ASC"
	case "imported_asc":
		groupOrder = "folder_imported_at ASC, folder_id ASC"
	case "imported_desc":
		groupOrder = "folder_imported_at DESC, folder_id DESC"
	case "last_played_asc":
		groupOrder = "folder_last_played_at ASC, folder_id ASC"
	case "last_played_desc":
		groupOrder = "folder_last_played_at DESC, folder_id DESC"
	default:
		groupOrder = "folder_timeline_at DESC, folder_id DESC"
	}
	return groupOrder + ", lower(parent_rel_path) ASC, parent_rel_path ASC, " + sortSQL(sort)
}

func anchorParts(sort string, item libraryAnchorRow) (string, string, string, int64) {
	switch sort {
	case "last_played_asc", "last_played_desc":
		if item.LastPlayedAt != nil {
			return dateAnchorParts(*item.LastPlayedAt)
		}
		return dateAnchorParts(0)
	case "filename", "filename_asc", "filename_desc":
		label := filenameAnchorLabel(item.FilenameSortKey)
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
	return sort == "timeline_asc" || sort == "timeline_desc" || sort == "imported_asc" || sort == "imported_desc" || sort == "last_played_asc" || sort == "last_played_desc" || sort == ""
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
		key := assetFilenameSortKey(current.Filename, current.FilenameSortKey)
		name := strings.ToLower(current.Filename)
		if previous {
			return naturalFilenameNeighborSQL("<"), []any{key, key, name, key, name, current.ID}, naturalFilenameOrderSQL("filename_sort_key", "lower(filename)", "DESC", "id")
		}
		return naturalFilenameNeighborSQL(">"), []any{key, key, name, key, name, current.ID}, naturalFilenameOrderSQL("filename_sort_key", "lower(filename)", "ASC", "id")
	case "filename_desc":
		key := assetFilenameSortKey(current.Filename, current.FilenameSortKey)
		name := strings.ToLower(current.Filename)
		if previous {
			return naturalFilenameNeighborSQL(">"), []any{key, key, name, key, name, current.ID}, naturalFilenameOrderSQL("filename_sort_key", "lower(filename)", "ASC", "id")
		}
		return naturalFilenameNeighborSQL("<"), []any{key, key, name, key, name, current.ID}, naturalFilenameOrderSQL("filename_sort_key", "lower(filename)", "DESC", "id")
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

func fastMediaNeighborSizeCondition(current model.Asset, sort string, previous bool) (string, []any, string) {
	switch sort {
	case "size", "size_desc":
		if previous {
			return "(ma.size_bytes > ? OR (ma.size_bytes = ? AND ma.id > ?))", []any{current.Size, current.Size, current.ID}, "ma.size_bytes ASC, ma.id ASC"
		}
		return "(ma.size_bytes < ? OR (ma.size_bytes = ? AND ma.id < ?))", []any{current.Size, current.Size, current.ID}, "ma.size_bytes DESC, ma.id DESC"
	case "size_asc":
		if previous {
			return "(ma.size_bytes < ? OR (ma.size_bytes = ? AND ma.id < ?))", []any{current.Size, current.Size, current.ID}, "ma.size_bytes DESC, ma.id DESC"
		}
		return "(ma.size_bytes > ? OR (ma.size_bytes = ? AND ma.id > ?))", []any{current.Size, current.Size, current.ID}, "ma.size_bytes ASC, ma.id ASC"
	default:
		return "ma.id <> ?", []any{current.ID}, "ma.id ASC"
	}
}

func assetSelectSQL() string {
	return assetSelectSQLFrom("assets")
}

func assetSelectSQLFrom(source string) string {
	return `SELECT id, rel_path, parent_rel_path, filename, filename_sort_key, ext, media_type, mime_type, size, mtime,
width, height, duration, taken_at, imported_at, timeline_at, cache_key, browser_playable,
scan_status, thumb_status, preview_status, video_poster_status, video_proxy_status,
rotation, rating,
metadata_json, nfo_json, nfo_search_text, error, deleted_at, created_at, updated_at,
hidden, sha256, has_subtitle, has_danmaku, last_played_at FROM ` + source
}

func assetRatingSQL(source string) string {
	return source + `.rating`
}

func scanAsset(row interface{ Scan(dest ...any) error }) (model.Asset, error) {
	var asset model.Asset
	var mime, metadata, nfoJSON, nfoSearchText, errorText sql.NullString
	var sha256Text sql.NullString
	var width, height, takenAt, deletedAt, lastPlayedAt sql.NullInt64
	var duration sql.NullFloat64
	var browserPlayable int
	err := row.Scan(&asset.ID, &asset.RelPath, &asset.ParentRelPath, &asset.Filename, &asset.FilenameSortKey, &asset.Ext, &asset.MediaType, &mime, &asset.Size, &asset.Mtime,
		&width, &height, &duration, &takenAt, &asset.ImportedAt, &asset.TimelineAt, &asset.CacheKey, &browserPlayable,
		&asset.ScanStatus, &asset.ThumbStatus, &asset.PreviewStatus, &asset.VideoPosterStatus, &asset.VideoProxyStatus,
		&asset.Rotation, &asset.Rating, &metadata, &nfoJSON, &nfoSearchText, &errorText, &deletedAt, &asset.CreatedAt, &asset.UpdatedAt,
		&asset.Hidden, &sha256Text, &asset.HasSubtitle, &asset.HasDanmaku, &lastPlayedAt)
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
	asset.LastPlayedAt = int64Ptr(lastPlayedAt)
	asset.SHA256 = stringPtr(sha256Text)
	asset.BrowserPlayable = browserPlayable == 1
	asset.Rotation = NormalizeRotation(asset.Rotation)
	asset.Rating = NormalizeRating(asset.Rating)
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

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
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

func normalizeCombinedTags(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" || len([]rune(tag)) > 80 {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) == 32 {
			break
		}
	}
	return out
}

func groupTagNodes(values []string) map[string][]string {
	groups := map[string][]string{}
	seen := map[string]struct{}{}
	for _, raw := range values {
		node := strings.TrimSpace(raw)
		if node == "" || len([]rune(node)) > 160 || (!strings.HasPrefix(node, "ai:") && !strings.HasPrefix(node, "manual:")) {
			continue
		}
		if _, exists := seen[node]; exists {
			continue
		}
		seen[node] = struct{}{}
		group := "manual"
		if strings.HasPrefix(node, "ai:") {
			base := strings.TrimPrefix(strings.SplitN(node, ":", 3)[1], "ai:")
			segments := strings.Split(base, ".")
			if len(segments) > 3 {
				segments = segments[:3]
			}
			group = strings.Join(segments, ".")
		}
		groups[group] = append(groups[group], node)
		if len(seen) == 32 {
			break
		}
	}
	return groups
}

func queryPlaceholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
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
	case model.MediaTypeAudio:
		return 3
	default:
		return 1
	}
}

func normalizeAssetOrientationFilter(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "landscape":
		return "landscape"
	case "portrait":
		return "portrait"
	default:
		return ""
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
		_ = proxyEnabled
		_ = browserPlayable
		return model.StatusPending, model.StatusNotRequired, model.StatusPending, model.StatusNotRequired
	}
	return model.StatusNotRequired, model.StatusNotRequired, model.StatusNotRequired, model.StatusNotRequired
}

func (d *DB) ReclassifyAssetAsAudio(ctx context.Context, assetID int64, mimeType string, browserPlayable bool) error {
	now := util.UnixNow()
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE media_asset
SET media_type = ?, mime_type = ?, browser_playable = ?,
    width = NULL, height = NULL, aspect_ratio = NULL, fps = NULL,
    video_codec = NULL, video_bitrate = NULL,
    thumb_status = ?, preview_status = ?, video_poster_status = ?, video_proxy_status = ?,
    thumb_ready = false, preview_ready = false, proxy_ready = false,
    error_text = NULL, updated_at = ?
WHERE id = ? AND media_type = ?`,
		mediaTypeCode(model.MediaTypeAudio), mimeType, browserPlayable,
		model.StatusNotRequired, model.StatusNotRequired, model.StatusNotRequired, model.StatusNotRequired,
		unixTime(now), assetID, mediaTypeCode(model.MediaTypeVideo)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE media_job
SET status = ?, error_text = NULL, finished_at = ?
WHERE asset_id = ? AND job_type IN ('thumb', 'video_poster')`,
		model.StatusNotRequired, unixTime(now), assetID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
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
