CREATE INDEX IF NOT EXISTS idx_media_asset_image_visible_size
ON media_asset (size_bytes DESC, id DESC)
WHERE media_type = 1 AND deleted_at IS NULL AND thumb_status = 'ready';

CREATE INDEX IF NOT EXISTS idx_media_asset_video_visible_size
ON media_asset (size_bytes DESC, id DESC)
WHERE media_type = 2 AND deleted_at IS NULL AND thumb_status = 'ready';

CREATE INDEX IF NOT EXISTS idx_file_instance_asset_present
ON file_instance (asset_id)
WHERE missing = false;
