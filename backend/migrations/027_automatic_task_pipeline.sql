UPDATE media_asset
SET thumb_status = CASE WHEN thumb_status = 'error' THEN 'pending' ELSE thumb_status END,
    preview_status = CASE WHEN preview_status = 'error' THEN 'pending' ELSE preview_status END,
    video_poster_status = CASE WHEN video_poster_status = 'error' THEN 'pending' ELSE video_poster_status END,
    error_text = NULL
WHERE deleted_at IS NULL
  AND (thumb_status = 'error' OR preview_status = 'error' OR video_poster_status = 'error');

UPDATE media_job
SET status = 'pending', attempts = 0, error_text = NULL, started_at = NULL, finished_at = NULL
WHERE status = 'error' AND job_type IN ('metadata', 'thumb', 'preview', 'video_poster', 'storyboard');

UPDATE asset_ai_result
SET status = 'pending', attempts = 0, error_text = NULL, started_at = NULL, finished_at = NULL, updated_at = now()
WHERE status = 'failed';

INSERT INTO app_settings(key, value)
VALUES ('ai_auto_analyze', 'true'), ('ai_manual_run', 'false')
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
