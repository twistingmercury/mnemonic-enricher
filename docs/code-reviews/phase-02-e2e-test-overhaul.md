# Code Review: Phase 02 E2E Test Overhaul

**Review Date:** 2026-03-20
**Reviewers:** code reviewer (tactical), go software architect (Go idioms), solutions architect (architecture)
**Phase:** 02 (E2E Test Fix — Enrich pipeline coverage)

## Files Reviewed

### Source Files

- `src/internal/config/config.go` — BaseURL field and validation (Cycle 1)
- `src/internal/config/defaults.go` — DefaultOpenAIBaseURL default (Cycle 1)
- `src/internal/service/openai/embedding.go` — BaseURL-aware constructor (Cycle 1)
- `src/internal/service/openai/extraction.go` — BaseURL-aware constructor (Cycle 1)
- `src/tests/e2e/openai-stub/main.go` — Deterministic OpenAI HTTP stub (Cycle 2)
- `src/tests/e2e/openai-stub/Dockerfile` — Stub image build (Cycle 2)
- `src/tests/docker-compose.yaml` — Retargeted test compose (Cycle 3)
- `src/build/build.sh` — Updated infra startup (Cycle 3)
- `src/tests/e2e/test-runner.sh` — Retargeted runner (Cycle 3)
- `src/tests/e2e/helpers/db.go` — Postgres seed/assert helpers (Cycle 4)
- `src/tests/e2e/helpers/neo4j.go` — Neo4j query helpers (Cycle 4)
- `src/tests/e2e/helpers/amqp.go` — RabbitMQ publish helpers (Cycle 4)
- `src/tests/e2e/helpers/helpers.go` — Test client (Cycle 4/5)
- `src/tests/e2e/go.mod` — E2E module deps (Cycle 4)

### Test Files

- `src/tests/e2e/api/operations_test.go` — Retargeted operational tests (Cycle 5)
- `src/tests/e2e/pipeline/enrichment_test.go` — Pipeline happy/unhappy path tests (Cycles 6–7)
- `src/internal/config/config_test.go` — BaseURL validation tests (Cycle 1)

## Validation Results

| Tool | Result |
| --- | --- |
| `go vet ./internal/config/... ./internal/service/openai/...` | PASS |
| `go test ./internal/config/... ./internal/service/openai/...` | PASS (config 0.025s, openai 0.063s) |
| `go vet ./...` (src/) | PASS |
| `go build ./...` (src/) | PASS |
| `go vet ./...` (src/tests/e2e/) | PASS |
| `go build ./...` (src/tests/e2e/) | PASS |
| `go build ./...` (src/tests/e2e/openai-stub/) | PASS |

## Design Compliance

Implementation satisfies all phase-02 behavioral requirements from `docs/superpowers/specs/2026-03-20-e2e-test-fix-design.md`.

### Behavioral Requirements Verified

- `MNEMONIC_OPENAI_BASE_URL` config field added with env var and default ✓
- OpenAI stub serves `/v1/embeddings`, `/v1/chat/completions`, `/control/fail-next` ✓
- Stub returns 2000-element float32 embedding vector (all 0.1) ✓
- Stub extraction response is the exact JSON string parseConcepts expects ✓
- `e2e_api` removed from docker-compose.yaml ✓
- `e2e_enricher` has healthcheck + MNEMONIC_OPENAI_BASE_URL env ✓
- `TestSwaggerUI_ReturnsOK` and `TestHealthCheck_UnhealthyReturns503` removed ✓
- Pipeline happy path seeds pattern+chunk+job, polls to completed, asserts embedding + Neo4j ✓
- Unhappy path tests cover malformed message, missing job ID, OpenAI failure ✓

### Design Doc Divergences (Post-Review)

Code review fixes introduced the following improvements over the design:

#### Structural Divergences (justified improvements over design doc)

| Divergence | Design Doc | Implementation | Assessment |
| --- | --- | --- | --- |
| `newOpenAIEmbedding` / `newOpenAIExtraction` internal constructors | Not specified | Added to eliminate type assertions in `newEmbeddingServiceWithURL` / `newExtractionServiceWithURL` | Safer — no runtime panic risk |
| `%w: %w` double-wrap on retry errors | Not specified | Wraps both sentinel and cause | Better error chain, allows `errors.Is` on inner error |
| `ErrEmptyEmbeddingResponse` / `ErrEmptyExtractionResponse` sentinels | Not specified | Replaces bare `fmt.Errorf` for empty-response cases | Testable, matchable via `errors.Is` |
| BaseURL validated for scheme+host, not just parse success | "verify it parses as a valid URL" | Rejects bare strings like `"not-a-url"` | Matches actual intent of the spec |
| `SeedEnrichmentJob` takes only `chunkID` | Spec shows 4-param signature | Removed unused `patternID` `_` parameter | Self-documenting: chunk-only jobs |
| `PollJobStatus` / `PollJobStatusRaw` use `context.WithTimeout` | Not specified | Replaces `time.Now().Before(deadline)` loop | Propagates cancellation to DB queries |
| `NewPGConn` registers `t.Cleanup(pool.Close)` | Not specified | Pool closed at test teardown automatically | Prevents goroutine/connection leak |
| Health poll in malformed/not-found tests | Fixed 2s sleep | 10-attempt × 300ms retry loop | Faster on fast hosts, more reliable on slow |
| Embedding vector uses `[]float32` in stub | Not specified | Matches consumer struct type | Eliminates float64→float32 implicit cast |

## Findings

### HIGH Priority

| ID | Source | Finding | Resolution |
| --- | --- | --- | --- |
| G2 | go software architect | `newEmbeddingServiceWithURL` and `newExtractionServiceWithURL` used type assertions on the public constructor result (`NewEmbeddingService(cfg).(*openaiEmbedding)`). If the constructor return type changes, this panics at runtime with no compile-time warning. | Fixed: introduced `newOpenAIEmbedding` / `newOpenAIExtraction` private constructors; both public and test constructors delegate to them. No type assertions remain. |
| G5/A1 | go software architect + solutions architect | `url.Parse` accepts almost any string without error (e.g., `"not-a-url"` parses successfully). The validation gave false confidence that `BaseURL` was a valid HTTP URL. | Fixed: added `u.Scheme == ""` and `u.Host == ""` checks; test `TestOpenAIBaseURL_ValidateRejectsBareString` added. |
| A3 | solutions architect | `SeedEnrichmentJob(t, pool, chunkID, _ uuid.UUID)` silently discarded its `patternID` argument (blank identifier). Callers passed `patternID` expecting it to be used; the mismatch was a maintainability trap. | Fixed: removed the unused parameter from the signature; all call sites updated. |

### MEDIUM Priority

| ID | Source | Finding | Resolution |
| --- | --- | --- | --- |
| G3 | go software architect | All retry error wraps used `fmt.Errorf("%w: %v", ErrXxx, err)` — the inner `err` was not in the error chain and could not be matched with `errors.Is`/`errors.As`. | Fixed: changed to `"%w: %w"` throughout both services (Go 1.26 multi-error wrapping). |
| G4 | go software architect | `fmt.Errorf("empty embedding response")` and `fmt.Errorf("empty chat response: no choices returned")` were bare strings — not matchable as sentinel errors. | Fixed: defined `ErrEmptyEmbeddingResponse` and `ErrEmptyExtractionResponse` sentinels; returned directly from `doEmbed`/`doExtract`. |
| G6 | go software architect | Polling loops in `PollJobStatus` and `PollJobStatusRaw` used `context.Background()` for each DB query. If the deadline expired between the loop guard and the query, the query could block beyond the test timeout. | Fixed: loops now use `context.WithTimeout(context.Background(), timeout)` — the context is passed to every `pool.QueryRow` call. |
| G7 | go software architect | `SeedPattern`, `SeedChunk`, `SeedEnrichmentJob`, `CleanupPattern`, `AssertChunkEmbeddingSet` used `context.Background()` with no timeout. A slow or unreachable Postgres would hang the test indefinitely. | Fixed: each function creates a 5-second context for its DB call. |
| G8 | go software architect | `NewPGConn` never registered `t.Cleanup(pool.Close)`, so the pool's background goroutines and connections leaked for the lifetime of the test binary. | Fixed: `t.Cleanup(pool.Close)` registered in `NewPGConn` after successful ping. |
| A4 | solutions architect | `TestEnrichmentPipeline_MalformedMessage` and `TestEnrichmentPipeline_JobNotFound` used `time.Sleep(2 * time.Second)` before asserting health — fragile on slow CI hosts, wasteful on fast ones. | Fixed: replaced with `pollHealthOK` helper (10 × 300ms) that returns as soon as `/health` returns 200. |
| A6/G13 | solutions architect + go software architect | Stub built embedding vector as `[]float64`, but the consumer struct in `embedding.go` deserializes into `[]float32`. Type mismatch between stub and client. | Fixed: stub now uses `[]float32` matching the consumer. |

### LOW Priority

| ID | Source | Finding | Resolution |
| --- | --- | --- | --- |
| G1 | go software architect | `EmbeddingService` and `ExtractionService` interfaces defined in the `openai` package (same package as implementation). Go convention places interfaces in consuming packages. | Not fixed — pre-existing design; moving interfaces would require touching `internal/enrichment` which is out of phase-02 scope. Document for Phase 03. |
| A2 | solutions architect | `handleFailNext` stores `10` instead of `1`. Spec says "causes the next call to return 500, then resets." | Not fixed — the value 10 was an intentional Cycle 8 fix to ensure all retry attempts within a single job fail. Functionally correct; the value of 10 exceeds `RetryAttempts=3` to handle edge cases. |
| A7 | solutions architect | `t.Cleanup` registration order in `TestEnrichmentPipeline_HappyPath` is counter-intuitive (LIFO execution is correct but not obvious from reading top-to-bottom). | Not fixed — execution order is correct; purely a readability concern. |
| G9 | go software architect | `PublishJob` and `PublishRaw` duplicate the AMQP connection setup. | Not fixed — acceptable for test helpers; comment added noting the queue name must match `DefaultRabbitMQQueue`. |
| A5 | solutions architect | `PollJobStatus` naming is surprising — it fatals on `failed` status. `PollJobStatusRaw` is the safer variant. | Not fixed — renaming would risk breakage; added to Phase 03 notes. |
| A9 | solutions architect | Enricher image name hardcoded in `docker-compose.yaml`. If `IMAGE_NAME` is overridden in `build.sh`, compose still uses the default name. | Not fixed — low practical risk (no tests fail); the `build.sh` and compose must agree on the image name. Document for Phase 03. |
| A10 | solutions architect | `openai-stub/Dockerfile` does not set `CGO_ENABLED=0 GOOS=linux` explicitly. | Not fixed — the builder container (golang:1.21-alpine) produces a Linux binary naturally. Low risk in practice. |
| A11 | solutions architect | `os.Getenv("ENRICHER_URL")` duplicated inline in `TestEnrichmentPipeline_MalformedMessage` and `TestEnrichmentPipeline_JobNotFound`. | Partially mitigated by `pollHealthOK` helper which accepts the URL as a parameter. Full deduplication deferred to Phase 03. |

## Patterns to Document

1. **Transient AMQP connection per publish** — `amqp.go` dials, declares queue, publishes, and closes on each call. Avoids shared connection state across parallel tests.
2. **`PollJobStatus` / `PollJobStatusRaw` split** — separate "fatal on failure" from "return for inspection" polling; useful for happy/unhappy path test pairs targeting the same async system.
3. **`context.WithTimeout` in polling loops** — pass the derived context to each query so the database deadline is bounded by the test timeout, not unbounded.
4. **Private factory + public constructor delegation** — `newOpenAIEmbedding` / `newOpenAIExtraction` pattern eliminates type assertions in test helpers while keeping the public API clean.

## Notes for Future Phases

**Phase 03** (future): Move `EmbeddingService` and `ExtractionService` interfaces to `internal/enrichment` (consuming package) per Go convention (G1).
**Phase 03** (future): Rename `PollJobStatus` → `PollJobStatusUntilComplete`, `PollJobStatusRaw` → `PollJobStatus` for clearer semantics (A5).
**Phase 03** (future): Parameterize enricher image name via `${ENRICHER_IMAGE:-...}` in `docker-compose.yaml` (A9).
