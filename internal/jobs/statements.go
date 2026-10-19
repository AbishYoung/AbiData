package jobs

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlByName = sqlgen.RegisterSQLiteOnly("jobs.ByName", `SELECT id FROM _sys_jobs WHERE name = ? COLLATE NOCASE`).
			WithPostgres(`SELECT id FROM _sys_jobs WHERE name = ? COLLATE abi_ci`)
	sqlReadTZ = sqlgen.Register("jobs.readTZ", `SELECT value FROM _sys_meta WHERE key = 'timezone'`)
	sqlPut    = sqlgen.Register("jobs.Put", `INSERT INTO _sys_jobs (id, name, script_id, schedule, timezone, enabled, timeout_seconds,
				overlap_policy, misfire_policy, max_retries, retry_backoff_seconds, args, version, updated_at, updated_by)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)`)
	sqlPut2 = sqlgen.Register("jobs.Put#2", `UPDATE _sys_jobs SET name = ?, script_id = ?, schedule = ?, timezone = ?, enabled = ?,
			timeout_seconds = ?, overlap_policy = ?, misfire_policy = ?, max_retries = ?, retry_backoff_seconds = ?, args = ?,
			version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?`)
	sqlConflictOrMissing = sqlgen.Register("jobs.conflictOrMissing", `SELECT count(*) FROM _sys_jobs WHERE id = ?`)
	sqlDelete            = sqlgen.Register("jobs.Delete", `DELETE FROM _sys_jobs WHERE id = ? AND version = ?`)
	sqlDelete2           = sqlgen.Register("jobs.Delete#2", `DELETE FROM _sys_job_runs WHERE job_id = ?`)
	sqlSQLStoreNextWake  = sqlgen.Register("jobs.SQLStore.NextWake", `SELECT min(t) FROM (
			SELECT min(next_run_at) AS t FROM _adm_job_index WHERE enabled = 1
			UNION ALL SELECT min(retry_at) FROM _adm_job_index WHERE enabled = 1)`)
	sqlSQLStoreDeleteEntry     = sqlgen.Register("jobs.SQLStore.DeleteEntry", `DELETE FROM _adm_job_index WHERE workspace_id = ? AND job_id = ?`)
	sqlSQLStoreDeleteWorkspace = sqlgen.Register("jobs.SQLStore.DeleteWorkspace", `DELETE FROM _adm_job_index WHERE workspace_id = ?`)
)
