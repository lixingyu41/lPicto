package db

import (
	"context"
	"database/sql"
)

func (d *DB) SearchAssetIDs(ctx context.Context, opts AssetListOptions) ([]int64, error) {
	where, args := assetFilterSQL(opts, true)
	return d.assetIDsByFilterSQL(ctx, "assets", where, args)
}

func (d *DB) FolderAssetIDs(ctx context.Context, folderID int64, opts AssetListOptions) ([]int64, error) {
	folder, err := d.getFolderRaw(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if opts.Recursive {
		ids, err := d.DescendantFolderIDs(ctx, folder.RelPath)
		if err != nil {
			return nil, err
		}
		opts.FolderIDs = ids
	} else {
		opts.FolderRel = &folder.RelPath
	}
	where, args := assetFilterSQL(opts, false)
	return d.assetIDsByFilterSQL(ctx, "assets", where, args)
}

func (d *DB) AlbumAssetIDs(ctx context.Context, albumID int64, opts AssetListOptions) ([]int64, error) {
	album, err := d.getAlbumWithSources(ctx, albumID)
	if err != nil {
		return nil, err
	}
	where, args := albumAssetFilterSQL(album, opts)
	return d.assetIDsByFilterSQL(ctx, "assets", where, args)
}

func (d *DB) SystemCollectionAssetIDs(ctx context.Context, kind string, opts AssetListOptions) ([]int64, error) {
	source, where, args, ok := d.systemCollectionFilter(kind, opts)
	if !ok {
		return nil, sql.ErrNoRows
	}
	return d.assetIDsByFilterSQL(ctx, source, where, args)
}

func (d *DB) assetIDsByFilterSQL(ctx context.Context, source string, where string, args []any) ([]int64, error) {
	rows, err := d.conn.QueryContext(ctx, "SELECT id FROM "+source+" WHERE "+where+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
