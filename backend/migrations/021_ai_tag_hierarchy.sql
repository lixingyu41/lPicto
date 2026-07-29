ALTER TABLE asset_ai_tag
  ADD COLUMN IF NOT EXISTS category_key TEXT NOT NULL DEFAULT 'other',
  ADD COLUMN IF NOT EXISTS category_label TEXT NOT NULL DEFAULT '其他',
  ADD COLUMN IF NOT EXISTS subject_key TEXT NOT NULL DEFAULT 'object',
  ADD COLUMN IF NOT EXISTS subject_label TEXT NOT NULL DEFAULT '物体';

CREATE TABLE IF NOT EXISTS asset_ai_tag_facet (
  asset_id BIGINT NOT NULL,
  tag TEXT NOT NULL,
  facet_key TEXT NOT NULL,
  node_id TEXT NOT NULL,
  node_ids TEXT[] NOT NULL,
  labels TEXT[] NOT NULL,
  PRIMARY KEY(asset_id, tag, node_id),
  FOREIGN KEY(asset_id, tag) REFERENCES asset_ai_tag(asset_id, tag) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_asset_ai_tag_facet_asset
  ON asset_ai_tag_facet(asset_id);
CREATE INDEX IF NOT EXISTS idx_asset_ai_tag_facet_nodes
  ON asset_ai_tag_facet USING gin(node_ids);
CREATE INDEX IF NOT EXISTS idx_asset_ai_tag_facet_group
  ON asset_ai_tag_facet(facet_key, asset_id);

DELETE FROM asset_ai_tag WHERE tag LIKE '%无法判断%';
