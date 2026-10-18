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

   The server runs as user 65532 inside the container, and Compose mounts the files with their host owner and mode. On Linux, hand them to that user instead of widening them (Docker Desktop on macOS and Windows needs no change):

   ```sh
   sudo chown 65532:65532 secrets/session_key secrets/manifest_key
   sudo chmod 400 secrets/session_key secrets/manifest_key
   ```

   Never make them world-readable. Anyone who can read the manifest key can forge backups that AbiData will accept.

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
   scripts/docker-prod.sh
   ```

   The script checks that `ABIDATA_PUBLIC_URL` is https, creates the secrets if they are missing, runs `docker compose up -d --build`, waits until the server is healthy, and tells you whether setup is still pending. `scripts/docker-prod.sh down`, `logs` and `status` stop the stack, follow the logs and list the containers. Plain `docker compose up -d --build` does the same start without the checks.

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

### Ports {#ports}

| Script | Binds | Serves |
|---|---|---|
| `scripts/docker-prod.sh` | 80 and 443 on all interfaces (Caddy) | `https://$ABIDATA_SITE`; the app's own port 8080 is not published |
| `scripts/docker-dev.sh` | `127.0.0.1:8080` only (set `ABIDATA_DEV_PORT` for another port; `up --lan` also listens on this network) | `http://localhost:8080` or `http://127.0.0.1:8080`, the app alone without Caddy, debug logging |

The development stack uses its own Compose project (`abidata-dev`) and volumes, so it can run next to a production stack on the same machine without sharing data. Stop it with `scripts/docker-dev.sh down`; `ABIDATA_DEV_RESET=1 scripts/docker-dev.sh down` also deletes its data. Never expose the development port beyond a network you trust: it serves plain http. To try the app from a phone on the same Wi-Fi, run `scripts/docker-dev.sh up --lan`: it listens on all interfaces, allows this machine's LAN address as an origin and prints it. Installing the app as a PWA needs https, so use the production stack for that.

### Tell AbiData about the proxy {#proxy}

AbiData sees Caddy's address, not the visitor's, so it reads the visitor's address from `X-Forwarded-For`, but only on requests that come from a trusted proxy. The compose file puts both services on an internal network with fixed addresses (`172.30.70.0/24`, Caddy at `172.30.70.2`) and sets `ABIDATA_TRUSTED_PROXIES` to Caddy's address alone. Client addresses then appear correctly in logs and in the sign-in rate limits, including for visitors on your own network.

If `172.30.70.0/24` collides with a network you already use, change the subnet, both `ipv4_address` values and `ABIDATA_TRUSTED_PROXIES` together. If you run AbiData behind a different proxy, set `ABIDATA_TRUSTED_PROXIES` to that proxy's address only, never to a whole range that clients also use. Only addresses in this list may supply `X-Forwarded-For`.

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

The password is read from standard input, or from `ABIDATA_PASSWORD` when that is set.

### Recover a locked-out administrator {#recover-admin}

If the last platform administrator has forgotten their password, reset it from the command line. As with `create-admin`, the server must be stopped:

```sh
docker compose stop abidata
docker compose run --rm --no-deps --entrypoint /abidata abidata user reset-password --email you@example.com
docker compose start abidata
```

Type the new password on standard input (at least 10 characters), or pass it in the `ABIDATA_PASSWORD` environment variable. All of that user's sessions end and their sign-in lockout is cleared. The reset is recorded in the audit log as `user.password_reset_cli`, without the password. Other administrators do not need this: they can use **Reset password…** on the **Users** tab of the Admin console.

## The sample workspaces {#demo}

When you finish setup with the setup token, AbiData also creates two sample workspaces and makes you their owner, so a new install has something to explore straight away:

- **Northwind Traders** holds a small trading company: employees, customers, products, orders and order lines (about 25 customers, 20 products and 60 orders dated over the last six months, so the data looks current when you install), four or more saved queries (sales by month, top customers, low stock, open orders), a customer form, a product catalogue report, and a scheduled job that refreshes each product's low-stock flag every morning at 06:30.
- **Northwind Invasives** is an invasive species programme on Northwind's supplier farms: suppliers with map locations, species, pest sightings with GPS positions and photos, quarantine zones drawn on a map, spatial queries, a sighting form and a report with maps. See [Example: invasive species](/docs/guide/example-invasive-species).

Nothing in either uses the network. They are ordinary workspaces: change them, back them up, or suspend them from the administration pages when you no longer need them. You can rename them or add your own tables next to the sample ones.

- Set `ABIDATA_DEMO_WORKSPACE=false` before first start to skip them. See [Configuration](/docs/operations/configuration).
- Setup never fails because of the sample workspaces; if one could not be built the reason is in the server log (look for `subsystem":"demo"`).
- Setup through `user create-admin` does not create them. To add them later to any install, stop the server and run `abidata demo --owner you@example.com` (in Docker: `docker compose run --rm --no-deps --entrypoint /abidata abidata demo --owner you@example.com`). Add `--kind traders` or `--kind invasives` for only one of them. Each is skipped if a workspace with its name already exists.
- To give everyone a sample of their own, open the admin console's **Workspaces** tab and choose **Northwind Traders for everyone…** or **Northwind Invasives for everyone…**. Each active user, administrators included, who does not already own that sample gets a copy named after them, such as **Northwind Invasives (their name)**, that they own, so changes in one copy never affect another. Running it again only adds copies for people who joined since. It runs while the server is up and builds the sample as it is today, maps and photos included.
- A Northwind Traders workspace created before the samples were split also holds the invasive species tables. Nothing changes it; Northwind Invasives is added beside it.

After you sign in, create workspaces and user accounts as the administrator. People do not register themselves: an administrator creates each account and an owner adds it to a workspace. See [Members and roles](/docs/guide/members-and-roles).

## Check that it works {#check}

- `https://your-site/healthz` answers `ok`.
- `https://your-site/readyz` answers `ready` once the admin database and data directory are usable.
- `docker compose ps` shows `abidata` as healthy.

See [Monitoring](/docs/operations/monitoring).

## Without Docker {#binary}

`make build` produces a static binary at `bin/abidata` with the web app embedded. Set the same `ABIDATA_*` variables and run `bin/abidata serve`. Put a TLS-terminating proxy in front of it, and make sure the proxy does not buffer `text/event-stream` responses.
