package sqlgen

import (
	"strings"
	"testing"
)

// The shape the catalog produces for a table: the autonumber key and row
// version, typed columns with their domain checks, a builder validation rule
// and default (still SQLite SQL, so Raw), a generated column and a foreign key.
func TestRenderCreateTable(t *testing.T) {
	s := &CreateTable{Name: "Customers", Strict: true, Columns: []ColumnDef{
		{Name: "id", AutoIncrement: true, Type: Text, NotNull: true}, // AutoIncrement ignores the rest
		{Name: "_version", Type: Integer, NotNull: true, Default: Lit{Value: 1}},
		{Name: "Name", Type: Text, MaxLen: 40, NotNull: true, TypeCheck: "ty_name"},
		{Name: "Notes", Type: Text, TypeCheck: "ty_notes"}, // no domain check, so the name is unused
		{Name: "Visits", Type: Integer, Checks: []Check{{Name: "ck_visits", Expr: Raw{SQL: "Visits >= 0"}}}},
		{Name: "Balance", Type: Currency, Default: Raw{SQL: "0"}},
		{Name: "Active", Type: Boolean, NotNull: true, Default: Lit{Value: 0}, TypeCheck: "ty_active"},
		{Name: "Label", Generated: Raw{SQL: `Name || ' (' || Visits || ')'`}},
		{Name: "Next", Type: Integer, Generated: Binary{Op: OpAdd, L: Col{Name: "Visits"}, R: Lit{Value: 1}}},
		{Name: "Customer", Type: Integer, Checks: []Check{{Name: "ck_customer", Expr: Binary{Op: OpGt, L: Col{Name: "Customer"}, R: Lit{Value: 0}}}}},
	}, ForeignKeys: []ForeignKey{{Name: "fk_r1", Column: "Customer", RefTable: "Customers", OnDelete: Restrict}}}
	want := `CREATE TABLE "Customers" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "_version" INTEGER NOT NULL DEFAULT 1,
  "Name" TEXT NOT NULL CONSTRAINT "ty_name" CHECK (length("Name") <= 40),
  "Notes" TEXT,
  "Visits" INTEGER CONSTRAINT "ck_visits" CHECK (Visits >= 0),
  "Balance" INTEGER DEFAULT (0),
  "Active" INTEGER NOT NULL DEFAULT 0 CONSTRAINT "ty_active" CHECK ("Active" IN (0,1)),
  "Label" ANY GENERATED ALWAYS AS (Name || ' (' || Visits || ')') VIRTUAL,
  "Next" INTEGER GENERATED ALWAYS AS ("Visits" + 1) VIRTUAL,
  "Customer" INTEGER CONSTRAINT "ck_customer" CHECK ("Customer" > 0),
  CONSTRAINT "fk_r1" FOREIGN KEY ("Customer") REFERENCES "Customers"("id") ON DELETE RESTRICT
) STRICT`
	got := mustRender(t, s)
	if got.SQL != want {
		t.Fatalf("SQL:\n got %s\nwant %s", got.SQL, want)
	}
	if len(got.Args) != 0 || len(got.Params) != 0 || len(got.Columns) != 0 {
		t.Errorf("args %v params %v columns %v, want none", got.Args, got.Params, got.Columns)
	}
	// Without Strict the table is declared as is.
	plain := mustRender(t, &CreateTable{Name: "t", Columns: []ColumnDef{{Name: "x", Type: Number}}})
	if want := "CREATE TABLE \"t\" (\n  \"x\" REAL\n)"; plain.SQL != want {
		t.Errorf("non-strict:\n got %s\nwant %s", plain.SQL, want)
	}
}

// Each logical type has one storage type, and the renderer owns the CHECK
// that keeps values in the type's domain.
func TestRenderLogicalTypes(t *testing.T) {
	cases := []struct {
		c    ColumnDef
		want string
	}{
		{ColumnDef{Type: Text}, `TEXT`},
		{ColumnDef{Type: Text, MaxLen: 10}, `TEXT CONSTRAINT "ty" CHECK (length("c") <= 10)`},
		{ColumnDef{Type: Text, URL: true}, `TEXT CONSTRAINT "ty" CHECK ("c" IS NULL OR "c" GLOB 'http*://*')`},
		{ColumnDef{Type: Integer}, `INTEGER`},
		{ColumnDef{Type: Number}, `REAL`},
		{ColumnDef{Type: Currency}, `INTEGER`},
		{ColumnDef{Type: Boolean}, `INTEGER CONSTRAINT "ty" CHECK ("c" IN (0,1))`},
		{ColumnDef{Type: Date}, `TEXT CONSTRAINT "ty" CHECK ("c" IS NULL OR date("c") IS "c")`},
		{ColumnDef{Type: DateTime}, `TEXT CONSTRAINT "ty" CHECK ("c" IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', "c") IS "c")`},
		{ColumnDef{Type: JSON}, `TEXT`},
		{ColumnDef{Type: GeoPoint}, `TEXT CONSTRAINT "ty" CHECK ("c" IS NULL OR (json_valid("c") AND json_extract("c", '$.type') IN ('Point')))`},
		{ColumnDef{Type: GeoArea}, `TEXT CONSTRAINT "ty" CHECK ("c" IS NULL OR (json_valid("c") AND json_extract("c", '$.type') IN ('Polygon','MultiPolygon')))`},
		{ColumnDef{Type: Attachments}, `TEXT CONSTRAINT "ty" CHECK ("c" IS NULL OR (json_valid("c") AND json_type("c") = 'array'))`},
		{ColumnDef{Type: Unknown}, `ANY`},
		// A generated column takes the storage type but never the domain check.
		{ColumnDef{Type: Boolean, Generated: Raw{SQL: "1"}}, `INTEGER GENERATED ALWAYS AS (1) VIRTUAL`},
	}
	for _, tc := range cases {
		tc.c.Name, tc.c.TypeCheck = "c", "ty"
		got := mustRender(t, &AddColumn{Table: "t", Column: tc.c})
		if want := `ALTER TABLE "t" ADD COLUMN "c" ` + tc.want; got.SQL != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.c.Type, got.SQL, want)
		}
	}
	// The column name is quoted inside the check too.
	got := mustRender(t, &AddColumn{Table: "t", Column: ColumnDef{Name: `we"ird`, Type: Boolean, TypeCheck: "ty"}})
	if want := `ALTER TABLE "t" ADD COLUMN "we""ird" INTEGER CONSTRAINT "ty" CHECK ("we""ird" IN (0,1))`; got.SQL != want {
		t.Errorf("quoted name:\n got %s\nwant %s", got.SQL, want)
	}
}

func TestRenderSchemaStatements(t *testing.T) {
	cases := []struct {
		name string
		s    Stmt
		want string
	}{
		{"unique case-insensitive index",
			&CreateIndex{Name: "ix_c_name", Table: "Customers", Unique: true, Columns: []Col{{Name: "Name", CI: true}, {Name: "Visits"}}},
			`CREATE UNIQUE INDEX "ix_c_name" ON "Customers" ("Name" COLLATE NOCASE, "Visits")`},
		{"index", &CreateIndex{Name: "ix", Table: "t", Columns: []Col{{Name: "a"}}}, `CREATE INDEX "ix" ON "t" ("a")`},
		{"rename table", &RenameTable{From: "a", To: `b"c`}, `ALTER TABLE "a" RENAME TO "b""c"`},
		{"rename column", &RenameColumn{Table: "t", From: "a", To: "b"}, `ALTER TABLE "t" RENAME COLUMN "a" TO "b"`},
		{"drop table", &DropTable{Name: "t"}, `DROP TABLE "t"`},
		{"drop table if exists", &DropTable{Name: "t", IfExists: true}, `DROP TABLE IF EXISTS "t"`},
		{"drop index", &DropIndex{Name: "ix"}, `DROP INDEX "ix"`},
		{"drop trigger", &DropTrigger{Name: "tr"}, `DROP TRIGGER "tr"`},
		{"drop trigger if exists", &DropTrigger{Name: "tr", IfExists: true}, `DROP TRIGGER IF EXISTS "tr"`},
		{"add column with default and check",
			&AddColumn{Table: "t", Column: ColumnDef{Name: "Code", Type: Text, MaxLen: 8, NotNull: true, Default: Raw{SQL: "'x'"}, TypeCheck: "ty_code",
				Checks: []Check{{Name: "ck_code", Expr: Raw{SQL: "length(Code) > 0"}}}}},
			`ALTER TABLE "t" ADD COLUMN "Code" TEXT NOT NULL DEFAULT ('x') CONSTRAINT "ty_code" CHECK (length("Code") <= 8) CONSTRAINT "ck_code" CHECK (length(Code) > 0)`},
	}
	for _, tc := range cases {
		if got := mustRender(t, tc.s); got.SQL != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got.SQL, tc.want)
		}
	}
}

// Trigger intents render to SQLite AFTER triggers. The bodies are compared
// exactly because existing workspaces hold them in sqlite_schema.
func TestRenderTriggerIntents(t *testing.T) {
	occ := mustRender(t, &OCCBump{Name: "_occ_t1", Table: "Orders"})
	if want := `CREATE TRIGGER "_occ_t1" AFTER UPDATE ON "Orders" FOR EACH ROW WHEN NEW."_version" = OLD."_version" ` +
		`BEGIN UPDATE "Orders" SET "_version" = OLD."_version" + 1 WHERE "id" = NEW."id"; END`; occ.SQL != want {
		t.Errorf("OCCBump:\n got %s\nwant %s", occ.SQL, want)
	}

	cols := []AttachmentColumn{{ID: "C1", Name: "Photos"}, {ID: "C2", Name: "Docs"}}
	ins := func(id, col string) string {
		return `INSERT OR IGNORE INTO _sys_file_refs(file_id, table_id, row_id, column_id) SELECT value, 'T1', NEW."id", '` + id +
			`' FROM json_each(NEW."` + col + `") WHERE type = 'text'; `
	}
	del := func(id, col string) string {
		return `DELETE FROM _sys_file_refs WHERE table_id = 'T1' AND row_id = OLD."id" AND column_id = '` + id +
			`' AND file_id NOT IN (SELECT value FROM json_each(NEW."` + col + `") WHERE type = 'text'); `
	}
	cases := []struct {
		event TriggerEvent
		want  string
	}{
		{OnInsert, `CREATE TRIGGER "tr" AFTER INSERT ON "Sites" FOR EACH ROW BEGIN ` + ins("C1", "Photos") + ins("C2", "Docs") + ` END`},
		{OnUpdate, `CREATE TRIGGER "tr" AFTER UPDATE OF "Photos", "Docs" ON "Sites" FOR EACH ROW BEGIN ` +
			del("C1", "Photos") + ins("C1", "Photos") + del("C2", "Docs") + ins("C2", "Docs") + ` END`},
		{OnDelete, `CREATE TRIGGER "tr" AFTER DELETE ON "Sites" FOR EACH ROW BEGIN DELETE FROM _sys_file_refs WHERE table_id = 'T1' AND row_id = OLD."id"; END`},
	}
	for _, tc := range cases {
		got := mustRender(t, &AttachmentRefs{Name: "tr", Event: tc.event, Table: "Sites", TableID: "T1", Columns: cols})
		if got.SQL != tc.want {
			t.Errorf("event %d:\n got %s\nwant %s", tc.event, got.SQL, tc.want)
		}
	}
	// Catalog IDs are inlined as literals, so a quote in one is escaped.
	got := mustRender(t, &AttachmentRefs{Name: "tr", Event: OnDelete, Table: "Sites", TableID: "it's"})
	if !strings.Contains(got.SQL, `table_id = 'it''s'`) {
		t.Errorf("table ID not escaped: %s", got.SQL)
	}
}

func TestRenderRefusesMalformedSchema(t *testing.T) {
	col := func(mut func(*ColumnDef)) Stmt {
		c := ColumnDef{Name: "c", Type: Integer}
		mut(&c)
		return &AddColumn{Table: "t", Column: c}
	}
	refs := func(mut func(*AttachmentRefs)) Stmt {
		s := &AttachmentRefs{Name: "tr", Event: OnInsert, Table: "t", TableID: "T1", Columns: []AttachmentColumn{{ID: "C1", Name: "a"}}}
		mut(s)
		return s
	}
	fk := func(f ForeignKey) Stmt {
		return &CreateTable{Name: "t", Columns: []ColumnDef{{Name: "p", Type: Integer}}, ForeignKeys: []ForeignKey{f}}
	}
	cases := map[string]Stmt{
		"table without columns":       &CreateTable{Name: "t"},
		"table without a name":        &CreateTable{Columns: []ColumnDef{{Name: "c"}}},
		"column without a name":       col(func(c *ColumnDef) { c.Name = "" }),
		"generated with a default":    col(func(c *ColumnDef) { c.Generated, c.Default = Raw{SQL: "1"}, Lit{Value: 1} }),
		"generated NOT NULL":          col(func(c *ColumnDef) { c.Generated, c.NotNull = Raw{SQL: "1"}, true }),
		"generated with a check":      col(func(c *ColumnDef) { c.Generated, c.Checks = Raw{SQL: "1"}, []Check{{Name: "ck", Expr: Raw{SQL: "1"}}} }),
		"domain check without a name": col(func(c *ColumnDef) { c.Type = Boolean }),
		"check without a name":        col(func(c *ColumnDef) { c.Checks = []Check{{Expr: Raw{SQL: "c > 0"}}} }),
		"bound default":               col(func(c *ColumnDef) { c.Default = Arg{Value: 1} }),
		"check with a parameter": col(func(c *ColumnDef) {
			c.Checks = []Check{{Name: "ck", Expr: Binary{Op: OpGt, L: Col{Name: "c"}, R: Param{Name: "min"}}}}
		}),
		"NUL in a default":             col(func(c *ColumnDef) { c.Default = Lit{Value: "a\x00"} }),
		"unknown ON DELETE":            fk(ForeignKey{Name: "fk", Column: "p", RefTable: "u", OnDelete: "NO ACTION"}),
		"foreign key without a column": fk(ForeignKey{Name: "fk", RefTable: "u", OnDelete: Cascade}),
		"add column without a table":   &AddColumn{Column: ColumnDef{Name: "c"}},
		"rename without a target":      &RenameTable{From: "a"},
		"rename column without a name": &RenameColumn{Table: "t", From: "a"},
		"drop table without a name":    &DropTable{IfExists: true},
		"drop index without a name":    &DropIndex{},
		"drop trigger without a name":  &DropTrigger{},
		"index without columns":        &CreateIndex{Name: "ix", Table: "t"},
		"index without a table":        &CreateIndex{Name: "ix", Columns: []Col{{Name: "a"}}},
		"qualified index column":       &CreateIndex{Name: "ix", Table: "t", Columns: []Col{{Table: "t", Name: "a"}}},
		"OCC trigger without a table":  &OCCBump{Name: "tr"},
		"refs without columns":         refs(func(s *AttachmentRefs) { s.Columns = nil }),
		"refs without a table ID":      refs(func(s *AttachmentRefs) { s.TableID = "" }),
		"refs with a NUL in an ID":     refs(func(s *AttachmentRefs) { s.Columns[0].ID = "C\x00" }),
		"refs for an unknown event":    refs(func(s *AttachmentRefs) { s.Event = TriggerEvent(9) }),
	}
	for name, s := range cases {
		for _, d := range []Dialect{SQLite, Postgres} {
			if d == Postgres && name == "domain check without a name" {
				continue // a yes/no column is boolean there, with no domain check to name
			}
			if r, err := Render(d, s); err == nil {
				t.Errorf("%s on %s: rendered %q, want an error", name, d, r.SQL)
			}
		}
	}
}
