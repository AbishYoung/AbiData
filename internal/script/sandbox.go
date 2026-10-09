package script

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

// Limits are the per-run resource caps enforced inside the VM.
type Limits struct {
	MaxString    int // bytes; caps string.rep/format/gsub and table.concat results
	PatternSteps int // per pattern operation
}

// DefaultPatternSteps is the per-operation step cap for Lua pattern matching.
const DefaultPatternSteps = 1_000_000

// ErrLimitExceeded marks a run stopped by a resource cap (SCRIPT_LIMIT_EXCEEDED).
var ErrLimitExceeded = errors.New("script limit exceeded")

// Error is a script failure with a classified kind and the Lua location when known.
type Error struct {
	Kind      ErrorKind
	Message   string
	Traceback string
}

// ErrorKind classifies a script failure for the error catalog.
type ErrorKind int

const (
	KindRuntime ErrorKind = iota // SCRIPT_ERROR
	KindTimeout                  // SCRIPT_TIMEOUT
	KindLimit                    // SCRIPT_LIMIT_EXCEEDED
	KindSyntax                   // SCRIPT_ERROR (compile)
)

func (e *Error) Error() string { return e.Message }

// Module resolves `require(name)` to the source of a shared module script.
type ModuleLoader func(name string) (source string, ok bool)

// runState is the per-run mutable state. A VM serves one run at a time.
type runState struct {
	ctx         context.Context
	cancel      context.CancelFunc
	limits      Limits
	limitErr    error
	rng         *rand.Rand
	print       func(string)
	loader      ModuleLoader
	modules     map[string]lua.LValue
	loading     map[string]bool
	hostGlobals map[string]lua.LValue
}

// VM is one sandboxed gopher-lua state. Library tables are exposed only through
// read-only proxies built once; every run gets a fresh environment table that holds
// only allowlisted values, so nothing a script does persists into the next run.
// A VM whose run ended in a timeout, panic, or host error must be discarded.
type VM struct {
	L       *lua.LState
	safe    map[string]lua.LValue // allowlisted base functions + library proxies
	run     *runState
	tainted bool
}

var allowedBase = []string{
	"assert", "error", "ipairs", "next", "pairs", "pcall", "select", "tonumber",
	"tostring", "type", "unpack", "xpcall", "getmetatable", "rawequal", "rawget",
}

// NewVM builds a sandboxed state. Only base, table, string and math are opened.
func NewVM() *VM {
	L := lua.NewState(lua.Options{
		SkipOpenLibs:        true,
		CallStackSize:       200,
		RegistrySize:        1024 * 4,
		RegistryMaxSize:     1024 * 256,
		MinimizeStackMemory: true,
	})
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{{lua.BaseLibName, lua.OpenBase}, {lua.TabLibName, lua.OpenTable}, {lua.StringLibName, lua.OpenString}, {lua.MathLibName, lua.OpenMath}} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	v := &VM{L: L, safe: map[string]lua.LValue{}}
	g := L.G.Global
	for _, name := range allowedBase {
		fn := g.RawGetString(name)
		if fn == lua.LNil {
			panic("sandbox: missing base function " + name)
		}
		v.safe[name] = fn
	}
	v.safe["setmetatable"] = L.NewFunction(safeSetMetatable)
	origUnpack := g.RawGetString("unpack").(*lua.LFunction).GFunction
	v.safe["unpack"] = L.NewFunction(func(L *lua.LState) int {
		tbl := L.CheckTable(1)
		i, j := L.OptInt(2, 1), L.OptInt(3, tbl.Len())
		if j-i >= maxResults {
			L.RaiseError("too many results to unpack")
		}
		return origUnpack(L)
	})
	v.safe["print"] = L.NewFunction(v.basePrint)
	v.safe["require"] = L.NewFunction(v.require)

	// string: copy, swap in budgeted functions, drop dump/gfind.
	realString := copyTable(L, g.RawGetString("string").(*lua.LTable))
	realString.RawSetString("dump", lua.LNil)
	realString.RawSetString("gfind", lua.LNil)
	realString.RawSetString("__index", lua.LNil)
	origFormat := realString.RawGetString("format").(*lua.LFunction).GFunction
	origByte := realString.RawGetString("byte").(*lua.LFunction).GFunction
	realString.RawSetString("byte", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		i := luaIndex2StringIndex(s, L.OptInt(2, 1), true)
		j := luaIndex2StringIndex(s, L.OptInt(3, L.OptInt(2, 1)), false)
		if j-i > maxResults {
			L.RaiseError("string slice too long")
		}
		return origByte(L)
	}))
	for name, fn := range map[string]lua.LGFunction{
		"find": v.strFind, "match": v.strMatch, "gmatch": v.strGmatch, "gsub": v.strGsub,
		"rep": v.strRep, "format": v.strFormat(origFormat),
	} {
		realString.RawSetString(name, L.NewFunction(fn))
	}
	stringProxy := readOnlyProxy(L, realString)
	v.safe["string"] = stringProxy
	// The string metatable resolves methods through the proxy and hides itself.
	strMT := L.NewTable()
	strMT.RawSetString("__index", stringProxy)
	strMT.RawSetString("__metatable", lua.LString("locked"))
	L.SetMetatable(lua.LString(""), strMT)

	realTable := copyTable(L, g.RawGetString("table").(*lua.LTable))
	realTable.RawSetString("concat", L.NewFunction(v.tableConcat))
	// insert/remove/sort write raw (bypassing __newindex); refuse locked tables.
	for _, name := range []string{"insert", "remove", "sort"} {
		orig := realTable.RawGetString(name).(*lua.LFunction).GFunction
		realTable.RawSetString(name, L.NewFunction(func(L *lua.LState) int {
			if isLocked(L.CheckTable(1)) {
				L.RaiseError("attempt to modify read-only table")
			}
			return orig(L)
		}))
	}
	v.safe["table"] = readOnlyProxy(L, realTable)

	realMath := copyTable(L, g.RawGetString("math").(*lua.LTable))
	realMath.RawSetString("random", L.NewFunction(v.mathRandom))
	realMath.RawSetString("randomseed", L.NewFunction(v.mathRandomSeed))
	v.safe["math"] = readOnlyProxy(L, realMath)

	// Nothing should resolve through the real globals table.
	clearTable(g)
	return v
}

func copyTable(L *lua.LState, src *lua.LTable) *lua.LTable {
	dst := L.NewTable()
	src.ForEach(func(k, val lua.LValue) { dst.RawSet(k, val) })
	return dst
}

func clearTable(t *lua.LTable) {
	var keys []lua.LValue
	t.ForEach(func(k, _ lua.LValue) { keys = append(keys, k) })
	for _, k := range keys {
		t.RawSet(k, lua.LNil)
	}
}

// maxResults caps values a builtin may push (Lua 5.1's LUAI_MAXCSTACK).
const maxResults = 8000

// isLocked reports whether t is a protected table (a library proxy or host object).
func isLocked(t *lua.LTable) bool {
	mt, ok := t.Metatable.(*lua.LTable)
	return ok && mt.RawGetString("__metatable") != lua.LNil
}

// readOnlyProxy returns an empty table whose metatable forwards reads to real,
// rejects writes, and is itself locked against getmetatable/setmetatable.
func readOnlyProxy(L *lua.LState, real *lua.LTable) *lua.LTable {
	proxy := L.NewTable()
	mt := L.NewTable()
	mt.RawSetString("__index", real)
	mt.RawSetString("__newindex", L.NewFunction(func(L *lua.LState) int {
		L.RaiseError("attempt to modify read-only table")
		return 0
	}))
	mt.RawSetString("__metatable", lua.LString("locked"))
	proxy.Metatable = mt
	return proxy
}

// ReadOnlyProxy exposes proxy construction to host-module registration.
func (v *VM) ReadOnlyProxy(real *lua.LTable) *lua.LTable { return readOnlyProxy(v.L, real) }

// safeSetMetatable follows Lua 5.1: only tables may have their metatable set from
// Lua. gopher-lua's builtin also accepts strings, numbers, functions and userdata,
// which would write per-state builtin metatables shared by later runs.
func safeSetMetatable(L *lua.LState) int {
	tbl := L.CheckTable(1)
	L.CheckTypes(2, lua.LTNil, lua.LTTable)
	if mt, ok := tbl.Metatable.(*lua.LTable); ok && mt.RawGetString("__metatable") != lua.LNil {
		L.RaiseError("cannot change a protected metatable")
	}
	tbl.Metatable = L.Get(2)
	L.SetTop(1)
	return 1
}

func (v *VM) basePrint(L *lua.LState) int {
	top := L.GetTop()
	parts := make([]string, 0, top)
	for i := 1; i <= top; i++ {
		parts = append(parts, L.ToStringMeta(L.Get(i)).String())
	}
	if v.run != nil && v.run.print != nil {
		v.run.print(strings.Join(parts, "\t"))
	}
	return 0
}

func (v *VM) mathRandom(L *lua.LState) int {
	rng := v.run.rng
	switch L.GetTop() {
	case 0:
		L.Push(lua.LNumber(rng.Float64()))
	case 1:
		n := L.CheckInt(1)
		if n < 1 {
			L.ArgError(1, "interval is empty")
		}
		L.Push(lua.LNumber(rng.IntN(n) + 1))
	default:
		lo, hi := L.CheckInt(1), L.CheckInt(2)
		if lo > hi {
			L.ArgError(2, "interval is empty")
		}
		L.Push(lua.LNumber(lo + rng.IntN(hi-lo+1)))
	}
	return 1
}

func (v *VM) mathRandomSeed(L *lua.LState) int {
	seed := uint64(L.CheckInt64(1))
	v.run.rng = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	return 0
}

// require loads shared module scripts only, each in its own fresh environment,
// cached for the duration of the run.
func (v *VM) require(L *lua.LState) int {
	name := L.CheckString(1)
	r := v.run
	if mod, ok := r.modules[name]; ok {
		L.Push(mod)
		return 1
	}
	if r.loading[name] {
		L.RaiseError("module '%s': circular require", name)
	}
	var src string
	var ok bool
	if r.loader != nil {
		src, ok = r.loader(name)
	}
	if !ok {
		L.RaiseError("module '%s' not found", name)
	}
	fn, err := v.compile(src, name)
	if err != nil {
		L.RaiseError("%s", err.Error())
	}
	fn.Env = v.newEnv()
	r.loading[name] = true
	L.Push(fn)
	L.Call(0, 1)
	delete(r.loading, name)
	mod := L.Get(-1)
	L.Pop(1)
	if mod == lua.LNil {
		mod = lua.LTrue
	}
	r.modules[name] = mod
	L.Push(mod)
	return 1
}

// Compile parses and compiles source into a function prototype for caching.
func Compile(source, chunkName string) (*lua.FunctionProto, error) {
	chunk, err := parse.Parse(strings.NewReader(source), chunkName)
	if err != nil {
		return nil, &Error{Kind: KindSyntax, Message: err.Error()}
	}
	proto, err := lua.Compile(chunk, chunkName)
	if err != nil {
		return nil, &Error{Kind: KindSyntax, Message: err.Error()}
	}
	return proto, nil
}

func (v *VM) compile(source, chunkName string) (*lua.LFunction, error) {
	proto, err := Compile(source, chunkName)
	if err != nil {
		return nil, err
	}
	return v.L.NewFunctionFromProto(proto), nil
}

// newEnv returns a fresh table holding only the allowlist and the run's host globals.
func (v *VM) newEnv() *lua.LTable {
	env := v.L.CreateTable(0, len(v.safe)+8)
	for k, val := range v.safe {
		env.RawSetString(k, val)
	}
	if v.run != nil {
		for k, val := range v.run.hostGlobals {
			env.RawSetString(k, val)
		}
	}
	return env
}

func (v *VM) raiseLimit(L *lua.LState, msg string) {
	if v.run.limitErr == nil {
		v.run.limitErr = fmt.Errorf("%w: %s", ErrLimitExceeded, msg)
	}
	// Cancelling the run makes the limit uncatchable: pcall may trap this error,
	// but the VM stops at the next instruction boundary.
	v.run.cancel()
	L.RaiseError("%s", msg)
}

// RunOptions configures one execution.
type RunOptions struct {
	Limits      Limits
	Seed        uint64
	Print       func(string)
	Loader      ModuleLoader
	HostGlobals map[string]lua.LValue
}

// Exec runs proto in a fresh environment and returns its results. Callers must
// discard the VM if Tainted() reports true afterwards.
func (v *VM) Exec(ctx context.Context, proto *lua.FunctionProto, opts RunOptions) (rets []lua.LValue, err error) {
	if opts.Limits.PatternSteps == 0 {
		opts.Limits.PatternSteps = DefaultPatternSteps
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	v.run = &runState{
		ctx:         ctx,
		cancel:      cancel,
		limits:      opts.Limits,
		rng:         rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15)),
		print:       opts.Print,
		loader:      opts.Loader,
		modules:     map[string]lua.LValue{},
		loading:     map[string]bool{},
		hostGlobals: opts.HostGlobals,
	}
	defer func() { v.run = nil }()

	L := v.L
	L.Options.MaxConcatLen = opts.Limits.MaxString
	L.Options.OnConcatLimit = func(*lua.LState) {
		if v.run.limitErr == nil {
			v.run.limitErr = fmt.Errorf("%w: string length limit exceeded (%d bytes)", ErrLimitExceeded, opts.Limits.MaxString)
		}
		v.run.cancel()
	}
	L.SetContext(ctx)
	defer L.RemoveContext()
	fn := L.NewFunctionFromProto(proto)
	fn.Env = v.newEnv()

	base := L.GetTop()
	L.Push(fn)
	callErr := func() (perr error) {
		defer func() {
			if p := recover(); p != nil {
				perr = fmt.Errorf("script runtime panic: %v", p)
			}
		}()
		return L.PCall(0, lua.MultRet, nil)
	}()
	if callErr != nil {
		v.tainted = true
		L.SetTop(base)
		return nil, v.classify(ctx, callErr)
	}
	n := L.GetTop() - base
	rets = make([]lua.LValue, n)
	for i := range n {
		rets[i] = L.Get(base + 1 + i)
	}
	L.SetTop(base)
	return rets, nil
}

// Tainted reports whether the VM must be discarded rather than pooled.
func (v *VM) Tainted() bool { return v.tainted }

// Close releases the state.
func (v *VM) Close() { v.L.Close() }

func (v *VM) classify(ctx context.Context, err error) error {
	if v.run.limitErr != nil {
		return &Error{Kind: KindLimit, Message: v.run.limitErr.Error()}
	}
	if ctx.Err() != nil {
		return &Error{Kind: KindTimeout, Message: ctx.Err().Error()}
	}
	var apiErr *lua.ApiError
	if errors.As(err, &apiErr) {
		msg := apiErr.Object.String()
		if strings.Contains(msg, "registry overflow") || strings.Contains(msg, "stack overflow") ||
			strings.Contains(msg, "string length limit exceeded") {
			return &Error{Kind: KindLimit, Message: msg, Traceback: apiErr.StackTrace}
		}
		return &Error{Kind: KindRuntime, Message: msg, Traceback: apiErr.StackTrace}
	}
	return &Error{Kind: KindRuntime, Message: err.Error()}
}
