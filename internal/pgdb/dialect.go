package pgdb

import (
	"context"
	"fmt"

	"abidata/internal/dbx"
	"abidata/internal/sqlgen"
)

// Dialect is the sqlgen dialect for the server q talks to: PostgreSQL 17
// lacks virtual generated columns, and anything older is unsupported
// (ADR 0012, "Minimum server").
func Dialect(ctx context.Context, q dbx.Querier) (sqlgen.Dialect, error) {
	var v int
	if err := q.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&v); err != nil {
		return 0, fmt.Errorf("pgdb: server version: %w", err)
	}
	switch {
	case v >= 180000:
		return sqlgen.Postgres, nil
	case v >= 170000:
		return sqlgen.Postgres17, nil
	}
	return 0, fmt.Errorf("pgdb: PostgreSQL %d.%d is not supported; 17 or later is required", v/10000, v%10000)
}

// CheckServer checks what the app needs of the server and its own role
// (ADR 0012 §1): PostgreSQL 17 or later, the database's ownership (to revoke
// PUBLIC's CONNECT and TEMPORARY) and CREATEROLE (for the workspace roles).
// A superuser is refused: the sandbox's guarantees assume an app role that
// can be contained.
func CheckServer(ctx context.Context, q dbx.Querier) error {
	if _, err := Dialect(ctx, q); err != nil {
		return err
	}
	var owner, createRole, super bool
	var role, db string
	if err := q.QueryRow(ctx, `SELECT current_user, current_database(),
		(SELECT pg_get_userbyid(datdba) = current_user FROM pg_database WHERE datname = current_database()),
		r.rolcreaterole, r.rolsuper FROM pg_roles r WHERE r.rolname = current_user`).Scan(&role, &db, &owner, &createRole, &super); err != nil {
		return fmt.Errorf("pgdb: checking the app role: %w", err)
	}
	switch {
	case super:
		return fmt.Errorf("pgdb: role %s is a superuser; connect as a role that owns database %s and has CREATEROLE, and nothing more", role, db)
	case !owner:
		return fmt.Errorf("pgdb: role %s does not own database %s; run ALTER DATABASE %s OWNER TO %s", role, db, Ident(db), Ident(role))
	case !createRole:
		return fmt.Errorf("pgdb: role %s lacks CREATEROLE; run ALTER ROLE %s CREATEROLE", role, Ident(role))
	}
	return nil
}
