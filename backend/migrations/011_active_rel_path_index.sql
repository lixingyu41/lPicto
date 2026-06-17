CREATE INDEX IF NOT EXISTS idx_assets_active_rel_path
ON assets(rel_path)
WHERE deleted_at IS NULL;
