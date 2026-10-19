package workspace

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlLeaseWriteWith    = sqlgen.RegisterSQLiteOnly("workspace.Lease.WriteWith", "PRAGMA foreign_keys=OFF")
	sqlLeaseWriteWith2   = sqlgen.RegisterSQLiteOnly("workspace.Lease.WriteWith#2", "PRAGMA foreign_keys=ON")
	sqlHandleCloseConns  = sqlgen.RegisterSQLiteOnly("workspace.handle.closeConns", "PRAGMA optimize")
	sqlHandleCloseConns2 = sqlgen.RegisterSQLiteOnly("workspace.handle.closeConns#2", "PRAGMA wal_checkpoint(TRUNCATE)")
	sqlPGOpen            = sqlgen.RegisterPostgresOnly("workspace.pgOpen", `SELECT to_regnamespace(?) IS NOT NULL`)
	sqlPGWrite           = sqlgen.RegisterPostgresOnly("workspace.pgWrite", `SELECT 1 FROM _sys_schema WHERE id = 1 FOR UPDATE`)
	sqlPGSchemaSize      = sqlgen.RegisterPostgresOnly("workspace.pgSchemaSize", `SELECT coalesce(sum(pg_total_relation_size(c.oid)), 0)::bigint
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = ? AND c.relkind IN ('r', 'm')`)
)
