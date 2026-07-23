ALTER TABLE media_asset
ADD COLUMN IF NOT EXISTS video_bitrate BIGINT,
ADD COLUMN IF NOT EXISTS audio_bitrate BIGINT,
ADD COLUMN IF NOT EXISTS overall_bitrate BIGINT;

WITH parsed AS (
  SELECT
    ma.id,
    CASE
      WHEN COALESCE(video.avg_frame_rate, video.r_frame_rate, '') ~ '^[0-9]+(\.[0-9]+)?/[0-9]+(\.[0-9]+)?$'
      THEN split_part(COALESCE(video.avg_frame_rate, video.r_frame_rate), '/', 1)::DOUBLE PRECISION
           / NULLIF(split_part(COALESCE(video.avg_frame_rate, video.r_frame_rate), '/', 2)::DOUBLE PRECISION, 0)
      WHEN COALESCE(video.avg_frame_rate, video.r_frame_rate, '') ~ '^[0-9]+(\.[0-9]+)?$'
      THEN COALESCE(video.avg_frame_rate, video.r_frame_rate)::DOUBLE PRECISION
    END AS fps,
    NULLIF(trim(concat_ws(' ', video.codec_name, NULLIF(video.profile, ''))), '') AS video_codec,
    NULLIF(trim(concat_ws(' ', audio.codec_name, NULLIF(audio.profile, ''))), '') AS audio_codec,
    NULLIF(ma.metadata_json->'format'->>'format_name', '') AS container,
    CASE WHEN COALESCE(video.bit_rate, '') ~ '^[0-9]+$' THEN video.bit_rate::BIGINT END AS video_bitrate,
    CASE WHEN COALESCE(audio.bit_rate, '') ~ '^[0-9]+$' THEN audio.bit_rate::BIGINT END AS audio_bitrate,
    CASE WHEN COALESCE(ma.metadata_json->'format'->>'bit_rate', '') ~ '^[0-9]+$'
      THEN (ma.metadata_json->'format'->>'bit_rate')::BIGINT END AS overall_bitrate
  FROM media_asset ma
  LEFT JOIN LATERAL (
    SELECT
      stream->>'codec_name' AS codec_name,
      stream->>'profile' AS profile,
      stream->>'bit_rate' AS bit_rate,
      stream->>'avg_frame_rate' AS avg_frame_rate,
      stream->>'r_frame_rate' AS r_frame_rate
    FROM jsonb_array_elements(COALESCE(ma.metadata_json->'streams', '[]'::jsonb)) stream
    WHERE stream->>'codec_type' = 'video'
    LIMIT 1
  ) video ON true
  LEFT JOIN LATERAL (
    SELECT stream->>'codec_name' AS codec_name, stream->>'profile' AS profile, stream->>'bit_rate' AS bit_rate
    FROM jsonb_array_elements(COALESCE(ma.metadata_json->'streams', '[]'::jsonb)) stream
    WHERE stream->>'codec_type' = 'audio'
    LIMIT 1
  ) audio ON true
  WHERE ma.metadata_json IS NOT NULL
)
UPDATE media_asset ma
SET fps = COALESCE(ma.fps, parsed.fps),
    video_codec = COALESCE(ma.video_codec, parsed.video_codec),
    audio_codec = COALESCE(ma.audio_codec, parsed.audio_codec),
    container = COALESCE(ma.container, parsed.container),
    video_bitrate = COALESCE(ma.video_bitrate, parsed.video_bitrate),
    audio_bitrate = COALESCE(ma.audio_bitrate, parsed.audio_bitrate),
    overall_bitrate = COALESCE(ma.overall_bitrate, parsed.overall_bitrate)
FROM parsed
WHERE ma.id = parsed.id;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_modified
ON media_asset (file_mtime DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_resolution
ON media_asset (((width::BIGINT * height::BIGINT)) DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_duration
ON media_asset (duration_ms DESC NULLS LAST, id DESC)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_container
ON media_asset (lower(container), id)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_video_codec
ON media_asset (lower(video_codec), id)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_audio_codec
ON media_asset (lower(audio_codec), id)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_fps
ON media_asset (fps DESC NULLS LAST, id DESC)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_bitrate
ON media_asset (overall_bitrate DESC NULLS LAST, id DESC)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_media_asset_live_sidecars
ON media_asset (has_subtitle, has_danmaku, id)
WHERE deleted_at IS NULL AND hidden = false;
CREATE INDEX IF NOT EXISTS idx_file_instance_rel_path_lower
ON file_instance (lower(rel_path), asset_id)
WHERE missing = false;
CREATE INDEX IF NOT EXISTS idx_asset_ai_result_description_sort
ON asset_ai_result (lower(description), asset_id)
WHERE status = 'ready';
CREATE INDEX IF NOT EXISTS idx_asset_ai_tag_top
ON asset_ai_tag (asset_id, confidence DESC, lower(tag));

CREATE OR REPLACE VIEW assets AS
SELECT
  ma.id, fi.rel_path, COALESCE(f.rel_path, '') AS parent_rel_path,
  ma.basename AS filename, ma.ext,
  CASE ma.media_type WHEN 1 THEN 'image' WHEN 2 THEN 'video' ELSE 'unknown' END AS media_type,
  ma.mime_type, ma.size_bytes AS size, EXTRACT(EPOCH FROM ma.file_mtime)::BIGINT AS mtime,
  ma.width, ma.height,
  CASE WHEN ma.duration_ms IS NULL THEN NULL ELSE ma.duration_ms::DOUBLE PRECISION / 1000 END AS duration,
  EXTRACT(EPOCH FROM ma.captured_at)::BIGINT AS taken_at,
  EXTRACT(EPOCH FROM ma.imported_at)::BIGINT AS imported_at,
  EXTRACT(EPOCH FROM ma.sort_time)::BIGINT AS timeline_at,
  ma.cache_key, CASE WHEN ma.browser_playable THEN 1 ELSE 0 END AS browser_playable,
  CASE ma.status WHEN 0 THEN 'ok' WHEN 1 THEN 'processing' WHEN 2 THEN 'error' ELSE 'ok' END AS scan_status,
  ma.thumb_status, ma.preview_status, ma.video_poster_status, ma.video_proxy_status,
  ma.metadata_json::TEXT AS metadata_json, ma.nfo_json::TEXT AS nfo_json, ma.nfo_search_text,
  COALESCE(ma.error_text, ma.error_code) AS error,
  EXTRACT(EPOCH FROM ma.deleted_at)::BIGINT AS deleted_at,
  EXTRACT(EPOCH FROM ma.created_at)::BIGINT AS created_at,
  EXTRACT(EPOCH FROM ma.updated_at)::BIGINT AS updated_at,
  ma.nfo_size, EXTRACT(EPOCH FROM ma.nfo_mtime)::BIGINT AS nfo_mtime,
  ma.hidden, encode(ma.sha256, 'hex') AS sha256, ma.has_subtitle, ma.has_danmaku,
  COALESCE(NULLIF(ma.filename_sort_key, ''), lower(ma.basename)) AS filename_sort_key,
  ma.rotation, ma.rating, ma.orientation,
  ma.deleted_at IS NULL AS is_live,
  ma.sort_time AS sort_time_value,
  ma.imported_at AS imported_at_value,
  ma.file_mtime AS modified_at_value,
  ma.media_type AS media_type_value,
  ma.width::BIGINT * ma.height::BIGINT AS resolution_value,
  ma.duration_ms AS duration_value,
  ma.fps AS fps_value,
  ma.container AS container_value,
  ma.video_codec AS video_codec_value,
  ma.audio_codec AS audio_codec_value,
  ma.overall_bitrate AS bitrate_value,
  (SELECT lower(r.description) FROM asset_ai_result r
    WHERE r.asset_id=ma.id AND r.status='ready' AND r.input_cache_key=ma.cache_key) AS ai_description_value,
  (SELECT lower(t.tag) FROM asset_ai_tag t JOIN asset_ai_result r ON r.asset_id=t.asset_id
    WHERE t.asset_id=ma.id AND r.status='ready' AND r.input_cache_key=ma.cache_key
    ORDER BY t.confidence DESC,t.tag LIMIT 1) AS ai_tag_value
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id AND fi.missing = false
LEFT JOIN folder f ON f.id = ma.folder_id;

CREATE OR REPLACE VIEW asset_records AS
SELECT
  ma.id, fi.rel_path, COALESCE(f.rel_path, '') AS parent_rel_path,
  ma.basename AS filename, ma.ext,
  CASE ma.media_type WHEN 1 THEN 'image' WHEN 2 THEN 'video' ELSE 'unknown' END AS media_type,
  ma.mime_type, ma.size_bytes AS size, EXTRACT(EPOCH FROM ma.file_mtime)::BIGINT AS mtime,
  ma.width, ma.height,
  CASE WHEN ma.duration_ms IS NULL THEN NULL ELSE ma.duration_ms::DOUBLE PRECISION / 1000 END AS duration,
  EXTRACT(EPOCH FROM ma.captured_at)::BIGINT AS taken_at,
  EXTRACT(EPOCH FROM ma.imported_at)::BIGINT AS imported_at,
  EXTRACT(EPOCH FROM ma.sort_time)::BIGINT AS timeline_at,
  ma.cache_key, CASE WHEN ma.browser_playable THEN 1 ELSE 0 END AS browser_playable,
  CASE ma.status WHEN 0 THEN 'ok' WHEN 1 THEN 'processing' WHEN 2 THEN 'error' ELSE 'ok' END AS scan_status,
  ma.thumb_status, ma.preview_status, ma.video_poster_status, ma.video_proxy_status,
  ma.metadata_json::TEXT AS metadata_json, ma.nfo_json::TEXT AS nfo_json, ma.nfo_search_text,
  COALESCE(ma.error_text, ma.error_code) AS error,
  EXTRACT(EPOCH FROM ma.deleted_at)::BIGINT AS deleted_at,
  EXTRACT(EPOCH FROM ma.created_at)::BIGINT AS created_at,
  EXTRACT(EPOCH FROM ma.updated_at)::BIGINT AS updated_at,
  ma.nfo_size, EXTRACT(EPOCH FROM ma.nfo_mtime)::BIGINT AS nfo_mtime,
  ma.hidden, encode(ma.sha256, 'hex') AS sha256, ma.has_subtitle, ma.has_danmaku,
  fi.missing,
  COALESCE(NULLIF(ma.filename_sort_key, ''), lower(ma.basename)) AS filename_sort_key,
  ma.rotation, ma.rating, ma.orientation,
  ma.deleted_at IS NULL AS is_live,
  ma.sort_time AS sort_time_value,
  ma.imported_at AS imported_at_value,
  ma.file_mtime AS modified_at_value,
  ma.media_type AS media_type_value,
  ma.width::BIGINT * ma.height::BIGINT AS resolution_value,
  ma.duration_ms AS duration_value,
  ma.fps AS fps_value,
  ma.container AS container_value,
  ma.video_codec AS video_codec_value,
  ma.audio_codec AS audio_codec_value,
  ma.overall_bitrate AS bitrate_value,
  (SELECT lower(r.description) FROM asset_ai_result r
    WHERE r.asset_id=ma.id AND r.status='ready' AND r.input_cache_key=ma.cache_key) AS ai_description_value,
  (SELECT lower(t.tag) FROM asset_ai_tag t JOIN asset_ai_result r ON r.asset_id=t.asset_id
    WHERE t.asset_id=ma.id AND r.status='ready' AND r.input_cache_key=ma.cache_key
    ORDER BY t.confidence DESC,t.tag LIMIT 1) AS ai_tag_value
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id
LEFT JOIN folder f ON f.id = ma.folder_id;
