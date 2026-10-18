package ingest

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlCreateStagingSchema = sqlgen.Register("ingest.createStagingSchema", `CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	sqlSetStatus           = sqlgen.Register("ingest.setStatus", `UPDATE meta SET value = ? WHERE key = 'status'`)
	sqlSaveMeta            = sqlgen.Register("ingest.saveMeta", `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	sqlReadMeta            = sqlgen.Register("ingest.readMeta", `SELECT key, value FROM meta`)
)
