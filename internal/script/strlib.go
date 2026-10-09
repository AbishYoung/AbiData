package script

// Budgeted replacements for string.find/match/gmatch/gsub/rep/format and table.concat.
//
// find/match/gmatch/gsub are adapted from github.com/yuin/gopher-lua stringlib.go
// (v1.1.2, MIT License, Copyright (c) 2015 Yusuke Inuzuka). Changes: matching goes
// through the budgeted internal/script/pm, gsub builds its result linearly with an
// output cap, and every result is checked against the run's max string size.

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script/pm"
)

func roBytes(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }

func luaIndex2StringIndex(str string, i int, start bool) int {
	if start && i != 0 {
		i--
	}
	l := len(str)
	if i < 0 {
		i = l + i + 1
	}
	i = max(0, i)
	if !start && i > l {
		i = l
	}
	return i
}

// find runs a budgeted pattern search, translating budget exhaustion into a
// classified limit/timeout error.
func (v *VM) find(L *lua.LState, pattern, str string, offset, limit int) []*pm.MatchData {
	r := v.run
	budget := &pm.Budget{Ctx: r.ctx, MaxSteps: r.limits.PatternSteps}
	mds, err := pm.Find(budget, pattern, roBytes(str), offset, limit)
	if err != nil {
		if errors.Is(err, pm.ErrStepLimit) {
			v.raiseLimit(L, "pattern too complex: step limit exceeded")
		}
		if r.ctx.Err() != nil {
			L.RaiseError("%s", r.ctx.Err().Error())
		}
		L.RaiseError("%s", err.Error())
	}
	return mds
}

func (v *VM) checkLen(L *lua.LState, n int, what string) {
	if n > v.run.limits.MaxString {
		v.raiseLimit(L, fmt.Sprintf("%s result exceeds max string size (%d bytes)", what, v.run.limits.MaxString))
	}
}

func (v *VM) strFind(L *lua.LState) int {
	str := L.CheckString(1)
	pattern := L.CheckString(2)
	if len(pattern) == 0 {
		L.Push(lua.LNumber(1))
		L.Push(lua.LNumber(0))
		return 2
	}
	init := luaIndex2StringIndex(str, L.OptInt(3, 1), true)
	plain := L.GetTop() == 4 && lua.LVAsBool(L.Get(4))
	if plain || !strings.ContainsAny(pattern, "^$*+?.([%-") {
		if init > len(str) {
			L.Push(lua.LNil)
			return 1
		}
		pos := strings.Index(str[init:], pattern)
		if pos < 0 {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(init+pos) + 1)
		L.Push(lua.LNumber(init + pos + len(pattern)))
		return 2
	}
	mds := v.find(L, pattern, str, init, 1)
	if len(mds) == 0 {
		L.Push(lua.LNil)
		return 1
	}
	md := mds[0]
	L.Push(lua.LNumber(md.Capture(0) + 1))
	L.Push(lua.LNumber(md.Capture(1)))
	for i := 2; i < md.CaptureLength(); i += 2 {
		if md.IsPosCapture(i) {
			L.Push(lua.LNumber(md.Capture(i)))
		} else {
			L.Push(lua.LString(str[md.Capture(i):md.Capture(i+1)]))
		}
	}
	return md.CaptureLength()/2 + 1
}

func (v *VM) strMatch(L *lua.LState) int {
	str := L.CheckString(1)
	pattern := L.CheckString(2)
	offset := L.OptInt(3, 1)
	if offset < 0 {
		offset = len(str) + offset + 1
	}
	offset = max(offset-1, 0)
	mds := v.find(L, pattern, str, offset, 1)
	if len(mds) == 0 {
		L.Push(lua.LNil)
		return 1
	}
	md := mds[0]
	nsubs := md.CaptureLength() / 2
	if nsubs == 1 {
		L.Push(lua.LString(str[md.Capture(0):md.Capture(1)]))
		return 1
	}
	for i := 2; i < md.CaptureLength(); i += 2 {
		if md.IsPosCapture(i) {
			L.Push(lua.LNumber(md.Capture(i)))
		} else {
			L.Push(lua.LString(str[md.Capture(i):md.Capture(i+1)]))
		}
	}
	return nsubs - 1
}

type gmatchState struct {
	str     string
	pos     int
	matches []*pm.MatchData
}

func (v *VM) strGmatch(L *lua.LState) int {
	str := L.CheckString(1)
	pattern := L.CheckString(2)
	st := &gmatchState{str: str, matches: v.find(L, pattern, str, 0, -1)}
	L.Push(L.NewFunction(func(L *lua.LState) int {
		if st.pos >= len(st.matches) {
			return 0
		}
		m := st.matches[st.pos]
		st.pos++
		if m.CaptureLength() == 2 {
			L.Push(lua.LString(st.str[m.Capture(0):m.Capture(1)]))
			return 1
		}
		for i := 2; i < m.CaptureLength(); i += 2 {
			if m.IsPosCapture(i) {
				L.Push(lua.LNumber(m.Capture(i)))
			} else {
				L.Push(lua.LString(st.str[m.Capture(i):m.Capture(i+1)]))
			}
		}
		return m.CaptureLength()/2 - 1
	}))
	return 1
}

func capturedString(L *lua.LState, m *pm.MatchData, str string, idx int) string {
	if idx > 2 && idx >= m.CaptureLength() {
		L.RaiseError("invalid capture index")
	}
	if idx >= m.CaptureLength() && idx == 2 {
		idx = 0
	}
	if m.IsPosCapture(idx) {
		return fmt.Sprint(m.Capture(idx))
	}
	return str[m.Capture(idx):m.Capture(idx+1)]
}

func (v *VM) strGsub(L *lua.LState) int {
	str := L.CheckString(1)
	pat := L.CheckString(2)
	L.CheckTypes(3, lua.LTString, lua.LTNumber, lua.LTTable, lua.LTFunction)
	repl := L.CheckAny(3)
	limit := L.OptInt(4, -1)

	mds := v.find(L, pat, str, 0, limit)
	if len(mds) == 0 {
		L.SetTop(1)
		L.Push(lua.LNumber(0))
		return 2
	}
	var out strings.Builder
	prev := 0
	for _, m := range mds {
		start, end := m.Capture(0), m.Capture(1)
		out.WriteString(str[prev:start])
		var rep string
		keep := true
		switch lv := repl.(type) {
		case lua.LString, lua.LNumber:
			rep = expandRepl(L, lv.String(), m, str)
		case *lua.LTable:
			idx := 0
			if m.CaptureLength() > 2 {
				idx = 2
			}
			var val lua.LValue
			if m.IsPosCapture(idx) {
				val = L.GetTable(lv, lua.LNumber(m.Capture(idx)))
			} else {
				val = L.GetField(lv, str[m.Capture(idx):m.Capture(idx+1)])
			}
			keep, rep = replValue(L, val)
		case *lua.LFunction:
			L.Push(lv)
			nargs := 0
			if m.CaptureLength() > 2 {
				for i := 2; i < m.CaptureLength(); i += 2 {
					if m.IsPosCapture(i) {
						L.Push(lua.LNumber(m.Capture(i)))
					} else {
						L.Push(lua.LString(capturedString(L, m, str, i)))
					}
					nargs++
				}
			} else {
				L.Push(lua.LString(capturedString(L, m, str, 0)))
				nargs++
			}
			L.Call(nargs, 1)
			ret := L.Get(-1)
			L.Pop(1)
			keep, rep = replValue(L, ret)
		}
		if keep {
			out.WriteString(rep)
		} else {
			out.WriteString(str[start:end])
		}
		prev = end
		v.checkLen(L, out.Len()+len(str)-prev, "string.gsub")
	}
	out.WriteString(str[prev:])
	L.Push(lua.LString(out.String()))
	L.Push(lua.LNumber(len(mds)))
	return 2
}

// replValue applies Lua 5.1 gsub semantics for table/function replacements:
// false/nil keeps the original match; strings and numbers replace it.
func replValue(L *lua.LState, val lua.LValue) (bool, string) {
	switch tv := val.(type) {
	case *lua.LNilType:
		return false, ""
	case lua.LBool:
		if !bool(tv) {
			return false, ""
		}
	case lua.LString, lua.LNumber:
		return true, tv.String()
	}
	L.RaiseError("invalid replacement value (a %s)", val.Type().String())
	return false, ""
}

func expandRepl(L *lua.LState, repl string, m *pm.MatchData, str string) string {
	if !strings.Contains(repl, "%") {
		return repl
	}
	var b strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		if c != '%' || i+1 >= len(repl) {
			b.WriteByte(c)
			continue
		}
		i++
		d := repl[i]
		if d >= '0' && d <= '9' {
			b.WriteString(capturedString(L, m, str, 2*int(d-'0')))
		} else {
			b.WriteByte(d)
		}
	}
	return b.String()
}

func (v *VM) strRep(L *lua.LState) int {
	str := L.CheckString(1)
	n := L.CheckInt(2)
	if n <= 0 || len(str) == 0 {
		L.Push(lua.LString(""))
		return 1
	}
	if n > v.run.limits.MaxString/len(str) {
		v.raiseLimit(L, fmt.Sprintf("string.rep result exceeds max string size (%d bytes)", v.run.limits.MaxString))
	}
	L.Push(lua.LString(strings.Repeat(str, n)))
	return 1
}

// strFormat validates the format like Lua 5.1 (width and precision are at most two
// digits) before delegating to gopher-lua's implementation, then caps the result.
func (v *VM) strFormat(orig lua.LGFunction) lua.LGFunction {
	return func(L *lua.LState) int {
		f := L.CheckString(1)
		for i := 0; i < len(f); i++ {
			if f[i] != '%' {
				continue
			}
			i++
			if i < len(f) && f[i] == '%' {
				continue
			}
			for i < len(f) && strings.IndexByte("-+ #0", f[i]) >= 0 {
				i++
			}
			digits := 0
			for i < len(f) && f[i] >= '0' && f[i] <= '9' {
				i++
				digits++
			}
			if i < len(f) && f[i] == '.' {
				i++
				pd := 0
				for i < len(f) && f[i] >= '0' && f[i] <= '9' {
					i++
					pd++
				}
				digits = max(digits, pd)
			}
			if digits > 2 {
				L.RaiseError("invalid format (width or precision too long)")
			}
		}
		n := orig(L)
		v.checkLen(L, len(L.CheckString(-1)), "string.format")
		return n
	}
}

func (v *VM) tableConcat(L *lua.LState) int {
	tbl := L.CheckTable(1)
	sep := L.OptString(2, "")
	i := L.OptInt(3, 1)
	j := L.OptInt(4, tbl.Len())
	var b strings.Builder
	for k := i; k <= j; k++ {
		if (k-i)%1024 == 1023 && v.run.ctx.Err() != nil {
			L.RaiseError("%s", v.run.ctx.Err().Error())
		}
		switch e := tbl.RawGetInt(k).(type) {
		case lua.LString, lua.LNumber:
			s := e.String()
			if b.Len()+len(s)+len(sep) > v.run.limits.MaxString {
				v.raiseLimit(L, fmt.Sprintf("table.concat result exceeds max string size (%d bytes)", v.run.limits.MaxString))
			}
			b.WriteString(s)
		default:
			L.RaiseError("invalid value (at index %d) in table for 'concat'", k)
		}
		if k < j {
			b.WriteString(sep)
		}
	}
	L.Push(lua.LString(b.String()))
	return 1
}
