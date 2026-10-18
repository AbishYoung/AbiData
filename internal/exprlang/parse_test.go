package exprlang

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

var orders = Scope{Mode: Stored, Sources: []Source{{Columns: []Column{
	{Name: "Quantity", Type: sqlgen.Integer},
	{Name: "Unit_price", Type: sqlgen.Currency},
	{Name: "Name", Type: sqlgen.Text, CI: true},
	{Name: "Visits", Type: sqlgen.Integer},
	{Name: "Code", Type: sqlgen.Text},
	{Name: "Credit", Type: sqlgen.Number},
	{Name: "Due", Type: sqlgen.Date},
	{Name: "Unit price", Type: sqlgen.Currency},
}}}}

func render(t *testing.T, e sqlgen.Expr) string {
	t.Helper()
	r, err := sqlgen.RenderExpr(sqlgen.SQLite, e)
	if err != nil {
		t.Fatal(err)
	}
	return r.SQL
}

func TestParseRendersCanonicalSQL(t *testing.T) {
	for src, want := range map[string]string{
		`Quantity * Unit_price`:                `"Quantity" * "Unit_price"`,
		`quantity*unit_price`:                  `"Quantity" * "Unit_price"`,
		`"Unit price" * 2`:                     `"Unit price" * 2`,
		`Name || ' (' || Visits || ')'`:        `(("Name" || ' (') || "Visits") || ')'`,
		`a.b`:                                  ``, // error case below
		`Visits >= 0 AND Credit >= 0`:          `("Visits" >= 0 AND "Credit" >= 0)`,
		`length(Code) > 0`:                     `length("Code") > 0`,
		`Code IN ('a', 'b')`:                   `"Code" IN ('a', 'b')`,
		`Code NOT IN ('a')`:                    `NOT ("Code" IN ('a'))`,
		`Code LIKE 'A%'`:                       `"Code" LIKE 'A%'`,
		`Code NOT LIKE 'A\%' ESCAPE '\'`:       `NOT like('A\%', "Code", '\')`,
		`Visits BETWEEN 1 AND 10`:              `"Visits" BETWEEN 1 AND 10`,
		`Code IS NULL OR Code IS NOT 'x'`:      `("Code" IS NULL OR "Code" IS NOT 'x')`,
		`Code ISNULL`:                          `"Code" IS NULL`,
		`-Visits + +2`:                         `(-"Visits") + 2`,
		`- -Visits`:                            `(-(-"Visits"))`,
		`Visits % 7 = 0`:                       `("Visits" % 7) = 0`,
		`1.50 + 0x10 + 2e3`:                    `(1.50 + 16) + 2e3`,
		`CASE Code WHEN 'a' THEN 1 ELSE 0 END`: `CASE WHEN "Code" = 'a' THEN 1 ELSE 0 END`,
		`CASE WHEN Visits > 1 THEN 'many' END`: `CASE WHEN "Visits" > 1 THEN 'many' END`,
		`CAST(Visits AS TEXT)`:                 `CAST("Visits" AS TEXT)`,
		`coalesce(Credit, 0) * 2`:              `coalesce("Credit", 0) * 2`,
		`NOT Visits > 3`:                       `NOT ("Visits" > 3)`,
		`TRUE AND NOT FALSE`:                   `(1 AND NOT 0)`,
		`'it''s'`:                              `'it''s'`,
		`Visits = 1 == 1`:                      `("Visits" = 1) = 1`,
		`1 + 2 * 3 || 'x'`:                     `1 + (2 * (3 || 'x'))`,
	} {
		if want == "" {
			continue
		}
		r, err := Parse(src, orders)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got := render(t, r.Expr); got != want {
			t.Errorf("%s:\n got %s\nwant %s", src, got, want)
		}
	}
}

func TestParseReportsResultAndReferences(t *testing.T) {
	r, err := Parse(`Quantity * Unit_price + quantity`, orders)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Columns, []string{"Quantity", "Unit_price"}) {
		t.Fatalf("columns %v", r.Columns)
	}
	for src, want := range map[string]sqlgen.Type{
		`Visits + 1`:              sqlgen.Integer,
		`Visits + 1.5`:            sqlgen.Number,
		`Name || 'x'`:             sqlgen.Text,
		`Visits > 1`:              sqlgen.Boolean,
		`Unit_price + Unit_price`: sqlgen.Currency,
		`date(Due, '+1 day')`:     sqlgen.Date,
		`ifnull(Credit, 0)`:       sqlgen.Number,
	} {
		r, err := Parse(src, orders)
		if err != nil || r.Type != want {
			t.Errorf("%s: %v %v, want %v", src, r.Type, err, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for src, want := range map[string]string{
		``:                                       "empty",
		`1); DROP TABLE T; --`:                   "';' is not allowed",
		`V > 0) , CHECK (1`:                      "there is no column named \"V\"",
		`Visits > 0) , CHECK (1`:                 "unexpected ')'",
		`Visits > 0 /* x */`:                     "comments are not allowed",
		`Visits > 0 -- x`:                        "comments are not allowed",
		`:p > 1`:                                 "parameters are not allowed",
		`? > 1`:                                  "parameters are not allowed",
		`[Visits] > 1`:                           "double quotes",
		"`Visits` > 1":                           "double quotes",
		`Visits IN (SELECT id FROM _sys_tables)`: "subqueries are not allowed",
		`(SELECT 1)`:                             "subqueries are not allowed",
		`EXISTS (SELECT 1)`:                      "subqueries are not allowed",
		`Vists + 1`:                              `did you mean "Visits"?`,
		`lenght(Code)`:                           `"lenght" is not a function; did you mean "length"?`, //nolint:misspell // the typo is the test
		`ifnul(Credit, 0)`:                       `did you mean "ifnull"?`,
		`load_extension('x')`:                    "is not a function",
		`random()`:                               "gives a different value each time",
		`uuid()`:                                 "is not a function",
		`zeroblob(10)`:                           "is not a function",
		`substr(Code)`:                           "substr() takes 2 to 3 values, not 1",
		`replace(Code, 'a')`:                     "replace() takes 3 values, not 2",
		`sum(Visits)`:                            "combines many rows",
		`count(*)`:                               "combines many rows",
		`date('now')`:                            "only be used in a default value",
		`CURRENT_TIMESTAMP`:                      "only be used in a default value",
		`strftime('%Y')`:                         "only be used in a default value",
		`Visits +`:                               "ends too early",
		`(Visits`:                                "expected ')'",
		`CASE WHEN 1 THEN 2`:                     "expected END",
		`CAST(Visits AS NUMERIC)`:                "use REAL or INTEGER",
		`'open`:                                  "not closed",
		`"Name`:                                  "not closed",
		`Visits Credit`:                          "unexpected 'Credit'",
		`1abc`:                                   "cannot run into a name",
		`Code REGEXP 'x'`:                        "not supported",
		`Visits(1)`:                              `is not a function`,
		`x.Visits`:                               `no source named "x"`,
		`(1, 2)`:                                 "only allowed after IN",
		`Visits IS DISTINCT FROM 1`:              "IS NOT",
		`abidata_convert('text','integer',A)`:    "is not a function",
		`length(zeroblob(10))`:                   "is not a function",
	} {
		_, err := Parse(src, orders)
		var pe *Error
		if !errors.As(err, &pe) {
			t.Errorf("%q: got %v, want an *Error containing %q", src, err, want)
			continue
		}
		if !strings.Contains(pe.Msg, want) && !strings.Contains(pe.Error(), want) {
			t.Errorf("%q: %v, want %q", src, err, want)
		}
	}
}

func TestErrorPositionsCountCharacters(t *testing.T) {
	_, err := Parse(`'é' || Vists`, orders)
	var pe *Error
	if !errors.As(err, &pe) || pe.Column != 8 {
		t.Fatalf("%v (column %d), want column 8", err, pe.Column)
	}
}

func TestDefaultsAndQueries(t *testing.T) {
	def := Scope{Mode: Stored, AllowNow: true}
	for _, src := range []string{`strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, `'n'`, `0`, `CURRENT_DATE`, `date()`} {
		if _, err := Parse(src, def); err != nil {
			t.Errorf("default %s: %v", src, err)
		}
	}
	if _, err := Parse(`Visits + 1`, def); err == nil || !strings.Contains(err.Error(), "cannot refer to columns") {
		t.Errorf("default with a column: %v", err)
	}

	q := Scope{Mode: Query, Sources: []Source{
		{Alias: "o", Columns: []Column{{Name: "Qty", Type: sqlgen.Integer}, {Name: "id", Type: sqlgen.Integer}}},
		{Alias: "c", Columns: []Column{{Name: "Name", Type: sqlgen.Text}, {Name: "id", Type: sqlgen.Integer}}},
	}}
	r, err := Parse(`sum(o.Qty) * 2 + count(DISTINCT c.id) + random()`, q)
	if err != nil || !r.Aggregate {
		t.Fatalf("%+v %v", r, err)
	}
	if got := render(t, r.Expr); got != `((SUM("o"."Qty") * 2) + COUNT(DISTINCT "c"."id")) + random()` {
		t.Fatalf("got %s", got)
	}
	if r, err := Parse(`max(Qty, 3)`, q); err != nil || r.Aggregate {
		t.Fatalf("scalar max: %+v %v", r, err)
	}
	for src, want := range map[string]string{
		`id + 1`:                      "more than one source",
		`sum(count(Qty))`:             "cannot be nested",
		`group_concat(DISTINCT Name)`: "DISTINCT is not supported",
		`count(*, 1)`:                 "expected ')'",
		`sum(*)`:                      "only count(*)",
	} {
		if _, err := Parse(src, q); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", src, err, want)
		}
	}
	if r, err := Parse(`group_concat(Name, ', ')`, q); err != nil || !r.Aggregate {
		t.Fatalf("group_concat: %+v %v", r, err)
	}
}

func TestNestingIsBounded(t *testing.T) {
	deep := strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200)
	if _, err := Parse(deep, orders); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("%v", err)
	}
	if _, err := Parse(strings.Repeat("-", 300)+"1", orders); err == nil {
		t.Fatal("deep unary accepted")
	}
	if _, err := Parse(strings.Repeat("x", MaxLen+1), orders); err == nil {
		t.Fatal("over-long accepted")
	}
}

// TestRenderedMeansTheSame evaluates each expression as written and as
// rendered on SQLite, over rows with NULLs, mixed case and edge values: the
// parse must not change what an existing expression computes.
func TestRenderedMeansTheSame(t *testing.T) {
	ctx := context.Background()
	c, err := sqlitex.Open(ctx, filepath.Join(t.TempDir(), "t.db"), sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.ExecScript(ctx, `
		CREATE TABLE t ("Quantity" INTEGER, "Unit_price" INTEGER, "Name" TEXT COLLATE NOCASE, "Visits" INTEGER,
			"Code" TEXT, "Credit" REAL, "Due" TEXT, "Unit price" INTEGER);
		INSERT INTO t VALUES (3, 25000, 'Ada', 2, 'A-1', 1.5, '2026-02-28', 10000),
			(NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
			(0, -1, 'bob', 0, 'a%_', -0.0, '2024-02-29', 0),
			(-7, 9223372036854775807, 'ÉMILE', 99, '', 1e300, 'not a date', 1);`); err != nil {
		t.Fatal(err)
	}
	exprs := []string{
		`Quantity * Unit_price`, `Name || ' (' || Visits || ')'`, `Name || ''`, `coalesce(Quantity, 0) * 2`,
		`Visits >= 0`, `Credit >= 0`, `length(Code) > 0`, `length(Name) = 3`, `Code LIKE 'a%'`, `Code GLOB 'A*'`,
		`Code NOT LIKE 'a\%\_' ESCAPE '\'`, `Name = 'ADA'`, `Name IN ('ada', 'BOB')`, `Visits BETWEEN 1 AND 50`,
		`NOT Visits BETWEEN 1 AND 50`, `Code IS NULL`, `Code IS NOT 'A-1'`, `Code IS 'A-1'`, `-Visits * - -2`,
		`Visits % 3`, `Visits / 2`, `Credit / 2`, `1 + 2 * 3 || 'x'`, `Visits = 2 == 1`, `Visits < 3 = 1`,
		`CASE Code WHEN 'A-1' THEN 'one' WHEN '' THEN 'empty' ELSE 'other' END`,
		`CASE WHEN Visits > 1 THEN Visits END`, `CAST(Credit AS INTEGER)`, `CAST(Visits AS TEXT) || 'x'`,
		`CAST(Code AS REAL)`, `iif(Visits > 1, 'y', 'n')`, `ifnull(Name, '?')`, `nullif(Visits, 0)`,
		`round(Credit, 1)`, `abs(Quantity)`, `upper(Name)`, `substr(Code, 2)`, `replace(Code, '-', '')`,
		`trim('  ' || Code || '  ')`, `instr(Code, '-')`, `date(Due, '+1 month')`, `strftime('%m', Due)`,
		`julianday(Due) - julianday('2026-01-01')`, `printf('%05.1f', Credit)`, `max(Visits, Quantity, 1)`,
		`min(Visits, 5)`, `Visits > 1 AND Credit > 1 OR Code = ''`, `NOT (Visits > 1 OR Code IS NULL)`,
		`TRUE`, `FALSE OR NULL`, `0x1F + 1.50`, `"Unit price" + 1`, `typeof(Credit)`, `hex(Code)`,
		`Code || NULL`, `json_valid('{"a":1}')`, `json_extract('{"a":[1,2]}', '$.a[1]')`, `unicode(Name)`,
		`9223372036854775807 + 1`, `'a' < 'B'`, `Name < 'b'`, `Visits IN (2, NULL)`,
		`Visits NOT IN (2, NULL)`,
	}
	for _, src := range exprs {
		r, err := Parse(src, orders)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		rendered := render(t, r.Expr)
		var got, want []string
		for _, pair := range []struct {
			sql string
			out *[]string
		}{{src, &want}, {rendered, &got}} {
			rows, err := c.Query(ctx, `SELECT quote(`+pair.sql+`), typeof(`+pair.sql+`) FROM t ORDER BY rowid`)
			if err != nil {
				t.Errorf("%s (%s): %v", src, pair.sql, err)
				break
			}
			for rows.Next() {
				v := rows.Values()
				*pair.out = append(*pair.out, fmt.Sprint(v...))
			}
			rows.Close()
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s\nrendered %s\n got %v\nwant %v", src, rendered, got, want)
		}
	}
}
