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
	FolderRelPaths    []string
	Sources           []AlbumSourceCreate
	MediaTypeFilter   string
	OrientationFilter string
}

func (d *DB) ListAlbums(ctx context.Context) ([]model.Album, error) {
	rows, err := d.conn.QueryContext(ctx, albumSelectSQL()+` ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	albums, err := scanAlbumRows(rows)
	if err != nil {
		return nil, err
	}
	for i := range albums {
		if err := d.loadAlbumDetails(ctx, &albums[i]); err != nil {
			return nil, err
		}
	}
	return albums, nil
}

func (d *DB) GetAlbum(ctx context.Context, id int64) (model.Album, error) {
	row := d.conn.QueryRowContext(ctx, albumSelectSQL()+` WHERE id = ?`, id)
	album, err := scanAlbum(row)
	if err != nil {
		return model.Album{}, err
	}
	if err := d.loadAlbumDetails(ctx, &album); err != nil {
		return model.Album{}, err
	}
	return album, nil
}

func (d *DB) CreateAlbum(ctx context.Context, p AlbumCreate) (model.Album, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.Album{}, errors.New("album name is required")
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
INSERT INTO albums (name, media_type_filter, orientation_filter, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)`, name, mediaFilter, orientationFilter, now, now)
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

func (d *DB) DeleteAlbum(ctx context.Context, id int64) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	return err
}

func (d *DB) TouchAlbum(ctx context.Context, id int64) error {
	_, err := d.conn.ExecContext(ctx, `UPDATE albums SET updated_at = ? WHERE id = ?`, util.UnixNow(), id)
	return err
}

func (d *DB) ListAlbumAssets(ctx context.Context, albumID int64, opts AssetListOptions) (model.Page[model.Asset], error) {
	album, err := d.GetAlbum(ctx, albumID)
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

func (d *DB) AlbumNeighbors(ctx context.Context, albumID int64, opts NeighborOptions) (Neighbors, error) {
	if opts.Limit <= 0 {
		opts.Limit = 5
	}
	current, err := d.GetAsset(ctx, opts.AssetID)
	if err != nil {
		return Neighbors{}, err
	}
	album, err := d.GetAlbum(ctx, albumID)
	if err != nil {
		return Neighbors{}, err
	}
	filterOpts := AssetListOptions{Sort: opts.Sort, Query: opts.Query}
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
	album, err := d.GetAlbum(ctx, id)
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
	where, args := albumAssetFilterSQL(album, AssetListOptions{})
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
	where := []string{"deleted_at IS NULL", albumSourceRuleExists}
	args := []any{album.ID}
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
	return `SELECT id, name, media_type_filter, orientation_filter, created_at, updated_at FROM albums`
}

func scanAlbum(row interface{ Scan(dest ...any) error }) (model.Album, error) {
	var album model.Album
	err := row.Scan(&album.ID, &album.Name, &album.MediaTypeFilter, &album.OrientationFilter, &album.CreatedAt, &album.UpdatedAt)
	if err != nil {
		return model.Album{}, err
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
