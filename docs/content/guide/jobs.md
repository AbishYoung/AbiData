---
title: Jobs
order: 60
summary: Run scripts on a schedule, handle overlap, misfires and retries, read logs, and allow outbound requests.
---

# Jobs

A job runs a job script on a schedule or on demand. Builders and owners create jobs. A scheduled run executes as the job principal, which has editor rights to data plus the job-only functions (`fetch`, `staging`, `artifacts`).

## Create a job {#create}

1. Create a script of kind **Job** in Script Studio. See [Scripts](/docs/guide/scripts).
2. In the **Jobs** group, choose **New job**.
3. Enter a **Job name**, choose the **Job script**, and set the schedule and options described below. Save with `Mod+S`.
4. Choose **Run now** to try it. **Run with arguments…** lets you supply `job.args` for one run.

Settings:

| Setting | Meaning | Default |
|---|---|---|
| Repeat | **Manual only**, **Every hour**, **Every day at…**, **Weekdays at…** or **Custom cron** | Manual only |
| Time zone | IANA name such as `Europe/Paris`. **Workspace default** uses the workspace time zone | Workspace default |
| Enabled | A disabled job never starts on its schedule. **Run now** still works | On |
| Timeout (seconds) | 1 to 3,600 | 300 |
| If the previous run is still going | **Skip the new run** or **Queue the new run**. See [Overlap](#overlap) | Skip the new run |
| After downtime | **Run once for missed times** or **Skip missed times**. See [Misfire](#misfire) | Run once for missed times |
| Retries | 0 to 10 | 0 |
| Retry delay (seconds) | 0 to 86,400. See [Retries](#retries) | 60 |
| Arguments (JSON) | A JSON object, available as `job.args` | `{}` |

## Schedules and time zones {#schedules}

Choose **Custom cron** to enter a schedule by hand. It takes five cron fields (minute, hour, day of month, month, day of week) or a descriptor such as `@hourly`, `@daily` or `@every 15m`. `@every` must be at least one minute. Do not put `TZ=` in the expression: set the job's time zone instead.

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

Overlap is set by **If the previous run is still going**:

- **Skip the new run**: if the previous run is still going when the next one is due, the new run is not started.
- **Queue the new run**: the new run waits and starts as soon as the previous one finishes. At most one run waits.

Each workspace runs at most two jobs at once by default, and the server runs at most `ABIDATA_JOB_WORKERS` jobs in total. See [Configuration](/docs/operations/configuration).

## Misfire {#misfire}

Misfire is set by **After downtime**. It decides what happens when the server was down when a run was due:

- **Run once for missed times**: the job runs once after startup, however many runs were missed.
- **Skip missed times**: the missed runs are ignored and the job waits for the next scheduled time.

A run that was in progress when the server stopped is marked **Interrupted**. It is not restarted.

## Retries {#retries}

When a run ends as failed or timed out, and the number of attempts is not above **Retries**, the job schedules another run as a new run with the trigger **Retry**. The wait is the **Retry delay** multiplied by the attempt number: with a delay of 60 seconds the retries start after 60, 120 and 180 seconds. Cancelled and interrupted runs are not retried.

## Run history and logs {#history}

Open a job to see its runs in **Run history**, with live status: **Queued**, **Running**, **Succeeded**, **Failed**, **Timed out**, **Cancelled** or **Interrupted**. The columns show the status, trigger (**Schedule**, **Manual**, **Enqueued** or **Retry**), attempt, when the run was queued and started, duration and any error. Select a run to see its **Run detail**: the facts, the error if any, its artifacts and the log.

- The log holds the output of `log.*` and `print`. It is capped at 256 KiB, and when it is longer the end is kept.
- Use the search box in the log viewer to find text, and **Next match** and **Previous match** to move between hits.
- **Cancel run** stops a queued or running run.

## Artifacts {#artifacts}

A job can produce files with `artifacts.save(name, content, { mime = "text/csv" })`. They appear under **Artifacts** on the run, where builders and owners can download them.

- A run can save up to 100 files.
- Files count toward the workspace's artifact quota (1 GiB by default). Over the quota, saving fails with [QUOTA_EXCEEDED](/docs/reference/errors#quota-exceeded).
- Artifacts are deleted 30 days after they are created.

## Outbound requests and the egress allowlist {#egress}

Only jobs can call `fetch`. A request is allowed only when the **platform allowlist** includes the host and port. The allowlist is set by platform administrators in the **Admin console**, on the **Settings** tab, under **Outbound requests (platform egress allowlist)**. It applies to every workspace, and owners and builders cannot change it. If a job needs a new host, ask a platform administrator to add it. A fresh install allows nothing.

Owners can see which hosts are currently allowed under **Workspace settings**, in the **Outbound requests** section.

Entries follow these rules:

- A pattern is an exact host (`api.example.com`) or `*.example.com` for any subdomain.
- Ports default to 443. Other ports are allowed only when listed. The list holds at most 200 entries.
- Only `https` is used, unless the administrator allows `http`, in which case the port (for example 80) must also be listed.

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
