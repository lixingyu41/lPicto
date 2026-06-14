CREATE TABLE IF NOT EXISTS assets (
  id INTEGER PRIMARY KEY,
  rel_path TEXT UNIQUE NOT NULL,
  parent_rel_path TEXT NOT NULL,
  filename TEXT NOT NULL,
  ext TEXT NOT NULL,
  media_type TEXT NOT NULL,
  mime_type TEXT,
  size INTEGER NOT NULL,
  mtime INTEGER NOT NULL,
  width INTEGER,
  height INTEGER,
  duration REAL,
  taken_at INTEGER,
  imported_at INTEGER NOT NULL,
  timeline_at INTEGER NOT NULL,
  cache_key TEXT NOT NULL,
  browser_playable INTEGER NOT NULL DEFAULT 0,
  scan_status TEXT NOT NULL,
  thumb_status TEXT NOT NULL,
  preview_status TEXT NOT NULL,
  video_poster_status TEXT NOT NULL DEFAULT 'not_required',
  video_proxy_status TEXT NOT NULL DEFAULT 'not_required',
  metadata_json TEXT,
  error TEXT,
  deleted_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS folders (
  id INTEGER PRIMARY KEY,
  rel_path TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  parent_rel_path TEXT,
  depth INTEGER NOT NULL DEFAULT 0,
  asset_count INTEGER NOT NULL DEFAULT 0,
  recursive_asset_count INTEGER NOT NULL DEFAULT 0,
  cover_asset_id INTEGER,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY (cover_asset_id) REFERENCES assets(id)
);

CREATE TABLE IF NOT EXISTS scan_runs (
  id INTEGER PRIMARY KEY,
  status TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  finished_at INTEGER,
  total_seen INTEGER NOT NULL DEFAULT 0,
  assets_added INTEGER NOT NULL DEFAULT 0,
  assets_updated INTEGER NOT NULL DEFAULT 0,
  assets_deleted INTEGER NOT NULL DEFAULT 0,
  errors INTEGER NOT NULL DEFAULT 0,
  last_error TEXT
);

CREATE TABLE IF NOT EXISTS app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_assets_rel_path ON assets(rel_path);
CREATE INDEX IF NOT EXISTS idx_assets_parent_rel_path ON assets(parent_rel_path);
CREATE INDEX IF NOT EXISTS idx_assets_media_type ON assets(media_type);
CREATE INDEX IF NOT EXISTS idx_assets_timeline ON assets(timeline_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_assets_imported ON assets(imported_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_assets_filename ON assets(filename);
CREATE INDEX IF NOT EXISTS idx_assets_size ON assets(size);
CREATE INDEX IF NOT EXISTS idx_assets_deleted_at ON assets(deleted_at);
CREATE INDEX IF NOT EXISTS idx_folders_parent_rel_path ON folders(parent_rel_path);
CREATE INDEX IF NOT EXISTS idx_folders_rel_path ON folders(rel_path);

INSERT OR IGNORE INTO folders (rel_path, name, parent_rel_path, depth, updated_at)
VALUES ('', '照片', NULL, 0, strftime('%s','now'));
