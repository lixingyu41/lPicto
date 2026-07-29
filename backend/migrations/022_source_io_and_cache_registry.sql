CREATE TABLE IF NOT EXISTS cache_entry (
  cache_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  asset_id BIGINT REFERENCES media_asset(id) ON DELETE CASCADE,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  last_accessed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  pinned_until TIMESTAMPTZ,
  state TEXT NOT NULL DEFAULT 'ready',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, cache_key, relative_path)
);

CREATE INDEX IF NOT EXISTS idx_cache_entry_evict
ON cache_entry (kind, last_accessed_at, size_bytes)
WHERE state = 'ready';

CREATE TABLE IF NOT EXISTS source_io_batch (
  id BIGSERIAL PRIMARY KEY,
  reason TEXT NOT NULL,
  priority INTEGER NOT NULL,
  state TEXT NOT NULL,
  item_count INTEGER NOT NULL DEFAULT 0,
  bytes_read BIGINT NOT NULL DEFAULT 0,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at TIMESTAMPTZ,
  error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_source_io_batch_started
ON source_io_batch (started_at DESC);

CREATE TABLE IF NOT EXISTS asset_ai_stage (
  asset_id BIGINT PRIMARY KEY REFERENCES media_asset(id) ON DELETE CASCADE,
  cache_key TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  stage_path TEXT,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  prepared_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  error_message TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_asset_ai_stage_state
ON asset_ai_stage (state, updated_at, asset_id);
