-- Scheduler state kept beside next_run_at; see the SQLite migration.
ALTER TABLE _adm_job_index ADD COLUMN spec text COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE _adm_job_index ADD COLUMN retry_at text COLLATE "C";
ALTER TABLE _adm_job_index ADD COLUMN retry_attempt bigint NOT NULL DEFAULT 0;
ALTER TABLE _adm_job_index ADD COLUMN queued_run_id text COLLATE "C";
CREATE INDEX _adm_job_index_retry ON _adm_job_index(retry_at) WHERE retry_at IS NOT NULL;
