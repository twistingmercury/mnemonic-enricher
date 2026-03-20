# Code Review: Dynamic Language and Domain Fields

**Review Date:** 2026-03-09
**Reviewers:** code-reviewer, solutions-architect, go-software-engineer
**Phase:** feature/dynamic-lang-and-domain

## Files Reviewed

### Source Files

- `src/mnemonic/internal/handlers/patterns/patterns.go` - Pattern CRUD and search handlers
- `src/mnemonic/internal/handlers/agents/agents.go` - Agent CRUD handlers
- `src/mnemonic/internal/handlers/skills/skills.go` - Skill CRUD handlers
- `src/mnemonic/internal/handlers/skillfiles/skillfiles.go` - Skill file CRUD handlers
- `docs/openapi/mnemonic-v1.yaml` - OpenAPI specification
- `src/mnemonic/tests/run-e2e.sh` - E2E test runner

### Test Files

- `src/mnemonic/internal/handlers/patterns/patterns_test.go`
- `src/mnemonic/internal/handlers/agents/agents_test.go`
- `src/mnemonic/internal/handlers/skills/skills_test.go`
- `src/mnemonic/internal/handlers/skillfiles/skillfiles_test.go`
- `src/mnemonic/tests/e2e/agents_test.go`
- `src/mnemonic/tests/e2e/patterns_test.go`
- `src/mnemonic/tests/e2e/skillfiles_test.go`
- `src/mnemonic/tests/e2e/skills_test.go`

## Validation Results

| Tool | Result |
| ---- | ------ |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `make analyze` | PASS |
| `go test ./internal/handlers/...` | PASS |
| `bash tests/run-e2e.sh` | PASS (all tests, 40s) |

## Design Compliance

Implementation satisfies all behavioral requirements from the PRD:

### Behavioral Requirements Verified

- `language` and `domain` accept any kebab-case value ✓
- Non-kebab-case values still return 400 ✓
- All PUT endpoints return 204 No Content with no body ✓
- OpenAPI spec updated: `enum:` removed, `pattern:` + `maxLength: 64` added across all 10 field locations ✓
- ADR-005 (open vocabulary) and ADR-006 (204 on PUT) written ✓

### Design Doc Divergences (Post-Review)

None.

## Findings

### HIGH Priority

| ID | Source | Finding | Resolution |
| -- | ------ | ------- | ---------- |
| H1 | go-software-engineer | `len()` used for language/domain max-length check in `patterns.go` lines 373 and 382 instead of `utf8.RuneCountInString()`. Every other string length check in `validatePatternFields` uses `utf8.RuneCountInString`. `len()` counts bytes, not runes — inconsistent with the rest of the file and with OpenAPI `maxLength` semantics (character count). The kebab-case regex prevents non-ASCII in practice, but the inconsistency is a latent correctness risk. | Replace `len(language)` and `len(domain)` with `utf8.RuneCountInString(language)` and `utf8.RuneCountInString(domain)`. |
| H2 | go-software-engineer | Double `Body.Close()` in E2E tests: `defer updateResp.Body.Close()` registered at call site, then `ReadBody(t, updateResp)` closes the body again internally. Closing an `io.ReadCloser` twice is undefined behavior and may trigger the race detector. Affects `agents_test.go`, `patterns_test.go`, `skills_test.go`, `skillfiles_test.go`. | Remove the outer `defer resp.Body.Close()` on call sites where `ReadBody` is used. |

### MEDIUM Priority

| ID | Source | Finding | Resolution |
| -- | ------ | ------- | ---------- |
| M1 | code-reviewer | `FieldError.code` enum in `mnemonic-v1.yaml` (~line 207) is out of sync with codes emitted by handlers. Enum lists `TOO_LONG`, `TOO_SHORT`, `CONTENT_TOO_LARGE`, `NOT_UNIQUE` — none of which are emitted. Missing: `MAX_LENGTH`, `MAX_ITEMS`, `MAX_SIZE`, `TOO_LARGE`. | Update the `FieldError.code` enum to match codes actually emitted. Remove phantom codes; add missing ones. |
| M2 | go-software-engineer | E2E test comments promise business invariant verification via a subsequent GET, but no subsequent GET was added. Affected tests: `TestUpdateAgent_HappyPathReturns204`, `TestUpdatePattern_UpdatedAtChangesCreatedAtPreserved`, `TestUpdatePattern_FullReplacementResetsOmittedFields`, `TestUpdateSkillFile_Success`. The 204 change removed the only end-to-end verification of these invariants. | Add a GET after each PUT in the affected tests and assert the invariant (e.g., `updated_at` changed, `created_at` preserved, fields reset). |
| M3 | go-software-engineer | Unit test `TestUpdate_CorruptDefinition` in `agents_test.go` (~line 531) now asserts 204 but the name gives no hint of the behavioral change. A reader auditing the test suite will be confused. | Rename to clarify current expected behavior, or add a comment explaining why a corrupt definition returns 204. |

### LOW Priority

| ID | Source | Finding | Resolution |
| -- | ------ | ------- | ---------- |
| L1 | code-reviewer | `patterns.go` Search handler error field named `"query"` but the query param is `"q"` (`c.Query("q")` on ~line 877). `FieldError.Field` should match the parameter name. | Change `Field: "query"` to `Field: "q"`. |
| L2 | code-reviewer | `skills.go` List handler reads the `tags` query param twice — once discarded with a comment, once used for in-memory filtering. The blank assignment serves no purpose and the comment is misleading. | Remove the blank `_ = c.Query("tags")` assignment and its comment. |
| L3 | go-software-engineer | `@Produce json` removed from PUT handler swaggo annotations, but `@Failure` annotations still imply JSON error responses. Swagger generators use `@Produce` to infer error response content type — without it, the generated spec may misrepresent error response format. | Add `@Produce json` back to PUT handlers (applies to error responses), or use `// @Produce` only on the success path with a separate content-type annotation for errors. Simplest fix: restore `// @Produce json`. |
| L4 | go-software-engineer | `ReadBody` return value silently discarded on 204 call sites. Intent (drain body for connection reuse) is unclear from the call. | Either add a `DrainBody(t, resp)` helper that documents intent, or assert `len(body) == 0` at 204 call sites. |

## Patterns to Document

1. **String length validation convention**: Use `utf8.RuneCountInString()` for all character-count limits; `len()` only when a byte limit is intentional. Add to handler design doc.
2. **Error code vocabulary**: Define a single authoritative list of `FieldError` codes in `docs/design/error-codes.md` to prevent OpenAPI enum drift after each handler change.

## Notes for Future Phases

**Pre-existing**: `POST /patterns` returns 202 Accepted rather than 201 Created. Not introduced by this branch — worth a separate review pass.
