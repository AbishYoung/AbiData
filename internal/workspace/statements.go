package workspace

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlLeaseWriteWith    = sqlgen.RegisterSQLiteOnly("workspace.Lease.WriteWith", "PRAGMA foreign_keys=OFF")
	sqlLeaseWriteWith2   = sqlgen.RegisterSQLiteOnly("workspace.Lease.WriteWith#2", "PRAGMA foreign_keys=ON")
	sqlHandleCloseConns  = sqlgen.RegisterSQLiteOnly("workspace.handle.closeConns", "PRAGMA optimize")
	sqlHandleCloseConns2 = sqlgen.RegisterSQLiteOnly("workspace.handle.closeConns#2", "PRAGMA wal_checkpoint(TRUNCATE)")
)
