package script

import (
	"context"
	"sync"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/ids"
)

func init() {
	Register(ModuleDef{
		Doc: ModuleDoc{Name: "testmod", Doc: "test only", Contexts: []Context{UIEvent},
			Funcs:  []FuncDoc{{Name: "twice", Signature: "testmod.twice(n)", Doc: "doubles"}, {Name: "jobonly", Signature: "testmod.jobonly()", Doc: "x", Contexts: []Context{Job}}},
			Values: []ValueDoc{{Name: "NULL", Type: "userdata", Doc: "null"}}},
		Build: func(r *Run) lua.LValue {
			t := r.L.NewTable()
			t.RawSetString("twice", r.L.NewFunction(func(L *lua.LState) int { L.Push(L.CheckNumber(1) * 2); return 1 }))
			t.RawSetString("jobonly", r.L.NewFunction(func(L *lua.LState) int { return 0 }))
			t.RawSetString("NULL", r.Sentinel(NullName))
			return t
		},
	})
}

func mustProto(t *testing.T, src string) *lua.FunctionProto {
	t.Helper()
	p, err := Compile(src, "t")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunnerEntryModulesAndConversion(t *testing.T) {
	rn := NewRunner(4, 2)
	ws := ids.NewWorkspaceID()
	res, err := rn.Exec(context.Background(), ExecRequest{Kind: UIEvent, Workspace: ws, Entry: "handle",
		Proto: mustProto(t, `function handle(a, b) print("hi", a.x) return testmod.twice(b), testmod.jobonly == nil, testmod.NULL, {1, 2}, {k = "v"} end`),
		Args:  []any{map[string]any{"x": int64(7)}, int64(21)}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Values[0] != int64(42) || res.Values[1] != true || res.Values[2] != (Null{}) {
		t.Fatalf("%#v", res.Values)
	}
	if arr, ok := res.Values[3].([]any); !ok || len(arr) != 2 {
		t.Fatalf("array: %#v", res.Values[3])
	}
	if m, ok := res.Values[4].(map[string]any); !ok || m["k"] != "v" {
		t.Fatalf("map: %#v", res.Values[4])
	}
	if len(res.Logs) != 1 || res.Logs[0].Message != "hi\t7" {
		t.Fatalf("logs %+v", res.Logs)
	}
	// Modules are absent outside their contexts.
	res, err = rn.Exec(context.Background(), ExecRequest{Kind: Report, Workspace: ws, Proto: mustProto(t, `return testmod == nil`)})
	if err != nil || res.Values[0] != true {
		t.Fatalf("%v %v", res.Values, err)
	}
	// Missing entry is a script error.
	if _, err := rn.Exec(context.Background(), ExecRequest{Kind: UIEvent, Workspace: ws, Entry: "nope", Proto: mustProto(t, `x = 1`)}); err == nil {
		t.Fatal("missing entry accepted")
	}
}

func TestRunnerTimeoutAndWatchdog(t *testing.T) {
	rn := NewRunner(4, 2)
	ws := ids.NewWorkspaceID()
	start := time.Now()
	_, err := rn.Exec(context.Background(), ExecRequest{Kind: UIEvent, Workspace: ws, Budget: Budget{Wall: 100 * time.Millisecond, MaxString: 1 << 20},
		Proto: mustProto(t, `while true do end`)})
	if k, _ := kindOf(err); k != KindTimeout || time.Since(start) > 150*time.Millisecond {
		t.Fatalf("timeout: %v after %v", err, time.Since(start))
	}
	// Watchdog: with a tiny soft limit, a long-budget run is cancelled as a limit error.
	rn.SetSoftLimit(1)
	_, err = rn.Exec(context.Background(), ExecRequest{Kind: Job, Workspace: ws, Budget: Budget{Wall: 10 * time.Second, MaxString: 1 << 20},
		Proto: mustProto(t, `local t = {} while true do t[#t+1] = {} end`)})
	if k, _ := kindOf(err); k != KindLimit {
		t.Fatalf("watchdog: %v", err)
	}
}

func TestRunnerPerWorkspaceSlots(t *testing.T) {
	rn := NewRunner(8, 1)
	ws := ids.NewWorkspaceID()
	var wg sync.WaitGroup
	started := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = rn.Exec(context.Background(), ExecRequest{Kind: UIEvent, Workspace: ws, Budget: Budget{Wall: 300 * time.Millisecond, MaxString: 1 << 20},
			Proto: mustProto(t, `while true do end`), Host: started})
	}()
	time.Sleep(50 * time.Millisecond)
	_, err := rn.Exec(context.Background(), ExecRequest{Kind: UIEvent, Workspace: ws, Budget: Budget{Wall: 100 * time.Millisecond, MaxString: 1 << 20},
		Proto: mustProto(t, `return 1`)})
	if k, _ := kindOf(err); k != KindTimeout {
		t.Fatalf("slot exhaustion: %v", err)
	}
	// Another workspace is not blocked.
	if _, err := rn.Exec(context.Background(), ExecRequest{Kind: UIEvent, Workspace: ids.NewWorkspaceID(), Proto: mustProto(t, `return 1`)}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestManifestParity(t *testing.T) {
	v := NewVM()
	defer v.Close()
	r := &Run{L: v.L, vm: v, Kind: UIEvent, Ctx: context.Background()}
	if problems := CheckParity(r); len(problems) > 0 {
		t.Fatalf("manifest ↔ registry mismatch:\n%v", problems)
	}
}
