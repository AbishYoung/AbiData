package sqlgen

func typeOf(e Expr) Type {
	switch e := e.(type) {
	case Col:
		return e.Type
	case Arg:
		return e.Type
	case Param:
		return e.Type
	case Lit:
		return e.Type
	case Raw:
		return e.Type
	case Cast:
		return e.Type
	case Binary:
		return binaryType(e)
	case And, Or, Not, IsNull, In, Between, Match:
		return Boolean
	case Func:
		def, ok := LookupFunc(e.Name)
		if !ok {
			return Unknown
		}
		if def.ResultArg >= 0 {
			if def.ResultArg < len(e.Args) {
				return typeOf(e.Args[def.ResultArg])
			}
			return Unknown
		}
		return def.Result
	case Agg:
		return aggType(e)
	case Case:
		t := Unknown
		for i, w := range e.Whens {
			wt := typeOf(w.Then)
			if i == 0 {
				t = wt
			} else if wt != t {
				return Unknown
			}
		}
		if e.Else != nil && typeOf(e.Else) != t {
			return Unknown
		}
		return t
	case Subquery:
		if e.Select != nil && len(e.Select.Columns) == 1 {
			return typeOf(e.Select.Columns[0].Expr)
		}
	}
	return Unknown
}

func binaryType(b Binary) Type {
	switch b.Op {
	case OpEq, OpNe, OpIsNot, OpLt, OpLe, OpGt, OpGe:
		return Boolean
	case OpConcat:
		return Text
	}
	l, r := typeOf(b.L), typeOf(b.R)
	if !l.numeric() || !r.numeric() {
		return Unknown
	}
	switch {
	case l == r && (l != Currency || b.Op == OpAdd || b.Op == OpSub):
		// Currency plus or minus currency stays currency; scaled values
		// multiplied or divided do not.
		return l
	case l == Currency || r == Currency:
		return Unknown
	default:
		return Number // integer mixed with number
	}
}

// aggType follows the result types the visual query designer has always
// reported: a count is an integer, an average of integers is a number, a total
// of yes/no values counts them, and text or dates cannot be summed.
func aggType(a Agg) Type {
	if a.Fn == AggCount {
		return Integer
	}
	base := Unknown
	if a.X != nil {
		base = typeOf(a.X)
	}
	switch {
	case base == Boolean:
		return Integer
	case base == Integer && a.Fn == AggAvg:
		return Number
	case a.Fn == AggSum || a.Fn == AggAvg:
		if !base.numeric() {
			return Unknown
		}
	}
	return base
}
