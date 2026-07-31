package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/util"
)

const (
	CollectionKindSystem = "system"
	CollectionKindSmart  = "smart"

	SystemCollectionUnclassified   = "unclassified"
	SystemCollectionUnrated        = "unrated"
	SystemCollectionUntagged       = "untagged"
	SystemCollectionWithDanmaku    = "with_danmaku"
	SystemCollectionWithSubtitles  = "with_subtitles"
	SystemCollectionNeedsTranscode = "needs_transcode"
	SystemCollectionDuplicates     = "duplicates"
	SystemCollectionMissing        = "missing"
	SystemCollectionHidden         = "hidden"
	SystemCollectionAIPending      = "ai_pending"
	SystemCollectionAIReady        = "ai_ready"
	SystemCollectionAIFailed       = "ai_failed"
)

var systemCollections = []model.Collection{
	{ID: SystemCollectionUnclassified, Name: "未分类", Kind: CollectionKindSystem, SystemKind: SystemCollectionUnclassified},
	{ID: SystemCollectionUnrated, Name: "未评分", Kind: CollectionKindSystem, SystemKind: SystemCollectionUnrated},
	{ID: SystemCollectionUntagged, Name: "无标签", Kind: CollectionKindSystem, SystemKind: SystemCollectionUntagged},
	{ID: SystemCollectionWithDanmaku, Name: "有弹幕", Kind: CollectionKindSystem, SystemKind: SystemCollectionWithDanmaku},
	{ID: SystemCollectionWithSubtitles, Name: "有字幕", Kind: CollectionKindSystem, SystemKind: SystemCollectionWithSubtitles},
	{ID: SystemCollectionNeedsTranscode, Name: "需要转码", Kind: CollectionKindSystem, SystemKind: SystemCollectionNeedsTranscode},
	{ID: SystemCollectionDuplicates, Name: "重复文件", Kind: CollectionKindSystem, SystemKind: SystemCollectionDuplicates},
	{ID: SystemCollectionMissing, Name: "缺失/不可访问", Kind: CollectionKindSystem, SystemKind: SystemCollectionMissing},
	{ID: SystemCollectionHidden, Name: "隐藏项", Kind: CollectionKindSystem, SystemKind: SystemCollectionHidden},
	{ID: SystemCollectionAIPending, Name: "AI 待分析", Kind: CollectionKindSystem, SystemKind: SystemCollectionAIPending},
	{ID: SystemCollectionAIReady, Name: "AI 已分析", Kind: CollectionKindSystem, SystemKind: SystemCollectionAIReady},
	{ID: SystemCollectionAIFailed, Name: "AI 分析失败", Kind: CollectionKindSystem, SystemKind: SystemCollectionAIFailed},
}

type CollectionCreate struct {
	Name     string
	RuleJSON string
}

func SystemCollections() []model.Collection {
	items := make([]model.Collection, len(systemCollections))
	copy(items, systemCollections)
	return items
}

func IsSystemCollectionKind(kind string) bool {
	for _, item := range systemCollections {
		if item.SystemKind == kind {
			return true
		}
	}
	return false
}

func (d *DB) ListSmartCollections(ctx context.Context) ([]model.Collection, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT id, name, rule_json::TEXT, created_at, updated_at
FROM smart_collections
ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Collection, 0)
	for rows.Next() {
		var id int64
		var name, ruleJSON string
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &name, &ruleJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		items = append(items, smartCollectionModel(id, name, ruleJSON, createdAt, updatedAt))
	}
	return items, rows.Err()
}

func (d *DB) CreateSmartCollection(ctx context.Context, p CollectionCreate) (model.Collection, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.Collection{}, errors.New("collection name is required")
	}
	ruleJSON := strings.TrimSpace(p.RuleJSON)
	if ruleJSON == "" {
		ruleJSON = "{}"
	}
	now := util.UnixNow()
	var id int64
	err := d.conn.QueryRowContext(ctx, `
INSERT INTO smart_collections (name, rule_json, created_at, updated_at)
VALUES (?, ?::jsonb, ?, ?)
RETURNING id`, name, ruleJSON, now, now).Scan(&id)
	if err != nil {
		return model.Collection{}, err
	}
	return d.GetSmartCollection(ctx, id)
}

func (d *DB) GetSmartCollection(ctx context.Context, id int64) (model.Collection, error) {
	var name, ruleJSON string
	var createdAt, updatedAt int64
	err := d.conn.QueryRowContext(ctx, `
SELECT name, rule_json::TEXT, created_at, updated_at
FROM smart_collections
WHERE id = ?`, id).Scan(&name, &ruleJSON, &createdAt, &updatedAt)
	if err != nil {
		return model.Collection{}, err
	}
	return smartCollectionModel(id, name, ruleJSON, createdAt, updatedAt), nil
}

func (d *DB) UpdateSmartCollection(ctx context.Context, id int64, p CollectionCreate) (model.Collection, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.Collection{}, errors.New("collection name is required")
	}
	ruleJSON := strings.TrimSpace(p.RuleJSON)
	if ruleJSON == "" {
		ruleJSON = "{}"
	}
	result, err := d.conn.ExecContext(ctx, `
UPDATE smart_collections
SET name = ?, rule_json = ?::jsonb, updated_at = ?
WHERE id = ?`, name, ruleJSON, util.UnixNow(), id)
	if err != nil {
		return model.Collection{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return model.Collection{}, sql.ErrNoRows
	}
	return d.GetSmartCollection(ctx, id)
}

func (d *DB) DeleteSmartCollection(ctx context.Context, id int64) error {
	result, err := d.conn.ExecContext(ctx, `DELETE FROM smart_collections WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) ListSystemCollectionAssets(ctx context.Context, kind string, opts AssetListOptions) (model.Page[model.Asset], error) {
	source, where, args, ok := d.systemCollectionFilter(kind, opts)
	if !ok {
		return model.Page[model.Asset]{}, sql.ErrNoRows
	}
	if kind == SystemCollectionDuplicates {
		return d.listDuplicateCollectionAssets(ctx, where, args, opts)
	}
	return d.ListAssetsByFilterSQL(ctx, source, where, args, opts)
}

func (d *DB) listDuplicateCollectionAssets(ctx context.Context, where string, args []any, opts AssetListOptions) (model.Page[model.Asset], error) {
	page := opts.Page
	if page <= 0 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	limit := pageSize + 1
	offset := (page - 1) * pageSize
	query := duplicateGroupedRankedSQL(where, opts.Sort) + assetSelectSQLFrom("ranked") +
		" ORDER BY " + duplicateGroupSortSQL(opts.Sort, opts.Group) + " LIMIT ? OFFSET ?"
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := d.conn.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	defer rows.Close()
	items, err := scanAssetRows(rows)
	if err != nil {
		return model.Page[model.Asset]{}, err
	}
	hasMore := len(items) > pageSize
	if hasMore {
		items = items[:pageSize]
	}
	return model.Page[model.Asset]{Items: items, Page: page, PageSize: pageSize, HasMore: hasMore}, nil
}

func duplicateGroupedRankedSQL(where string, sort string) string {
	return `WITH filtered AS (
  SELECT * FROM assets WHERE ` + where + `
), ranked AS (
  SELECT filtered.*,
    FIRST_VALUE(timeline_at) OVER duplicate_window AS duplicate_timeline_at,
    FIRST_VALUE(imported_at) OVER duplicate_window AS duplicate_imported_at,
    FIRST_VALUE(size) OVER duplicate_window AS duplicate_size,
		FIRST_VALUE(filename_sort_key) OVER duplicate_window AS duplicate_filename_sort_key,
		FIRST_VALUE(lower(filename)) OVER duplicate_window AS duplicate_filename,
		FIRST_VALUE(parent_rel_path) OVER duplicate_window AS duplicate_parent_rel_path,
		FIRST_VALUE(id) OVER duplicate_window AS duplicate_id
  FROM filtered
  WINDOW duplicate_window AS (PARTITION BY sha256, size ORDER BY ` + sortSQL(sort) + `)
) `
}

func duplicateGroupSortSQL(sort string, group string) string {
	var groupOrder string
	switch sort {
	case "timeline_asc":
		groupOrder = "duplicate_timeline_at ASC, duplicate_id ASC"
	case "filename", "filename_asc":
		groupOrder = "duplicate_filename_sort_key ASC, duplicate_filename ASC, duplicate_id ASC"
	case "filename_desc":
		groupOrder = "duplicate_filename_sort_key DESC, duplicate_filename DESC, duplicate_id DESC"
	case "size", "size_desc":
		groupOrder = "duplicate_size DESC, duplicate_id DESC"
	case "size_asc":
		groupOrder = "duplicate_size ASC, duplicate_id ASC"
	case "imported_asc":
		groupOrder = "duplicate_imported_at ASC, duplicate_id ASC"
	case "imported_desc":
		groupOrder = "duplicate_imported_at DESC, duplicate_id DESC"
	default:
		groupOrder = "duplicate_timeline_at DESC, duplicate_id DESC"
	}
	if group == assetGroupFolder {
		groupOrder = "lower(duplicate_parent_rel_path) ASC, duplicate_parent_rel_path ASC, " + groupOrder
	}
	return groupOrder + ", sha256 ASC, size ASC, " + sortSQL(sort)
}

func (d *DB) CountSystemCollectionAssets(ctx context.Context, kind string, opts AssetListOptions) (int, error) {
	source, where, args, ok := d.systemCollectionFilter(kind, opts)
	if !ok {
		return 0, sql.ErrNoRows
	}
	return d.CountAssetsByFilterSQL(ctx, source, where, args)
}

func (d *DB) SystemCollectionAnchors(ctx context.Context, kind string, opts AssetListOptions) (LibraryAnchorResult, error) {
	if kind == SystemCollectionMissing {
		return LibraryAnchorResult{}, nil
	}
	_, where, args, ok := d.systemCollectionFilter(kind, opts)
	if !ok {
		return LibraryAnchorResult{}, sql.ErrNoRows
	}
	return d.anchorsForFilter(ctx, where, args, opts.Sort, opts.Group, opts.PageSize)
}

func (d *DB) SystemCollectionNeighbors(ctx context.Context, kind string, assetID int64, opts AssetListOptions, limit int) (Neighbors, error) {
	if limit <= 0 {
		limit = 5
	}
	source, where, args, ok := d.systemCollectionFilter(kind, opts)
	if !ok {
		return Neighbors{}, sql.ErrNoRows
	}
	prefix := systemCollectionOrderedSQL(kind, source, where, opts.Sort, opts.Group)
	query := prefix + `,
current_row AS (
  SELECT item_row FROM ordered WHERE id = ?
)
` + assetSelectSQLFrom("ordered") + `
CROSS JOIN current_row
WHERE ordered.item_row BETWEEN current_row.item_row - ? AND current_row.item_row + ?
ORDER BY ordered.item_row ASC`
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, assetID, limit, limit)
	rows, err := d.conn.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return Neighbors{}, err
	}
	defer rows.Close()
	items, err := scanAssetRows(rows)
	if err != nil {
		return Neighbors{}, err
	}
	currentIndex := -1
	for index := range items {
		if items[index].ID == assetID {
			currentIndex = index
			break
		}
	}
	if currentIndex < 0 {
		return Neighbors{}, sql.ErrNoRows
	}
	previous := append([]model.Asset{}, items[:currentIndex]...)
	for left, right := 0, len(previous)-1; left < right; left, right = left+1, right-1 {
		previous[left], previous[right] = previous[right], previous[left]
	}
	next := append([]model.Asset{}, items[currentIndex+1:]...)
	return Neighbors{Current: items[currentIndex], Previous: previous, Next: next}, nil
}

func (d *DB) SystemCollectionAssetPosition(ctx context.Context, kind string, assetID int64, opts AssetListOptions) (AssetPosition, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	source, where, args, ok := d.systemCollectionFilter(kind, opts)
	if !ok {
		return AssetPosition{}, sql.ErrNoRows
	}
	prefix := systemCollectionOrderedSQL(kind, source, where, opts.Sort, opts.Group)
	query := prefix + `
SELECT item_row - 1, (SELECT COUNT(*) FROM ordered)
FROM ordered
WHERE id = ?`
	queryArgs := append(append([]any{}, args...), assetID)
	var index, total int
	if err := d.conn.QueryRowContext(ctx, query, queryArgs...).Scan(&index, &total); err != nil {
		return AssetPosition{}, err
	}
	position := 0.0
	if total > 1 {
		position = float64(index) / float64(total-1)
	}
	return AssetPosition{Index: index, Page: index/opts.PageSize + 1, Position: position, Total: total}, nil
}

func systemCollectionOrderedSQL(kind string, source string, where string, sort string, group string) string {
	if kind == SystemCollectionDuplicates {
		return duplicateGroupedRankedSQL(where, sort) + `,
ordered AS (
  SELECT ranked.*, ROW_NUMBER() OVER (ORDER BY ` + duplicateGroupSortSQL(sort, group) + `) AS item_row
  FROM ranked
)`
	}
	if source == "assets" && group == assetGroupFolder {
		return folderGroupedRankedSQL(where, sort) + `,
ordered AS (
  SELECT ranked.*, ROW_NUMBER() OVER (ORDER BY ` + folderGroupSortSQL(sort) + `) AS item_row
  FROM ranked
)`
	}
	return `WITH filtered AS (
  SELECT * FROM ` + source + ` WHERE ` + where + `
), ordered AS (
  SELECT filtered.*, ROW_NUMBER() OVER (ORDER BY ` + groupedSortSQL(group, sort) + `) AS item_row
  FROM filtered
)`
}

func (d *DB) systemCollectionFilter(kind string, opts AssetListOptions) (string, string, []any, bool) {
	if !IsSystemCollectionKind(kind) {
		return "", "", nil, false
	}
	source := "assets"
	baseOpts := opts
	baseOpts.IncludeHidden = kind == SystemCollectionHidden
	where, args := assetFilterSQL(baseOpts, false)
	switch kind {
	case SystemCollectionUnclassified:
		where += " AND NOT " + albumMembershipExistsSQL()
	case SystemCollectionUnrated:
		where += " AND " + assetRatingSQL("assets") + " = 0"
	case SystemCollectionUntagged:
		where += ` AND NOT EXISTS (SELECT 1 FROM asset_tag WHERE asset_tag.asset_id = assets.id)`
	case SystemCollectionWithDanmaku:
		where += " AND has_danmaku = true"
	case SystemCollectionWithSubtitles:
		where += " AND has_subtitle = true"
	case SystemCollectionNeedsTranscode:
		where += " AND media_type = 'video' AND (browser_playable = 0 OR video_proxy_status IN ('pending', 'processing', 'error'))"
	case SystemCollectionDuplicates:
		where += ` AND sha256 IS NOT NULL AND sha256 <> '' AND EXISTS (
  SELECT 1
  FROM assets duplicate_asset
  WHERE duplicate_asset.id <> assets.id
    AND duplicate_asset.size = assets.size
    AND duplicate_asset.sha256 = assets.sha256
    AND duplicate_asset.deleted_at IS NULL
    AND duplicate_asset.hidden = false
)`
	case SystemCollectionHidden:
		where += " AND hidden = true"
	case SystemCollectionAIPending:
		where += ` AND media_type IN ('image','video') AND (NOT EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id) OR EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id AND (air.status IN ('pending','processing') OR air.input_cache_key<>assets.cache_key)))`
	case SystemCollectionAIReady:
		where += ` AND media_type IN ('image','video') AND EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id AND air.status='ready' AND air.input_cache_key=assets.cache_key)`
	case SystemCollectionAIFailed:
		where += ` AND media_type IN ('image','video') AND EXISTS (SELECT 1 FROM asset_ai_result air WHERE air.asset_id=assets.id AND air.status='failed' AND air.input_cache_key=assets.cache_key)`
	case SystemCollectionMissing:
		source = "asset_records"
		parts := []string{"missing = true", "is_live = true"}
		args = nil
		if !opts.IncludeHidden {
			parts = append(parts, "hidden = false")
		}
		if opts.Type == model.MediaTypeImage || opts.Type == model.MediaTypeVideo || opts.Type == model.MediaTypeAudio {
			parts = append(parts, "media_type = ?")
			args = append(args, opts.Type)
		}
		if opts.Query != "" {
			parts = append(parts, "lower(filename) LIKE ? ESCAPE '\\'")
			args = append(args, "%"+escapeLike(strings.ToLower(opts.Query))+"%")
		}
		where = strings.Join(parts, " AND ")
	}
	return source, where, args, true
}

func (d *DB) DuplicateHashCandidates(ctx context.Context, limit int) ([]model.Asset, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.conn.QueryContext(ctx, assetSelectSQL()+`
WHERE deleted_at IS NULL
  AND hidden = false
  AND sha256 IS NULL
  AND size IN (
    SELECT size
    FROM assets
    WHERE deleted_at IS NULL AND hidden = false
    GROUP BY size
    HAVING COUNT(*) > 1
  )
ORDER BY size DESC, id ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssetRows(rows)
}

func (d *DB) SetAssetSHA256Hex(ctx context.Context, assetID int64, hashHex string) error {
	if strings.TrimSpace(hashHex) == "" {
		return nil
	}
	_, err := d.conn.ExecContext(ctx, `UPDATE media_asset SET sha256 = decode(?, 'hex'), updated_at = now() WHERE id = ?`, hashHex, assetID)
	return err
}

func (d *DB) DuplicateGroups(ctx context.Context, limit int) ([]model.DuplicateGroup, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.conn.QueryContext(ctx, `
SELECT sha256, size, COUNT(*)::INT
FROM assets
WHERE deleted_at IS NULL AND hidden = false AND sha256 IS NOT NULL AND sha256 <> ''
GROUP BY sha256, size
HAVING COUNT(*) > 1
ORDER BY size DESC, sha256
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]model.DuplicateGroup, 0)
	for rows.Next() {
		var hash string
		var size int64
		var count int
		if err := rows.Scan(&hash, &size, &count); err != nil {
			return nil, err
		}
		items, err := d.duplicateGroupAssets(ctx, hash, size)
		if err != nil {
			return nil, err
		}
		groups = append(groups, model.DuplicateGroup{
			Key: fmt.Sprintf("%s:%d", hash, size), Size: size, SHA256: hash, Items: items,
		})
	}
	return groups, rows.Err()
}

func (d *DB) DuplicateDeleteCandidateIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.conn.QueryContext(ctx, `
WITH ranked AS (
  SELECT
    id,
    ROW_NUMBER() OVER (PARTITION BY sha256, size ORDER BY imported_at ASC, id ASC) AS duplicate_rank,
    COUNT(*) OVER (PARTITION BY sha256, size) AS duplicate_count
  FROM assets
  WHERE deleted_at IS NULL
    AND hidden = false
    AND sha256 IS NOT NULL
    AND sha256 <> ''
)
SELECT id
FROM ranked
WHERE duplicate_count > 1 AND duplicate_rank > 1
ORDER BY id`)
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

func (d *DB) duplicateGroupAssets(ctx context.Context, hash string, size int64) ([]model.Asset, error) {
	rows, err := d.conn.QueryContext(ctx, assetSelectSQL()+`
WHERE deleted_at IS NULL AND hidden = false AND sha256 = ? AND size = ?
ORDER BY parent_rel_path, filename, id`, hash, size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssetRows(rows)
}

func smartCollectionModel(id int64, name string, ruleJSON string, createdAt int64, updatedAt int64) model.Collection {
	return model.Collection{
		ID: fmt.Sprintf("smart-%d", id), Name: name, Kind: CollectionKindSmart, AssetCount: 0,
		RuleJSON: &ruleJSON, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}
