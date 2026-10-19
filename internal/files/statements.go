package files

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlUpload  = sqlgen.Register("files.Upload", `SELECT EXISTS (SELECT 1 FROM _sys_files WHERE sha256 = ?)`)
	sqlUpload2 = sqlgen.Register("files.Upload#2", `SELECT COALESCE(SUM(size), 0) FROM (SELECT MAX(size) AS size FROM _sys_files GROUP BY sha256) AS blobs`)
	sqlUpload3 = sqlgen.Register("files.Upload#3", `INSERT INTO _sys_files(id, sha256, size, name, content_type, width, height, created_at, created_by, state)
			VALUES (?,?,?,?,?,?,?,?,?, 'pending')`)
	sqlUsage      = sqlgen.Register("files.Usage", `SELECT count(*) FROM _sys_files`)
	sqlSweepBatch = sqlgen.Register("files.sweepBatch", `UPDATE _sys_files SET last_unreferenced_at = ? WHERE state = 'attached' AND last_unreferenced_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = _sys_files.id)`)
	sqlSweepBatch2 = sqlgen.Register("files.sweepBatch#2", `SELECT id, sha256, state, size FROM _sys_files
			WHERE (state = 'pending' AND created_at < ? AND NOT EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = _sys_files.id))
			   OR (state = 'attached' AND last_unreferenced_at IS NOT NULL AND last_unreferenced_at < ?
			       AND NOT EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = _sys_files.id))
			ORDER BY created_at LIMIT ?`)
	sqlSweepBatch3 = sqlgen.Register("files.sweepBatch#3", `SELECT EXISTS (SELECT 1 FROM _sys_row_history WHERE at >= ?
					AND (before LIKE '%' || ? || '%' OR after LIKE '%' || ? || '%'))`)
	sqlSweepBatch4  = sqlgen.Register("files.sweepBatch#4", `DELETE FROM _sys_files WHERE id = ?`)
	sqlSweepOrphans = sqlgen.Register("files.sweepOrphans", `SELECT DISTINCT sha256 FROM _sys_files`)
)
