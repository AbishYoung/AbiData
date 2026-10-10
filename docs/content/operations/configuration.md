---
title: Configuration
order: 20
summary: Every ABIDATA_* environment variable, its default and how it is validated.
---

# Configuration

AbiData is configured with environment variables. The server reads them once at start, so restart it after a change.

## Secrets from files {#file-variants}

Every variable also accepts a `_FILE` variant, such as `ABIDATA_SESSION_KEY_FILE`. Its value is the path of a file, and the server uses the file's content with surrounding whitespace removed. Use this with Docker or Compose secrets so keys do not appear in `docker inspect` or the process list.

- Setting both `ABIDATA_NAME` and `ABIDATA_NAME_FILE` is an error.
- A `_FILE` that cannot be read is an error that names the variable but never prints the content.
- An empty value counts as set, and is rejected by variables that must not be empty.

An invalid value stops the server at start with a message naming the variable.

## Variables {#variables}

| Variable | Default | Meaning |
|---|---|---|
| `ABIDATA_LISTEN_ADDR` | `:8080` | Address and port the HTTP server listens on. Must not be empty |
| `ABIDATA_PUBLIC_URL` | `http://localhost:8080` | The address people use to reach AbiData: scheme and host, optionally a port. No path, query, fragment or credentials. See below |
| `ABIDATA_DATA_DIR` | `/var/lib/abidata` | The data volume: admin database, keys, workspaces, staging, artifacts |
| `ABIDATA_BACKUP_DIR` | `/var/lib/abidata-backups` | The backups volume. Keep it on different storage from the data directory |
| `ABIDATA_MAX_MEMORY_MB` | `0` (not set) | A soft memory limit for the process in MiB (0 or more). When set, the Go runtime works harder to stay under it, and the script memory watchdog stops runaway scripts at 75% of it |
| `ABIDATA_JOB_WORKERS` | Number of CPUs | How many jobs can run at the same time across all workspaces (1 or more) |
| `ABIDATA_MAX_OPEN_WORKSPACES` | `128` | How many workspace databases are kept open (1 or more). Idle ones are closed after 10 minutes, and the least recently used are closed when the limit is reached |
| `ABIDATA_SYNCHRONOUS` | `normal` | SQLite durability: `normal` or `full`. See below |
| `ABIDATA_SESSION_KEY` | Generated on first start | Key used to protect session identifiers: base64, at least 32 bytes once decoded |
| `ABIDATA_MANIFEST_KEY` | Generated on first start | Key that signs backup manifests: same format. See [Backup strategy](/docs/operations/backup-strategy#manifest-key) |
| `ABIDATA_BOOTSTRAP_ADMIN_EMAIL` | (none) | Email for the first platform administrator. While no user exists, the server prints a one-time setup token. See [Install](/docs/operations/install#first-run) |
| `ABIDATA_DEBUG_ADDR` | (empty: off) | Address for the Go profiler (`/debug/pprof/`), such as `127.0.0.1:6060`. See below |
| `ABIDATA_METRICS_ADDR` | (empty: off) | Extra listener serving `/metrics` without authentication, for Prometheus. Bind it to a private address. See [Monitoring](/docs/operations/monitoring#metrics) |
| `ABIDATA_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `ABIDATA_TRUSTED_PROXIES` | (none) | Comma-separated IP addresses or CIDR ranges of reverse proxies whose `X-Forwarded-For` header is trusted |

## Notes {#notes}

### ABIDATA_PUBLIC_URL {#public-url}

AbiData compares the `Origin` header of every request that changes data with this value, and refuses requests from any other origin. If it does not match the address in the browser, sign-in and saving fail with [FORBIDDEN](/docs/reference/errors#forbidden). Use `https://` in production: the session cookie is marked `Secure` (and uses the `__Host-` prefix) only when the URL starts with `https://`.

### Keys {#keys}

Keys are base64 text, standard or URL-safe, with or without padding, that decode to at least 32 bytes. Generate one with:

```sh
openssl rand -base64 32
```

If you do not supply a key, the server creates one on first start in `system/keys/` inside the data directory, with mode 600 (it refuses to start if the file is readable by others). A key you supply always wins over a file.

Changing the session key signs everyone out. Changing or losing the manifest key makes existing backups fail verification: see [Backup strategy](/docs/operations/backup-strategy#manifest-key).

### ABIDATA_SYNCHRONOUS {#synchronous}

`normal` is the default and is safe against application crashes. After an operating-system crash or power loss it can lose the last few committed transactions, but it cannot corrupt the database. `full` waits for the disk on every commit, which prevents that loss and makes writes slower. Choose `full` if you cannot afford to lose recent data and your disks are fast.

### ABIDATA_MAX_MEMORY_MB {#memory}

Set this to about 75 to 80 percent of the memory you give the container. Scripts that allocate too much are stopped with [SCRIPT_LIMIT_EXCEEDED](/docs/reference/errors#script-limit-exceeded) before the process runs out of memory.

### ABIDATA_DEBUG_ADDR {#debug}

The profiler has no authentication. Leave it off, or bind it to `127.0.0.1` or a private network that only operators can reach. Never publish it.

### ABIDATA_TRUSTED_PROXIES {#trusted-proxies}

Without it, the server uses the connection's address as the client address, which behind a proxy is the proxy itself. With it, the server reads the client address from `X-Forwarded-For`, but only when the connection comes from a listed proxy. The client address is used in logs and for sign-in rate limiting. List only proxies you control.

## Limits that are not configurable yet {#fixed}

These are fixed in this version:

- Sign-in rate limit: 5 failed attempts per 15 minutes per account and address.
- Sessions: 12 hours idle, 7 days absolute.
- Import upload size: 100 MiB.
- Script time budgets and string limits: see [Scripts](/docs/guide/scripts#contexts).
- Backup retention: see [Backup strategy](/docs/operations/backup-strategy#retention).
