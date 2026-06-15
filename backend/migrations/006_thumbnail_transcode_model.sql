UPDATE assets
SET thumb_status = 'pending', updated_at = strftime('%s','now')
WHERE deleted_at IS NULL
  AND thumb_status <> 'pending';

UPDATE assets
SET video_poster_status = 'not_required', updated_at = strftime('%s','now')
WHERE video_poster_status <> 'not_required';
