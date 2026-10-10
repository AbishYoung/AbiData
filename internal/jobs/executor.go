package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"abidata/internal/artifacts"
	"abidata/internal/catalog"
	"abidata/internal/egress"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/ingest"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	"abidata/internal/scripts"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// JobRole is the workspace role a run executes with (brief: data rights of an editor).
const JobRole = "editor"

// LuaExecutor runs a job's script in the job context.
type LuaExecutor struct {
	Workspaces *workspace.Manager
	Scripts    *scripts.Service
	Runner     *script.Runner
	Schemas    *catalog.Cache
	Records    *records.Service
	Egress     *egress.Gateway // nil: fetch unavailable
	Artifacts  *artifacts.Store
	Ingest     *ingest.Service // nil: the staging module is unavailable
	// Enqueue queues another job from inside a run (jobs.enqueue).
	Enqueue func(ctx context.Context, ws ids.WorkspaceID, jobName, by string, args map[string]any, fromForm bool) (string, error)
}

// Principal is the identity a job run acts as (audit "updated_by" etc.).
func Principal(jobID string) string { return "job:" + jobID }

// Execute implements Executor.
func (x *LuaExecutor) Execute(ctx context.Context, ws ids.WorkspaceID, job Job, run Run) Result {
	l, err := x.Workspaces.Acquire(ctx, ws)
	if err != nil {
		return failure(err)
	}
	defer l.Release()
	ctx, stop := l.Bind(ctx)
	defer stop()

	sc, err := x.Scripts.Get(ctx, l, job.ScriptID)
	if err != nil {
		return failure(err)
	}
	if sc.Kind != "job" {
		return failure(fault.Newf(fault.BadRequest, "Script %q is not a job script.", sc.Name))
	}
	proto, err := x.Scripts.Proto(ws, sc)
	if err != nil {
		return failure(err)
	}
	user := Principal(job.ID)
	d := &host.Data{Lease: l, Schemas: x.Schemas, Records: x.Records, CanWrite: true, UserID: user,
		Job: &host.JobRun{JobID: job.ID, JobName: job.Name, RunID: run.ID, Args: run.Args,
			SaveArtifact: func(name, mime string, content []byte) (int64, error) {
				if x.Artifacts == nil {
					return 0, fault.New(fault.Forbidden, "Artifacts are not configured.")
				}
				info, err := x.Artifacts.Save(ws, run.ID, name, mime, strings.NewReader(string(content)))
				return info.Size, err
			}},
	}
	if x.Ingest != nil {
		rs := x.Ingest.NewRunStaging(l, run.ID)
		defer rs.Close() // staging databases are deleted when the run ends
		d.Staging = rs
	}
	if x.Egress != nil {
		d.Fetch = func(ctx context.Context, req egress.Request) (*egress.Response, error) {
			return x.Egress.Do(ctx, ws, req)
		}
	}
	if x.Enqueue != nil {
		d.Enqueue = func(ctx context.Context, name string, args map[string]any) (string, error) {
			return x.Enqueue(ctx, ws, name, user, args, false)
		}
	}
	tz := time.UTC
	if loc, err := time.LoadLocation(job.Timezone); err == nil {
		tz = loc
	}
	budget := script.DefaultBudgets[script.Job]
	if deadline, ok := ctx.Deadline(); ok {
		budget.Wall = max(time.Until(deadline), time.Millisecond)
	}
	res, err := x.Runner.Exec(ctx, script.ExecRequest{Kind: script.Job, Workspace: ws, TZ: tz,
		Principal: script.Principal{UserID: user, DisplayName: job.Name, Role: JobRole},
		Budget:    budget, Proto: proto, ChunkName: sc.Name, Host: d, Loader: x.Scripts.Loader(ctx, l)})
	out := Result{Status: StatusSucceeded, Log: FormatLogs(res.Logs)}
	if err != nil {
		f := failure(err)
		f.Log = out.Log
		return f
	}
	return out
}

func failure(err error) Result {
	var se *script.Error
	if errors.As(err, &se) {
		code := map[script.ErrorKind]fault.Code{script.KindTimeout: fault.ScriptTimeout, script.KindLimit: fault.ScriptLimitExceeded}[se.Kind]
		if code == "" {
			code = fault.ScriptError
		}
		status := StatusFailed
		if se.Kind == script.KindTimeout {
			status = StatusTimedOut
		}
		return Result{Status: status, ErrorCode: string(code), ErrorMessage: se.Error()}
	}
	if errors.Is(err, context.Canceled) {
		return Result{Status: StatusCancelled, ErrorCode: "CANCELLED", ErrorMessage: "The run was cancelled."}
	}
	fe := fault.Classify(err)
	return Result{Status: StatusFailed, ErrorCode: string(fe.Code), ErrorMessage: fe.DisplayMessage()}
}

// FormatLogs renders log entries as text lines: "<time> <LEVEL> message {fields}".
func FormatLogs(entries []script.LogEntry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s %-5s %s", e.At.UTC().Format("15:04:05.000"), strings.ToUpper(e.Level), e.Message)
		if len(e.Fields) > 0 {
			keys := make([]string, 0, len(e.Fields))
			for k := range e.Fields {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				v, _ := json.Marshal(e.Fields[k])
				parts = append(parts, k+"="+string(v))
			}
			b.WriteString(" " + strings.Join(parts, " "))
		}
		b.WriteByte('\n')
	}
	return TailLog(b.String())
}

// ReadRuns lists a job's runs, newest first (limit ≤ 200), optionally before a queued_at cursor.
func ReadRuns(ctx context.Context, q sqlitex.Querier, jobID string, limit int, before time.Time) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cursor := "9999"
	if !before.IsZero() {
		cursor = sqlitex.FormatTime(before)
	}
	rows, err := q.Query(ctx, `SELECT `+RunCols+` FROM _sys_job_runs WHERE job_id = ? AND queued_at < ?
		ORDER BY queued_at DESC, id DESC LIMIT ?`, jobID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := ScanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
