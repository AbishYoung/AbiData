package scripts

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlDelete  = sqlgen.Register("scripts.Delete", `SELECT count(*) FROM _sys_jobs WHERE script_id = ?`)
	sqlDelete2 = sqlgen.Register("scripts.Delete#2", `DELETE FROM _sys_scripts WHERE id = ?`)
	sqlLoader  = sqlgen.Register("scripts.Loader", `SELECT source FROM _sys_scripts WHERE kind = 'module' AND name = ?`)
)
