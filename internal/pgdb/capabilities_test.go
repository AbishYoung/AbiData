package pgdb

// ADR 0012 Phase 1: the sandbox's capability tests, the PostgreSQL
// counterpart of internal/sqlitex/capabilities_test.go. They need a server:
// ABIDATA_TEST_POSTGRES_URL names a role that may create databases and roles
// (setup only). Everything the sandbox does then runs as a fresh app role
// with CREATEROLE and no superuser, as in production.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/dbx"
	"abidata/internal/pgtest"
)

type env struct {
	app  *pgxpool.Pool
	sb   *Sandbox
	a, b Names
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pe := pgtest.New(t)
	app, db, prefix := pe.App, pe.Database, pe.Prefix
	e := &env{app: app, sb: NewSandbox(app),
		a: Names{Prefix: prefix, Workspace: "01J0000000000000000000000A"},
		b: Names{Prefix: prefix, Workspace: "01J0000000000000000000000B"}}
	for _, n := range []Names{e.a, e.b} {
		pe.DropRoles(n.ReadLogin(), n.WriteLogin(), n.WriteRole(), n.ReadRole())
	}
	t.Cleanup(e.sb.Close)

	d := Pool(app)
	var super bool
	if err := d.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&super); err != nil || super {
		t.Fatalf("app role must not be a superuser: %v %v", super, err)
	}
	if err := HardenDatabase(ctx, d, db); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `CREATE SCHEMA `+ident(e.a.Lib())); err != nil {
		t.Fatal(err)
	}
	for _, n := range []Names{e.a, e.b} {
		if err := ProvisionWorkspace(ctx, d, db, n); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(ctx, `GRANT USAGE ON SCHEMA `+ident(n.Lib())+` TO `+ident(n.ReadRole())); err != nil {
			t.Fatal(err)
		}
		s := ident(n.Schema())
		for _, q := range []string{
			`CREATE TABLE ` + s + `."Orders"(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, item text NOT NULL)`,
			`INSERT INTO ` + s + `."Orders"(item) VALUES ('` + n.Workspace[len(n.Workspace)-1:] + `-1'), ('` + n.Workspace[len(n.Workspace)-1:] + `-2')`,
			`CREATE TABLE ` + s + `._sys_meta(key text PRIMARY KEY, value text)`,
			`INSERT INTO ` + s + `._sys_meta VALUES ('secret', 'system')`,
		} {
			if _, err := d.Exec(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		if err := GrantUserTable(ctx, d, n, "Orders"); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

var lim = Limits{Timeout: 2 * time.Second, MaxRows: 100}

func (e *env) query(t *testing.T, sql string, args ...any) (Result, error) {
	t.Helper()
	return e.sb.Query(context.Background(), e.a, sql, args, lim)
}

// denied asserts the statement fails and the error mentions want.
func (e *env) denied(t *testing.T, sql, want string, args ...any) {
	t.Helper()
	r, err := e.query(t, sql, args...)
	if err == nil {
		t.Errorf("%s: ran, returned %v", sql, r.Rows)
		return
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("%s: got %v, want an error containing %q", sql, err, want)
	}
}

func TestSandboxReadsOwnTables(t *testing.T) {
	e := setup(t)
	r, err := e.query(t, `SELECT item FROM "Orders" WHERE id >= :min ORDER BY id`, dbx.Named{Name: "min", Value: 1})
	if err != nil || len(r.Rows) != 2 || r.Rows[0][0] != "A-1" || r.Columns[0] != "item" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = e.query(t, `WITH x AS (SELECT count(*) AS n FROM "Orders") SELECT n FROM x`)
	if err != nil || r.Rows[0][0] != int64(2) {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = e.query(t, `VALUES (1), (2), (3)`)
	if err != nil || len(r.Rows) != 3 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestSandboxRowCap(t *testing.T) {
	e := setup(t)
	r, err := e.sb.Query(context.Background(), e.a, `SELECT g FROM generate_series(1, 1000) g`, nil, Limits{Timeout: time.Second, MaxRows: 10})
	if err != nil || len(r.Rows) != 10 || !r.Truncated {
		t.Fatalf("%d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
}

func TestSandboxIsolatesWorkspaces(t *testing.T) {
	e := setup(t)
	e.denied(t, `SELECT * FROM `+ident(e.b.Schema())+`."Orders"`, "permission denied for schema")
	e.denied(t, `SELECT * FROM _sys_meta`, "permission denied for table _sys_meta")
	e.denied(t, `SELECT * FROM `+ident(e.a.Schema())+`._sys_meta`, "permission denied")
}

func TestSandboxCannotChangeRole(t *testing.T) {
	e := setup(t)
	// The escalation the spike found: as the app's own login, set_config
	// could reach any role the app is a member of. As the _sqlr login it
	// can't, not even its own workspace's write role.
	for _, target := range []string{e.b.ReadRole(), e.a.WriteRole(), e.a.WriteLogin(), "postgres"} {
		e.denied(t, `SELECT set_config('role', '`+target+`', true)`, "permission denied to set role")
	}
	var app string
	if err := Pool(e.app).QueryRow(context.Background(), `SELECT current_user`).Scan(&app); err != nil {
		t.Fatal(err)
	}
	e.denied(t, `SELECT set_config('role', '`+app+`', true)`, "permission denied to set role")
	e.denied(t, `SELECT set_config('session_authorization', '`+app+`', true)`, "")
	// Functions that run SQL text reach set_config too; the session user
	// check still holds there.
	e.denied(t, `SELECT query_to_xml('select set_config(''role'', ''`+app+`'', true)', true, false, '')`, "permission denied to set role")
	e.denied(t, `SELECT * FROM ts_stat('select to_tsvector(set_config(''role'', ''`+app+`'', true))')`, "permission denied to set role")
}

func TestSandboxOnlyRunsOneQuery(t *testing.T) {
	e := setup(t)
	// Without arguments, a naive Exec would use the simple protocol and run
	// every statement.
	e.denied(t, `SELECT 1; SET ROLE `+e.a.WriteRole()+`; DELETE FROM "Orders"`, "")
	// That attack also meets the missing write grants, so check the gate on
	// its own: a harmless second statement must be refused too.
	for _, sql := range []string{`SELECT 1; SELECT 2`, `SELECT 1; SET LOCAL work_mem = '1MB'`} {
		if _, err := e.query(t, sql); !errors.Is(err, ErrMultipleStatements) {
			t.Errorf("%s: %v, want ErrMultipleStatements", sql, err)
		}
	}
	e.denied(t, `SET ROLE `+e.a.WriteRole(), "syntax error")
	e.denied(t, `WITH d AS (DELETE FROM "Orders" RETURNING id) SELECT * FROM d`, "data-modifying")
	e.denied(t, `DELETE FROM "Orders"`, "syntax error")
	e.denied(t, `COPY (SELECT 1) TO PROGRAM 'id'`, "syntax error")
	e.denied(t, `SELECT 1) ; COMMIT; DELETE FROM "Orders" WHERE (1=1`, "")
	r, err := e.query(t, `SELECT count(*) FROM "Orders"`)
	if err != nil || r.Rows[0][0] != int64(2) {
		t.Fatalf("rows deleted? %+v %v", r, err)
	}
}

func TestSandboxCannotWriteOrCreate(t *testing.T) {
	e := setup(t)
	e.denied(t, `SELECT lo_create(0)`, "read-only transaction")
	e.denied(t, `SELECT nextval(pg_get_serial_sequence('"Orders"', 'id'))`, "")
	// The cursor gate stops DDL before privileges are consulted; the
	// privileges are checked directly below.
	ctx := context.Background()
	login := e.loginConn(t)
	defer login.Close(ctx)
	for sql, want := range map[string]string{
		`CREATE TEMP TABLE t(x int)`:                                          "permission denied to create temporary tables",
		`CREATE TABLE public.t(x int)`:                                        "permission denied for schema public",
		`CREATE TABLE ` + ident(e.a.Schema()) + `.t(x int)`:                   "permission denied for schema",
		`INSERT INTO ` + ident(e.a.Schema()) + `."Orders"(item) VALUES ('x')`: "permission denied for table Orders",
	} {
		_, err := login.Exec(ctx, `BEGIN; SET LOCAL ROLE `+ident(e.a.ReadRole())+`; `+sql+`; ROLLBACK`)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", sql, err, want)
		}
		_, _ = login.Exec(ctx, `ROLLBACK`)
	}
}

// loginConn opens a raw connection as workspace A's _sqlr login, as an
// attacker with its password would have.
func (e *env) loginConn(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	p, err := e.sb.pool(ctx, e.a.ReadLogin())
	if err != nil {
		t.Fatal(err)
	}
	c, err := pgx.ConnectConfig(ctx, p.Config().ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSandboxServerFileAccessDenied(t *testing.T) {
	e := setup(t)
	e.denied(t, `SELECT pg_read_file('/etc/passwd')`, "permission denied")
	e.denied(t, `SELECT lo_import('/etc/passwd')`, "permission denied")
	e.denied(t, `SELECT pg_ls_dir('.')`, "permission denied")
}

func TestSandboxTimeout(t *testing.T) {
	e := setup(t)
	start := time.Now()
	_, err := e.sb.Query(context.Background(), e.a,
		`WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r) SELECT count(*) FROM r`, nil,
		Limits{Timeout: 300 * time.Millisecond})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "57014" {
		t.Fatalf("got %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
	_, err = e.sb.Query(context.Background(), e.a, `SELECT pg_sleep(5)`, nil, Limits{Timeout: 200 * time.Millisecond})
	if !errors.As(err, &pg) || pg.Code != "57014" {
		t.Fatalf("pg_sleep: %v", err)
	}
}

func TestSandboxLeavesNothingForTheNextBorrower(t *testing.T) {
	e := setup(t)
	// Session-level state a statement can create: settings (is_local false),
	// advisory locks (they outlive ROLLBACK) and LISTEN.
	for _, sql := range []string{
		`SELECT set_config('search_path', 'public', false)`,
		`SELECT set_config('work_mem', '77kB', false)`,
		`SELECT pg_advisory_lock(4242)`,
	} {
		if _, err := e.query(t, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	p, err := e.sb.pool(context.Background(), e.a.ReadLogin())
	if err != nil {
		t.Fatal(err)
	}
	// Drain the pool's idle connections and inspect each one.
	ctx := context.Background()
	var conns []*pgxpool.Conn
	for range p.Stat().TotalConns() {
		c, err := p.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		var sp, workMem string
		var locks int
		if err := c.QueryRow(ctx, `SELECT current_setting('search_path'), current_setting('work_mem'),
			(SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid())`).Scan(&sp, &workMem, &locks); err != nil {
			t.Fatal(err)
		}
		if sp == "public" || workMem == "77kB" || locks != 0 {
			t.Errorf("leaked: search_path %q, work_mem %q, advisory locks %d", sp, workMem, locks)
		}
		c.Release()
	}
	// An advisory lock taken in the sandbox doesn't block the app after the
	// statement ends.
	var got bool
	if err := Pool(e.app).QueryRow(ctx, `SELECT pg_try_advisory_lock(4242)`).Scan(&got); err != nil || !got {
		t.Fatalf("lock still held: %v %v", got, err)
	}
}

// The tests below document what the privilege model does NOT hide, so the
// ADR's findings stay true or the test says otherwise.

func TestKnownLeakOtherTableNamesAreVisible(t *testing.T) {
	e := setup(t)
	// pg_class is readable by PUBLIC and only a superuser can revoke that;
	// views over it (pg_tables, pg_stat_*) run as their owner anyway.
	r, err := e.query(t, `SELECT tablename FROM pg_tables WHERE schemaname = :s`, dbx.Named{Name: "s", Value: e.b.Schema()})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) == 0 {
		t.Fatal("other workspace's table names are hidden now; update ADR 0012's findings")
	}
	// information_schema filters by privilege.
	r, err = e.query(t, `SELECT table_name FROM information_schema.tables WHERE table_schema = :s`, dbx.Named{Name: "s", Value: e.b.Schema()})
	if err != nil || len(r.Rows) != 0 {
		t.Fatalf("information_schema shows %v %v", r.Rows, err)
	}
}

func TestKnownLeakSandboxCanNotify(t *testing.T) {
	e := setup(t)
	// pg_notify works in a read-only transaction: a LISTEN/NOTIFY event bus
	// (Phase 6) must treat payloads as untrusted wake-ups.
	if _, err := e.query(t, `SELECT pg_notify('abidata_events', 'spoofed')`); err != nil {
		t.Fatalf("pg_notify is denied now; update ADR 0012's findings: %v", err)
	}
}

// DescribeIn checks user SQL on the app's own transaction, as the read role,
// which ADR 0012 §3 otherwise forbids: the escalation of
// TestSandboxCannotChangeRole would work there. It is safe only because the
// SQL is prepared and planned, never run: the planner folds immutable and
// stable functions alone, and nothing that runs SQL text or sets the role is
// either. The statements below would fail, or escalate, if any of it ran.
func TestDescribeInRunsNothing(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var app string
	if err := Pool(e.app).QueryRow(ctx, `SELECT current_user`).Scan(&app); err != nil {
		t.Fatal(err)
	}
	escalate := `set_config(''role'', ''` + app + `'', true)`
	for _, sql := range []string{
		`SELECT * FROM "Orders" WHERE item = query_to_xml('select 1/0', true, false, '')::text`,
		`SELECT * FROM "Orders" WHERE item > query_to_xml('select 1/0', true, false, '')::text AND id = 1`,
		`SELECT query_to_xml('select 1/0', true, false, '')`,
		`SELECT * FROM "Orders" o JOIN generate_series(1, length(query_to_xml('select 1/0', true, false, '')::text)) g ON g = o.id`,
		`SELECT * FROM "Orders" WHERE item = (SELECT set_config('role', '` + app + `', true))`,
		`SELECT * FROM "Orders" WHERE item = query_to_xml('select ` + escalate + `', true, false, '')::text`,
		`SELECT * FROM ts_stat('select to_tsvector(` + escalate + `)')`,
	} {
		tx, err := e.app.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wt := WrapTx(tx)
		if _, err := wt.Exec(ctx, `SET LOCAL search_path = `+ident(e.a.Schema())); err != nil {
			t.Fatal(err)
		}
		if _, _, err := DescribeIn(ctx, wt, e.a.ReadRole(), sql); err != nil {
			t.Errorf("%s: %v", sql, err)
		}
		var role string
		if err := wt.QueryRow(ctx, `SELECT current_setting('role')`).Scan(&role); err != nil || role != "none" {
			t.Errorf("%s: role is %q after the check (%v)", sql, role, err)
		}
		_ = tx.Rollback(ctx)
	}
	// What the read role may not see is still refused. Immutable expressions
	// are folded: why no lib function that is immutable or stable may run SQL
	// text (only triggers and the revoked abi_copy_rows do).
	for _, sql := range []string{`SELECT * FROM _sys_meta`, `SELECT * FROM ` + ident(e.b.Schema()) + `."Orders"`, `SELECT 1/0`} {
		tx, err := e.app.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wt := WrapTx(tx)
		if _, err := wt.Exec(ctx, `SET LOCAL search_path = `+ident(e.a.Schema())); err != nil {
			t.Fatal(err)
		}
		if _, _, err := DescribeIn(ctx, wt, e.a.ReadRole(), sql); err == nil || !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "division by zero") {
			t.Errorf("%s: %v", sql, err)
		}
		_ = tx.Rollback(ctx)
	}
}
