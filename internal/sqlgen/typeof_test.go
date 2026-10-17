package sqlgen

import "testing"

func TestTypeOf(t *testing.T) {
	col := func(ty Type) Col { return Col{Name: "c", Type: ty} }
	cases := []struct {
		name string
		e    Expr
		want Type
	}{
		// Totals, matching the types the visual query designer has always reported.
		{"count of text", Agg{Fn: AggCount, X: col(Text)}, Integer},
		{"count(*)", Agg{Fn: AggCount}, Integer},
		{"sum of currency", Agg{Fn: AggSum, X: col(Currency)}, Currency},
		{"avg of currency", Agg{Fn: AggAvg, X: col(Currency)}, Currency},
		{"avg of integer", Agg{Fn: AggAvg, X: col(Integer)}, Number},
		{"sum of integer", Agg{Fn: AggSum, X: col(Integer)}, Integer},
		{"sum of yes/no counts", Agg{Fn: AggSum, X: col(Boolean)}, Integer},
		{"max of yes/no", Agg{Fn: AggMax, X: col(Boolean)}, Integer},
		{"sum of text", Agg{Fn: AggSum, X: col(Text)}, Unknown},
		{"avg of dates", Agg{Fn: AggAvg, X: col(Date)}, Unknown},
		{"min of dates", Agg{Fn: AggMin, X: col(Date)}, Date},
		{"max of text", Agg{Fn: AggMax, X: col(Text)}, Text},
		// Arithmetic.
		{"currency + currency", Binary{Op: OpAdd, L: col(Currency), R: col(Currency)}, Currency},
		{"currency * currency", Binary{Op: OpMul, L: col(Currency), R: col(Currency)}, Unknown},
		{"currency * integer", Binary{Op: OpMul, L: col(Currency), R: col(Integer)}, Unknown},
		{"integer * number", Binary{Op: OpMul, L: col(Integer), R: col(Number)}, Number},
		{"integer / integer", Binary{Op: OpDiv, L: col(Integer), R: col(Integer)}, Integer},
		{"text + integer", Binary{Op: OpAdd, L: col(Text), R: col(Integer)}, Unknown},
		{"concat", Binary{Op: OpConcat, L: col(Integer), R: col(Integer)}, Text},
		{"comparison", Binary{Op: OpLt, L: col(Date), R: col(Date)}, Boolean},
		{"match", Match{Kind: MatchContains, X: col(Text), Pattern: Lit{Value: "x"}}, Boolean},
		// Functions: fixed result types and argument-typed ones.
		{"date()", Func{Name: "date", Args: []Expr{col(DateTime)}}, Date},
		{"geo_within", Func{Name: "geo_within", Args: []Expr{col(GeoPoint), col(GeoArea)}}, Boolean},
		{"coalesce takes its first argument's type", Func{Name: "coalesce", Args: []Expr{col(Currency), Lit{Value: 0}}}, Currency},
		{"iif takes its first branch's type", Func{Name: "iif", Args: []Expr{col(Boolean), col(Date), col(Date)}}, Date},
		{"abs with no argument", Func{Name: "abs"}, Unknown},
		{"unknown function", Func{Name: "nope"}, Unknown},
		// Other nodes.
		{"cast", Cast{X: col(Text), Type: Number}, Number},
		{"raw keeps its declared type", Raw{SQL: "1", Type: Integer}, Integer},
		{"case with one type", Case{Whens: []When{{Cond: col(Boolean), Then: col(Text)}}, Else: Lit{Value: "x", Type: Text}}, Text},
		{"case with mixed types", Case{Whens: []When{{Cond: col(Boolean), Then: col(Text)}}, Else: col(Integer)}, Unknown},
		{"scalar subquery", Subquery{Select: &Select{Columns: []Column{{Expr: col(GeoArea)}}}}, GeoArea},
	}
	for _, tc := range cases {
		if got := TypeOf(tc.e); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
