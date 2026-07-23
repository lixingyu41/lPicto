INSERT INTO media_job (asset_id, job_type, status, error_text, started_at, finished_at)
SELECT
  ma.id,
  'metadata',
  CASE WHEN ma.error_text IS NULL THEN 'ready' ELSE 'error' END,
  ma.error_text,
  ma.created_at,
  ma.updated_at
FROM media_asset ma
WHERE ma.deleted_at IS NULL
ON CONFLICT(asset_id, job_type) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_media_job_type_status
  ON media_job(job_type, status, asset_id);

UPDATE media_asset
SET video_poster_status = CASE
  WHEN thumb_status = 'ready' THEN 'ready'
  WHEN thumb_status = 'error' THEN 'error'
  WHEN thumb_status = 'processing' THEN 'processing'
  ELSE 'pending'
END
WHERE media_type = 2 AND video_poster_status = 'not_required';

INSERT INTO media_job (asset_id, job_type, status, error_text, started_at, finished_at)
SELECT id, 'video_poster', video_poster_status, error_text,
  CASE WHEN video_poster_status IN ('ready','error') THEN updated_at ELSE NULL END,
  CASE WHEN video_poster_status IN ('ready','error') THEN updated_at ELSE NULL END
FROM media_asset
WHERE deleted_at IS NULL AND media_type = 2
ON CONFLICT(asset_id, job_type) DO NOTHING;
