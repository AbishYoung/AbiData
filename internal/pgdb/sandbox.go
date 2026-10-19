// Package pgdb is the PostgreSQL backend (ADR 0012). This file is the Phase 1
// spike's result: the user-SQL sandbox, built from schemas, roles and
// privileges instead of SQLite's authorizer.
//
// The design the spike settled on (ADR 0012 §3, "Spike findings"):
//
//   - Each workspace has a schema and NOLOGIN roles _read and _write, plus
//     LOGIN roles _sqlr (a member of _read only) and _sqlw (a member of
//     _write). User-typed SQL runs on a connection logged in as one of them,
//     never as the app's own role. PostgreSQL checks SET ROLE (and
//     set_config('role', …)) against the session user, so a session that is
//     the app role could become any workspace's role, or the app role itself.
//     A session that is _sqlr can reach nothing but _read, so a read context
//     has two barriers against writes: READ ONLY and the missing grants.
//   - Login passwords are random, set by the app when it opens a pool, and
//     sent as SCRAM verifiers, so they are never stored and never appear in
//     server logs.
//   - User SQL runs as DECLARE … CURSOR inside a READ ONLY transaction, which
//     the parser restricts to one SELECT, VALUES or WITH without data-modifying
//     parts, and rows are fetched in bounded batches.
//   - Nothing here needs a superuser.
package pgdb

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/dbx"
)

// Names derives every object name of one workspace. Identifiers are lower
// case, so they need no quoting in psql, and stay within PostgreSQL's 63 bytes.
type Names struct {
	Prefix    string // install prefix (ABIDATA_POSTGRES_SCHEMA_PREFIX)
	Workspace string // workspace ID
}

func (n Names) base() string   { return n.Prefix + "_ws_" + strings.ToLower(n.Workspace) }
func (n Names) Schema() string { return n.base() }

// ReadRole may read the workspace's user tables.
func (n Names) ReadRole() string { return ReadRoleOf(n.Schema()) }

// WriteRole adds INSERT, UPDATE and DELETE on user tables (Lua db.exec).
func (n Names) WriteRole() string { return n.base() + "_write" }

// ReadLogin is the role user SQL connects as in read contexts.
func (n Names) ReadLogin() string { return n.base() + "_sqlr" }

// WriteLogin is the role Lua db.exec connects as in write contexts.
func (n Names) WriteLogin() string { return n.base() + "_sqlw" }

// ReadRoleOf is the read role of the workspace whose schema is schema.
func ReadRoleOf(schema string) string { return schema + "_read" }

// Lib is the install's shared function schema.
func (n Names) Lib() string { return n.Prefix + "_lib" }

// Admin is the install's admin schema.
func (n Names) Admin() string { return n.Prefix + "_admin" }

// Ident quotes a PostgreSQL identifier.
func Ident(s string) string { return ident(s) }

func ident(s string) string { return pgx.Identifier{s}.Sanitize() }

// HardenDatabase removes what PUBLIC may do in the app's database by default.
// The app's role must own the database. It is idempotent.
func HardenDatabase(ctx context.Context, c dbx.Execer, database string) error {
	for _, s := range []string{
		`REVOKE TEMPORARY, CONNECT ON DATABASE ` + ident(database) + ` FROM PUBLIC`,
		`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
	} {
		if _, err := c.Exec(ctx, s); err != nil {
			return fmt.Errorf("pgdb: harden: %w", err)
		}
	}
	return nil
}

// ProvisionWorkspace creates a workspace's schema and roles. The app's role
// (CREATEROLE, owner of the database) runs it; it becomes the schema's owner
// and keeps membership in _read and _write for generated SQL, which runs on
// the app's own pool under SET LOCAL ROLE as defence in depth.
func ProvisionWorkspace(ctx context.Context, c dbx.Execer, database string, n Names) error {
	s, r, w := ident(n.Schema()), ident(n.ReadRole()), ident(n.WriteRole())
	lr, lw := ident(n.ReadLogin()), ident(n.WriteLogin())
	for _, q := range []string{
		`CREATE SCHEMA ` + s,
		`CREATE ROLE ` + r + ` NOLOGIN`,
		`CREATE ROLE ` + w + ` NOLOGIN`,
		`GRANT ` + r + ` TO ` + w,
		// NOINHERIT: a login holds no privileges itself; it must SET ROLE.
		`CREATE ROLE ` + lr + ` LOGIN NOINHERIT CONNECTION LIMIT 4`,
		`CREATE ROLE ` + lw + ` LOGIN NOINHERIT CONNECTION LIMIT 2`,
		`GRANT ` + r + ` TO ` + lr,
		`GRANT ` + w + ` TO ` + lw,
		`GRANT ` + r + `, ` + w + ` TO CURRENT_USER`,
		`GRANT CONNECT ON DATABASE ` + ident(database) + ` TO ` + lr + `, ` + lw,
		`GRANT USAGE ON SCHEMA ` + s + ` TO ` + r,
		// Generated SQL calls <prefix>_lib's functions, as user SQL may.
		`GRANT USAGE ON SCHEMA ` + ident(n.Lib()) + ` TO ` + r,
	} {
		if _, err := c.Exec(ctx, q); err != nil {
			return fmt.Errorf("pgdb: provision %s: %w", n.Workspace, err)
		}
	}
	return nil
}

// GrantUserTable opens a user table to the workspace's sandbox roles. System
// tables (_sys_*) are never granted.
func GrantUserTable(ctx context.Context, c dbx.Execer, n Names, table string) error {
	if strings.HasPrefix(table, "_sys_") {
		return fmt.Errorf("pgdb: %s is a system table", table)
	}
	t := ident(n.Schema()) + "." + ident(table)
	for _, q := range []string{
		`GRANT SELECT ON ` + t + ` TO ` + ident(n.ReadRole()),
		`GRANT INSERT, UPDATE, DELETE ON ` + t + ` TO ` + ident(n.WriteRole()),
	} {
		if _, err := c.Exec(ctx, q); err != nil {
			return fmt.Errorf("pgdb: grant %s: %w", table, err)
		}
	}
	return nil
}

// DropWorkspace removes a workspace's schema and roles.
func DropWorkspace(ctx context.Context, c dbx.Execer, n Names) error {
	var database string
	if err := c.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return fmt.Errorf("pgdb: drop %s: %w", n.Workspace, err)
	}
	// CONNECT on the database, and USAGE on <prefix>_lib, would keep the
	// roles from being dropped.
	var qs []string
	var read bool
	if err := c.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)`, n.ReadRole()).Scan(&read); err != nil {
		return fmt.Errorf("pgdb: drop %s: %w", n.Workspace, err)
	}
	if read {
		qs = append(qs, `REVOKE USAGE ON SCHEMA `+ident(n.Lib())+` FROM `+ident(n.ReadRole()))
	}
	for _, login := range []string{n.ReadLogin(), n.WriteLogin()} {
		var exists bool
		if err := c.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)`, login).Scan(&exists); err != nil {
			return fmt.Errorf("pgdb: drop %s: %w", n.Workspace, err)
		}
		if exists {
			qs = append(qs, `REVOKE CONNECT ON DATABASE `+ident(database)+` FROM `+ident(login))
		}
	}
	for _, q := range append(qs,
		`DROP SCHEMA IF EXISTS `+ident(n.Schema())+` CASCADE`,
		`DROP ROLE IF EXISTS `+ident(n.ReadLogin()),
		`DROP ROLE IF EXISTS `+ident(n.WriteLogin()),
		`DROP ROLE IF EXISTS `+ident(n.WriteRole()),
		`DROP ROLE IF EXISTS `+ident(n.ReadRole()),
	) {
		if _, err := c.Exec(ctx, q); err != nil {
			return fmt.Errorf("pgdb: drop %s: %w", n.Workspace, err)
		}
	}
	return nil
}

// scramVerifier returns the SCRAM-SHA-256 verifier PostgreSQL stores for
// password, so ALTER ROLE never carries the password itself.
func scramVerifier(password string) (string, error) {
	const iter = 4096
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	salted, err := pbkdf2.Key(sha256.New, password, salt, iter, 32)
	if err != nil {
		return "", err
	}
	mac := func(key []byte, msg string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(msg))
		return h.Sum(nil)
	}
	stored := sha256.Sum256(mac(salted, "Client Key"))
	b64 := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iter, b64(salt), b64(stored[:]), b64(mac(salted, "Server Key"))), nil
}

// Limits bound one user SQL statement (ADR 0012 §3).
type Limits struct {
	Timeout time.Duration // statement_timeout; required
	MaxRows int           // rows returned; one more is fetched to report truncation
	WorkMem string        // work_mem, e.g. "16MB"; empty keeps the server's
}

// Result is a sandboxed query's output.
type Result struct {
	Columns   []string
	Origins   []Origin // per column
	Rows      [][]any
	Truncated bool
}

// Origin is where a result column comes from: a table column (TableOID and
// Attr, zero for an expression) and the column's type.
type Origin struct {
	TableOID uint32
	Attr     uint16
	TypeOID  uint32
}

func origins(fs []pgconn.FieldDescription) []Origin {
	out := make([]Origin, len(fs))
	for i, f := range fs {
		out[i] = Origin{TableOID: f.TableOID, Attr: f.TableAttributeNumber, TypeOID: f.DataTypeOID}
	}
	return out
}

// Sandbox runs user-typed SQL for workspaces, each on its own small pool
// logged in as that workspace's _sqlr role.
type Sandbox struct {
	admin  *pgxpool.Pool   // the app's own pool: rotates _sql passwords
	config *pgxpool.Config // template for workspace pools (host, database, TLS)

	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
}

// NewSandbox prepares a sandbox. config is the app's pool configuration; only
// its connection target is reused.
func NewSandbox(admin *pgxpool.Pool) *Sandbox {
	return &Sandbox{admin: admin, config: admin.Config(), pools: map[string]*pgxpool.Pool{}}
}

// Close closes every workspace pool.
func (s *Sandbox) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, p := range s.pools {
		p.Close()
		delete(s.pools, k)
	}
}

// Forget closes a workspace's pool (on workspace deletion or restore).
func (s *Sandbox) Forget(n Names) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range []string{n.ReadLogin(), n.WriteLogin()} {
		if p, ok := s.pools[l]; ok {
			p.Close()
			delete(s.pools, l)
		}
	}
}

func (s *Sandbox) pool(ctx context.Context, login string) (*pgxpool.Pool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.pools[login]; ok {
		return p, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	password := hex.EncodeToString(secret)
	verifier, err := scramVerifier(password)
	if err != nil {
		return nil, err
	}
	// The verifier is base64 and '$'-separated, so it is a safe literal.
	if _, err := s.admin.Exec(ctx, `ALTER ROLE `+ident(login)+` PASSWORD '`+verifier+`'`); err != nil {
		return nil, fmt.Errorf("pgdb: sandbox login %s: %w", login, err)
	}
	cfg := s.config.Copy()
	cfg.ConnConfig.User = login
	cfg.ConnConfig.Password = password
	cfg.ConnConfig.RuntimeParams = map[string]string{"application_name": "abidata-sandbox"}
	// No cached prepared statements: user SQL rarely repeats, and DISCARD ALL
	// below would leave pgx's cache pointing at deallocated statements.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = time.Minute
	cfg.BeforeConnect = nil
	cfg.AfterConnect = nil
	// Anything a statement managed to leave on the session (advisory locks,
	// settings, prepared statements, LISTEN) is discarded before reuse; a
	// connection that cannot be reset is dropped.
	cfg.AfterRelease = func(c *pgx.Conn) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := c.Exec(ctx, "DISCARD ALL")
		return err == nil
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s.pools[login] = p
	return p, nil
}

// ErrMultipleStatements reports user SQL with more than one statement.
var ErrMultipleStatements = errors.New("pgdb: enter a single statement")

// Query runs one read-only statement typed by a person, as the workspace's
// read role. args may be positional or dbx.Named, as on SQLite.
func (s *Sandbox) Query(ctx context.Context, n Names, sql string, args []any, lim Limits) (Result, error) {
	if lim.Timeout <= 0 {
		return Result{}, errors.New("pgdb: sandbox query needs a timeout")
	}
	body, bound, err := dbx.Rebind(strings.TrimRight(strings.TrimSpace(sql), ";"), args)
	if err != nil {
		return Result{}, err
	}
	p, err := s.pool(ctx, n.ReadLogin())
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, lim.Timeout+5*time.Second)
	defer cancel()
	tx, err := p.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	setup := []string{
		`SET LOCAL ROLE ` + ident(n.ReadRole()),
		`SET LOCAL search_path = ` + ident(n.Schema()) + `, ` + ident(n.Lib()),
		fmt.Sprintf(`SET LOCAL statement_timeout = %d`, lim.Timeout.Milliseconds()),
		fmt.Sprintf(`SET LOCAL idle_in_transaction_session_timeout = %d`, (lim.Timeout + 5*time.Second).Milliseconds()),
	}
	if lim.WorkMem != "" {
		setup = append(setup, `SET LOCAL work_mem = '`+strings.ReplaceAll(lim.WorkMem, "'", "")+`'`)
	}
	for _, q := range setup {
		if _, err := tx.Exec(ctx, q); err != nil {
			return Result{}, err
		}
	}
	// The cursor gate: DECLARE accepts only a query, and the extended
	// protocol only one statement. It goes through Query, not Exec: pgx's
	// Exec switches to the simple protocol whenever there are no arguments,
	// whatever mode is asked for, and the simple protocol runs every
	// statement in the string.
	declared, err := tx.Query(ctx, `DECLARE abidata_q NO SCROLL CURSOR FOR `+body, append([]any{pgx.QueryExecModeExec}, bound...)...)
	if err == nil {
		declared.Close()
		err = declared.Err()
	}
	if err != nil {
		if strings.Contains(err.Error(), "multiple commands") {
			return Result{}, ErrMultipleStatements
		}
		return Result{}, err
	}
	maxRows := lim.MaxRows
	if maxRows <= 0 {
		maxRows = 10000
	}
	rows, err := tx.Query(ctx, fmt.Sprintf(`FETCH FORWARD %d FROM abidata_q`, maxRows+1))
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var out Result
	for _, f := range rows.FieldDescriptions() {
		out.Columns = append(out.Columns, f.Name)
	}
	out.Origins = origins(rows.FieldDescriptions())
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			return Result{}, err
		}
		if len(out.Rows) == maxRows {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, v)
	}
	return out, rows.Err()
}

// Describe checks that sql is one statement that only reads, as the
// workspace's read role on its own login, and returns its result columns
// without running it.
func (s *Sandbox) Describe(ctx context.Context, n Names, sql string) ([]string, []Origin, error) {
	p, err := s.pool(ctx, n.ReadLogin())
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := p.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = `+ident(n.Schema())+`, `+ident(n.Lib())); err != nil {
		return nil, nil, err
	}
	return DescribeIn(ctx, WrapTx(tx), n.ReadRole(), sql)
}

// errDescribed rolls back DescribeIn's savepoint.
var errDescribed = errors.New("pgdb: described")

// DescribeIn checks, inside tx and as role, that sql is one statement that
// only reads, and returns its result columns without running it. It runs in
// a savepoint that is rolled back, which also ends the role, so it can check
// user SQL inside the app's own transaction: a schema change checks saved
// queries against the schema it is about to commit.
//
// The statement is prepared as a subquery, which PostgreSQL allows only for
// a SELECT, VALUES or WITH without data-modifying parts; the newline keeps a
// trailing comment from swallowing the closing parenthesis. Preparing checks
// no privileges, so the statement is also planned (EXPLAIN GENERIC_PLAN, which
// needs no parameter values), which does.
func DescribeIn(ctx context.Context, tx Tx, role, sql string) (columns []string, orig []Origin, err error) {
	body, err := dbx.RebindText(strings.TrimRight(strings.TrimSpace(sql), ";"))
	if err != nil {
		return nil, nil, err
	}
	wrapped := "SELECT * FROM (\n" + body + "\n) AS abidata_q"
	err = tx.Savepoint(ctx, func() error {
		for _, q := range []string{`SET LOCAL ROLE ` + ident(role), `SET LOCAL statement_timeout = 5000`} {
			if _, err := tx.tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		sd, err := tx.tx.Conn().Prepare(ctx, "", wrapped)
		if err != nil {
			return err
		}
		plan, err := tx.tx.Query(ctx, "EXPLAIN (GENERIC_PLAN, COSTS OFF) "+wrapped, pgx.QueryExecModeExec)
		if err == nil {
			plan.Close()
			err = plan.Err()
		}
		if err != nil {
			return err
		}
		for _, f := range sd.Fields {
			columns = append(columns, f.Name)
		}
		orig = origins(sd.Fields)
		return errDescribed
	})
	if errors.Is(err, errDescribed) {
		return columns, orig, nil
	}
	return nil, nil, err
}
