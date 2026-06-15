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
      album_sources.recursive = 1
      AND (
        album_sources.rel_path = ''
        OR assets.parent_rel_path = album_sources.rel_path
        OR assets.parent_rel_path LIKE album_sources.rel_path || '/%'
      )
    )
    OR (
      album_sources.recursive = 0
      AND assets.parent_rel_path = album_sources.rel_path
    )
  )
  AND (
    album_sources.media_type_filter = 'all'
    OR assets.media_type = album_sources.media_type_filter
  )
  AND (
    album_sources.orientation_filter = 'all'
    OR (album_sources.orientation_filter = 'landscape' AND width IS NOT NULL AND height IS NOT NULL AND ` + effectiveWidthSQL() + ` >= ` + effectiveHeightSQL() + `)
    OR (album_sources.orientation_filter = 'portrait' AND width IS NOT NULL AND height IS NOT NULL AND ` + effectiveHeightSQL() + ` > ` + effectiveWidthSQL() + `)
  )
)`

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
	result, err := d.conn.ExecContext(ctx, `
INSERT INTO album_groups (name, created_at, updated_at)
VALUES (?, ?, ?)`, name, now, now)
	if err != nil {
		return model.AlbumGroup{}, err
	}
	id, err := result.LastInsertId()
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
	if err := d.loadAlbumStats(ctx, &album); err != nil {
		return model.Album{}, err
	}
	return album, nil
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
	result, err := tx.ExecContext(ctx, `
INSERT INTO albums (name, group_id, media_type_filter, orientation_filter, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`, name, nullableInt64(p.GroupID), mediaFilter, orientationFilter, now, now)
	if err != nil {
		_ = tx.Rollback()
		return model.Album{}, err
	}
	albumID, err := result.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return model.Album{}, err
	}
	for _, source := range sources {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO album_sources (album_id, source_type, rel_path, recursive, media_type_filter, orientation_filter, created_at)
VALUES (?, 'folder', ?, ?, ?, ?, ?)`,
			albumID, source.RelPath, boolInt(source.Recursive), source.MediaTypeFilter, source.OrientationFilter, now); err != nil {
			_ = tx.Rollback()
			return model.Album{}, err
		}
	}
	if err := tx.Commit(); err != nil {
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
			id, source.RelPath, boolInt(source.Recursive), source.MediaTypeFilter, source.OrientationFilter, now); err != nil {
			_ = tx.Rollback()
			return model.Album{}, err
		}
	}
	if err := tx.Commit(); err != nil {
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

func (d *DB) ListAlbumAssets(ctx context.Context, albumID int64, opts AssetListOptions) (model.Page[model.Asset], error) {
	album, err := d.getAlbumWithSources(ctx, albumID)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	where, args := albumAssetFilterSQL(album, opts)
	limit := opts.PageSize + 1
	offset := (opts.Page - 1) * opts.PageSize
	query := assetSelectSQL() + " WHERE " + where + " ORDER BY " + sortSQL(opts.Sort) + " LIMIT ? OFFSET ?"
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
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.PageSize)
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
	filterOpts := AssetListOptions{Sort: opts.Sort, Query: opts.Query, VisibleOnly: true}
	where, args := albumAssetFilterSQL(album, filterOpts)
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
	count, cover, err := d.albumCountCover(ctx, *album)
	if err != nil {
		return err
	}
	album.AssetCount = count
	album.CoverAssetID = cover
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
		var recursive int
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
		source.Recursive = recursive == 1
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (d *DB) albumCountCover(ctx context.Context, album model.Album) (int, *int64, error) {
	where, args := albumAssetFilterSQL(album, AssetListOptions{VisibleOnly: true})
	var count int
	if err := d.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets WHERE "+where, args...).Scan(&count); err != nil {
		return 0, nil, err
	}
	row := d.conn.QueryRowContext(ctx, "SELECT id FROM assets WHERE "+where+" ORDER BY timeline_at DESC, id DESC LIMIT 1", args...)
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

func albumAssetFilterSQL(album model.Album, opts AssetListOptions) (string, []any) {
	sourceWhere, sourceArgs := albumSourceFilterSQL(album.Sources)
	if sourceWhere == "" {
		return "0 = 1", nil
	}
	where := []string{"deleted_at IS NULL", "(" + sourceWhere + ")"}
	args := append([]any{}, sourceArgs...)
	if opts.VisibleOnly {
		where = append(where, "thumb_status = 'ready'")
	}
	mediaFilter := normalizeAlbumMediaFilter(album.MediaTypeFilter)
	if opts.Type == model.MediaTypeImage || opts.Type == model.MediaTypeVideo {
		mediaFilter = opts.Type
	}
	if mediaFilter == model.MediaTypeImage || mediaFilter == model.MediaTypeVideo {
		where = append(where, "media_type = ?")
		args = append(args, mediaFilter)
	}
	if opts.Query != "" {
		where = append(where, "lower(filename) LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(strings.ToLower(opts.Query))+"%")
	}
	switch normalizeAlbumOrientationFilter(album.OrientationFilter) {
	case AlbumOrientationWide:
		where = append(where, "width IS NOT NULL AND height IS NOT NULL AND "+effectiveWidthSQL()+" >= "+effectiveHeightSQL())
	case AlbumOrientationTall:
		where = append(where, "width IS NOT NULL AND height IS NOT NULL AND "+effectiveHeightSQL()+" > "+effectiveWidthSQL())
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
				lower, upper := descendantPathBounds(source.RelPath)
				parts = append(parts, `(assets.parent_rel_path = ? OR (assets.parent_rel_path >= ? AND assets.parent_rel_path < ?))`)
				args = append(args, source.RelPath, lower, upper)
			}
		} else {
			parts = append(parts, `assets.parent_rel_path = ?`)
			args = append(args, source.RelPath)
		}
		switch normalizeAlbumMediaFilter(source.MediaTypeFilter) {
		case model.MediaTypeImage, model.MediaTypeVideo:
			parts = append(parts, `assets.media_type = ?`)
			args = append(args, normalizeAlbumMediaFilter(source.MediaTypeFilter))
		}
		switch normalizeAlbumOrientationFilter(source.OrientationFilter) {
		case AlbumOrientationWide:
			parts = append(parts, "width IS NOT NULL AND height IS NOT NULL AND "+effectiveWidthSQL()+" >= "+effectiveHeightSQL())
		case AlbumOrientationTall:
			parts = append(parts, "width IS NOT NULL AND height IS NOT NULL AND "+effectiveHeightSQL()+" > "+effectiveWidthSQL())
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
	return `CASE WHEN COALESCE((SELECT rotation FROM asset_preferences WHERE asset_id = assets.id), 0) IN (90, 270) THEN height ELSE width END`
}

func effectiveHeightSQL() string {
	return `CASE WHEN COALESCE((SELECT rotation FROM asset_preferences WHERE asset_id = assets.id), 0) IN (90, 270) THEN width ELSE height END`
}

func normalizeAlbumMediaFilter(value string) string {
	switch value {
	case model.MediaTypeImage, model.MediaTypeVideo:
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
	return `SELECT id, name, group_id, media_type_filter, orientation_filter, created_at, updated_at FROM albums`
}

func scanAlbum(row interface{ Scan(dest ...any) error }) (model.Album, error) {
	var album model.Album
	var groupID sql.NullInt64
	err := row.Scan(&album.ID, &album.Name, &groupID, &album.MediaTypeFilter, &album.OrientationFilter, &album.CreatedAt, &album.UpdatedAt)
	if err != nil {
		return model.Album{}, err
	}
	if groupID.Valid {
		album.GroupID = int64Ptr(groupID)
	}
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
