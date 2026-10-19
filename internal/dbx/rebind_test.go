package dbx

import (
	"reflect"
	"strings"
	"testing"
)

func TestRebind(t *testing.T) {
	cases := []struct {
		sql  string
		args []any
		want string
		out  []any
	}{
		{"SELECT 1", nil, "SELECT 1", []any{}},
		{"SELECT * FROM t WHERE a = ? AND b = ?", []any{1, "x"}, "SELECT * FROM t WHERE a = $1 AND b = $2", []any{1, "x"}},
		{"SELECT ?2, ?1, ?2", []any{"a", "b"}, "SELECT $1, $2, $1", []any{"b", "a"}},
		{"SELECT ?2, ?", []any{"a", "b", "c"}, "SELECT $1, $2", []any{"b", "c"}},
		{"SELECT :a, @b, $c, :a", []any{Named{Name: "c", Value: 3}, Named{Name: ":a", Value: 1}, Named{Name: "b", Value: 2}},
			"SELECT $1, $2, $3, $1", []any{1, 2, 3}},
		{"SELECT ?, :n", []any{Named{Name: "n", Value: "N"}, "P"}, "SELECT $1, $2", []any{"P", "N"}},
		// Placeholders inside literals, identifiers, comments and dollar quotes are text.
		{`SELECT '?', 'it''s :x', "col?", ? -- what?
/* :y */ FROM t`, []any{1}, `SELECT '?', 'it''s :x', "col?", $1 -- what?
/* :y */ FROM t`, []any{1}},
		{"SELECT $$ ? $$, $fn$ :a $fn$, ?", []any{7}, "SELECT $$ ? $$, $fn$ :a $fn$, $1", []any{7}},
		{"SELECT x::text, ?", []any{1}, "SELECT x::text, $1", []any{1}},
	}
	for _, c := range cases {
		got, out, err := Rebind(c.sql, c.args)
		if err != nil {
			t.Errorf("%q: %v", c.sql, err)
			continue
		}
		if got != c.want || !reflect.DeepEqual(out, c.out) {
			t.Errorf("%q:\n got %q %v\nwant %q %v", c.sql, got, out, c.want, c.out)
		}
		// The cached parse gives the same answer.
		if again, _, _ := Rebind(c.sql, c.args); again != got {
			t.Errorf("%q: cached %q", c.sql, again)
		}
	}
}

func TestRebindArgumentErrors(t *testing.T) {
	for _, c := range []struct {
		sql  string
		args []any
	}{
		{"SELECT ?", nil},
		{"SELECT ?", []any{1, 2}},
		{"SELECT :a", nil},
		{"SELECT :a", []any{Named{Name: "b", Value: 1}}},
		{"SELECT 1", []any{Named{Name: "b", Value: 1}}},
		{"SELECT ?0", []any{1}},
		{"SELECT /* open", nil},
		{"SELECT $q$ open", nil},
	} {
		if _, _, err := Rebind(c.sql, c.args); err == nil {
			t.Errorf("%q %v: no error", c.sql, c.args)
		}
	}
}

func TestParams(t *testing.T) {
	got, err := Params(`SELECT * FROM t WHERE a = :a AND b = ? AND c = @c AND d = :a AND e = '?' AND f::text = $f`)
	if err != nil || strings.Join(got, ",") != ":a,?1,:c,:f" {
		t.Fatalf("%v %v", got, err)
	}
}
