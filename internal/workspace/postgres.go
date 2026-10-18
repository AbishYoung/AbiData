package workspace

// Workspaces on PostgreSQL (ADR 0012 §5, Phase 3): each one is a schema with
// its roles in the install's database, reached through the install's one pool.
// A lease means the schema is known and migrated rather than that files are
// open. The gate still serialises writes in the process, and each write
// transaction also locks the workspace's _sys_schema row, which reproduces
// BEGIN IMMEDIATE across connections. That row is not granted to the sandbox
// roles, so user SQL cannot hold it, as it could an advisory lock.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/sqlgen"
)

// ErrSQLiteOnly is returned by the lease methods that hand out a SQLite
// connection or file paths, on a PostgreSQL workspace.
var ErrSQLiteOnly = errors.New("workspace: not available on PostgreSQL yet")

// PostgresConfig puts workspaces in schemas of one PostgreSQL database.
type PostgresConfig struct {
	Pool     *pgxpool.Pool // the app's pool; its role owns the database
	Database string
	Prefix   string         // ABIDATA_POSTGRES_SCHEMA_PREFIX
	Dialect  sqlgen.Dialect // the server's (pgdb.Dialect): Postgres or Postgres17
	// Migrate creates or migrates a workspace's system tables (catalog.MigratePostgres).
	Migrate func(ctx context.Context, id ids.WorkspaceID, d dbx.Execer, n pgdb.Names) error
}

func (m *Manager) pg() *PostgresConfig { return m.cfg.Postgres }

// Names returns a workspace's PostgreSQL object names.
func (m *Manager) Names(id ids.WorkspaceID) pgdb.Names {
	return pgdb.Names{Prefix: m.pg().Prefix, Workspace: string(id)}
}

// Dialect is the SQL dialect of the workspace's backend.
func (l *Lease) Dialect() sqlgen.Dialect {
	if pc := l.m.pg(); pc != nil {
		return pc.Dialect
	}
	return sqlgen.SQLite
}

// Postgres returns the workspace's PostgreSQL object names, or false on SQLite.
func (l *Lease) Postgres() (pgdb.Names, bool) {
	if l.m.pg() == nil {
		return pgdb.Names{}, false
	}
	return l.m.Names(l.h.id), true
}

// pgCreate provisions and migrates a workspace in one transaction, so a
// failure leaves neither its schema nor its roles behind.
func (m *Manager) pgCreate(ctx context.Context, id ids.WorkspaceID) error {
	pc, n := m.pg(), m.Names(id)
	err := pgx.BeginFunc(ctx, pc.Pool, func(tx pgx.Tx) error {
		d := pgdb.WrapTx(tx)
		if err := pgdb.ProvisionWorkspace(ctx, d, pc.Database, n); err != nil {
			return err
		}
		if pc.Migrate != nil {
			return pc.Migrate(ctx, id, d, n)
		}
		return nil
	})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && (pe.Code == "42P06" || pe.Code == "42710") { // duplicate_schema, duplicate_object
		return fmt.Errorf("workspace %s: %w", id, fs.ErrExist)
	}
	if err != nil {
		return fmt.Errorf("workspace %s: %w", id, err)
	}
	return nil
}

// pgOpen checks the workspace's schema exists and migrates it.
func (m *Manager) pgOpen(ctx context.Context, h *handle) error {
	pc, n := m.pg(), m.Names(h.id)
	d := pgdb.Pool(pc.Pool)
	var exists bool
	if err := d.QueryRow(ctx, sqlPGOpen.SQL, pgdb.Ident(n.Schema())).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotExist
	}
	if pc.Migrate != nil {
		if err := pc.Migrate(ctx, h.id, d, n); err != nil {
			return fmt.Errorf("workspace %s: migrate: %w", h.id, err)
		}
	}
	h.pgReaders = make(chan struct{}, m.cfg.MaxReaders)
	h.scriptCtx, h.scriptStop = context.WithCancel(context.Background())
	return nil
}

// begin starts a transaction with the workspace's schema, then <prefix>_lib,
// on its search path. SET LOCAL ends with the transaction, so nothing reaches
// the connection's next borrower.
func (m *Manager) begin(ctx context.Context, id ids.WorkspaceID, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := m.pg().Pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	n := m.Names(id)
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = `+pgdb.Ident(n.Schema())+`, `+pgdb.Ident(n.Lib())); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, err
	}
	return tx, nil
}

// pgRead runs fn in a READ ONLY, REPEATABLE READ transaction: the snapshot a
// WAL reader has. At most MaxReaders run at once per workspace.
func (l *Lease) pgRead(ctx context.Context, fn func(q dbx.Querier) error) error {
	start := time.Now()
	select {
	case l.h.pgReaders <- struct{}{}:
	case <-ctx.Done():
		return fault.Classify(ctx.Err())
	}
	defer func() { <-l.h.pgReaders }()
	if o := l.m.cfg.Observe; o != nil {
		o.ReaderWait(time.Since(start))
	}
	tx, err := l.m.begin(ctx, l.h.id, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fault.Classify(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	return fn(pgdb.WrapTx(tx))
}

// pgWrite runs fn in a transaction holding the workspace's writer row. The
// caller holds the gate; an interactive wait for the row is bounded as the
// gate's is.
func (l *Lease) pgWrite(ctx context.Context, p Priority, fn func(tx dbx.Tx) error) error {
	tx, err := l.m.begin(ctx, l.h.id, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if p == Interactive {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL lock_timeout = %d`, l.m.cfg.GateTimeout.Milliseconds())); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, sqlPGWrite.SQL); err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "55P03" { // lock_not_available
			return &fault.Error{Code: fault.WorkspaceBusy, Cause: err}
		}
		return err
	}
	if p == Interactive {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = 0`); err != nil {
			return err
		}
	}
	if err := fn(pgdb.WrapTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Drop removes a closed workspace's schema and roles, or its database files.
func (m *Manager) Drop(ctx context.Context, id ids.WorkspaceID) error {
	m.mu.Lock()
	_, open := m.handles[id]
	m.mu.Unlock()
	if open {
		return fmt.Errorf("workspace %s: still open", id)
	}
	if m.pg() == nil {
		p, err := m.PathsFor(id)
		if err != nil {
			return err
		}
		for _, f := range []string{p.DB, p.WAL, p.SHM} {
			if err := removeIfExists(f); err != nil {
				return err
			}
		}
		return nil
	}
	if !ids.Valid(string(id)) {
		return ids.ErrInvalidID
	}
	return pgx.BeginFunc(ctx, m.pg().Pool, func(tx pgx.Tx) error {
		return pgdb.DropWorkspace(ctx, pgdb.WrapTx(tx), m.Names(id))
	})
}
