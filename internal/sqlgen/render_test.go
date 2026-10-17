package sqlgen

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func mustRender(t *testing.T, s Stmt) Rendered {
	t.Helper()
	r, err := Render(SQLite, s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The shape the visual query designer produces: aliased sources, an
// inner join, criteria rows OR-ed together, grouping, totals and sorting,
// with literals inlined because the SQL is stored and shown to builders.
func TestRenderVisualQueryShape(t *testing.T) {
	total := Col{Table: "o", Name: "Total", Type: Currency}
	name := Col{Table: "c", Name: "Name", Type: Text, CI: true}
	s := &Select{
		Columns: []Column{{Expr: name, As: "Customer"}, {Expr: Agg{Fn: AggSum, X: total}, As: "sum of Total"}},
		From:    Table{Name: "Customers", Alias: "c"},
		Joins:   []Join{{Kind: InnerJoin, Source: Table{Name: "Orders", Alias: "o"}, On: Binary{Op: OpEq, L: Col{Table: "o", Name: "Customer"}, R: Col{Table: "c", Name: "id"}}}},
		Where: Or{
			And{Match{Kind: MatchStarts, X: name, Pattern: Lit{Value: "50%_off"}}, Binary{Op: OpGe, L: total, R: Lit{Value: int64(1000000)}}},
			And{IsNull{X: total}},
		},
		GroupBy: []Expr{name},
		Having:  And{Binary{Op: OpGt, L: Agg{Fn: AggSum, X: total}, R: Param{Name: "min_total"}}},
		OrderBy: []Order{{Expr: Agg{Fn: AggSum, X: total}, Desc: true}, {Expr: name}},
		Limit:   Lit{Value: 50},
	}
	got := mustRender(t, s)
	want := `SELECT "c"."Name" AS "Customer", SUM("o"."Total") AS "sum of Total" FROM "Customers" AS "c" ` +
		`INNER JOIN "Orders" AS "o" ON "o"."Customer" = "c"."id" ` +
		`WHERE (("c"."Name" LIKE '50\%\_off%' ESCAPE '\' AND "o"."Total" >= 1000000) OR ("o"."Total" IS NULL)) ` +
		`GROUP BY "c"."Name" HAVING (SUM("o"."Total") > :min_total) ` +
		`ORDER BY SUM("o"."Total") DESC, "c"."Name" COLLATE NOCASE ASC LIMIT 50`
	if got.SQL != want {
		t.Fatalf("SQL:\n got %s\nwant %s", got.SQL, want)
	}
	if len(got.Args) != 0 || !reflect.DeepEqual(got.Params, []string{"min_total"}) {
		t.Errorf("args %v params %v, want none and [min_total]", got.Args, got.Params)
	}
	if want := []ResultColumn{{"Customer", Text}, {"sum of Total", Currency}}; !reflect.DeepEqual(got.Columns, want) {
		t.Errorf("columns %v, want %v", got.Columns, want)
	}
}

// The shape a datasheet page read produces: bound values, a lookup display
// subquery, a case-insensitive sort with an id tiebreak, LIMIT/OFFSET.
func TestRenderRecordsPageShape(t *testing.T) {
	s := &Select{
		Columns: []Column{
			{Expr: Col{Table: "t", Name: "id", Type: Integer}},
			{Expr: Col{Table: "t", Name: "Name", Type: Text}},
			{Expr: Subquery{Select: &Select{
				Columns: []Column{{Expr: Col{Table: "p", Name: "Company", Type: Text}}},
				From:    Table{Name: "Customers", Alias: "p"},
				Where:   Binary{Op: OpEq, L: Col{Table: "p", Name: "id"}, R: Col{Table: "t", Name: "Customer"}},
			}}},
		},
		From:    Table{Name: "Orders", Alias: "t"},
		Where:   And{Match{Kind: MatchContains, X: Col{Table: "t", Name: "Name"}, Pattern: Arg{Value: "a_b"}}, In{X: Col{Table: "t", Name: "Qty"}, List: []Expr{Arg{Value: int64(1)}, Arg{Value: int64(2)}}}},
		OrderBy: []Order{{Expr: Col{Table: "t", Name: "Name", CI: true}}, {Expr: Col{Table: "t", Name: "id"}}},
		Limit:   Arg{Value: 51},
		Offset:  Arg{Value: 0},
	}
	got := mustRender(t, s)
	want := `SELECT "t"."id", "t"."Name", (SELECT "p"."Company" FROM "Customers" AS "p" WHERE "p"."id" = "t"."Customer") ` +
		`FROM "Orders" AS "t" WHERE ("t"."Name" LIKE ? ESCAPE '\' AND "t"."Qty" IN (?, ?)) ` +
		`ORDER BY "t"."Name" COLLATE NOCASE ASC, "t"."id" ASC LIMIT ? OFFSET ?`
	if got.SQL != want {
		t.Fatalf("SQL:\n got %s\nwant %s", got.SQL, want)
	}
	if want := []any{`%a\_b%`, int64(1), int64(2), 51, 0}; !reflect.DeepEqual(got.Args, want) {
		t.Errorf("args %#v, want %#v", got.Args, want)
	}
	if want := []ResultColumn{{"id", Integer}, {"Name", Text}, {"", Text}}; !reflect.DeepEqual(got.Columns, want) {
		t.Errorf("columns %v, want %v", got.Columns, want)
	}
}

func TestRenderWrites(t *testing.T) {
	cases := []struct {
		name string
		s    Stmt
		want string
		args []any
	}{
		{"insert rows returning",
			&Insert{Table: "Orders", Columns: []string{"Name", "Qty"}, Rows: [][]Expr{{Arg{Value: "a"}, Arg{Value: int64(1)}}, {Arg{Value: "b"}, Null}},
				Returning: []Column{{Expr: Col{Name: "id"}}, {Expr: Col{Name: "_version"}}}},
			`INSERT INTO "Orders" ("Name", "Qty") VALUES (?, ?), (?, NULL) RETURNING "id", "_version"`, []any{"a", int64(1), "b"}},
		{"upsert do nothing",
			&Insert{Table: "_sys_meta", Columns: []string{"key", "value"}, Rows: [][]Expr{{Arg{Value: "k"}, Arg{Value: "v"}}}, OnConflict: &OnConflict{Target: []string{"key"}}},
			`INSERT INTO "_sys_meta" ("key", "value") VALUES (?, ?) ON CONFLICT ("key") DO NOTHING`, []any{"k", "v"}},
		{"upsert do update from excluded",
			&Insert{Table: "_sys_meta", Columns: []string{"key", "value"}, Rows: [][]Expr{{Arg{Value: "k"}, Arg{Value: "v"}}},
				OnConflict: &OnConflict{Target: []string{"key"}, Set: []Assign{{Column: "value", Value: Excluded("value")}}}},
			`INSERT INTO "_sys_meta" ("key", "value") VALUES (?, ?) ON CONFLICT ("key") DO UPDATE SET "value" = excluded."value"`, []any{"k", "v"}},
		{"insert select upsert gets a WHERE",
			&Insert{Table: "b", Columns: []string{"x"}, From: &Select{Columns: []Column{{Expr: Col{Name: "x"}}}, From: Table{Name: "a"}}, OnConflict: &OnConflict{}},
			`INSERT INTO "b" ("x") SELECT "x" FROM "a" WHERE 1 ON CONFLICT DO NOTHING`, nil},
		{"update with version check",
			&Update{Table: "Orders", Set: []Assign{{Column: "Qty", Value: Arg{Value: int64(3)}}, {Column: "_version", Value: Binary{Op: OpAdd, L: Col{Name: "_version"}, R: Lit{Value: 1}}}},
				Where: And{Binary{Op: OpEq, L: Col{Name: "id"}, R: Arg{Value: int64(7)}}, Binary{Op: OpEq, L: Col{Name: "_version"}, R: Arg{Value: int64(2)}}}},
			`UPDATE "Orders" SET "Qty" = ?, "_version" = "_version" + 1 WHERE ("id" = ? AND "_version" = ?)`, []any{int64(3), int64(7), int64(2)}},
		{"insert defaults returning",
			&Insert{Table: "Orders", Returning: []Column{{Expr: Col{Name: "id"}}}},
			`INSERT INTO "Orders" DEFAULT VALUES RETURNING "id"`, nil},
		{"delete",
			&Delete{Table: "Orders", Where: Binary{Op: OpEq, L: Col{Name: "id"}, R: Arg{Value: int64(7)}}},
			`DELETE FROM "Orders" WHERE "id" = ?`, []any{int64(7)}},
	}
	for _, tc := range cases {
		got := mustRender(t, tc.s)
		if got.SQL != tc.want || !reflect.DeepEqual(got.Args, tc.args) {
			t.Errorf("%s:\n got %s %v\nwant %s %v", tc.name, got.SQL, got.Args, tc.want, tc.args)
		}
	}
}

func TestRenderExpressions(t *testing.T) {
	x := Col{Name: "x", Type: Integer}
	cases := []struct {
		e    Expr
		want string
	}{
		// Operands that are themselves operations are parenthesised.
		{Binary{Op: OpMul, L: Binary{Op: OpAdd, L: x, R: Lit{Value: 1}}, R: Lit{Value: 2}}, `("x" + 1) * 2`},
		{Not{X: Binary{Op: OpEq, L: x, R: Lit{Value: 1}}}, `NOT ("x" = 1)`},
		{Binary{Op: OpIsNot, L: x, R: Lit{Value: 1}}, `"x" IS NOT 1`},
		{IsNull{X: x, Negate: true}, `"x" IS NOT NULL`},
		{Between{X: x, Lo: Lit{Value: 1}, Hi: Lit{Value: 9}}, `"x" BETWEEN 1 AND 9`},
		{Match{Kind: MatchLike, X: Col{Name: "s"}, Pattern: Lit{Value: AccessWildcards("A*_?")}}, `"s" LIKE 'A%\__' ESCAPE '\'`},
		{Match{Kind: MatchContains, X: Col{Name: "s"}, Pattern: Param{Name: "q"}}, `"s" LIKE '%' || :q || '%' ESCAPE '\'`},
		{Match{Kind: MatchStarts, X: Col{Name: "s"}, Pattern: Param{Name: "q"}}, `"s" LIKE :q || '%' ESCAPE '\'`},
		{Match{Kind: MatchEnds, X: Col{Name: "s"}, Pattern: Param{Name: "q"}}, `"s" LIKE '%' || :q ESCAPE '\'`},
		{Func{Name: "IFNULL", Args: []Expr{x, Lit{Value: 0}}}, `ifnull("x", 0)`},
		{Cast{X: Func{Name: "round", Args: []Expr{Binary{Op: OpMul, L: Param{Name: "amount"}, R: Lit{Value: 10000.0}}}}, Type: Currency}, `CAST(round(:amount * 10000.0) AS INTEGER)`},
		{Agg{Fn: AggCount}, `COUNT(*)`},
		{Agg{Fn: AggCount, X: x, Distinct: true}, `COUNT(DISTINCT "x")`},
		{Case{Whens: []When{{Cond: Binary{Op: OpGt, L: x, R: Lit{Value: 0}}, Then: Lit{Value: "pos"}}}, Else: Lit{Value: "it's not"}}, `CASE WHEN "x" > 0 THEN 'pos' ELSE 'it''s not' END`},
		{Raw{SQL: `"a" + "b"`}, `("a" + "b")`},
		{Col{Name: `we"ird`}, `"we""ird"`},
		{Lit{Value: true}, `1`},
		{Lit{Value: 2.5}, `2.5`},
		{Lit{Value: 1e21}, `1e+21`},
	}
	for _, tc := range cases {
		got, err := RenderExpr(SQLite, tc.e)
		if err != nil {
			t.Errorf("%#v: %v", tc.e, err)
			continue
		}
		if got.SQL != tc.want {
			t.Errorf("%#v:\n got %s\nwant %s", tc.e, got.SQL, tc.want)
		}
	}
}

func TestRenderRefusesMalformedInput(t *testing.T) {
	sel := func(mut func(*Select)) Stmt {
		s := &Select{Columns: []Column{{Expr: Col{Name: "x"}}}, From: Table{Name: "t"}}
		mut(s)
		return s
	}
	cases := map[string]Stmt{
		"unknown function":         sel(func(s *Select) { s.Columns[0].Expr = Func{Name: "load_extension"} }),
		"empty IN":                 sel(func(s *Select) { s.Where = In{X: Col{Name: "x"}} }),
		"bad parameter name":       sel(func(s *Select) { s.Where = Binary{Op: OpEq, L: Col{Name: "x"}, R: Param{Name: "a;b"}} }),
		"NUL in a literal":         sel(func(s *Select) { s.Where = Binary{Op: OpEq, L: Col{Name: "x"}, R: Lit{Value: "a\x00b"}} }),
		"non-finite literal":       sel(func(s *Select) { s.Where = Binary{Op: OpEq, L: Col{Name: "x"}, R: Lit{Value: math.Inf(1)}} }),
		"unsupported literal":      sel(func(s *Select) { s.Where = Binary{Op: OpEq, L: Col{Name: "x"}, R: Lit{Value: []byte("x")}} }),
		"offset without limit":     sel(func(s *Select) { s.Offset = Arg{Value: 1} }),
		"raw source without alias": sel(func(s *Select) { s.From = RawSource{SQL: "SELECT 1"} }),
		"join without ON":          sel(func(s *Select) { s.Joins = []Join{{Kind: InnerJoin, Source: Table{Name: "u"}}} }),
		"no columns":               &Select{From: Table{Name: "t"}},
		"insert row width":         &Insert{Table: "t", Columns: []string{"a", "b"}, Rows: [][]Expr{{Arg{Value: 1}}}},
		"insert rows and select":   &Insert{Table: "t", Columns: []string{"a"}},
		"defaults with values":     &Insert{Table: "t", Rows: [][]Expr{{Arg{Value: 1}}}},
		"update without set":       &Update{Table: "t"},
		"upsert update no target":  &Insert{Table: "t", Columns: []string{"a"}, Rows: [][]Expr{{Arg{Value: 1}}}, OnConflict: &OnConflict{Set: []Assign{{Column: "a", Value: Arg{Value: 2}}}}},
	}
	for name, s := range cases {
		if r, err := Render(SQLite, s); err == nil {
			t.Errorf("%s: rendered %q, want an error", name, r.SQL)
		}
	}
	if _, err := Render(Postgres, &Select{Columns: []Column{{Expr: Lit{Value: 1}}}}); !errors.Is(err, ErrDialect) {
		t.Errorf("Postgres: err = %v, want ErrDialect until Phase F", err)
	}
}

// A repeated parameter is listed once, under the spelling first used.
func TestRenderParamsListedOnce(t *testing.T) {
	r, err := RenderExpr(SQLite, Or{Binary{Op: OpEq, L: Col{Name: "a"}, R: Param{Name: "Start"}}, Binary{Op: OpEq, L: Col{Name: "b"}, R: Param{Name: "start"}}, Binary{Op: OpEq, L: Col{Name: "c"}, R: Param{Name: "end"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Params, []string{"Start", "end"}) {
		t.Errorf("params = %v", r.Params)
	}
	if strings.Count(r.SQL, ":") != 3 {
		t.Errorf("SQL = %s", r.SQL)
	}
}

func TestRenderOrderBy(t *testing.T) {
	r, err := RenderOrderBy(SQLite, []Order{{Expr: Col{Name: "Name", CI: true}, Desc: true, Nulls: NullsLast}, {Expr: Col{Name: "id"}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `"Name" COLLATE NOCASE DESC NULLS LAST, "id" ASC`; r.SQL != want {
		t.Errorf("got %s, want %s", r.SQL, want)
	}
	if _, err := RenderOrderBy(SQLite, nil); err == nil {
		t.Error("an empty ORDER BY rendered")
	}
}
