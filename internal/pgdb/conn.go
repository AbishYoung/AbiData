package pgdb

import (
	"context"
	"errors"
	"strings"

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
type DB struct {
	q pgxQuerier
	// key is prepended to each statement as a comment. pgx caches prepared
	// statements by their text, per connection, and a cached statement fails
	// once its result types change ("cached plan must not change result
	// type"): after a column's type changes, or when the same text runs in
	// another workspace's schema. A key naming the schema and its version
	// keeps those apart.
	key string
	// uncached prepares nothing: for transactions that change the schema.
	uncached bool
}

// Keyed returns d with statements keyed by key (see DB.key), or not cached at all.
func (d DB) Keyed(key string, uncached bool) DB {
	if strings.Contains(key, "*/") {
		panic("pgdb: statement key " + key + " ends a comment")
	}
	d.key, d.uncached = key, uncached
	return d
}

// prepare rebinds sql and encodes args for pgx, with d's key and mode.
func (d DB) prepare(sql string, args []any) (string, []any, error) {
	sql, args, err := dbx.Rebind(sql, args)
	if err != nil {
		return "", nil, err
	}
	args = encodeArgs(args)
	if d.key != "" {
		sql = "/* " + d.key + " */ " + sql
	}
	if d.uncached {
		args = append([]any{pgx.QueryExecModeDescribeExec}, args...)
	}
	return sql, args, nil
}

// Wrap adapts q.
func Wrap(q pgxQuerier) DB { return DB{q: q} }

// Pool adapts a pool.
func Pool(p *pgxpool.Pool) DB { return DB{q: p} }

// Tx is a dbx.Tx over a pgx transaction.
type Tx struct {
	DB
	tx pgx.Tx
}

// WrapTx adapts a transaction.
func WrapTx(tx pgx.Tx) Tx { return Tx{DB: Wrap(tx), tx: tx} }

// Keyed is DB.Keyed for a transaction.
func (t Tx) Keyed(key string, uncached bool) Tx {
	t.DB = t.DB.Keyed(key, uncached)
	return t
}

var _ dbx.Tx = Tx{}

// Savepoint runs fn in a savepoint, rolled back alone when fn fails.
func (t Tx) Savepoint(ctx context.Context, fn func() error) (err error) {
	sp, err := t.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = sp.Rollback(ctx)
		}
	}()
	if err = fn(); err != nil {
		return err
	}
	return sp.Commit(ctx)
}

var _ dbx.Preparer = Tx{}

// Prepare compiles sql without running it and returns its result columns.
// It runs in a savepoint, so a statement that does not compile leaves the
// transaction usable, as it does on SQLite.
func (t Tx) Prepare(ctx context.Context, sql string) (columns []string, err error) {
	text, err := dbx.RebindText(sql)
	if err != nil {
		return nil, err
	}
	err = t.Savepoint(ctx, func() error {
		sd, err := t.tx.Conn().Prepare(ctx, "", text)
		if err != nil {
			return err
		}
		for _, f := range sd.Fields {
			columns = append(columns, f.Name)
		}
		return nil
	})
	return columns, err
}

var (
	_ dbx.Execer = DB{}
	_ dbx.Rows   = rows{}
)

// Exec runs a statement. Without arguments pgx sends it over the simple
// protocol, which runs every statement in the string: Exec is for the app's
// own SQL only. User SQL goes through Sandbox.
func (d DB) Exec(ctx context.Context, sql string, args ...any) (dbx.Result, error) {
	sql, args, err := d.prepare(sql, args)
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
	sql, args, err := d.prepare(sql, args)
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
	sql, args, err := d.prepare(sql, args)
	if err != nil {
		return errRow{err}
	}
	return row{d.q.QueryRow(ctx, sql, args...)}
}

type rows struct{ r pgx.Rows }

func (r rows) Next() bool             { return r.r.Next() }
func (r rows) Err() error             { return r.r.Err() }
func (r rows) Close()                 { r.r.Close() }
func (r rows) Scan(dest ...any) error { return r.r.Scan(scanDests(dest)...) }

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
	err := r.r.Scan(scanDests(dest)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbx.ErrNoRows
	}
	return err
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// IsUniqueViolation reports whether err is a unique or primary-key
// violation (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
