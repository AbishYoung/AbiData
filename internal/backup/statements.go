package backup

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlSnapshot   = sqlgen.RegisterSQLiteOnly("backup.snapshot", `VACUUM INTO ?`)
	sqlInspect    = sqlgen.RegisterSQLiteOnly("backup.inspect", `PRAGMA application_id`)
	sqlInspect2   = sqlgen.RegisterSQLiteOnly("backup.inspect#2", `PRAGMA user_version`)
	sqlInspect3   = sqlgen.RegisterSQLiteOnly("backup.inspect#3", `SELECT sqlite_version()`)
	sqlInspect4   = sqlgen.Register("backup.inspect#4", `SELECT value FROM _sys_meta WHERE key = 'schema_version'`)
	sqlInspect5   = sqlgen.RegisterSQLiteOnly("backup.inspect#5", `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	sqlPrepare    = sqlgen.Register("backup.prepare", `UPDATE _sys_meta SET value = ? WHERE key = 'workspace_id'`)
	sqlPrepare2   = sqlgen.RegisterSQLiteOnly("backup.prepare#2", `PRAGMA journal_mode = DELETE`)
	sqlFinish     = sqlgen.RegisterSQLiteOnly("backup.finish", `PRAGMA quick_check`)
	sqlListFiles  = sqlgen.RegisterSQLiteOnly("backup.listFiles", `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = '_sys_files'`)
	sqlListFiles2 = sqlgen.Register("backup.listFiles#2", `SELECT sha256, MAX(size) FROM _sys_files GROUP BY sha256 ORDER BY sha256`)
)
