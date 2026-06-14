package db

import (
	"context"
	"fmt"

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
	Preview     WorkStatusCounts
	VideoPoster WorkStatusCounts
	VideoProxy  WorkStatusCounts
}

func (d *DB) ProcessingProgress(ctx context.Context) (ProcessingProgress, error) {
	var progress ProcessingProgress
	if err := d.conn.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN media_type = 'image' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN media_type = 'video' THEN 1 ELSE 0 END), 0)
FROM assets
WHERE deleted_at IS NULL`).Scan(&progress.AssetTotal, &progress.ImageTotal, &progress.VideoTotal); err != nil {
		return ProcessingProgress{}, err
	}

	var err error
	if progress.Thumb, err = d.statusCounts(ctx, "thumb_status", model.MediaTypeImage); err != nil {
		return ProcessingProgress{}, err
	}
	if progress.Preview, err = d.statusCounts(ctx, "preview_status", model.MediaTypeImage); err != nil {
		return ProcessingProgress{}, err
	}
	if progress.VideoPoster, err = d.statusCounts(ctx, "video_poster_status", model.MediaTypeVideo); err != nil {
		return ProcessingProgress{}, err
	}
	if progress.VideoProxy, err = d.statusCounts(ctx, "video_proxy_status", model.MediaTypeVideo); err != nil {
		return ProcessingProgress{}, err
	}
	return progress, nil
}

func (d *DB) statusCounts(ctx context.Context, field string, mediaType string) (WorkStatusCounts, error) {
	if !validStatusField(field) {
		return WorkStatusCounts{}, fmt.Errorf("invalid status field %s", field)
	}
	var counts WorkStatusCounts
	query := fmt.Sprintf(`
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN %[1]s = 'ready' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'pending' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'processing' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'error' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN %[1]s = 'not_required' THEN 1 ELSE 0 END), 0)
FROM assets
WHERE deleted_at IS NULL AND media_type = ?`, field)
	err := d.conn.QueryRowContext(ctx, query, mediaType).Scan(
		&counts.Total,
		&counts.Ready,
		&counts.Pending,
		&counts.Processing,
		&counts.Error,
		&counts.NotRequired,
	)
	return counts, err
}
