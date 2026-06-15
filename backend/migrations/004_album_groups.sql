CREATE TABLE IF NOT EXISTS album_groups (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

ALTER TABLE albums ADD COLUMN group_id INTEGER;

CREATE INDEX IF NOT EXISTS idx_albums_group_id ON albums(group_id);
