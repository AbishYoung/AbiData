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
