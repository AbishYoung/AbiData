package script

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/ids"
)

// Principal is the identity a script runs as.
type Principal struct {
	UserID      string
	Email       string
	DisplayName string
	Role        string // workspace role used for data rights (jobs run as "editor")
}

// LogEntry is one script log line.
type LogEntry struct {
	At      time.Time      `json:"at"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Run is the per-execution state host modules see.
type Run struct {
	Ctx       context.Context
	Kind      Context
	Workspace ids.WorkspaceID
	Principal Principal
	TZ        *time.Location
	Budget    Budget
	// Host carries context-specific bindings (form session, job run, DB access)
	// for the host modules that need them.
	Host any
	L    *lua.LState

	vm     *VM
	mu     sync.Mutex
	logs   []LogEntry
	logLen int
}

// maxLogBytes caps a run's log (tail kept), per brief (256 KiB).
const maxLogBytes = 256 << 10

// Log appends to the run log (keeping the tail within maxLogBytes).
func (r *Run) Log(level, msg string, fields map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := LogEntry{At: time.Now(), Level: level, Message: msg, Fields: fields}
	r.logs = append(r.logs, e)
	r.logLen += len(msg) + 64
	for r.logLen > maxLogBytes && len(r.logs) > 1 {
		r.logLen -= len(r.logs[0].Message) + 64
		r.logs = r.logs[1:]
	}
}

// Logs returns the run log.
func (r *Run) Logs() []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]LogEntry(nil), r.logs...)
}

// Sentinel returns a per-VM singleton userdata (stable identity across the run)
// with a locked metatable whose __tostring is name.
func (r *Run) Sentinel(name string) *lua.LUserData {
	if r.vm.sentinels == nil {
		r.vm.sentinels = map[string]*lua.LUserData{}
	}
	if ud, ok := r.vm.sentinels[name]; ok {
		return ud
	}
	ud := r.L.NewUserData()
	ud.Value = name
	mt := r.L.NewTable()
	mt.RawSetString("__metatable", lua.LString("locked"))
	mt.RawSetString("__tostring", r.L.NewFunction(func(L *lua.LState) int { L.Push(lua.LString(name)); return 1 }))
	ud.Metatable = mt
	r.vm.sentinels[name] = ud
	return ud
}

// RaiseLimit stops the run with SCRIPT_LIMIT_EXCEEDED (uncatchable by pcall).
func (r *Run) RaiseLimit(msg string) { r.vm.raiseLimit(r.L, msg) }

// ExecRequest asks the runner to execute a compiled chunk.
type ExecRequest struct {
	Kind      Context
	Workspace ids.WorkspaceID
	Principal Principal
	TZ        *time.Location
	Budget    Budget // zero → DefaultBudgets[Kind]
	Proto     *lua.FunctionProto
	ChunkName string
	Entry     string
	Args      []any // converted with ToLua
	Host      any
	Loader    ModuleLoader
	Seed      uint64
}

// ExecResult is the outcome of a run.
type ExecResult struct {
	Values []any
	Logs   []LogEntry
	Took   time.Duration
}

// Runner executes scripts on pooled VMs under global and per-workspace
// concurrency limits, with a heap watchdog.
type Runner struct {
	global  chan struct{}
	perWS   int
	mu      sync.Mutex
	wsSlots map[ids.WorkspaceID]chan struct{}
	pool    chan *VM

	active   map[*activeRun]struct{}
	activeMu sync.Mutex
	wake     chan struct{}
	// SoftLimit is the heap size above which the watchdog cancels runs
	// (default 75% of GOMEMLIMIT, or 1 GiB when no limit is set).
	SoftLimit uint64
}

type activeRun struct {
	cancel context.CancelCauseFunc
	budget time.Duration
}

// ErrMemory is the watchdog's cancellation cause.
var ErrMemory = errors.New("script stopped: server memory limit reached")

// NewRunner creates a runner allowing global concurrent runs (0 = 2×GOMAXPROCS
// semantics applied by caller) and perWorkspace concurrent runs per workspace.
func NewRunner(global, perWorkspace int) *Runner {
	if global <= 0 {
		global = 8
	}
	if perWorkspace <= 0 {
		perWorkspace = max(2, global/2)
	}
	soft := uint64(1 << 30)
	if lim := debug.SetMemoryLimit(-1); lim > 0 && lim < math.MaxInt64 {
		soft = uint64(lim) / 4 * 3
	}
	r := &Runner{global: make(chan struct{}, global), perWS: perWorkspace, wsSlots: map[ids.WorkspaceID]chan struct{}{},
		pool: make(chan *VM, global), active: map[*activeRun]struct{}{}, wake: make(chan struct{}, 1), SoftLimit: soft}
	go r.watchdog()
	return r
}

func (rn *Runner) slots(ws ids.WorkspaceID) chan struct{} {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	s, ok := rn.wsSlots[ws]
	if !ok {
		s = make(chan struct{}, rn.perWS)
		rn.wsSlots[ws] = s
	}
	return s
}

func (rn *Runner) getVM() *VM {
	select {
	case v := <-rn.pool:
		return v
	default:
		return NewVM()
	}
}

func (rn *Runner) putVM(v *VM) {
	if v.Tainted() {
		v.Close()
		return
	}
	select {
	case rn.pool <- v:
	default:
		v.Close()
	}
}

// Exec runs req. Errors are *Error (classified) or context errors.
func (rn *Runner) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	b := req.Budget
	if b.Wall == 0 {
		b = DefaultBudgets[req.Kind]
	}
	// Waiting for a slot counts against the budget's wall time.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	ctx, cancelT := context.WithTimeout(ctx, b.Wall)
	defer cancelT()
	select {
	case rn.global <- struct{}{}:
		defer func() { <-rn.global }()
	case <-ctx.Done():
		return ExecResult{}, &Error{Kind: KindTimeout, Message: "the server is busy running other scripts"}
	}
	ws := rn.slots(req.Workspace)
	select {
	case ws <- struct{}{}:
		defer func() { <-ws }()
	case <-ctx.Done():
		return ExecResult{}, &Error{Kind: KindTimeout, Message: "this workspace is busy running other scripts"}
	}
	ar := &activeRun{cancel: cancel, budget: b.Wall}
	rn.activeMu.Lock()
	rn.active[ar] = struct{}{}
	rn.activeMu.Unlock()
	select {
	case rn.wake <- struct{}{}:
	default:
	}
	defer func() {
		rn.activeMu.Lock()
		delete(rn.active, ar)
		rn.activeMu.Unlock()
	}()

	vm := rn.getVM()
	defer rn.putVM(vm)
	tz := req.TZ
	if tz == nil {
		tz = time.UTC
	}
	run := &Run{Ctx: ctx, Kind: req.Kind, Workspace: req.Workspace, Principal: req.Principal, TZ: tz, Budget: b, Host: req.Host, L: vm.L, vm: vm}
	start := time.Now()
	args := make([]lua.LValue, len(req.Args))
	for i, a := range req.Args {
		args[i] = ToLua(run, a)
	}
	rets, err := vm.Exec(ctx, req.Proto, RunOptions{
		Limits: Limits{MaxString: b.MaxString}, Seed: req.Seed, Loader: req.Loader,
		Print:       func(s string) { run.Log("info", s, nil) },
		HostGlobals: hostGlobals(run), Entry: req.Entry, EntryArgs: args,
	})
	res := ExecResult{Logs: run.Logs(), Took: time.Since(start)}
	if err != nil {
		if errors.Is(context.Cause(ctx), ErrMemory) {
			vm.tainted = true
			return res, &Error{Kind: KindLimit, Message: ErrMemory.Error()}
		}
		var se *Error
		if errors.As(err, &se) && se.Kind == KindTimeout {
			se.Message = fmt.Sprintf("the script ran longer than its %s budget and was stopped", b.Wall)
		}
		return res, err
	}
	res.Values = make([]any, len(rets))
	for i, v := range rets {
		res.Values[i] = FromLua(run, v)
	}
	return res, nil
}

// watchdog samples heap usage every 10 ms while scripts run and cancels the
// largest-budget run when above the soft limit.
func (rn *Runner) watchdog() {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		rn.activeMu.Lock()
		n := len(rn.active)
		rn.activeMu.Unlock()
		if n == 0 {
			<-rn.wake
			continue
		}
		<-t.C
		metrics.Read(sample)
		if sample[0].Value.Kind() != metrics.KindUint64 || sample[0].Value.Uint64() < rn.SoftLimit {
			continue
		}
		rn.activeMu.Lock()
		var victim *activeRun
		for a := range rn.active {
			if victim == nil || a.budget > victim.budget {
				victim = a
			}
		}
		if victim != nil {
			victim.cancel(ErrMemory)
			delete(rn.active, victim)
		}
		rn.activeMu.Unlock()
	}
}
