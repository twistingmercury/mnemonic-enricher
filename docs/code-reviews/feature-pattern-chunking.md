# Code Review: Pattern Chunking

**Review Date:** 2026-03-02
**Reviewers:** code-reviewer, solutions-architect, go-software-engineer
**Phase:** Pattern Chunking (H2-section splitting, per-chunk embeddings, chunk enrichment pipeline)

## Files Reviewed

### Source Files

- `src/mnemonic/cmd/loader/main.go`
- `src/mnemonic/internal/enricher/enrichment.go`
- `src/mnemonic/internal/handlers/patterns/patterns.go`
- `src/mnemonic/internal/handlers/skillfiles/skillfiles.go`
- `src/mnemonic/internal/mcpserver/format.go`
- `src/mnemonic/internal/repository/chunk/chunk.go`
- `src/mnemonic/internal/repository/chunk/doc.go`
- `src/mnemonic/internal/repository/chunk/errors.go`
- `src/mnemonic/internal/repository/chunk/repository.go`
- `src/mnemonic/internal/repository/enrichmentjob/enrichmentjob.go`
- `src/mnemonic/internal/repository/enrichmentjob/repository.go`
- `src/mnemonic/internal/repository/pattern/doc.go`
- `src/mnemonic/internal/repository/pattern/pattern.go`
- `src/mnemonic/internal/repository/pattern/repository.go`
- `src/mnemonic/internal/server/routes.go`
- `src/mnemonic/internal/server/server.go`
- `src/mnemonic/internal/service/enrichment/service.go`
- `src/mnemonic/internal/service/pattern/service.go`
- `src/mnemonic/internal/service/search/service.go`
- `src/migrations/postgres/000009_pattern_schema_chunks.up.sql`
- `src/migrations/postgres/000009_pattern_schema_chunks.down.sql`
- `docs/architecture/04-data-architecture.md`
- `docs/design/mcp-server.md`
- `docs/design/pattern-processing.md`
- `docs/design/service-layer.md`
- `docs/openapi/mnemonic-v1.yaml`

### Test Files

- `src/mnemonic/internal/enricher/enrichment_test.go`
- `src/mnemonic/internal/handlers/patterns/patterns_test.go`
- `src/mnemonic/internal/mcpserver/deps_test.go`
- `src/mnemonic/internal/mcpserver/format_test.go`
- `src/mnemonic/internal/mcpserver/handlers_test.go`
- `src/mnemonic/internal/repository/chunk/repository_test.go`
- `src/mnemonic/internal/repository/enrichmentjob/repository_test.go`
- `src/mnemonic/internal/repository/pattern/repository_integration_test.go`
- `src/mnemonic/internal/repository/pattern/repository_test.go`
- `src/mnemonic/internal/service/enrichment/service_test.go`
- `src/mnemonic/internal/service/pattern/service_test.go`
- `src/mnemonic/internal/service/pattern/split_chunks_test.go`
- `src/mnemonic/internal/service/search/service_test.go`
- `src/mnemonic/tests/e2e/agents_test.go`
- `src/mnemonic/tests/e2e/patterns_test.go`
- `src/mnemonic/tests/e2e/types.go`

## Validation Results

| Tool            | Result       |
| --------------- | ------------ |
| `go vet ./...`  | PASS (clean) |
| `golangci-lint` | Not run      |

## Design Compliance

Implementation satisfies core chunking behavioral requirements from `docs/plans/2026-02-27-pattern-schema-chunks-design.md`.

### Behavioral Requirements Verified

- Pattern content split into H2-bounded chunks on create ✓
- Per-chunk embeddings stored in `pattern_chunks` table ✓
- Per-chunk enrichment jobs created on pattern create ✓
- Chunk-level aggregate status updates pattern status ✓
- MCP search returns chunk-level results with section context ✓
- `GET /v1/api/patterns/:id/chunks` endpoint added ✓

### Design Doc Divergences (Post-Review)

No renames applied. One structural divergence: the design doc describes the update path replacing chunks, but the implementation defers this (TODO Task 6).

#### Structural Divergences (justified improvements over design doc)

| Divergence              | Design Doc                | Implementation                                                | Assessment                           |
| ----------------------- | ------------------------- | ------------------------------------------------------------- | ------------------------------------ |
| Update path re-chunking | Chunks replaced on update | Update creates legacy pattern-level job; chunks not refreshed | Must fix before merge; tracked as H8 |

#### Documents Updated

| Document                                                | Scope                                                 | Status               |
| ------------------------------------------------------- | ----------------------------------------------------- | -------------------- |
| `docs/plans/2026-02-27-pattern-schema-chunks-design.md` | Annotate update path deferral                         | Pending (see L4)     |
| `docs/architecture/04-data-architecture.md`             | Reflect post-009 chunk_id index                       | Pending (see L3)     |
| `docs/design/pattern-processing.md`                     | Correct async embedding pseudocode; update index docs | Pending (see L2, L3) |

## Findings

### HIGH Priority

| ID  | Source                                        | Finding                                                                                                                                                                                                                                                                                                                                                                                                           | Resolution                                                                                                                                                                                                                           |
| --- | --------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| H1  | solutions-architect                           | Handler enforces a 10,240-byte content limit (`handlers/patterns/patterns.go:321`) that migration 000009 explicitly dropped from the DB. Pattern files up to 18 KB described in the design doc are rejected before reaching the database.                                                                                                                                                                         | Remove the upper bound from `validatePatternFields`. Keep only the minimum check (`contentLen == 0`). Update error message and OpenAPI spec.                                                                                         |
| H2  | solutions-architect                           | Migration 000009 makes `pattern_id` nullable in `enrichment_jobs` but adds no unique partial index on `chunk_id`. Duplicate pending chunk jobs can be enqueued. (`000009_pattern_schema_chunks.up.sql:83`)                                                                                                                                                                                                        | Add `CREATE UNIQUE INDEX idx_enrichment_jobs_unique_pending_chunk ON enrichment_jobs (chunk_id) WHERE status IN ('pending', 'processing')`. Also add a CHECK constraint ensuring exactly one of `pattern_id`/`chunk_id` is non-null. |
| H3  | code-reviewer                                 | `json.Marshal` error silently discarded in loader (`cmd/loader/main.go:119`): `body, _ := json.Marshal(req)`. If the struct ever gains a non-serializable field, the loader POSTs an empty body with no diagnostic.                                                                                                                                                                                               | `body, err := json.Marshal(req); if err != nil { return fmt.Errorf("marshal request: %w", err) }`                                                                                                                                    |
| H4  | code-reviewer                                 | `http.NewRequest` error silently discarded in PUT fallback (`cmd/loader/main.go:129`). If `apiURL` is malformed, `putReq` is nil and the next line panics.                                                                                                                                                                                                                                                        | `putReq, err := http.NewRequest(...); if err != nil { return fmt.Errorf("build PUT request: %w", err) }`                                                                                                                             |
| H5  | go-software-engineer                          | `enrichmentjob` package has a `Status string` field on `Job` and a `type Status string` declaration in the same package. The field uses untyped `string`, allowing arbitrary values without going through `IsValidStatus`. The doc comment for the type appears directly above the field, creating maximum confusion. (`enrichmentjob/enrichmentjob.go:23,50`)                                                    | Rename the field type to `Status Status` (typed), or rename the type to `JobStatus`. The typed approach makes invalid assignment a compile error.                                                                                    |
| H6  | go-software-engineer                          | Multiple bare `return err` calls in `enrichmentjob/repository.go` (lines 125, 256, 280, 337) bypass the repository error abstraction, leaking raw `*pgconn.PgError` through the boundary. All other files in the project wrap errors with `fmt.Errorf`.                                                                                                                                                           | Wrap all repository errors with contextual `fmt.Errorf("...: %w", err)`. Define a sentinel (e.g., `ErrInvalidState`) and map the check-violation PG error code to it.                                                                |
| H7  | all three agents + external                   | `service/search/service.go:101-148` — both `AgentName` scoping and `Tags` filtering are resolved/validated above the `chunkRepo.FindSimilar` call but never forwarded to `chunkrepo.SimilarityOptions`. Any request with `?agent=...` or `?tags=...` silently returns unfiltered results from all agents' patterns. This is a behavior regression from the pre-chunking search path, which enforced both filters. | Add `PatternIDs []uuid.UUID` and `Tags []string` to `chunkrepo.SimilarityOptions`. Pass resolved pattern IDs and tags into `FindSimilar` via `AND pc.pattern_id = ANY($N)` and `AND p.tags @> $N`.                                   |
| H8  | solutions-architect, code-reviewer + external | `service/pattern/service.go:385-391` — `Update` creates a legacy pattern-level enrichment job but does not delete stale `pattern_chunks` rows or enqueue per-chunk jobs. Because semantic search now reads exclusively from chunk embeddings, updating a pattern leaves old chunk embeddings as the only search-visible representation of the new content — the update is invisible to search.                    | When `chunkRepo != nil`, `Update` must call `chunkRepo.DeleteByPatternID`, re-run `splitIntoChunks`, call `chunkRepo.CreateBatch`, and enqueue per-chunk enrichment jobs, matching the create path.                                  |

### MEDIUM Priority

| ID  | Source                                    | Finding                                                                                                                                                                                                                                                                                                             | Resolution                                                                                                                                                                                 |
| --- | ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| M1  | solutions-architect, code-reviewer        | IVFFlat index in migration 000009 (`up.sql:69`) is created on an empty table. PostgreSQL builds it with zero centroids, silently degrading all vector searches to sequential scans until data is seeded and the index is rebuilt.                                                                                   | Switch to HNSW (`USING hnsw`) which builds correctly on empty tables, or move index creation to a post-seed step with operational documentation.                                           |
| M2  | solutions-architect, go-software-engineer | `GetChunks` handler (`handlers/patterns/patterns.go:63`) and `routes.go:24` expose `ChunkRepo chunkrepo.Repository` directly in the `Services` struct — the only handler that bypasses the service layer. Creates a precedent and makes future access-control or business logic changes require two-location edits. | Add `ListChunks(ctx, patternID)` to `patternsvc.Service`, remove `ChunkRepo` from `Services`, route the handler through the service.                                                       |
| M3  | solutions-architect                       | `patternResponse.Chunks []chunkSummary` field (line 147) is defined in the `GET /patterns/:id` response struct but never populated. Design doc shows chunks inline in the pattern detail response. Clients expecting the design behavior receive an omitted field.                                                  | Either populate `Chunks` in the `Get` handler by calling a service method, or remove the field and update the OpenAPI spec to clarify chunks are only available at `/patterns/:id/chunks`. |
| M4  | go-software-engineer                      | `AllEnrichedForPattern` uses `NOT EXISTS (... status != 'enriched')` which is vacuously true for a pattern with zero chunks. The comment at line 195 notes this but it is an assertion, not an enforced invariant.                                                                                                  | Add a `COUNT(*) > 0` guard to the `AllEnrichedForPattern` query, or add a DB-level constraint preventing zero-chunk patterns.                                                              |
| M5  | go-software-engineer                      | `processChunkJob` treats `s.chunkRepo == nil` as a recoverable `failJob` condition (`service/enrichment/service.go:134`). If `chunkRepo` were nil, every chunk job would be marked failed, requiring manual DB cleanup. This is a startup misconfiguration, not a runtime error.                                    | Assert `chunkRepo != nil` in `New` and return a startup error or panic rather than deferring to runtime.                                                                                   |
| M6  | go-software-engineer                      | `ValidStatuses []string` and `IsValidStatus(status string)` in the `enrichmentjob` package accept `string` rather than the typed `Status`. Callers can pass arbitrary strings where a typed argument would reject them at compile time. (`enrichmentjob/enrichmentjob.go:68,76`)                                    | Change `ValidStatuses` to `[]Status` and `IsValidStatus` to accept `Status`.                                                                                                               |
| M7  | go-software-engineer, code-reviewer       | `IsValidEnrichmentStatus` / `ValidEnrichmentStatuses` are duplicated verbatim in `chunk/chunk.go` and `pattern/pattern.go`. A new status requires two edits.                                                                                                                                                        | Extract to a shared `repository` package constant or `enrichmentstatus` sub-package.                                                                                                       |
| M8  | go-software-engineer                      | `splitIntoChunks` tests missing edge cases: empty string, whitespace-only content, empty H2 title (`## ` with no text), consecutive empty sections, H2 inside a fenced code block. (`service/pattern/split_chunks_test.go`)                                                                                         | Add table-driven subtests for each case. For H2 inside a fenced code block: if the split behavior is intentional, document it; otherwise add fence-tracking state to the implementation.   |
| M9  | code-reviewer                             | `service_test.go:341` test for agent filter has a misleading green: the mock allows `FindSimilar` to return a chunk that coincidentally belongs to the agent's patterns, but scoping is not actually enforced. The test comment acknowledges this.                                                                  | Convert to `t.Skip("agent scoping not yet implemented — see issue #X")` so CI reports a skipped test rather than a false green. Now superseded by H7 fix.                                  |
| M10 | code-reviewer                             | E2E test `TestGetPatternChunks_ReturnsChunksForPattern` asserts only `chunkList.Chunks != nil`, which passes for an empty slice. Chunks are created synchronously during `Create`, so a stronger assertion is safe. (`tests/e2e/patterns_test.go:2568`)                                                             | Add `assert.GreaterOrEqual(t, chunkList.Count, 2)` and assert expected section titles.                                                                                                     |
| M11 | code-reviewer                             | `skillfiles.go:241` — the file count limit check is silently skipped when `ListBySkill` returns an error. A transient DB error bypasses the limit, allowing excess files to be created.                                                                                                                             | Propagate the error: `if listErr != nil { handlers.RespondError(c, listErr); return }`                                                                                                     |
| M12 | external                                  | `repository/chunk/repository.go:321-323` — chunk similarity query filters on `embedding IS NOT NULL` but not `enrichment_status = 'enriched'`. A chunk that received an embedding and then transitioned to `failed` (e.g., graph pipeline failed after embedding write) remains eligible for search results.        | Add `AND pc.enrichment_status = 'enriched'` to the `FindSimilar` query.                                                                                                                    |
| M13 | external                                  | `cmd/loader/main.go:128-131` — on 409 conflict, the loader retries with `PUT /v1/api/patterns/{name}`, but the update route expects a UUID path parameter, not a name. The fallback always produces a 404 or routing mismatch; loader re-runs cannot upsert existing patterns.                                      | Fix the fallback to look up the pattern UUID first (via `GET /v1/api/patterns/{name}`) and retry with `PUT /v1/api/patterns/{uuid}`.                                                       |
| M14 | external                                  | `handlers/patterns/patterns.go:730-743` — `GET /patterns/{id}/chunks` returns `200 {"chunks": [], "count": 0}` when the pattern ID does not exist. The OpenAPI spec documents a 404 for missing patterns, so callers cannot distinguish "pattern exists but has no chunks" from "pattern does not exist."           | Before listing chunks, verify the pattern exists (e.g., call `patternSvc.Get`); return 404 if not found.                                                                                   |

### LOW Priority

| ID  | Source               | Finding                                                                                                                                                                                                            | Resolution                                                                                                                                                         |
| --- | -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| L1  | solutions-architect  | `mcpserver/format.go:23` — "Found N patterns matching" counts chunk matches, not distinct patterns. Multiple chunks from one pattern inflate the count misleadingly for LLM consumers.                             | Count distinct `PatternID` values and report "Found N sections across M patterns matching '...'".                                                                  |
| L2  | solutions-architect  | `pattern-processing.md:174` pseudocode implies embedding is synchronous at write time. Actual flow is two-phase async.                                                                                             | Update pseudocode to reflect the async split: write-time creates chunk rows without embeddings; the enrichment worker embeds them asynchronously.                  |
| L3  | solutions-architect  | Unique index docs in `04-data-architecture.md` and `pattern-processing.md` do not reflect the post-009 chunk_id gap (see H2). Update after H2 is resolved.                                                         | Update both docs to show the chunk_id unique partial index.                                                                                                        |
| L4  | solutions-architect  | Design docs describe update path chunk replacement (`docs/plans/2026-02-27-pattern-schema-chunks-design.md:64`). The update path is intentionally deferred (TODO Task 6).                                          | Annotate the design doc to note the update path is deferred to Task 6.                                                                                             |
| L5  | go-software-engineer | Per-chunk job creation in `service/pattern/service.go:284` omits `Status: string(enrichmentrepo.StatusPending)` whereas the pattern-level job path sets it explicitly.                                             | Set `Status: string(enrichmentrepo.StatusPending)` on chunk job creation to match.                                                                                 |
| L6  | go-software-engineer | `MarkFailed` in `enrichmentjob/repository.go:322` scans `RETURNING` values into local variables that are immediately discarded. Extra DB serialization with no benefit.                                            | Remove the `RETURNING` clause and switch to `Exec`.                                                                                                                |
| L7  | go-software-engineer | `inflightCount atomic.Int64` in `enricher/enrichment.go` tracks what `sync.WaitGroup` already tracks.                                                                                                              | Remove `inflightCount` and derive the count from the WaitGroup, or replace the drain log message with a fixed string.                                              |
| L8  | go-software-engineer | Per-chunk job creation failures in `service/pattern/service.go:273-278` are silently swallowed as warnings. If all job creation calls fail, chunks exist with no enrichment jobs and no recovery mechanism.        | Document the gap explicitly and consider a periodic reconciler (scan for chunks with no pending or completed job).                                                 |
| L9  | code-reviewer        | `enricher/enrichment.go:55` comment on `drainCtx` does not explain why `context.Background()` is used rather than `ctx`.                                                                                           | Expand comment: "drainCtx is intentionally derived from Background, not ctx — it must remain live after ctx is cancelled to give in-flight jobs time to complete." |
| L10 | code-reviewer        | Parallel subtests in `repository/chunk/repository_test.go:865` share a mutable `chunks` slice. Production `CreateBatch` mutates `c.ID` in place; a race would occur if subtests ran against a real implementation. | Declare per-subtest chunk slices inside the test case struct, not shared across subtests.                                                                          |

## Patterns to Document

1. **Transaction detection via interface assertion** (`chunk/repository.go:97-106`): `CreateBatch` detects whether the caller holds an existing transaction by asserting the `pgx.Tx` interface on the connection, then conditionally calling `BeginTx`. This is the idiomatic pattern for batch inserts that must work in both transactional and non-transactional contexts. Document as a project-level convention.

2. **Compile-time interface checks** (`var _ Repository = (*pgxRepository)(nil)`): Used consistently across all new repository implementations. Codify as a required pattern for all future repository packages.

## Notes for Future Phases

**Task 6** (Update chunk-awareness): Implement chunk deletion and re-creation on pattern update, replacing the current legacy pattern-level job path.

**Task 9** (ChunkRepo wiring): Remove the nil-accepting `chunkRepo` optional parameter pattern from `patternsvc.New` and `enrichmentsvc.New` once the repo is fully wired. Document the typed-nil footgun hazard until then.

**Post-merge** (Agent scoping): Add `PatternIDs []uuid.UUID` to `chunkrepo.SimilarityOptions` and implement the agent filter end-to-end (currently resolves IDs but never uses them).

---

## Final Pre-Merge Review

**Review Date:** 2026-03-02
**Reviewers:** code-reviewer, solutions-architect, go-software-engineer (second pass)

### HIGH Finding Verification

All 8 HIGH findings from the initial review are confirmed fixed. Evidence from all three agents:

| ID | Finding Summary | Verified By | Status |
| -- | --------------- | ----------- | ------ |
| H1 | 10KB handler limit replaced with 100KB | All three agents | FIXED |
| H2 | Unique partial index + exclusive CHECK on enrichment_jobs | All three agents | FIXED |
| H3 | json.Marshal error wrapped in loader | All three agents | FIXED |
| H4 | http.NewRequest error wrapped in loader | All three agents | FIXED |
| H5 | JobStatus named type; Status field is typed | All three agents | FIXED |
| H6 | Error wrapping in enrichmentjob/repository.go Mark* methods; ErrInvalidJobTarget sentinel added | All three agents | FIXED |
| H7 | PatternIDs and Tags forwarded to FindSimilar; dynamic SQL with nextParam tracking | All three agents | FIXED |
| H8 | Update deletes stale chunks, re-splits, CreateBatch, enqueues per-chunk jobs | All three agents | FIXED |

### New Findings (Final Pass)

#### MEDIUM Priority

| ID  | Source              | Finding                                                                                                                                                                                                                                                                                                         | Resolution                                                                                                                                                             |
| --- | ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| F1  | solutions-architect | `Update` in `service/pattern/service.go` executes four sequential DB operations (patternRepo.Update → chunkRepo.DeleteByPatternID → chunkRepo.CreateBatch → enqueue jobs) with no enclosing transaction. A crash between steps leaves the pattern in a partially-updated state (e.g., no chunks). Recoverable via re-PUT but creates a gap window. | Wrap the four operations in a transaction. Architecture verdict: merge-ready if team accepts this as recoverable via re-PUT (i.e., idempotent re-run heals the gap). |
| F2  | code-reviewer       | `testJob()` helper in `repository/enrichmentjob/repository_test.go:26` sets `Status: "pending"` as an untyped string literal rather than `enrichmentrepo.StatusPending`. Compiles because `JobStatus` is `~string`, but defeats the type-strengthening intent of H5. Corroborated by go-software-engineer.      | Change to `Status: enrichmentrepo.StatusPending` in all test helper sites. Non-blocking.                                                                               |
| F3  | code-reviewer       | `ErrInvalidJobTarget` (added by H6/H2) has no unit test in `repository/enrichmentjob/repository_test.go`. `TestRepository_Create` covers `ErrPatternNotFound` (23503) but not the new check-violation path (23514).                                                                                              | Add a table case to `TestRepository_Create` that mocks `pgconn.PgError{Code: "23514"}` and asserts `errors.Is(err, ErrInvalidJobTarget)`. Non-blocking.               |
| F4  | code-reviewer       | `scanJob` (repository.go:184) fallthrough error path returns bare `err`. H6 wrapped only the three `Mark*` methods; `scanJob`, `ReclaimStale`, `DeleteCompleted`, and `DeleteFailed` still return unwrapped errors, creating asymmetric error context across the same file.                                     | Wrap with `fmt.Errorf` context at each remaining bare-return site. Non-blocking.                                                                                       |

#### LOW Priority

| ID  | Source              | Finding                                                                                                                                                                                                             | Resolution                                                                                                     |
| --- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| F5  | go-software-engineer | `patternService` has no compile-time interface guard (`var _ Service = (*patternService)(nil)`). By contrast, `chunk/repository.go:17` already has this guard. Interface drift would be caught at the call site, not definition. | Add `var _ Service = (*patternService)(nil)` near the type declaration. Non-blocking.                         |
| F6  | code-reviewer        | `ErrNoPending` in `repository/enrichmentjob/errors.go:17` is defined but never returned. `ClaimPending` returns `nil, nil` when no jobs are available. The sentinel is dead code from an earlier design.           | Remove `ErrNoPending`, or add a `// Reserved for future use` comment if intentionally preserved. Non-blocking. |

### Merge Verdict

**All three agents independently conclude: branch is production-ready.**

The one team decision required before merge is whether F1 (non-atomic `Update`) is acceptable for the current release. All agents agree it is recoverable via re-PUT and does not cause data loss or silent corruption — it creates a transient gap window only on crash. If the team accepts that risk, the branch is ready to merge to develop.
