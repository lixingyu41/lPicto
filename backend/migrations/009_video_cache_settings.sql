CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO app_settings (key, value, updated_at)
VALUES
  ('video_proxy_cache_ttl_seconds', '259200', now()),
  ('video_proxy_cache_max_bytes', '0', now())
ON CONFLICT (key) DO NOTHING;
