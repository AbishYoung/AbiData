package pgtest

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7).
var sqlPrefixRoles = sqlgen.RegisterPostgresOnly("pgtest.prefixRoles",
	`SELECT rolname FROM pg_roles WHERE starts_with(rolname, ?) ORDER BY rolcanlogin DESC, rolname DESC`)
