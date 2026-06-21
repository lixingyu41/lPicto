package db

import (
	"context"
	"fmt"
	"strings"

	"lpicto/backend/internal/model"
)

type WorkStatusCounts struct {
	Total       int
	Ready       int
	Pending     int
	Processing  int
	Error       int
	NotRequired int
}

type ProcessingProgress struct {
	AssetTotal  int
	ImageTotal  int
	VideoTotal  int
	Thumb       WorkStatusCounts
	Transcode   WorkStatusCounts
	Preview     WorkStatusCounts
	VideoPoster WorkStatusCounts
	VideoProxy  WorkStatusCounts
}

func (d *DB) ProcessingProgress(ctx context.Context) (ProcessingProgress, error) {
	return d.processingProgress(ctx, "deleted_at IS NULL", nil)
}

func (d *DB) ProcessingProgressForRoots(ctx context.Context, roots []string) (ProcessingProgress, error) {
	where, args, err := assetRootsWhere(roots)
	if err != nil {
		return ProcessingProgress{}, err
	}
	return d.processingProgress(ctx, where, args)
}

func (d *DB) AssetCountForRoots(ctx context.Context, roots []string) (int, error) {
	where, args, err := assetRootsWhere(roots)
	if err != nil {
		return 0, err
	}
	var total int
	err = d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE `+where, args...).Scan(&total)
	return total, err
}

func (d *DB) AssetCountsForLibraries(ctx context.Context, libraries []ScanLibrary) (map[string]int, error) {
	counts := make(map[string]int, len(libraries))
	for _, library := range libraries {
		counts[library.ID] = 0
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT rel_path FROM assets WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		for _, library := range libraries {
			if AssetInScanFolders(rel, library.Roots) {
				counts[library.ID]++
			}
		}
	}
	return counts, rows.Err()
}

func (d *DB) processingProgress(ctx context.Context, where string, args []any) (ProcessingProgress, error) {
	var progress ProcessingProgress
	if err := d.conn.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN media_type = 'image' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN media_type = 'video' THEN 1 ELSE 0 END), 0)
FROM assets
WHERE `+where, args...).Scan(&progress.AssetTotal, &progress.ImageTotal, &progress.VideoTotal); err != nil {
		return ProcessingProgress{}, err
	}

	var err error
	if progress.Thumb, err = d.statusCounts(ctx, "thumb_status", "", where, args); err != nil {
		return ProcessingProgress{}, err
	}
	if progress.Preview, err = d.statusCounts(ctx, "preview_status", model.MediaTypeImage, where, args); err != nil {
		return ProcessingProgress{}, err
	}
	if progress.VideoPoster, err = d.statusCounts(ctx, "video_poster_status", model.MediaTypeVideo, where, args); err != nil {
		return ProcessingProgress{}, err
	}
	if progress.VideoProxy, err = d.statusCounts(ctx, "video_proxy_status", model.MediaTypeVideo, where, args); err != nil {
		return ProcessingProgress{}, err
	}
	if progress.Transcode, err = d.transcodeCounts(ctx, where, args); err != nil {
		return ProcessingProgress{}, err
	}
	return progress, nil
}

func (d *DB) statusCounts(ctx context.Context, field string, mediaType string, where string, args []any) (WorkStatusCounts, error) {
	if !validStatusField(field) {
		return WorkStatusCounts{}, fmt.Errorf("invalid status field %s", field)
	}
	var counts WorkStatusCounts
	queryArgs := append([]any(nil), args...)
	if mediaType != "" {
		where += " AND media_type = ?"
		queryArgs = append(queryArgs, mediaType)
	}
	query := fmt.Sprintf(`
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN %[1]s = 'ready' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'pending' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'processing' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'error' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'not_required' THEN 1 ELSE 0 END), 0)
FROM assets
WHERE `+where, field)
	err := d.conn.QueryRowContext(ctx, query, queryArgs...).Scan(
		&counts.Total,
		&counts.Ready,
		&counts.Pending,
		&counts.Processing,
		&counts.Error,
		&counts.NotRequired,
	)
	return counts, err
}

func (d *DB) transcodeCounts(ctx context.Context, where string, args []any) (WorkStatusCounts, error) {
	var counts WorkStatusCounts
	imageWhere := where + " AND media_type = ? AND preview_status <> 'not_required'"
	videoWhere := where + " AND media_type = ? AND video_proxy_status <> 'not_required'"
	imageArgs := append(append([]any(nil), args...), model.MediaTypeImage)
	videoArgs := append(append([]any(nil), args...), model.MediaTypeVideo)
	queryArgs := append(imageArgs, videoArgs...)
	err := d.conn.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN status = 'ready' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'processing' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
  0
FROM (
  SELECT preview_status AS status
  FROM assets
  WHERE `+imageWhere+`
  UNION ALL
  SELECT video_proxy_status AS status
  FROM assets
  WHERE `+videoWhere+`
)`, queryArgs...).Scan(
		&counts.Total,
		&counts.Ready,
		&counts.Pending,
		&counts.Processing,
		&counts.Error,
		&counts.NotRequired,
	)
	return counts, err
}

func assetRootsWhere(roots []string) (string, []any, error) {
	normalized, err := NormalizeScanFolders(roots)
	if err != nil {
		return "", nil, err
	}
	if len(normalized) == 0 {
		return "0 = 1", nil, nil
	}
	if len(normalized) == 1 && normalized[0] == "" {
		return "deleted_at IS NULL", nil, nil
	}
	clauses := make([]string, 0, len(normalized))
	args := make([]any, 0, len(normalized)*2)
	for _, root := range normalized {
		clauses = append(clauses, "(rel_path = ? OR rel_path LIKE ? ESCAPE '\\')")
		args = append(args, root, descendantPathLike(root))
	}
	return "deleted_at IS NULL AND (" + strings.Join(clauses, " OR ") + ")", args, nil
}
