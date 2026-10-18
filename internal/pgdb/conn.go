package pgdb

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/dbx"
)

// pgxQuerier is what pgx pools, connections and transactions share.
type pgxQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB adapts a pgx pool, connection or transaction to dbx. SQL is written with
// SQLite-style placeholders, as everywhere else, and rebound to $n here.
//
// Values come back as pgx decodes them; converting them to AbiData's logical
// types is the codecs' job (ADR 0013 §3, Phase F).
type DB struct{ q pgxQuerier }

// Wrap adapts q.
func Wrap(q pgxQuerier) DB { return DB{q: q} }

// Pool adapts a pool.
func Pool(p *pgxpool.Pool) DB { return DB{q: p} }

var (
	_ dbx.Execer = DB{}
	_ dbx.Rows   = rows{}
)

// Exec runs a statement. Without arguments pgx sends it over the simple
// protocol, which runs every statement in the string: Exec is for the app's
// own SQL only. User SQL goes through Sandbox.
func (d DB) Exec(ctx context.Context, sql string, args ...any) (dbx.Result, error) {
	sql, args, err := dbx.Rebind(sql, args)
	if err != nil {
		return dbx.Result{}, err
	}
	tag, err := d.q.Exec(ctx, sql, args...)
	if err != nil {
		return dbx.Result{}, err
	}
	// PostgreSQL has no last-insert id; callers use RETURNING.
	return dbx.Result{Changes: tag.RowsAffected()}, nil
}

func (d DB) Query(ctx context.Context, sql string, args ...any) (dbx.Rows, error) {
	sql, args, err := dbx.Rebind(sql, args)
	if err != nil {
		return nil, err
	}
	r, err := d.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return rows{r}, nil
}

func (d DB) QueryRow(ctx context.Context, sql string, args ...any) dbx.Row {
	sql, args, err := dbx.Rebind(sql, args)
	if err != nil {
		return errRow{err}
	}
	return row{d.q.QueryRow(ctx, sql, args...)}
}

type rows struct{ r pgx.Rows }

func (r rows) Next() bool             { return r.r.Next() }
func (r rows) Err() error             { return r.r.Err() }
func (r rows) Close()                 { r.r.Close() }
func (r rows) Scan(dest ...any) error { return r.r.Scan(dest...) }

func (r rows) Columns() []string {
	fs := r.r.FieldDescriptions()
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}

func (r rows) Values() []any {
	v, err := r.r.Values()
	if err != nil {
		return nil
	}
	return v
}

type row struct{ r pgx.Row }

func (r row) Scan(dest ...any) error {
	err := r.r.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbx.ErrNoRows
	}
	return err
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }
