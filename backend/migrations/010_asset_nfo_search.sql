ALTER TABLE assets ADD COLUMN nfo_json TEXT;
ALTER TABLE assets ADD COLUMN nfo_search_text TEXT;

CREATE INDEX IF NOT EXISTS idx_assets_active_dimensions ON assets(width, height) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_assets_active_duration ON assets(duration) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_assets_active_nfo_search ON assets(nfo_search_text) WHERE deleted_at IS NULL;
