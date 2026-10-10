-- Scheduler state kept beside next_run_at (brief <jobs_and_scheduler>).
-- spec: schedule|timezone|enabled the next_run_at was computed for (a change recomputes it).
-- retry_at/retry_attempt: a pending retry of a failed run.
-- queued_run_id: the one run waiting behind running_run_id (overlap_policy=queue).
ALTER TABLE _adm_job_index ADD COLUMN spec TEXT NOT NULL DEFAULT '';
ALTER TABLE _adm_job_index ADD COLUMN retry_at TEXT;
ALTER TABLE _adm_job_index ADD COLUMN retry_attempt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE _adm_job_index ADD COLUMN queued_run_id TEXT;
CREATE INDEX _adm_job_index_retry ON _adm_job_index(retry_at) WHERE retry_at IS NOT NULL;
