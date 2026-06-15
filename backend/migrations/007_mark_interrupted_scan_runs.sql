UPDATE scan_runs
SET status = 'interrupted',
    finished_at = strftime('%s','now'),
    last_error = '扫描已中断'
WHERE status = 'running';
