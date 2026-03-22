# E2E Test Suite Rebuild

**Date:** 2026-03-21
**Status:** Approved

## Context

The E2E test suite accumulated 9 disabled test files (`//go:build ignore`) during Phase 02 ralph loops. These files test HTTP endpoints (agents, patterns, skills, skillfiles, MCP) that do not exist — `mnemonic-enricher` is a queue worker exposing only `/health`, `/version`, and `/metrics`. The disabled files also reference helper functions and types that were never implemented. They provide no value and should be removed.

Only two test files currently compile and run:
- `api/operations_test.go` — smoke tests for `/health`, `/version`, `/metrics`
- `pipeline/enrichment_test.go` — enrichment pipeline tests (1 good, 3 shallow)

## What Gets Deleted

Remove these files entirely — they test non-existent endpoints:

- `src/tests/e2e/api/agents_test.go`
- `src/tests/e2e/api/patterns_test.go`
- `src/tests/e2e/api/skills_test.go`
- `src/tests/e2e/api/skillfiles_test.go`
- `src/tests/e2e/mcp/mcp_test.go`

## What Stays Unchanged

- `src/tests/e2e/api/operations_test.go` — tests the three real endpoints; no changes needed
- `src/tests/e2e/helpers/` — all helper files are sound; no changes needed

## What Gets Improved

### `src/tests/e2e/pipeline/enrichment_test.go`

**Keep as-is:**

- `TestEnrichmentPipeline_HappyPath` — seeds pattern + chunk + job, publishes to RabbitMQ, polls until completed, asserts chunk embedding set and Neo4j pattern + concept nodes created. This is a genuine end-to-end test.
- `TestEnrichmentPipeline_OpenAIFailure` — triggers stub failure, seeds and publishes a job, asserts status=failed with lastError set and no Neo4j node created. Good.

**Improve (two shallow tests):**

The current `TestEnrichmentPipeline_MalformedMessage` and `TestEnrichmentPipeline_JobNotFound` publish bad input then poll `/health`. A worker that deadlocks after a bad message would still return 200 on `/health` briefly — so these tests prove almost nothing.

**New assertion pattern:** after the bad input, seed a real enrichment job and verify it completes. This proves the worker did not stall, deadlock, or exit after the error.

**`TestEnrichmentPipeline_MalformedMessage`:**
1. Publish raw bytes that are not valid JSON
2. Seed a pattern + chunk + job
3. Publish the real job ID
4. Poll until job status = completed
5. Assert chunk embedding set
6. Cleanup

**`TestEnrichmentPipeline_JobNotFound`:**
1. Publish a random UUID (no corresponding DB row)
2. Seed a pattern + chunk + job
3. Publish the real job ID
4. Poll until job status = completed
5. Assert chunk embedding set
6. Cleanup

## What Is Not Covered

- Retry behavior (`max_attempts`) — out of scope for this rebuild
- Concurrent job submission — out of scope for this rebuild
- Dead-letter queue assertions — not configured in the test environment

## Success Criteria

- No disabled test files remain in the suite
- All test files compile without `//go:build ignore`
- `go test ./...` in `src/tests/e2e/` passes with all infrastructure running
- The two improved tests verify worker resilience, not just liveness
