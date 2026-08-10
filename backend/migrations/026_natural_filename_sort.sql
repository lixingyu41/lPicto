CREATE COLLATION IF NOT EXISTS lpicto_natural_numeric (
  provider = icu,
  locale = 'und-u-kn-true',
  deterministic = false
);

DROP INDEX IF EXISTS idx_media_asset_filename_sort_key;
DROP INDEX IF EXISTS idx_media_asset_live_filename;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_filename_natural
ON media_asset (
  (COALESCE(NULLIF(filename_sort_key, ''), lower(basename)) COLLATE "lpicto_natural_numeric"),
  lower(basename),
  id
)
WHERE deleted_at IS NULL AND hidden = false;
