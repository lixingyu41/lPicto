package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

const (
	AlbumMediaAll         = "all"
	AlbumOrientationAll   = "all"
	AlbumOrientationWide  = "landscape"
	AlbumOrientationTall  = "portrait"
	AlbumSourceTypeFolder = "folder"
)

var albumSourceRuleExists = `EXISTS (
SELECT 1 FROM album_sources
WHERE album_sources.album_id = ?
  AND album_sources.source_type = 'folder'
  AND (
    (
      album_sources.recursive = true
      AND (
        album_sources.rel_path = ''
        OR assets.parent_rel_path = album_sources.rel_path
        OR assets.parent_rel_path LIKE album_sources.rel_path || '/%'
      )
    )
    OR (
      album_sources.recursive = false
      AND assets.parent_rel_path = album_sources.rel_path
    )
  )
  AND (
    album_sources.media_type_filter = 'all'
    OR assets.media_type = album_sources.media_type_filter
  )
  AND (
    album_sources.orientation_filter = 'all'
    OR (album_sources.orientation_filter = 'landscape' AND orientation IN (1, 3))
    OR (album_sources.orientation_filter = 'portrait' AND orientation = 2)
  )
)`

func albumMembershipExistsSQL() string {
	return albumMembershipExistsWhereSQL("")
}

func albumMembershipExistsForAlbumIDsSQL(count int) string {
	if count <= 0 {
		return albumMembershipExistsSQL()
	}
	return albumMembershipExistsWhereSQL("  AND album_filter.id IN (" + sqlPlaceholders(count) + ")\n")
}

func albumMembershipExistsWhereSQL(albumFilterCondition string) string {
	return `EXISTS (
SELECT 1
FROM albums album_filter
WHERE 1 = 1
` + albumFilterCondition + `
  AND (
    EXISTS (
      SELECT 1
      FROM album_sources album_source_filter
      WHERE album_source_filter.album_id = album_filter.id
        AND album_source_filter.source_type = 'folder'
        AND (
          (
            album_source_filter.recursive = true
            AND (
              album_source_filter.rel_path = ''
              OR assets.parent_rel_path = album_source_filter.rel_path
              OR assets.parent_rel_path LIKE replace(replace(replace(album_source_filter.rel_path, '\', '\\'), '%', '\%'), '_', '\_') || '/%' ESCAPE '\'
            )
          )
          OR (
            album_source_filter.recursive = false
            AND assets.parent_rel_path = album_source_filter.rel_path
          )
        )
        AND (
          album_source_filter.media_type_filter = 'all'
          OR assets.media_type = album_source_filter.media_type_filter
        )
        AND (
          album_source_filter.orientation_filter = 'all'
          OR (album_source_filter.orientation_filter = 'landscape' AND orientation IN (1, 3))
          OR (album_source_filter.orientation_filter = 'portrait' AND orientation = 2)
        )
    )
    OR EXISTS (
      SELECT 1
      FROM album_asset album_asset_filter
      WHERE album_asset_filter.album_id = album_filter.id
        AND album_asset_filter.asset_id = assets.id
    )
  )
  AND (
    album_filter.media_type_filter = 'all'
    OR assets.media_type = album_filter.media_type_filter
  )
  AND (
    album_filter.orientation_filter = 'all'
    OR (album_filter.orientation_filter = 'landscape' AND orientation IN (1, 3))
    OR (album_filter.orientation_filter = 'portrait' AND orientation = 2)
  )
)`
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

type AlbumSourceCreate struct {
	RelPath           string
	Recursive         bool
	MediaTypeFilter   string
	OrientationFilter string
}

type AlbumCreate struct {
	Name              string
	GroupID           *int64
	FolderRelPaths    []string
	Sources           []AlbumSourceCreate
	MediaTypeFilter   string
	OrientationFilter string
}

type AlbumGroupCreate struct {
	Name string
}

func (d *DB) ListAlbums(ctx context.Context) ([]model.Album, error) {
	albums, err := d.listAlbumsWithSources(ctx)
	if err != nil {
		return nil, err
	}
	needsStats := false
	for _, album := range albums {
		if album.StatsUpdatedAt == 0 {
			needsStats = true
			break
		}
	}
	if !needsStats {
		return albums, nil
	}
	if err := d.RefreshAlbumStats(ctx); err != nil {
		return nil, err
	}
	return d.listAlbumsWithSources(ctx)
}

func (d *DB) listAlbumsWithSources(ctx context.Context) ([]model.Album, error) {
	rows, err := d.conn.QueryContext(ctx, albumSelectSQL()+` ORDER BY group_id IS NULL, group_id, updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	albums, err := scanAlbumRows(rows)
	if err != nil {
		return nil, err
	}
	for i := range albums {
		sources, err := d.albumSources(ctx, albums[i].ID)
		if err != nil {
			return nil, err
		}
		albums[i].Sources = sources
	}
	return albums, nil
}

func (d *DB) ListAlbumGroups(ctx context.Context) ([]model.AlbumGroup, error) {
	rows, err := d.conn.QueryContext(ctx, `SELECT id, name, created_at, updated_at FROM album_groups ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []model.AlbumGroup{}
	for rows.Next() {
		var group model.AlbumGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (d *DB) CreateAlbumGroup(ctx context.Context, p AlbumGroupCreate) (model.AlbumGroup, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.AlbumGroup{}, errors.New("album group name is required")
	}
	now := util.UnixNow()
	var id int64
	err := d.conn.QueryRowContext(ctx, `
INSERT INTO album_groups (name, created_at, updated_at)
VALUES (?, ?, ?)
RETURNING id`, name, now, now).Scan(&id)
	if err != nil {
		return model.AlbumGroup{}, err
	}
	return d.GetAlbumGroup(ctx, id)
}

func (d *DB) GetAlbumGroup(ctx context.Context, id int64) (model.AlbumGroup, error) {
	var group model.AlbumGroup
	err := d.conn.QueryRowContext(ctx, `SELECT id, name, created_at, updated_at FROM album_groups WHERE id = ?`, id).
		Scan(&group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt)
	return group, err
}

func (d *DB) GetAlbum(ctx context.Context, id int64) (model.Album, error) {
	album, err := d.getAlbumWithSources(ctx, id)
	if err != nil {
		return model.Album{}, err
	}
	if album.StatsUpdatedAt != 0 {
		return album, nil
	}
	if err := d.RefreshAlbumStatsForAlbum(ctx, id); err != nil {
		return model.Album{}, err
	}
	return d.getAlbumWithSources(ctx, id)
}

func (d *DB) getAlbumWithSources(ctx context.Context, id int64) (model.Album, error) {
	row := d.conn.QueryRowContext(ctx, albumSelectSQL()+` WHERE id = ?`, id)
	album, err := scanAlbum(row)
	if err != nil {
		return model.Album{}, err
	}
	sources, err := d.albumSources(ctx, album.ID)
	if err != nil {
		return model.Album{}, err
	}
	album.Sources = sources
	return album, nil
}

func (d *DB) CreateAlbum(ctx context.Context, p AlbumCreate) (model.Album, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.Album{}, errors.New("album name is required")
	}
	if err := d.validateAlbumGroup(ctx, p.GroupID); err != nil {
		return model.Album{}, err
	}
	sources, err := normalizeAlbumSourceCreates(p)
	if err != nil {
		return model.Album{}, err
	}
	if len(sources) == 0 {
		return model.Album{}, errors.New("album source folder is required")
	}
	mediaFilter := AlbumMediaAll
	orientationFilter := AlbumOrientationAll
	if len(p.Sources) == 0 {
		mediaFilter = normalizeAlbumMediaFilter(p.MediaTypeFilter)
		orientationFilter = normalizeAlbumOrientationFilter(p.OrientationFilter)
	}
	now := util.UnixNow()
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return model.Album{}, err
	}
	var albumID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO albums (name, group_id, media_type_filter, orientation_filter, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id`, name, nullableInt64(p.GroupID), mediaFilter, orientationFilter, now, now).Scan(&albumID)
	if err != nil {
		_ = tx.Rollback()
		return model.Album{}, err
	}
	for _, source := range sources {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO album_sources (album_id, source_type, rel_path, recursive, media_type_filter, orientation_filter, created_at)
VALUES (?, 'folder', ?, ?, ?, ?, ?)`,
			albumID, source.RelPath, source.Recursive, source.MediaTypeFilter, source.OrientationFilter, now); err != nil {
			_ = tx.Rollback()
			return model.Album{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Album{}, err
	}
	if err := d.RefreshAlbumStatsForAlbum(ctx, albumID); err != nil {
		return model.Album{}, err
	}
	return d.GetAlbum(ctx, albumID)
}

func (d *DB) UpdateAlbum(ctx context.Context, id int64, p AlbumCreate) (model.Album, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.Album{}, errors.New("album name is required")
	}
	if err := d.validateAlbumGroup(ctx, p.GroupID); err != nil {
		return model.Album{}, err
	}
	sources, err := normalizeAlbumSourceCreates(p)
	if err != nil {
		return model.Album{}, err
	}
	if len(sources) == 0 {
		return model.Album{}, errors.New("album source folder is required")
	}
	mediaFilter := AlbumMediaAll
	orientationFilter := AlbumOrientationAll
	if len(p.Sources) == 0 {
		mediaFilter = normalizeAlbumMediaFilter(p.MediaTypeFilter)
		orientationFilter = normalizeAlbumOrientationFilter(p.OrientationFilter)
	}
	now := util.UnixNow()
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return model.Album{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE albums
SET name = ?, group_id = ?, media_type_filter = ?, orientation_filter = ?, updated_at = ?
WHERE id = ?`, name, nullableInt64(p.GroupID), mediaFilter, orientationFilter, now, id)
	if err != nil {
		_ = tx.Rollback()
		return model.Album{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return model.Album{}, err
	}
	if affected == 0 {
		_ = tx.Rollback()
		return model.Album{}, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM album_sources WHERE album_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return model.Album{}, err
	}
	for _, source := range sources {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO album_sources (album_id, source_type, rel_path, recursive, media_type_filter, orientation_filter, created_at)
VALUES (?, 'folder', ?, ?, ?, ?, ?)`,
			id, source.RelPath, source.Recursive, source.MediaTypeFilter, source.OrientationFilter, now); err != nil {
			_ = tx.Rollback()
			return model.Album{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Album{}, err
	}
	if err := d.RefreshAlbumStatsForAlbum(ctx, id); err != nil {
		return model.Album{}, err
	}
	return d.GetAlbum(ctx, id)
}

func (d *DB) DeleteAlbum(ctx context.Context, id int64) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	return err
}

func (d *DB) TouchAlbum(ctx context.Context, id int64) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE albums SET updated_at = ? WHERE id = ?`, util.UnixNow(), id)
	return err
}

func (d *DB) AddAlbumAssets(ctx context.Context, albumID int64, assetIDs []int64) ([]int64, error) {
	if len(assetIDs) == 0 {
		return nil, nil
	}
	if _, err := d.GetAlbum(ctx, albumID); err != nil {
		return nil, err
	}
	now := util.UnixNow()
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	updated := make([]int64, 0, len(assetIDs))
	for _, assetID := range uniqueInt64s(assetIDs) {
		result, err := tx.ExecContext(ctx, `
INSERT INTO album_asset (album_id, asset_id, created_at)
SELECT ?, id, ?
FROM media_asset
WHERE id = ? AND deleted_at IS NULL
ON CONFLICT(album_id, asset_id) DO NOTHING`, albumID, now, assetID)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			updated = append(updated, assetID)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := d.RefreshAlbumStatsForAlbum(ctx, albumID); err != nil {
		return nil, err
	}
	return updated, nil
}

func (d *DB) RemoveAlbumAssets(ctx context.Context, albumID int64, assetIDs []int64) ([]int64, error) {
	if len(assetIDs) == 0 {
		return nil, nil
	}
	if _, err := d.GetAlbum(ctx, albumID); err != nil {
		return nil, err
	}
	ids := uniqueInt64s(assetIDs)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, albumID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := d.conn.ExecContext(ctx, `DELETE FROM album_asset WHERE album_id = ? AND asset_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	if err := d.RefreshAlbumStatsForAlbum(ctx, albumID); err != nil {
		return nil, err
	}
	return ids, nil
}

func (d *DB) RefreshAlbumStats(ctx context.Context) error {
	albums, err := d.listAlbumsWithSources(ctx)
	if err != nil {
		return err
	}
	for _, album := range albums {
		if err := d.refreshAlbumStats(ctx, album); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) RefreshAlbumStatsForAlbum(ctx context.Context, id int64) error {
	album, err := d.getAlbumWithSources(ctx, id)
	if err != nil {
		return err
	}
	return d.refreshAlbumStats(ctx, album)
}

func (d *DB) ListAlbumAssets(ctx context.Context, albumID int64, opts AssetListOptions) (model.Page[model.Asset], error) {
	album, err := d.getAlbumWithSources(ctx, albumID)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	where, args := albumAssetFilterSQL(album, opts)
	limit := opts.PageSize + 1
	offset := (opts.Page - 1) * opts.PageSize
	query := assetSelectSQL() + " WHERE " + where + " ORDER BY " + groupedSortSQL(opts.Group, opts.Sort) + " LIMIT ? OFFSET ?"
	if opts.Group == assetGroupFolder {
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

func (d *DB) AlbumAnchors(ctx context.Context, albumID int64, opts AssetListOptions) (LibraryAnchorResult, error) {
	album, err := d.getAlbumWithSources(ctx, albumID)
	if err != nil {
		return LibraryAnchorResult{}, err
	}
	where, args := albumAssetFilterSQL(album, opts)
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) AlbumAssetPosition(ctx context.Context, albumID int64, assetID int64, opts AssetListOptions) (AssetPosition, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	album, err := d.getAlbumWithSources(ctx, albumID)
	if err != nil {
		return AssetPosition{}, err
	}
	where, args := albumAssetFilterSQL(album, opts)
	return d.assetPositionForFilter(ctx, assetID, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) AlbumNeighbors(ctx context.Context, albumID int64, opts NeighborOptions) (Neighbors, error) {
	if opts.Limit <= 0 {
		opts.Limit = 5
	}
	current, err := d.GetAsset(ctx, opts.AssetID)
	if err != nil {
		return Neighbors{}, err
	}
	album, err := d.getAlbumWithSources(ctx, albumID)
	if err != nil {
		return Neighbors{}, err
	}
	filterOpts := assetListOptionsFromNeighbor(opts)
	where, args := albumAssetFilterSQL(album, filterOpts)
	if filterOpts.Group != "" || !legacyNeighborSort(filterOpts.Sort) {
		previous, err := d.groupedNeighborSide(ctx, where, args, filterOpts.Group, filterOpts.Sort, opts.AssetID, true, opts.Limit)
		if err != nil {
			return Neighbors{}, err
		}
		next, err := d.groupedNeighborSide(ctx, where, args, filterOpts.Group, filterOpts.Sort, opts.AssetID, false, opts.Limit)
		if err != nil {
			return Neighbors{}, err
		}
		return Neighbors{Current: current, Previous: previous, Next: next}, nil
	}
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

func (d *DB) AlbumScanRoots(ctx context.Context, id int64) ([]string, error) {
	album, err := d.getAlbumWithSources(ctx, id)
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(album.Sources))
	for _, source := range album.Sources {
		if source.SourceType == AlbumSourceTypeFolder {
			roots = append(roots, source.RelPath)
		}
	}
	return NormalizeScanFolders(roots)
}

func (d *DB) loadAlbumDetails(ctx context.Context, album *model.Album) error {
	sources, err := d.albumSources(ctx, album.ID)
	if err != nil {
		return err
	}
	album.Sources = sources
	return d.loadAlbumStats(ctx, album)
}

func (d *DB) loadAlbumStats(ctx context.Context, album *model.Album) error {
	if err := d.RefreshAlbumStatsForAlbum(ctx, album.ID); err != nil {
		return err
	}
	refreshed, err := d.getAlbumWithSources(ctx, album.ID)
	if err != nil {
		return err
	}
	album.AssetCount = refreshed.AssetCount
	album.CoverAssetID = refreshed.CoverAssetID
	album.StatsUpdatedAt = refreshed.StatsUpdatedAt
	return nil
}

func (d *DB) albumSources(ctx context.Context, albumID int64) ([]model.AlbumSource, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT id, album_id, source_type, rel_path, recursive, media_type_filter, orientation_filter, created_at
FROM album_sources
WHERE album_id = ?
ORDER BY id ASC`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []model.AlbumSource
	for rows.Next() {
		var source model.AlbumSource
		var recursive bool
		if err := rows.Scan(
			&source.ID,
			&source.AlbumID,
			&source.SourceType,
			&source.RelPath,
			&recursive,
			&source.MediaTypeFilter,
			&source.OrientationFilter,
			&source.CreatedAt,
		); err != nil {
			return nil, err
		}
		source.Recursive = recursive
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (d *DB) albumCountCover(ctx context.Context, album model.Album) (int, *int64, error) {
	where, args := albumAssetFilterSQL(album, AssetListOptions{})
	var count int
	if err := d.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets WHERE "+where, args...).Scan(&count); err != nil {
		return 0, nil, err
	}
	coverWhere, coverArgs := albumAssetFilterSQL(album, AssetListOptions{VisibleOnly: true})
	row := d.conn.QueryRowContext(ctx, "SELECT id FROM assets WHERE "+coverWhere+" ORDER BY timeline_at DESC, id DESC LIMIT 1", coverArgs...)
	var cover sql.NullInt64
	err := row.Scan(&cover)
	if errors.Is(err, sql.ErrNoRows) {
		return count, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	return count, int64Ptr(cover), nil
}

func (d *DB) refreshAlbumStats(ctx context.Context, album model.Album) error {
	count, cover, err := d.albumCountCover(ctx, album)
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, `
UPDATE albums
SET asset_count = ?, cover_asset_id = ?, stats_updated_at = ?
WHERE id = ?`, count, nullInt64(cover), util.UnixNow(), album.ID)
	return err
}

func albumAssetFilterSQL(album model.Album, opts AssetListOptions) (string, []any) {
	sourceWhere, sourceArgs := albumSourceFilterSQL(album.Sources)
	memberWhere := `assets.id IN (SELECT asset_id FROM album_asset WHERE album_id = ?)`
	if sourceWhere == "" && album.ID == 0 {
		return "0 = 1", nil
	}
	memberRules := make([]string, 0, 2)
	args := make([]any, 0, len(sourceArgs)+1)
	if sourceWhere != "" {
		memberRules = append(memberRules, "("+sourceWhere+")")
		args = append(args, sourceArgs...)
	}
	memberRules = append(memberRules, memberWhere)
	args = append(args, album.ID)
	filterWhere, filterArgs := assetFilterSQL(opts, false)
	where := []string{"(" + filterWhere + ")", "(" + strings.Join(memberRules, " OR ") + ")"}
	args = append(filterArgs, args...)
	mediaFilter := normalizeAlbumMediaFilter(album.MediaTypeFilter)
	if opts.Type == model.MediaTypeImage || opts.Type == model.MediaTypeVideo || opts.Type == model.MediaTypeAudio {
		mediaFilter = opts.Type
	}
	if mediaFilter == model.MediaTypeImage || mediaFilter == model.MediaTypeVideo || mediaFilter == model.MediaTypeAudio {
		where = append(where, "assets.id IN (SELECT id FROM media_asset WHERE media_type = ?)")
		args = append(args, mediaTypeCode(mediaFilter))
	}
	switch normalizeAlbumOrientationFilter(album.OrientationFilter) {
	case AlbumOrientationWide:
		where = append(where, "orientation IN (1, 3)")
	case AlbumOrientationTall:
		where = append(where, "orientation = 2")
	}
	return strings.Join(where, " AND "), args
}

func albumSourceFilterSQL(sources []model.AlbumSource) (string, []any) {
	var rules []string
	var args []any
	for _, source := range sources {
		if source.SourceType != AlbumSourceTypeFolder {
			continue
		}
		var parts []string
		if source.Recursive {
			if source.RelPath != "" {
				parts = append(parts, `assets.id IN (
SELECT ma.id
FROM media_asset ma
JOIN folder f ON f.id = ma.folder_id
WHERE f.rel_path = ? OR f.rel_path LIKE ? ESCAPE '\'
)`)
				args = append(args, source.RelPath, descendantPathLike(source.RelPath))
			}
		} else {
			parts = append(parts, `assets.id IN (
SELECT ma.id
FROM media_asset ma
JOIN folder f ON f.id = ma.folder_id
WHERE f.rel_path = ?
)`)
			args = append(args, source.RelPath)
		}
		switch normalizeAlbumMediaFilter(source.MediaTypeFilter) {
		case model.MediaTypeImage, model.MediaTypeVideo, model.MediaTypeAudio:
			parts = append(parts, `assets.id IN (SELECT id FROM media_asset WHERE media_type = ?)`)
			args = append(args, mediaTypeCode(normalizeAlbumMediaFilter(source.MediaTypeFilter)))
		}
		switch normalizeAlbumOrientationFilter(source.OrientationFilter) {
		case AlbumOrientationWide:
			parts = append(parts, "orientation IN (1, 3)")
		case AlbumOrientationTall:
			parts = append(parts, "orientation = 2")
		}
		if len(parts) == 0 {
			rules = append(rules, "1 = 1")
			continue
		}
		rules = append(rules, "("+strings.Join(parts, " AND ")+")")
	}
	return strings.Join(rules, " OR "), args
}

func normalizeAlbumSourceCreates(p AlbumCreate) ([]AlbumSourceCreate, error) {
	if len(p.Sources) == 0 {
		folders, err := NormalizeScanFolders(p.FolderRelPaths)
		if err != nil {
			return nil, err
		}
		sources := make([]AlbumSourceCreate, 0, len(folders))
		for _, rel := range folders {
			sources = append(sources, AlbumSourceCreate{
				RelPath:           rel,
				Recursive:         true,
				MediaTypeFilter:   normalizeAlbumMediaFilter(p.MediaTypeFilter),
				OrientationFilter: normalizeAlbumOrientationFilter(p.OrientationFilter),
			})
		}
		return sources, nil
	}
	seen := make(map[string]struct{}, len(p.Sources))
	sources := make([]AlbumSourceCreate, 0, len(p.Sources))
	for _, source := range p.Sources {
		rel, err := storage.NormalizeRelPath(source.RelPath)
		if err != nil {
			return nil, err
		}
		normalized := AlbumSourceCreate{
			RelPath:           rel,
			Recursive:         source.Recursive,
			MediaTypeFilter:   normalizeAlbumMediaFilter(source.MediaTypeFilter),
			OrientationFilter: normalizeAlbumOrientationFilter(source.OrientationFilter),
		}
		key := fmt.Sprintf("%s\x00%t\x00%s\x00%s", normalized.RelPath, normalized.Recursive, normalized.MediaTypeFilter, normalized.OrientationFilter)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sources = append(sources, normalized)
	}
	return sources, nil
}

func effectiveWidthSQL() string {
	return `CASE WHEN rotation IN (90, 270) THEN height ELSE width END`
}

func effectiveHeightSQL() string {
	return `CASE WHEN rotation IN (90, 270) THEN width ELSE height END`
}

func normalizeAlbumMediaFilter(value string) string {
	switch value {
	case model.MediaTypeImage, model.MediaTypeVideo, model.MediaTypeAudio:
		return value
	default:
		return AlbumMediaAll
	}
}

func normalizeAlbumOrientationFilter(value string) string {
	switch value {
	case AlbumOrientationWide, AlbumOrientationTall:
		return value
	default:
		return AlbumOrientationAll
	}
}

func albumSelectSQL() string {
	return `SELECT id, name, group_id, media_type_filter, orientation_filter, asset_count, cover_asset_id, stats_updated_at, created_at, updated_at FROM albums`
}

func scanAlbum(row interface{ Scan(dest ...any) error }) (model.Album, error) {
	var album model.Album
	var groupID sql.NullInt64
	var cover sql.NullInt64
	err := row.Scan(
		&album.ID,
		&album.Name,
		&groupID,
		&album.MediaTypeFilter,
		&album.OrientationFilter,
		&album.AssetCount,
		&cover,
		&album.StatsUpdatedAt,
		&album.CreatedAt,
		&album.UpdatedAt,
	)
	if err != nil {
		return model.Album{}, err
	}
	if groupID.Valid {
		album.GroupID = int64Ptr(groupID)
	}
	album.CoverAssetID = int64Ptr(cover)
	return album, nil
}

func scanAlbumRows(rows *sql.Rows) ([]model.Album, error) {
	var albums []model.Album
	for rows.Next() {
		album, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		albums = append(albums, album)
	}
	return albums, rows.Err()
}

func ValidateAlbumFolderInScanRoots(rel string, scanRoots []string) error {
	normalized, err := storage.NormalizeRelPath(rel)
	if err != nil {
		return err
	}
	if !AssetInScanFolders(normalized, scanRoots) {
		return fmt.Errorf("folder is outside scan libraries")
	}
	return nil
}

func (d *DB) validateAlbumGroup(ctx context.Context, groupID *int64) error {
	if groupID == nil {
		return nil
	}
	var exists int
	if err := d.conn.QueryRowContext(ctx, `SELECT 1 FROM album_groups WHERE id = ?`, *groupID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("album group not found")
		}
		return err
	}
	return nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
