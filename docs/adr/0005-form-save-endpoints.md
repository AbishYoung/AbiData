# ADR 0005: Form saves go through /forms/{id}/save and /delete

Status: accepted · 2026-10-09

## Context
The brief's form lifecycle says "Save sends `PATCH /rows/{id}` with `If-Match` and an `Idempotency-Key`. The server runs `before_update` (outside the writer), then the OCC write, then `after_update`." The generic rows endpoint has no form context, so it cannot know which form module's handlers to run, which form permissions apply (allow_edits/additions/deletions), or which calculated controls to recompute.

## Decision
Forms save and delete through form-scoped endpoints: `POST /w/{ws}/forms/{id}/save` (`{record_id, record_version, is_new, fields}` + `Idempotency-Key`) and `POST /w/{ws}/forms/{id}/delete` (`{record_id, record_version}`). They run exactly the brief's sequence — `before_insert`/`before_update` outside the writer (may cancel or assign fields), the OCC write through the same `records` core (If-Match semantics via `record_version`, idempotency stored in the same transaction), then `after_update` — and return the D10 patch response including recomputed calculated controls. The plain rows API is unchanged and remains what datasheets use.

## Consequences
Form clients use one endpoint per lifecycle action; OCC, idempotency, validation translation, history and the change feed are shared with the rows API, so there is no second write path to secure.
