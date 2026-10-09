// Package host defines the bindings data-access Lua modules receive through
// script.Run.Host. Each context (form events, reports, jobs) fills the parts it
// supports; modules check for what they need.
package host

import (
	"context"

	"abidata/internal/catalog"
	"abidata/internal/records"
	"abidata/internal/sqlitex"
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
	tx *sqlitex.Tx
	// txCtx is the transaction's context (bounded by the TxBlock budget).
	txCtx context.Context

	// UI holds form-event state (ui_event context only).
	UI *UISession
	// Extra carries context-specific values (job args, run id, …).
	Extra map[string]any
}

// Tx returns the active db.transaction, if any.
func (d *Data) Tx() (*sqlitex.Tx, context.Context, bool) { return d.tx, d.txCtx, d.tx != nil }

// SetTx installs (or clears with nil) the active transaction.
func (d *Data) SetTx(ctx context.Context, tx *sqlitex.Tx) { d.tx, d.txCtx = tx, ctx }
