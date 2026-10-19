package query

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlLoadAll    = sqlgen.Register("query.loadAll", `SELECT id, name, kind, visual_model, sql_text, dialect, parameters, description, version, updated_at, updated_by FROM _sys_queries`)
	sqlWriteDeps  = sqlgen.Register("query.writeDeps", `DELETE FROM _sys_dependencies WHERE dependent_kind = 'query' AND dependent_id = ?`)
	sqlWriteDeps2 = sqlgen.Register("query.writeDeps#2", `INSERT INTO _sys_dependencies(dependent_kind, dependent_id, target_kind, target_id, confidence)
				VALUES ('query', ?, ?, ?, 'exact') ON CONFLICT DO NOTHING`)
	sqlSave = sqlgen.Register("query.save", `INSERT INTO _sys_queries(id, name, kind, visual_model, sql_text, dialect, parameters, description, version, updated_at, updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name = excluded.name, kind = excluded.kind,
		visual_model = excluded.visual_model, sql_text = excluded.sql_text, dialect = excluded.dialect, parameters = excluded.parameters,
		description = excluded.description, version = excluded.version, updated_at = excluded.updated_at, updated_by = excluded.updated_by`)
	sqlRecompileDependents = sqlgen.Register("query.recompileDependents", `SELECT dependent_id FROM _sys_dependencies WHERE dependent_kind = 'query' AND target_kind = 'query' AND target_id = ?`)
	sqlDelete              = sqlgen.Register("query.Delete", `SELECT version FROM _sys_queries WHERE id = ?`)
	sqlDelete2             = sqlgen.Register("query.Delete#2", `SELECT q.name FROM _sys_dependencies d JOIN _sys_queries q ON q.id = d.dependent_id
			WHERE d.dependent_kind = 'query' AND d.target_kind = 'query' AND d.target_id = ? ORDER BY q.name`)
	sqlUsages = sqlgen.Register("query.Usages", `SELECT d.dependent_kind, d.dependent_id, d.confidence,
			coalesce((SELECT name FROM _sys_queries WHERE id = d.dependent_id), (SELECT name FROM _sys_forms WHERE id = d.dependent_id),
			         (SELECT name FROM _sys_reports WHERE id = d.dependent_id), (SELECT name FROM _sys_scripts WHERE id = d.dependent_id), d.dependent_id)
			FROM _sys_dependencies d WHERE d.target_kind = ? AND d.target_id = ? ORDER BY 1, 4`)
	sqlRefreshCompiled  = sqlgen.Register("query.RefreshCompiled", `SELECT value FROM _sys_meta WHERE key = 'query_compiler'`)
	sqlRefreshCompiled2 = sqlgen.Register("query.RefreshCompiled#2", `INSERT INTO _sys_meta(key, value) VALUES ('query_compiler', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	sqlPGDescribe    = sqlgen.RegisterPostgresOnly("query.pgDescribe", `SELECT current_schema()`)
	sqlPGOriginTypes = sqlgen.RegisterPostgresOnly("query.pgOriginTypes", `SELECT c.oid::bigint, a.attnum::bigint, c.relname::text, a.attname::text
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.oid = ANY(CAST(? AS oid[])) AND c.relnamespace = current_schema()::regnamespace AND a.attnum > 0`)
)
