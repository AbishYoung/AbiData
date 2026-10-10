package secreview

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"abidata/internal/ids"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
)

// Regression tests for the Phase 3 security review's sandbox findings (each
// comment describes the original issue; the assertions pin the fix).
func proto(t *testing.T, src string) script.ExecRequest {
	t.Helper()
	p, err := script.Compile(src, "t")
	if err != nil {
		t.Fatal(err)
	}
	return script.ExecRequest{Kind: script.UIEvent, Workspace: ids.NewWorkspaceID(), Proto: p}
}

// allocDuring returns bytes allocated (TotalAlloc delta) while fn runs.
func allocDuring(fn func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	fn()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

const mib = 1 << 20

// F1 (fixed; regression): string.format built the whole result with fmt.Sprintf before checking it
// against MaxString. With N "%s" arguments that each hold a max-size string the
// allocation is N × MaxString in one uninterruptible Go call. N is bounded only
// by unpack's 8000-result cap: 8000 × 1 MiB = ~8 GiB in a ui_event (1 MiB cap),
// ~128 GiB in a job (16 MiB cap).
func TestStringFormatAmplification(t *testing.T) {
	rn := script.NewRunner(4, 4)
	req := proto(t, `
		local s = string.rep("x", 1024*1024 - 1)
		local args = {}
		for i = 1, 200 do args[i] = s end
		return pcall(string.format, string.rep("%s", 200), unpack(args))`)
	var err error
	got := allocDuring(func() { _, err = rn.Exec(context.Background(), req) })
	t.Logf("string.format with 200 × 1 MiB args allocated %d MiB (MaxString 1 MiB); err=%v", got/mib, err)
	if got > 32*mib {
		t.Fatalf("string.format allocated %d MiB before the cap check", got/mib)
	}
}

// F2 (fixed; regression): string.gsub expanded a "%0%0…" replacement for one match fully before the
// per-match checkLen. One 16 KiB match × 8000 "%0" = 125 MiB of output (750 MiB
// allocated with builder growth) here; a ~20 KiB match (pm recursion cap) with a
// 1 MiB replacement of "%0" (524,288 copies) asks for ~10 GiB in one call.
func TestGsubReplacementAmplification(t *testing.T) {
	rn := script.NewRunner(4, 4)
	req := proto(t, `
		local s = string.rep("a", 16*1024)
		local ok, r = pcall(string.gsub, s, "a+", string.rep("%0", 8000))
		return ok, (type(r) == "string" and #r < 100) and r or type(r)`)
	var err error
	var res script.ExecResult
	got := allocDuring(func() { res, err = rn.Exec(context.Background(), req) })
	t.Logf("gsub allocated %d MiB (MaxString 1 MiB); res=%v err=%v", got/mib, res.Values, err)
	if got > 32*mib {
		t.Fatalf("gsub allocated %d MiB before the cap check", got/mib)
	}
}

// F3 (fixed; regression): csv.encode wrote a whole record before checking MaxString. A row table
// holding the same 1 MiB string 200 times costs nothing in Lua and makes the
// csv writer allocate ≥200 MiB in one call (any row length works).
func TestCSVEncodeRowAmplification(t *testing.T) {
	rn := script.NewRunner(4, 4)
	req := proto(t, `
		local s = string.rep("x", 1024*1024 - 1)
		local row = {}
		for i = 1, 200 do row[i] = s end
		return pcall(csv.encode, {row}, {header = false})`)
	var err error
	got := allocDuring(func() { _, err = rn.Exec(context.Background(), req) })
	t.Logf("csv.encode allocated %d MiB (MaxString 1 MiB); err=%v", got/mib, err)
	if got > 32*mib {
		t.Fatalf("csv.encode allocated %d MiB before the cap check", got/mib)
	}
}

// F4 (fixed; regression): script.FromLua walked a DAG of shared tables as a tree (only a depth cap of
// 64), so 22 Lua table constructions expand to 2^22 Go maps in one host call
// that never checks ctx. Reached by log.*(msg, fields), db.* params, ui.set
// {value=…}, ui.open_form/open_report opts, record.<f> = v, records.insert/update
// values, report __emit, and entry return values. At depth ~40 the call runs
// effectively forever and exhausts memory; the 1 s ui_event budget does not apply.
func TestFromLuaDAGBlowup(t *testing.T) {
	rn := script.NewRunner(4, 4)
	req := proto(t, `
		local t = {}
		for i = 1, 24 do t = {a = t, b = t} end
		log.info("x", {f = t})
		return 1`)
	req.Budget = script.Budget{Wall: 200 * time.Millisecond, MaxString: 1 << 20, Writes: true}
	start := time.Now()
	_, err := rn.Exec(context.Background(), req)
	el := time.Since(start)
	t.Logf("log.info with a 24-level shared-table DAG took %v (budget 200ms); err=%v", el, err)
	if el > time.Second || err == nil || !strings.Contains(err.Error(), "elements") {
		t.Fatalf("expected the node budget to stop the conversion quickly: %v %v", el, err)
	}
}

// F5 (fixed; regression): Runner.Exec took a global slot before the per-workspace slot and keeps
// holding it while it waits for the workspace slot. One workspace that fires
// more runs than its per-workspace cap therefore parks its excess runs on the
// remaining global slots, and every other tenant gets "the server is busy".
func TestOneWorkspaceStarvesGlobalSlots(t *testing.T) {
	runtime.GC()
	rn := script.NewRunner(4, 2) // production: global 2×GOMAXPROCS, perWS max(2, global/2)
	hog := ids.NewWorkspaceID()
	spin, err := script.Compile(`while true do end`, "spin")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 { // 2 running + 2 parked holding global slots
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rn.Exec(context.Background(), script.ExecRequest{Kind: script.Job, Workspace: hog, Proto: spin,
				Budget: script.Budget{Wall: time.Second, MaxString: 1 << 20}})
		}()
	}
	time.Sleep(200 * time.Millisecond)
	victim, _ := script.Compile(`return 1`, "v")
	_, err = rn.Exec(context.Background(), script.ExecRequest{Kind: script.UIEvent, Workspace: ids.NewWorkspaceID(), Proto: victim})
	wg.Wait()
	t.Logf("other workspace's trivial run: err=%v", err)
	if err != nil {
		t.Fatalf("another workspace was starved: %v", err)
	}
}

// F9 (fixed; regression): "uncatchable" limit errors were catchable in tail position. raiseLimit
// cancels the run so the next VM instruction fails, but `return pcall(...)` as
// the last statement is a TAILCALL to a Go function with no further instruction,
// so the run completes successfully with (false, msg).
func TestLimitErrorEscapesViaTailPcall(t *testing.T) {
	rn := script.NewRunner(4, 4)
	res, err := rn.Exec(context.Background(), proto(t, `return pcall(string.rep, "x", 2^30)`))
	t.Logf("values=%v err=%v", res.Values, err)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("a tail pcall swallowed the limit error: %v", err)
	}
}

// F6 (fixed; regression): the heap watchdog cancelled the run with the largest *budget*, not the one
// allocating. A long job in workspace B is killed while a ui_event in workspace A
// is the one growing the heap.
func TestWatchdogKillsBystander(t *testing.T) {
	rn := script.NewRunner(4, 4)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rn.SoftLimit = ms.HeapAlloc + 64*mib
	spin, _ := script.Compile(`local x = 0 while true do x = x + 1 end`, "spin")
	jobErr := make(chan error, 1)
	go func() {
		_, err := rn.Exec(context.Background(), script.ExecRequest{Kind: script.Job, Workspace: ids.NewWorkspaceID(), Proto: spin,
			Budget: script.Budget{Wall: 2 * time.Second, MaxString: 16 << 20}})
		jobErr <- err
	}()
	time.Sleep(50 * time.Millisecond)
	hog, _ := script.Compile(`
		local t = {}
		for i = 1, 400 do t[i] = string.rep(tostring(i), 100000) end
		local x = 0 for i = 1, 3e7 do x = x + 1 end
		return #t`, "hog")
	_, hogErr := rn.Exec(context.Background(), script.ExecRequest{Kind: script.UIEvent, Workspace: ids.NewWorkspaceID(), Proto: hog,
		Budget: script.Budget{Wall: 4 * time.Second, MaxString: 1 << 20}})
	err := <-jobErr
	t.Logf("bystander job: %v; allocating ui_event: %v", err, hogErr)
	if err != nil && strings.Contains(err.Error(), "memory") {
		t.Fatalf("the watchdog killed the bystander job: %v", err)
	}
	if hogErr == nil || !strings.Contains(hogErr.Error(), "memory") {
		t.Fatalf("the watchdog did not stop the allocating run: %v", hogErr)
	}
}
