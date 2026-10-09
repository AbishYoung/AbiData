# ADR 0002: Lua sandbox construction (Spike S2)

Status: accepted · 2026-10-09

## Result
`internal/script` (`sandbox.go`, `strlib.go`) and the vendored, budgeted `internal/script/pm` implement D9. `sandbox_test.go` passes under `-race`:
- `TestNaiveSandboxLeaks` shows the naive approach leaking (`string.leak` persists; `getfenv`/`loadstring`/`require`/`module` reachable; stock `string.find` overruns a 10 ms deadline).
- The sandboxed equivalents pass: read-only library proxies, locked string metatable, dangerous globals absent, globals do not persist across runs on one VM, `while true do end` stops within budget + 50 ms, the Gate 3 pathological pattern returns SCRIPT_LIMIT_EXCEEDED, the pattern VM honours ctx, string caps on rep/format/gsub/table.concat, recursion and `unpack` overflow are errors rather than crashes, per-run `math.random` seeding, `require` only for module scripts.

## Additional finding (beyond the brief)
gopher-lua's `setmetatable` accepts strings, numbers, booleans, functions and userdata, and writes **per-state builtin metatables** for non-table types. That would let run A install `setmetatable(1, {__index=…})` and leak it into run B, or replace a host userdata's metatable. The sandbox exposes a `setmetatable` that follows Lua 5.1 and accepts tables only.

## Other changes vs. the stock library
- Vendored `pm`: recursion limit lowered from 1,000,000 to 20,000 to bound Go stack growth; `%b` and back-reference scans are charged per byte.
- `gsub` builds its output linearly (the stock version is quadratic) with a running size cap.
- `string.format` rejects width/precision longer than two digits, as Lua 5.1 does, before formatting.

## Measurements (Apple M-series, `go test -bench`)
- `NewVM` (state + libs + proxies): ~29 µs, ~149 KB.
- Fresh environment + trivial run on a pooled VM: ~2.5 µs, ~9 KB.

Pools are therefore cheap to refill; pool size follows the global script semaphore (default 2×GOMAXPROCS) as specified, and any tainted VM is simply discarded.
