UPDATE assets
SET preview_status = 'pending', updated_at = strftime('%s','now')
WHERE deleted_at IS NULL
  AND media_type = 'image'
  AND preview_status = 'ready';

UPDATE assets
SET video_proxy_status = 'pending', updated_at = strftime('%s','now')
WHERE deleted_at IS NULL
  AND media_type = 'video'
  AND video_proxy_status = 'ready';
