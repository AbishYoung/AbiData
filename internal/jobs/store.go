package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// SQLStore implements Store over the admin DB (index) and workspace DBs (jobs, runs).
type SQLStore struct {
	Admin      *admin.Store
	Workspaces *workspace.Manager
}

var _ Store = (*SQLStore)(nil)

func fmtT(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return sqlitex.FormatTime(t)
}

func parseT(s sql.NullString) time.Time {
	if !s.Valid || s.String == "" {
		return time.Time{}
	}
	t, _ := sqlitex.ParseTime(s.String)
	return t
}

const indexCols = `workspace_id, job_id, spec, next_run_at, retry_at, retry_attempt, enabled, running_run_id, queued_run_id, last_status`

func scanEntries(rows *sqlitex.Rows) ([]IndexEntry, error) {
	defer rows.Close()
	var out []IndexEntry
	for rows.Next() {
		var e IndexEntry
		var ws string
		var next, retry, running, queued, last sql.NullString
		var enabled int
		if err := rows.Scan(&ws, &e.JobID, &e.Spec, &next, &retry, &e.RetryAttempt, &enabled, &running, &queued, &last); err != nil {
			return nil, err
		}
		e.WS, e.NextRunAt, e.RetryAt, e.Enabled = ids.WorkspaceID(ws), parseT(next), parseT(retry), enabled == 1
		e.RunningRunID, e.QueuedRunID, e.LastStatus = running.String, queued.String, last.String
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLStore) Due(ctx context.Context, now time.Time) ([]IndexEntry, error) {
	var out []IndexEntry
	err := s.Admin.Read(ctx, func(c *sqlitex.Conn) error {
		rows, err := c.Query(ctx, `SELECT `+indexCols+` FROM _adm_job_index
			WHERE enabled = 1 AND (next_run_at <= ?1 OR retry_at <= ?1) ORDER BY workspace_id, job_id`, sqlitex.FormatTime(now))
		if err != nil {
			return err
		}
		out, err = scanEntries(rows)
		return err
	})
	return out, err
}

func (s *SQLStore) NextWake(ctx context.Context) (time.Time, bool, error) {
	var v sql.NullString
	err := s.Admin.Read(ctx, func(c *sqlitex.Conn) error {
		return c.QueryRow(ctx, `SELECT min(t) FROM (
			SELECT min(next_run_at) AS t FROM _adm_job_index WHERE enabled = 1
			UNION ALL SELECT min(retry_at) FROM _adm_job_index WHERE enabled = 1)`).Scan(&v)
	})
	if err != nil {
		return time.Time{}, false, err
	}
	t := parseT(v)
	return t, !t.IsZero(), nil
}

func (s *SQLStore) Entries(ctx context.Context, ws ids.WorkspaceID) ([]IndexEntry, error) {
	var out []IndexEntry
	err := s.Admin.Read(ctx, func(c *sqlitex.Conn) error {
		rows, err := c.Query(ctx, `SELECT `+indexCols+` FROM _adm_job_index WHERE ?1 = '' OR workspace_id = ?1`, string(ws))
		if err != nil {
			return err
		}
		out, err = scanEntries(rows)
		return err
	})
	return out, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *SQLStore) PutEntry(ctx context.Context, e IndexEntry) error {
	return s.Admin.Write(ctx, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO _adm_job_index (`+indexCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(workspace_id, job_id) DO UPDATE SET spec = excluded.spec, next_run_at = excluded.next_run_at,
			retry_at = excluded.retry_at, retry_attempt = excluded.retry_attempt, enabled = excluded.enabled,
			running_run_id = excluded.running_run_id, queued_run_id = excluded.queued_run_id, last_status = excluded.last_status`,
			string(e.WS), e.JobID, e.Spec, fmtT(e.NextRunAt), fmtT(e.RetryAt), e.RetryAttempt, boolInt(e.Enabled),
			nullStr(e.RunningRunID), nullStr(e.QueuedRunID), nullStr(e.LastStatus))
		return err
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *SQLStore) DeleteEntry(ctx context.Context, ws ids.WorkspaceID, jobID string) error {
	return s.Admin.Write(ctx, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM _adm_job_index WHERE workspace_id = ? AND job_id = ?`, string(ws), jobID)
		return err
	})
}

// DeleteWorkspace drops a workspace's index rows (workspace deletion).
func (s *SQLStore) DeleteWorkspace(ctx context.Context, ws ids.WorkspaceID) error {
	return s.Admin.Write(ctx, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM _adm_job_index WHERE workspace_id = ?`, string(ws))
		return err
	})
}

func (s *SQLStore) withLease(ctx context.Context, ws ids.WorkspaceID, fn func(l *workspace.Lease) error) error {
	l, err := s.Workspaces.Acquire(ctx, ws)
	if err != nil {
		return err
	}
	defer l.Release()
	return fn(l)
}

// JobCols is the _sys_jobs column list ScanJob expects.
const JobCols = `id, name, script_id, schedule, timezone, enabled, timeout_seconds, overlap_policy, misfire_policy,
	max_retries, retry_backoff_seconds, args, version, updated_at, updated_by`

// ScanJob scans one _sys_jobs row selected with JobCols.
func ScanJob(rows *sqlitex.Rows) (Job, error) {
	var j Job
	var schedule sql.NullString
	var enabled int
	var args, updated string
	if err := rows.Scan(&j.ID, &j.Name, &j.ScriptID, &schedule, &j.Timezone, &enabled, &j.TimeoutSeconds, &j.OverlapPolicy,
		&j.MisfirePolicy, &j.MaxRetries, &j.RetryBackoff, &args, &j.Version, &updated, &j.UpdatedBy); err != nil {
		return Job{}, err
	}
	j.Schedule, j.Enabled = schedule.String, enabled == 1
	j.UpdatedAt, _ = sqlitex.ParseTime(updated)
	if err := json.Unmarshal([]byte(args), &j.Args); err != nil {
		return Job{}, err
	}
	if j.Args == nil {
		j.Args = map[string]any{}
	}
	return j, nil
}

// ReadJobs lists a workspace's jobs (optionally one by id) on q.
func ReadJobs(ctx context.Context, q sqlitex.Querier, id string) ([]Job, error) {
	rows, err := q.Query(ctx, `SELECT `+JobCols+` FROM _sys_jobs WHERE ?1 = '' OR id = ?1 ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := ScanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *SQLStore) Jobs(ctx context.Context, ws ids.WorkspaceID) ([]Job, error) {
	var out []Job
	err := s.withLease(ctx, ws, func(l *workspace.Lease) error {
		return l.Read(ctx, func(q sqlitex.Querier) error {
			var err error
			out, err = ReadJobs(ctx, q, "")
			return err
		})
	})
	return out, err
}

func (s *SQLStore) Job(ctx context.Context, ws ids.WorkspaceID, id string) (Job, error) {
	if id == "" {
		return Job{}, fault.New(fault.NotFound, "Job not found.")
	}
	js, err := s.Jobs(ctx, ws)
	if err != nil {
		return Job{}, err
	}
	for _, j := range js {
		if j.ID == id {
			return j, nil
		}
	}
	return Job{}, fault.New(fault.NotFound, "Job not found.")
}

// RunCols is the _sys_job_runs column list ScanRun expects.
const RunCols = `id, job_id, trigger_kind, status, attempt, triggered_by, queued_at, started_at, finished_at,
	rows_read, rows_written, error_code, error_message, log_text`

// ScanRun scans one _sys_job_runs row selected with RunCols.
func ScanRun(rows *sqlitex.Rows) (Run, error) {
	var r Run
	var by, started, finished, code, msg sql.NullString
	var queued string
	if err := rows.Scan(&r.ID, &r.JobID, &r.TriggerKind, &r.Status, &r.Attempt, &by, &queued, &started, &finished,
		&r.RowsRead, &r.RowsWritten, &code, &msg, &r.Log); err != nil {
		return Run{}, err
	}
	r.TriggeredBy, r.ErrorCode, r.ErrorMessage = by.String, code.String, msg.String
	r.QueuedAt, _ = sqlitex.ParseTime(queued)
	if t := parseT(started); !t.IsZero() {
		r.StartedAt = &t
	}
	if t := parseT(finished); !t.IsZero() {
		r.FinishedAt = &t
	}
	return r, nil
}

func ptrT(t *time.Time) any {
	if t == nil {
		return nil
	}
	return sqlitex.FormatTime(*t)
}

func (s *SQLStore) PutRun(ctx context.Context, ws ids.WorkspaceID, r Run) error {
	return s.withLease(ctx, ws, func(l *workspace.Lease) error {
		return l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO _sys_job_runs (`+RunCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(id) DO UPDATE SET status = excluded.status, started_at = excluded.started_at,
				finished_at = excluded.finished_at, rows_read = excluded.rows_read, rows_written = excluded.rows_written,
				error_code = excluded.error_code, error_message = excluded.error_message, log_text = excluded.log_text`,
				r.ID, r.JobID, r.TriggerKind, r.Status, r.Attempt, nullStr(r.TriggeredBy), sqlitex.FormatTime(r.QueuedAt),
				ptrT(r.StartedAt), ptrT(r.FinishedAt), r.RowsRead, r.RowsWritten, nullStr(r.ErrorCode), nullStr(r.ErrorMessage), r.Log)
			return err
		})
	})
}

func (s *SQLStore) GetRun(ctx context.Context, ws ids.WorkspaceID, id string) (Run, error) {
	var out Run
	err := s.withLease(ctx, ws, func(l *workspace.Lease) error {
		return l.Read(ctx, func(q sqlitex.Querier) error {
			rows, err := q.Query(ctx, `SELECT `+RunCols+` FROM _sys_job_runs WHERE id = ?`, id)
			if err != nil {
				return err
			}
			defer rows.Close()
			if !rows.Next() {
				if err := rows.Err(); err != nil {
					return err
				}
				return fault.New(fault.NotFound, "Run not found.")
			}
			out, err = ScanRun(rows)
			return err
		})
	})
	return out, err
}
