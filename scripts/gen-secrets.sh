#!/usr/bin/env bash
set -euo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/secrets"
mkdir -p "$dir"
umask 077
for name in session_key manifest_key; do
  f="$dir/$name"
  if [ -e "$f" ]; then
    echo "exists:  $f"
  else
    openssl rand -base64 32 > "$f"
    chmod 600 "$f"
    echo "created: $f"
  fi
done
cat <<'EOF2'
The container runs as uid 65532. On Linux, give it the files without widening
them (Docker Desktop on macOS/Windows needs no change):
  sudo chown 65532:65532 secrets/session_key secrets/manifest_key
  sudo chmod 400 secrets/session_key secrets/manifest_key
Never make them world-readable: the manifest key signs backups.
EOF2
