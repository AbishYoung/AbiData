package catalog

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

// ImportReport records the outcome of importing a workspace across dialects (ADR 0012 §8).
type ImportReport struct {
	SourceDialect string        `json:"source_dialect"`
	TargetDialect string        `json:"target_dialect"`
	Tables        int           `json:"tables"`
	Rows          int64         `json:"rows"`
	Flagged       []ImportIssue `json:"flagged,omitempty"`
}

// ImportIssue is an object translated with a warning or disabled upon import.
type ImportIssue struct {
	Kind    string `json:"kind"` // "query", "script", "expression", "column"
	Name    string `json:"name"`
	Message string `json:"message"`
}

// ExportPostgresToSQLite exports all tables and metadata from a PostgreSQL
// workspace schema into an open SQLite staging database (ADR 0012 §8).
// sc must already have had catalog.Migrate run on it so all system tables exist.
func ExportPostgresToSQLite(ctx context.Context, q dbx.Querier, dialect sqlgen.Dialect, sc *sqlitex.Conn, wsID ids.WorkspaceID, schema string) error {
	if _, err := sc.Exec(ctx, sqlInterchangeFKOff.SQL); err != nil {
		return err
	}
	defer func() { _, _ = sc.Exec(context.Background(), sqlInterchangeFKOn.SQL) }()

	// 1. Copy _sys_* metadata tables from PostgreSQL to SQLite.
	// System tables share column types across backends (text timestamps, integer flags).
	rows, err := sc.Query(ctx, sqlInterchangeListSysTables.SQL)
	if err != nil {
		return err
	}
	var sysTables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		sysTables = append(sysTables, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, tbl := range sysTables {
		// Check if tbl exists in PostgreSQL schema
		var exists bool
		if err := q.QueryRow(ctx, sqlInterchangePGTableExists.SQL, schema, tbl).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		colRows, err := sc.Query(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, Quote(tbl)))
		if err != nil {
			return err
		}
		var cols []string
		for colRows.Next() {
			var cid int
			var cname, ctype string
			var notnull, pk int
			var dflt any
			if err := colRows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
				colRows.Close()
				return err
			}
			cols = append(cols, cname)
		}
		if err := colRows.Err(); err != nil {
			return err
		}
		if len(cols) == 0 {
			continue
		}

		if tbl == "_sys_meta" {
			if _, err := sc.Exec(ctx, sqlInterchangeClearSysMeta.SQL); err != nil {
				return err
			}
		}

		var quotedCols []string
		var placeholders []string
		for _, c := range cols {
			quotedCols = append(quotedCols, Quote(c))
			placeholders = append(placeholders, "?")
		}

		selectSQL := fmt.Sprintf(`SELECT %s FROM %s.%s`, strings.Join(quotedCols, ", "), Quote(schema), Quote(tbl))
		pRows, err := q.Query(ctx, selectSQL)
		if err != nil {
			return fmt.Errorf("export pg table %s: %w", tbl, err)
		}
		insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, Quote(tbl), strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))
		for pRows.Next() {
			vals := pRows.Values()
			if _, err := sc.Exec(ctx, insertSQL, vals...); err != nil {
				pRows.Close()
				return fmt.Errorf("insert sqlite table %s: %w", tbl, err)
			}
		}
		if err := pRows.Err(); err != nil {
			return err
		}
	}

	// 2. Load catalog schema and export user tables.
	sch, err := Load(ctx, q)
	if err != nil {
		return fmt.Errorf("load catalog from pg: %w", err)
	}

	exportedTables := map[string]bool{}
	for _, t := range sch.Tables {
		exportedTables[t.PhysicalName] = true
		ddl, err := CreateTableSQL(sqlgen.SQLite, sch, &t, t.PhysicalName)
		if err != nil {
			return fmt.Errorf("create table sql for %s: %w", t.PhysicalName, err)
		}
		if _, err := sc.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("exec create table %s: %w", t.PhysicalName, err)
		}

		// Physical columns to read
		type colInfo struct {
			name string
			typ  FieldType
		}
		var cols []colInfo
		cols = append(cols, colInfo{name: "id", typ: Autonumber})
		cols = append(cols, colInfo{name: "_version", typ: Integer})
		for _, c := range t.Columns {
			if c.Type == Autonumber || c.Type == Calculated {
				continue
			}
			cols = append(cols, colInfo{name: c.PhysicalName, typ: c.Type})
		}

		var qcols []string
		var phs []string
		for _, c := range cols {
			qcols = append(qcols, Quote(c.name))
			phs = append(phs, "?")
		}

		selectSQL := fmt.Sprintf(`SELECT %s FROM %s.%s ORDER BY "id"`, strings.Join(qcols, ", "), Quote(schema), Quote(t.PhysicalName))
		pRows, err := q.Query(ctx, selectSQL)
		if err != nil {
			return fmt.Errorf("query user table %s: %w", t.PhysicalName, err)
		}
		insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, Quote(t.PhysicalName), strings.Join(qcols, ", "), strings.Join(phs, ", "))
		for pRows.Next() {
			rawVals := pRows.Values()
			vals := make([]any, len(rawVals))
			for i, v := range rawVals {
				vals[i] = StorageForm(cols[i].typ, v)
			}
			if _, err := sc.Exec(ctx, insertSQL, vals...); err != nil {
				pRows.Close()
				return fmt.Errorf("insert sqlite user table %s: %w", t.PhysicalName, err)
			}
		}
		if err := pRows.Err(); err != nil {
			return err
		}
	}

	// 3. Any raw user tables in PostgreSQL (e.g. created outside the catalog in tests)
	pgRawRows, err := q.Query(ctx, sqlInterchangePGRawTables.SQL, schema)
	if err != nil {
		return err
	}
	var rawTables []string
	for pgRawRows.Next() {
		var name string
		if err := pgRawRows.Scan(&name); err != nil {
			pgRawRows.Close()
			return err
		}
		if !exportedTables[name] {
			rawTables = append(rawTables, name)
		}
	}
	if err := pgRawRows.Err(); err != nil {
		return err
	}

	for _, tbl := range rawTables {
		cRows, err := q.Query(ctx, sqlInterchangePGColumns.SQL, schema, tbl)
		if err != nil {
			return err
		}
		var colDefs []string
		var cnames []string
		var phs []string
		for cRows.Next() {
			var col, typ string
			if err := cRows.Scan(&col, &typ); err != nil {
				cRows.Close()
				return err
			}
			sqlType := "TEXT"
			switch strings.ToLower(typ) {
			case "bigint", "integer", "smallint":
				sqlType = "INTEGER"
			case "double precision", "real", "numeric":
				sqlType = "REAL"
			case "boolean":
				sqlType = "INTEGER"
			}
			colDefs = append(colDefs, fmt.Sprintf("%s %s", Quote(col), sqlType))
			cnames = append(cnames, Quote(col))
			phs = append(phs, "?")
		}
		if err := cRows.Err(); err != nil {
			return err
		}
		if len(colDefs) == 0 {
			continue
		}
		createSQL := fmt.Sprintf(`CREATE TABLE %s (%s)`, Quote(tbl), strings.Join(colDefs, ", "))
		if _, err := sc.Exec(ctx, createSQL); err != nil {
			return fmt.Errorf("create raw table %s: %w", tbl, err)
		}
		selSQL := fmt.Sprintf(`SELECT %s FROM %s.%s`, strings.Join(cnames, ", "), Quote(schema), Quote(tbl))
		rRows, err := q.Query(ctx, selSQL)
		if err != nil {
			return err
		}
		insSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, Quote(tbl), strings.Join(cnames, ", "), strings.Join(phs, ", "))
		for rRows.Next() {
			vals := rRows.Values()
			conv := make([]any, len(vals))
			for i, v := range vals {
				conv[i] = StorageForm("", v)
			}
			if _, err := sc.Exec(ctx, insSQL, conv...); err != nil {
				rRows.Close()
				return err
			}
		}
		if err := rRows.Err(); err != nil {
			return err
		}
	}

	// 4. Create indexes, OCC triggers, and attachment triggers in SQLite
	for _, t := range sch.Tables {
		for _, x := range t.Indexes {
			idxSQL, err := CreateIndexSQL(sqlgen.SQLite, &t, x)
			if err != nil {
				return err
			}
			if _, err := sc.Exec(ctx, idxSQL); err != nil {
				return fmt.Errorf("create index %s: %w", x.PhysicalName, err)
			}
		}
		occStmt := OCCTrigger(&t)
		occSQL, err := Render(sqlgen.SQLite, occStmt)
		if err != nil {
			return err
		}
		if _, err := sc.Exec(ctx, occSQL); err != nil {
			return fmt.Errorf("create occ trigger on %s: %w", t.PhysicalName, err)
		}
		if HasAttachTriggers(&t) {
			for _, trig := range AttachTriggers(&t) {
				trigSQL, err := Render(sqlgen.SQLite, trig)
				if err != nil {
					return err
				}
				if _, err := sc.Exec(ctx, trigSQL); err != nil {
					return fmt.Errorf("create attach trigger on %s: %w", t.PhysicalName, err)
				}
			}
		}
	}

	return nil
}

// ImportSQLiteToPostgres imports a SQLite workspace database into targetSchema
// of PostgreSQL (ADR 0012 §8).
func ImportSQLiteToPostgres(ctx context.Context, sc *sqlitex.Conn, d dbx.Execer, dialect sqlgen.Dialect, wsID ids.WorkspaceID, n pgdb.Names, targetSchema string) (*ImportReport, error) {
	sIdent := pgdb.Ident(targetSchema)
	if _, err := d.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+sIdent); err != nil {
		return nil, fmt.Errorf("create schema %s: %w", targetSchema, err)
	}
	if _, err := d.Exec(ctx, `GRANT USAGE ON SCHEMA `+sIdent+` TO `+pgdb.Ident(n.ReadRole())); err != nil {
		return nil, fmt.Errorf("grant usage on schema %s: %w", targetSchema, err)
	}
	if _, err := d.Exec(ctx, `SET LOCAL search_path = `+sIdent+`, `+pgdb.Ident(n.Lib())+`, pg_catalog`); err != nil {
		return nil, fmt.Errorf("set search_path: %w", err)
	}

	if err := MigratePostgresInSchema(ctx, wsID, d, n, targetSchema); err != nil {
		return nil, fmt.Errorf("migrate target schema %s: %w", targetSchema, err)
	}

	report := &ImportReport{
		SourceDialect: "sqlite",
		TargetDialect: dialect.String(),
	}

	// 1. Copy _sys_* tables from SQLite to PostgreSQL.
	sysRows, err := sc.Query(ctx, sqlInterchangeListSysTables.SQL)
	if err != nil {
		return nil, err
	}
	var sysTables []string
	for sysRows.Next() {
		var name string
		if err := sysRows.Scan(&name); err != nil {
			sysRows.Close()
			return nil, err
		}
		sysTables = append(sysTables, name)
	}
	if err := sysRows.Err(); err != nil {
		return nil, err
	}
	sortSysTables(sysTables)

	for _, tbl := range sysTables {
		if tbl == "_sys_meta" {
			if _, err := d.Exec(ctx, sqlInterchangeClearSysMeta.SQL); err != nil {
				return nil, err
			}
		}

		cRows, err := sc.Query(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, Quote(tbl)))
		if err != nil {
			return nil, err
		}
		var cols []string
		for cRows.Next() {
			var cid int
			var cname, ctype string
			var notnull, pk int
			var dflt any
			if err := cRows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
				cRows.Close()
				return nil, err
			}
			cols = append(cols, cname)
		}
		if err := cRows.Err(); err != nil {
			return nil, err
		}
		if len(cols) == 0 {
			continue
		}

		var qcols []string
		var phs []string
		for _, c := range cols {
			qcols = append(qcols, Quote(c))
			phs = append(phs, "?")
		}

		selSQL := fmt.Sprintf(`SELECT %s FROM %s`, strings.Join(qcols, ", "), Quote(tbl))
		sRows, err := sc.Query(ctx, selSQL)
		if err != nil {
			return nil, fmt.Errorf("read sqlite table %s: %w", tbl, err)
		}
		insSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, Quote(tbl), strings.Join(qcols, ", "), strings.Join(phs, ", "))
		for sRows.Next() {
			vals := sRows.Values()
			if _, err := d.Exec(ctx, insSQL, vals...); err != nil {
				sRows.Close()
				return nil, fmt.Errorf("insert pg table %s: %w", tbl, err)
			}
		}
		if err := sRows.Err(); err != nil {
			return nil, err
		}

		// Advance sequence for identity tables if applicable
		if tbl == "_sys_audit" || tbl == "_sys_row_history" {
			seqSQL := fmt.Sprintf(`DO $g$ DECLARE s text := pg_get_serial_sequence('%s', 'id'); m bigint;
BEGIN IF s IS NOT NULL THEN
	EXECUTE 'SELECT max(id) FROM %s' INTO m;
	IF m IS NOT NULL THEN PERFORM setval(s, m, true);
	ELSE PERFORM setval(s, 1, false); END IF;
END IF; END $g$;`, tbl, pgdb.Ident(tbl))
			if _, err := d.Exec(ctx, seqSQL); err != nil {
				return nil, fmt.Errorf("reset sequence for %s: %w", tbl, err)
			}
		}
	}

	// 2. Load catalog schema from SQLite.
	sch, err := Load(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("load catalog from sqlite: %w", err)
	}

	importedTables := map[string]bool{}

	// Create user tables without foreign keys first so creation order doesn't fail.
	for _, t := range sch.Tables {
		importedTables[t.PhysicalName] = true
		ddl, err := CreateTableSQLWithFK(dialect, sch, &t, t.PhysicalName, false)
		if err != nil {
			return nil, fmt.Errorf("create table sql for %s: %w", t.PhysicalName, err)
		}
		if _, err := d.Exec(ctx, ddl); err != nil {
			return nil, fmt.Errorf("exec create table %s: %w", t.PhysicalName, err)
		}

		type colInfo struct {
			name string
			typ  FieldType
		}
		var cols []colInfo
		cols = append(cols, colInfo{name: "id", typ: Autonumber})
		cols = append(cols, colInfo{name: "_version", typ: Integer})
		for _, c := range t.Columns {
			if c.Type == Autonumber || c.Type == Calculated {
				continue
			}
			cols = append(cols, colInfo{name: c.PhysicalName, typ: c.Type})
		}

		var qcols []string
		var phs []string
		for _, c := range cols {
			qcols = append(qcols, Quote(c.name))
			phs = append(phs, "?")
		}

		selSQL := fmt.Sprintf(`SELECT %s FROM %s ORDER BY "id"`, strings.Join(qcols, ", "), Quote(t.PhysicalName))
		sRows, err := sc.Query(ctx, selSQL)
		if err != nil {
			return nil, fmt.Errorf("query sqlite user table %s: %w", t.PhysicalName, err)
		}
		insSQL := fmt.Sprintf(`INSERT INTO %s (%s) OVERRIDING SYSTEM VALUE VALUES (%s)`, Quote(t.PhysicalName), strings.Join(qcols, ", "), strings.Join(phs, ", "))
		var rowCount int64
		for sRows.Next() {
			rawVals := sRows.Values()
			vals := make([]any, len(rawVals))
			for i, v := range rawVals {
				if cols[i].typ == YesNo {
					if n, ok := v.(int64); ok {
						vals[i] = (n != 0)
					} else {
						vals[i] = false
					}
				} else {
					vals[i] = v
				}
			}
			if _, err := d.Exec(ctx, insSQL, vals...); err != nil {
				sRows.Close()
				return nil, fmt.Errorf("insert pg user table %s: %w", t.PhysicalName, err)
			}
			rowCount++
		}
		if err := sRows.Err(); err != nil {
			return nil, err
		}
		report.Tables++
		report.Rows += rowCount

		// Advance identity sequence
		seqSQL := fmt.Sprintf(`DO $g$ DECLARE s text := pg_get_serial_sequence('%s', 'id'); m bigint;
BEGIN IF s IS NOT NULL THEN
	EXECUTE 'SELECT max(id) FROM %s' INTO m;
	IF m IS NOT NULL THEN PERFORM setval(s, m, true);
	ELSE PERFORM setval(s, 1, false); END IF;
END IF; END $g$;`, Quote(t.PhysicalName), pgdb.Ident(t.PhysicalName))
		if _, err := d.Exec(ctx, seqSQL); err != nil {
			return nil, fmt.Errorf("advance sequence for %s: %w", t.PhysicalName, err)
		}
	}

	// 3. Add foreign keys now that all tables and rows are in place.
	for _, r := range sch.Relationships {
		if !r.Enforce {
			continue
		}
		child, ok := sch.Table(r.ChildTableID)
		if !ok {
			continue
		}
		parent, ok := sch.Table(r.ParentTableID)
		if !ok {
			continue
		}
		col, ok := child.Column(r.ChildColumnID)
		if !ok {
			continue
		}
		fk := sqlgen.ForeignKey{Name: fkName(r.ID), Column: col.PhysicalName, RefTable: parent.PhysicalName, OnDelete: sqlgen.RefAction(r.OnDelete)}
		stmt := &sqlgen.AddForeignKey{Table: child.PhysicalName, FK: fk}
		fkSQL, err := Render(dialect, stmt)
		if err != nil {
			return nil, err
		}
		if _, err := d.Exec(ctx, fkSQL); err != nil {
			return nil, fmt.Errorf("add foreign key %s: %w", r.Name, err)
		}
	}

	// 4. Create indexes, OCC triggers, attachment triggers, and grant permissions
	for _, t := range sch.Tables {
		occStmt := OCCTrigger(&t)
		occSQL, err := Render(dialect, occStmt)
		if err != nil {
			return nil, err
		}
		if _, err := d.Exec(ctx, occSQL); err != nil {
			return nil, fmt.Errorf("create occ trigger %s: %w", t.PhysicalName, err)
		}

		if HasAttachTriggers(&t) {
			for _, trig := range AttachTriggers(&t) {
				trigSQL, err := Render(dialect, trig)
				if err != nil {
					return nil, err
				}
				if _, err := d.Exec(ctx, trigSQL); err != nil {
					return nil, fmt.Errorf("create attach trigger on %s: %w", t.PhysicalName, err)
				}
			}
		}

		for _, x := range t.Indexes {
			idxSQL, err := CreateIndexSQL(dialect, &t, x)
			if err != nil {
				return nil, err
			}
			if _, err := d.Exec(ctx, idxSQL); err != nil {
				return nil, fmt.Errorf("create index %s: %w", x.PhysicalName, err)
			}
		}

		if err := grantTableInSchema(ctx, d, targetSchema, n, t.PhysicalName); err != nil {
			return nil, err
		}
	}

	// 5. Raw user tables from SQLite
	sqRawRows, err := sc.Query(ctx, sqlInterchangeSQLiteRawTables.SQL)
	if err != nil {
		return nil, err
	}
	var rawTables []string
	for sqRawRows.Next() {
		var name string
		if err := sqRawRows.Scan(&name); err != nil {
			sqRawRows.Close()
			return nil, err
		}
		if !importedTables[name] {
			rawTables = append(rawTables, name)
		}
	}
	if err := sqRawRows.Err(); err != nil {
		return nil, err
	}

	for _, tbl := range rawTables {
		cRows, err := sc.Query(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, Quote(tbl)))
		if err != nil {
			return nil, err
		}
		var colDefs []string
		var cnames []string
		var phs []string
		for cRows.Next() {
			var cid int
			var col, typ string
			var notnull, pk int
			var dflt any
			if err := cRows.Scan(&cid, &col, &typ, &notnull, &dflt, &pk); err != nil {
				cRows.Close()
				return nil, err
			}
			sqlType := "text"
			switch strings.ToUpper(typ) {
			case "INTEGER", "INT":
				sqlType = "bigint"
			case "REAL", "FLOAT", "DOUBLE":
				sqlType = "double precision"
			}
			colDefs = append(colDefs, fmt.Sprintf("%s %s", Quote(col), sqlType))
			cnames = append(cnames, Quote(col))
			phs = append(phs, "?")
		}
		if err := cRows.Err(); err != nil {
			return nil, err
		}
		if len(colDefs) == 0 {
			continue
		}
		createSQL := fmt.Sprintf(`CREATE TABLE %s (%s)`, Quote(tbl), strings.Join(colDefs, ", "))
		if _, err := d.Exec(ctx, createSQL); err != nil {
			return nil, fmt.Errorf("create raw table %s in pg: %w", tbl, err)
		}

		selSQL := fmt.Sprintf(`SELECT %s FROM %s`, strings.Join(cnames, ", "), Quote(tbl))
		sRows, err := sc.Query(ctx, selSQL)
		if err != nil {
			return nil, err
		}
		insSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, Quote(tbl), strings.Join(cnames, ", "), strings.Join(phs, ", "))
		for sRows.Next() {
			vals := sRows.Values()
			if _, err := d.Exec(ctx, insSQL, vals...); err != nil {
				sRows.Close()
				return nil, err
			}
		}
		if err := sRows.Err(); err != nil {
			return nil, err
		}
		if err := grantTableInSchema(ctx, d, targetSchema, n, tbl); err != nil {
			return nil, err
		}
	}

	return report, nil
}

func grantTableInSchema(ctx context.Context, d dbx.Execer, schema string, n pgdb.Names, table string) error {
	t := pgdb.Ident(schema) + "." + Quote(table)
	grantSeqSQL := fmt.Sprintf(`DO $g$
DECLARE
	seq text;
BEGIN
	FOR seq IN
		SELECT pg_get_serial_sequence(quote_ident(table_schema) || '.' || quote_ident(table_name), column_name)
		FROM information_schema.columns
		WHERE table_schema = '%s' AND table_name = '%s'
	LOOP
		IF seq IS NOT NULL THEN
			EXECUTE format('GRANT USAGE ON SEQUENCE %%s TO %%I', seq, '%s');
		END IF;
	END LOOP;
END $g$;`, strings.ReplaceAll(schema, "'", "''"), strings.ReplaceAll(table, "'", "''"), strings.ReplaceAll(n.WriteRole(), "'", "''"))

	qs := []string{
		`GRANT SELECT ON ` + t + ` TO ` + pgdb.Ident(n.ReadRole()),
		`GRANT INSERT, UPDATE, DELETE ON ` + t + ` TO ` + pgdb.Ident(n.WriteRole()),
		grantSeqSQL,
	}
	for _, q := range qs {
		if _, err := d.Exec(ctx, q); err != nil {
			return fmt.Errorf("grant %s: %w", table, err)
		}
	}
	return nil
}

// AdoptPostgres provisions workspace id and imports SQLite archive src into its schema.
func AdoptPostgres(ctx context.Context, pool *pgxpool.Pool, database, prefix string, dialect sqlgen.Dialect, id ids.WorkspaceID, src string) error {
	_, err := AdoptPostgresReport(ctx, pool, database, prefix, dialect, id, src)
	return err
}

// AdoptPostgresReport is AdoptPostgres, returning the import report (§8).
func AdoptPostgresReport(ctx context.Context, pool *pgxpool.Pool, database, prefix string, dialect sqlgen.Dialect, id ids.WorkspaceID, src string) (*ImportReport, error) {
	if !ids.Valid(string(id)) {
		return nil, ids.ErrInvalidID
	}
	n := pgdb.Names{Prefix: prefix, Workspace: string(id)}
	d := pgdb.Pool(pool)
	var exists bool
	if err := d.QueryRow(ctx, sqlInterchangeSchemaExists.SQL, n.Schema()).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("workspace %s: %w", id, fs.ErrExist)
	}

	sc, err := sqlitex.Open(ctx, src, sqlitex.Options{Kind: sqlitex.Snapshot})
	if err != nil {
		return nil, err
	}
	defer func() { _ = sc.Close() }()

	var report *ImportReport
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		dtx := pgdb.WrapTx(tx)
		if err := pgdb.ProvisionWorkspace(ctx, dtx, database, n); err != nil {
			return err
		}
		var err error
		report, err = ImportSQLiteToPostgres(ctx, sc, dtx, dialect, id, n, n.Schema())
		return err
	})
	if err != nil {
		_ = pgdb.DropWorkspace(ctx, d, n)
		return nil, fmt.Errorf("workspace %s: adopt pg: %w", id, err)
	}
	_ = os.Remove(src)
	return report, nil
}

func sortSysTables(tables []string) {
	order := map[string]int{
		"_sys_schema":        1,
		"_sys_meta":          2,
		"_sys_tables":        3,
		"_sys_relationships": 4,
		"_sys_columns":       5,
		"_sys_indexes":       6,
		"_sys_scripts":       7,
		"_sys_jobs":          8,
		"_sys_job_runs":      9,
		"_sys_files":         10, // before _sys_file_refs, which references it
	}
	sort.Slice(tables, func(i, j int) bool {
		oi, oki := order[tables[i]]
		oj, okj := order[tables[j]]
		if !oki {
			oi = 100
		}
		if !okj {
			oj = 100
		}
		if oi != oj {
			return oi < oj
		}
		return tables[i] < tables[j]
	})
}
