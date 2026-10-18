package sqlgen

import (
	"errors"
	"slices"
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
		`"name" COLLATE "default" ILIKE ? ESCAPE '\'`:                                Match{Kind: MatchContains, X: name, Pattern: Arg{Value: "a"}, CI: true},
		`"name" COLLATE "default" LIKE 'a%' ESCAPE '\'`:                              Match{Kind: MatchStarts, X: name, Pattern: Lit{Value: "a"}},
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
	// Sorting a case-insensitive column relies on its collation, and OR
	// IGNORE has no PostgreSQL equivalent for CHECK failures.
	if r, err := Render(Postgres, &Select{Columns: []Column{{Expr: name}}, From: Table{Name: "t"}, OrderBy: []Order{{Expr: name, Nulls: NullsLast}}}); err != nil || r.SQL != `SELECT "name" FROM "t" ORDER BY "name" ASC NULLS LAST` {
		t.Errorf("order: %q %v", r.SQL, err)
	}
	if _, err := Render(Postgres, &Insert{Table: "t", OrIgnore: true, Columns: []string{"a"}, Rows: [][]Expr{{Arg{Value: 1}}}}); err == nil {
		t.Error("OR IGNORE rendered on PostgreSQL")
	}
}
