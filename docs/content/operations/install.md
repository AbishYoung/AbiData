---
title: Install
order: 10
summary: Run AbiData with Docker Compose and Caddy, create the first administrator, and know which volumes hold your data.
---

# Install

AbiData ships as one container image. The repository's `compose.yaml` runs it behind Caddy, which provides HTTPS and HTTP/2.

## What you need {#requirements}

- A Linux host with Docker and the Compose plugin.
- A DNS name pointing at the host, and ports 80 and 443 open, so Caddy can get a certificate. For a trial on your own machine, `localhost` works with Caddy's local certificate.
- Enough disk for the data and backups volumes. Each workspace is one SQLite file.

## The image {#image}

The `Dockerfile` has three stages: build the web app with Node, build the Go binary with `CGO_ENABLED=0`, and copy both into `gcr.io/distroless/static-debian12:nonroot`. The final image:

- runs as user 65532, with no shell or package manager;
- starts `/abidata serve` and listens on port 8080;
- declares two volumes, `/var/lib/abidata` (data) and `/var/lib/abidata-backups` (backups);
- has a built-in `HEALTHCHECK` that runs `/abidata healthcheck`, which asks the local server for `/healthz`.

Build it yourself, for one platform or several:

```sh
make docker-local     # loads abidata:dev into the local Docker daemon
make docker           # linux/amd64 and linux/arm64, build only
```

## Install with Compose {#compose}

1. Get the repository on the host.
2. Create the two secrets. This writes `secrets/session_key` and `secrets/manifest_key`, each with 32 random bytes in base64 and mode 600:

   ```sh
   scripts/gen-secrets.sh
   ```

3. Set your public address. `ABIDATA_PUBLIC_URL` must be the exact address people type in the browser, with no path:

   ```sh
   export ABIDATA_PUBLIC_URL=https://abidata.example.com
   export ABIDATA_SITE=abidata.example.com
   ```

   `ABIDATA_SITE` is the host name Caddy serves and gets a certificate for. If you leave both unset, they default to `https://localhost` and `localhost`.
4. Set the first administrator's email (the compose file passes it to the server):

   ```sh
   export ABIDATA_BOOTSTRAP_ADMIN_EMAIL=you@example.com
   ```

5. Start it:

   ```sh
   docker compose up -d --build
   ```

6. Create the first administrator: see [First run](#first-run).

The compose file defines:

| Service or volume | Purpose |
|---|---|
| `abidata` | The application. `stop_grace_period` is 45 seconds so a shutdown can finish cleanly |
| `caddy` | TLS, HTTP/2, reverse proxy to `abidata:8080`. `flush_interval -1` is required so live updates (server-sent events) are not buffered |
| `abidata-data` | Mounted at `/var/lib/abidata`: the admin database, keys, every workspace file, staging files and job artifacts |
| `abidata-backups` | Mounted at `/var/lib/abidata-backups`: the backups |
| `caddy-data`, `caddy-config` | Caddy's certificates and configuration |

The two secret files are passed as `ABIDATA_SESSION_KEY_FILE` and `ABIDATA_MANIFEST_KEY_FILE`. See [Configuration](/docs/operations/configuration) for every setting.

### Tell AbiData about the proxy {#proxy}

AbiData sees Caddy's address, not the visitor's. For correct client addresses in logs and in the sign-in rate limit, it must trust `X-Forwarded-For` from Caddy. The compose file sets `ABIDATA_TRUSTED_PROXIES` to the private address ranges by default (`10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`). That is safe there because the app's port is not published: only containers on the Compose network can reach it.

If you publish the app's port or run it outside Compose, narrow this to your proxy's address, for example:

```sh
export ABIDATA_TRUSTED_PROXIES=172.18.0.0/16
```

Find the real range with `docker network inspect`. Only addresses in this list are allowed to supply `X-Forwarded-For`.

## First run {#first-run}

On first start the server creates its folders under the data volume, the lock file `system/server.lock`, the admin database, and any key that you did not supply.

There are two ways to create the first platform administrator.

**Setup token (recommended).** With `ABIDATA_BOOTSTRAP_ADMIN_EMAIL` set and no users yet, the server writes a one-time setup token to its log at warning level:

```sh
docker compose logs abidata | grep setup_token
```

Open your site. You are sent to the first-run setup page. Paste the token, enter a display name and a password of at least 10 characters. The account uses the email you configured. The token works once, and a new one is generated each time the server starts while there are still no users.

**Command line.** The `user create-admin` command writes to the same data directory, so it cannot run while the server holds the lock. Stop the server first, then run it in a one-off container with the same volumes and secrets:

```sh
docker compose stop abidata
docker compose run --rm --no-deps --entrypoint /abidata abidata user create-admin --email you@example.com --name "Your Name"
docker compose start abidata
```

The password is read from standard input.

After you sign in, create workspaces and user accounts as the administrator. People do not register themselves: an administrator creates each account and an owner adds it to a workspace. See [Members and roles](/docs/guide/members-and-roles).

## Check that it works {#check}

- `https://your-site/healthz` answers `ok`.
- `https://your-site/readyz` answers `ready` once the admin database and data directory are usable.
- `docker compose ps` shows `abidata` as healthy.

See [Monitoring](/docs/operations/monitoring).

## Without Docker {#binary}

`make build` produces a static binary at `bin/abidata` with the web app embedded. Set the same `ABIDATA_*` variables and run `bin/abidata serve`. Put a TLS-terminating proxy in front of it, and make sure the proxy does not buffer `text/event-stream` responses.
