#!/usr/bin/env bash

set -e

THIS_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "${THIS_DIR}/.." && pwd)"
MODULE_ROOT="$(cd "${REPO_DIR}/../.." && pwd)"
MNEMONIC_DIR="$(cd "${MODULE_ROOT}" && pwd)"
SRC_DIR="$(cd "${MNEMONIC_DIR}/.." && pwd)"

MAX_RETRIES="${MAX_RETRIES:-30}"
RETRY_INTERVAL="${RETRY_INTERVAL:-2}"
NEO4J_URI="${NEO4J_URI:-bolt://localhost:7688}"
NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASSWORD="${NEO4J_PASSWORD:-mnemonic_dev}"
NEO4J_DATABASE="${NEO4J_DATABASE:-neo4j}"

printf "THIS_DIR     : %s\n" "$THIS_DIR"
printf "REPO_DIR     : %s\n" "$REPO_DIR"
printf "MODULE_ROOT  : %s\n" "$MODULE_ROOT"
printf "MNEMONIC_DIR : %s\n" "$MNEMONIC_DIR"
printf "SRC_DIR      : %s\n" "$SRC_DIR"

get_neo4j_container_id() {
    docker compose -f "${SRC_DIR}/migrations/docker-compose.yaml" ps -q neo4j 2>/dev/null
}

start_test_infra() {
    printf "Starting test infrastructure...\n" >&2

    if ! docker compose -f "${SRC_DIR}/migrations/docker-compose.yaml" up -d; then
        printf "ERROR: Failed to start docker compose services\n" >&2
        return 1
    fi

    neo4j_container="$(get_neo4j_container_id)"
    if [ -z "${neo4j_container}" ]; then
        printf "ERROR: Could not find Neo4j container\n" >&2
        return 1
    fi
    printf "Found Neo4j container: %s\n" "${neo4j_container}" >&2

    printf "Waiting for Neo4j to be ready...\n" >&2
    for i in $(seq 1 "${MAX_RETRIES}"); do
        if docker exec "${neo4j_container}" cypher-shell -u "${NEO4J_USER}" -p "${NEO4J_PASSWORD}" "RETURN 1" > /dev/null 2>&1; then
            printf "Neo4j is ready (attempt %d/%d)\n" "${i}" "${MAX_RETRIES}" >&2
            return 0
        fi

        if [ "${i}" -eq "${MAX_RETRIES}" ]; then
            printf "ERROR: Neo4j failed to become ready after %d attempts\n" "${MAX_RETRIES}" >&2
            return 1
        fi

        printf "Waiting for Neo4j... (%d/%d)\n" "${i}" "${MAX_RETRIES}" >&2
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

    printf "Running graph integration tests...\n" >&2
    (cd "${MODULE_ROOT}" && go test -tags=integration ./internal/repository/graph/... -v)
}

main "$@"
