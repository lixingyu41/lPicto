CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS library (
  id BIGSERIAL PRIMARY KEY,
  public_id TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS library_root (
  id BIGSERIAL PRIMARY KEY,
  library_id BIGINT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL,
  position INT NOT NULL DEFAULT 0,
  UNIQUE(library_id, rel_path)
);

CREATE TABLE IF NOT EXISTS scan_library (
  id BIGSERIAL PRIMARY KEY,
  public_id TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS scan_library_root (
  id BIGSERIAL PRIMARY KEY,
  scan_library_id BIGINT NOT NULL REFERENCES scan_library(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL,
  position INT NOT NULL DEFAULT 0,
  UNIQUE(scan_library_id, rel_path)
);

CREATE TABLE IF NOT EXISTS folder (
  id BIGSERIAL PRIMARY KEY,
  library_id BIGINT NOT NULL DEFAULT 1,
  parent_id BIGINT REFERENCES folder(id) ON DELETE SET NULL,
  rel_path TEXT NOT NULL,
  name TEXT NOT NULL,
  depth INT NOT NULL,
  asset_count INT NOT NULL DEFAULT 0,
  recursive_asset_count INT NOT NULL DEFAULT 0,
  cover_asset_id BIGINT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(library_id, rel_path)
);

CREATE TABLE IF NOT EXISTS media_asset (
  id BIGSERIAL PRIMARY KEY,
  media_type SMALLINT NOT NULL,
  status SMALLINT NOT NULL DEFAULT 0,
  hidden BOOLEAN NOT NULL DEFAULT FALSE,
  deleted BOOLEAN NOT NULL DEFAULT FALSE,
  title TEXT,
  basename TEXT NOT NULL,
  ext TEXT NOT NULL,
  mime_type TEXT,
  width INT,
  height INT,
  aspect_ratio DOUBLE PRECISION,
  orientation SMALLINT,
  duration_ms BIGINT,
  fps DOUBLE PRECISION,
  video_codec TEXT,
  audio_codec TEXT,
  container TEXT,
  size_bytes BIGINT NOT NULL,
  file_mtime TIMESTAMPTZ NOT NULL,
  captured_at TIMESTAMPTZ,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sort_time TIMESTAMPTZ NOT NULL,
  sha256 BYTEA,
  phash BIGINT,
  dominant_color INT,
  blurhash TEXT,
  folder_id BIGINT REFERENCES folder(id) ON DELETE SET NULL,
  metadata_json JSONB,
  nfo_json JSONB,
  nfo_search_text TEXT,
  cache_key TEXT NOT NULL,
  browser_playable BOOLEAN NOT NULL DEFAULT FALSE,
  thumb_ready BOOLEAN NOT NULL DEFAULT FALSE,
  preview_ready BOOLEAN NOT NULL DEFAULT FALSE,
  proxy_ready BOOLEAN NOT NULL DEFAULT FALSE,
  thumb_status TEXT NOT NULL DEFAULT 'pending',
  preview_status TEXT NOT NULL DEFAULT 'not_required',
  video_poster_status TEXT NOT NULL DEFAULT 'not_required',
  video_proxy_status TEXT NOT NULL DEFAULT 'not_required',
  error_code TEXT,
  error_text TEXT,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS file_instance (
  id BIGSERIAL PRIMARY KEY,
  asset_id BIGINT NOT NULL REFERENCES media_asset(id) ON DELETE CASCADE,
  library_id BIGINT NOT NULL DEFAULT 1 REFERENCES library(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL,
  abs_path_hash BYTEA NOT NULL DEFAULT decode('', 'hex'),
  device_id TEXT,
  inode TEXT,
  size_bytes BIGINT NOT NULL,
  file_mtime TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  missing BOOLEAN NOT NULL DEFAULT FALSE,
  UNIQUE(library_id, rel_path)
);

CREATE TABLE IF NOT EXISTS media_variant (
  id BIGSERIAL PRIMARY KEY,
  asset_id BIGINT NOT NULL REFERENCES media_asset(id) ON DELETE CASCADE,
  variant_type SMALLINT NOT NULL,
  path TEXT NOT NULL,
  width INT,
  height INT,
  size_bytes BIGINT,
  codec TEXT,
  ready BOOLEAN NOT NULL DEFAULT FALSE,
  generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(asset_id, variant_type)
);

CREATE TABLE IF NOT EXISTS tag (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS asset_tag (
  asset_id BIGINT NOT NULL REFERENCES media_asset(id) ON DELETE CASCADE,
  tag_id BIGINT NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
  PRIMARY KEY(asset_id, tag_id)
);

CREATE TABLE IF NOT EXISTS media_job (
  id BIGSERIAL PRIMARY KEY,
  asset_id BIGINT REFERENCES media_asset(id) ON DELETE CASCADE,
  job_type TEXT NOT NULL,
  priority INT NOT NULL DEFAULT 100,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  error_text TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  UNIQUE(asset_id, job_type)
);

CREATE TABLE IF NOT EXISTS asset_preferences (
  asset_id BIGINT PRIMARY KEY REFERENCES media_asset(id) ON DELETE CASCADE,
  rotation INT NOT NULL DEFAULT 0,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS album_groups (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS albums (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  group_id BIGINT REFERENCES album_groups(id) ON DELETE SET NULL,
  media_type_filter TEXT NOT NULL DEFAULT 'all',
  orientation_filter TEXT NOT NULL DEFAULT 'all',
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS album_sources (
  id BIGSERIAL PRIMARY KEY,
  album_id BIGINT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  source_type TEXT NOT NULL,
  rel_path TEXT NOT NULL,
  recursive BOOLEAN NOT NULL DEFAULT TRUE,
  media_type_filter TEXT NOT NULL DEFAULT 'all',
  orientation_filter TEXT NOT NULL DEFAULT 'all',
  created_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS scan_runs (
  id BIGSERIAL PRIMARY KEY,
  status TEXT NOT NULL,
  started_at BIGINT NOT NULL,
  finished_at BIGINT,
  total_seen INT NOT NULL DEFAULT 0,
  assets_added INT NOT NULL DEFAULT 0,
  assets_updated INT NOT NULL DEFAULT 0,
  assets_deleted INT NOT NULL DEFAULT 0,
  errors INT NOT NULL DEFAULT 0,
  last_error TEXT
);

CREATE INDEX IF NOT EXISTS idx_asset_timeline
ON media_asset (sort_time DESC, id DESC)
INCLUDE (media_type, width, height, duration_ms, dominant_color, blurhash, thumb_ready, preview_ready)
WHERE deleted = false AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_asset_folder_timeline
ON media_asset (folder_id, sort_time DESC, id DESC)
INCLUDE (media_type, width, height, duration_ms, blurhash)
WHERE deleted = false AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_asset_image_timeline
ON media_asset (sort_time DESC, id DESC)
WHERE media_type = 1 AND deleted = false AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_asset_video_timeline
ON media_asset (sort_time DESC, id DESC)
WHERE media_type = 2 AND deleted = false AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_asset_metadata_gin
ON media_asset USING GIN (metadata_json);

CREATE INDEX IF NOT EXISTS idx_asset_basename_trgm
ON media_asset USING GIN (basename gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_asset_nfo_trgm
ON media_asset USING GIN (nfo_search_text gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_asset_imported_brin
ON media_asset USING BRIN (imported_at);

CREATE INDEX IF NOT EXISTS idx_file_instance_rel_path
ON file_instance (library_id, rel_path);

CREATE INDEX IF NOT EXISTS idx_folder_parent
ON folder (library_id, parent_id);

CREATE INDEX IF NOT EXISTS idx_folder_rel_path
ON folder (library_id, rel_path);

CREATE INDEX IF NOT EXISTS idx_albums_group_id
ON albums (group_id);

CREATE INDEX IF NOT EXISTS idx_album_sources_album_id
ON album_sources (album_id);

CREATE INDEX IF NOT EXISTS idx_album_sources_rel_path
ON album_sources (rel_path);

CREATE VIEW assets AS
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
  EXTRACT(EPOCH FROM ma.updated_at)::BIGINT AS updated_at
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id AND fi.missing = false
LEFT JOIN folder f ON f.id = ma.folder_id;

INSERT INTO library (id, public_id, name)
VALUES (1, 'default', '默认来源')
ON CONFLICT (id) DO NOTHING;

INSERT INTO library_root (library_id, rel_path, position)
VALUES (1, '', 0)
ON CONFLICT (library_id, rel_path) DO NOTHING;

INSERT INTO scan_library (id, public_id, name)
VALUES (1, 'default', '默认来源')
ON CONFLICT (id) DO NOTHING;

INSERT INTO scan_library_root (scan_library_id, rel_path, position)
VALUES (1, '', 0)
ON CONFLICT (scan_library_id, rel_path) DO NOTHING;

INSERT INTO folder (library_id, rel_path, name, parent_id, depth)
VALUES (1, '', '照片', NULL, 0)
ON CONFLICT (library_id, rel_path) DO NOTHING;
