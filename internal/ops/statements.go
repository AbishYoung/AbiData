package ops

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlStart       = sqlgen.Register("ops.Start", `INSERT INTO _sys_operations (id, kind, status, progress, created_by, created_at) VALUES (?,?,?,0,?,?)`)
	sqlFinish      = sqlgen.Register("ops.finish", `UPDATE _sys_operations SET status = ?, progress = ?, result = ?, error_code = ?, error_message = ?, finished_at = ? WHERE id = ?`)
	sqlProgressSet = sqlgen.Register("ops.Progress.Set", `UPDATE _sys_operations SET progress = ? WHERE id = ?`)
	sqlReconcile   = sqlgen.Register("ops.Reconcile", `SELECT id FROM _sys_operations WHERE status IN ('queued','running')`)
	sqlReconcile2  = sqlgen.Register("ops.Reconcile#2", `UPDATE _sys_operations SET status = 'failed', error_code = 'INTERRUPTED',
				error_message = 'The server stopped while this operation was running.', finished_at = ? WHERE id = ?`)
)
