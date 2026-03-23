# Code Review: Phase 01 Cleanup and Hardening

**Review Date:** 2026-03-23
**Reviewers:** code reviewer (tactical), go software architect (Go idioms), solutions architect (architecture)
**Phase:** 01 (Cleanup and Hardening — swagger removal, CI fixes, E2E test rebuild)

## Files Reviewed

### Source Files

- `.github/workflows/mnemonic-enrichment-ci.yaml` — CI ordering fix + IMAGE_NAME env
- `src/build/Dockerfile` — swagger dead code removal
- `src/cmd/main/main.go` — swagger annotation removal
- `src/go.mod` / `src/go.sum` — dependency cleanup

### Test Files

- `src/tests/docker-compose.yaml` — image name correction
- `src/tests/e2e/pipeline/enrichment_test.go` — resilience test improvements

### Deleted Files (5)

- `src/tests/e2e/api/agents_test.go`
- `src/tests/e2e/api/patterns_test.go`
- `src/tests/e2e/api/skills_test.go`
- `src/tests/e2e/api/skillfiles_test.go`
- `src/tests/e2e/mcp/mcp_test.go`

## Validation Results

| Tool | Result |
| --- | --- |
| `go build ./...` (src/) | PASS |
| `go build ./tests/e2e/...` | PASS |
| `grep "go:build ignore" src/tests/e2e/` | No output (all disabled files removed) |

## Design Compliance

Implementation satisfies all requirements from `docs/superpowers/specs/2026-03-21-e2e-test-rebuild-design.md`.

### Behavioral Requirements Verified

- 5 disabled `//go:build ignore` test files with undefined helpers removed ✓
- `TestEnrichmentPipeline_MalformedMessage` uses publish-poison → seed-real-job → assert-completion pattern ✓
- `TestEnrichmentPipeline_JobNotFound` uses publish-unknown-UUID → seed-real-job → assert-completion pattern ✓
- `pollHealthOK` helper removed (deadlocked workers still return 200 briefly) ✓
- GHCR login moved before `build.sh` in CI (build.sh pulls private GHCR images) ✓
- `IMAGE_NAME` env var set on build step to correct image name ✓
- `swag init` and `swag install` removed from Dockerfile ✓
- `go.mod` cleaned of swaggo and transitive openapi dependencies ✓
- `amqp091-go` promoted from indirect to direct dependency ✓
- docker-compose image name corrected from `mnemonic:latest` to `mnemonic-enrichment:latest` ✓

## Disagreements Resolved During Synthesis

**Code reviewer H-1 (race condition in resilience tests) — Overridden**

The code reviewer flagged that the worker could process the real job before the DB row was seeded. Analysis: `SeedEnrichmentJob` is called synchronously (pgx waits for round-trip) and completes before `PublishJob`. The worker cannot process a message that has not yet been published. The ordering is safe. No fix needed.

**Code reviewer H-2 (missing `packages: write` permission) — Invalid**

The workflow already contains `packages: write` in the job-level permissions block (line ~34). This finding is incorrect.

**Solutions architect H2 (stale warm runner cache) — Invalid**

`build.sh` uses `docker build --rm --no-cache` on every invocation, ensuring the image is always built from source. No stale cache risk.

**Code reviewer M-2 (build metadata not passed to Dockerfile ARGs) — Invalid**

`build.sh` already resolves `BUILD_VER`, `BUILD_DATE`, and `BUILD_COMMIT` from git inside the script and passes them as `--build-arg` to the Dockerfile. The CI env vars are optional overrides; the defaults work correctly.

## Findings

### HIGH Priority

None.

### MEDIUM Priority

| ID | Source | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | go software architect | `TestEnrichmentPipeline_HappyPath` registers cleanups in a different order than the other three tests. HappyPath registers: `CleanupPattern`, `driver.Close`, `CleanupPatternGraph`. LIFO execution: `CleanupPatternGraph` → `driver.Close` → `CleanupPattern`. This is safe today but creates a maintenance trap: a future contributor adding a Neo4j cleanup in HappyPath after `driver.Close` would create a use-after-close bug. | Standardize all four tests to the same registration order: `driver.Close` first (runs last), `CleanupPatternGraph` second, `CleanupPattern` third (runs first). The MalformedMessage/JobNotFound convention is the canonical form. |
| M2 | go software architect | `TestEnrichmentPipeline_OpenAIFailure` registers cleanups in a third distinct order: `driver.Close`, `CleanupPattern`, `CleanupPatternGraph`. All four tests should share one canonical cleanup registration order. | Same fix as M1: standardize to driver.Close → CleanupPatternGraph → CleanupPattern registration order across all four tests. |
| M3 | solutions architect | No fast-fail on enricher container crash. If the enricher exits at startup (bad config, missing env var), the seed job will never complete and the poll will time out with "poll timed out" rather than "enricher exited with code 1." This makes CI failures harder to diagnose. | Consider adding a brief `/health` check with a short timeout (3–5 seconds) as a startup guard in test helpers — not a liveness loop. This fails fast on crash without masking deadlocks like the old `pollHealthOK` did. Deferred to Phase 03. |

### LOW Priority

| ID | Source | Finding | Resolution |
| --- | --- | --- | --- |
| L1 | go software architect | OpenAI stub control call (`http.DefaultClient.Do`) is inline in `TestEnrichmentPipeline_OpenAIFailure`. All other infrastructure interactions go through `helpers.*`. | Extract to `helpers.ArmOpenAIFailure(t)` for consistency. Deferred. |
| L2 | go software architect | `openAIStubURL` default `"http://localhost:8090"` hardcoded in test body. | Move default resolution to helpers package, alongside `NewPGConn` and `NewNeo4jDriver`. Deferred. |
| L3 | solutions architect | No architectural decision record (ADR) for swagger/REST API removal. The annotations implied the service was once intended to have a documented HTTP API; future architects have no record of the decision to remain API-free. | Add an ADR entry in `docs/architecture/` noting that this service is intentionally API-free beyond `/health`, `/version`, and `/metrics`, and that swaggo tooling was removed accordingly. |
| L4 | solutions architect | `docker-compose.yaml` image name is now hardcoded to the correct value but not fully parameterized. If `IMAGE_NAME` is overridden at the CI job level for a release tag or multi-arch build, the compose file still uses the hardcoded default. | Parameterize with `${IMAGE_NAME:-ghcr.io/twistingmercury/mnemonic-enrichment}` in Phase 03. |
| L5 | code reviewer | `controlResp.Body` in `TestEnrichmentPipeline_OpenAIFailure` is deferred-closed but never drained. Go's HTTP client does not reuse the TCP connection unless the body is fully read before close. Minor resource leak in tests. | Add `io.Copy(io.Discard, controlResp.Body)` before the defer, or drain inline. |

## Patterns to Document

1. **Resilience test structure for queue workers**: publish poison/invalid message → seed real data → publish real job → assert completion. Proves worker consumer loop is still running. Stronger than HTTP health polling, which can mask deadlocks. Tag: `testing`, `queue-worker`.
2. **CI registry login before compose**: Registry login must precede any `docker-compose` or `docker build` step that pulls private images. Compose pulls happen at `up` time, not build time — failures appear as image-not-found rather than auth errors, making root cause non-obvious.
3. **Canonical `t.Cleanup` order for Neo4j + Postgres tests**: Register in this order so LIFO runs correctly: `driver.Close` (registered 1st, runs last), `CleanupPatternGraph` (registered 2nd, runs 2nd), `CleanupPattern` (registered 3rd, runs first). Graph cleanup must precede driver close; Postgres cleanup can run first or last safely.

## Notes for Future Phases

**Phase 03** (future): Add startup guard in E2E test helpers — brief `/health` check with short timeout to fail fast on enricher crash (M3).
**Phase 03** (future): Extract `helpers.ArmOpenAIFailure(t)` and stub URL default resolution into helpers package (L1, L2).
**Phase 03** (future): Parameterize enricher image name in `docker-compose.yaml` with `${IMAGE_NAME:-...}` (L4).
**Phase 03** (future): Add ADR for API-free design decision (L3).
