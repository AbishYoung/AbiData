# Local patches to gopher-lua v1.1.2

Vendored from github.com/yuin/gopher-lua v1.1.2 (MIT, see LICENSE) and wired in
with a `replace` directive in the root go.mod (docs/adr/0004-gopher-lua-fork.md).
Removed: `cmd/` (REPL; readline dependency), upstream tests and go-inline
templates (`_vm.go`, `_state.go`; `vm.go`/`state.go` are the generated output).

Changes (search for "[AbiData patch]"):
1. `Options.MaxConcatLen` — when > 0, `stringConcat` (the `..` operator) raises a
   Lua error before allocating a result longer than the limit. The sandbox sets
   it per run to the context's max string size. `Options.OnConcatLimit` is called
   first so the sandbox can cancel the run (making the error uncatchable by pcall).
2. `LState.NoteAlloc` / `Allocated` / `ResetAllocated` — a per-state (root of
   coroutines) atomic estimate of bytes allocated, fed by `..` results and
   `OP_NEWTABLE`, plus host functions that build strings. The sandbox's heap
   watchdog uses it to stop the run that is actually growing the heap.
