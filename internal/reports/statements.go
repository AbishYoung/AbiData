package reports

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlPut  = sqlgen.Register("reports.Put", `DELETE FROM _sys_dependencies WHERE dependent_kind = 'report' AND dependent_id = ?`)
	sqlPut2 = sqlgen.Register("reports.Put#2", `INSERT INTO _sys_dependencies(dependent_kind, dependent_id, target_kind, target_id, confidence) VALUES ('report', ?, ?, ?, 'exact') ON CONFLICT DO NOTHING`)
)
