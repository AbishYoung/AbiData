#!/usr/bin/env bash
# Runs the PostgreSQL backend's tests (ADR 0012 §10) against a throwaway
# server for each image in ABIDATA_TEST_POSTGRES_IMAGES. Each server listens on
# 127.0.0.1 only, with a random password that is never printed, and is removed
# afterwards.
set -euo pipefail

images=${ABIDATA_TEST_POSTGRES_IMAGES:-postgres:17 postgres:18}
# The packages with PostgreSQL tests; tools/testplan's postgresPackages lists
# the same ones.
packages=${ABIDATA_TEST_POSTGRES_PACKAGES:-./internal/pgdb/... ./internal/admin/...}

for image in $images; do
	name="abidata-test-pg-$$-${RANDOM}"
	password=$(openssl rand -hex 24)
	docker run -d --rm --name "$name" -e POSTGRES_PASSWORD="$password" \
		-p 127.0.0.1::5432 "$image" >/dev/null
	trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
	for _ in $(seq 1 60); do
		docker exec "$name" pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1 && break
		sleep 1
	done
	port=$(docker port "$name" 5432/tcp | head -n1 | sed 's/.*://')
	version=$(docker exec "$name" postgres --version)
	echo "== $image ($version) on 127.0.0.1:$port"
	ABIDATA_TEST_POSTGRES_URL="postgres://postgres:${password}@127.0.0.1:${port}/postgres?sslmode=disable" \
		go test -race -count=1 $packages
	docker rm -f "$name" >/dev/null
	trap - EXIT
done
