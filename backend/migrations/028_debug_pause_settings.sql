INSERT INTO app_settings (key, value, updated_at)
VALUES
  ('debug_external_file_access_paused', 'false', now()),
  ('debug_background_processing_paused', 'false', now())
ON CONFLICT (key) DO NOTHING;
