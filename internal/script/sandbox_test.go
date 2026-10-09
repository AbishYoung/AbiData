package script

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func run(t *testing.T, v *VM, src string, budget time.Duration) ([]lua.LValue, error) {
	t.Helper()
	proto, err := Compile(src, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	return v.Exec(ctx, proto, RunOptions{Limits: Limits{MaxString: 1 << 20}})
}

func mustRun(t *testing.T, v *VM, src string) []lua.LValue {
	t.Helper()
	rets, err := run(t, v, src, time.Second)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return rets
}

func kindOf(err error) (ErrorKind, bool) {
	var se *Error
	if errors.As(err, &se) {
		return se.Kind, true
	}
	return 0, false
}

// TestNaiveSandboxLeaks documents why the naive approach (globals-backed env, shared
// library tables, stock pattern functions) is unsafe. These are the failures the
// brief verified; the sandboxed equivalents below must pass.
func TestNaiveSandboxLeaks(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	if err := L.DoString(`string.leak = "x"`); err != nil {
		t.Fatal(err)
	}
	if err := L.DoString(`assert(string.leak == "x"); assert(getfenv ~= nil and loadstring ~= nil and require ~= nil and module ~= nil)`); err != nil {
		t.Fatalf("expected naive state to leak, got %v", err)
	}
	// Stock string.find ignores the context deadline until the match finishes
	// (n=12 takes ~130 ms; n=16 takes ~26 s on an M-series laptop).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	L.SetContext(ctx)
	start := time.Now()
	_ = L.DoString(`string.find(string.rep("a", 12), string.rep("a*", 12) .. "b")`)
	if el := time.Since(start); el < 30*time.Millisecond {
		t.Fatalf("naive pathological match returned in %v; expected it to overrun the 10 ms deadline", el)
	}
}

func TestIsolationLibraryMutation(t *testing.T) {
	v := NewVM()
	defer v.Close()
	_, err := run(t, v, `string.leak = "x"`, time.Second)
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("want read-only error, got %v", err)
	}
	v2 := NewVM() // a tainted VM is discarded; also check same-VM persistence below
	defer v2.Close()
	rets := mustRun(t, v2, `return string.leak == nil, table.leak == nil, math.leak == nil`)
	for i, r := range rets {
		if r != lua.LTrue {
			t.Errorf("ret %d = %v", i, r)
		}
	}
	for _, src := range []string{`table.insert = nil`, `math.pi = 3`, `rawset(string, "x", 1)`} {
		if _, err := run(t, NewVM(), src, time.Second); err == nil {
			t.Errorf("%s: expected error", src)
		}
	}
}

func TestIsolationGlobalsDoNotPersist(t *testing.T) {
	v := NewVM()
	defer v.Close()
	mustRun(t, v, `leaked = 42; function f() return 1 end`)
	if v.Tainted() {
		t.Fatal("successful run tainted VM")
	}
	rets := mustRun(t, v, `return leaked == nil and f == nil`)
	if rets[0] != lua.LTrue {
		t.Fatal("globals from run A visible in run B on the same VM")
	}
}

func TestIsolationStringMetatable(t *testing.T) {
	v := NewVM()
	defer v.Close()
	rets := mustRun(t, v, `return getmetatable(""), getmetatable(string), ("abc"):upper()`)
	if rets[0].String() != "locked" || rets[1].String() != "locked" || rets[2].String() != "ABC" {
		t.Fatalf("got %v", rets)
	}
	// getmetatable("") is the string "locked", so .__index resolves through the
	// string proxy and finds nothing: the real string table is unreachable.
	if r := mustRun(t, v, `return getmetatable("").__index`); r[0] != lua.LNil {
		t.Fatalf("getmetatable(\"\").__index reachable: %v", r[0])
	}
	for _, src := range []string{
		`setmetatable("", {})`, `setmetatable(1, {__index = function() return 1 end})`,
		`setmetatable(print, {})`, `setmetatable(string, {})`, `setmetatable(true, {})`,
	} {
		if _, err := run(t, NewVM(), src, time.Second); err == nil {
			t.Errorf("%s: expected error", src)
		}
	}
	// Setting a metatable on a plain table still works.
	rets = mustRun(t, v, `local t = setmetatable({}, {__index = function() return 7 end}); return t.x`)
	if rets[0].String() != "7" {
		t.Fatalf("got %v", rets)
	}
}

func TestIsolationDangerousGlobalsAbsent(t *testing.T) {
	v := NewVM()
	defer v.Close()
	names := []string{"getfenv", "setfenv", "loadstring", "load", "loadfile", "dofile", "module",
		"collectgarbage", "rawset", "newproxy", "_printregs", "coroutine", "os", "io", "debug",
		"package", "channel", "_G"}
	for _, n := range names {
		rets := mustRun(t, v, `return `+n+` == nil`)
		if rets[0] != lua.LTrue {
			t.Errorf("%s is reachable", n)
		}
	}
	if _, err := run(t, v, `return require("os")`, time.Second); err == nil {
		t.Fatal(`require("os") should fail`)
	}
	rets := mustRun(t, v, `return string.dump == nil`)
	if rets[0] != lua.LTrue {
		t.Error("string.dump reachable")
	}
}

func TestTerminationInfiniteLoop(t *testing.T) {
	v := NewVM()
	defer v.Close()
	budget := 200 * time.Millisecond
	start := time.Now()
	_, err := run(t, v, `while true do end`, budget)
	el := time.Since(start)
	if k, ok := kindOf(err); !ok || k != KindTimeout {
		t.Fatalf("want timeout, got %v", err)
	}
	if el > budget+50*time.Millisecond {
		t.Fatalf("timeout took %v (budget %v)", el, budget)
	}
	if !v.Tainted() {
		t.Fatal("timed-out VM not tainted")
	}
}

func TestTerminationPcallCannotSwallowTimeout(t *testing.T) {
	v := NewVM()
	defer v.Close()
	start := time.Now()
	_, err := run(t, v, `while true do pcall(function() while true do end end) end`, 200*time.Millisecond)
	if k, ok := kindOf(err); !ok || k != KindTimeout {
		t.Fatalf("want timeout, got %v", err)
	}
	if el := time.Since(start); el > 300*time.Millisecond {
		t.Fatalf("took %v", el)
	}
}

func TestTerminationPathologicalPattern(t *testing.T) {
	v := NewVM()
	defer v.Close()
	start := time.Now()
	_, err := run(t, v, `string.find(string.rep("a",28), string.rep("a*",28).."b")`, time.Second)
	el := time.Since(start)
	if k, ok := kindOf(err); !ok || k != KindLimit {
		t.Fatalf("want limit, got %v", err)
	}
	if el > 1100*time.Millisecond {
		t.Fatalf("took %v", el)
	}
}

func TestTerminationPatternHonorsDeadline(t *testing.T) {
	v := NewVM()
	defer v.Close()
	proto, _ := Compile(`string.find(string.rep("a",28), string.rep("a*",28).."b")`, "t")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := v.Exec(ctx, proto, RunOptions{Limits: Limits{MaxString: 1 << 20, PatternSteps: 1 << 40}})
	if k, ok := kindOf(err); !ok || k != KindTimeout {
		t.Fatalf("want timeout, got %v", err)
	}
	if el := time.Since(start); el > 100*time.Millisecond {
		t.Fatalf("deadline ignored for %v", el)
	}
}

func TestStringCaps(t *testing.T) {
	cases := []string{
		`string.rep("x", 1e9)`,
		`string.rep("xy", 2^30)`,
		`string.format("%099999d", 1)`,
		`local t = {} for i=1,2000 do t[i] = string.rep("x", 1000) end return table.concat(t)`,
		`return (string.rep("x", 1000)):gsub("x", string.rep("y", 2000))`,
	}
	for _, src := range cases {
		v := NewVM()
		_, err := run(t, v, src, time.Second)
		if err == nil {
			t.Errorf("%s: expected error", src)
		}
		if k, _ := kindOf(err); src != `string.format("%099999d", 1)` && k != KindLimit {
			t.Errorf("%s: want limit, got %v", src, err)
		}
		v.Close()
	}
}

func TestInfiniteRecursion(t *testing.T) {
	v := NewVM()
	defer v.Close()
	_, err := run(t, v, `local function f() return 1 + f() end return f()`, time.Second)
	if err == nil {
		t.Fatal("expected stack error")
	}
	_, err = run(t, NewVM(), `return unpack({}, 1, 1e8)`, time.Second)
	if err == nil {
		t.Fatal("expected unpack overflow error")
	}
}

func TestStringFunctionsStillWork(t *testing.T) {
	v := NewVM()
	defer v.Close()
	rets := mustRun(t, v, `
		local a, b = string.find("hello world", "o w")
		local m = string.match("key=val", "(%w+)=(%w+)")
		local g = {}
		for w in string.gmatch("one two three", "%a+") do g[#g+1] = w end
		local s, n = string.gsub("hello world", "o", "0")
		local s2 = string.gsub("abc", "%w", {a = "1", b = true and "2"})
		local s3 = string.gsub("abc", "(%w)", function(c) return c:upper() end)
		local s4 = string.gsub("hello", "(l)(l)", "%2%1")
		return a, b, m, table.concat(g, ","), s, n, s2, s3, s4, string.format("%5.2f|%s", 3.14159, "x"), ("x"):rep(3, "")
	`)
	want := []string{"5", "7", "key", "one,two,three", "hell0 w0rld", "2", "12c", "ABC", "hello", " 3.14|x", "xxx"}
	for i, w := range want {
		if rets[i].String() != w {
			t.Errorf("ret %d = %q, want %q", i, rets[i].String(), w)
		}
	}
}

func TestMathRandomPerRunSeed(t *testing.T) {
	v := NewVM()
	defer v.Close()
	a := mustRun(t, v, `return math.random(1, 1000000)`)
	b := mustRun(t, v, `return math.random(1, 1000000)`)
	if a[0].String() != b[0].String() {
		t.Fatal("same seed should give same sequence per run (no shared generator state)")
	}
	mustRun(t, v, `math.randomseed(7)`)
	c := mustRun(t, v, `return math.random(1, 1000000)`)
	if c[0].String() != a[0].String() {
		t.Fatal("randomseed leaked into the next run")
	}
}

func TestRequireModulesOnly(t *testing.T) {
	v := NewVM()
	defer v.Close()
	proto, _ := Compile(`local m = require("util"); local m2 = require("util"); return m.double(21), m == m2, helper == nil`, "t")
	loads := 0
	rets, err := v.Exec(context.Background(), proto, RunOptions{
		Limits: Limits{MaxString: 1 << 20},
		Loader: func(name string) (string, bool) {
			if name != "util" {
				return "", false
			}
			loads++
			return `helper = 1; local M = {} function M.double(x) return x * 2 end return M`, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rets[0].String() != "42" || rets[1] != lua.LTrue || rets[2] != lua.LTrue || loads != 1 {
		t.Fatalf("got %v loads=%d", rets, loads)
	}
}

func BenchmarkNewVM(b *testing.B) {
	for b.Loop() {
		NewVM().Close()
	}
}

func BenchmarkFreshEnvRun(b *testing.B) {
	v := NewVM()
	defer v.Close()
	proto, _ := Compile(`return 1 + 1`, "b")
	ctx := context.Background()
	for b.Loop() {
		if _, err := v.Exec(ctx, proto, RunOptions{Limits: Limits{MaxString: 1 << 20}}); err != nil {
			b.Fatal(err)
		}
	}
}

// Regression (Phase 0 security review #1): table library functions write raw and
// must not be able to mutate the read-only library proxies.
func TestProxyNotWritableViaTableLib(t *testing.T) {
	v := NewVM()
	defer v.Close()
	for _, src := range []string{
		`table.insert(string, "leak")`, `table.insert(math, 1, "leak")`, `table.insert(table, "leak")`,
		`table.remove(string)`, `table.sort(string)`,
	} {
		if _, err := run(t, v, src, time.Second); err == nil {
			t.Errorf("%s: expected error", src)
		}
	}
	rets := mustRun(t, v, `return string[1] == nil and ("")[1] == nil and math[1] == nil and table[1] == nil`)
	if rets[0] != lua.LTrue {
		t.Fatal("proxy mutated through table library")
	}
}

// Regression (#6): limit errors cannot be swallowed by pcall.
func TestLimitNotSwallowedByPcall(t *testing.T) {
	v := NewVM()
	defer v.Close()
	_, err := run(t, v, `for i = 1, 5 do pcall(string.rep, "x", 1e9) end return "survived"`, time.Second)
	if k, ok := kindOf(err); !ok || k != KindLimit {
		t.Fatalf("want limit error, got %v", err)
	}
}

// Regression (#5): builtins that push many values are capped.
func TestBuiltinValueCountCaps(t *testing.T) {
	for _, src := range []string{
		`local s = string.rep("x", 1000000) return string.byte(s, 1, -1)`,
		`return unpack({}, 1, 1e8)`,
	} {
		v := NewVM()
		start := time.Now()
		_, err := run(t, v, src, 2*time.Second)
		if err == nil {
			t.Errorf("%s: expected error", src)
		}
		if el := time.Since(start); el > 500*time.Millisecond {
			t.Errorf("%s: took %v", src, el)
		}
		v.Close()
	}
}

// Regression (#11): the pattern step budget is per operation, not per run.
func TestPatternBudgetPerOperation(t *testing.T) {
	v := NewVM()
	defer v.Close()
	mustRun(t, v, `local s = string.rep("ab", 200) for i = 1, 5000 do string.find(s, "b+a") end`)
}

// Gate 3 / review #2: repeated doubling with `..` is stopped by the concat cap
// long before it can exhaust memory.
func TestConcatDoublingStopped(t *testing.T) {
	v := NewVM()
	defer v.Close()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, err := run(t, v, `local s = "x" for i = 1, 40 do s = s .. s end return #s`, 5*time.Second)
	if k, ok := kindOf(err); !ok || k != KindLimit {
		t.Fatalf("want limit, got %v", err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 64<<20 {
		t.Fatalf("heap grew by %d MiB", grew>>20)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("took %v", el)
	}
	// A pcall cannot swallow it either (the run is cancelled).
	_, err = run(t, NewVM(), `local s = "x" for i = 1, 40 do pcall(function() s = s .. s end) end return #s`, 5*time.Second)
	if k, _ := kindOf(err); k != KindLimit {
		t.Fatalf("pcall swallowed concat limit: %v", err)
	}
}
