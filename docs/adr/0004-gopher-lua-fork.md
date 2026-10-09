# ADR 0004: Vendored gopher-lua with a concatenation limit

Status: accepted · 2026-10-09

## Context
The Phase 0 security review (finding #2, confirmed) showed that Lua's `..` operator bypasses every string cap in the sandbox: doubling a string 27 times produced a 1 GiB string in 158 ms. gopher-lua v1.1.2 offers no hook into `OP_CONCAT`, and the run context is only checked between VM instructions, so a heap watchdog sampling every 10 ms cannot reliably stop a single multi-GiB allocation before the process is OOM-killed.

## Decision
Vendor gopher-lua v1.1.2 under `third_party/gopher-lua` (MIT, license kept) with a `replace` directive, and apply one minimal patch (documented in `third_party/gopher-lua/PATCHES.md`): `Options.MaxConcatLen` caps the result length of `..` before allocation, and `Options.OnConcatLimit` lets the sandbox cancel the run first so `pcall` cannot swallow the error. The sandbox sets both per run from the context's max string size. No new third-party dependency is introduced (the REPL and its readline dependency were removed).

## Consequences
- Gate 3's `s..s ×40` test is deterministic: SCRIPT_LIMIT_EXCEEDED with negligible heap growth.
- Upgrading gopher-lua means re-applying a ~15-line patch (search for `[AbiData patch]`).
- The heap watchdog (Phase 3) still guards gradual growth (tables, many strings).
