# Shared helpers for docker-dev.sh and docker-prod.sh (sourced, not run).

# Docker Desktop installs its CLI outside the default PATH on macOS.
if ! command -v docker >/dev/null 2>&1 && [ -x "$HOME/.docker/bin/docker" ]; then
  export PATH="$HOME/.docker/bin:$PATH"
fi
command -v docker >/dev/null 2>&1 || { echo "docker not found; install Docker first" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "the Docker daemon is not running (start Docker Desktop or dockerd)" >&2; exit 1; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

ensure_secrets() {
  if [ ! -s secrets/session_key ] || [ ! -s secrets/manifest_key ]; then
    ./scripts/gen-secrets.sh
  fi
  for f in secrets/session_key secrets/manifest_key; do
    case "$(stat -c %a "$f" 2>/dev/null || stat -f %Lp "$f")" in
      *[4-7]) echo "warning: $f is readable by other users; see docs/operations/install (secrets)" >&2 ;;
    esac
  done
}

# wait_healthy <url>: waits up to two minutes for <url>/healthz.
wait_healthy() {
  for _ in $(seq 1 60); do
    if curl -ksf "$1/healthz" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  echo "the server did not become healthy at $1; recent log:" >&2
  "${compose[@]}" logs --no-color --tail 40 abidata >&2 || true
  return 1
}

# show_setup <url>: if no administrator exists yet, says where the setup token is.
show_setup() {
  if curl -ksf "$1/api/v1/auth/setup" 2>/dev/null | grep -q '"pending":true'; then
    echo
    echo "No administrator yet. Open $1 and complete setup with the one-time token from:"
    echo "  ${compose[*]} logs abidata | grep setup_token"
  fi
}

usage() {
  echo "usage: $0 [up|down|logs|status]  (default: up)" >&2
  exit 2
}
