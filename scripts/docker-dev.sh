#!/usr/bin/env bash
# Runs AbiData in Docker for development: the app alone on http://localhost:8080
# (loopback only), no Caddy, debug logging, its own volumes (project "abidata-dev").
# http://127.0.0.1:8080 and http://[::1]:8080 work as well as localhost.
#   ABIDATA_DEV_PORT=9090 scripts/docker-dev.sh      # another port
#   scripts/docker-dev.sh up --lan                    # also reachable from this network (e.g. a phone)
#   scripts/docker-dev.sh down                        # stop (keeps data)
#   ABIDATA_DEV_RESET=1 scripts/docker-dev.sh down    # stop and delete dev data
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/docker-lib.sh"

port="${ABIDATA_DEV_PORT:-8080}"
url="http://localhost:$port"
compose=(docker compose -p abidata-dev -f compose.yaml -f compose.dev.yaml)

case "${1:-up}" in
  up)
    if [ "${2:-}" = "--lan" ]; then
      lan_ip="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')"
      [ -n "$lan_ip" ] || { echo "could not find this machine's LAN address" >&2; exit 1; }
      export ABIDATA_DEV_BIND=0.0.0.0
      export ABIDATA_ALLOWED_ORIGINS="http://$lan_ip:$port${ABIDATA_ALLOWED_ORIGINS:+,$ABIDATA_ALLOWED_ORIGINS}"
    fi
    ensure_secrets
    "${compose[@]}" up -d --build abidata
    wait_healthy "$url"
    echo "AbiData (dev) is running at $url"
    if [ -n "${lan_ip:-}" ]; then
      echo "On this network: http://$lan_ip:$port (plain http: the PWA cannot be installed from it; use the production stack's https for that)"
    fi
    show_setup "$url"
    ;;
  down)
    if [ "${ABIDATA_DEV_RESET:-}" = "1" ]; then "${compose[@]}" down -v; else "${compose[@]}" down; fi
    ;;
  logs) "${compose[@]}" logs -f abidata ;;
  status) "${compose[@]}" ps ;;
  *) usage ;;
esac
