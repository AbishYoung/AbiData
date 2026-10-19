package blogs

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7).
var (
	sqlReadPins  = sqlgen.Register("blogs.ReadPins", `SELECT value FROM _sys_meta WHERE key = ?`)
	sqlWritePins = sqlgen.Register("blogs.writePins", `INSERT INTO _sys_meta (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
)
