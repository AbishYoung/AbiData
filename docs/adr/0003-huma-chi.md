# ADR 0003: huma v2 on chi for the API contract (Spike S5)

Status: accepted · 2026-10-09

huma v2.39.1 with the `humachi` adapter on chi v5.3.2 generates OpenAPI **3.1.0**. Overriding `huma.NewError` with a type that implements `huma.StatusError` produces the brief's `{"error":{code,title,message,recovery,request_id,details}}` envelope for both handler errors and huma's own validation errors. The envelope is referenced from every declared error response, and `openapi-typescript` 7.13 turns the `code` enum into a TS string union.

Decisions:
- D18 primary path adopted (no tygo fallback).
- The `$schema` link transformer is disabled so bodies match the brief.
- huma's request-validation status (422) is remapped to `BAD_REQUEST`/400 in the error hook; `VALIDATION_FAILED`/422 stays reserved for column validation rules.
- Streaming endpoints (SSE, NDJSON report runs, file downloads) are plain chi handlers registered alongside huma; they are documented in the spec by hand-registered operations where useful.
