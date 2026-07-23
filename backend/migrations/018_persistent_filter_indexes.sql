ALTER TABLE media_asset
ADD COLUMN IF NOT EXISTS rating SMALLINT NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS rotation SMALLINT NOT NULL DEFAULT 0;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'media_asset_rating_range' AND conrelid = 'media_asset'::regclass
  ) THEN
    ALTER TABLE media_asset ADD CONSTRAINT media_asset_rating_range CHECK (rating BETWEEN 0 AND 5);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'media_asset_rotation_values' AND conrelid = 'media_asset'::regclass
  ) THEN
    ALTER TABLE media_asset ADD CONSTRAINT media_asset_rotation_values CHECK (rotation IN (0, 90, 180, 270));
  END IF;
END $$;

CREATE OR REPLACE FUNCTION lpicto_asset_orientation(asset_width INT, asset_height INT, asset_rotation INT)
RETURNS SMALLINT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $$
  SELECT CASE
    WHEN asset_width IS NULL OR asset_height IS NULL OR asset_width <= 0 OR asset_height <= 0 THEN 0::SMALLINT
    WHEN asset_rotation IN (90, 270) AND asset_height > asset_width THEN 1::SMALLINT
    WHEN asset_rotation IN (90, 270) AND asset_width > asset_height THEN 2::SMALLINT
    WHEN asset_width > asset_height THEN 1::SMALLINT
    WHEN asset_height > asset_width THEN 2::SMALLINT
    ELSE 3::SMALLINT
  END
$$;

UPDATE media_asset ma
SET rating = COALESCE(ap.rating, 0),
    rotation = COALESCE(ap.rotation, 0),
    orientation = lpicto_asset_orientation(ma.width, ma.height, COALESCE(ap.rotation, 0))
FROM (SELECT id FROM media_asset) existing
LEFT JOIN asset_preferences ap ON ap.asset_id = existing.id
WHERE ma.id = existing.id;

CREATE OR REPLACE FUNCTION lpicto_sync_asset_filter_fields()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.rating := COALESCE(NEW.rating, 0);
  NEW.rotation := COALESCE(NEW.rotation, 0);
  NEW.orientation := lpicto_asset_orientation(NEW.width, NEW.height, NEW.rotation);
  RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_media_asset_filter_fields ON media_asset;
CREATE TRIGGER trg_media_asset_filter_fields
BEFORE INSERT OR UPDATE OF width, height, rotation, rating ON media_asset
FOR EACH ROW EXECUTE FUNCTION lpicto_sync_asset_filter_fields();

CREATE OR REPLACE FUNCTION lpicto_sync_asset_preferences()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    UPDATE media_asset
    SET rating = 0, rotation = 0
    WHERE id = OLD.asset_id;
    RETURN OLD;
  END IF;
  UPDATE media_asset
  SET rating = COALESCE(NEW.rating, 0),
      rotation = COALESCE(NEW.rotation, 0)
  WHERE id = NEW.asset_id;
  RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_asset_preferences_filter_fields ON asset_preferences;
CREATE TRIGGER trg_asset_preferences_filter_fields
AFTER INSERT OR UPDATE OR DELETE ON asset_preferences
FOR EACH ROW EXECUTE FUNCTION lpicto_sync_asset_preferences();

CREATE INDEX IF NOT EXISTS idx_media_asset_live_timeline
ON media_asset (sort_time DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_imported
ON media_asset (imported_at DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_size
ON media_asset (size_bytes DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_filename
ON media_asset (filename_sort_key, id)
WHERE deleted_at IS NULL AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_type_timeline
ON media_asset (media_type, sort_time DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_rating_timeline
ON media_asset (rating, sort_time DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_orientation_timeline
ON media_asset (orientation, sort_time DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false;

CREATE INDEX IF NOT EXISTS idx_media_asset_filename_lower_trgm
ON media_asset USING GIN (lower(basename) gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_media_asset_nfo_lower_trgm
ON media_asset USING GIN (lower(nfo_search_text) gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_folder_rel_path_prefix
ON folder (rel_path text_pattern_ops);

CREATE TABLE IF NOT EXISTS asset_nfo_value (
  asset_id BIGINT NOT NULL REFERENCES media_asset(id) ON DELETE CASCADE,
  field TEXT NOT NULL CHECK (field IN ('actor', 'id', 'tag', 'title', 'year')),
  value TEXT NOT NULL,
  normalized_value TEXT NOT NULL,
  PRIMARY KEY (asset_id, field, normalized_value)
);

CREATE INDEX IF NOT EXISTS idx_asset_nfo_value_field_asset
ON asset_nfo_value (field, asset_id);

CREATE INDEX IF NOT EXISTS idx_asset_nfo_value_normalized_trgm
ON asset_nfo_value USING GIN (normalized_value gin_trgm_ops);

CREATE OR REPLACE FUNCTION lpicto_refresh_asset_nfo_values(target_asset_id BIGINT, target_nfo JSONB)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
  DELETE FROM asset_nfo_value WHERE asset_id = target_asset_id;
  IF target_nfo IS NULL THEN
    RETURN;
  END IF;

  INSERT INTO asset_nfo_value (asset_id, field, value, normalized_value)
  SELECT target_asset_id, classified.field, MIN(classified.value), classified.normalized_value
  FROM (
    SELECT fields.field,
           trim(COALESCE(item.item_value->>'value', '')) AS value,
           lower(trim(COALESCE(item.item_value->>'value', ''))) AS normalized_value
    FROM jsonb_array_elements(COALESCE(target_nfo->'groups', '[]'::jsonb)) AS grp(group_value)
    CROSS JOIN LATERAL jsonb_array_elements(COALESCE(grp.group_value->'items', '[]'::jsonb)) AS item(item_value)
    CROSS JOIN LATERAL (
      SELECT unnest(array_remove(ARRAY[
        CASE WHEN lower(COALESCE(grp.group_value->>'title', '')) = '演员'
               OR lower(COALESCE(item.item_value->>'key', '')) = 'actor' THEN 'actor' END,
        CASE WHEN lower(COALESCE(grp.group_value->>'title', '')) = 'id'
               OR lower(COALESCE(item.item_value->>'key', '')) = 'uniqueid'
               OR lower(COALESCE(item.item_value->>'key', '')) LIKE 'uniqueid:%' THEN 'id' END,
        CASE WHEN lower(COALESCE(grp.group_value->>'title', '')) IN ('标记', '类型')
               OR lower(COALESCE(item.item_value->>'key', '')) IN ('tag', 'genre') THEN 'tag' END,
        CASE WHEN lower(COALESCE(item.item_value->>'key', '')) IN ('title', 'originaltitle', 'sorttitle') THEN 'title' END,
        CASE WHEN lower(COALESCE(item.item_value->>'key', '')) = 'year' THEN 'year' END
      ], NULL)) AS field
    ) fields
    WHERE trim(COALESCE(item.item_value->>'value', '')) <> ''
  ) classified
  GROUP BY classified.field, classified.normalized_value
  ON CONFLICT (asset_id, field, normalized_value)
  DO UPDATE SET value = EXCLUDED.value;
END
$$;

CREATE OR REPLACE FUNCTION lpicto_media_asset_nfo_trigger()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  PERFORM lpicto_refresh_asset_nfo_values(NEW.id, NEW.nfo_json);
  RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_media_asset_nfo_values ON media_asset;
CREATE TRIGGER trg_media_asset_nfo_values
AFTER INSERT OR UPDATE OF nfo_json ON media_asset
FOR EACH ROW EXECUTE FUNCTION lpicto_media_asset_nfo_trigger();

SELECT lpicto_refresh_asset_nfo_values(id, nfo_json)
FROM media_asset
WHERE nfo_json IS NOT NULL;

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
  ma.imported_at AS imported_at_value
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
  ma.imported_at AS imported_at_value
FROM media_asset ma
JOIN file_instance fi ON fi.asset_id = ma.id
LEFT JOIN folder f ON f.id = ma.folder_id;

ALTER ROLE CURRENT_USER SET pg_trgm.similarity_threshold = 0.18;
ALTER ROLE CURRENT_USER SET pg_trgm.word_similarity_threshold = 0.18;
SELECT set_config('pg_trgm.similarity_threshold', '0.18', false);
SELECT set_config('pg_trgm.word_similarity_threshold', '0.18', false);
