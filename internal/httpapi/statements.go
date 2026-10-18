package httpapi

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlWorkspaceTZ = sqlgen.Register("httpapi.workspaceTZ", `SELECT value FROM _sys_meta WHERE key = 'timezone'`)
)
