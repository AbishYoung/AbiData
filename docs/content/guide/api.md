---
title: API access
order: 85
summary: Let bots and other programs read and write a workspace with an API key.
---

# API access

Everything the web app does goes through a JSON API, and workspace owners can give that API to other programs. A program authenticates with an **API key** that belongs to one workspace. Use it for scheduled imports from another system, a bot that adds records, a dashboard that reads a query, or a script on your own machine.

This page covers creating keys, signing requests, and worked `curl` examples for the common tasks. Every route is described in the OpenAPI document (see [The OpenAPI document](#openapi)).

## Create a key {#create}

Only workspace owners can create and revoke keys.

1. Open **Workspace settings** from the user menu and go to **API keys**.
2. Choose **Create API key**. Give it a name that says what uses it, such as "Nightly stock sync".
3. Pick the access level and an expiry (see below).
4. Copy the key from the next dialog and store it in your secret manager.

**The full key is shown exactly once.** AbiData keeps only a keyed hash of it, so nobody, including administrators, can read it again. If you lose a key, revoke it and create another.

A key looks like `abd_01J9Z3...Q4_...` and is about 74 characters long. Treat it like a password.

### Access levels {#roles}

A key has a role in its workspace, and the [permissions table](/docs/guide/members-and-roles#permissions) applies to it unchanged.

| Role | What the key can do |
|---|---|
| Viewer | Read tables, run saved queries and reports |
| Editor | Also add, change and delete records, and run unsaved visual queries |
| Builder | Also change tables, queries, forms, scripts and jobs, import data and run jobs |

A key can never be an **owner**. It cannot manage members, backups, the egress allowlist or other keys, and it cannot create or revoke keys. Choose the lowest role that does the job: a bot that only adds records needs Editor, and a dashboard that only reads needs Viewer.

### What a key can reach {#scope}

- A key works only on the workspace routes `/api/v1/w/<workspace id>/...` of **its own workspace**. Another workspace answers `404 NOT_FOUND`, exactly as it does for someone who is not a member.
- A key never works on `/api/v1/admin/...`, `/api/v1/auth/...`, `/metrics` or the log stream. It gets `403 FORBIDDEN` or `401 UNAUTHENTICATED` there.
- Writes made with a key are recorded as the key: in a record's **History**, in audit entries and in the `created_by` of anything it creates, as `API key "Nightly stock sync"`.
- A key belongs to the workspace, not to the person who created it. If that person is removed from the workspace or leaves, **the key keeps working**. Owners and platform administrators can revoke it at any time (**API keys** in the workspace settings, or **API keys** in the admin console).
- Revoking is immediate: the next request, and any open event stream within 30 seconds, is refused.
- A workspace can hold at most 25 active keys.

## Sign requests {#auth}

Send the key in an `Authorization` header on every request. There is no login step and no cookie.

```bash
export ABIDATA=https://abidata.example.com    # your server's public address
export WS=01J9Z3K8M2N4P6Q8R0S2T4V6W8          # the workspace id (see below)
export KEY=abd_01J9Z3...                      # the API key

curl -sS -H "Authorization: Bearer $KEY" "$ABIDATA/api/v1/w/$WS"
```

```json
{"id":"01J9Z3K8M2N4P6Q8R0S2T4V6W8","name":"Shop","status":"active","role":"editor"}
```

- **Base URL**: your server's address plus `/api/v1`. Workspace data lives under `/api/v1/w/<workspace id>/`. The workspace id is in the address bar when you have the workspace open (`/w/<workspace id>`).
- **If an `Authorization` header is present, it is the only credential used.** Cookies are ignored. A wrong, expired or revoked key gets `401 UNAUTHENTICATED` with the same generic message whichever part was wrong.
- Requests that change data need **no** `Origin` header and no CSRF token. Those protect browser sessions; a key is never sent by a browser on its own. The server sends no CORS headers, so web pages on other sites cannot use a key even if they had it. That is also why you must never put a key in browser code (see [Keep keys safe](#safety)).
- Send `Content-Type: application/json` with request bodies. Responses are JSON, except downloads and the event stream.

## The OpenAPI document {#openapi}

The complete, current description of every route, parameter and response is served at:

- `/api/v1/openapi.json` (also `/api/v1/openapi.yaml`)

It needs no authentication. Load it into Postman, Insomnia, or a client generator such as `openapi-generator`. The examples below are a tour of the routes bots use most; use the document for the rest. Routes the document lists under `/api/v1/admin`, `/api/v1/auth` and for members, backups, settings and keys are not available to keys.

## Find your tables and fields {#schema}

Rows are addressed by **table id** and **field id**, not by display name, so renaming a table or field never breaks a bot. List them with the schema (viewer):

```bash
curl -sS -H "Authorization: Bearer $KEY" "$ABIDATA/api/v1/w/$WS/schema"
```

```json
{
  "schema_version": 4,
  "tables": [
    {
      "id": "01J9Z4A1...",
      "display_name": "Products",
      "columns": [
        {"id": "01J9Z4A2...", "display_name": "ID", "field_type": "autonumber", "ordinal": 0},
        {"id": "01J9Z4A3...", "display_name": "Name", "field_type": "short_text", "is_required": true, "ordinal": 1},
        {"id": "01J9Z4A4...", "display_name": "Price", "field_type": "currency", "ordinal": 2}
      ]
    }
  ],
  "relationships": []
}
```

The response carries an `ETag` of the schema version. Send it back as `If-None-Match` to get `304` when nothing changed. The examples below use these shell variables:

```bash
export T=01J9Z4A1...        # table id
export NAME=01J9Z4A3...     # id of the Name field
export PRICE=01J9Z4A4...    # id of the Price field
export H=(-H "Authorization: Bearer $KEY" -H "Content-Type: application/json")
```

### Values by field type {#values}

Send and receive each field in this JSON form. `null` clears a field.

| Field type | JSON value |
|---|---|
| Short text, long text | string |
| Whole number, lookup (the linked row's `id`) | number |
| Number | number |
| Currency | number or string with at most 4 decimals. Responses use a string such as `"19.9900"` so no precision is lost |
| Yes/No | `true` or `false` |
| Date | `"2026-03-01"` |
| Date and time | RFC 3339 such as `"2026-03-01T12:30:00Z"` (returned in UTC with milliseconds) |
| Hyperlink | string starting with `http://` or `https://` |
| Autonumber, calculated | read-only; sending them is refused |

## Read rows {#read}

List a page of rows (viewer):

```bash
curl -sS "${H[@]}" "$ABIDATA/api/v1/w/$WS/tables/$T/rows?limit=2&sort=-$PRICE"
```

```json
{
  "items": [
    {"id": 7, "_version": 3, "fields": {"01J9Z4A3...": "Gadget", "01J9Z4A4...": "24.5000"}},
    {"id": 3, "_version": 1, "fields": {"01J9Z4A3...": "Widget", "01J9Z4A4...": "19.9900"}}
  ],
  "next_offset": 2
}
```

- `limit` is 1 to 500 (default 200). Page with `offset`: while the response has a `next_offset`, request again with `offset=<next_offset>`. When it is missing you have the last page.
- `sort` is a comma-separated list of field ids (or `id`), each optionally prefixed with `-` for descending.
- `filter` is a JSON filter, URL-encoded. A condition is `{"col": <field id or "id">, "op": ..., "value": ...}`. The operators are `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `contains`, `starts`, `ends`, `in`, `between` (both take a list), `is_null` and `not_null`. Combine conditions with `{"and": [...]}` and `{"or": [...]}`.

```bash
curl -sS -G "${H[@]}" "$ABIDATA/api/v1/w/$WS/tables/$T/rows" \
  --data-urlencode "filter={\"and\":[{\"col\":\"$NAME\",\"op\":\"starts\",\"value\":\"W\"},{\"col\":\"$PRICE\",\"op\":\"lt\",\"value\":50}]}" \
  --data-urlencode "limit=100"
```

Count matching rows (capped at 100,000, with `"capped": true` when it was reached):

```bash
curl -sS -G "${H[@]}" "$ABIDATA/api/v1/w/$WS/tables/$T/rows/count" \
  --data-urlencode "filter={\"col\":\"$NAME\",\"op\":\"contains\",\"value\":\"get\"}"
```

```json
{"count":2,"capped":false}
```

Get one row. The `ETag` is the row's version, and you need it to update or delete the row:

```bash
curl -sS -i "${H[@]}" "$ABIDATA/api/v1/w/$WS/tables/$T/rows/3"
```

```
HTTP/1.1 200 OK
Etag: "1"
Content-Type: application/json

{"id":3,"_version":1,"fields":{"01J9Z4A3...":"Widget","01J9Z4A4...":"19.9900"}}
```

## Insert a row {#insert} 

Editor role or higher. Send the fields keyed by field id. Always add an `Idempotency-Key`, a unique string you choose for this attempt (a UUID works). If a network error leaves you unsure whether the request arrived, repeat it with the **same key** and body: the server returns the first result and does not insert a second row. The same key with a different body is refused with [IDEMPOTENCY_CONFLICT](/docs/reference/errors#idempotency-conflict).

```bash
curl -sS -i "${H[@]}" -X POST "$ABIDATA/api/v1/w/$WS/tables/$T/rows" \
  -H "Idempotency-Key: 5d7c1b2e-0000-4000-8000-feedc0ffee01" \
  -d "{\"fields\": {\"$NAME\": \"Sprocket\", \"$PRICE\": 4.25}}"
```

```
HTTP/1.1 201 Created
Etag: "1"

{"id":8,"_version":1,"fields":{"01J9Z4A3...":"Sprocket","01J9Z4A4...":"4.2500"}}
```

A missing required field gives `422` [REQUIRED_FIELD](/docs/reference/errors#required-field), a value of the wrong shape `400` [BAD_REQUEST](/docs/reference/errors#bad-request), and a unique-index clash `409` [DUPLICATE_VALUE](/docs/reference/errors#duplicate-value). The message names the field.

## Update a row with optimistic locking {#update}

Two programs, or a program and a person, may edit the same row. To stop one from silently overwriting the other, an update must say which version it started from, in an `If-Match` header (the `ETag` or `_version` you read). Only the fields you send change.

```bash
curl -sS -i "${H[@]}" -X PATCH "$ABIDATA/api/v1/w/$WS/tables/$T/rows/8" \
  -H 'If-Match: "1"' \
  -d "{\"fields\": {\"$PRICE\": 4.75}}"
```

```
HTTP/1.1 200 OK
Etag: "2"

{"id":8,"_version":2,"fields":{"01J9Z4A3...":"Sprocket","01J9Z4A4...":"4.7500"}}
```

If somebody changed the row since you read it, nothing is written and the response is `409`:

```json
{"error":{"code":"CONCURRENT_MODIFICATION","title":"Changed by someone else","message":"...","recovery":"...","request_id":"...","details":{"current":{"id":8,"_version":3,"fields":{"...":"..."}}}}}
```

`details.current` is the row as it is now. Re-apply your change on top of it and retry with `If-Match: "3"`. Leaving out `If-Match` is a `400`/`422` error. The same header is required to delete.

## Delete a row {#delete}

```bash
curl -sS -i "${H[@]}" -X DELETE "$ABIDATA/api/v1/w/$WS/tables/$T/rows/8" -H 'If-Match: "2"'
```

```
HTTP/1.1 204 No Content
```

Deleting a row that other rows link to fails with `409` [LINKED_RECORDS](/docs/reference/errors#linked-records). A stale `If-Match` is `409 CONCURRENT_MODIFICATION`, as for updates.

## Change many rows at once {#batch}

A batch applies up to 1,000 inserts, updates and deletes in **one transaction**: either all succeed or none do. Updates and deletes carry the `version` you read.

```bash
curl -sS "${H[@]}" -X POST "$ABIDATA/api/v1/w/$WS/tables/$T/rows/batch" \
  -H "Idempotency-Key: 5d7c1b2e-0000-4000-8000-feedc0ffee02" \
  -d "{\"ops\": [
        {\"op\": \"insert\", \"fields\": {\"$NAME\": \"Bolt\", \"$PRICE\": 0.10}},
        {\"op\": \"update\", \"id\": 3, \"version\": 1, \"fields\": {\"$PRICE\": 21}},
        {\"op\": \"delete\", \"id\": 7, \"version\": 3}
      ]}"
```

```json
{"results":[
  {"op":"insert","id":9,"row":{"id":9,"_version":1,"fields":{"...":"Bolt"}}},
  {"op":"update","id":3,"row":{"id":3,"_version":2,"fields":{"...":"Widget"}}},
  {"op":"delete","id":7}
]}
```

If any operation fails, the response is the error for that operation and `details.op_index` tells you which one (counting from 0). Nothing was applied, so fix it and send the whole batch again.

## Run a saved query {#queries}

Saved queries are read with the viewer role. List them to find the id, then run one. `params` fills the query's parameters by name and `limit` caps the rows (up to 50,000).

```bash
curl -sS "${H[@]}" "$ABIDATA/api/v1/w/$WS/queries"          # items[].id and items[].name

curl -sS "${H[@]}" -X POST "$ABIDATA/api/v1/w/$WS/queries/$QUERY/run" \
  -d '{"params": {"min_price": 10}, "limit": 1000}'
```

```json
{"columns":["Name","Price"],"rows":[["Widget","21.0000"],["Gadget","24.5000"]],"truncated":false}
```

`truncated` is `true` when the row limit cut the result short. Reports run the same way at `POST /api/v1/w/$WS/reports/<id>/run`; see the OpenAPI document.

## Start a job {#jobs}

Starting a job needs the **builder** role. The job and its script are designed in the web app first; see [Jobs](/docs/guide/jobs).

```bash
curl -sS "${H[@]}" "$ABIDATA/api/v1/w/$WS/jobs"             # items[].id

curl -sS -i "${H[@]}" -X POST "$ABIDATA/api/v1/w/$WS/jobs/$JOB/run" -d '{"args": {"region": "north"}}'
```

```
HTTP/1.1 202 Accepted

{"id":"01J9Z5...","job_id":"01J9Z4...","trigger_kind":"manual","status":"queued","attempt":1,"triggered_by":"apikey:01J9Z3...","queued_at":"2026-03-01T12:30:00Z","rows_read":0,"rows_written":0}
```

`args` are merged over the job's own arguments for this run only. The run is recorded as triggered by the key. Poll `GET /api/v1/w/$WS/runs/<run id>` until `status` is `succeeded`, `failed` or `cancelled`; the response includes the log. A job that is already running and set to skip overlapping runs is refused with `409`.

## Watch for changes (events) {#events}

`GET /api/v1/w/<workspace id>/events` is a Server-Sent Events stream of changes, so a bot can react instead of polling. Any role can subscribe.

```bash
curl -sS -N -H "Authorization: Bearer $KEY" "$ABIDATA/api/v1/w/$WS/events"
```

```
id: 41
event: hello
data: {}

id: 42
event: rows.changed
data: {"table_id":"01J9Z4A1...","op":"insert","ids":[8],"versions":[1]}
```

- `rows.changed` carries the table, the operation and the changed row ids and versions. Fetch the rows if you need their values.
- `table.invalidated`, `schema.changed` and `design.changed` mean you should reload tables, the schema, or a saved object. `job.run` (builders only) reports job progress. `reset` means you missed too many events: reload everything.
- Every event has an `id`. After a dropped connection, reconnect with the header `Last-Event-ID: <last id>` and the server replays what you missed.
- The server checks the key again about every 30 seconds. A revoked or expired key ends the stream, and the next connection attempt gets `401`. Reconnect logic should stop on `401`, `403` and `404` instead of retrying.
- Heartbeat comment lines (`: ping`) arrive every 15 seconds.

## Errors {#errors}

Every error uses the same envelope, with an HTTP status that matches the code:

```json
{"error":{"code":"VALIDATION_FAILED","title":"...","message":"...","recovery":"...","request_id":"01J9Z6...","details":{}}}
```

Branch on `error.code`, which is stable; show `message` and `recovery` to people. Quote `request_id` (also sent as the `X-Request-ID` header) when you ask an administrator to look something up. The full list is in the [error reference](/docs/reference/errors). The ones a bot meets most:

| Status | Code | Meaning |
|---|---|---|
| 401 | [UNAUTHENTICATED](/docs/reference/errors#unauthenticated) | The key is missing, malformed, wrong, revoked or expired |
| 403 | [FORBIDDEN](/docs/reference/errors#forbidden) | The key's role is too low for this route, the route is not available to keys, or the workspace is suspended |
| 404 | [NOT_FOUND](/docs/reference/errors#not-found) | No such object, or the key belongs to another workspace |
| 409 | [CONCURRENT_MODIFICATION](/docs/reference/errors#concurrent-modification) | The row changed since you read it: use `details.current` and retry |
| 409 | [IDEMPOTENCY_CONFLICT](/docs/reference/errors#idempotency-conflict) | The `Idempotency-Key` was used with a different request |
| 422 | [REQUIRED_FIELD](/docs/reference/errors#required-field), [VALIDATION_FAILED](/docs/reference/errors#validation-failed) | A value was refused; the message names the field |
| 429 | [RATE_LIMITED](/docs/reference/errors#rate-limited) | Too many requests: wait for `Retry-After` seconds |
| 503 | [WORKSPACE_MAINTENANCE](/docs/reference/errors#workspace-maintenance), [WORKSPACE_BUSY](/docs/reference/errors#workspace-busy) | A restore is running or the workspace is busy: retry after `Retry-After` |

Retry `429` and `503` after the `Retry-After` delay, with growing waits. Do not retry other `4xx` errors unchanged.

## Rate limits {#limits}

- Each key may send **600 requests a minute**, as a steady 10 per second with bursts of up to 100. Beyond that, requests get `429 RATE_LIMITED` with a `Retry-After` header. Other keys are not affected. Page through large tables with `limit=500` and use [batches](#batch) for bulk writes instead of many single requests.
- A client address that sends **more than 30 requests with bad keys in a minute** is refused for the rest of that minute, even with a correct key. This slows down anyone guessing keys. A bot with a revoked or mistyped key should stop, not loop.
- The workspace's own limits (storage quota, busy-workspace protection) still apply to key requests.

## Keep keys safe {#safety}

- **Give each program its own key** with a clear name, so you can revoke one without breaking the others and the history shows who changed what.
- **Use the lowest role.** A viewer key cannot damage anything, even if it leaks.
- **Set an expiry** and put a reminder in your calendar to rotate. To rotate: create the new key, deploy it, check that the old key's **Last used** stops changing, then revoke the old one.
- **Store keys in a secret manager or environment variable**, never in source control, chat messages, tickets or logs.
- **Never use a key in browser code or a mobile app.** Anyone who opens the page can read it. Call the API from a server you control, and let browsers talk to that server.
- **Revoke at once** if a key may have leaked. Check **Last used** and the record **History** to see what it did.
- Use HTTPS only. Keys sent over plain `http` can be read by anyone on the network.
- Keys are not tied to an address or network. If you need that, restrict access at your reverse proxy.

For how keys are protected on the server, see [Security](/docs/operations/security#api-keys).
