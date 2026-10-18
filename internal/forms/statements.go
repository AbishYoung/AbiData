package forms

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlPut        = sqlgen.Register("forms.Put", `SELECT count(*) FROM _sys_forms WHERE id = ?`)
	sqlWriteDeps  = sqlgen.Register("forms.writeDeps", `DELETE FROM _sys_dependencies WHERE dependent_kind = 'form' AND dependent_id = ?`)
	sqlWriteDeps2 = sqlgen.Register("forms.writeDeps#2", `INSERT INTO _sys_dependencies(dependent_kind, dependent_id, target_kind, target_id, confidence) VALUES ('form', ?, ?, ?, 'exact') ON CONFLICT DO NOTHING`)
	sqlDelete     = sqlgen.Register("forms.Delete", `SELECT count(*) FROM _sys_dependencies WHERE dependent_kind = 'form' AND target_kind = 'form' AND target_id = ?`)
)
