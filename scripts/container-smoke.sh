#!/usr/bin/env bash
# Container smoke (Gate 7): build the image, `docker compose up` from a clean volume, complete first-run
# setup with the logged one-time token, create a workspace, then run tests/e2e/smoke.spec.ts against it.
# Used by CI (job container-smoke) and locally. Env: ABIDATA_SMOKE_PORT (default 18094),
# ABIDATA_SMOKE_PLATFORM (default linux/amd64), ABIDATA_SMOKE_PROJECT (default abidata-smoke), ABIDATA_SMOKE_KEEP=1 to skip teardown.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
export PATH="$HOME/.docker/bin:$PATH"
export ABIDATA_SMOKE_PORT="${ABIDATA_SMOKE_PORT:-18094}"
project="${ABIDATA_SMOKE_PROJECT:-abidata-smoke}"
base="http://127.0.0.1:${ABIDATA_SMOKE_PORT}"
email="admin@example.com"
password="admin-password-1"
compose=(docker compose -p "$project" -f compose.yaml -f tests/e2e/compose.smoke.yaml)

teardown() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- container logs ---" >&2
    "${compose[@]}" logs --no-color abidata 2>&1 | tail -n 80 >&2 || true
  fi
  if [ "${ABIDATA_SMOKE_KEEP:-}" != "1" ]; then
    "${compose[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  [ -z "${jar:-}" ] || rm -f "$jar"
  exit "$status"
}
trap teardown EXIT

./scripts/gen-secrets.sh
# The container runs as uid 65532; the bind-mounted secret files must be readable by it.
chmod 644 secrets/session_key secrets/manifest_key

"${compose[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
docker buildx build --platform "${ABIDATA_SMOKE_PLATFORM:-linux/amd64}" --load -t abidata:smoke .
"${compose[@]}" up -d --no-build abidata

echo "waiting for $base/healthz"
for _ in $(seq 1 60); do
  if curl -fsS "$base/healthz" >/dev/null 2>&1; then break; fi
  sleep 2
done
curl -fsS "$base/healthz" >/dev/null

token="$("${compose[@]}" logs --no-color abidata | grep -o '"setup_token":"[^"]*"' | head -n1 | cut -d'"' -f4)"
if [ -z "$token" ]; then echo "no setup_token in the container log" >&2; exit 1; fi

jar="$(mktemp)"
me="$(curl -fsS -c "$jar" -H "Origin: $base" -H 'Content-Type: application/json' \
  -d "{\"token\":\"$token\",\"display_name\":\"Admin\",\"password\":\"$password\"}" "$base/api/v1/auth/setup")"
csrf="$(printf '%s' "$me" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')"
curl -fsS -b "$jar" -H "Origin: $base" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
  -d '{"name":"Smoke"}' "$base/api/v1/admin/workspaces" >/dev/null
echo "setup complete for $email; workspace Smoke created"

(cd tests/e2e && \
  npm ci --no-audit --no-fund && \
  { npx playwright install --with-deps chromium >/dev/null 2>&1 || npx playwright install chromium; } && \
  ABIDATA_E2E_EXTERNAL_URL="$base" npx playwright test smoke.spec.ts)
