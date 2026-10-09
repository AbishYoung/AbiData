package records

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
)

func testDecoder(c catalog.Column, v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case float64:
		if x == float64(int64(x)) {
			return int64(x), nil
		}
		return x, nil
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		return x.Float64()
	case string:
		return x, nil
	case bool:
		if x {
			return int64(1), nil
		}
		return int64(0), nil
	}
	return nil, fault.New(fault.BadRequest, "bad value")
}

func testTable() *catalog.Table {
	return &catalog.Table{ID: "T", PhysicalName: "t", Columns: []catalog.Column{
		{ID: "cid", PhysicalName: "id", Type: catalog.Autonumber},
		{ID: "cname", PhysicalName: "name", Type: catalog.ShortText},
		{ID: "cnotes", PhysicalName: "x_1", Type: catalog.LongText},
		{ID: "cord", PhysicalName: "Order", Type: catalog.Integer},
		{ID: "cdone", PhysicalName: "done", Type: catalog.YesNo},
		{ID: "clink", PhysicalName: "link", Type: catalog.Hyperlink},
		{ID: "ccalc", PhysicalName: "calc", Type: catalog.Calculated},
		{ID: "ccalcn", PhysicalName: "calcn", Type: catalog.Calculated, Options: catalog.TypeOptions{ResultType: "integer"}},
		{ID: "cprice", PhysicalName: "price", Type: catalog.Currency},
	}}
}

func isBadRequest(err error) bool {
	var fe *fault.Error
	return errors.As(err, &fe) && fe.Code == fault.BadRequest
}

func TestCompileFilter(t *testing.T) {
	tbl := testTable()
	c := func(col string, op FilterOp, v any) Filter { return Filter{Col: col, Op: op, Value: v} }
	tests := []struct {
		name  string
		f     *Filter
		where string
		args  []any
	}{
		{"nil", nil, "", nil},
		{"empty", &Filter{}, "", nil},
		{"eq", &Filter{Col: "cname", Op: OpEq, Value: "bob"}, `"name" = ?`, []any{"bob"}},
		{"ne", &Filter{Col: "cname", Op: OpNe, Value: "bob"}, `"name" IS NOT ?`, []any{"bob"}},
		{"lt", &Filter{Col: "cord", Op: OpLt, Value: 3}, `"Order" < ?`, []any{int64(3)}},
		{"le", &Filter{Col: "cord", Op: OpLe, Value: 3}, `"Order" <= ?`, []any{int64(3)}},
		{"gt", &Filter{Col: "cord", Op: OpGt, Value: 3}, `"Order" > ?`, []any{int64(3)}},
		{"ge", &Filter{Col: "cord", Op: OpGe, Value: 3}, `"Order" >= ?`, []any{int64(3)}},
		{"id literal", &Filter{Col: "id", Op: OpEq, Value: 7}, `"id" = ?`, []any{int64(7)}},
		{"contains", &Filter{Col: "cname", Op: OpContains, Value: "ab"}, `"name" LIKE ? ESCAPE '\'`, []any{"%ab%"}},
		{"starts", &Filter{Col: "cnotes", Op: OpStarts, Value: "ab"}, `"x_1" LIKE ? ESCAPE '\'`, []any{"ab%"}},
		{"ends", &Filter{Col: "clink", Op: OpEnds, Value: "ab"}, `"link" LIKE ? ESCAPE '\'`, []any{"%ab"}},
		{"calc text contains", &Filter{Col: "ccalc", Op: OpContains, Value: "q"}, `"calc" LIKE ? ESCAPE '\'`, []any{"%q%"}},
		{"like escaping", &Filter{Col: "cname", Op: OpContains, Value: `50%_\x`}, `"name" LIKE ? ESCAPE '\'`, []any{`%50\%\_\\x%`}},
		{"in", &Filter{Col: "cord", Op: OpIn, Value: []any{1, 2, 3}}, `"Order" IN (?, ?, ?)`, []any{int64(1), int64(2), int64(3)}},
		{"in one", &Filter{Col: "cord", Op: OpIn, Value: []any{1}}, `"Order" IN (?)`, []any{int64(1)}},
		{"between", &Filter{Col: "cord", Op: OpBetween, Value: []any{1, 5}}, `"Order" BETWEEN ? AND ?`, []any{int64(1), int64(5)}},
		{"is_null", &Filter{Col: "cname", Op: OpIsNull}, `"name" IS NULL`, nil},
		{"not_null", &Filter{Col: "cname", Op: OpNotNull}, `"name" IS NOT NULL`, nil},
		{"yes_no eq", &Filter{Col: "cdone", Op: OpEq, Value: true}, `"done" = ?`, []any{int64(1)}},
		{"yes_no ne", &Filter{Col: "cdone", Op: OpNe, Value: false}, `"done" IS NOT ?`, []any{int64(0)}},
		{"and", &Filter{And: []Filter{c("cname", OpEq, "a"), c("cord", OpGt, 1)}}, `("name" = ? AND "Order" > ?)`, []any{"a", int64(1)}},
		{"or", &Filter{Or: []Filter{c("cname", OpEq, "a"), c("cord", OpGt, 1)}}, `("name" = ? OR "Order" > ?)`, []any{"a", int64(1)}},
		{"nested", &Filter{And: []Filter{
			c("cname", OpIsNull, nil),
			{Or: []Filter{c("cord", OpIn, []any{1, 2}), {And: []Filter{c("cprice", OpLt, 9), c("cdone", OpEq, true)}}}},
		}}, `("name" IS NULL AND ("Order" IN (?, ?) OR ("price" < ? AND "done" = ?)))`, []any{int64(1), int64(2), int64(9), int64(1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, a, err := CompileFilter(tbl, tc.f, testDecoder)
			if err != nil {
				t.Fatal(err)
			}
			if w != tc.where {
				t.Errorf("where = %s, want %s", w, tc.where)
			}
			if !reflect.DeepEqual(a, tc.args) && !(len(a) == 0 && len(tc.args) == 0) {
				t.Errorf("args = %#v, want %#v", a, tc.args)
			}
		})
	}
}

func deepAnd(n int) *Filter {
	f := Filter{Col: "cname", Op: OpIsNull}
	for i := 1; i < n; i++ {
		f = Filter{And: []Filter{f}}
	}
	return &f
}

func TestCompileFilterErrors(t *testing.T) {
	tbl := testTable()
	many := make([]any, MaxInValues+1)
	for i := range many {
		many[i] = i
	}
	conds := make([]Filter, MaxFilterConditions+1)
	for i := range conds {
		conds[i] = Filter{Col: "cname", Op: OpIsNull}
	}
	tests := []struct {
		name string
		f    *Filter
	}{
		{"unknown col", &Filter{Col: "nope", Op: OpIsNull}},
		{"physical name not addressable", &Filter{Col: "name", Op: OpIsNull}},
		{"_version", &Filter{Col: "_version", Op: OpEq, Value: 1}},
		{"empty col", &Filter{Op: OpIsNull}},
		{"unknown op", &Filter{Col: "cname", Op: "like", Value: "a"}},
		{"missing op", &Filter{Col: "cname", Value: "a"}},
		{"eq nil", &Filter{Col: "cname", Op: OpEq}},
		{"ne nil", &Filter{Col: "cname", Op: OpNe}},
		{"group and condition", &Filter{Col: "cname", Op: OpIsNull, And: []Filter{{Col: "cname", Op: OpIsNull}}}},
		{"and and or", &Filter{And: []Filter{{Col: "cname", Op: OpIsNull}}, Or: []Filter{{Col: "cname", Op: OpIsNull}}}},
		{"empty nested group", &Filter{And: []Filter{{}}}},
		{"contains on integer", &Filter{Col: "cord", Op: OpContains, Value: "1"}},
		{"contains on calc integer", &Filter{Col: "ccalcn", Op: OpContains, Value: "1"}},
		{"contains non-string", &Filter{Col: "cname", Op: OpContains, Value: 1}},
		{"in empty", &Filter{Col: "cord", Op: OpIn, Value: []any{}}},
		{"in non-list", &Filter{Col: "cord", Op: OpIn, Value: 1}},
		{"in too many", &Filter{Col: "cord", Op: OpIn, Value: many}},
		{"in nil element", &Filter{Col: "cord", Op: OpIn, Value: []any{nil}}},
		{"between one", &Filter{Col: "cord", Op: OpBetween, Value: []any{1}}},
		{"between three", &Filter{Col: "cord", Op: OpBetween, Value: []any{1, 2, 3}}},
		{"between non-list", &Filter{Col: "cord", Op: OpBetween, Value: 1}},
		{"yes_no lt", &Filter{Col: "cdone", Op: OpLt, Value: true}},
		{"yes_no between", &Filter{Col: "cdone", Op: OpBetween, Value: []any{false, true}}},
		{"decoder error", &Filter{Col: "cord", Op: OpEq, Value: map[string]any{}}},
		{"decoder error in list", &Filter{Col: "cord", Op: OpIn, Value: []any{1, map[string]any{}}}},
		{"too deep", deepAnd(MaxFilterDepth + 1)},
		{"too many conditions", &Filter{And: conds}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, a, err := CompileFilter(tbl, tc.f, testDecoder)
			if !isBadRequest(err) {
				t.Fatalf("err = %v, want BAD_REQUEST", err)
			}
			if w != "" || a != nil {
				t.Errorf("got where %q args %v with error", w, a)
			}
		})
	}
	if _, _, err := CompileFilter(tbl, &Filter{Col: "nope", Op: OpIsNull}, testDecoder); err == nil || err.(*fault.Error).Message != "Unknown field." {
		t.Errorf("unknown field message: %v", err)
	}
	// Limits are inclusive.
	if _, _, err := CompileFilter(tbl, deepAnd(MaxFilterDepth), testDecoder); err != nil {
		t.Errorf("depth %d: %v", MaxFilterDepth, err)
	}
	if _, _, err := CompileFilter(tbl, &Filter{Col: "cord", Op: OpIn, Value: many[:MaxInValues]}, testDecoder); err != nil {
		t.Errorf("max in: %v", err)
	}
	if _, _, err := CompileFilter(tbl, &Filter{And: conds[:MaxFilterConditions]}, testDecoder); err != nil {
		t.Errorf("max conds: %v", err)
	}
}

func TestCompileSort(t *testing.T) {
	tbl := testTable()
	tests := []struct {
		name string
		s    []Sort
		want string
	}{
		{"empty", nil, `"id" ASC`},
		{"asc", []Sort{{Col: "cord"}}, `"Order" ASC, "id" ASC`},
		{"desc", []Sort{{Col: "cord", Desc: true}}, `"Order" DESC, "id" ASC`},
		{"text nocase", []Sort{{Col: "cname"}, {Col: "cprice", Desc: true}}, `"name" COLLATE NOCASE ASC, "price" DESC, "id" ASC`},
		{"calc text", []Sort{{Col: "ccalc", Desc: true}}, `"calc" COLLATE NOCASE DESC, "id" ASC`},
		{"calc int", []Sort{{Col: "ccalcn"}}, `"calcn" ASC, "id" ASC`},
		{"id only", []Sort{{Col: "id"}}, `"id" ASC`},
		{"id desc", []Sort{{Col: "id", Desc: true}}, `"id" DESC`},
		{"id mid", []Sort{{Col: "cord"}, {Col: "id", Desc: true}}, `"Order" ASC, "id" DESC`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CompileSort(tbl, tc.s)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
	many := make([]Sort, MaxSorts+1)
	for i := range many {
		many[i] = Sort{Col: fmt.Sprint("c", i)}
	}
	bad := map[string][]Sort{
		"unknown":      {{Col: "zzz"}},
		"physical":     {{Col: "name"}},
		"_version":     {{Col: "_version"}},
		"duplicate":    {{Col: "cord"}, {Col: "cord", Desc: true}},
		"dup id":       {{Col: "id"}, {Col: "id"}},
		"id and cid":   {{Col: "id"}, {Col: "cid"}},
		"too many":     many,
		"empty column": {{}},
	}
	for name, s := range bad {
		if _, err := CompileSort(tbl, s); !isBadRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	ok := []Sort{{Col: "cname"}, {Col: "cnotes"}, {Col: "cord"}, {Col: "cdone"}, {Col: "clink"}, {Col: "ccalc"}, {Col: "ccalcn"}, {Col: "cprice"}}
	if _, err := CompileSort(tbl, ok); err != nil {
		t.Errorf("max sorts: %v", err)
	}
}

var quotedIdent = regexp.MustCompile(`"([^"]*)"`)

func checkCompiled(t *testing.T, tbl *catalog.Table, where string, args []any) {
	t.Helper()
	allowed := map[string]bool{"id": true}
	for _, c := range tbl.Columns {
		allowed[c.PhysicalName] = true
	}
	for _, m := range quotedIdent.FindAllStringSubmatch(where, -1) {
		if !allowed[m[1]] {
			t.Fatalf("unexpected identifier %q in %s", m[1], where)
		}
	}
	rest := strings.ReplaceAll(where, `ESCAPE '\'`, "")
	for _, bad := range []string{";", "--", "/*", "'"} {
		if strings.Contains(rest, bad) {
			t.Fatalf("output contains %q: %s", bad, where)
		}
	}
	if n := strings.Count(where, "?"); n != len(args) {
		t.Fatalf("%d placeholders, %d args: %s", n, len(args), where)
	}
}

func FuzzFilterCompile(f *testing.F) {
	seeds := []string{
		`{}`,
		`null`,
		`{"col":"cname","op":"eq","value":"x"}`,
		`{"col":"cname","op":"ne","value":"x'; DROP TABLE t; --"}`,
		`{"col":"cname","op":"contains","value":"100%_\\"}`,
		`{"col":"cname","op":"starts","value":"a\"b"}`,
		`{"col":"x_1","op":"ends","value":"/* c */"}`,
		`{"col":"cord","op":"in","value":[1,2,3]}`,
		`{"col":"cord","op":"in","value":[]}`,
		`{"col":"cord","op":"between","value":[1,10]}`,
		`{"col":"cord","op":"between","value":[1]}`,
		`{"col":"id","op":"ge","value":5}`,
		`{"col":"_version","op":"eq","value":1}`,
		`{"col":"name\" OR 1=1 --","op":"is_null"}`,
		`{"col":"cdone","op":"eq","value":true}`,
		`{"col":"cdone","op":"gt","value":true}`,
		`{"col":"cprice","op":"not_null"}`,
		`{"and":[{"col":"cname","op":"eq","value":"a"},{"or":[{"col":"cord","op":"lt","value":3},{"col":"cord","op":"gt","value":9}]}]}`,
		`{"and":[{"or":[{"and":[{"col":"cname","op":"is_null"}]}]}]}`,
		`{"and":[{"col":"cname","op":"is_null"}],"col":"cname","op":"is_null"}`,
		`{"and":[{}]}`,
		`{"or":[{"col":"ccalc","op":"contains","value":"é\u0000"}]}`,
		`{"col":"cord","op":"eq","value":null}`,
		`{"col":"cord","op":"eq","value":{"a":1}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	tbl := &catalog.Table{ID: "T", PhysicalName: "t", Columns: []catalog.Column{
		{ID: "cid", PhysicalName: "id", Type: catalog.Autonumber},
		{ID: "cname", PhysicalName: "name", Type: catalog.ShortText},
		{ID: "x_1", PhysicalName: "x_1", Type: catalog.LongText},
		{ID: "cord", PhysicalName: "Order", Type: catalog.Integer},
		{ID: "cdone", PhysicalName: "done", Type: catalog.YesNo},
		{ID: "ccalc", PhysicalName: "calc", Type: catalog.Calculated},
		{ID: "cprice", PhysicalName: "price", Type: catalog.Currency},
	}}
	f.Fuzz(func(t *testing.T, data []byte) {
		var fl Filter
		if err := json.Unmarshal(data, &fl); err != nil {
			return
		}
		where, args, err := CompileFilter(tbl, &fl, testDecoder)
		if err != nil {
			return
		}
		checkCompiled(t, tbl, where, args)
	})
}
