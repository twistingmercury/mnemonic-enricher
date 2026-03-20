# Architectural Review: Cycles 7-12 — VocabularyConfig

[Back to Overview](README.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Scope](#scope)
- [Files Reviewed](#files-reviewed)
- [Findings Summary](#findings-summary)
- [Detailed Findings](#detailed-findings)
  - [F-01: ADR-005 is superseded but not updated](#f-01-adr-005-is-superseded-but-not-updated)
  - [F-02: Handler imports config package directly](#f-02-handler-imports-config-package-directly)
  - [F-03: config.yaml and defaults.go are out of sync](#f-03-configyaml-and-defaultsgo-are-out-of-sync)
  - [F-04: Startup validation contradicts runtime bypass](#f-04-startup-validation-contradicts-runtime-bypass)
  - [F-05: Vocabulary validation belongs in the handler layer](#f-05-vocabulary-validation-belongs-in-the-handler-layer)
  - [F-06: containsString is O(n); no semantic validation of vocab entries](#f-06-containsstring-is-on-no-semantic-validation-of-vocab-entries)
  - [F-07: Asymmetry between patterns and other resource types](#f-07-asymmetry-between-patterns-and-other-resource-types)
  - [F-08: Config-driven vocabulary vs. database-driven vocabulary](#f-08-config-driven-vocabulary-vs-database-driven-vocabulary)
- [Positive Observations](#positive-observations)
- [Decision Required](#decision-required)

## Scope

Review of cycles 7-12 on `feature/dynamic-lang-and-domain`. These cycles introduce `VocabularyConfig` — configurable allow-lists for pattern `language` and `domain` fields loaded from `config.yaml` or environment variables at startup. The review evaluates design coherence, coupling, invariant consistency, and long-term maintainability.

## Files Reviewed

| File                                                  | Role                                                       |
| ----------------------------------------------------- | ---------------------------------------------------------- |
| `src/mnemonic/internal/config/config.go`              | VocabularyConfig struct, validation                        |
| `src/mnemonic/internal/config/defaults.go`            | DefaultVocabularyLanguages, DefaultVocabularyDomains       |
| `src/mnemonic/internal/handlers/patterns/patterns.go` | Handler constructor, validatePatternFields, containsString |
| `src/mnemonic/internal/server/routes.go`              | RegisterAPIRoutes wiring                                   |
| `src/mnemonic/internal/server/server.go`              | ListenAndServe, cfg.Vocabulary threading                   |
| `src/mnemonic/config.yaml`                            | Shipped vocabulary config                                  |
| `docs/architecture/00-architectural-decisions.md`     | ADR-005 (now superseded)                                   |

---

## Findings Summary

| ID   | Priority | Title                                                           |
| ---- | -------- | --------------------------------------------------------------- |
| F-01 | HIGH     | ADR-005 is superseded but not updated                           |
| F-02 | MEDIUM   | Handler imports config package directly                         |
| F-03 | HIGH     | config.yaml and defaults.go are out of sync                     |
| F-04 | MEDIUM   | Startup validation contradicts runtime bypass                   |
| F-05 | LOW      | Vocabulary validation belongs in the handler layer              |
| F-06 | LOW      | containsString is O(n); no semantic validation of vocab entries |
| F-07 | LOW      | Asymmetry between patterns and other resource types             |
| F-08 | LOW      | Config-driven vocabulary vs. database-driven vocabulary         |

---

## Detailed Findings

### F-01: ADR-005 is superseded but not updated

**Priority:** HIGH

**Description**

ADR-005 (2026-03-09, ACTIVE) states: "Remove enum enforcement from the API. Validate format only (kebab-case, max 64 characters). Vocabulary governance belongs to `mnemonic-patterns/config/validate.yaml`."

Cycles 7-12 reverse that decision. The handler now enforces vocabulary at the API layer via configurable allow-lists (`allowedLanguages`, `allowedDomains`). The governance boundary has moved from the sync tooling layer back into the Admin REST API.

ADR-005 has not been updated or superseded. The ADR log and the code now say opposite things, which is the most serious documentation debt in this changeset.

**Recommended Resolution**

Supersede ADR-005 with ADR-007. The new ADR should record:

- The context that prompted the reversal (operators need the API to enforce vocabulary; `mnemonic-patterns` is not always the only write path)
- The decision: config-driven allow-lists enforced at the handler layer, with an empty list meaning open vocabulary
- The consequences, including the startup validation / runtime bypass tension (see F-04)

Mark ADR-005 status as **Superseded by ADR-007**.

---

### F-02: Handler imports config package directly

**Priority:** MEDIUM

**Description**

`patterns.go` imports `github.com/twistingmercury/mnemonic-enricher/internal/config` to receive `config.VocabularyConfig` in its constructor:

```go
func New(patternSvc patternsvc.Service, searchSvc searchsvc.Service, vocab config.VocabularyConfig) *Handler {
```

The `config` package sits at the root of Mnemonic's internal dependency graph — it is the widest-reaching package. Handler packages importing `config` creates a fan-in that makes the handler harder to test in isolation (constructing a `config.VocabularyConfig` is fine today, but the type lives alongside the full config graph).

This is a mild concern, not a blocking one. The handler does not read any config field other than `vocab.Languages` and `vocab.Domains`, so the actual coupling is narrow even if the import line suggests otherwise.

**Recommended Resolution**

Define a thin interface or plain struct in the `handlers/patterns` package itself:

```go
// VocabularyConfig holds the allowed values for language and domain fields.
type VocabularyConfig struct {
    Languages []string
    Domains   []string
}
```

`RegisterAPIRoutes` converts `config.VocabularyConfig` to `patterns.VocabularyConfig` at the wiring point. The handler package then has no import dependency on `config`. This is a straightforward decoupling with no behavioural change.

---

### F-03: config.yaml and defaults.go are out of sync

**Priority:** HIGH

**Description**

`defaults.go` defines `DefaultVocabularyLanguages` with 38 entries including `bash`, `json`, `markdown`, `toml`, `yaml`, and `zig`. The shipped `config.yaml` lists 32 languages, omitting those six. `defaults.go` includes `data-access` and `security` in `DefaultVocabularyDomains`; `config.yaml` lists only 8 domains and omits both.

Because Viper applies file values on top of defaults, any operator who deploys with `config.yaml` gets the file's vocabulary, not the defaults. The 6 missing languages and 2 missing domains are silently dropped. An operator who discovers `bash` is rejected at runtime will find the defaults in Go source but not in the config file they were given as the deployment artifact.

This is an operator experience defect with a real blast radius: any existing patterns using those values will fail validation after an upgrade if config.yaml is the active config file.

**Recommended Resolution**

Synchronize `config.yaml` with `defaults.go`. The config file should either:

1. Include every entry present in the defaults (simplest — the file is the reference), or
2. Be removed from the repository entirely and rely on compiled defaults (operators override only what they need)

Option 1 is lower risk. Add the missing entries to `config.yaml` before merging this branch.

---

### F-04: Startup validation contradicts runtime bypass

**Priority:** MEDIUM

**Description**

`VocabularyConfig.validate()` rejects empty `Languages` and `Domains` at startup — the server will not start if either list is empty. However, `validatePatternFields` explicitly skips vocabulary enforcement when the list is empty:

```go
} else if len(h.allowedLanguages) > 0 && !containsString(h.allowedLanguages, language) {
```

The intent is documented: empty vocab = open vocabulary. But the startup validator prevents `allowedLanguages` from ever being empty via config, making the runtime bypass unreachable through normal operation.

These two invariants are in tension:

- Config says: a non-empty vocab is required.
- Handler says: if vocab is empty, any value passes.

An operator who wants open vocabulary has no supported path — they cannot set an empty list in the config without triggering a startup error.

**Recommended Resolution**

Pick one of two consistent positions:

1. **Closed vocabulary is always required.** Remove the `len > 0` guard from the handler. The startup validation already guarantees the list is populated. Document that "open vocabulary" is not a supported mode.

2. **Open vocabulary is a supported mode.** Remove the startup non-empty check from `VocabularyConfig.validate()`. An empty list is valid and means "accept any well-formed value." Update the config docs and shipped `config.yaml` accordingly.

Option 2 is more flexible and matches the original ADR-005 philosophy. Option 1 is simpler and matches the actual behaviour under the current shipped config.

---

### F-05: Vocabulary validation belongs in the handler layer

**Priority:** LOW

**Description**

Placing vocabulary enforcement in the HTTP handler is correct for this system. The reasons:

- Vocabulary is a presentation-layer constraint: it determines what values the API accepts, not what the service does with a value. The service layer has no opinion on whether `"bash"` is an approved language.
- The service layer and repository are already stable. Adding a vocabulary parameter to service method signatures would spread a config concern into the business logic layer.
- Handler-level validation returns consistent 400 error responses via the existing `FieldError` / `RespondValidationError` path.

**No action required.** This finding confirms the placement is appropriate.

---

### F-06: containsString is O(n); no semantic validation of vocab entries

**Priority:** LOW

**Description**

`containsString` does a linear scan. With 38 languages and 10 domains, worst-case is 48 comparisons per request — negligible. It would become a concern only if vocabulary lists grew into the hundreds, which is not a realistic scenario for language or domain identifiers.

A separate concern: individual vocabulary entries loaded from config are not validated for format. An operator who puts `"Go Language"` (with a space) in `config.yaml` will load a non-kebab-case entry. A pattern submitting `"go-language"` would correctly fail validation, but the configured entry itself would never match anything.

**Recommended Resolution**

No change needed on the O(n) lookup. Optionally, add a format check in `VocabularyConfig.validate()` that rejects any entry that does not match `^[a-z][a-z0-9-]*$`. This surfaces operator misconfiguration at startup rather than silently accepting an unreachable entry.

---

### F-07: Asymmetry between patterns and other resource types

**Priority:** LOW

**Description**

Only patterns carry `language` and `domain` fields. Agent, skill, and skillfile handlers do not use vocabulary. This asymmetry is justified: agents and skills are tooling artifacts, not content artifacts. Their identity fields (`name`) follow the same kebab-case constraint but do not require semantic classification by language or domain.

If future resource types carry language/domain fields, the vocabulary system will need to be extended to cover them. The current design does not prevent this — `VocabularyConfig` is passed through `RegisterAPIRoutes` and could be forwarded to any handler.

**No action required.** The asymmetry is intentional and well-scoped.

---

### F-08: Config-driven vocabulary vs. database-driven vocabulary

**Priority:** LOW

**Description**

Vocabulary as static config (file/env) is appropriate for this service. The primary write path is the sync tooling (`mnemonic-patterns`), which has its own validation layer. Vocabulary values change infrequently — on the order of adding a new language or problem domain, not per-request.

Database-driven vocabulary would add complexity (a new table, CRUD endpoints, cache invalidation) with no clear benefit at this stage. It would be appropriate only if:

- Multiple teams with different vocabularies share a single Mnemonic instance, or
- Operators need to add vocabulary values without a server restart

Neither scenario is in current scope.

**No action required.** Config-driven vocabulary is the right choice.

---

## Positive Observations

- The wiring is clean. `cfg.Vocabulary` flows from `ListenAndServe` through `RegisterAPIRoutes` to `patternhandler.New` — one straight line with no global state.
- `validatePatternFields` becoming a method on `Handler` is the correct Go idiom for injected configuration. The method signature is unchanged from the caller's perspective; the vocabulary check reads from receiver state.
- The `INVALID_VALUE` error code is distinct from `INVALID_FORMAT`, giving callers a clear signal about whether the value is structurally wrong or semantically out-of-vocabulary. This is useful for API consumers.
- Default vocabularies are defined in `defaults.go` as named variables, not inline. This makes them testable and overridable without touching business logic.
- The `len > 0` guard preserves backward compatibility for any deployment that somehow loads an empty vocabulary. Intent is clear from the comment.

---

## Decision Required

Before merging, three items need a decision from the team:

1. **F-01**: Write ADR-007 superseding ADR-005. The open-vocabulary model is no longer active policy.
2. **F-03**: Synchronize `config.yaml` with `defaults.go`. Decide whether `config.yaml` is the canonical vocabulary reference or whether compiled defaults are.
3. **F-04**: Choose one consistent invariant — either startup validation allows empty lists (open vocabulary supported) or the `len > 0` guard is removed (closed vocabulary always required).
