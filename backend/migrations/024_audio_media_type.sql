DO $$
DECLARE
  view_name TEXT;
  definition TEXT;
  updated_definition TEXT;
BEGIN
  FOREACH view_name IN ARRAY ARRAY['assets', 'asset_records']
  LOOP
    SELECT pg_get_viewdef(view_name::regclass, true) INTO definition;
    updated_definition := replace(
      definition,
      'WHEN 2 THEN ''video''::text',
      'WHEN 2 THEN ''video''::text WHEN 3 THEN ''audio''::text'
    );
    IF updated_definition = definition THEN
      RAISE EXCEPTION 'cannot add audio media type to view %', view_name;
    END IF;
    EXECUTE format('CREATE OR REPLACE VIEW %I AS %s', view_name, updated_definition);
  END LOOP;
END
$$;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_audio_timeline
ON media_asset (sort_time DESC, id DESC)
WHERE deleted_at IS NULL AND hidden = false AND media_type = 3;

CREATE INDEX IF NOT EXISTS idx_media_asset_live_audio_duration
ON media_asset (duration_ms DESC NULLS LAST, id DESC)
WHERE deleted_at IS NULL AND hidden = false AND media_type = 3;
