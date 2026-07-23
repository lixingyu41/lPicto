ALTER TABLE scan_runs
  ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT 'metadata';

CREATE INDEX IF NOT EXISTS idx_scan_runs_task_started
  ON scan_runs(task_type, started_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS system_task_state (
  task_key TEXT PRIMARY KEY,
  status TEXT NOT NULL DEFAULT 'never',
  last_started_at BIGINT,
  last_finished_at BIGINT,
  last_success_at BIGINT,
  message TEXT,
  updated_at BIGINT NOT NULL
);
