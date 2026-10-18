package sqlgen

import "fmt"

// ddl renders a schema statement; it reports false for any other statement.
func (r *renderer) ddl(s Stmt) bool {
	switch s := s.(type) {
	case *CreateTable:
		r.createTable(s)
	case *CreateIndex:
		r.createIndex(s)
	case *AddColumn:
		if s.Table == "" {
			r.fail("ADD COLUMN needs a table")
		}
		r.w("ALTER TABLE " + Quote(s.Table) + " ADD COLUMN ")
		r.columnDef(s.Column)
	case *RenameTable:
		r.need(s.From, s.To)
		r.w("ALTER TABLE " + Quote(s.From) + " RENAME TO " + Quote(s.To))
	case *RenameColumn:
		r.need(s.Table, s.From, s.To)
		r.w("ALTER TABLE " + Quote(s.Table) + " RENAME COLUMN " + Quote(s.From) + " TO " + Quote(s.To))
	case *DropTable:
		r.need(s.Name)
		r.w("DROP TABLE " + ifExists(s.IfExists) + Quote(s.Name))
	case *DropIndex:
		r.need(s.Name)
		r.w("DROP INDEX " + Quote(s.Name))
	case *DropTrigger:
		r.need(s.Name)
		r.w("DROP TRIGGER " + ifExists(s.IfExists) + Quote(s.Name))
		if r.pg() {
			r.need(s.Table)
			r.w(" ON " + Quote(s.Table))
		}
	case *OCCBump:
		r.need(s.Name, s.Table)
		if r.pg() {
			r.w(`CREATE TRIGGER ` + Quote(s.Name) + ` BEFORE UPDATE ON ` + Quote(s.Table) + ` FOR EACH ROW EXECUTE FUNCTION abi_occ_bump()`)
			break
		}
		t := Quote(s.Table)
		r.w(`CREATE TRIGGER ` + Quote(s.Name) + ` AFTER UPDATE ON ` + t + ` FOR EACH ROW WHEN NEW."_version" = OLD."_version" ` +
			`BEGIN UPDATE ` + t + ` SET "_version" = OLD."_version" + 1 WHERE "id" = NEW."id"; END`)
	case *AttachmentRefs:
		r.attachmentRefs(s)
	default:
		return false
	}
	// A schema object outlives the statement that created it, so its defaults
	// and checks are literals: there is nothing to bind a value to.
	if len(r.args) > 0 || len(r.params) > 0 {
		r.fail("a schema statement cannot bind values; use literals")
	}
	return true
}

// need fails when any of the names is empty.
func (r *renderer) need(names ...string) {
	for _, n := range names {
		if n == "" {
			r.fail("a schema statement is missing a name")
			return
		}
	}
}

func ifExists(b bool) string {
	if b {
		return "IF EXISTS "
	}
	return ""
}

func (r *renderer) createTable(s *CreateTable) {
	if s.Name == "" || len(s.Columns) == 0 {
		r.fail("CREATE TABLE needs a name and columns")
		return
	}
	r.w("CREATE TABLE " + Quote(s.Name) + " (\n  ")
	r.list(len(s.Columns), ",\n  ", func(i int) { r.columnDef(s.Columns[i]) })
	for _, fk := range s.ForeignKeys {
		r.need(fk.Name, fk.Column, fk.RefTable)
		switch fk.OnDelete {
		case Restrict, Cascade, SetNull:
		default:
			r.fail("unknown ON DELETE action %q", fk.OnDelete)
		}
		r.w(",\n  CONSTRAINT " + Quote(fk.Name) + " FOREIGN KEY (" + Quote(fk.Column) + ") REFERENCES " +
			Quote(fk.RefTable) + `("id") ON DELETE ` + string(fk.OnDelete))
	}
	r.w("\n)")
	if s.Strict && !r.pg() { // PostgreSQL is always strict
		r.w(" STRICT")
	}
}

func (r *renderer) columnDef(c ColumnDef) {
	if c.Name == "" {
		r.fail("a column needs a name")
		return
	}
	q := Quote(c.Name)
	r.w(q + " ")
	if r.pg() {
		r.pgColumnDef(q, c)
		return
	}
	if c.AutoIncrement {
		r.w("INTEGER PRIMARY KEY AUTOINCREMENT")
		return
	}
	if c.Generated != nil {
		if c.Default != nil || c.NotNull || len(c.Checks) > 0 {
			r.fail("generated column %s takes no default, NOT NULL or checks", c.Name)
		}
		r.w(storageType(c.Type) + " GENERATED ALWAYS AS ")
		r.parenthesised(c.Generated)
		r.w(" VIRTUAL")
		return
	}
	r.w(storageType(c.Type))
	if c.NotNull {
		r.w(" NOT NULL")
	}
	if c.Default != nil {
		r.w(" DEFAULT ")
		if _, ok := c.Default.(Lit); ok {
			r.expr(c.Default)
		} else {
			r.parenthesised(c.Default)
		}
	}
	if dc := domainCheck(q, c); dc != "" {
		if c.TypeCheck == "" {
			r.fail("column %s needs a name for its type check", c.Name)
		}
		r.w(" CONSTRAINT " + Quote(c.TypeCheck) + " CHECK (" + dc + ")")
	}
	for _, ck := range c.Checks {
		r.need(ck.Name)
		r.w(" CONSTRAINT " + Quote(ck.Name) + " CHECK ")
		r.parenthesised(ck.Expr)
	}
}

// parenthesised writes e in parentheses (Raw already brings its own).
func (r *renderer) parenthesised(e Expr) {
	if _, ok := e.(Raw); ok {
		r.expr(e)
		return
	}
	r.w("(")
	r.expr(e)
	r.w(")")
}

// storageType is the SQLite STRICT column type of a logical type.
func storageType(t Type) string {
	switch t {
	case Integer, Currency, Boolean:
		return "INTEGER"
	case Number:
		return "REAL"
	case Text, Date, DateTime, JSON, GeoPoint, GeoArea, Attachments:
		return "TEXT"
	}
	return "ANY"
}

// domainCheck is the CHECK that keeps a column's values in its type's domain
// ("" when the type has none). It uses only SQLite builtins so workspace files
// stay readable by stock sqlite3; the records codecs validate fully.
func domainCheck(q string, c ColumnDef) string {
	switch c.Type {
	case Text:
		switch {
		case c.MaxLen > 0:
			return fmt.Sprintf("length(%s) <= %d", q, c.MaxLen)
		case c.URL:
			return fmt.Sprintf("%s IS NULL OR %s GLOB 'http*://*'", q, q)
		}
	case Boolean:
		return q + " IN (0,1)"
	case Date:
		return fmt.Sprintf("%s IS NULL OR date(%s) IS %s", q, q, q)
	case DateTime:
		return fmt.Sprintf("%s IS NULL OR strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', %s) IS %s", q, q, q)
	case Attachments:
		return fmt.Sprintf("%s IS NULL OR (json_valid(%s) AND json_type(%s) = 'array')", q, q, q)
	case GeoPoint, GeoArea:
		types := "'Point'"
		if c.Type == GeoArea {
			types = "'Polygon','MultiPolygon'"
		}
		return fmt.Sprintf("%s IS NULL OR (json_valid(%s) AND json_extract(%s, '$.type') IN (%s))", q, q, q, types)
	}
	return ""
}

// attachmentRefs renders the SQLite trigger for one row event.
func (r *renderer) attachmentRefs(s *AttachmentRefs) {
	r.need(s.Name, s.Table, s.TableID)
	if len(s.Columns) == 0 && s.Event != OnDelete {
		r.fail("attachment references need at least one column")
		return
	}
	if r.pg() {
		r.pgAttachmentRefs(s)
		return
	}
	tid, err := literal(s.TableID)
	if err != nil {
		r.fail("%v", err)
		return
	}
	t := Quote(s.Table)
	lit := func(v string) string {
		l, err := literal(v)
		if err != nil {
			r.fail("%v", err)
		}
		return l
	}
	insertRefs := func(c AttachmentColumn) string {
		return `INSERT OR IGNORE INTO _sys_file_refs(file_id, table_id, row_id, column_id) ` +
			`SELECT value, ` + tid + `, NEW."id", ` + lit(c.ID) + ` FROM json_each(NEW.` + Quote(c.Name) + `) WHERE type = 'text';`
	}
	switch s.Event {
	case OnInsert:
		r.w(`CREATE TRIGGER ` + Quote(s.Name) + ` AFTER INSERT ON ` + t + ` FOR EACH ROW BEGIN `)
		for _, c := range s.Columns {
			r.w(insertRefs(c) + " ")
		}
		r.w(" ")
	case OnUpdate:
		r.w(`CREATE TRIGGER ` + Quote(s.Name) + ` AFTER UPDATE OF `)
		r.list(len(s.Columns), ", ", func(i int) { r.w(Quote(s.Columns[i].Name)) })
		r.w(` ON ` + t + ` FOR EACH ROW BEGIN `)
		for _, c := range s.Columns {
			r.w(`DELETE FROM _sys_file_refs WHERE table_id = ` + tid + ` AND row_id = OLD."id" AND column_id = ` + lit(c.ID) + ` ` +
				`AND file_id NOT IN (SELECT value FROM json_each(NEW.` + Quote(c.Name) + `) WHERE type = 'text'); `)
			r.w(insertRefs(c) + " ")
		}
		r.w(" ") // the body ends with a space, then " END"
	case OnDelete:
		r.w(`CREATE TRIGGER ` + Quote(s.Name) + ` AFTER DELETE ON ` + t + ` FOR EACH ROW BEGIN ` +
			`DELETE FROM _sys_file_refs WHERE table_id = ` + tid + ` AND row_id = OLD."id"; `)
	default:
		r.fail("unknown trigger event %d", s.Event)
		return
	}
	r.w("END")
}

func (r *renderer) createIndex(s *CreateIndex) {
	r.need(s.Name, s.Table)
	if len(s.Columns) == 0 {
		r.fail("an index needs columns")
		return
	}
	r.w("CREATE ")
	if s.Unique {
		r.w("UNIQUE ")
	}
	r.w("INDEX " + Quote(s.Name) + " ON " + Quote(s.Table) + " (")
	r.list(len(s.Columns), ", ", func(i int) {
		c := s.Columns[i]
		if c.Table != "" {
			r.fail("index column %s cannot name a table", c.Name)
		}
		r.w(Quote(c.Name))
		if c.CI {
			r.w(" COLLATE " + r.ciCollation())
		}
	})
	r.w(")")
}
