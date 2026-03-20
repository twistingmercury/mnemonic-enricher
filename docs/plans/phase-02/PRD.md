# Product Requirements Document: mnemonic-enricher Phase 02

*Gralph processes cycles in this document from top to bottom. Checklist markers are significant: `- [ ]` (open), `- [x]` (complete), `- [~]` (abandoned). Each cycle must be small, independently verifiable, and assigned to exactly one agent.*

## Objective

Replace the broken E2E tests with a suite that targets the enricher service directly and validates the full enrichment pipeline end-to-end: queue message → embedding → extraction → Postgres storage → Neo4j graph. Stub OpenAI with a deterministic Go HTTP server so tests are fast, free, and offline-capable.

## Problem Statement

The existing E2E tests have three critical defects. All tests target `e2e_api` (`mnemonic-api:latest-dev`), a different service — the enricher runs in docker-compose but is never tested. The enricher's core function (consuming RabbitMQ messages and running the enrichment pipeline) has zero E2E test coverage. Two tests are permanently dead (`TestSwaggerUI_ReturnsOK` for a non-existent endpoint, `TestHealthCheck_UnhealthyReturns503` with an unconditional `t.Skip`). The current tests provide false confidence.

## Success Criteria

- `make build` exits 0 with all E2E tests passing against the enricher (not a separate API service)
- A seeded chunk-based enrichment job, published to RabbitMQ, transitions to `completed` and produces a chunk embedding in Postgres and a Pattern node + Concept nodes in Neo4j
- Malformed queue messages, missing job IDs, and OpenAI failures are verified to produce the correct failure behavior (nack, job failed with `last_error`, no spurious graph nodes)
- `e2e_api` service is absent from `src/tests/docker-compose.yaml`
- `TestSwaggerUI_ReturnsOK` and `TestHealthCheck_UnhealthyReturns503` do not exist in the test suite

## Scope

### In scope

- Add `MNEMONIC_OPENAI_BASE_URL` config field and env var; update `NewEmbeddingService` and `NewExtractionService` to use it when non-empty
- Build a Go HTTP stub server for OpenAI (`/v1/embeddings`, `/v1/chat/completions`, `/control/fail-next`)
- Retarget `src/tests/docker-compose.yaml` and `src/build/build.sh` to the enricher as the primary service under test
- Add E2E helper packages: Postgres seed/assert, Neo4j query, RabbitMQ publish
- Retarget and clean up `api/operations_test.go`
- Write pipeline happy path and unhappy path tests
- Update `CHANGELOG.md` to document phase-02 completion

### Out of scope

- Changes to the enrichment pipeline logic (`processChunkJob`, `runGraphPipeline`, etc.)
- Adding a second queue provider
- Load or concurrency testing
- OpenAI rate-limit simulation
- Agent-to-pattern graph edge assertions

## Constraints and Decisions

- Go module root: `src/`; E2E module root: `src/tests/e2e/`
- OpenAI stub listens on `:8090`; returns a 2000-element embedding vector (all `0.1`) to match `DefaultOpenAIEmbeddingDimensions`
- Extraction stub response: `choices[0].message.content` must be the JSON string `{"concepts":["pattern","design"],"technologies":["go"],"practices":["testing"]}` — `parseConcepts` unmarshals the inner string, not the outer envelope
- `MNEMONIC_ENRICHMENT_RETRY_DELAY: 1s` set in `e2e_enricher` compose env for fast failure feedback
- Enricher healthcheck: `["CMD", "/mnemonic-enricher", "--health"]`, interval 2s, retries 10
- E2E test container env vars: `ENRICHER_URL`, `METRICS_URL` (retargeted to enricher), `POSTGRES_DSN`, `NEO4J_URI`, `NEO4J_USER`, `NEO4J_PASSWORD`, `RABBITMQ_URL`
- Unhappy path OpenAI failure: stub exposes `POST /control/fail-next` to make the next `/v1/embeddings` call return 500, then resets
- All commits must be GPG-signed (`git commit -S`)
- Full build verification: `make build` from the project root

## Implementation Plan

- [x] **Cycle 1 - Add MNEMONIC_OPENAI_BASE_URL to config and service constructors**: Add `BaseURL string` to `OpenAIConfig`; register `MNEMONIC_OPENAI_BASE_URL` env var with default `https://api.openai.com/v1`; update `NewEmbeddingService` and `NewExtractionService` to use `cfg.BaseURL` when non-empty, otherwise fall back to their hardcoded endpoint constants.
  - Agent: `go software engineer`
  - Files: `src/internal/config/config.go`, `src/internal/config/defaults.go`, `src/internal/config/config_test.go`, `src/internal/service/openai/embedding.go`, `src/internal/service/openai/extraction.go`
  - Steps:
    - Add `BaseURL string \`mapstructure:"base_url"\`` to `OpenAIConfig` in `config.go`
    - Add `DefaultOpenAIBaseURL = "https://api.openai.com/v1"` to `defaults.go`; register `v.SetDefault("openai.base_url", DefaultOpenAIBaseURL)` in `SetDefaults`
    - In `NewEmbeddingService`: when `cfg.BaseURL` is non-empty, set `baseURL: cfg.BaseURL`; otherwise keep `embeddingsEndpoint`; preserve the existing unexported `newEmbeddingServiceWithURL` helper unchanged
    - In `NewExtractionService`: same pattern with `chatCompletionsEndpoint`; preserve `newExtractionServiceWithURL`
    - Add validation in `OpenAIConfig.validate()`: if `BaseURL` is non-empty, verify it parses as a valid URL
    - Add test coverage in `config_test.go` for the new field default and non-empty override
  - Verify: `cd src && go vet ./... && go test ./internal/config/... ./internal/service/openai/...`
  - Done: Tests exit 0; `OpenAIConfig` has `BaseURL`; both service constructors use it when set

- [x] **Cycle 2 - Build OpenAI stub server**: Write a standalone Go HTTP stub server that handles `/v1/embeddings`, `/v1/chat/completions`, and `/control/fail-next`; package it as a Docker image.
  - Agent: `go software engineer`
  - Files: `src/tests/e2e/openai-stub/main.go`, `src/tests/e2e/openai-stub/go.mod`, `src/tests/e2e/openai-stub/Dockerfile`
  - Steps:
    - Create `src/tests/e2e/openai-stub/go.mod` with module path `github.com/twistingmercury/mnemonic-enricher/tests/e2e/openai-stub` and Go 1.21; no external dependencies required
    - Write `src/tests/e2e/openai-stub/main.go`: listen on `:8090`; for all routes return `401` when `Authorization` header is missing or empty; handle `POST /v1/embeddings` returning a 2000-element `float64` array of `0.1` in the OpenAI embeddings response envelope; handle `POST /v1/chat/completions` returning a response whose `choices[0].message.content` is exactly `{"concepts":["pattern","design"],"technologies":["go"],"practices":["testing"]}`; handle `POST /control/fail-next` setting an atomic flag so the next `/v1/embeddings` call returns HTTP 500 (then resets the flag)
    - Write `src/tests/e2e/openai-stub/Dockerfile`: multi-stage build — copy `go.mod` and `main.go`, build the binary, produce a minimal final image with the binary; `EXPOSE 8090`; `CMD ["/openai-stub"]`
  - Verify: `docker build -t e2e-openai-stub src/tests/e2e/openai-stub && docker rm -f _stub_test 2>/dev/null; docker run -d --name _stub_test -p 8090:8090 e2e-openai-stub && sleep 1 && curl -sf -X POST http://localhost:8090/v1/embeddings -H "Authorization: Bearer test" -H "Content-Type: application/json" -d "{}" | grep '"object"' && docker rm -f _stub_test`
  - Done: The `curl` exits 0 and the response contains `"object"`; the image builds without error

- [x] **Cycle 3 - Update docker-compose, build script, and test runner**: Remove `e2e_api` from the test compose; add `e2e_openai_stub`; add healthcheck and new env vars to `e2e_enricher`; retarget `e2e_tests` dependencies and env vars; update `build.sh` and `test-runner.sh` service names.
  - Agent: `devops engineer`
  - Files: `src/tests/docker-compose.yaml`, `src/build/build.sh`, `src/tests/e2e/test-runner.sh`
  - Steps:
    - In `src/tests/docker-compose.yaml`: remove the `e2e_api` service entirely; add `e2e_openai_stub` service (build context `./e2e/openai-stub`, dockerfile `Dockerfile`, no exposed ports beyond internal); add to `e2e_enricher`: `healthcheck` (`CMD /mnemonic-enricher --health`, interval 2s, timeout 5s, retries 10), env vars `MNEMONIC_OPENAI_BASE_URL: http://e2e_openai_stub:8090/v1` and `MNEMONIC_ENRICHMENT_RETRY_DELAY: 1s`, `depends_on: e2e_openai_stub: condition: service_started`; update `e2e_tests`: replace `depends_on: e2e_api` with `e2e_enricher: condition: service_healthy`; replace env vars `API_URL`/`MCP_URL` with `ENRICHER_URL: http://e2e_enricher:8080`, keep and retarget `METRICS_URL: http://e2e_enricher:9090`; add `POSTGRES_DSN: postgres://mnemonic:mnemonic_dev@e2e_postgres:5432/mnemonic?sslmode=disable`, `NEO4J_URI: bolt://e2e_neo4j:7687`, `NEO4J_USER: neo4j`, `NEO4J_PASSWORD: mnemonic_dev`, `RABBITMQ_URL: amqp://guest:guest@e2e_rabbitmq:5672/`
    - In `src/build/build.sh`: in the `e2e_tests` function, add `e2e_openai_stub` to the infrastructure startup list alongside `e2e_postgres`, `e2e_neo4j`, `e2e_rabbitmq`; replace `e2e_api e2e_tests` with `e2e_enricher e2e_tests` in the `docker compose up` call
    - In `src/tests/e2e/test-runner.sh`: replace `API_URL` with `ENRICHER_URL`; update the default from `http://e2e_api:8080` to `http://e2e_enricher:8080`
  - Verify: `docker compose -f src/tests/docker-compose.yaml config > /dev/null`
  - Done: Compose config parses without error; `e2e_api` service does not appear in config output; `e2e_openai_stub` and enricher `healthcheck` are present

- [x] **Cycle 4 - Add E2E helper packages and update go.mod**: Add `db.go`, `neo4j.go`, and `amqp.go` to the E2E helpers package; add required Go module dependencies; trim dead auth fields from `helpers.go` and update `apiBaseURL` to use `ENRICHER_URL`.
  - Agent: `go e2e test engineer`
  - Files: `src/tests/e2e/helpers/db.go`, `src/tests/e2e/helpers/neo4j.go`, `src/tests/e2e/helpers/amqp.go`, `src/tests/e2e/helpers/helpers.go`, `src/tests/e2e/go.mod`, `src/tests/e2e/go.sum`
  - Steps:
    - Add `github.com/jackc/pgx/v5` and `github.com/neo4j/neo4j-go-driver/v5` to `src/tests/e2e/go.mod` (run `go get` from `src/tests/e2e/`); `github.com/rabbitmq/amqp091-go` may already be present — verify before adding
    - Write `helpers/db.go`: `NewPGConn(t) *pgxpool.Pool` (reads `POSTGRES_DSN`); `SeedPattern(t, pool)` inserting a pattern row and returning its ID; `SeedChunk(t, pool, patternID)` inserting a chunk row and returning chunk ID; `SeedEnrichmentJob(t, pool, chunkID, patternID)` inserting a pending job row and returning job ID; `CleanupPattern(t, pool, patternID)` deleting pattern and cascading rows; `PollJobStatus(t, pool, jobID, timeout) string` polling `enrichment_jobs.status` until it is `completed` or `failed` or timeout expires; `AssertChunkEmbeddingSet(t, pool, chunkID)` asserting the chunk's `embedding` column is non-null
    - Write `helpers/neo4j.go`: `NewNeo4jDriver(t) neo4j.DriverWithContext` (reads `NEO4J_URI`, `NEO4J_USER`, `NEO4J_PASSWORD`); `AssertPatternNodeExists(t, driver, patternID)` querying `MATCH (p:Pattern {id: $id}) RETURN p`; `AssertConceptNodesExist(t, driver, patternID)` querying `MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $id}) RETURN c` asserting at least one result; `CleanupPatternGraph(t, driver, patternID)` deleting the pattern node and all connected concept edges/nodes
    - Write `helpers/amqp.go`: `PublishJob(t, jobID uuid.UUID)` connecting to `RABBITMQ_URL`, publishing `{"job_id":"<jobID>"}` to the `enrichment-jobs` queue with a transient connection that closes after publish
    - In `helpers/helpers.go`: remove `UserID`, `TeamID`, `UserRoles`, `RequestID` fields from `TestClient`; remove the code in `Do` that sets those headers; change `apiBaseURL()` to read `ENRICHER_URL` env var (falling back to `http://localhost:8080`); merge `NewTestClient` and `NewUnauthenticatedClient` into a single `NewTestClient` (no auth distinction needed)
  - Verify: `cd src/tests/e2e && go build ./...`
  - Done: `go build ./...` exits 0; all three new helper files compile cleanly

- [x] **Cycle 5 - Retarget and clean up operational tests**: Remove dead tests from `operations_test.go`; retarget remaining tests to the enricher.
  - Agent: `go e2e test engineer`
  - Files: `src/tests/e2e/api/operations_test.go`
  - Steps:
    - Delete `TestSwaggerUI_ReturnsOK` entirely
    - Delete `TestHealthCheck_UnhealthyReturns503` entirely
    - Verify all remaining tests use `helpers.NewTestClient(t)` (no auth distinction) and that `metricsBaseURL()` reads `METRICS_URL` env var (already does — confirm, do not change if correct)
    - Remove any `NewUnauthenticatedClient` call sites (replace with `NewTestClient` after the Cycle 4 merge)
  - Verify: `cd src/tests/e2e && go build ./... && ! grep -rq "TestSwaggerUI\|TestHealthCheck_Unhealthy" src/tests/e2e/`
  - Done: `go build ./...` exits 0; neither deleted test name appears anywhere under `src/tests/e2e/`

- [x] **Cycle 6 - Write pipeline happy path test**: Write `TestEnrichmentPipeline_HappyPath` verifying the full enrichment pipeline for a chunk-based job.
  - Agent: `go e2e test engineer`
  - Files: `src/tests/e2e/pipeline/enrichment_test.go`
  - Steps:
    - Create `src/tests/e2e/pipeline/enrichment_test.go` in package `pipeline_test`
    - Write `TestEnrichmentPipeline_HappyPath`: use `helpers.NewPGConn`, `helpers.SeedPattern`, `helpers.SeedChunk`, `helpers.SeedEnrichmentJob` to set up a pattern + chunk + pending job; call `helpers.PublishJob` with the job ID; call `helpers.PollJobStatus` with a 30-second timeout asserting `completed` (abort on `failed`); call `helpers.AssertChunkEmbeddingSet`; create a Neo4j driver via `helpers.NewNeo4jDriver` and call `helpers.AssertPatternNodeExists` and `helpers.AssertConceptNodesExist`; defer cleanup via `helpers.CleanupPattern` and `helpers.CleanupPatternGraph`
  - Verify: `cd src/tests/e2e && go build ./pipeline/...`
  - Done: `go build ./pipeline/...` exits 0

- [ ] **Cycle 7 - Write pipeline unhappy path tests**: Add three unhappy path tests covering malformed messages, missing job IDs, and OpenAI failures.
  - Agent: `go e2e test engineer`
  - Files: `src/tests/e2e/pipeline/enrichment_test.go`
  - Steps:
    - Add `TestEnrichmentPipeline_MalformedMessage`: publish the raw bytes `not-json` directly to the queue via `helpers.PublishJob` (or a raw publish variant); sleep 2s; assert the enricher `/health` still returns 200; assert no new rows appeared in `enrichment_jobs` with `status != pending` for this test run (or verify via queue depth if the AMQP management API is accessible)
    - Add `TestEnrichmentPipeline_JobNotFound`: publish `{"job_id":"<random-uuid-not-in-db>"}` via `helpers.PublishJob`; sleep 2s; assert the enricher `/health` still returns 200
    - Add `TestEnrichmentPipeline_OpenAIFailure`: call `POST /control/fail-next` on the stub (using `ENRICHER_URL` base, stub is at `http://e2e_openai_stub:8090` — use a separate `OPENAI_STUB_URL` env var or derive from `ENRICHER_URL` hostname); seed pattern + chunk + job; publish job; poll Postgres for `status = failed` (30s timeout); assert `last_error` is non-empty; call `helpers.AssertPatternNodeNotExists` (new helper: `MATCH (p:Pattern {id:$id}) RETURN p` — assert zero results)
    - Add `AssertPatternNodeNotExists` to `helpers/neo4j.go`
    - Add `PublishRaw(t, body []byte)` to `helpers/amqp.go` for the malformed message test
  - Verify: `cd src/tests/e2e && go build ./pipeline/...`
  - Done: `go build ./pipeline/...` exits 0; three unhappy path tests exist in `enrichment_test.go`

- [ ] **Cycle 8 - Full E2E verification and CHANGELOG update**: Run the complete CI pipeline to confirm all operational and pipeline tests pass against the enricher; update `CHANGELOG.md` to record the phase-02 release.
  - Agent: `devops engineer`
  - Files: `CHANGELOG.md`
  - Steps:
    - Run `make build` from the project root; if it fails, diagnose and fix only issues caused by the phase-02 changes (do not touch enrichment pipeline logic)
    - Once `make build` exits 0, update `CHANGELOG.md`: add a new `[0.2.0]` entry documenting the E2E test overhaul — correct SUT, OpenAI stub, pipeline happy and unhappy path tests
  - Verify: `make build`
  - Done: `make build` exits 0; `CHANGELOG.md` contains a `[0.2.0]` entry

- [ ] **Cycle 9 - Code review and fix critical/high/medium issues**: Run a full code review across all phase-02 changes; fix every critical, high, and medium finding; verify the build is clean after fixes.
  - Agent: `code reviewer`
  - Files: all files created or modified in Cycles 1–8
  - Steps:
    - Run `git diff main...HEAD --name-only` to enumerate every file changed in this phase
    - Review all changed files against Go conventions, the approved design spec (`docs/superpowers/specs/2026-03-20-e2e-test-fix-design.md`), and general correctness; classify each finding as critical, high, medium, or low
    - Fix every critical, high, and medium finding in place; leave low findings as comments in the progress log
    - Run `cd src && go vet ./... && go test ./internal/config/... ./internal/service/openai/...` to confirm main module is clean
    - Run `cd src/tests/e2e && go vet ./... && go build ./...` to confirm E2E module is clean
    - Run `make build` to confirm the full pipeline still passes after fixes
  - Verify: `cd src && go vet ./... && go build ./... && cd ../tests/e2e && go vet ./... && go build ./...`
  - Done: Both `go vet` and `go build` commands exit 0; no unfixed critical, high, or medium findings remain; `make build` exits 0

## Risks and Mitigations

- Risk: The enricher healthcheck (`--health` flag) is not wired into the binary, causing `service_healthy` in compose to never resolve.
  - Mitigation: Verify the flag exists in `src/cmd/main/main.go` before implementing Cycle 3; if missing, add it in Cycle 3 as a necessary support change.

- Risk: The Postgres `chunks.embedding` column is typed as `vector(N)` where N ≠ 2000, causing a dimension mismatch when the stub returns a 2000-element vector.
  - Mitigation: The stub returns exactly 2000 elements to match `DefaultOpenAIEmbeddingDimensions`. If the schema uses a different dimension, update the stub to match in Cycle 2.

- Risk: The `go.mod` in `src/tests/e2e/` already imports `amqp091-go` from phase-01 Cycle 3; adding it again will cause a conflict.
  - Mitigation: Cycle 4 instructs the agent to verify before adding — run `go list -m github.com/rabbitmq/amqp091-go` first.

- Risk: Cycle 7 needs `OPENAI_STUB_URL` to call `/control/fail-next` but the compose does not expose the stub externally.
  - Mitigation: The `e2e_tests` container is on the same `enrichment_network` as `e2e_openai_stub`; add `OPENAI_STUB_URL: http://e2e_openai_stub:8090` to the `e2e_tests` env block in Cycle 3.

- Risk: `make build` in Cycle 8 fails because `build.sh` still references `e2e_api` (stale after Cycle 3 update).
  - Mitigation: Cycle 3 explicitly updates `build.sh`; Cycle 8 is a verification gate, not a fix cycle — if build fails from a prior cycle's miss, fix in the responsible cycle.

## Definition of Done

- `make build` exits 0 from the project root
- `e2e_api` service does not exist in `src/tests/docker-compose.yaml`
- `TestSwaggerUI_ReturnsOK` and `TestHealthCheck_UnhealthyReturns503` do not exist in the test suite
- A chunk-based enrichment job published to RabbitMQ produces `status = completed`, a non-null chunk embedding, a Neo4j Pattern node, and at least one Neo4j Concept node with a `MENTIONED_IN` edge
- A malformed message and a missing job ID are handled gracefully (enricher stays healthy)
- An OpenAI failure produces `status = failed` with `last_error` set and no spurious Neo4j nodes
- No unfixed critical, high, or medium code review findings remain across phase-02 changes
