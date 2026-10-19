package catalog

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlPlannerPhysicalNameTaken = sqlgen.RegisterSQLiteOnly("catalog.planner.physicalNameTaken", `SELECT count(*) FROM sqlite_schema WHERE name = ? COLLATE NOCASE`).
					WithPostgres(`SELECT count(*) FROM pg_class WHERE relnamespace = current_schema()::regnamespace AND lower(relname::text) = lower(?)`)
	sqlPlannerCreateTable = sqlgen.Register("catalog.planner.createTable", `INSERT INTO _sys_tables(id, physical_name, display_name, description, track_history, version, created_at, updated_at)
		VALUES (?,?,?,?,?,1,?,?)`)
	sqlPlannerRenameTable       = sqlgen.Register("catalog.planner.renameTable", `UPDATE _sys_tables SET display_name = ?, physical_name = ? WHERE id = ?`)
	sqlPlannerDropTable         = sqlgen.Register("catalog.planner.dropTable", `DELETE FROM _sys_file_refs WHERE table_id = ?`)
	sqlPlannerDropTable2        = sqlgen.Register("catalog.planner.dropTable#2", `DELETE FROM _sys_row_history WHERE table_id = ?`)
	sqlPlannerSetTableProps     = sqlgen.Register("catalog.planner.setTableProps", `UPDATE _sys_tables SET description = ?, track_history = ? WHERE id = ?`)
	sqlPlannerAlterColumn       = sqlgen.Register("catalog.planner.alterColumn", `DELETE FROM _sys_relationships WHERE id = ?`)
	sqlPlannerDropColumn        = sqlgen.Register("catalog.planner.dropColumn", `DELETE FROM _sys_indexes WHERE id = ?`)
	sqlPlannerDropColumn2       = sqlgen.Register("catalog.planner.dropColumn#2", `UPDATE _sys_columns SET lookup_display_column_id = NULL WHERE id = ?`)
	sqlPlannerDropColumn3       = sqlgen.Register("catalog.planner.dropColumn#3", `DELETE FROM _sys_relationships WHERE child_column_id = ?`)
	sqlPlannerDropColumn4       = sqlgen.Register("catalog.planner.dropColumn#4", `DELETE FROM _sys_file_refs WHERE column_id = ?`)
	sqlPlannerDropColumn5       = sqlgen.Register("catalog.planner.dropColumn#5", `DELETE FROM _sys_columns WHERE id = ?`)
	sqlPlannerReorderColumns    = sqlgen.Register("catalog.planner.reorderColumns", `UPDATE _sys_columns SET ordinal = ? WHERE id = ?`)
	sqlPlannerCreateIndex       = sqlgen.Register("catalog.planner.createIndex", `INSERT INTO _sys_indexes(id, table_id, physical_name, is_unique, column_ids) VALUES (?,?,?,?,?)`)
	sqlPlannerRebuild           = sqlgen.RegisterSQLiteOnly("catalog.planner.rebuild", `SELECT seq FROM sqlite_sequence WHERE name = ?`)
	sqlPlannerRebuild2          = sqlgen.RegisterSQLiteOnly("catalog.planner.rebuild#2", `UPDATE sqlite_sequence SET seq = max(seq, ?) WHERE name = ?`)
	sqlPlannerRebuild3          = sqlgen.RegisterSQLiteOnly("catalog.planner.rebuild#3", `INSERT INTO sqlite_sequence(name, seq) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = ?)`)
	sqlPlannerImpact            = sqlgen.RegisterSQLiteOnly("catalog.planner.impact", `SELECT "table", parent, count(*) FROM pragma_foreign_key_check GROUP BY 1, 2`)
	sqlPlannerCheckSavedQueries = sqlgen.Register("catalog.planner.checkSavedQueries", `SELECT id, name, sql_text FROM _sys_queries ORDER BY name`)
	sqlPlannerFinish            = sqlgen.Register("catalog.planner.finish", `UPDATE _sys_tables SET version = version + 1, updated_at = ? WHERE id = ?`)
	sqlPlannerFinish2           = sqlgen.Register("catalog.planner.finish#2", `UPDATE _sys_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'schema_version'`)
	sqlPlannerFinish3           = sqlgen.Register("catalog.planner.finish#3", `INSERT INTO _sys_audit(at, user_id, action, object_kind, object_id, details) VALUES (?, ?, 'schema.apply', 'schema', NULL, ?)`)
	sqlPlannerInsertColumnMeta  = sqlgen.Register("catalog.planner.insertColumnMeta", `INSERT INTO _sys_columns(id, table_id, physical_name, display_name, field_type, type_options, is_required,
		default_sql, validation_sql, validation_text, lookup_relationship_id, lookup_display_column_id, description, ordinal, is_hidden)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	sqlPlannerUpdateColumnMeta = sqlgen.Register("catalog.planner.updateColumnMeta", `UPDATE _sys_columns SET physical_name = ?, display_name = ?, field_type = ?, type_options = ?, is_required = ?,
		default_sql = ?, validation_sql = ?, validation_text = ?, lookup_relationship_id = ?, lookup_display_column_id = ?,
		description = ?, ordinal = ?, is_hidden = ? WHERE id = ?`)
	sqlPlannerInsertRelationship = sqlgen.Register("catalog.planner.insertRelationship", `INSERT INTO _sys_relationships(id, name, parent_table_id, child_table_id, child_column_id, on_delete, enforce)
		VALUES (?,?,?,?,?,?,?)`)
	sqlPlannerInsertOrUpdateRelationship = sqlgen.Register("catalog.planner.insertOrUpdateRelationship", `INSERT INTO _sys_relationships(id, name, parent_table_id, child_table_id, child_column_id, on_delete, enforce)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET on_delete = excluded.on_delete, enforce = excluded.enforce`)
	sqlPlannerPGRebuild     = sqlgen.RegisterPostgresOnly("catalog.planner.pgRebuild", `SELECT pg_sequence_last_value(pg_get_serial_sequence(?, 'id')::regclass)`)
	sqlPlannerPGRebuild2    = sqlgen.RegisterPostgresOnly("catalog.planner.pgRebuild#2", `SELECT setval(pg_get_serial_sequence(?, 'id'), ?)`)
	sqlPlannerPGCopy        = sqlgen.RegisterPostgresOnly("catalog.planner.pgCopy", `SELECT copied, skipped, sample FROM abi_copy_rows(?, ?)`)
	sqlPlannerPGForeignKeys = sqlgen.RegisterPostgresOnly("catalog.planner.pgCheckForeignKeys", `SELECT NOT convalidated FROM pg_constraint WHERE conrelid = to_regclass(?) AND conname = ?`)
	sqlCacheGet             = sqlgen.Register("catalog.Cache.Get", `SELECT value FROM _sys_meta WHERE key = 'schema_version'`)
	sqlLoad                 = sqlgen.RegisterSQLiteOnly("catalog.Load", `SELECT id, physical_name, display_name, description, track_history, version
		FROM _sys_tables ORDER BY display_name COLLATE NOCASE, id`).
		WithPostgres(`SELECT id, physical_name, display_name, description, track_history, version
		FROM _sys_tables ORDER BY display_name COLLATE abi_ci, id`)
	sqlLoad2 = sqlgen.Register("catalog.Load#2", `SELECT id, table_id, physical_name, display_name, field_type, type_options, is_required,
		default_sql, validation_sql, validation_text, lookup_relationship_id, lookup_display_column_id, description,
		ordinal, is_hidden FROM _sys_columns ORDER BY table_id, ordinal, id`)
	sqlLoad3 = sqlgen.Register("catalog.Load#3", `SELECT id, table_id, physical_name, is_unique, column_ids FROM _sys_indexes ORDER BY physical_name`)
	sqlLoad4 = sqlgen.RegisterSQLiteOnly("catalog.Load#4", `SELECT id, name, parent_table_id, child_table_id, child_column_id, on_delete, enforce
		FROM _sys_relationships ORDER BY name COLLATE NOCASE`).
		WithPostgres(`SELECT id, name, parent_table_id, child_table_id, child_column_id, on_delete, enforce
		FROM _sys_relationships ORDER BY name, id`)
	sqlMigrate   = sqlgen.RegisterSQLiteOnly("catalog.Migrate", "PRAGMA application_id")
	sqlMigrate2  = sqlgen.RegisterSQLiteOnly("catalog.Migrate#2", "PRAGMA user_version")
	sqlMigrate3  = sqlgen.Register("catalog.Migrate#3", `INSERT INTO _sys_meta(key, value) VALUES (?, ?) ON CONFLICT DO NOTHING`)
	sqlReconcile = sqlgen.RegisterSQLiteOnly("catalog.Reconcile", `SELECT name, hidden FROM pragma_table_xinfo(?)`).
			WithPostgres(`SELECT column_name AS name, CASE WHEN is_generated = 'ALWAYS' THEN 2 ELSE 0 END AS hidden
		FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ?`)
	sqlReconcile2 = sqlgen.RegisterSQLiteOnly("catalog.Reconcile#2", `SELECT count(*) FROM sqlite_schema WHERE type = 'trigger' AND name = ?`).
			WithPostgres(`SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid
		WHERE c.relnamespace = current_schema()::regnamespace AND t.tgname = ?`)
	sqlReconcile3 = sqlgen.RegisterSQLiteOnly("catalog.Reconcile#3", `SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = ?`).
			WithPostgres(`SELECT count(*) FROM pg_class WHERE relkind = 'i' AND relnamespace = current_schema()::regnamespace AND relname = ?`)
	sqlReconcile4 = sqlgen.RegisterSQLiteOnly("catalog.Reconcile#4", `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		AND name NOT LIKE '\_sys\_%' ESCAPE '\'`).
		WithPostgres(`SELECT relname AS name FROM pg_class WHERE relkind IN ('r', 'p') AND relnamespace = current_schema()::regnamespace
		AND relname NOT LIKE '\_sys\_%' ESCAPE '\'`)
)
