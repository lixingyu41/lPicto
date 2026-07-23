UPDATE media_job
SET started_at = finished_at
WHERE job_type IN ('thumb', 'preview', 'video_poster')
  AND finished_at IS NOT NULL
  AND (started_at IS NULL OR finished_at - started_at > INTERVAL '1 hour');
