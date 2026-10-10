---
title: Monitoring
order: 60
summary: Health checks, Prometheus metrics, JSON logs and the administrator's live log stream.
---

# Monitoring

## Health checks {#health}

| Endpoint | Meaning | Use for |
|---|---|---|
| `GET /healthz` | The process is up. Always answers `ok` | Liveness. The container's `HEALTHCHECK` runs `abidata healthcheck`, which calls this on the local port |
| `GET /readyz` | The server can take traffic: the admin database answers and the data directory is writable. Answers `ready`, or 503 with a reason (`draining`, `admin db unavailable`, `data dir not writable`) | Readiness, load balancers, deploy checks |

`/readyz` answers 503 `draining` as soon as a shutdown starts, so a load balancer stops sending new requests while running requests finish. Neither endpoint needs a session.

## Metrics {#metrics}

`GET /metrics` serves Prometheus text format. On the main port it requires a signed-in **platform administrator** session cookie, so you can check it by hand from a browser where you are signed in as an administrator.

For Prometheus, set `ABIDATA_METRICS_ADDR` (for example `10.0.0.5:9100`) to also serve `/metrics` on a separate listener. That listener has **no authentication**: bind it to a private interface or container network that only your scraper can reach, never to a public address. See [Configuration](/docs/operations/configuration).

All AbiData metrics start with `abidata_`. The Go runtime and process collectors are included too.

| Metric | Type | Labels | Tells you |
|---|---|---|---|
| `abidata_http_request_duration_seconds` | histogram | `method`, `route`, `status` | Latency and error rate per route |
| `abidata_open_workspaces` | gauge | | Workspace databases currently open (limit: `ABIDATA_MAX_OPEN_WORKSPACES`) |
| `abidata_writer_gate_wait_seconds` | histogram | `priority` | How long writes wait for their turn. Rising values mean write contention |
| `abidata_writer_gate_hold_seconds` | histogram | | How long writes hold the turn |
| `abidata_reader_pool_wait_seconds` | histogram | | How long reads wait for a connection |
| `abidata_script_runs_total` | counter | `context`, `outcome` | Script runs by context (`ui_event`, `report`, `job`) and result |
| `abidata_script_duration_seconds` | histogram | `context` | Script run time |
| `abidata_job_runs_total` | counter | `status` | Job runs by final status |
| `abidata_egress_requests_total` | counter | `outcome` | Outbound requests from jobs |
| `abidata_backup_duration_seconds` | histogram | | Time to make a backup |
| `abidata_backup_size_bytes` | histogram | | Compressed size of backups |
| `abidata_wal_size_bytes` | gauge | `workspace` | Size of each open workspace's write-ahead log |

Useful alerts:

- A rising `abidata_wal_size_bytes` for a workspace: a long-running read may be stopping SQLite from checkpointing.
- A growing `abidata_writer_gate_wait_seconds` p95: writes are queueing.
- `abidata_job_runs_total{status="failed"}` or `timed_out` increasing.
- No successful `abidata_backup_duration_seconds` observations in a day.
- HTTP 5xx in `abidata_http_request_duration_seconds`.

## Logs {#logs}

The server writes one JSON object per line to standard output. Read them with `docker compose logs abidata`, or collect them with your logging system. Every record has a time, level and message, and request records add some of these fields:

| Field | Meaning |
|---|---|
| `subsystem` | The part of the server, such as `http`, `auth`, `workspace`, `jobs`, `egress`, `backup` or `janitor` |
| `request_id` | The ID shown in error dialogs as the request ID. Search for it to find the cause of an error |
| `workspace_id` | The workspace involved |
| `user_id` | The user involved |

Set `ABIDATA_LOG_LEVEL` to `debug`, `info`, `warn` or `error`. The default is `info`. At the default level the log shows the start-up setup token while no user exists, so treat the log as sensitive until the first administrator has been created.

## The live log stream {#live-logs}

Platform administrators can follow the log in the browser. The server keeps the most recent 5,000 records at `info` level and above in memory and streams them as server-sent events at `GET /api/v1/admin/logs/stream`. You can filter by `level` (`info`, `warn`, `error`), `subsystem` and `workspace`.

If the connection drops, the client resumes from where it left off with no gaps. If it was away too long for the 5,000 records to cover, the stream sends a `reset` and starts again from the newest records. Only platform administrators can open it: others get [FORBIDDEN](/docs/reference/errors#forbidden).

The in-memory stream is for live viewing only. It is lost on restart, so keep the standard output log for history.

## Profiling {#pprof}

Set `ABIDATA_DEBUG_ADDR` to start the Go profiler on a separate address, for example `127.0.0.1:6060`. It has no authentication, so it is off by default. See [Configuration](/docs/operations/configuration#debug).

## Routine tasks the server does itself {#janitor}

Every 5 minutes a janitor:

- closes workspaces that have been idle for 10 minutes;
- removes import staging files older than 24 hours and job artifacts older than 30 days;
- removes expired sessions and old idempotency keys;
- closes finished workspaces cleanly, which checkpoints their write-ahead log.

Failures are logged with `subsystem=janitor`.
