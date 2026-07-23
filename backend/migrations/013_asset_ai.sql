CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS asset_ai_result (
  asset_id BIGINT PRIMARY KEY REFERENCES media_asset(id) ON DELETE CASCADE,
  input_cache_key TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'ready', 'failed')),
  description TEXT,
  tag_model TEXT NOT NULL DEFAULT '',
  tag_model_version TEXT NOT NULL DEFAULT '',
  description_model TEXT NOT NULL DEFAULT '',
  description_model_version TEXT NOT NULL DEFAULT '',
  taxonomy_version TEXT NOT NULL DEFAULT '',
  sampled_frames JSONB NOT NULL DEFAULT '[]'::jsonb,
  attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  error_text TEXT,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS asset_ai_tag (
  asset_id BIGINT NOT NULL REFERENCES media_asset(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  confidence DOUBLE PRECISION NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (asset_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_asset_ai_result_status_updated
  ON asset_ai_result(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_asset_ai_result_description_trgm
  ON asset_ai_result USING gin (lower(description) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_asset_ai_tag_tag
  ON asset_ai_tag(tag);
CREATE INDEX IF NOT EXISTS idx_asset_ai_tag_asset
  ON asset_ai_tag(asset_id);
