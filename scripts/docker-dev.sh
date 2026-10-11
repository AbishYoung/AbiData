#!/usr/bin/env bash
# Runs AbiData in Docker for development: the app alone on http://localhost:8080
# (loopback only), no Caddy, debug logging, its own volumes (project "abidata-dev").
#   ABIDATA_DEV_PORT=9090 scripts/docker-dev.sh      # another port
#   scripts/docker-dev.sh down                        # stop (keeps data)
#   ABIDATA_DEV_RESET=1 scripts/docker-dev.sh down    # stop and delete dev data
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/docker-lib.sh"

port="${ABIDATA_DEV_PORT:-8080}"
url="http://localhost:$port"
compose=(docker compose -p abidata-dev -f compose.yaml -f compose.dev.yaml)

case "${1:-up}" in
  up)
    ensure_secrets
    "${compose[@]}" up -d --build abidata
    wait_healthy "$url"
    echo "AbiData (dev) is running at $url"
    show_setup "$url"
    ;;
  down)
    if [ "${ABIDATA_DEV_RESET:-}" = "1" ]; then "${compose[@]}" down -v; else "${compose[@]}" down; fi
    ;;
  logs) "${compose[@]}" logs -f abidata ;;
  status) "${compose[@]}" ps ;;
  *) usage ;;
esac
