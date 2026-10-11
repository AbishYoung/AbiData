#!/usr/bin/env bash
# Runs AbiData in Docker for production: Caddy on ports 80 and 443 (automatic
# HTTPS for ABIDATA_SITE) in front of the app, which is not published itself.
#   ABIDATA_SITE=abidata.example.com ABIDATA_PUBLIC_URL=https://abidata.example.com \
#   ABIDATA_BOOTSTRAP_ADMIN_EMAIL=you@example.com scripts/docker-prod.sh
#   scripts/docker-prod.sh down     # stop (data volumes are kept)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/docker-lib.sh"

site="${ABIDATA_SITE:-localhost}"
public="${ABIDATA_PUBLIC_URL:-https://$site}"
export ABIDATA_SITE="$site" ABIDATA_PUBLIC_URL="$public"
compose=(docker compose -f compose.yaml)

case "${1:-up}" in
  up)
    case "$public" in
      https://*) ;;
      *) echo "ABIDATA_PUBLIC_URL must start with https:// in production (got $public)" >&2; exit 1 ;;
    esac
    if [ "$site" = "localhost" ]; then
      echo "note: ABIDATA_SITE is not set; serving https://localhost with Caddy's local certificate." >&2
    fi
    ensure_secrets
    "${compose[@]}" config -q
    "${compose[@]}" up -d --build
    wait_healthy "$public"
    echo "AbiData is running at $public (ports 80 and 443)"
    show_setup "$public"
    ;;
  down) "${compose[@]}" down ;;
  logs) "${compose[@]}" logs -f ;;
  status) "${compose[@]}" ps ;;
  *) usage ;;
esac
