ALTER TABLE media_asset
ADD COLUMN IF NOT EXISTS has_subtitle BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS has_danmaku BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS smart_collections (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  rule_json JSONB NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS album_asset (
  album_id BIGINT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  asset_id BIGINT NOT NULL REFERENCES media_asset(id) ON DELETE CASCADE,
  created_at BIGINT NOT NULL,
  PRIMARY KEY(album_id, asset_id)
);

CREATE INDEX IF NOT EXISTS idx_album_asset_asset_album
ON album_asset (asset_id, album_id);

CREATE INDEX IF NOT EXISTS idx_media_asset_sha256_size
ON media_asset (sha256, size_bytes, id)
WHERE sha256 IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_media_asset_hidden
ON media_asset (hidden, id)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_media_asset_sidecars
ON media_asset (has_danmaku, has_subtitle, id)
WHERE deleted_at IS NULL;

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
  EXTRACT(EPOCH FROM ma.nfo_mtime)::BIGINT AS nfo_mtime,
  ma.hidden,
  encode(ma.sha256, 'hex') AS sha256,
  ma.has_subtitle,
  ma.has_danmaku
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id AND fi.missing = false
LEFT JOIN folder f ON f.id = ma.folder_id;

CREATE OR REPLACE VIEW asset_records AS
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
  EXTRACT(EPOCH FROM ma.nfo_mtime)::BIGINT AS nfo_mtime,
  ma.hidden,
  encode(ma.sha256, 'hex') AS sha256,
  ma.has_subtitle,
  ma.has_danmaku,
  fi.missing
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id
LEFT JOIN folder f ON f.id = ma.folder_id;
