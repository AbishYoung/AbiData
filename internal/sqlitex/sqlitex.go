// Package sqlitex is the only path to SQLite (D2). It wraps github.com/ncruces/go-sqlite3
// connections (ADR 0001), applies and verifies per-connection pragmas, binds context
// cancellation to SQLite interrupts, caches prepared statements, and exposes the
// authorizer and limits used for tenant isolation.
//
// A *Conn is not safe for concurrent use; owners (workspace, admin) serialise access.
package sqlitex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
)

// Kind selects the pragma profile for a connection.
type Kind int

const (
	Writer      Kind = iota // read-write, WAL, synchronous per config
	Reader                  // query_only
	Maintenance             // read-write, on demand (VACUUM INTO)
	Staging                 // scratch import DBs: journal_mode=OFF, synchronous=OFF
	Snapshot                // read-only verification of a backup file (mode=ro)
)

func (k Kind) String() string {
	return [...]string{"writer", "reader", "maintenance", "staging", "snapshot"}[k]
}

// Options configures Open.
type Options struct {
	Kind         Kind
	Synchronous  string // "NORMAL" (default) or "FULL"; writer only
	MaxPageCount int64  // 0 = unlimited; writer only
	BusyTimeout  time.Duration
}

// ErrNoRows is returned by Row.Scan when the query produced no rows.
var ErrNoRows = errors.New("sqlitex: no rows in result set")

// Conn is one SQLite connection.
type Conn struct {
	c     *sqlite3.Conn
	kind  Kind
	path  string
	cache *stmtCache
	inTx  bool
}

// Open opens path with the profile for opts.Kind. The writer is the only kind that
// may create the file; readers require it to exist (open the writer first).
func Open(ctx context.Context, path string, opts Options) (*Conn, error) {
	uri := "file:" + escapeURIPath(path)
	flags := sqlite3.OPEN_URI | sqlite3.OPEN_NOMUTEX
	switch opts.Kind {
	case Writer, Staging:
		flags |= sqlite3.OPEN_READWRITE | sqlite3.OPEN_CREATE
	case Reader, Maintenance:
		flags |= sqlite3.OPEN_READWRITE
	case Snapshot:
		flags |= sqlite3.OPEN_READONLY
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := sqlite3.OpenFlags(uri, flags)
	if err != nil {
		return nil, fmt.Errorf("sqlitex: open %s (%s): %w", path, opts.Kind, err)
	}
	c := &Conn{c: raw, kind: opts.Kind, path: path, cache: newStmtCache(64)}
	if err := c.configure(opts); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func escapeURIPath(p string) string {
	r := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")
	return r.Replace(p)
}

func (c *Conn) configure(opts Options) error {
	bt := opts.BusyTimeout
	if bt == 0 {
		bt = 5 * time.Second
	}
	// No ATTACH on any connection: isolation must not depend on the authorizer alone.
	c.c.Limit(sqlite3.LIMIT_ATTACHED, 0)

	stmts := []string{
		fmt.Sprintf("PRAGMA busy_timeout=%d", bt.Milliseconds()),
		"PRAGMA trusted_schema=OFF",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA cache_size=-4096",
	}
	switch opts.Kind {
	case Writer:
		sync := strings.ToUpper(opts.Synchronous)
		if sync == "" {
			sync = "NORMAL"
		}
		if sync != "NORMAL" && sync != "FULL" {
			return fmt.Errorf("sqlitex: invalid synchronous %q", opts.Synchronous)
		}
		stmts = append(stmts, "PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA synchronous="+sync,
			"PRAGMA journal_size_limit=67108864")
		if opts.MaxPageCount > 0 {
			stmts = append(stmts, fmt.Sprintf("PRAGMA max_page_count=%d", opts.MaxPageCount))
		}
	case Reader:
		stmts = append(stmts, "PRAGMA foreign_keys=ON", "PRAGMA query_only=ON")
	case Maintenance:
		stmts = append(stmts, "PRAGMA foreign_keys=ON")
	case Staging:
		stmts = append(stmts, "PRAGMA foreign_keys=ON", "PRAGMA journal_mode=OFF", "PRAGMA synchronous=OFF")
	case Snapshot:
		stmts = append(stmts, "PRAGMA query_only=ON")
	}
	for _, s := range stmts {
		if err := c.c.Exec(s); err != nil {
			return fmt.Errorf("sqlitex: %s: %w", s, err)
		}
	}
	return nil
}

// Kind returns the connection's profile.
func (c *Conn) Kind() Kind { return c.kind }

// Path returns the database file path.
func (c *Conn) Path() string { return c.path }

// Raw exposes the driver connection to sqlitex-internal helpers and tests.
func (c *Conn) Raw() *sqlite3.Conn { return c.c }

// Close finalises cached statements and closes the connection.
func (c *Conn) Close() error {
	if c.c == nil {
		return nil
	}
	c.cache.closeAll()
	err := c.c.Close()
	c.c = nil
	return err
}

// Authorizer decides whether an action is permitted. Return AuthOK, AuthDeny or AuthIgnore.
type Authorizer func(action sqlite3.AuthorizerActionCode, arg3, arg4, schema, trigger string) sqlite3.AuthorizerReturnCode

// Authorizer return codes re-exported for policy code.
const (
	AuthOK     = sqlite3.AUTH_OK
	AuthDeny   = sqlite3.AUTH_DENY
	AuthIgnore = sqlite3.AUTH_IGNORE
)

// WithAuthorizer installs auth for the duration of fn. Cached statements are
// discarded on entry and exit because SQLite checks authorization at prepare time.
func (c *Conn) WithAuthorizer(auth Authorizer, fn func() error) error {
	c.cache.closeAll()
	if err := c.c.SetAuthorizer(auth); err != nil {
		return err
	}
	defer func() {
		_ = c.c.SetAuthorizer(nil)
		c.cache.closeAll()
	}()
	return fn()
}

// bindCtx makes ctx interrupt any statement on this connection. It returns a restore func.
func (c *Conn) bindCtx(ctx context.Context) func() {
	old := c.c.SetInterrupt(ctx)
	return func() { c.c.SetInterrupt(old) }
}

// Result reports the effect of Exec.
type Result struct {
	Changes      int64
	LastInsertID int64
}

// Named binds a value to a :name, @name or $name parameter.
type Named struct {
	Name  string
	Value any
}

// ExecScript runs a trusted multi-statement script (migrations, internal DDL).
// Never pass user-supplied SQL here.
func (c *Conn) ExecScript(ctx context.Context, sql string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	defer c.bindCtx(ctx)()
	return wrapErr(ctx, c.c.Exec(sql))
}

// Exec runs exactly one statement.
func (c *Conn) Exec(ctx context.Context, sql string, args ...any) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	defer c.bindCtx(ctx)()
	st, release, err := c.prepare(sql)
	if err != nil {
		return Result{}, wrapErr(ctx, err)
	}
	defer release()
	if err := bindArgs(st, args); err != nil {
		return Result{}, err
	}
	if err := st.Exec(); err != nil {
		return Result{}, wrapErr(ctx, err)
	}
	return Result{Changes: c.c.Changes(), LastInsertID: c.c.LastInsertRowID()}, nil
}

// Query runs a statement and returns its rows. Rows must be closed.
func (c *Conn) Query(ctx context.Context, sql string, args ...any) (*Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	restore := c.bindCtx(ctx)
	st, release, err := c.prepare(sql)
	if err != nil {
		restore()
		return nil, wrapErr(ctx, err)
	}
	if err := bindArgs(st, args); err != nil {
		release()
		restore()
		return nil, err
	}
	return &Rows{st: st, ctx: ctx, release: func() { release(); restore() }}, nil
}

// QueryRow runs a query expected to return at most one row.
func (c *Conn) QueryRow(ctx context.Context, sql string, args ...any) *Row {
	rows, err := c.Query(ctx, sql, args...)
	return &Row{rows: rows, err: err}
}

// Prepare compiles sql without executing it (used to validate saved queries and
// expressions). The statement is not cached.
func (c *Conn) Prepare(ctx context.Context, sql string) (columns []string, err error) {
	defer c.bindCtx(ctx)()
	st, tail, err := c.c.Prepare(sql)
	if err != nil {
		return nil, wrapErr(ctx, err)
	}
	if st == nil {
		return nil, errors.New("sqlitex: empty statement")
	}
	defer st.Close()
	if strings.TrimSpace(tail) != "" {
		return nil, errors.New("sqlitex: multiple statements are not allowed")
	}
	n := st.ColumnCount()
	columns = make([]string, n)
	for i := range n {
		columns[i] = st.ColumnName(i)
	}
	return columns, nil
}

func (c *Conn) prepare(sql string) (*sqlite3.Stmt, func(), error) {
	if st := c.cache.take(sql); st != nil {
		return st, func() { c.cache.put(sql, st) }, nil
	}
	st, tail, err := c.c.Prepare(sql)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, errors.New("sqlitex: empty statement")
	}
	if strings.TrimSpace(tail) != "" {
		_ = st.Close()
		return nil, nil, errors.New("sqlitex: multiple statements are not allowed")
	}
	return st, func() { c.cache.put(sql, st) }, nil
}

// ClearStmtCache finalises cached statements (call after schema changes).
func (c *Conn) ClearStmtCache() { c.cache.closeAll() }

func wrapErr(ctx context.Context, err error) error {
	if errors.Is(err, sqlite3.INTERRUPT) && ctx.Err() != nil {
		return errors.Join(ctx.Err(), err)
	}
	return err
}

func bindArgs(st *sqlite3.Stmt, args []any) error {
	if err := st.ClearBindings(); err != nil {
		return err
	}
	pos := 0
	for _, a := range args {
		idx := 0
		if n, ok := a.(Named); ok {
			name := n.Name
			if !strings.ContainsAny(name[:1], ":@$") {
				name = ":" + name
			}
			idx = st.BindIndex(name)
			if idx == 0 {
				return fmt.Errorf("sqlitex: unknown parameter %s", name)
			}
			a = n.Value
		} else {
			pos++
			idx = pos
		}
		if err := bindOne(st, idx, a); err != nil {
			return fmt.Errorf("sqlitex: bind %d: %w", idx, err)
		}
	}
	return nil
}

func bindOne(st *sqlite3.Stmt, i int, v any) error {
	switch x := v.(type) {
	case nil:
		return st.BindNull(i)
	case int:
		return st.BindInt64(i, int64(x))
	case int32:
		return st.BindInt64(i, int64(x))
	case int64:
		return st.BindInt64(i, x)
	case uint32:
		return st.BindInt64(i, int64(x))
	case float64:
		return st.BindFloat(i, x)
	case bool:
		return st.BindBool(i, x)
	case string:
		return st.BindText(i, x)
	case []byte:
		if x == nil {
			return st.BindNull(i)
		}
		return st.BindBlob(i, x)
	case time.Time:
		return st.BindText(i, FormatTime(x))
	case *string:
		if x == nil {
			return st.BindNull(i)
		}
		return st.BindText(i, *x)
	case *int64:
		if x == nil {
			return st.BindNull(i)
		}
		return st.BindInt64(i, *x)
	default:
		return fmt.Errorf("unsupported type %T", v)
	}
}

// TimeLayout is the canonical timestamp format: UTC, millisecond precision.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime renders t in TimeLayout.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// Now returns the current time in TimeLayout.
func Now() string { return FormatTime(time.Now()) }

// ParseTime parses TimeLayout.
func ParseTime(s string) (time.Time, error) { return time.Parse(TimeLayout, s) }
