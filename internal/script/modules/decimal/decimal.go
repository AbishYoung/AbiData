// Package decimal registers the `decimal` script module: exact base-10
// arithmetic for currency and quantities (Lua numbers are float64).
package decimal

import (
	"math"
	"strconv"
	"strings"

	sd "github.com/shopspring/decimal"
	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

// Value is the Go value behind a Lua decimal userdata. It implements
// script.GoValuer so conversions to Go (and JSON) yield the decimal string.
type Value struct{ D sd.Decimal }

// GoValue returns the canonical decimal string, e.g. "12.34".
func (v *Value) GoValue() any { return v.D.String() }

const (
	divPrecision = 16
	maxInputLen  = 1024    // characters accepted by decimal.new(string)
	maxExponent  = 1000    // |exponent| bound for any value
	maxBits      = 1 << 14 // coefficient size bound (about 4,900 digits)
	maxRound     = 64
)

var allCtx = []script.Context{script.UIEvent, script.Report, script.Job}

func init() {
	script.Register(script.ModuleDef{Doc: doc(), Build: build})
	script.DecimalFactory = factory
}

const registryKey = "abidata.decimal.mt"

// factory builds a decimal for host modules (currency values). The metatable
// of the current run is kept in the state registry by build.
func factory(r *script.Run, s string) lua.LValue {
	mt, ok := r.L.Get(lua.RegistryIndex).(*lua.LTable).RawGetString(registryKey).(*lua.LTable)
	d, err := sd.NewFromString(s)
	if !ok || err != nil {
		return lua.LString(s)
	}
	ud := r.L.NewUserData()
	ud.Value = &Value{D: d}
	ud.Metatable = mt
	return ud
}

func doc() script.ModuleDoc {
	return script.ModuleDoc{
		Name: "decimal",
		Doc: "Exact base-10 arithmetic. Currency is stored as an integer scaled by 10^4 and appears in scripts as decimal values, so decimal.new(\"0.1\") + decimal.new(\"0.2\") == decimal.new(\"0.3\") is true.\n\n" +
			"Decimals support + - * / % unary minus, == < <= > >= and .. (concatenation). Arithmetic operands may be decimals, numbers (converted through their shortest exact decimal representation) or numeric strings. " +
			"Lua 5.1 caveats: == is only evaluated when both sides are decimals, so decimal == number is always false; < <= > >= only work between two decimals and comparing a decimal with a number raises an error. " +
			"Convert first: d == decimal.new(1), d < decimal.new(\"2.5\"). " +
			"Division keeps 16 decimal places and raises on division by zero; % follows Lua (the result has the sign of the divisor). Values are limited to about 4,900 digits and an exponent of 1000.\n\n" +
			"Decimal values have the methods d:round(places), d:tostring() and d:tonumber().",
		Contexts: allCtx,
		Funcs: []script.FuncDoc{
			{
				Name: "new", Signature: "decimal.new(value)",
				Params:  []script.ParamDoc{{Name: "value", Type: "string|number|decimal", Doc: "A numeric string such as \"12.50\" or \"1e3\", a finite number, or another decimal."}},
				Returns: "decimal",
				Errors:  []string{"value is not a number, numeric string or decimal", "the string is not a valid decimal or is too large"},
				Doc:     "Creates a decimal. Prefer strings for exact values; a number is converted through its shortest round-trip representation, so decimal.new(0.1) is exactly 0.1.",
				Example: "local price = decimal.new(\"19.99\")\nreturn price * 3  -- 59.97",
			},
			{
				Name: "is_decimal", Signature: "decimal.is_decimal(value)",
				Params:  []script.ParamDoc{{Name: "value", Type: "any", Doc: "Any value."}},
				Returns: "boolean",
				Doc:     "Returns true when value is a decimal.",
				Example: "return decimal.is_decimal(decimal.new(1))  -- true",
			},
		},
	}
}

type mod struct {
	r  *script.Run
	mt *lua.LTable
}

func build(r *script.Run) lua.LValue {
	m := &mod{r: r}
	L := r.L
	m.mt = L.NewTable()
	m.mt.RawSetString("__metatable", lua.LString("locked"))
	L.Get(lua.RegistryIndex).(*lua.LTable).RawSetString(registryKey, m.mt)
	binop := func(name string, f func(a, b sd.Decimal) sd.Decimal) {
		m.mt.RawSetString(name, L.NewFunction(func(L *lua.LState) int {
			a, b := m.operand(L, L.Get(1)), m.operand(L, L.Get(2))
			L.Push(m.wrap(L, f(a, b)))
			return 1
		}))
	}
	binop("__add", func(a, b sd.Decimal) sd.Decimal { return a.Add(b) })
	binop("__sub", func(a, b sd.Decimal) sd.Decimal { return a.Sub(b) })
	binop("__mul", func(a, b sd.Decimal) sd.Decimal { return a.Mul(b) })
	m.mt.RawSetString("__div", L.NewFunction(func(L *lua.LState) int {
		a, b := m.operand(L, L.Get(1)), m.operand(L, L.Get(2))
		if b.IsZero() {
			L.RaiseError("decimal: division by zero")
		}
		L.Push(m.wrap(L, a.DivRound(b, divPrecision)))
		return 1
	}))
	m.mt.RawSetString("__mod", L.NewFunction(func(L *lua.LState) int {
		a, b := m.operand(L, L.Get(1)), m.operand(L, L.Get(2))
		if b.IsZero() {
			L.RaiseError("decimal: modulo by zero")
		}
		rem := a.Mod(b) // sign of a
		if !rem.IsZero() && rem.Sign() != b.Sign() {
			rem = rem.Add(b)
		}
		L.Push(m.wrap(L, rem))
		return 1
	}))
	m.mt.RawSetString("__unm", L.NewFunction(func(L *lua.LState) int {
		L.Push(m.wrap(L, m.operand(L, L.Get(1)).Neg()))
		return 1
	}))
	// gopher-lua only dispatches comparison metamethods when both operands are
	// userdata sharing the same metamethod function, so these are decimal-only.
	cmp := func(name string, f func(c int) bool) {
		m.mt.RawSetString(name, L.NewFunction(func(L *lua.LState) int {
			a, ok1 := m.value(L.Get(1))
			b, ok2 := m.value(L.Get(2))
			if !ok1 || !ok2 {
				L.Push(lua.LFalse)
				return 1
			}
			L.Push(lua.LBool(f(a.Cmp(b))))
			return 1
		}))
	}
	cmp("__eq", func(c int) bool { return c == 0 })
	cmp("__lt", func(c int) bool { return c < 0 })
	cmp("__le", func(c int) bool { return c <= 0 })
	m.mt.RawSetString("__tostring", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(m.operand(L, L.Get(1)).String()))
		return 1
	}))
	m.mt.RawSetString("__concat", L.NewFunction(func(L *lua.LState) int {
		s := m.concatPart(L, L.Get(1)) + m.concatPart(L, L.Get(2))
		if r.Budget.MaxString > 0 && len(s) > r.Budget.MaxString {
			r.RaiseLimit("decimal: concatenation exceeds max string size")
		}
		L.Push(lua.LString(s))
		return 1
	}))

	methods := L.NewTable()
	methods.RawSetString("round", L.NewFunction(func(L *lua.LState) int {
		d := m.self(L)
		n := L.OptInt(2, 0)
		if n < -maxRound || n > maxRound {
			L.ArgError(2, "places must be between -64 and 64")
		}
		L.Push(m.wrap(L, d.Round(int32(n))))
		return 1
	}))
	methods.RawSetString("tostring", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(m.self(L).String()))
		return 1
	}))
	methods.RawSetString("tonumber", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(m.self(L).InexactFloat64()))
		return 1
	}))
	m.mt.RawSetString("__index", methods)

	t := L.NewTable()
	t.RawSetString("new", L.NewFunction(func(L *lua.LState) int {
		v := L.Get(1)
		if v == lua.LNil {
			L.ArgError(1, "number, numeric string or decimal expected, got nil")
		}
		L.Push(m.wrap(L, m.operandArg(L, v, 1)))
		return 1
	}))
	t.RawSetString("is_decimal", L.NewFunction(func(L *lua.LState) int {
		_, ok := m.value(L.Get(1))
		L.Push(lua.LBool(ok))
		return 1
	}))
	return t
}

// value extracts a decimal from a Lua value that is already a decimal.
func (m *mod) value(v lua.LValue) (sd.Decimal, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		if dv, ok := ud.Value.(*Value); ok {
			return dv.D, true
		}
	}
	return sd.Decimal{}, false
}

func (m *mod) self(L *lua.LState) sd.Decimal {
	d, ok := m.value(L.Get(1))
	if !ok {
		L.ArgError(1, "decimal expected (call methods with ':')")
	}
	return d
}

func (m *mod) wrap(L *lua.LState, d sd.Decimal) lua.LValue {
	if d.Exponent() > maxExponent || d.Exponent() < -maxExponent || d.Coefficient().BitLen() > maxBits {
		L.RaiseError("decimal: value out of range")
	}
	ud := L.NewUserData()
	ud.Value = &Value{D: d}
	ud.Metatable = m.mt
	return ud
}

// operand converts an operator operand; failures are runtime errors.
func (m *mod) operand(L *lua.LState, v lua.LValue) sd.Decimal { return m.operandArg(L, v, 0) }

func (m *mod) operandArg(L *lua.LState, v lua.LValue, arg int) sd.Decimal {
	fail := func(msg string) {
		if arg > 0 {
			L.ArgError(arg, msg)
		}
		L.RaiseError("decimal: %s", msg)
	}
	switch x := v.(type) {
	case *lua.LUserData:
		if d, ok := m.value(x); ok {
			return d
		}
	case lua.LNumber:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			fail("cannot convert NaN or infinity to a decimal")
		}
		return m.parse(strconv.FormatFloat(f, 'f', -1, 64), fail)
	case lua.LString:
		return m.parse(string(x), fail)
	}
	fail("cannot convert " + v.Type().String() + " to a decimal")
	return sd.Decimal{}
}

func (m *mod) parse(s string, fail func(string)) sd.Decimal {
	s = strings.TrimSpace(s)
	if len(s) > maxInputLen {
		fail("numeric string is too long")
	}
	d, err := sd.NewFromString(s)
	if err != nil {
		fail("invalid decimal " + strconv.Quote(truncate(s, 40)))
	}
	if d.Exponent() > maxExponent || d.Exponent() < -maxExponent {
		fail("decimal exponent out of range")
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func (m *mod) concatPart(L *lua.LState, v lua.LValue) string {
	switch x := v.(type) {
	case lua.LString:
		return string(x)
	case lua.LNumber:
		return x.String()
	}
	if d, ok := m.value(v); ok {
		return d.String()
	}
	L.RaiseError("cannot concatenate %s with a decimal", v.Type().String())
	return ""
}
