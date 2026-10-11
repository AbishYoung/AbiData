package httpapi

import (
	"context"
	"math"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/backup"
	"abidata/internal/obs"
)

// OverviewSeries holds the dashboard time series in columns: every array has
// one entry per element of Times. Requests are per second; script, job and
// egress counts are per interval (IntervalSeconds). Latency is milliseconds
// and null for intervals without requests.
type OverviewSeries struct {
	IntervalSeconds int        `json:"interval_seconds"`
	Times           []int64    `json:"times" doc:"Unix seconds at the end of each interval."`
	Req2xx          []float64  `json:"req_2xx"`
	Req3xx          []float64  `json:"req_3xx"`
	Req4xx          []float64  `json:"req_4xx"`
	Req5xx          []float64  `json:"req_5xx"`
	LatencyP50Ms    []*float64 `json:"latency_p50_ms"`
	LatencyP95Ms    []*float64 `json:"latency_p95_ms"`
	RateLimited     []float64  `json:"rate_limited"`
	LoginFailures   []float64  `json:"login_failures"`
	HeapBytes       []float64  `json:"heap_bytes"`
	Goroutines      []float64  `json:"goroutines"`
	OpenWorkspaces  []float64  `json:"open_workspaces"`
	ScriptOK        []float64  `json:"script_ok"`
	ScriptError     []float64  `json:"script_error"`
	JobsSucceeded   []float64  `json:"jobs_succeeded"`
	JobsFailed      []float64  `json:"jobs_failed" doc:"Failed and timed-out runs."`
	JobsOther       []float64  `json:"jobs_other" doc:"Cancelled and interrupted runs."`
	EgressAllowed   []float64  `json:"egress_allowed"`
	EgressDenied    []float64  `json:"egress_denied"`
	EgressError     []float64  `json:"egress_error"`
}

// OverviewWorkspace is one workspace's storage and activity.
type OverviewWorkspace struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Open         bool       `json:"open" doc:"The database is currently open in memory."`
	DBBytes      int64      `json:"db_bytes"`
	WALBytes     int64      `json:"wal_bytes"`
	LastActivity *time.Time `json:"last_activity,omitempty" doc:"Last write to the database file (file modification time)."`
}

// OverviewFailure is a recent job run or backup failure (kept since process start).
type OverviewFailure struct {
	At            time.Time `json:"at"`
	Kind          string    `json:"kind" enum:"job,backup"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	Subject       string    `json:"subject" doc:"Job ID, or the backup kind."`
	SubjectName   string    `json:"subject_name" doc:"Job name resolved when the failure was recorded; empty for backups and for jobs that no longer exist."`
	Status        string    `json:"status"`
	Message       string    `json:"message"`
}

// OverviewBackup is a workspace's backup status.
type OverviewBackup struct {
	WorkspaceID    string     `json:"workspace_id"`
	WorkspaceName  string     `json:"workspace_name"`
	Status         string     `json:"status" enum:"ok,stale,never,failed" doc:"failed: a backup failed since the last success. stale: scheduled backups are enabled and the newest backup is over 36 hours old."`
	Count          int        `json:"count"`
	LastAt         *time.Time `json:"last_at,omitempty"`
	LastKind       string     `json:"last_kind,omitempty"`
	LastBytes      int64      `json:"last_bytes,omitempty"`
	AgeSeconds     int64      `json:"age_seconds,omitempty"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	LastFailureMsg string     `json:"last_failure_message,omitempty"`
}

// OverviewAudit is a recent audit event.
type OverviewAudit struct {
	At         time.Time `json:"at"`
	Actor      string    `json:"actor" doc:"The user's email, an API key, or system."`
	Action     string    `json:"action"`
	ObjectKind string    `json:"object_kind,omitempty"`
	ObjectID   string    `json:"object_id,omitempty"`
}

// OverviewBackupPolicy summarises the scheduled-backup policy.
type OverviewBackupPolicy struct {
	Enabled bool `json:"enabled"`
	HourUTC int  `json:"hour_utc"`
}

// AdminOverview is the operations dashboard payload.
type AdminOverview struct {
	Stats        AdminStats           `json:"stats"`
	Range        string               `json:"range" enum:"1h,24h"`
	GeneratedAt  time.Time            `json:"generated_at"`
	HistorySince *time.Time           `json:"history_since,omitempty" doc:"When the in-memory history starts (the process start for ranges not yet full)."`
	Series       OverviewSeries       `json:"series"`
	Workspaces   []OverviewWorkspace  `json:"workspaces"`
	Failures     []OverviewFailure    `json:"failures" doc:"Recent job and backup failures since the process started, newest first."`
	Backups      []OverviewBackup     `json:"backups"`
	BackupPolicy OverviewBackupPolicy `json:"backup_policy"`
	Audit        []OverviewAudit      `json:"audit"`
}

const (
	overviewFailureLimit = 10
	overviewAuditLimit   = 15
	staleBackupAge       = 36 * time.Hour
	maxFailureMessage    = 300
)

func seriesColumns(pts []obs.Point, interval int) OverviewSeries {
	n := len(pts)
	out := OverviewSeries{IntervalSeconds: interval, Times: make([]int64, n),
		Req2xx: make([]float64, n), Req3xx: make([]float64, n), Req4xx: make([]float64, n), Req5xx: make([]float64, n),
		LatencyP50Ms: make([]*float64, n), LatencyP95Ms: make([]*float64, n),
		RateLimited: make([]float64, n), LoginFailures: make([]float64, n),
		HeapBytes: make([]float64, n), Goroutines: make([]float64, n), OpenWorkspaces: make([]float64, n),
		ScriptOK: make([]float64, n), ScriptError: make([]float64, n),
		JobsSucceeded: make([]float64, n), JobsFailed: make([]float64, n), JobsOther: make([]float64, n),
		EgressAllowed: make([]float64, n), EgressDenied: make([]float64, n), EgressError: make([]float64, n)}
	opt := func(v float64) *float64 {
		if math.IsNaN(v) {
			return nil
		}
		return &v
	}
	for i, p := range pts {
		out.Times[i] = p.T
		out.Req2xx[i], out.Req3xx[i], out.Req4xx[i], out.Req5xx[i] = p.Req2xx, p.Req3xx, p.Req4xx, p.Req5xx
		out.LatencyP50Ms[i], out.LatencyP95Ms[i] = opt(p.P50), opt(p.P95)
		out.RateLimited[i], out.LoginFailures[i] = p.RateLimited, p.LoginFailures
		out.HeapBytes[i], out.Goroutines[i], out.OpenWorkspaces[i] = p.Heap, p.Goroutines, p.OpenWorkspaces
		out.ScriptOK[i], out.ScriptError[i] = p.ScriptOK, p.ScriptError
		out.JobsSucceeded[i], out.JobsFailed[i], out.JobsOther[i] = p.JobSucceeded, p.JobFailed, p.JobOther
		out.EgressAllowed[i], out.EgressDenied[i], out.EgressError[i] = p.EgressAllowed, p.EgressDenied, p.EgressErr
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *Server) registerAdminOverview(api huma.API, adminOnly map[string]any, tags []string) {
	huma.Register(api, huma.Operation{
		OperationID: "adminOverview", Method: http.MethodGet, Path: "/api/v1/admin/overview", Tags: tags, Metadata: adminOnly,
		Summary: "Operations dashboard: statistics, time series, storage, failures, backups and audit",
		Errors:  []int{401, 403},
	}, func(ctx context.Context, in *struct {
		Range string `query:"range" enum:"1h,24h" default:"1h" doc:"Time series window: 1h (10 s points) or 24h (1 min points)."`
	}) (*struct{ Body AdminOverview }, error) {
		stats, err := s.collectStats(ctx)
		if err != nil {
			return nil, err
		}
		rng := obs.Range1h
		if in.Range == "24h" {
			rng = obs.Range24h
		}
		out := AdminOverview{Stats: stats, Range: string(rng), GeneratedAt: time.Now().UTC(),
			Workspaces: []OverviewWorkspace{}, Failures: []OverviewFailure{}, Backups: []OverviewBackup{}, Audit: []OverviewAudit{}}
		var pts []obs.Point
		if s.deps.Series != nil {
			pts = s.deps.Series.Series(rng)
		}
		out.Series = seriesColumns(pts, rng.IntervalSeconds())
		since := s.started.UTC()
		out.HistorySince = &since

		all, err := s.deps.Admin.ListWorkspaces(ctx)
		if err != nil {
			return nil, err
		}
		names := map[string]string{}
		open := map[string]bool{}
		for _, id := range s.deps.Workspaces.OpenIDs() {
			open[string(id)] = true
		}
		backups, err := s.deps.Admin.ListBackups(ctx, "")
		if err != nil {
			return nil, err
		}
		policy, err := backup.LoadPolicy(ctx, s.deps.Admin)
		if err != nil {
			return nil, err
		}
		out.BackupPolicy = OverviewBackupPolicy{Enabled: policy.Enabled, HourUTC: policy.HourUTC}
		var failures []obs.Failure
		if s.deps.Series != nil {
			failures = s.deps.Series.Failures("", 200)
		}
		now := time.Now()
		for _, w := range all {
			if w.Status == admin.StatusDeleted {
				continue
			}
			id := string(w.ID)
			names[id] = w.Name
			ow := OverviewWorkspace{ID: id, Name: w.Name, Status: string(w.Status), Open: open[id]}
			if p, err := s.deps.Workspaces.PathsFor(w.ID); err == nil {
				var last time.Time
				if fi, err := os.Stat(p.DB); err == nil {
					ow.DBBytes, last = fi.Size(), fi.ModTime()
				}
				if fi, err := os.Stat(p.WAL); err == nil {
					ow.WALBytes = fi.Size()
					if fi.ModTime().After(last) {
						last = fi.ModTime()
					}
				}
				if !last.IsZero() {
					t := last.UTC()
					ow.LastActivity = &t
				}
			}
			out.Workspaces = append(out.Workspaces, ow)

			ob := OverviewBackup{WorkspaceID: id, WorkspaceName: w.Name, Status: "never"}
			for _, b := range backups { // newest first
				if b.WorkspaceID != w.ID {
					continue
				}
				ob.Count++
				if ob.LastAt == nil {
					t := b.CreatedAt.UTC()
					ob.LastAt, ob.LastKind, ob.LastBytes = &t, b.Kind, b.GzBytes
					ob.AgeSeconds = int64(now.Sub(b.CreatedAt).Seconds())
				}
			}
			if ob.LastAt != nil {
				ob.Status = "ok"
				if policy.Enabled && now.Sub(*ob.LastAt) > staleBackupAge {
					ob.Status = "stale"
				}
			}
			for _, f := range failures { // newest first
				if f.Kind == "backup" && f.WorkspaceID == id {
					t := f.At.UTC()
					ob.LastFailureAt, ob.LastFailureMsg = &t, clip(f.Message, maxFailureMessage)
					if ob.LastAt == nil || f.At.After(*ob.LastAt) {
						ob.Status = "failed"
					}
					break
				}
			}
			out.Backups = append(out.Backups, ob)
		}
		sort.Slice(out.Workspaces, func(i, j int) bool { return out.Workspaces[i].DBBytes > out.Workspaces[j].DBBytes })
		sort.Slice(out.Backups, func(i, j int) bool { return out.Backups[i].WorkspaceName < out.Backups[j].WorkspaceName })

		for _, f := range failures {
			if len(out.Failures) >= overviewFailureLimit {
				break
			}
			out.Failures = append(out.Failures, OverviewFailure{At: f.At.UTC(), Kind: f.Kind, WorkspaceID: f.WorkspaceID,
				WorkspaceName: names[f.WorkspaceID], Subject: f.Subject, SubjectName: f.SubjectName, Status: f.Status, Message: clip(f.Message, maxFailureMessage)})
		}

		entries, err := s.deps.Admin.ListAudit(ctx, overviewAuditLimit)
		if err != nil {
			return nil, err
		}
		actors := map[string]string{}
		for _, e := range entries {
			actor, ok := actors[e.UserID]
			if !ok {
				actor = "system"
				if e.UserID != "" {
					actor = e.UserID
					if u, err := s.deps.Admin.GetUser(ctx, e.UserID); err == nil {
						actor = u.Email
					}
				}
				actors[e.UserID] = actor
			}
			out.Audit = append(out.Audit, OverviewAudit{At: e.At.UTC(), Actor: actor, Action: e.Action,
				ObjectKind: e.ObjectKind, ObjectID: e.ObjectID})
		}
		return &struct{ Body AdminOverview }{Body: out}, nil
	})
}
