# Code Review: Vocabulary Config (Cycles 7–12)

**Review Date:** 2026-03-10
**Reviewers:** code-reviewer, solutions-architect, go-software-engineer
**Phase:** feature/dynamic-lang-and-domain — cycles 7–12

## Files Reviewed

### Source Files

- `src/mnemonic/internal/config/config.go` — VocabularyConfig struct, validate(), SetDefaults wiring
- `src/mnemonic/internal/config/defaults.go` — DefaultVocabularyLanguages, DefaultVocabularyDomains
- `src/mnemonic/internal/handlers/patterns/patterns.go` — Handler carries vocab, containsString, validatePatternFields promoted to method
- `src/mnemonic/internal/server/routes.go` — RegisterAPIRoutes accepts vocab param
- `src/mnemonic/internal/server/server.go` — Passes cfg.Vocabulary through to RegisterAPIRoutes
- `src/mnemonic/config.yaml` — Shipped vocabulary config file
- `docs/architecture/00-architectural-decisions.md` — ADR-005 superseded, ADR-007 written (in this review)

### Test Files

- `src/mnemonic/internal/config/config_test.go` — VocabularyConfig validation tests, applyTestVocabulary helper
- `src/mnemonic/internal/handlers/patterns/patterns_test.go` — INVALID_VALUE unit tests, testVocab
- `src/mnemonic/tests/e2e/agents_test.go` — 200→204 fixes, domain "agnostic"→"backend" fix
- `src/mnemonic/tests/e2e/patterns_test.go` — 200→204 fixes, test function renames

## Validation Results

| Tool                     | Result                              |
| ------------------------ | ----------------------------------- |
| `go build ./...`         | PASS (confirmed by cycle 12 commit) |
| `go vet ./...`           | PASS (confirmed by cycle 12 commit) |
| `go test ./internal/...` | PASS (all cycles pass)              |
| `bash tests/run-e2e.sh`  | PASS (cycle 12: all tests pass)     |

## Prior Findings Status

| Prior ID | Status       | Notes                                                                                                                                                   |
| -------- | ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| H1       | RESOLVED     | `utf8.RuneCountInString` now used for language and domain max-length checks                                                                             |
| H2       | OPEN         | Double `Body.Close()` in E2E tests: `defer resp.Body.Close()` + `ReadBody`/`ParseJSON` both close — persists in `patterns_test.go` and `agents_test.go` |
| M1       | ESCALATED    | `INVALID_VALUE` is now emitted but still absent from the OpenAPI spec `FieldError.code` field                                                           |
| M2       | OPEN         | No GET-after-PUT to verify invariants in E2E tests; tests now only assert 204 and drain body                                                            |
| L1       | OPEN         | `Field: "query"` vs param `"q"` in Search handler                                                                                                       |
| L2       | NOT IN SCOPE | skills.go double tags read — not changed in this batch                                                                                                  |
| L3       | NOT IN SCOPE | `@Produce json` on PUT annotations — not changed in this batch                                                                                          |
| L4       | NOT IN SCOPE | ReadBody return discarded — not changed in this batch                                                                                                   |

## Design Compliance

Implementation satisfies the vocabulary PRD behavioral requirements.

### Behavioral Requirements Verified

- Config-driven allow-lists for `language` and `domain` accepted from `config.yaml` or env vars ✓
- `INVALID_VALUE` (400) returned for values that are valid kebab-case but not in the vocabulary ✓
- Vocabulary check is skipped when allow-list is empty (open-vocabulary mode preserved at runtime) ✓
- Defaults ship with 38 languages and 10 domains in `defaults.go` ✓
- Server fails to start if vocabulary lists are empty in config ✓

### Design Doc Divergences (Post-Review)

ADR-005 stated "open vocabulary; format validation only; vocabulary governed externally." Cycles 7–12 reverse that decision. ADR-007 has been written in this review to record the reversal.

#### Documents Updated

| Document                                             | Scope                                         | Status                        |
| ---------------------------------------------------- | --------------------------------------------- | ----------------------------- |
| `docs/architecture/00-architectural-decisions.md`    | ADR-005 marked Superseded; ADR-007 added      | Done                          |
| `docs/architecture/review-cycles-7-12-vocabulary.md` | Full architectural analysis of this changeset | Done (created in this review) |

## Findings

### HIGH Priority

| ID  | Source           | Finding                                                                                                                                                                                                                                                                                                                                                                          | Resolution                                                                           |
| --- | ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| H1  | all three agents | `config.yaml` ships missing 6 languages (`bash`, `json`, `markdown`, `toml`, `yaml`, `zig`) and 2 domains (`data-access`, `security`) relative to `defaults.go`. Viper applies file values on top of defaults, so any deployment using `config.yaml` silently loses those entries. A pattern with `language: yaml` returns 400 INVALID_VALUE on a server using this config file. | **RESOLVED** — `config.yaml` removed. Vocabulary served entirely from `defaults.go`. |

### MEDIUM Priority

| ID  | Source                              | Finding                                                                                                                                                                                                                                                                                                                                                                                 | Resolution                                                                                                                                                                                                                                         |
| --- | ----------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | solutions-architect                 | Startup `VocabularyConfig.validate()` rejects empty lists (server will not start). Runtime `validatePatternFields` skips enforcement when the list is empty (`len(h.allowedLanguages) > 0`). These invariants contradict: open vocabulary is unreachable via normal config loading. An operator who wants an open vocabulary has no supported path.                                     | **RESOLVED** — removed `len > 0` guards from `validatePatternFields`. Defaults always populate the lists so empty vocab is unreachable. `containsString` also replaced with `slices.Contains` and the helper deleted.                              |
| M2  | go-software-engineer                | `New()` assigns `vocab.Languages` and `vocab.Domains` directly to Handler fields without copying. Handler and caller share the underlying array. If either slice is later appended to, `slices.Contains` silently reads mutated state. The package-level `testVocab` var in `patterns_test.go` sets up this precondition for tests.                                                     | **RESOLVED** — `New()` now allocates fresh slices via `make`/`copy` before storing. |
| M3  | code-reviewer, go-software-engineer | INVALID_VALUE unit tests (`TestPatternCreate_InvalidLanguageValue`, `TestPatternCreate_InvalidDomainValue`, `TestPatternUpdate_InvalidLanguageValue`, `TestPatternUpdate_InvalidDomainValue`) assert `http.StatusBadRequest` only. A regression emitting `INVALID_FORMAT` instead of `INVALID_VALUE`, or using `"lang"` instead of `"language"` as the field name, would not be caught. | **RESOLVED** — all four tests now assert `field` and `code` from the response body. |
| M4  | code-reviewer                       | `INVALID_VALUE` is now emitted by the handler but the OpenAPI spec `FieldError.code` field has no enum (prior finding M1). `INVALID_VALUE` is not documented anywhere in the spec.                                                                                                                                                                                                      | **RESOLVED** — `enums` tag added to `FieldError.Code` in `respond.go`; swagger regenerated via `make docs-swagger`. |
| M5  | code-reviewer                       | E2E double-close persists (prior H2). `defer resp.Body.Close()` is registered at call sites where `ReadBody` or `ParseJSON` also closes the body. Confirmed in `patterns_test.go` lines 52, 56 and throughout `agents_test.go`.                                                                                                                                                         | **RESOLVED** — 55 double-close sites removed across `agents_test.go`, `patterns_test.go`, and `skillfiles_test.go`. |

### LOW Priority

| ID  | Source                              | Finding                                                                                                                                                                                                                                                                                                                                                                                        | Resolution                                                                                                                                                                                                |
| --- | ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| L1  | go-software-engineer                | `DefaultVocabularyLanguages` and `DefaultVocabularyDomains` are package-level `var` slices. Callers can mutate the global (e.g. `DefaultVocabularyLanguages[0] = "evil"`). In practice Viper copies on `SetDefault`, but tests that modify either slice corrupt subsequent tests in the same package.                                                                                          | **RESOLVED** — converted to functions `DefaultVocabularyLanguages()` and `DefaultVocabularyDomains()` returning a fresh slice each call. Call sites in `SetDefaults` updated. |
| L2  | code-reviewer, go-software-engineer | `testVocab` in `patterns_test.go` includes `"dotnet"` and `"react"`, which are not in `defaults.go`. These are fictional vocabulary entries. The INVALID_VALUE tests use `"brainfuck"` as the invalid value — they pass only because "brainfuck" is also absent from `testVocab`. If a future test uses "dotnet" as a valid language, it will pass against `testVocab` but fail in production. | **RESOLVED** — replaced `"dotnet"` with `"csharp"` and `"react"` with `"javascript"`. |
| L3  | solutions-architect                 | `handlers/patterns` imports `internal/config` to receive `config.VocabularyConfig` in `New()`. No import cycle exists and the pattern is already established in other service packages. The coupling is narrow (two fields).                                                                                                                                                                   | Won't fix — no import cycle, pattern established in codebase. Optional decoupling deferred. |
| L4  | code-reviewer                       | Search handler error uses `Field: "query"` but the actual query parameter is `"q"` (prior finding L1, still open).                                                                                                                                                                                                                                                                             | **RESOLVED** — changed to `Field: "q"` in `patterns.go`. |

## Patterns to Document

1. **`applyTestVocabulary` pattern**: When adding a required config section, provide a package-level viper helper in the config test file that injects minimal valid values. Prevents required-field validation from breaking every unrelated config test. Generalizes to any future required section.
2. **Receiver-owned vocab at construction**: Config slices injected into handlers should be copied at `New()` time, not aliased. The caller's config object and the handler must not share mutable state.

## Notes for Future Phases

**Pre-existing (from prior review):** `POST /patterns` returns 202 Accepted rather than 201 Created. Not introduced by this branch — worth a separate review pass.

**Vocabulary validation at startup**: `VocabularyConfig.validate()` could be extended to reject individual entries that do not match `^[a-z][a-z0-9-]*$`. This surfaces operator misconfiguration at startup (e.g. `"Go Language"` in `config.yaml`) rather than silently loading an unreachable entry.
