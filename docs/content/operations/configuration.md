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
| `ABIDATA_MAX_FILE_BYTES` | `26214400` (25 MiB) | The largest single file attachment, in bytes (1 to 1073741824). Uploads over it are refused. See below |
| `ABIDATA_SYNCHRONOUS` | `normal` | SQLite durability: `normal` or `full`. See below |
| `ABIDATA_SESSION_KEY` | Generated on first start | Key used to protect session identifiers: base64, at least 32 bytes once decoded |
| `ABIDATA_MANIFEST_KEY` | Generated on first start | Key that signs backup manifests: same format. See [Backup strategy](/docs/operations/backup-strategy#manifest-key) |
| `ABIDATA_BOOTSTRAP_ADMIN_EMAIL` | (none) | Email for the first platform administrator. While no user exists, the server prints a one-time setup token. See [Install](/docs/operations/install#first-run) |
| `ABIDATA_DEMO_WORKSPACE` | `true` | When the first administrator is created through the setup page, also create the **Northwind Traders** and **Northwind Invasives** sample workspaces for them. `false` or `0` turns it off. See [Install](/docs/operations/install#demo) |
| `ABIDATA_PUBLIC_APPS` | `true` | Whether workspace owners can publish [public apps](/docs/guide/apps). `false` or `0` turns them off for the whole install, and the administrator setting cannot turn them back on. |
| `ABIDATA_PUBLIC_BLOGS` | `true` | Whether [user blogs](/docs/guide/blogs) can be served at `/b/{username}`. Blogs are off until an administrator turns them on; `false` or `0` turns them off for good, and the administrator setting cannot turn them back on. |
| `ABIDATA_BLOG_HOST` | unset | A separate host name for blogs, such as `blogs.example.com` (host and optional port, no scheme), pointed at the same server. When set, that host serves only `/b/...` and the public blog API, and blog paths on the main host redirect there. It must differ from the host of `ABIDATA_PUBLIC_URL`. See [below](#blogs) |
| `ABIDATA_DEBUG_ADDR` | (empty: off) | Address for the Go profiler (`/debug/pprof/`), such as `127.0.0.1:6060`. See below |
| `ABIDATA_METRICS_ADDR` | (empty: off) | Extra listener serving `/metrics` without authentication, for Prometheus. Bind it to a private address. See [Monitoring](/docs/operations/monitoring#metrics) |
| `ABIDATA_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `ABIDATA_ALLOWED_ORIGINS` | (none) | Comma-separated extra origins (`scheme://host[:port]`) allowed to change data, besides the public URL. See [below](#public-url) |
| `ABIDATA_TRUSTED_PROXIES` | (none) | Comma-separated IP addresses or CIDR ranges of reverse proxies whose `X-Forwarded-For` header is trusted |
| `ABIDATA_DB` | `sqlite` | The database backend: `sqlite` or `postgres`. PostgreSQL is not available yet. See [below](#database) |
| `ABIDATA_POSTGRES_URL` | (none) | PostgreSQL connection string, required with `ABIDATA_DB=postgres`. It holds a password, so prefer `ABIDATA_POSTGRES_URL_FILE` |
| `ABIDATA_POSTGRES_MAX_CONNS` | `20` | The most connections the server opens to PostgreSQL (1 or more) |
| `ABIDATA_POSTGRES_SCHEMA_PREFIX` | `abd` | Prefix of every schema and role the server creates in the database: a lower-case letter, then up to 15 lower-case letters, digits or underscores |

## Notes {#notes}

### ABIDATA_PUBLIC_URL {#public-url}

AbiData compares the `Origin` header of every request that changes data with this value, and refuses requests from any other origin. If it does not match the address in the browser, sign-in and saving fail with [FORBIDDEN](/docs/reference/errors#forbidden). Use `https://` in production: the session cookie is marked `Secure` (and uses the `__Host-` prefix) only when the URL starts with `https://`.

When the public URL is a loopback address (`localhost`, `127.0.0.1` or `[::1]`), the other loopback spellings on the same port are accepted too, because they are the same machine and a browser sends whichever one is in its address bar. A different port is a different origin and is still refused.

To reach a server by a second address, for example a LAN address while testing on a phone, list it in `ABIDATA_ALLOWED_ORIGINS`, such as `http://192.168.1.20:8080`. Each entry must match exactly: no wildcards and no paths.

### Keys {#keys}

Keys are base64 text, standard or URL-safe, with or without padding, that decode to at least 32 bytes. Generate one with:

```sh
openssl rand -base64 32
```

If you do not supply a key, the server creates one on first start in `system/keys/` inside the data directory, with mode 600 (it refuses to start if the file is readable by others). A key you supply always wins over a file.

Changing the session key signs everyone out and also makes every [API key](/docs/guide/api) stop working (they are hashed under it); owners must create new keys. Changing or losing the manifest key makes existing backups fail verification: see [Backup strategy](/docs/operations/backup-strategy#manifest-key).

### ABIDATA_MAX_FILE_BYTES {#max-file-bytes}

Attached files are stored in `workspaces/<workspace id>.files/` inside `ABIDATA_DATA_DIR`, one file per distinct content. This setting limits one file; a reverse proxy in front of AbiData must allow request bodies at least this large (for Caddy, `request_body max_size`). The total per workspace is limited by the **file storage** quota (`max_file_bytes` in the workspace quota, counting distinct bytes; default 5 GiB), which an administrator can change in the admin console. Expired files are removed by a background task every few minutes: files never attached after 24 hours, and files no row refers to after 7 days.

### ABIDATA_SYNCHRONOUS {#synchronous}

`normal` is the default and is safe against application crashes. After an operating-system crash or power loss it can lose the last few committed transactions, but it cannot corrupt the database. `full` waits for the disk on every commit, which prevents that loss and makes writes slower. Choose `full` if you cannot afford to lose recent data and your disks are fast.

### ABIDATA_MAX_MEMORY_MB {#memory}

Set this to about 75 to 80 percent of the memory you give the container. Scripts that allocate too much are stopped with [SCRIPT_LIMIT_EXCEEDED](/docs/reference/errors#script-limit-exceeded) before the process runs out of memory.

### ABIDATA_DEBUG_ADDR {#debug}

The profiler has no authentication. Leave it off, or bind it to `127.0.0.1` or a private network that only operators can reach. Never publish it.

### ABIDATA_DB {#database}

AbiData stores everything in SQLite files under the data directory. Running on PostgreSQL instead is in progress (ADR 0012). For now, `ABIDATA_DB=postgres` only checks the server and stops:

- It connects with `ABIDATA_POSTGRES_URL` and checks three things: the server is PostgreSQL 17 or later; the role owns the database; and the role has `CREATEROLE` but is not a superuser.
- It then stops with a message saying workspaces cannot run on PostgreSQL yet. It creates nothing.

An install never mixes backends. The server refuses to start in these cases:

- `ABIDATA_DB=postgres` with a data directory that already holds a SQLite install;
- `ABIDATA_POSTGRES_URL` set while `ABIDATA_DB` is `sqlite`.

The admin database also records which backend created it, and refuses to open under the other one.

### ABIDATA_TRUSTED_PROXIES {#trusted-proxies}

Without it, the server uses the connection's address as the client address, which behind a proxy is the proxy itself. With it, the server reads the client address from `X-Forwarded-For`, but only when the connection comes from a listed proxy. The client address is used in logs and for sign-in rate limiting. List only proxies you control.

### User blogs and ABIDATA_BLOG_HOST {#blogs}

[User blogs](/docs/guide/blogs) are served at `/b/{username}` and are off by default on a fresh server until an administrator enables them in the admin console's **Blogs** tab.

When you enable public blogs in production, we recommend setting `ABIDATA_BLOG_HOST` to a separate host name (for example `blogs.example.com`) pointed at the same reverse proxy and server:

- The blog host serves only `/b/...` and `/api/v1/blogs/...`, and requests to `/b/...` on `ABIDATA_PUBLIC_URL` redirect (`301`) to `ABIDATA_BLOG_HOST`.
- Because `ABIDATA_BLOG_HOST` is a separate browser origin from `ABIDATA_PUBLIC_URL`, session cookies for the main application are never sent to public blog pages.

Published blog pages and Atom feeds send `Cache-Control: public, max-age=60, stale-while-revalidate=300` and a weak `ETag` derived from the post version. Missing, unpublished or taken-down pages send `Cache-Control: no-store`. Taking a blog or post down purges AbiData's in-memory page cache at once; if you put a caching reverse proxy or CDN in front of `/b/`, configure it to honour `Cache-Control` and purge `/b/{username}*` from the proxy or CDN cache when taking content down.

## Limits that are not configurable yet {#fixed}

These are fixed in this version:

- Sign-in rate limits, each over 15 minutes: 5 failed attempts per account and address; 20 per account from any address; 300 per address across all accounts. While a limit is reached, sign-in is refused even with the correct password, so someone who keeps guessing an account's password can keep that account (including an administrator's) out for as long as they continue. Signed-in sessions are not affected; an administrator who is locked out can use an existing session, or wait for the window to pass.
- Sessions: 12 hours idle, 7 days absolute.
- Import upload size: 100 MiB.
- Script time budgets and string limits: see [Scripts](/docs/guide/scripts#contexts).
- Backup retention: see [Backup strategy](/docs/operations/backup-strategy#retention).

## Settings that live in the admin console {#console-settings}

Some operator settings are not environment variables because administrators change them while the server runs. The outbound-request allowlist is one of them: no environment variable lets jobs reach the internet, and a fresh install allows nothing. Add hosts under **Settings**, **Outbound requests** in the admin console. See [Security](/docs/operations/security#egress).

### Maps {#maps}

Maps for [location and area fields](/docs/guide/tables#geo) draw a plain latitude/longitude grid by default. They then load nothing from outside the server and work offline. To show a street or satellite map underneath, open **Settings**, **Maps** in the admin console and set:

- **Tile URL template**: an XYZ tile URL containing `{z}`, `{x}` and `{y}`, for example `https://tile.openstreetmap.org/{z}/{x}/{y}.png`. `{s}` may be the first part of the host name for `a`, `b`, `c` subdomains (`{r}` is accepted and left empty). It must use `https://`; `http://` is accepted only for `localhost`.
- **Attribution**: the credit the provider requires, shown on every map.

While a tile server is set, the server's `Content-Security-Policy` allows images from that one origin (`img-src`); clearing the URL removes it again. People see the change after they reload the page.

With tiles on, every user's browser asks the tile server for the images of the map areas they view. The provider therefore learns roughly where your users are looking, and their IP addresses. For sensitive locations, such as protected species, use a self-hosted tile server or leave tiles off. Check the provider's usage policy too: the public OpenStreetMap servers, for example, are not meant for heavy use.
