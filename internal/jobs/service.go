package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

// JobInput creates or replaces a job.
type JobInput struct {
	Name           string         `json:"name" minLength:"1" maxLength:"200"`
	ScriptID       string         `json:"script_id"`
	Schedule       string         `json:"schedule,omitempty" maxLength:"200" doc:"Cron (5 fields) or a descriptor such as @daily; empty for manual-only jobs."`
	Timezone       string         `json:"timezone,omitempty" doc:"IANA time zone the schedule is evaluated in (default: the workspace's)."`
	Enabled        *bool          `json:"enabled,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty" minimum:"0" maximum:"3600"`
	OverlapPolicy  string         `json:"overlap_policy,omitempty" enum:"skip,queue,"`
	MisfirePolicy  string         `json:"misfire_policy,omitempty" enum:"run_once,skip,"`
	MaxRetries     int            `json:"max_retries,omitempty" minimum:"0" maximum:"10"`
	RetryBackoff   int            `json:"retry_backoff_seconds,omitempty" minimum:"0" maximum:"86400"`
	Args           map[string]any `json:"args,omitempty" doc:"Arguments passed as job.args. Set allow_enqueue = true to let forms enqueue this job."`
}

// maxArgsBytes bounds a job's (or an enqueue call's) serialized args.
const maxArgsBytes = 64 << 10

// Service manages job definitions and run history.
type Service struct {
	Scripts   *scripts.Service
	Scheduler *Scheduler
	Events    *events.Hub
}

// List returns a workspace's jobs.
func (s *Service) List(ctx context.Context, l *workspace.Lease) ([]Job, error) {
	var out []Job
	err := l.Read(ctx, func(q dbx.Querier) error {
		var err error
		out, err = ReadJobs(ctx, q, "")
		return err
	})
	if out == nil {
		out = []Job{}
	}
	return out, err
}

// Get returns one job.
func (s *Service) Get(ctx context.Context, l *workspace.Lease, id string) (Job, error) {
	var out []Job
	err := l.Read(ctx, func(q dbx.Querier) error {
		var err error
		out, err = ReadJobs(ctx, q, id)
		return err
	})
	if err != nil {
		return Job{}, err
	}
	if len(out) == 0 || id == "" {
		return Job{}, fault.New(fault.NotFound, "Job not found.")
	}
	return out[0], nil
}

// ByName returns a job by its (case-insensitive) name.
func (s *Service) ByName(ctx context.Context, l *workspace.Lease, name string) (Job, error) {
	var id string
	err := l.Read(ctx, func(q dbx.Querier) error {
		return q.QueryRow(ctx, sqlByName.For(pgdb.DialectOf(q)), name).Scan(&id)
	})
	if dbx.IsNoRows(err) {
		return Job{}, fault.Newf(fault.NotFound, "There is no job named %q.", name)
	}
	if err != nil {
		return Job{}, err
	}
	return s.Get(ctx, l, id)
}

func (s *Service) normalize(ctx context.Context, l *workspace.Lease, in JobInput, wsTZ string) (JobInput, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return in, "", fault.New(fault.BadRequest, "The job needs a name (at most 200 characters).")
	}
	sc, err := s.Scripts.Get(ctx, l, in.ScriptID)
	if err != nil {
		if fault.Classify(err).Code == fault.NotFound {
			return in, "", fault.New(fault.BadRequest, "Choose a job script.").WithDetails(map[string]any{"field": "script_id"})
		}
		return in, "", err
	}
	if sc.Kind != "job" {
		return in, "", fault.Newf(fault.BadRequest, "Script %q is a %s, not a job script.", sc.Name, sc.Kind).WithDetails(map[string]any{"field": "script_id"})
	}
	if in.Timezone == "" {
		in.Timezone = wsTZ
	}
	if loc, err := time.LoadLocation(in.Timezone); err != nil || in.Timezone == "" || strings.EqualFold(in.Timezone, "local") || loc == nil {
		return in, "", fault.Newf(fault.BadRequest, "Unknown time zone %q.", in.Timezone).WithDetails(map[string]any{"field": "timezone"})
	}
	in.Schedule = strings.TrimSpace(in.Schedule)
	if in.Schedule != "" {
		if _, err := ParseSchedule(in.Schedule, in.Timezone); err != nil {
			fe := fault.Classify(err)
			return in, "", fe.WithDetails(map[string]any{"field": "schedule"})
		}
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 300
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 3600 {
		return in, "", fault.New(fault.BadRequest, "The timeout must be between 1 and 3600 seconds.")
	}
	if in.OverlapPolicy == "" {
		in.OverlapPolicy = "skip"
	}
	if in.MisfirePolicy == "" {
		in.MisfirePolicy = "run_once"
	}
	if in.MaxRetries < 0 || in.MaxRetries > 10 {
		return in, "", fault.New(fault.BadRequest, "Retries must be between 0 and 10.")
	}
	if in.RetryBackoff == 0 {
		in.RetryBackoff = 60
	}
	if in.Args == nil {
		in.Args = map[string]any{}
	}
	args, err := json.Marshal(in.Args)
	if err != nil {
		return in, "", fault.Wrap(fault.BadRequest, err)
	}
	if len(args) > maxArgsBytes {
		return in, "", fault.New(fault.BadRequest, "Job args are larger than 64 KiB.")
	}
	return in, string(args), nil
}

func readTZ(ctx context.Context, l *workspace.Lease) string {
	var name string
	_ = l.Read(ctx, func(q dbx.Querier) error {
		return q.QueryRow(ctx, sqlReadTZ.SQL).Scan(&name)
	})
	if name == "" {
		return "UTC"
	}
	return name
}

// Put creates (id "") or replaces a job. expectedVersion is required for updates.
func (s *Service) Put(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64, in JobInput, user string) (Job, error) {
	in, args, err := s.normalize(ctx, l, in, readTZ(ctx, l))
	if err != nil {
		return Job{}, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	now := dbx.Now()
	err = l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		var sched any
		if in.Schedule != "" {
			sched = in.Schedule
		}
		if id == "" {
			id = ids.New()
			_, err := tx.Exec(ctx, sqlPut.SQL,
				id, in.Name, in.ScriptID, sched, in.Timezone, boolInt(enabled), in.TimeoutSeconds, in.OverlapPolicy,
				in.MisfirePolicy, in.MaxRetries, in.RetryBackoff, args, now, user)
			return err
		}
		res, err := tx.Exec(ctx, sqlPut2.SQL,
			in.Name, in.ScriptID, sched, in.Timezone, boolInt(enabled), in.TimeoutSeconds, in.OverlapPolicy, in.MisfirePolicy,
			in.MaxRetries, in.RetryBackoff, args, now, user, id, expectedVersion)
		if err != nil {
			return err
		}
		if res.Changes == 0 {
			return conflictOrMissing(ctx, tx, id)
		}
		return nil
	})
	if err != nil {
		if fe := fault.Classify(err); fe.Code == fault.DuplicateValue {
			return Job{}, fault.Newf(fault.DuplicateValue, "A job named %q already exists.", in.Name)
		}
		return Job{}, err
	}
	out, err := s.Get(ctx, l, id)
	if err != nil {
		return Job{}, err
	}
	s.changed(ctx, l.ID(), map[string]any{"kind": "job", "id": id, "version": out.Version})
	return out, nil
}

func conflictOrMissing(ctx context.Context, tx dbx.Tx, id string) error {
	var n int
	if err := tx.QueryRow(ctx, sqlConflictOrMissing.SQL, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fault.New(fault.NotFound, "Job not found.")
	}
	return fault.New(fault.ConcurrentModification, "Someone else changed this job. Reload it and try again.")
}

// Delete removes a job and its run history.
func (s *Service) Delete(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64) error {
	err := l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlDelete.SQL, id, expectedVersion)
		if err != nil {
			return err
		}
		if res.Changes == 0 {
			return conflictOrMissing(ctx, tx, id)
		}
		_, err = tx.Exec(ctx, sqlDelete2.SQL, id)
		return err
	})
	if err != nil {
		return err
	}
	s.changed(ctx, l.ID(), map[string]any{"kind": "job", "id": id, "deleted": true})
	return nil
}

func (s *Service) changed(ctx context.Context, ws ids.WorkspaceID, data map[string]any) {
	if s.Events != nil {
		s.Events.Publish(string(ws), events.DesignChanged, data)
	}
	if s.Scheduler != nil {
		// The index is derived; a failed rebuild is retried on the next save or open.
		_ = s.Scheduler.Rebuild(context.WithoutCancel(ctx), ws)
	}
}

// Runs lists a job's runs, newest first.
func (s *Service) Runs(ctx context.Context, l *workspace.Lease, jobID string, limit int, before time.Time) ([]Run, error) {
	var out []Run
	err := l.Read(ctx, func(q dbx.Querier) error {
		var err error
		out, err = ReadRuns(ctx, q, jobID, limit, before)
		return err
	})
	for i := range out {
		out[i].Log = "" // the list omits logs; GetRun returns them
	}
	return out, err
}

// GetRun returns one run with its log.
func (s *Service) GetRun(ctx context.Context, l *workspace.Lease, runID string) (Run, error) {
	var out Run
	err := l.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, `SELECT `+RunCols+` FROM _sys_job_runs WHERE id = ?`, runID)
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
	return out, err
}

// Enqueue starts a run of the job named jobName. fromForm applies the brief's rule
// that forms may only enqueue jobs whose args set allow_enqueue = true.
func (s *Service) Enqueue(ctx context.Context, l *workspace.Lease, jobName, by string, args map[string]any, fromForm bool) (string, error) {
	job, err := s.ByName(ctx, l, jobName)
	if err != nil {
		return "", err
	}
	if fromForm {
		if allow, _ := job.Args["allow_enqueue"].(bool); !allow {
			return "", fault.Newf(fault.Forbidden, "Job %q does not allow being started from forms (set allow_enqueue = true in its args).", job.Name)
		}
	}
	if b, _ := json.Marshal(args); len(b) > maxArgsBytes {
		return "", fault.New(fault.BadRequest, "Enqueue args are larger than 64 KiB.")
	}
	kind := TriggerEnqueue
	run, err := s.Scheduler.RunNow(ctx, l.ID(), job.ID, kind, by, args)
	if err != nil {
		return "", err
	}
	return run.ID, nil
}
