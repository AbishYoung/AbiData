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
