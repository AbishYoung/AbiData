package pgdb

// ADR 0013 Phase F: differential tests. The same sqlgen model, rendered for
// SQLite and for PostgreSQL, must give the same answers on the same data. The
// tables mirror each other the way the catalog stores user tables: text is
// binary on both (TEXT on SQLite, COLLATE "C" on PostgreSQL) and only sorting
// and matching ignore case; yes/no is boolean on PostgreSQL and 0/1 on SQLite;
// numbers are double precision; dates and geometry are native on PostgreSQL.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/dbx"
	"abidata/internal/exprlang"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

// items holds the rows both engines get: mixed case, NULLs, negatives,
// fractions, LIKE's wildcards and non-ASCII text.
var items = []struct {
	name, note  any
	qty         any
	price       any
	active      any
	due, at     any // a date and a date and time, as SQLite stores them
	pt, zone    any // a location and an area, canonical GeoJSON
	doc         any // a JSON document, its keys in jsonb's order (by length, then bytes)
	description string
}{
	{"Apple", "50% off", int64(3), 1.25, true, "2026-01-31", "2026-10-18T09:30:15.250Z", `{"type":"Point","coordinates":[-0.1278,51.5074]}`, `{"type":"Polygon","coordinates":[[[-1,51],[1,51],[1,52],[-1,52],[-1,51]]]}`, `{"n":3,"ok":true,"tags":["a","b"],"price":1.5,"colour":"red"}`, "plain"},
	{"banana", "50 percent", nil, 0.5, false, "2024-02-29", "2024-02-29T23:59:59.999Z", `{"type":"Point","coordinates":[2.3522,48.8566]}`, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]],[[0.25,0.25],[0.25,0.75],[0.75,0.75],[0.75,0.25],[0.25,0.25]]]}`, `{"n":"5","ok":false,"nested":{"x":[1,{"y":null}]}}`, "lower case, no quantity, leap day"},
	{"Cherry", "a_b", int64(10), -2.5, true, "2025-12-31", "2026-01-01T00:00:00.000Z", `{"type":"Point","coordinates":[0.5,0.5]}`, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`, `[1,2.5,"three",null,true]`, "negative price, year end"},
	{"apricot", "axb", int64(-7), 2.675, nil, nil, nil, nil, nil, `"just text"`, "negative quantity, unknown flag, no dates"},
	{nil, nil, int64(0), nil, false, "1970-01-01", "1969-12-31T12:00:00.000Z", `{"type":"Point","coordinates":[1,0.25,12.5]}`, `{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]],[[[2,2],[3,2],[3,3],[2,3],[2,2]]]]}`, nil, "NULL text, epoch"},
	{"Éclair", "crème brûlée", int64(1), 100.0, true, "2000-03-01", "2000-02-28T12:00:00.001Z", `{"type":"Point","coordinates":[0,0]}`, `{"type":"Polygon","coordinates":[[[0,0],[0.0000001,0],[0.0000001,0.0000001],[0,0.0000001],[0,0]]]}`, `{"n":-2,"a.b":1,"tags":[]}`, "non-ASCII, century leap year"},
	{"date", "[x]*?", int64(2), 1e-3, false, "2026-07-05", "2026-07-05T00:00:00.000Z", `{"type":"Point","coordinates":[-73.9857,40.7484]}`, `{"type":"Polygon","coordinates":[[[-74.1,40.6],[-73.8,40.6],[-73.8,40.9],[-74.1,40.9],[-74.1,40.6]]]}`, `{"n":0,"big":12345678901234}`, "glob characters, a Sunday"},
}

type diffEnv struct {
	lite *sqlitex.Conn
	pg   *pgxpool.Conn
	d    sqlgen.Dialect // the server's
}

func diffSetup(t *testing.T) *diffEnv {
	t.Helper()
	e := setup(t)
	ctx := context.Background()
	d := Pool(e.app)
	if err := InstallLib(ctx, d, e.a); err != nil {
		t.Fatal(err)
	}
	s := ident(e.a.Schema())
	for _, q := range []string{
		`CREATE TABLE ` + s + `.items (id bigint PRIMARY KEY, name text COLLATE "C", qty bigint,
			price double precision, active boolean, note text COLLATE "C", due date, at timestamptz, pt jsonb, zone jsonb, doc jsonb)`,
	} {
		if _, err := d.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	lite, err := sqlitex.Open(ctx, filepath.Join(t.TempDir(), "t.db"), sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() })
	if err := lite.ExecScript(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, qty INTEGER,
		price REAL, active INTEGER, note TEXT, due TEXT, at TEXT, pt TEXT, zone TEXT, doc TEXT) STRICT`); err != nil {
		t.Fatal(err)
	}
	for i, it := range items {
		var flag any
		if b, ok := it.active.(bool); ok {
			flag = map[bool]int64{false: 0, true: 1}[b]
		}
		if _, err := lite.Exec(ctx, `INSERT INTO items VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, i+1, it.name, it.qty, it.price, flag, it.note, it.due, it.at, it.pt, it.zone, it.doc); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(ctx, `INSERT INTO `+s+`.items VALUES (?, ?, ?, ?, ?, ?, CAST(? AS date), CAST(? AS timestamptz), CAST(? AS jsonb), CAST(? AS jsonb), CAST(? AS jsonb))`, i+1, it.name, it.qty, it.price, it.active, it.note, it.due, it.at, it.pt, it.zone, it.doc); err != nil {
			t.Fatal(err)
		}
	}
	pg, err := e.app.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pg.Release)
	if _, err := pg.Exec(ctx, `SET search_path = `+s+`, `+ident(e.a.Lib())); err != nil {
		t.Fatal(err)
	}
	dialect, err := Dialect(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	return &diffEnv{lite: lite, pg: pg, d: dialect}
}

// rows runs a rendered model on both engines.
func (e *diffEnv) rows(t *testing.T, s sqlgen.Stmt) (lite, pg [][]any, pgSQL string, err error) {
	t.Helper()
	ctx := context.Background()
	lr, err := sqlgen.Render(sqlgen.SQLite, s)
	if err != nil {
		t.Fatalf("SQLite render: %v", err)
	}
	pr, err := sqlgen.Render(e.d, s)
	if err != nil {
		return nil, nil, "", err
	}
	got, err := e.lite.Query(ctx, lr.SQL, lr.Args...)
	if err != nil {
		t.Fatalf("SQLite: %s: %v", lr.SQL, err)
	}
	if lite, err = collect(got); err != nil {
		t.Fatalf("SQLite: %s: %v", lr.SQL, err)
	}
	pgRows, err := Wrap(e.pg).Query(ctx, pr.SQL, pr.Args...)
	if err != nil {
		return lite, nil, pr.SQL, err
	}
	pg, err = collect(pgRows)
	return lite, pg, pr.SQL, err
}

func collect(r dbx.Rows) ([][]any, error) {
	defer r.Close()
	var out [][]any
	for r.Next() {
		out = append(out, r.Values())
	}
	return out, r.Err()
}

// same compares one value from each engine. A PostgreSQL value must be one
// the codecs know (no numeric, which would mean a SUM or literal escaped
// double precision); booleans compare as SQLite's 0 and 1, numbers within a
// rounding error.
func same(lite, pg any) (bool, string) { return sameWithin(lite, pg, 1e-9) }

// sameWithin is same with a relative tolerance for numbers.
func sameWithin(lite, pg any, tol float64) (bool, string) {
	if t, ok := pg.(time.Time); ok {
		// A date or timestamptz column read back; SQLite holds the text,
		// in any of the forms its date functions write.
		s, ok := lite.(string)
		if !ok {
			return false, fmt.Sprintf("PostgreSQL returned a time, SQLite %T", lite)
		}
		for _, layout := range []string{time.DateOnly, dbx.TimeLayout, time.DateTime, "2006-01-02 15:04:05.000"} {
			if lt, err := time.Parse(layout, s); err == nil {
				if layout == time.DateOnly {
					return t.Format(time.DateOnly) == s, ""
				}
				return t.Equal(lt), ""
			}
		}
		if len(s) == 11 && s[0] == '-' && t.Year() < 0 {
			// A date before year 1. Both SQLite and Go count years
			// astronomically: -4713 is 4714 BC.
			return fmt.Sprintf("-%04d-%s", -t.Year(), t.Format("01-02")) == s, ""
		}
		return false, "unreadable SQLite time " + s
	}
	switch pg.(type) {
	case map[string]any, []any:
		// A jsonb column read back, compared as JSON values.
		var lv any
		s, _ := lite.(string)
		if err := json.Unmarshal([]byte(s), &lv); err != nil {
			return false, "SQLite holds no JSON"
		}
		return reflect.DeepEqual(lv, pg), ""
	}
	switch pg.(type) {
	case nil, bool, int64, int32, int16, float64, float32, string:
	default:
		return false, fmt.Sprintf("PostgreSQL returned %T", pg)
	}
	norm := func(v any) any {
		switch x := v.(type) {
		case bool:
			if x {
				return 1.0
			}
			return 0.0
		case int64:
			return float64(x)
		case int32:
			return float64(x)
		case int16:
			return float64(x)
		case float32:
			return float64(x)
		}
		return v
	}
	a, b := norm(lite), norm(pg)
	if fa, ok := a.(float64); ok {
		if fb, ok := b.(float64); ok {
			if fa == fb || math.Abs(fa-fb) <= tol*math.Max(math.Abs(fa), math.Abs(fb)) {
				return true, ""
			}
		}
	}
	return a == b, ""
}

var itemColumns = []exprlang.Column{
	{Name: "id", Type: sqlgen.Integer},
	{Name: "name", Type: sqlgen.Text, CI: true},
	{Name: "qty", Type: sqlgen.Integer},
	{Name: "price", Type: sqlgen.Number},
	{Name: "active", Type: sqlgen.Boolean},
	{Name: "note", Type: sqlgen.Text, CI: true},
	{Name: "due", Type: sqlgen.Date},
	{Name: "at", Type: sqlgen.DateTime},
	{Name: "pt", Type: sqlgen.GeoPoint},
	{Name: "zone", Type: sqlgen.GeoArea},
	{Name: "doc", Type: sqlgen.JSON},
}

// expressionCases are AbiData expressions over the items columns, evaluated
// per row by the differential tests.
var expressionCases = []string{
	// operators
	"qty + 1", "qty - price", "qty * 2", "qty / 2", "qty / 0", "price / 0", "qty % 3", "price % 2", "qty % 0",
	"-qty", "-price", "qty * price", "7 / 2", "7.0 / 2", "qty || '!'", "name || ' ' || note", "active + 1",
	"qty = 3", "qty <> 3", "qty > 2 AND price < 2", "qty < 0 OR active", "NOT active", "active = 1", "active = 0",
	"qty IS NULL", "qty IS NOT NULL", "qty IS 3", "qty IS NOT 3", "name IS NULL", "active IS NULL",
	"qty BETWEEN 1 AND 5", "qty IN (1, 2, 3)", "qty NOT IN (1, 2, 3)", "name = 'APPLE'", "name < 'B'",
	"name LIKE 'a%'", "name LIKE '%E%'", "note LIKE '50%'", "name GLOB 'a*'", "name GLOB '[A-C]*'", "note GLOB '*[[]x]*'",
	"name NOT LIKE 'a%'",
	"CASE WHEN qty > 2 THEN 'many' WHEN qty > 0 THEN 'some' ELSE 'none' END",
	"CASE WHEN active THEN price END",
	"CAST(price AS INTEGER)", "CAST(qty AS REAL) / 2", "CAST(qty AS TEXT)",
	// text functions
	"lower(name)", "upper(note)", "length(name)", "length(qty)", "trim('  x  ')", "ltrim('xxy', 'x')",
	"rtrim(note, 'f')", "replace(note, '0', 'o')", "substr(name, 2)", "substr(name, 2, 3)", "substr(name, -3)",
	"substr(name, -3, 2)", "substr(name, 0, 2)", "substr(name, 3, -2)", "substr(name, 1, 0)", "instr(name, 'a')",
	"instr(note, '')", "concat(name, qty)", "concat_ws('-', name, note)", "unicode(name)", "hex(name)", "hex(qty)",
	"char(72, 105)", "coalesce(qty, -1)", "ifnull(name, '?')", "nullif(qty, 0)",
	"iif(qty > 2, 'big', 'small')", "iif(active, 1, 0)", "max(qty, 2)", "min(price, 1)", "max(qty, price)",
	// numbers
	"abs(qty)", "abs(price)", "round(price)", "round(price, 1)", "round(price, 2)", "round(qty)", "ceil(price)",
	"floor(price)", "ceil(qty)", "trunc(price)", "sign(price)", "sign(qty)", "sqrt(qty)", "sqrt(price)",
	"exp(qty)", "ln(qty)", "log(qty)", "log(2, qty)", "log10(price)", "log2(qty)", "pow(qty, 2)",
	"power(price, 0.5)", "pow(0, -1)", "mod(qty, 4)", "mod(price, 1)", "mod(qty, 0)", "pi()", "degrees(price)",
	// dates and times
	"date(due)", "date(at)", "datetime(at)", "time(at)", "time(at, 'subsec')", "datetime(at, 'subsec')",
	"date(due, '+1 month')", "date(due, '-1 month')", "date(due, '+1 year')", "date(due, '+1 year', '-1 day')",
	"date(due, '+1.5 months')", "date(due, '-3 days')", "date(due, 'start of month')", "date(due, 'start of year')",
	"date(due, 'weekday 0')", "date(due, 'weekday 3')", "datetime(at, '+90 minutes')", "datetime(at, '-30 seconds')",
	"datetime(at, 'start of day', '+1 day', '-1 second')", "datetime(at, '+01:30')", "datetime(at, '-12:00:00.5')",
	"julianday(due)", "julianday(at) - julianday(due)", "unixepoch(at)", "unixepoch(at, 'subsec')", "unixepoch(due)",
	"strftime('%Y-%m', due)", "strftime('%d.%m.%Y', due)", "strftime('%j %W %U %w %u', due)", "strftime('%V %G %g', due)",
	"strftime('%H:%M:%f %p %P %I %l %k', at)", "strftime('%s', at)", "strftime('%J', at)", "strftime('%e|%F|%T|%R|%%', at)",
	"strftime('%Q', at)", "date(qty * 100000000, 'unixepoch')", "datetime(price * 1000000000, 'unixepoch')",
	"date(2460000.5)", "date('2026-02-30')", "date('2026-13-01')", "date('garbage')", "date(due, 'nonsense')",
	"datetime('2026-03-04 05:06')", "datetime('2026-03-04T05:06:07+02:00')", "time('12:34')", "date(due, 'auto')",
	"date(price, 'auto')", "date(due, 'unixepoch')", "date(due, '+1 day', 'unixepoch')",
	"date(qty * 1000000, 'start of month')", "date(NULL, '+1 day')", "date(due, NULL)",
	"due < '2026-06-01'", "due = '2024-02-29'", "at > '2026-01-01T00:00:00.000Z'", "due = date(at)",
	"date(due) < date(at)", "julianday(at) > 2461000", "due || ''", "at || ''", "length(at)",
	// locations and areas
	"geo_lat(pt)", "geo_lon(pt)", "geo_point(price, qty)", "geo_point(51.5, -0.1278)", "geo_point(91, 0)",
	"geo_distance_m(pt, geo_point(51.5, -0.12))", "geo_distance_m(pt, '48.85, 2.35')", "geo_within(pt, zone)",
	"geo_within(geo_point(0.5, 0.5), zone)", "geo_within(geo_point(0.25, 0.5), zone)", "geo_within('0.5, 0', zone)",
	"geo_area_m2(zone)", "geo_centroid(zone)", "geo_bbox(zone)", "pt || ''", "zone || ''", "length(zone)",
	"geo_lat('51.5, -0.12')", "geo_lat('51.5, -0.12, 30')", "geo_lon('POINT(-0.12 51.5)')", "geo_lat('{\"lat\": \"1.5\", \"lng\": 2}')",
	"geo_lat('{\"type\":\"Feature\",\"geometry\":{\"type\":\"Point\",\"coordinates\":[3,4]}}')",
	"geo_lat('garbage')", "geo_lat('91, 0')", "geo_lat(name)", "geo_area_m2(note)",
	"geo_area_m2('{\"type\":\"Polygon\",\"coordinates\":[[[170,0],[-170,0],[-170,1],[170,0]]]}')",
	"geo_area_m2('{\"type\":\"Polygon\",\"coordinates\":[[[0,0],[1,1],[2,2],[0,0]]]}')",
	"geo_centroid('{\"type\":\"Polygon\",\"coordinates\":[[[0,0],[3,0],[3,1],[0,1]]]}')",
	"radians(qty)", "sin(qty)", "cos(price)", "tan(qty)", "atan(price)", "atan2(qty, price)", "acos(price)",
	"asin(price)", "acosh(qty)", "asinh(price)", "atanh(price)", "sinh(price)", "cosh(price)", "tanh(qty)",
	// JSON
	"json_extract(doc, '$.n')", "json_extract(doc, '$.n') + 1", "json_extract(doc, '$.price') * 2",
	"json_extract(doc, '$.colour') || '!'", "json_extract(doc, '$.tags')", "json_extract(doc, '$.tags[1]')",
	"json_extract(doc, '$.tags[#-1]')", "json_extract(doc, '$.tags[#]')", "json_extract(doc, '$[2]')",
	"json_extract(doc, '$.nested.x[1].y')", "json_extract(doc, '$.nested.x[1]')", `json_extract(doc, '$."a.b"')`,
	"json_extract(doc, '$.ok')", "json_extract(doc, '$.n', '$.ok')", "json_extract(doc, '$')", "json_extract(doc, '$.big')",
	"json_extract(doc, '$.n') = 3", "json_extract(doc, '$.colour') = 'red'", "json_extract(doc, '$.ok') = 1",
	"json_extract(doc, '$.n') > qty", "iif(json_extract(doc, '$.ok'), 'yes', 'no')",
	"coalesce(json_extract(doc, '$.price'), 0)", "CAST(json_extract(doc, '$.n') AS INTEGER)",
	"CAST(json_extract(doc, '$.price') AS TEXT)", "CASE WHEN json_extract(doc, '$.ok') THEN json_extract(doc, '$.n') ELSE 0 END",
	`json_extract('{"a":[1,2]}', '$.a[0]')`, "json_extract(pt, '$.type')", "json_extract(zone, '$.coordinates[0][0][0]')",
	"json_type(doc)", "json_type(doc, '$.n')", "json_type(doc, '$.tags')", "json_type(doc, '$.price')",
	"json_type(doc, '$.nested.x[1].y')", "json_type(doc, '$.missing')", "json_array_length(doc)",
	"json_array_length(doc, '$.tags')", "json_array_length(doc, '$.missing')", "json_valid(doc)", "json_valid(note)",
	`json_valid('{"a":1}')`, "json_valid(qty)", "json(doc)", `json(' { "a" : [ 1 , 2 ] } ')`, "doc || ''", "length(doc)",
}

// TestExpressionsAgree evaluates AbiData expressions on every row on both
// engines.
func TestExpressionsAgree(t *testing.T) {
	e := diffSetup(t)
	scope := exprlang.Scope{Mode: exprlang.Query, Sources: []exprlang.Source{{Columns: itemColumns}}}
	for _, src := range expressionCases {
		r, err := exprlang.Parse(src, scope)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		stmt := &sqlgen.Select{
			Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}, {Expr: asText(r.Expr, r.Type), As: "v"}},
			From:    sqlgen.Table{Name: "items"},
			OrderBy: []sqlgen.Order{{Expr: sqlgen.Col{Name: "id"}}},
		}
		e.agree(t, src, stmt, knownDifferences[src]...)
	}
}

// asText compares a JSON result as its text: jsonb reads back as Go values, in
// which a JSON string and text look alike.
func asText(e sqlgen.Expr, t sqlgen.Type) sqlgen.Expr {
	if t != sqlgen.JSON {
		return e
	}
	return sqlgen.Binary{Op: sqlgen.OpConcat, L: e, R: sqlgen.Lit{Value: "", Type: sqlgen.Text}}
}

// knownDifferences are the rows (1-based) where the engines are allowed to
// disagree, each for a stated reason (ADR 0013 §9). A "stored " key is the
// expression as a generated column.
var knownDifferences = map[string][]int{
	// jsonb keeps a number's value, not its spelling: 1e-7 reads back as
	// 0.0000001.
	"stored geo_bbox(zone)": {6},
	// A whole double as text: 3 on PostgreSQL, 3.0 on SQLite (the codecs'
	// number formatting, ADR 0013 §3).
	"stored json_extract(doc, '$.price') * 2": {1},
}

// tolerances loosen the comparison of numbers for an expression. A 1 cm²
// area's spherical-excess sum cancels almost completely, so the last bits of
// the platform's sin show: a relative 2e-8 on some C libraries, none on others.
var tolerances = map[string]float64{
	"geo_area_m2(zone)": 1e-6,
}

// TestStatementsAgree covers what is not a per-row expression: grouping and
// aggregates, case-insensitive sorting with explicit NULL placement, text
// matching and upserts.
func TestStatementsAgree(t *testing.T) {
	e := diffSetup(t)
	scope := exprlang.Scope{Mode: exprlang.Query, Sources: []exprlang.Source{{Columns: itemColumns}}}
	expr := func(src string) sqlgen.Expr {
		r, err := exprlang.Parse(src, scope)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		return r.Expr
	}
	col := func(name string) sqlgen.Expr {
		for _, c := range itemColumns {
			if c.Name == name {
				return sqlgen.Col{Name: name, Type: c.Type, CI: c.CI}
			}
		}
		panic(name)
	}
	items := sqlgen.Table{Name: "items"}
	byActive := []sqlgen.Order{{Expr: col("active"), Nulls: sqlgen.NullsFirst}}
	cases := map[string]sqlgen.Stmt{
		"aggregates": &sqlgen.Select{Columns: []sqlgen.Column{
			{Expr: expr("count(*)")}, {Expr: expr("count(qty)")}, {Expr: expr("sum(qty)")}, {Expr: expr("avg(qty)")},
			{Expr: expr("sum(price)")}, {Expr: expr("avg(price)")}, {Expr: expr("min(qty)")}, {Expr: expr("max(price)")},
			{Expr: expr("sum(active)")}, {Expr: expr("total(qty)")}, {Expr: expr("min(name)")}, {Expr: expr("max(name)")},
			{Expr: expr("count(DISTINCT active)")},
		}, From: items},
		"group by yes/no": &sqlgen.Select{Columns: []sqlgen.Column{
			{Expr: col("active")}, {Expr: expr("count(*)")}, {Expr: expr("sum(qty)")}, {Expr: expr("min(active)")}, {Expr: expr("max(active)")},
		}, From: items, GroupBy: []sqlgen.Expr{col("active")}, OrderBy: byActive},
		"having": &sqlgen.Select{Columns: []sqlgen.Column{{Expr: col("active")}, {Expr: expr("count(*)")}},
			From: items, GroupBy: []sqlgen.Expr{col("active")}, Having: expr("count(*) > 1"), OrderBy: byActive},
		"case-insensitive sort, NULLs last": &sqlgen.Select{Columns: []sqlgen.Column{{Expr: col("id")}, {Expr: col("name")}},
			From: items, OrderBy: []sqlgen.Order{{Expr: col("name"), Nulls: sqlgen.NullsLast}}},
		"descending, NULLs first": &sqlgen.Select{Columns: []sqlgen.Column{{Expr: col("id")}},
			From: items, OrderBy: []sqlgen.Order{{Expr: col("qty"), Desc: true, Nulls: sqlgen.NullsFirst}, {Expr: col("id")}}},
		"limit and offset": &sqlgen.Select{Columns: []sqlgen.Column{{Expr: col("id")}}, From: items,
			OrderBy: []sqlgen.Order{{Expr: col("id")}}, Limit: sqlgen.Arg{Value: 2, Type: sqlgen.Integer}, Offset: sqlgen.Arg{Value: 1, Type: sqlgen.Integer}},
	}
	for _, m := range []struct {
		kind    sqlgen.MatchKind
		pattern string
	}{{sqlgen.MatchContains, "%"}, {sqlgen.MatchContains, "_"}, {sqlgen.MatchStarts, "AP"}, {sqlgen.MatchEnds, "Y"}, {sqlgen.MatchLike, "c%"}} {
		for _, arg := range []bool{false, true} {
			var p sqlgen.Expr = sqlgen.Lit{Value: m.pattern, Type: sqlgen.Text}
			if arg {
				p = sqlgen.Arg{Value: m.pattern, Type: sqlgen.Text}
			}
			cases[fmt.Sprintf("match %d %q arg=%v", m.kind, m.pattern, arg)] = &sqlgen.Select{
				Columns: []sqlgen.Column{{Expr: col("id")}}, From: items,
				Where:   sqlgen.Or{sqlgen.Match{Kind: m.kind, X: col("name"), Pattern: p, CI: true}, sqlgen.Match{Kind: m.kind, X: col("note"), Pattern: p, CI: true}},
				OrderBy: []sqlgen.Order{{Expr: col("id")}},
			}
		}
	}
	for name, s := range cases {
		e.agree(t, name, s)
	}
}

func (e *diffEnv) agree(t *testing.T, name string, s sqlgen.Stmt, allowed ...int) {
	t.Helper()
	lite, pg, pgSQL, err := e.rows(t, s)
	if err != nil {
		t.Errorf("%s: PostgreSQL: %v\n%s", name, err, pgSQL)
		return
	}
	if len(lite) != len(pg) {
		t.Errorf("%s: %d rows on SQLite, %d on PostgreSQL\n%s", name, len(lite), len(pg), pgSQL)
		return
	}
	var diffs []string
	for i := range lite {
		if slices.Contains(allowed, i+1) {
			if ok, _ := same(lite[i][1], pg[i][1]); ok {
				diffs = append(diffs, fmt.Sprintf("row %d now agrees; drop it from knownDifferences", i+1))
			}
			continue
		}
		for j := range lite[i] {
			tol, ok := tolerances[strings.TrimPrefix(name, "stored ")]
			if !ok {
				tol = 1e-9
			}
			if ok, why := sameWithin(lite[i][j], pg[i][j], tol); !ok {
				diffs = append(diffs, fmt.Sprintf("row %d col %d: SQLite %#v, PostgreSQL %#v %s", i+1, j+1, lite[i][j], pg[i][j], why))
			}
		}
	}
	if len(diffs) > 0 {
		t.Errorf("%s:\n  %s\n  %s", name, strings.Join(diffs, "\n  "), pgSQL)
	}
}
