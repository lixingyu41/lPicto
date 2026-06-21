ALTER TABLE scan_library
ADD COLUMN IF NOT EXISTS discovered_files INT NOT NULL DEFAULT 0;

ALTER TABLE scan_library
ADD COLUMN IF NOT EXISTS discovered_at BIGINT;

ALTER TABLE media_asset
ADD COLUMN IF NOT EXISTS nfo_size BIGINT;

ALTER TABLE media_asset
ADD COLUMN IF NOT EXISTS nfo_mtime TIMESTAMPTZ;

CREATE OR REPLACE VIEW assets AS
SELECT
  ma.id,
  fi.rel_path,
  COALESCE(f.rel_path, '') AS parent_rel_path,
  ma.basename AS filename,
  ma.ext,
  CASE ma.media_type WHEN 1 THEN 'image' WHEN 2 THEN 'video' ELSE 'unknown' END AS media_type,
  ma.mime_type,
  ma.size_bytes AS size,
  EXTRACT(EPOCH FROM ma.file_mtime)::BIGINT AS mtime,
  ma.width,
  ma.height,
  CASE WHEN ma.duration_ms IS NULL THEN NULL ELSE ma.duration_ms::DOUBLE PRECISION / 1000 END AS duration,
  EXTRACT(EPOCH FROM ma.captured_at)::BIGINT AS taken_at,
  EXTRACT(EPOCH FROM ma.imported_at)::BIGINT AS imported_at,
  EXTRACT(EPOCH FROM ma.sort_time)::BIGINT AS timeline_at,
  ma.cache_key,
  CASE WHEN ma.browser_playable THEN 1 ELSE 0 END AS browser_playable,
  CASE ma.status WHEN 0 THEN 'ok' WHEN 1 THEN 'processing' WHEN 2 THEN 'error' ELSE 'ok' END AS scan_status,
  ma.thumb_status,
  ma.preview_status,
  ma.video_poster_status,
  ma.video_proxy_status,
  ma.metadata_json::TEXT AS metadata_json,
  ma.nfo_json::TEXT AS nfo_json,
  ma.nfo_search_text,
  COALESCE(ma.error_text, ma.error_code) AS error,
  EXTRACT(EPOCH FROM ma.deleted_at)::BIGINT AS deleted_at,
  EXTRACT(EPOCH FROM ma.created_at)::BIGINT AS created_at,
  EXTRACT(EPOCH FROM ma.updated_at)::BIGINT AS updated_at,
  ma.nfo_size,
  EXTRACT(EPOCH FROM ma.nfo_mtime)::BIGINT AS nfo_mtime
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id AND fi.missing = false
LEFT JOIN folder f ON f.id = ma.folder_id;
