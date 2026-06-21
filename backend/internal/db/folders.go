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
INSERT INTO folder (library_id, rel_path, name, parent_id, depth, updated_at)
VALUES (1, ?, ?, (SELECT id FROM folder WHERE library_id = 1 AND rel_path = ?), ?, ?)
ON CONFLICT(library_id, rel_path) DO UPDATE SET
  name = excluded.name,
  parent_id = excluded.parent_id,
  depth = excluded.depth,
  updated_at = excluded.updated_at`,
		rel, storage.FolderName(rel), parentValue, storage.FolderDepth(rel), unixTime(now))
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
	activeFolders, err := d.rebuildFoldersFromAssets(ctx)
	if err != nil {
		return err
	}
	if err := d.relinkAssetFolders(ctx); err != nil {
		return err
	}
	folders, err := d.folderStatsSnapshot(ctx)
	if err != nil {
		return err
	}
	folders, err = d.populateFolderStats(ctx, folders)
	if err != nil {
		return err
	}
	return d.writeFolderStats(ctx, folders, activeFolders)
}

func (d *DB) folderStatsSnapshot(ctx context.Context) ([]model.Folder, error) {
	rows, err := d.conn.QueryContext(ctx, folderSelectSQL()+` ORDER BY f.depth ASC, lower(f.rel_path) ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFolderRows(rows)
}

func (d *DB) writeFolderStats(ctx context.Context, folders []model.Folder, activeFolders map[string]struct{}) error {
	now := util.UnixNow()
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, folder := range folders {
		if _, err := tx.ExecContext(ctx, `
UPDATE folder
SET asset_count = ?,
    recursive_asset_count = ?,
    cover_asset_id = ?,
    updated_at = ?
WHERE id = ?`,
			folder.AssetCount, folder.RecursiveAssetCount, nullInt64(folder.CoverAssetID), unixTime(now), folder.ID); err != nil {
			return err
		}
	}
	for _, folder := range folders {
		if folder.RelPath == "" {
			continue
		}
		if _, ok := activeFolders[folder.RelPath]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM folder WHERE id = ?`, folder.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) rebuildFoldersFromAssets(ctx context.Context) (map[string]struct{}, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT DISTINCT fi.rel_path
FROM file_instance fi
JOIN media_asset ma ON ma.id = fi.asset_id
WHERE fi.missing = false AND ma.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needed := map[string]struct{}{"": {}}
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		parent := storage.ParentRelPath(rel)
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
		return nil, err
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
			return nil, err
		}
	}
	return needed, nil
}

func (d *DB) relinkAssetFolders(ctx context.Context) error {
	_, err := d.conn.ExecContext(ctx, `
WITH asset_folders AS (
  SELECT DISTINCT ON (fi.asset_id)
    fi.asset_id,
    f.id AS folder_id
  FROM file_instance fi
  JOIN media_asset ma ON ma.id = fi.asset_id
  JOIN folder f ON f.library_id = fi.library_id AND f.rel_path = CASE
    WHEN POSITION('/' IN REVERSE(fi.rel_path)) = 0 THEN ''
    ELSE LEFT(fi.rel_path, LENGTH(fi.rel_path) - POSITION('/' IN REVERSE(fi.rel_path)))
  END
  WHERE fi.missing = false AND ma.deleted_at IS NULL
  ORDER BY fi.asset_id, fi.library_id, fi.id
)
UPDATE media_asset ma
SET folder_id = asset_folders.folder_id,
    updated_at = now()
FROM asset_folders
WHERE ma.id = asset_folders.asset_id
  AND ma.folder_id IS DISTINCT FROM asset_folders.folder_id`)
	return err
}

func (d *DB) GetFolder(ctx context.Context, id int64) (model.Folder, error) {
	folder, err := d.getFolderRaw(ctx, id)
	if err != nil {
		return model.Folder{}, err
	}
	return d.populateFolderStat(ctx, folder)
}

func (d *DB) getFolderRaw(ctx context.Context, id int64) (model.Folder, error) {
	if id == 0 {
		return d.getFolderByRelRaw(ctx, "")
	}
	row := d.conn.QueryRowContext(ctx, folderSelectSQL()+` WHERE f.id = ?`, id)
	return scanFolder(row)
}

func (d *DB) GetFolderByRel(ctx context.Context, rel string) (model.Folder, error) {
	folder, err := d.getFolderByRelRaw(ctx, rel)
	if err != nil {
		return model.Folder{}, err
	}
	return d.populateFolderStat(ctx, folder)
}

func (d *DB) getFolderByRelRaw(ctx context.Context, rel string) (model.Folder, error) {
	row := d.conn.QueryRowContext(ctx, folderSelectSQL()+` WHERE f.rel_path = ?`, rel)
	return scanFolder(row)
}

func (d *DB) ListFolders(ctx context.Context, parentID int64) ([]model.Folder, error) {
	parent, err := d.getFolderRaw(ctx, parentID)
	if err != nil {
		return nil, err
	}
	rows, err := d.conn.QueryContext(ctx, folderSelectSQL()+` WHERE f.parent_id = ? ORDER BY lower(f.name) ASC`, parent.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folders, err := scanFolderRows(rows)
	if err != nil {
		return nil, err
	}
	for i := range folders {
		folders[i], err = d.populateFolderStat(ctx, folders[i])
		if err != nil {
			return nil, err
		}
	}
	return filterFoldersWithAssets(folders), nil
}

func (d *DB) FolderTree(ctx context.Context) ([]model.Folder, error) {
	return d.FolderTreeWithRoots(ctx, nil)
}

func (d *DB) FolderTreeWithRoots(ctx context.Context, includedRoots []string) ([]model.Folder, error) {
	rows, err := d.conn.QueryContext(ctx, folderSelectSQL()+` ORDER BY f.depth ASC, lower(f.rel_path) ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folders, err := scanFolderRows(rows)
	if err != nil {
		return nil, err
	}
	folders, err = d.populateFolderStats(ctx, folders)
	if err != nil {
		return nil, err
	}
	return filterFoldersWithAssets(folders, includedRoots), nil
}

func folderSelectSQL() string {
	return `SELECT f.id, f.rel_path, f.name, p.rel_path AS parent_rel_path, f.depth, f.asset_count, f.recursive_asset_count, f.cover_asset_id, EXTRACT(EPOCH FROM f.updated_at)::BIGINT AS updated_at FROM folder f LEFT JOIN folder p ON p.id = f.parent_id`
}

func (d *DB) populateFolderStat(ctx context.Context, folder model.Folder) (model.Folder, error) {
	recursiveWhere, recursiveArgs := folderAssetScopeSQL(folder.RelPath)
	countQuery := `SELECT
COALESCE(SUM(CASE WHEN parent_rel_path = ? THEN 1 ELSE 0 END), 0),
COUNT(*)
FROM assets WHERE deleted_at IS NULL AND thumb_status = 'ready'`
	args := []any{folder.RelPath}
	if recursiveWhere != "" {
		countQuery += " AND " + recursiveWhere
		args = append(args, recursiveArgs...)
	}
	if err := d.conn.QueryRowContext(ctx, countQuery, args...).Scan(&folder.AssetCount, &folder.RecursiveAssetCount); err != nil {
		return model.Folder{}, err
	}
	coverQuery := `SELECT id FROM assets WHERE deleted_at IS NULL AND thumb_status = 'ready'`
	coverArgs := append([]any{}, recursiveArgs...)
	if recursiveWhere != "" {
		coverQuery += " AND " + recursiveWhere
	}
	coverQuery += ` ORDER BY timeline_at DESC, id DESC LIMIT 1`
	var cover sql.NullInt64
	err := d.conn.QueryRowContext(ctx, coverQuery, coverArgs...).Scan(&cover)
	if errors.Is(err, sql.ErrNoRows) {
		folder.CoverAssetID = nil
		return folder, nil
	}
	if err != nil {
		return model.Folder{}, err
	}
	folder.CoverAssetID = int64Ptr(cover)
	return folder, nil
}

func folderAssetScopeSQL(rel string) (string, []any) {
	if rel == "" {
		return "", nil
	}
	return `(parent_rel_path = ? OR parent_rel_path LIKE ? ESCAPE '\')`, []any{rel, descendantPathLike(rel)}
}

func folderPathLikeSQL(expr string) string {
	return `replace(replace(replace(` + expr + `, '\', '\\'), '%', '\%'), '_', '\_') || '/%'`
}

func (d *DB) populateFolderStats(ctx context.Context, folders []model.Folder) ([]model.Folder, error) {
	if len(folders) == 0 {
		return folders, nil
	}
	relIndex := make(map[string]int, len(folders))
	for index := range folders {
		folders[index].AssetCount = 0
		folders[index].RecursiveAssetCount = 0
		folders[index].CoverAssetID = nil
		relIndex[folders[index].RelPath] = index
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT id, parent_rel_path, timeline_at, thumb_status FROM assets WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	coverIDs := make(map[string]int64, len(folders))
	coverTimeline := make(map[string]int64, len(folders))
	for rows.Next() {
		var id int64
		var parent string
		var timelineAt int64
		var thumbStatus string
		if err := rows.Scan(&id, &parent, &timelineAt, &thumbStatus); err != nil {
			return nil, err
		}
		if thumbStatus != model.StatusReady {
			continue
		}
		if index, ok := relIndex[parent]; ok {
			folders[index].AssetCount++
		}
		for _, rel := range folderAncestorRels(parent) {
			index, ok := relIndex[rel]
			if !ok {
				continue
			}
			folders[index].RecursiveAssetCount++
			currentTimeline, hasCover := coverTimeline[rel]
			if !hasCover || timelineAt > currentTimeline || (timelineAt == currentTimeline && id > coverIDs[rel]) {
				coverTimeline[rel] = timelineAt
				coverIDs[rel] = id
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for rel, id := range coverIDs {
		index, ok := relIndex[rel]
		if !ok {
			continue
		}
		coverID := id
		folders[index].CoverAssetID = &coverID
	}
	return folders, nil
}

func folderAncestorRels(rel string) []string {
	result := []string{""}
	if rel == "" {
		return result
	}
	current := ""
	for _, part := range strings.Split(rel, "/") {
		if part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		result = append(result, current)
	}
	return result
}

func filterFoldersWithAssets(folders []model.Folder, includedRoots ...[]string) []model.Folder {
	if len(includedRoots) > 0 && includedRoots[0] != nil && len(includedRoots[0]) == 0 {
		return []model.Folder{}
	}
	included := map[string]struct{}{}
	if len(includedRoots) > 0 {
		for _, root := range includedRoots[0] {
			included[root] = struct{}{}
		}
	}
	result := make([]model.Folder, 0, len(folders))
	for _, folder := range folders {
		_, keepRoot := included[folder.RelPath]
		if folder.RelPath == "" || folder.RecursiveAssetCount > 0 || keepRoot {
			result = append(result, folder)
		}
	}
	return result
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
