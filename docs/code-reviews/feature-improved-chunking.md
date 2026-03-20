# Code Review: Improved Chunking

**Review Date:** 2026-03-12
**Reviewers:** code-reviewer, solutions-architect, go-conventions-reviewer
**Phase:** feature/improved-chunking

## Files Reviewed

### Source Files

- `src/mnemonic/internal/service/enrichment/service.go` - Enrichment pipeline; adds pattern-load step and enriched embedding text
- `src/mnemonic/internal/service/openai/embedding.go` - Embedding model and dimension constants updated
- `src/mnemonic/internal/config/defaults.go` - Default config values (embedding model/dimensions)
- `src/mnemonic/build/Dockerfile` - Build/test command updated
- `src/mnemonic/tests/docker-compose.yaml` - E2E test environment
- `src/migrations/postgres/000010_update_embedding_dimensions.up.sql` - Alter embedding column to vector(2000), recreate HNSW index
- `src/migrations/postgres/000010_update_embedding_dimensions.down.sql` - Revert to vector(1536)

### Deleted Files

- `src/mnemonic/cmd/loader/main.go` - CLI loader binary removed
- `src/mnemonic/cmd/loader/main_test.go` - Loader tests removed

### Test Files

- `src/mnemonic/internal/service/enrichment/service_test.go`
- `src/migrations/tests/bats/migrations.bats`

## Validation Results

| Tool                                             | Result                            |
| ------------------------------------------------ | --------------------------------- |
| `go build ./...`                                 | Not run — environment unavailable |
| `go test ./internal/service/enrichment/...`      | Not run — environment unavailable |
| `bats src/migrations/tests/bats/migrations.bats` | Not run — environment unavailable |

## Design Compliance

Implementation satisfies the enriched-chunk-embedding plan at `docs/superpowers/plans/2026-03-12-enriched-chunk-embedding.md`.

### Behavioral Requirements Verified

- Enriched text format (`{name} | {tags} | {section_title}\n\n{content}`) implemented in `processChunkJob` ✓
- Embedding model upgraded to `text-embedding-3-large` with Matryoshka truncation at 2000 dimensions ✓
- Migration 000010 alters `pattern_chunks.embedding` to `vector(2000)` and recreates HNSW index ✓
- Loader CLI deleted (replaced by API-driven workflow) ✓

### Design Doc Divergences

None.

## Findings

### HIGH Priority

| ID  | Source                             | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Resolution                                                                                                                                                                                                  |
| --- | ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| H1  | code-reviewer, solutions-architect | Migration 000010 BATS test comments are stale and factually wrong. Lines ~530-538 and ~700-707 in `migrations.bats` claim the migration "fails" because `vector(3072)` exceeds the HNSW 2000-dimension cap. The actual SQL targets `vector(2000)` — the cap itself — and should succeed. As a result, the up-migration test weakens its assertion to `grep -q '^vector('` (matches any dimension) instead of asserting exactly `vector(2000)`. The down-migration test accepts this broken state. If 000010 failed silently in any deployed environment, chunks are being embedded at 2000 dimensions but stored in a `vector(1536)` column — a data integrity risk. | Resolved (Resolved 2026-03-12) — stale comments removed, up-test renamed and assertion tightened to `[ "$col_type" = "vector(2000)" ]`, down-test comment corrected. `psql_file` helper now exposes stderr. |
| H2  | solutions-architect, code-reviewer | No re-enrichment path exists for existing chunks. The enriched-text format (`name \| tags \| section \| content`) changes the semantic content of all future embeddings. Chunks embedded before this commit encode raw content; chunks after encode the enriched format. PGVector cosine similarity compares all chunks against each other — mixing formats in the same column makes comparisons incoherent. There is no bulk re-enrichment endpoint or runbook.                                                                                                                                                                                                     | Deferred — requires bulk re-enrichment endpoint in the Admin API and operator runbook. Tracked as future API work.                                                                                          |

### MEDIUM Priority

| ID  | Source                                 | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                | Resolution                                                                                                                                                                                |
| --- | -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | code-reviewer, go-conventions-reviewer | `processChunkJob` function docstring at ~line 128-134 in `service.go` is out of date. It lists six steps and omits the newly inserted pattern-load step (now Step 2). Maintainers reading the docstring will have an inaccurate mental model of the pipeline.                                                                                                                                                                          | Resolved — processChunkJob docstring updated to list all seven pipeline steps in order.                                                                                                   |
| M2  | code-reviewer                          | `New` constructor in `service/enrichment/service.go` guards only `chunkRepo` with a nil check (~line 96-98). All other required dependencies (`jobRepo`, `patternRepo`, `agentRepo`, `graphRepo`, `embeddingSvc`, `extractionSvc`) are accepted without validation and would panic at runtime if nil. The asymmetry looks like an oversight from when `chunkRepo` was added.                                                           | Resolved — nil checks added for all required dependencies in `New()`.                                                                                                                     |
| M3  | go-conventions-reviewer                | Empty `pattern.Tags` produces misleading enriched text. If `Tags` is empty, `strings.Join` returns `""` and the embed text becomes `"my-pattern \|  \| Section Title\n\n..."` — a double-separator with a blank field that degrades the embedding. The test fixture always has `Tags: []string{"test"}`, so this case is untested.                                                                                                     | Resolved — empty tags branch added to `processChunkJob`; `testEnrichedEmbedText` updated to mirror production logic; test case added for empty-tags path.                                 |
| M4  | solutions-architect                    | The enriched-text format (name+tags+section prepended to chunk content) is an invisible asymmetric embedding assumption — query vectors are derived from raw query text, chunk vectors from enriched text. This is correct practice but is undocumented. The data architecture doc (`docs/architecture/04-data-architecture.md`) describes similarity queries and the enrichment pipeline without mentioning the enriched-text format. | Resolved — "Enriched Text Format for Embeddings" section added to docs/design/pattern-processing.md covering the format spec, query/document asymmetry, and the re-enrichment constraint. |
| M5  | code-reviewer, solutions-architect     | The loader CLI (`cmd/loader/`) was deleted. The pattern-loading tool is being developed as a separate external project. Until that tool is ready, operators have no documented workflow for bulk-loading patterns into Mnemonic.                                                                                                                                                                                                       | False positive — Dockerfile updated to `./cmd/main/...`. Loader replacement is an external project; no documentation gap in this repo.                                                    |

### LOW Priority

| ID  | Source                                 | Finding                                                                                                                                                                                                                                                                                                                                                    | Resolution                                                                                                            |
| --- | -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| L1  | code-reviewer, go-conventions-reviewer | Test function `TestProcessJob_ChunkJob_Step2_EmbeddingFails` is now mislabeled — the embedding step is Step 3 after the pattern-load step was inserted at Step 2. The step numbering contains two `Step2` entries: `Step1_LoadChunkFails`, `Step2_LoadPatternFails`, `Step2_EmbeddingFails`, `Step3_UpdateEmbeddingFails`, `Step4_UpdateChunkStatusFails`. | Resolved — test functions renumbered: Step3_EmbeddingFails, Step4_UpdateEmbeddingFails, Step5_UpdateChunkStatusFails. |
| L2  | go-conventions-reviewer                | `go.mod` declares `go 1.26`, which does not exist (latest stable is 1.24). Almost certainly a typo for `go 1.24`. Not introduced by this commit but present in the module file.                                                                                                                                                                            | False positive — Go 1.26.1 is current.                                                                                |
| L3  | code-reviewer                          | `psql_file` helper in `migrations.bats` redirects both stdout and stderr to `/dev/null` (`>/dev/null 2>&1`). Migration errors are silently discarded, which can cause tests to produce false passes when the SQL fails.                                                                                                                                    | Resolved — Keep stderr visible: `psql -f "$file" >/dev/null` (redirect stdout only).                                  |
| L4  | go-conventions-reviewer                | `testEnrichedEmbedText()` in `service_test.go` duplicates the production format string `"%s \| %s \| %s\n\n%s"` verbatim. If the format changes in production, the test helper will silently diverge.                                                                                                                                                      | Deferred — format is stable and unlikely to change; YAGNI.                                                            |

## Patterns to Document

1. **Enriched embedding text as a schema-level contract**: The `"{name} | {tags} | {section_title}\n\n{content}"` format is encoded into the vector database for all chunk embeddings. Any change to this format makes existing embeddings semantically stale relative to new ones. Document that format changes require a full re-enrichment pass.

2. **Asymmetric embedding (document vs. query)**: Query vectors use raw query text; document vectors use enriched metadata-prefixed text. This is correct for retrieval but must be documented as an architectural assumption so future engineers understand why they differ.

## Notes for Future Phases

**feature/improved-chunking**: Before this branch ships to any deployed environment, confirm (a) migration 000010 column dimension is `vector(2000)` in the target DB and (b) a plan for re-enriching all existing chunks is in place or documented.
