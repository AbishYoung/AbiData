---
title: Jobs
order: 60
summary: Run scripts on a schedule, handle overlap, misfires and retries, read logs, and allow outbound requests.
---

# Jobs

A job runs a job script on a schedule or on demand. Builders and owners create jobs; the schedule runs them as the job principal, which has editor rights to data plus the job-only functions (`fetch`, `staging`, `artifacts`).

## Create a job {#create}

1. Create a script of kind **Job** in Script Studio. See [Scripts](/docs/guide/scripts).
2. In the **Jobs** group, choose **New job**.
3. Pick the script, a schedule and a time zone, then set the options below.
4. Choose **Run now** to try it. **Run with arguments…** lets you supply `job.args` for one run.

Settings:

| Setting | Meaning | Default |
|---|---|---|
| Schedule | **Manual only**, **Every hour**, **Every day at…**, **Weekdays at…** or **Custom cron** | Manual only |
| Time zone | IANA name such as `Europe/Paris`. Defaults to the workspace time zone | Workspace |
| Enabled | A disabled job never starts on its schedule. **Run now** still works | On |
| Timeout | 1 to 3,600 seconds | 300 |
| Overlap | What to do when a run is due while the previous one is still running | Skip |
| Misfire | What to do after downtime | Run once |
| Retries | 0 to 10 | 0 |
| Retry backoff | 0 to 86,400 seconds | 60 |
| Arguments | JSON object, available as `job.args` | `{}` |

## Schedules and time zones {#schedules}

Custom schedules use five cron fields (minute, hour, day of month, month, day of week) or a descriptor such as `@hourly`, `@daily` or `@every 15m`. `@every` must be at least one minute. Do not put `TZ=` in the expression: set the job's time zone instead.

```text
30 2 * * *      every day at 02:30
0 9 * * 1-5     weekdays at 09:00
*/15 * * * *    every 15 minutes
```

The schedule is evaluated in the job's time zone, so clock changes matter:

- **Spring forward.** A local time that does not exist that day is skipped. A job set for 02:30 does not run on the night the clock jumps from 02:00 to 03:00.
- **Fall back.** A local time that happens twice runs twice, once in each offset. A job set for 01:30 runs at both 01:30s.

If a job must run exactly once a day, schedule it outside the 01:00 to 03:00 window of your time zone, or use `UTC`.

## Overlap {#overlap}

- **Skip**: if the previous run is still going when the next one is due, the new run is not started and nothing is recorded.
- **Queue**: the new run waits and starts when the previous one finishes. At most one run waits.

Each workspace runs at most two jobs at once by default, and the server runs at most `ABIDATA_JOB_WORKERS` jobs in total. See [Configuration](/docs/operations/configuration).

## Misfire {#misfire}

If the server was down when a run was due:

- **Run once for missed times**: the job runs once after startup, however many runs were missed.
- **Skip**: the missed runs are ignored and the job waits for the next scheduled time.

A run that was in progress when the server stopped is marked `interrupted`. It is not restarted.

## Retries {#retries}

When a run ends as `failed` or `timed_out`, and the number of attempts is not above **Retries**, the job schedules another run as a new run with the trigger `retry`. The wait is the backoff multiplied by the attempt number: with a backoff of 60 seconds the retries start after 60, 120 and 180 seconds. Cancelled and interrupted runs are not retried.

## Run history and logs {#history}

Open a job to see its runs with live status: `queued`, `running`, `succeeded`, `failed`, `timed_out`, `cancelled` or `interrupted`. Select a run to see its trigger (`schedule`, `manual`, `enqueue` or `retry`), attempt, times, rows read and written, the error if any, and the log.

- The log holds the output of `log.*` and `print`. It is capped at 256 KiB, and when it is longer the end is kept.
- Use the search box in the log viewer to find text, and **Next match** and **Previous match** to move between hits.
- **Cancel** stops a queued or running run.

## Artifacts {#artifacts}

A job can produce files with `artifacts.save(name, content, { mime = "text/csv" })`. They appear under **Artifacts** on the run, where builders can download them.

- A run can save up to 100 files.
- Files count toward the workspace's artifact quota (1 GiB by default). Over the quota, saving fails with [QUOTA_EXCEEDED](/docs/reference/errors#quota-exceeded).
- Artifacts are deleted 30 days after they are created.

## Outbound requests and the egress allowlist {#egress}

Only jobs can call `fetch`. A request is allowed only when both the platform policy and the workspace allowlist allow it.

To allow a host, an owner opens **Workspace settings** and adds it to the **Egress allowlist**:

- A pattern is an exact host (`api.example.com`) or `*.example.com` for any subdomain.
- Ports default to 443. List other ports explicitly. The workspace list holds at most 200 entries.
- Only `https` is used, unless the administrator allows `http`, in which case you must also list port 80.

Whatever the allowlist says, these are always refused: private, loopback, link-local and other non-public addresses, including after a redirect or a DNS change. At most 5 redirects are followed.

Other limits:

| Limit | Value |
|---|---|
| Timeout | 10 s default; `timeout_ms` up to 30,000 |
| Response size | 2 MiB default; `max_bytes` up to 10 MiB |
| Request body | 10 MiB |
| Rate | 60 requests per minute per workspace |

Errors you may see: [EGRESS_DENIED](/docs/reference/errors#egress-denied) (host not allowed), [EGRESS_FAILED](/docs/reference/errors#egress-failed) (the request did not complete), [EGRESS_RESPONSE_TOO_LARGE](/docs/reference/errors#egress-response-too-large) and [RATE_LIMITED](/docs/reference/errors#rate-limited). See [`fetch`](/docs/reference/lua-api#fetch) in the API reference.

## Start a job from a form {#enqueue}

A form handler can call `jobs.enqueue("Job name", { ... })`. The user must be an editor or above, and the job's arguments must contain `"allow_enqueue": true`. Otherwise the call fails with [FORBIDDEN](/docs/reference/errors#forbidden). Disabled jobs are never started this way.
