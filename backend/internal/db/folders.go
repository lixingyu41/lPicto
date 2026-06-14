package db

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"sort"
	"strings"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

func (d *DB) EnsureFolder(ctx context.Context, rel string) error {
	now := util.UnixNow()
	parent := storage.ParentRelPath(rel)
	var parentValue any
	if rel == "" {
		parentValue = nil
	} else {
		parentValue = parent
	}
	_, err := d.conn.ExecContext(ctx, `
INSERT INTO folders (rel_path, name, parent_rel_path, depth, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(rel_path) DO UPDATE SET
  name = excluded.name,
  parent_rel_path = excluded.parent_rel_path,
  depth = excluded.depth,
  updated_at = excluded.updated_at`,
		rel, storage.FolderName(rel), parentValue, storage.FolderDepth(rel), now)
	return err
}

func (d *DB) EnsureAssetFolders(ctx context.Context, assetRel string) error {
	for _, rel := range storage.AncestorFolders(assetRel) {
		if err := d.EnsureFolder(ctx, rel); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) RefreshFolders(ctx context.Context) error {
	if err := d.rebuildFoldersFromAssets(ctx); err != nil {
		return err
	}
	now := util.UnixNow()
	_, err := d.conn.ExecContext(ctx, `
UPDATE folders
SET
  asset_count = (
    SELECT COUNT(*) FROM assets
    WHERE assets.deleted_at IS NULL AND assets.parent_rel_path = folders.rel_path
  ),
  recursive_asset_count = (
    SELECT COUNT(*) FROM assets
    WHERE assets.deleted_at IS NULL AND (
      folders.rel_path = '' OR
      assets.parent_rel_path = folders.rel_path OR
      assets.parent_rel_path LIKE folders.rel_path || '/%'
    )
  ),
  cover_asset_id = (
    SELECT id FROM assets
    WHERE assets.deleted_at IS NULL AND (
      folders.rel_path = '' OR
      assets.parent_rel_path = folders.rel_path OR
      assets.parent_rel_path LIKE folders.rel_path || '/%'
    )
    ORDER BY timeline_at DESC, id DESC
    LIMIT 1
  ),
  updated_at = ?`, now)
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, `DELETE FROM folders WHERE rel_path <> '' AND recursive_asset_count = 0`)
	return err
}

func (d *DB) rebuildFoldersFromAssets(ctx context.Context) error {
	rows, err := d.conn.QueryContext(ctx, `SELECT DISTINCT parent_rel_path FROM assets WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	needed := map[string]struct{}{"": {}}
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			return err
		}
		if parent == "" {
			continue
		}
		current := ""
		for _, part := range strings.Split(parent, "/") {
			if current == "" {
				current = part
			} else {
				current += "/" + part
			}
			needed[current] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var rels []string
	for rel := range needed {
		rels = append(rels, rel)
	}
	sort.Slice(rels, func(i, j int) bool {
		if storage.FolderDepth(rels[i]) == storage.FolderDepth(rels[j]) {
			return rels[i] < rels[j]
		}
		return storage.FolderDepth(rels[i]) < storage.FolderDepth(rels[j])
	})
	for _, rel := range rels {
		if err := d.EnsureFolder(ctx, rel); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) GetFolder(ctx context.Context, id int64) (model.Folder, error) {
	if id == 0 {
		return d.GetFolderByRel(ctx, "")
	}
	row := d.conn.QueryRowContext(ctx, folderSelectSQL()+` WHERE id = ?`, id)
	return scanFolder(row)
}

func (d *DB) GetFolderByRel(ctx context.Context, rel string) (model.Folder, error) {
	row := d.conn.QueryRowContext(ctx, folderSelectSQL()+` WHERE rel_path = ?`, rel)
	return scanFolder(row)
}

func (d *DB) ListFolders(ctx context.Context, parentID int64) ([]model.Folder, error) {
	parent, err := d.GetFolder(ctx, parentID)
	if err != nil {
		return nil, err
	}
	rows, err := d.conn.QueryContext(ctx, folderSelectSQL()+` WHERE parent_rel_path = ? ORDER BY name COLLATE NOCASE ASC`, parent.RelPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFolderRows(rows)
}

func (d *DB) FolderTree(ctx context.Context) ([]model.Folder, error) {
	rows, err := d.conn.QueryContext(ctx, folderSelectSQL()+` ORDER BY depth ASC, rel_path COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFolderRows(rows)
}

func folderSelectSQL() string {
	return `SELECT id, rel_path, name, parent_rel_path, depth, asset_count, recursive_asset_count, cover_asset_id, updated_at FROM folders`
}

func scanFolder(row interface{ Scan(dest ...any) error }) (model.Folder, error) {
	var folder model.Folder
	var parent sql.NullString
	var cover sql.NullInt64
	err := row.Scan(&folder.ID, &folder.RelPath, &folder.Name, &parent, &folder.Depth, &folder.AssetCount, &folder.RecursiveAssetCount, &cover, &folder.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Folder{}, err
	}
	if err != nil {
		return model.Folder{}, err
	}
	folder.ParentRelPath = stringPtr(parent)
	folder.CoverAssetID = int64Ptr(cover)
	return folder, nil
}

func scanFolderRows(rows *sql.Rows) ([]model.Folder, error) {
	var items []model.Folder
	for rows.Next() {
		folder, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, folder)
	}
	return items, rows.Err()
}

func FolderParentRel(rel string) *string {
	if rel == "" {
		return nil
	}
	parent := path.Dir(rel)
	if parent == "." || parent == "/" {
		parent = ""
	}
	return &parent
}
