ALTER TABLE media_asset
ADD COLUMN IF NOT EXISTS last_played_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_last_played
ON media_asset (last_played_at DESC, id DESC)
WHERE deleted_at IS NULL
  AND hidden = false
  AND media_type = 2
  AND last_played_at IS NOT NULL;

DO $$
DECLARE
  view_name TEXT;
  definition TEXT;
  updated_definition TEXT;
BEGIN
  FOREACH view_name IN ARRAY ARRAY['assets', 'asset_records']
  LOOP
    SELECT pg_get_viewdef(view_name::regclass, true) INTO definition;
    updated_definition := regexp_replace(
      definition,
      E'\n[[:space:]]*FROM media_asset ma',
      E',\n    ma.last_played_at AS last_played_at_value,\n    EXTRACT(EPOCH FROM ma.last_played_at)::BIGINT AS last_played_at\n   FROM media_asset ma'
    );
    IF updated_definition = definition THEN
      RAISE EXCEPTION 'cannot add recent playback fields to view %', view_name;
    END IF;
    EXECUTE format('CREATE OR REPLACE VIEW %I AS %s', view_name, updated_definition);
  END LOOP;
END
$$;
