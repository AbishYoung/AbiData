#!/usr/bin/env bash
# Prepares a CI job's PostgreSQL service container (ADR 0012 §10) and exports
# its connection strings to the job's later steps through $GITHUB_ENV:
#
#   ABIDATA_TEST_POSTGRES_URL  the superuser, for internal/pgtest (go test)
#   ABIDATA_E2E_POSTGRES_URL   a role that owns a database and has CREATEROLE
#                              (and nothing more), as the app runs in production,
#                              for tests/e2e/server.mjs
#
# The service starts with a throwaway password; this replaces it with random
# ones, masked in the log and never printed. Usage: ci-postgres.sh CONTAINER PORT
set -euo pipefail

container=$1
port=$2
: "${GITHUB_ENV:?run this in a GitHub Actions job}"

for _ in $(seq 1 60); do
	docker exec "$container" pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1 && break
	sleep 1
done

super=$(openssl rand -hex 24)
app=$(openssl rand -hex 24)
echo "::add-mask::$super"
echo "::add-mask::$app"
# Over the container's local socket, where the image trusts postgres; the
# passwords go on stdin, not on a command line.
docker exec -i "$container" psql -q -v ON_ERROR_STOP=1 -U postgres -d postgres >/dev/null <<SQL
ALTER ROLE postgres PASSWORD '$super';
CREATE ROLE abidata_e2e LOGIN CREATEROLE PASSWORD '$app';
CREATE DATABASE abidata_e2e OWNER abidata_e2e;
SQL

{
	echo "ABIDATA_TEST_POSTGRES_URL=postgres://postgres:${super}@127.0.0.1:${port}/postgres?sslmode=disable"
	echo "ABIDATA_E2E_POSTGRES_URL=postgres://abidata_e2e:${app}@127.0.0.1:${port}/abidata_e2e?sslmode=disable"
} >>"$GITHUB_ENV"
echo "PostgreSQL $(docker exec "$container" postgres --version | awk '{print $3}') ready on 127.0.0.1:${port}"
