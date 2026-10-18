package records

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlHistory = sqlgen.Register("records.History", `SELECT id, row_version, op, user_id, at, before, after FROM _sys_row_history
			WHERE table_id = ? AND row_id = ? AND id < ? ORDER BY id DESC LIMIT ?`)
	sqlMutation         = sqlgen.Register("records.mutation", `SELECT user_id, request_hash, response FROM _sys_idempotency WHERE key = ?`)
	sqlMutation2        = sqlgen.Register("records.mutation#2", `INSERT INTO _sys_idempotency(key, user_id, request_hash, status_code, response, created_at) VALUES (?,?,?,?,?,?)`)
	sqlHistory2         = sqlgen.Register("records.history#2", `INSERT INTO _sys_row_history(table_id, row_id, row_version, op, user_id, at, before, after) VALUES (?,?,?,?,?,?,?,?)`)
	sqlPurgeIdempotency = sqlgen.Register("records.PurgeIdempotency", `DELETE FROM _sys_idempotency WHERE created_at < ?`)
)
