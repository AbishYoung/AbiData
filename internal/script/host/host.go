// Package host defines the bindings data-access Lua modules receive through
// script.Run.Host. Each context (form events, reports, jobs) fills the parts it
// supports; modules check for what they need.
package host

import (
	"context"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/egress"
	"abidata/internal/files"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

// Data is the host binding for a run.
type Data struct {
	Lease   *workspace.Lease
	Schemas *catalog.Cache
	Records *records.Service
	// Builder grants read access to _sys_* metadata in db.query.
	Builder bool
	// CanWrite is the role-derived write permission (editor+); the context's
	// Budget.Writes must also allow writes.
	CanWrite bool
	UserID   string

	// tx is set while a db.transaction block runs.
	tx dbx.Tx
	// txCtx is the transaction's context (bounded by the TxBlock budget).
	txCtx context.Context

	// afterCommit runs when the enclosing db.transaction commits (change feed).
	afterCommit []func()

	// Files backs the files module (nil: file functions answer FORBIDDEN). Reads
	// and creates run against Lease as UserID.
	Files *files.Service

	// Fetch performs an egress-checked HTTP request (job context only; nil when
	// the run has no network access).
	Fetch func(ctx context.Context, req egress.Request) (*egress.Response, error)

	// Enqueue queues a run of the named job (jobs.enqueue; ui_event and job contexts).
	Enqueue func(ctx context.Context, jobName string, args map[string]any) (runID string, err error)

	// Job describes the running job (job context only).
	Job *JobRun

	// Staging backs the staging module (job context only; nil elsewhere).
	Staging StagingStore

	// UI holds form-event state (ui_event context only).
	UI *UISession
	// Extra carries context-specific values (job args, run id, …).
	Extra map[string]any
}

// JobRun is the job-context binding: the run's identity and arguments, and
// artifact storage for the run.
type JobRun struct {
	JobID, JobName, RunID string
	Args                  map[string]any
	// SaveArtifact stores content as a named artifact of this run and returns its size.
	SaveArtifact func(name, mime string, content []byte) (int64, error)
}

// Tx returns the active db.transaction, if any.
func (d *Data) Tx() (dbx.Tx, context.Context, bool) { return d.tx, d.txCtx, d.tx != nil }

// SetTx installs (or clears with nil) the active transaction.
func (d *Data) SetTx(ctx context.Context, tx dbx.Tx) { d.tx, d.txCtx = tx, ctx }

// AfterCommit queues fn to run after the active transaction commits, or runs
// it immediately when no transaction is active.
func (d *Data) AfterCommit(fn func()) {
	if d.tx == nil {
		fn()
		return
	}
	d.afterCommit = append(d.afterCommit, fn)
}

// FinishTx clears the transaction and runs (committed) or drops queued hooks.
func (d *Data) FinishTx(committed bool) {
	hooks := d.afterCommit
	d.tx, d.txCtx, d.afterCommit = nil, nil, nil
	if committed {
		for _, fn := range hooks {
			fn()
		}
	}
}

// StagingStore is the job-context binding of the staging module: scratch
// SQLite databases that live for one run. Handles are small integers returned
// by Create. Values are nil, bool, int64, float64 or string.
type StagingStore interface {
	// Create makes a staging table with the given column names.
	Create(ctx context.Context, columns []string) (handle int, err error)
	// Insert adds one row: a map keyed by column name or a slice in column order.
	Insert(ctx context.Context, handle int, row any) error
	// Query runs one SELECT against the staging table (named `data`).
	// params is nil, a []any (positional ?) or a map[string]any (named :key).
	Query(ctx context.Context, handle int, sql string, params any) (columns []string, rows [][]any, err error)
	// Count returns the number of staged rows.
	Count(ctx context.Context, handle int) (int64, error)
	// Merge copies the rows into a workspace table; opts holds mode, key,
	// chunk and all_or_nothing. The result has inserted, updated, skipped,
	// error_count and errors.
	Merge(ctx context.Context, handle int, table string, opts map[string]any) (map[string]any, error)
}
