ALTER TABLE album_sources ADD COLUMN media_type_filter TEXT NOT NULL DEFAULT 'all';
ALTER TABLE album_sources ADD COLUMN orientation_filter TEXT NOT NULL DEFAULT 'all';

