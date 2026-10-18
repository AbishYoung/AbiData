package sqlgen_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"abidata/internal/dbx"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

// These tests run rendered SQL on SQLite: the model's promises (literal
// matching, explicit NULL placement, upserts, typed literals) are checked
// against the engine, not against strings.

func testDB(t *testing.T) *sqlitex.Conn {
	t.Helper()
	ctx := context.Background()
	c, err := sqlitex.Open(ctx, filepath.Join(t.TempDir(), "t.db"), sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.ExecScript(ctx, `
		CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, qty INTEGER, note TEXT) STRICT;
		INSERT INTO items VALUES (1, 'Apple', 3, '50% off'), (2, 'banana', NULL, '50 percent'),
			(3, 'Cherry', 10, 'a_b'), (4, 'apricot', 7, 'axb'), (5, NULL, 1, NULL);
		CREATE TABLE kv (k TEXT PRIMARY KEY, v INTEGER NOT NULL) STRICT;`); err != nil {
		t.Fatal(err)
	}
	return c
}

func run(t *testing.T, c *sqlitex.Conn, s sqlgen.Stmt, named ...dbx.Named) [][]any {
	t.Helper()
	r, err := sqlgen.Render(sqlgen.SQLite, s)
	if err != nil {
		t.Fatal(err)
	}
	args := append([]any{}, r.Args...)
	for _, n := range named {
		args = append(args, n)
	}
	rows, err := c.Query(context.Background(), r.SQL, args...)
	if err != nil {
		t.Fatalf("%s: %v", r.SQL, err)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		out = append(out, rows.Values())
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func ids(rows [][]any) []int64 {
	out := []int64{}
	for _, r := range rows {
		out = append(out, r[0].(int64))
	}
	return out
}

var (
	id   = sqlgen.Col{Name: "id", Type: sqlgen.Integer}
	name = sqlgen.Col{Name: "name", Type: sqlgen.Text, CI: true}
	qty  = sqlgen.Col{Name: "qty", Type: sqlgen.Integer}
	note = sqlgen.Col{Name: "note", Type: sqlgen.Text}
)

func selectIDs(where sqlgen.Expr, order ...sqlgen.Order) *sqlgen.Select {
	if len(order) == 0 {
		order = []sqlgen.Order{{Expr: id}}
	}
	return &sqlgen.Select{Columns: []sqlgen.Column{{Expr: id}}, From: sqlgen.Table{Name: "items"}, Where: where, OrderBy: order}
}

func TestExecTextMatchingIsLiteral(t *testing.T) {
	c := testDB(t)
	cases := []struct {
		name string
		m    sqlgen.Match
		want []int64
	}{
		// % and _ in the searched text are matched literally, whether the
		// value is bound or inlined.
		{"contains % bound", sqlgen.Match{Kind: sqlgen.MatchContains, X: note, Pattern: sqlgen.Arg{Value: "0% "}}, []int64{1}},
		{"contains _ inlined", sqlgen.Match{Kind: sqlgen.MatchContains, X: note, Pattern: sqlgen.Lit{Value: "a_b"}}, []int64{3}},
		{"starts, any case", sqlgen.Match{Kind: sqlgen.MatchStarts, X: name, Pattern: sqlgen.Arg{Value: "AP"}, CI: true}, []int64{1, 4}},
		{"ends", sqlgen.Match{Kind: sqlgen.MatchEnds, X: name, Pattern: sqlgen.Lit{Value: "ana"}}, []int64{2}},
		// Access wildcards: * any run, ? one character.
		{"access wildcards", sqlgen.Match{Kind: sqlgen.MatchLike, X: name, Pattern: sqlgen.Lit{Value: sqlgen.AccessWildcards("ap?i*")}}, []int64{4}},
	}
	for _, tc := range cases {
		if got := ids(run(t, c, selectIDs(tc.m))); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: ids %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestExecNullPlacementIsExplicit(t *testing.T) {
	c := testDB(t)
	first := ids(run(t, c, selectIDs(nil, sqlgen.Order{Expr: qty, Nulls: sqlgen.NullsFirst}, sqlgen.Order{Expr: id})))
	last := ids(run(t, c, selectIDs(nil, sqlgen.Order{Expr: qty, Nulls: sqlgen.NullsLast}, sqlgen.Order{Expr: id})))
	if want := []int64{2, 5, 1, 4, 3}; !reflect.DeepEqual(first, want) {
		t.Errorf("NULLS FIRST: %v, want %v", first, want)
	}
	if want := []int64{5, 1, 4, 3, 2}; !reflect.DeepEqual(last, want) {
		t.Errorf("NULLS LAST: %v, want %v", last, want)
	}
	// A case-insensitive column sorts without regard to case.
	byName := ids(run(t, c, selectIDs(sqlgen.IsNull{X: name, Negate: true}, sqlgen.Order{Expr: name})))
	if want := []int64{1, 4, 2, 3}; !reflect.DeepEqual(byName, want) {
		t.Errorf("by name: %v, want %v", byName, want)
	}
}

func TestExecPredicatesAndParams(t *testing.T) {
	c := testDB(t)
	where := sqlgen.Or{
		sqlgen.And{sqlgen.In{X: qty, List: []sqlgen.Expr{sqlgen.Arg{Value: int64(3)}, sqlgen.Arg{Value: int64(10)}}}},
		sqlgen.And{sqlgen.Between{X: qty, Lo: sqlgen.Param{Name: "lo"}, Hi: sqlgen.Param{Name: "hi"}}, sqlgen.Binary{Op: sqlgen.OpIsNot, L: name, R: sqlgen.Lit{Value: "x"}}},
	}
	got := ids(run(t, c, selectIDs(where), dbx.Named{Name: ":lo", Value: int64(6)}, dbx.Named{Name: ":hi", Value: int64(8)}))
	if want := []int64{1, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("ids %v, want %v", got, want)
	}
	// IS NOT is null-safe: a NULL name is "not equal" to 'x'; <> is not.
	isNot := ids(run(t, c, selectIDs(sqlgen.Binary{Op: sqlgen.OpIsNot, L: name, R: sqlgen.Lit{Value: "x"}})))
	ne := ids(run(t, c, selectIDs(sqlgen.Binary{Op: sqlgen.OpNe, L: name, R: sqlgen.Lit{Value: "x"}})))
	if len(isNot) != 5 || len(ne) != 4 {
		t.Errorf("IS NOT matched %v, <> matched %v; want 5 and 4 rows", isNot, ne)
	}
}

// A float literal stays REAL, so it never turns into integer division.
func TestExecFloatLiteralStaysReal(t *testing.T) {
	c := testDB(t)
	got := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{
		{Expr: sqlgen.Binary{Op: sqlgen.OpDiv, L: sqlgen.Lit{Value: int64(10)}, R: sqlgen.Lit{Value: 4.0}}},
		{Expr: sqlgen.Binary{Op: sqlgen.OpDiv, L: sqlgen.Lit{Value: int64(10)}, R: sqlgen.Lit{Value: int64(4)}}},
	}})
	if got[0][0] != 2.5 || got[0][1] != int64(2) {
		t.Errorf("10/4.0 and 10/4 = %v, want 2.5 and 2", got[0])
	}
}

func TestExecUpsertsAndReturning(t *testing.T) {
	c := testDB(t)
	put := func(k string, v int64) [][]any {
		return run(t, c, &sqlgen.Insert{Table: "kv", Columns: []string{"k", "v"}, Rows: [][]sqlgen.Expr{{sqlgen.Arg{Value: k}, sqlgen.Arg{Value: v}}},
			OnConflict: &sqlgen.OnConflict{Target: []sqlgen.Col{{Name: "k"}}, Set: []sqlgen.Assign{{Column: "v", Value: sqlgen.Binary{Op: sqlgen.OpAdd, L: sqlgen.Col{Name: "v"}, R: sqlgen.Excluded("v")}}}},
			Returning:  []sqlgen.Column{{Expr: sqlgen.Col{Name: "v"}}}})
	}
	put("a", 2)
	if got := put("a", 5); got[0][0] != int64(7) {
		t.Errorf("upsert returned %v, want 7", got)
	}
	// INSERT … SELECT … ON CONFLICT needs the WHERE the renderer adds.
	run(t, c, &sqlgen.Insert{Table: "kv", Columns: []string{"k", "v"},
		From:       &sqlgen.Select{Columns: []sqlgen.Column{{Expr: name}, {Expr: id}}, From: sqlgen.Table{Name: "items"}, Where: sqlgen.IsNull{X: name, Negate: true}},
		OnConflict: &sqlgen.OnConflict{}})
	rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Agg{Fn: sqlgen.AggCount}}}, From: sqlgen.Table{Name: "kv"}})
	if rows[0][0] != int64(5) { // "a" plus four named items
		t.Errorf("kv has %v rows, want 5", rows[0][0])
	}
	// The same with no WHERE of its own: SQLite needs the one the renderer adds.
	run(t, c, &sqlgen.Insert{Table: "kv", Columns: []string{"k", "v"},
		From: &sqlgen.Select{Columns: []sqlgen.Column{
			{Expr: sqlgen.Binary{Op: sqlgen.OpConcat, L: sqlgen.Col{Name: "k"}, R: sqlgen.Lit{Value: "2"}}}, {Expr: sqlgen.Col{Name: "v"}}},
			From: sqlgen.Table{Name: "kv"}},
		OnConflict: &sqlgen.OnConflict{}})
	if rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Agg{Fn: sqlgen.AggCount}}}, From: sqlgen.Table{Name: "kv"}}); rows[0][0] != int64(10) {
		t.Errorf("kv has %v rows after copying, want 10", rows[0][0])
	}
	del := run(t, c, &sqlgen.Delete{Table: "kv", Where: sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: "k"}, R: sqlgen.Arg{Value: "a"}}, Returning: []sqlgen.Column{{Expr: sqlgen.Col{Name: "v"}}}})
	if len(del) != 1 || del[0][0] != int64(7) {
		t.Errorf("delete returned %v", del)
	}
}

// Every catalog function renders to a call SQLite accepts. (Argument counts
// differ, so this checks the name resolves, using the error text: an unknown
// function and a wrong argument count fail differently.)
func TestExecCatalogFunctionsExistInSQLite(t *testing.T) {
	c := testDB(t)
	for _, f := range sqlgen.DeterministicFuncs() {
		r, err := sqlgen.RenderExpr(sqlgen.SQLite, sqlgen.Func{Name: f})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Exec(context.Background(), "SELECT "+r.SQL)
		if err != nil && strings.Contains(err.Error(), "no such function") {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// An upsert on a case-insensitive unique index needs the collation in its
// conflict target, or SQLite finds no matching constraint.
func TestExecUpsertOnCaseInsensitiveKey(t *testing.T) {
	c := testDB(t)
	if err := c.ExecScript(context.Background(), `CREATE TABLE people (email TEXT, n INTEGER) STRICT;
		CREATE UNIQUE INDEX people_email ON people(email COLLATE NOCASE);`); err != nil {
		t.Fatal(err)
	}
	put := func(email string, n int64) {
		run(t, c, &sqlgen.Insert{Table: "people", Columns: []string{"email", "n"}, Rows: [][]sqlgen.Expr{{sqlgen.Arg{Value: email}, sqlgen.Arg{Value: n}}},
			OnConflict: &sqlgen.OnConflict{Target: []sqlgen.Col{{Name: "email", CI: true}}, Set: []sqlgen.Assign{{Column: "n", Value: sqlgen.Excluded("n")}}}})
	}
	put("Ada@example.com", 1)
	put("ada@EXAMPLE.com", 2)
	rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "email"}}, {Expr: sqlgen.Col{Name: "n"}}}, From: sqlgen.Table{Name: "people"}})
	if len(rows) != 1 || rows[0][0] != "Ada@example.com" || rows[0][1] != int64(2) {
		t.Errorf("people = %v, want one row updated in place", rows)
	}
}
