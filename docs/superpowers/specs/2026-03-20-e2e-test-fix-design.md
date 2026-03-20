# E2E Test Fix Design

**Date:** 2026-03-20
**Status:** Approved

## Problem

The existing E2E tests have three critical defects:

1. **Wrong service under test.** All tests target `e2e_api` (`mnemonic-api:latest-dev`), a different service. The enricher (`e2e_enricher`) runs in docker-compose but is never tested.
2. **Pipeline is untested.** The enricher's core function — consuming RabbitMQ messages and running the enrichment pipeline — has zero test coverage.
3. **Dead test.** `TestSwaggerUI_ReturnsOK` tests an endpoint the enricher does not expose. `TestHealthCheck_UnhealthyReturns503` is permanently skipped.

## Goal

Replace the existing operational-only E2E tests with a suite that:

- Targets the enricher service directly
- Verifies the full enrichment pipeline end-to-end: queue message → embedding → extraction → Postgres storage → Neo4j graph
- Stubs OpenAI with a deterministic Go HTTP server so tests are fast, free, and offline-capable

## Architecture

```
RabbitMQ ──► enricher ──► Postgres (job status + chunk embedding)
                │
                ├──► openai-stub (embedding + extraction)
                │
                └──► Neo4j (pattern nodes + concept edges)

e2e_tests ──► enricher /health, /version, /metrics  (operational)
          ──► Postgres   (seed data + assert outcomes)
          ──► Neo4j      (assert nodes + edges)
          ──► RabbitMQ   (publish job messages)
```

The test container connects directly to all four services for seeding and assertion.

## Changes

### 1. Config: Add `MNEMONIC_OPENAI_BASE_URL`

Add `BaseURL string` to `OpenAIConfig` in `src/internal/config/config.go`:

```go
type OpenAIConfig struct {
    BaseURL              string        `mapstructure:"base_url"`
    // ... existing fields
}
```

- Env var: `MNEMONIC_OPENAI_BASE_URL`
- Default (in `defaults.go`): `https://api.openai.com/v1`
- When `BaseURL` is empty or unset, both services fall back to their existing hardcoded endpoint constants — no behavior change in production
- `NewEmbeddingService` uses `cfg.BaseURL` when non-empty, otherwise uses the `embeddingsEndpoint` constant
- `NewExtractionService` uses `cfg.BaseURL` when non-empty, otherwise uses the `chatCompletionsEndpoint` constant
- `wireDependencies` in `server.go` requires no changes — constructors read `cfg.OpenAI.BaseURL` internally
- The existing unexported `newEmbeddingServiceWithURL` and `newExtractionServiceWithURL` helpers are preserved for existing unit tests
- Validation: `BaseURL` must be a valid URL when non-empty; no change to existing required fields

### 2. OpenAI Stub Server

New package at `src/tests/e2e/openai-stub/main.go`. Listens on `:8090`.

**Routes:**

`POST /v1/embeddings` — returns a canned embedding vector of length **2000** (matching `DefaultOpenAIEmbeddingDimensions`), all values `0.1`. The length must match the configured dimensions so the `UpdateEmbedding` call does not fail with a Postgres vector dimension mismatch:

```json
{
  "object": "list",
  "data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.1, ...]}],
  "model": "text-embedding-3-small",
  "usage": {"prompt_tokens": 8, "total_tokens": 8}
}
```

`POST /v1/chat/completions` — returns a canned extraction response. The `choices[0].message.content` field must be a JSON-encoded string matching the `extractionResult` struct, because `parseConcepts` unmarshals that inner string — not the outer response envelope:

```json
{
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "{\"concepts\":[\"pattern\",\"design\"],\"technologies\":[\"go\"],\"practices\":[\"testing\"]}"
      }
    }
  ]
}
```

This produces three `Concept` values (types: `domain`, `technology`, `practice`) which `runGraphPipeline` syncs to Neo4j.

Both routes return `401` for a missing or empty `Authorization` header.

Built via `src/tests/e2e/openai-stub/Dockerfile`.

### 3. Docker Compose Changes (`src/tests/docker-compose.yaml`)

- **Remove** `e2e_api` service
- **Add** `e2e_openai_stub` service (build from `./e2e/openai-stub/Dockerfile`, internal port 8090)
- **Update** `e2e_enricher`:
  - `pull_policy: never` (build from local image produced by `build.sh`)
  - Add `MNEMONIC_OPENAI_BASE_URL: http://e2e_openai_stub:8090/v1`
  - Add `MNEMONIC_ENRICHMENT_RETRY_DELAY: 1s` (fast failure feedback in tests)
  - Add `depends_on: e2e_openai_stub: condition: service_started`
  - Add `healthcheck` block (required for `service_healthy` condition in `e2e_tests`):
    ```yaml
    healthcheck:
      test: ["CMD", "/mnemonic-enricher", "--health"]
      interval: 2s
      timeout: 5s
      retries: 10
    ```
- **Update** `e2e_tests`:
  - `depends_on`: replace `e2e_api` (removed) with `e2e_enricher: condition: service_healthy`
  - Replace `API_URL` with `ENRICHER_URL=http://e2e_enricher:8080`
  - Keep `METRICS_URL` but retarget to `http://e2e_enricher:9090`
  - Remove `MCP_URL` (enricher has no MCP server)
  - Add direct infra env vars: `POSTGRES_DSN`, `NEO4J_URI`, `NEO4J_USER`, `NEO4J_PASSWORD`, `RABBITMQ_URL`

### 4. Test Structure

#### Operational tests (`api/operations_test.go`)

Retarget to `ENRICHER_URL`. Remove:

- `TestSwaggerUI_ReturnsOK` (endpoint does not exist on enricher)
- `TestHealthCheck_UnhealthyReturns503` (permanently skipped)
- Auth header fields from `TestClient` (`UserID`, `TeamID`, `UserRoles`, `RequestID`) — enricher has no auth

Keep: health, version, and metrics tests, all corrected to target the enricher. `METRICS_URL` is kept and retargeted to `http://e2e_enricher:9090`.

#### Pipeline test (`pipeline/enrichment_test.go`)

The pipeline dispatches on `job.ChunkID != nil`. A chunk-based job is the current production path: it generates an embedding for the chunk, and when all chunks for the parent pattern are enriched, runs graph sync (concept extraction + Neo4j write). The test seeds a single chunk so the aggregate check (`AllEnrichedForPattern`) triggers graph sync after this one job.

**Seed (per test, deferred cleanup):**

1. Insert a `pattern` row (name, description, tags)
2. Insert a `chunk` row linked to the pattern (`pattern_id`, `section_title`, `content`)
3. Insert an `enrichment_job` row with `chunk_id = chunk.ID`, `pattern_id = pattern.ID`, `status = pending`
4. Publish `{"job_id": "<job.ID>"}` to RabbitMQ via AMQP client

No agent seeding required — `GetAgentAssociations` returns an empty slice for a pattern with no associations, and `runGraphPipeline` skips agent node sync gracefully.

**Poll + assert (30s timeout, 500ms interval):**

1. **Job status**: Postgres `enrichment_jobs.status = completed`. Abort immediately if `status = failed` (surface `last_error`).
2. **Chunk embedding**: Postgres `chunks` row has a non-null `embedding` vector column.
3. **Neo4j Pattern node**: `MATCH (p:Pattern {id: $id}) RETURN p` — assert node exists.
4. **Neo4j Concept nodes**: `MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $id}) RETURN c` — assert at least one concept node exists with a `MENTIONED_IN` edge.

#### New helpers (`helpers/`)

| File       | Purpose                                                                |
| ---------- | ---------------------------------------------------------------------- |
| `db.go`    | Postgres connection (`pgx`), seed/cleanup helpers, embedding assertion |
| `neo4j.go` | Neo4j connection (`neo4j-go-driver`), node/edge query helpers          |
| `amqp.go`  | RabbitMQ AMQP publish helper (`amqp091-go`)                            |

Existing `helpers.go` trimmed to remove dead `UserID`, `TeamID`, `UserRoles`, `RequestID` fields. `types.go` unchanged.

## Out of Scope

- Unhappy path pipeline tests (malformed message, job not found) — covered by unit tests
- Load or concurrency testing
- OpenAI rate-limit or retry simulation
- Agent-to-pattern graph edge assertions (agent seeding adds complexity; graph sync skips cleanly with no associations)
