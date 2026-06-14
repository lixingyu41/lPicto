CREATE TABLE IF NOT EXISTS asset_preferences (
  asset_id INTEGER PRIMARY KEY,
  rotation INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS albums (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  media_type_filter TEXT NOT NULL DEFAULT 'all',
  orientation_filter TEXT NOT NULL DEFAULT 'all',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS album_sources (
  id INTEGER PRIMARY KEY,
  album_id INTEGER NOT NULL,
  source_type TEXT NOT NULL,
  rel_path TEXT NOT NULL,
  recursive INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_album_sources_album_id ON album_sources(album_id);
CREATE INDEX IF NOT EXISTS idx_album_sources_rel_path ON album_sources(rel_path);
