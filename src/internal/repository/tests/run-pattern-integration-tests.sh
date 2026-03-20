#!/usr/bin/env bash

set -e

THIS_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "${THIS_DIR}/.." && pwd)"
MODULE_ROOT="$(cd "${REPO_DIR}/../.." && pwd)"
MNEMONIC_DIR="$(cd "${MODULE_ROOT}" && pwd)"
SRC_DIR="$(cd "${MNEMONIC_DIR}/.." && pwd)"

MAX_RETRIES="${MAX_RETRIES:-30}"
RETRY_INTERVAL="${RETRY_INTERVAL:-2}"
PG_USR_NAME="${PG_USR_NAME:-mnemonic}"
PG_DB_NAME="${PG_DB_NAME:-mnemonic}"
PG_HOST_NAME="${PG_HOST_NAME:-localhost}"
PG_PORT="${PG_PORT:-5433}"

printf "THIS_DIR     : %s\n" "$THIS_DIR"
printf "REPO_DIR     : %s\n" "$REPO_DIR"
printf "MODULE_ROOT  : %s\n" "$MODULE_ROOT"
printf "MNEMONIC_DIR : %s\n" "$MNEMONIC_DIR"
printf "SRC_DIR      : %s\n" "$SRC_DIR"

start_test_infra() {
    printf "Starting test infrastructure...\n" >&2

    # Migrations run automatically via docker-entrypoint-initdb.d when container starts
    if ! docker compose -f "${SRC_DIR}/migrations/docker-compose.yaml" up -d; then
        printf "ERROR: Failed to start docker compose services\n" >&2
        return 1
    fi

    printf "Waiting for PostgreSQL to be ready...\n" >&2
    for i in $(seq 1 "${MAX_RETRIES}"); do
        if pg_isready -h "${PG_HOST_NAME}" -p "${PG_PORT}" -U "${PG_USR_NAME}" > /dev/null 2>&1; then
            printf "PostgreSQL is ready (attempt %d/%d)\n" "${i}" "${MAX_RETRIES}" >&2
            return 0
        fi

        if [ "${i}" -eq "${MAX_RETRIES}" ]; then
            printf "ERROR: PostgreSQL failed to become ready after %d attempts\n" "${MAX_RETRIES}" >&2
            return 1
        fi

        printf "Waiting for PostgreSQL... (%d/%d)\n" "${i}" "${MAX_RETRIES}" >&2
        sleep "${RETRY_INTERVAL}"
    done

    return 1
}

cleanup() {
    printf "Cleaning up test infrastructure...\n" >&2
    docker compose -f "${SRC_DIR}/migrations/docker-compose.yaml" down -v --remove-orphans
}

main() {
    trap cleanup EXIT

    if ! start_test_infra; then
        printf "ERROR: Failed to start test infrastructure\n" >&2
        return 1
    fi
    printf "SUCCESS: database infrastructure started\n" >&2

    printf "Running pattern integration tests...\n" >&2
    (cd "${MODULE_ROOT}" && go test -tags=integration ./internal/repository/pattern/... -v)
}

main "$@"
