package sqlgen

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// Every catalog function either renders on PostgreSQL or is listed as
// pending, never both: the list is the remaining work, kept honest.
func TestPostgresFunctionCoverage(t *testing.T) {
	for _, name := range FuncNames() {
		_, mapped := pgFuncs[name]
		pending := slices.Contains(pgPending, name)
		if mapped == pending {
			t.Errorf("%s: mapped %v, pending %v; want exactly one", name, mapped, pending)
		}
	}
	for name := range pgFuncs {
		if _, ok := LookupFunc(name); !ok {
			t.Errorf("%s is mapped for PostgreSQL but not in the catalog", name)
		}
	}
	for _, name := range pgPending {
		def, _ := LookupFunc(name)
		args := make([]Expr, def.MinArgs)
		for i := range args {
			args[i] = Lit{Value: "x", Type: Text}
		}
		if _, err := RenderExpr(Postgres, Func{Name: name, Args: args}); !errors.Is(err, ErrDialect) {
			t.Errorf("pending %s: err = %v, want ErrDialect", name, err)
		}
	}
}

// The rewrites that pin SQLite's answers on PostgreSQL (the differential
// tests in internal/pgdb run them).
func TestRenderPostgres(t *testing.T) {
	qty := Col{Name: "qty", Type: Integer}
	price := Col{Name: "price", Type: Number}
	active := Col{Name: "active", Type: Boolean}
	name := Col{Name: "name", Type: Text, CI: true}
	for want, e := range map[string]Expr{
		`"qty" / NULLIF(2, 0)`:                                                       Binary{Op: OpDiv, L: qty, R: Lit{Value: 2, Type: Integer}},
		`CAST(trunc("price") AS bigint) % NULLIF(3, 0)`:                              Binary{Op: OpMod, L: price, R: Lit{Value: 3, Type: Integer}},
		`"qty" IS NOT DISTINCT FROM ?`:                                               Binary{Op: OpIs, L: qty, R: Arg{Value: 1}},
		`"qty" IS DISTINCT FROM NULL`:                                                Binary{Op: OpIsNot, L: qty, R: Null},
		`CAST("active" AS integer) = 1`:                                              Binary{Op: OpEq, L: active, R: Lit{Value: 1, Type: Integer}},
		`"active" = TRUE`:                                                            Binary{Op: OpEq, L: active, R: Lit{Value: true, Type: Boolean}},
		`CAST("qty" AS text) || '!'`:                                                 Binary{Op: OpConcat, L: qty, R: Lit{Value: "!", Type: Text}},
		`"price" * 1.5::float8`:                                                      Binary{Op: OpMul, L: price, R: Lit{Value: 1.5, Type: Number}},
		`"name" COLLATE "C" ILIKE ? ESCAPE '\'`:                                      Match{Kind: MatchContains, X: name, Pattern: Arg{Value: "a"}, CI: true},
		`"name" COLLATE "C" LIKE 'a%' ESCAPE '\'`:                                    Match{Kind: MatchStarts, X: name, Pattern: Lit{Value: "a"}},
		`CAST(trunc(CAST("price" AS double precision)) AS bigint)`:                   Cast{X: price, Type: Integer},
		`CASE WHEN "qty" IS NULL OR 2 IS NULL THEN NULL ELSE GREATEST("qty", 2) END`: Func{Name: "max", Args: []Expr{qty, Lit{Value: 2, Type: Integer}}},
		`CASE WHEN "qty" <> 0 THEN 'y' ELSE 'n' END`:                                 Func{Name: "iif", Args: []Expr{qty, Lit{Value: "y"}, Lit{Value: "n"}}},
		`abi_round("price", 2)`:                                                      Func{Name: "round", Args: []Expr{price, Lit{Value: 2, Type: Integer}}},
		`"qty"`:                                                                      Func{Name: "ceil", Args: []Expr{qty}},
		`CAST(SUM("qty") AS bigint)`:                                                 Agg{Fn: AggSum, X: qty},
		`CAST(AVG(CAST("active" AS integer)) AS double precision)`:                   Agg{Fn: AggAvg, X: active},
		`bool_or("active")`:                                                          Agg{Fn: AggMax, X: active},
	} {
		r, err := RenderExpr(Postgres, e)
		if err != nil {
			t.Errorf("%s: %v", want, err)
			continue
		}
		if r.SQL != want {
			t.Errorf("got  %s\nwant %s", r.SQL, want)
		}
	}
	// A case-insensitive column sorts by <prefix>_lib's abi_ci, and OR
	// IGNORE has no PostgreSQL equivalent for CHECK failures.
	if r, err := Render(Postgres, &Select{Columns: []Column{{Expr: name}}, From: Table{Name: "t"}, OrderBy: []Order{{Expr: name, Nulls: NullsLast}}}); err != nil || r.SQL != `SELECT "name" FROM "t" ORDER BY "name" COLLATE abi_ci ASC NULLS LAST` {
		t.Errorf("order: %q %v", r.SQL, err)
	}
	if _, err := Render(Postgres, &Insert{Table: "t", OrIgnore: true, Columns: []string{"a"}, Rows: [][]Expr{{Arg{Value: 1}}}}); err == nil {
		t.Error("OR IGNORE rendered on PostgreSQL")
	}
}

// PostgreSQL DDL: native storage types, binary text, abi_ci where SQLite says
// NOCASE, and the <prefix>_lib trigger functions.
func TestRenderPostgresDDL(t *testing.T) {
	table := &CreateTable{Name: "Orders", Strict: true, Columns: []ColumnDef{
		{Name: "id", AutoIncrement: true},
		{Name: "Ref", Type: Text, MaxLen: 20, NotNull: true, TypeCheck: "ty_ref", Checks: []Check{{Name: "ck_ref", Expr: Binary{Op: OpGt, L: Func{Name: "length", Args: []Expr{Col{Name: "Ref", Type: Text}}}, R: Lit{Value: 0, Type: Integer}}}}},
		{Name: "Site", Type: Text, URL: true, TypeCheck: "ty_site"},
		{Name: "Paid", Type: Boolean, NotNull: true, Default: Lit{Value: 0, Type: Integer}},
		{Name: "Due", Type: Date, Default: Func{Name: "date", Args: []Expr{Lit{Value: "now"}, Lit{Value: "+30 days"}}}},
		{Name: "At", Type: DateTime},
		{Name: "Where", Type: GeoPoint, TypeCheck: "ty_where"},
		{Name: "Files", Type: Attachments, TypeCheck: "ty_files"},
		{Name: "Total", Type: Currency},
		{Name: "Late", Type: Boolean, Generated: Binary{Op: OpLt, L: Col{Name: "Due", Type: Date}, R: Func{Name: "date", Args: []Expr{Col{Name: "At", Type: DateTime}}}}},
		{Name: "Month", Type: Text, Generated: Func{Name: "strftime", Args: []Expr{Lit{Value: "%Y-%m"}, Col{Name: "Due", Type: Date}}}},
		{Name: "Net", Type: Currency, Generated: Binary{Op: OpSub, L: Col{Name: "Total", Type: Currency}, R: Lit{Value: 100, Type: Currency}}},
	}, ForeignKeys: []ForeignKey{{Name: "fk_x", Column: "Total", RefTable: "Other", OnDelete: SetNull}}}
	want := `CREATE TABLE "Orders" (
  "id" bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
  "Ref" text COLLATE "C" NOT NULL CONSTRAINT "ty_ref" CHECK (char_length("Ref") <= 20) CONSTRAINT "ck_ref" CHECK (length("Ref") > 0),
  "Site" text COLLATE "C" CONSTRAINT "ty_site" CHECK ("Site" IS NULL OR "Site" ~ '^http.*://'),
  "Paid" boolean NOT NULL DEFAULT (FALSE),
  "Due" date DEFAULT (abi_to_date(abi_date(statement_timestamp(), '+30 days'))),
  "At" timestamptz,
  "Where" jsonb CONSTRAINT "ty_where" CHECK ("Where" IS NULL OR ("Where" ->> 'type') IN ('Point')),
  "Files" jsonb CONSTRAINT "ty_files" CHECK ("Files" IS NULL OR jsonb_typeof("Files") = 'array'),
  "Total" bigint,
  "Late" boolean GENERATED ALWAYS AS ("Due" < abi_to_date(abi_date("At"))) STORED,
  "Month" text COLLATE "C" GENERATED ALWAYS AS (abi_strftime('%Y-%m', "Due")) STORED,
  "Net" bigint GENERATED ALWAYS AS ("Total" - 100) VIRTUAL,
  CONSTRAINT "fk_x" FOREIGN KEY ("Total") REFERENCES "Other"("id") ON DELETE SET NULL
)`
	for d, gen := range map[Dialect]string{Postgres: "VIRTUAL", Postgres17: "STORED"} {
		r, err := Render(d, table)
		if err != nil {
			t.Fatal(err)
		}
		if w := strings.ReplaceAll(want, "VIRTUAL", gen); r.SQL != w {
			t.Errorf("%s:\ngot  %s\nwant %s", d, r.SQL, w)
		}
	}
	for want, s := range map[string]Stmt{
		`CREATE UNIQUE INDEX "ux" ON "Orders" ("Ref" COLLATE abi_ci, "Total")`:                                                        &CreateIndex{Name: "ux", Table: "Orders", Unique: true, Columns: []Col{{Name: "Ref", CI: true}, {Name: "Total"}}},
		`CREATE TRIGGER "_occ" BEFORE UPDATE ON "Orders" FOR EACH ROW EXECUTE FUNCTION abi_occ_bump()`:                                &OCCBump{Name: "_occ", Table: "Orders"},
		`DROP TRIGGER IF EXISTS "_occ" ON "Orders"`:                                                                                   &DropTrigger{Name: "_occ", Table: "Orders", IfExists: true},
		`CREATE TRIGGER "_att_u" AFTER UPDATE OF "Files" ON "Orders" FOR EACH ROW EXECUTE FUNCTION abi_att_refs('T1', 'C1', 'Files')`: &AttachmentRefs{Name: "_att_u", Event: OnUpdate, Table: "Orders", TableID: "T1", Columns: []AttachmentColumn{{ID: "C1", Name: "Files"}}},
		`CREATE TRIGGER "_att_d" AFTER DELETE ON "Orders" FOR EACH ROW EXECUTE FUNCTION abi_att_refs('T1')`:                           &AttachmentRefs{Name: "_att_d", Event: OnDelete, Table: "Orders", TableID: "T1", Columns: []AttachmentColumn{{ID: "C1", Name: "Files"}}},
	} {
		r, err := Render(Postgres, s)
		if err != nil {
			t.Errorf("%s: %v", want, err)
			continue
		}
		if r.SQL != want {
			t.Errorf("got  %s\nwant %s", r.SQL, want)
		}
	}
	if _, err := Render(Postgres, &DropTrigger{Name: "x"}); err == nil {
		t.Error("a PostgreSQL DROP TRIGGER without its table rendered")
	}
}

func TestCICollation(t *testing.T) {
	for d, want := range map[Dialect]string{SQLite: "NOCASE", Postgres: "abi_ci", Postgres17: "abi_ci"} {
		if got := CICollation(d); got != want {
			t.Errorf("%v: %s, want %s", d, got, want)
		}
	}
}
