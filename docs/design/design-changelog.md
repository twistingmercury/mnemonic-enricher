# Mnemonic Design Change Log

This document tracks changes to design documents when implementation differs from original design. Each entry explains what changed and why.

> **Architecture Reference:** [Observability Architecture](../architecture/07-observability-architecture.md) | [Deployment Architecture](../architecture/06-deployment-architecture.md)

## Format

Each entry includes:

- **Date**: When the change was made
- **Design Document**: Which document was updated
- **Section**: Which section changed
- **Original Design**: What the design originally specified
- **Implementation**: What was actually built
- **Justification**: Why the implementation differs

## Change Log

### 2026-02-15: Architectural Pivot

- **Removed:** Routing engine, routing rules, ACE CLI
- **Added:** MCP server for Claude Code integration, skills storage, Admin REST API
- **Changed:** Mnemonic's focus from agent routing to team knowledge graph + tooling synchronization
- **See:** [Pivot Proposal](../plans/2026-02-14-mnemonic-pivot-knowledge-sync.md), [ADR-008](../architecture/00-architectural-decisions.md)

### 2026-01-28: Phase 3 Observability Implementation

**Design Document**: `observability-implementation.md`

#### 1. Configuration Structure

- **Original**: Standalone `ObservabilityConfig` struct with helper functions
- **Implementation**: Integrated into `MnemonicConfig` with nested `Observability` section
- **Justification**: Phase 2 established unified configuration pattern. Observability config follows same pattern for consistency.

#### 2. Telemetry Struct Fields

- **Original**: `Telemetry` wraps `*otelx.Telemetry` with `cfg config.ObservabilityConfig`
- **Implementation**: `Telemetry` has `otel *otelx.Telemetry` and `logger zerolog.Logger`, no cfg field
- **Justification**: Logger cached for direct access. Config not stored since it's only needed during initialization.

#### 3. Telemetry Methods

- **Original**: Only `Shutdown()` method documented
- **Implementation**: Added `Logger()`, `Tracer()`, `Meter()`, `TracerProvider()`, `MeterProvider()`, `Otelx()`
- **Justification**: Practical usage requires accessing these components. Methods provide clean encapsulation.

#### 4. Server Lifecycle Ownership

- **Original**: `main.go` initializes telemetry, passes to `server.ListenAndServe(tel)`
- **Implementation**: Server package owns telemetry initialization internally
- **Justification**: Phase 2 established server owning config loading. Telemetry follows same ownership pattern.

#### 5. Middleware Import Aliases

- **Original**: `otelgin` for otelx middleware
- **Implementation**: `otelxgin` for otelx middleware (distinguishes from contrib otelgin)
- **Justification**: Clarity - two different packages with similar names need distinct aliases.

#### 6. Middleware Option Signatures

- **Original**: `otelgin.WithSkipPaths([]string{"/health"})`
- **Implementation**: `otelxgin.WithSkipPaths("/health", "/metrics")`
- **Justification**: otelx library uses variadic parameters, not slices. Additional paths for ops endpoints.

#### 7. Tracing Middleware Functions

- **Original**: Single `TracingMiddleware()` function
- **Implementation**: Two functions: `TracingMiddleware()` and `TracingMiddlewareWithSkipPaths()`
- **Justification**: Flexibility - default skip paths for common case, configurable for special cases.

#### 8. Skip Path Implementation

- **Original**: Loop-based path checking
- **Implementation**: Uses `slices.Contains()` for cleaner code
- **Justification**: Standard library function is more idiomatic and readable.

#### 9. Handler Dependencies (Deferred)

- **Original**: `internal/handlers/deps.go` with Dependencies struct
- **Implementation**: Not implemented in Phase 3
- **Justification**: Handler instrumentation is Phase 1D per design checklist. Will be implemented when handlers need metrics/tracing.

#### 10. Database Instrumentation (Deferred)

- **Original**: `internal/repository/postgres/instrumented.go` and `neo4j/instrumented.go`
- **Implementation**: Not implemented in Phase 3
- **Justification**: Database instrumentation is Phase 1E per design checklist. Will be implemented with repository layer.

### 2026-01-28: Architectural Fixes

**Design Document**: `observability-implementation.md`

#### 11. MetricsRegistry Location

- **Original**: Not specified in design
- **Initial Implementation**: Package-level variable in server.go
- **Final Implementation**: Field in Telemetry struct, accessed via tel.MetricsRegistry()
- **Justification**: Server package is for HTTP lifecycle, not dependency management. Telemetry owns observability concerns.

#### 12. Version Package Location

- **Original**: cmd/version (build metadata in command layer)
- **Implementation**: Created internal/version, cmd/version delegates to it
- **Justification**: Internal packages should not import from cmd packages. Version info is build metadata needed by internal packages (telemetry, handlers).

### 2026-02-10: Phase 12a Pattern Matcher Design Correction

**Design Document**: `routing-engine.md` (archived to `docs/archive/routing-engine.md.archive`)

#### 1. Cosine Similarity Delegation

- **Original**: PatternMatcher computes cosine similarity in Go using per-pattern embedding retrieval via `PatternStore.GetEmbedding`
- **Implementation**: Cosine similarity delegated to pgvector via `<=>` operator; PatternStore uses `FindSimilarByIDs` for a single filtered query
- **Justification**: pgvector's optimized C implementation is more efficient than Go-side vector arithmetic. Eliminates per-pattern round-trips. Aligns with existing `pattern.Repository.FindSimilar` infrastructure.

#### 2. Vector Type Clarification

- **Original**: Design doc discussed `[]float32` vs `[]float64` as a trade-off decision
- **Implementation**: `[]float32` for embeddings (dictated by pgvector storage format); `float64` for similarity scores (returned by PostgreSQL)
- **Justification**: pgvector stores vectors as `float32`. This is not a design choice — it is a constraint of the backing infrastructure.

#### 3. SimilarityOptions Extension

- **Original**: `SimilarityOptions` had `MinSimilarity`, `MaxResults`, `Tags` only
- **Implementation**: Added `PatternIDs []uuid.UUID` field for filtering similarity search to specific pattern IDs
- **Justification**: PatternMatcher needs to restrict similarity search to pattern IDs referenced in routing rules. Extends existing options-bag pattern for backward compatibility.

**Design Document**: `data-storage.md`

#### 4. SimilarityOptions PatternIDs Field

- **Original**: `SimilarityOptions` struct had three fields: `MinSimilarity`, `MaxResults`, `Tags`
- **Implementation**: Added `PatternIDs []uuid.UUID` field
- **Justification**: Phase 12 PatternMatcher requires filtering FindSimilar results to specific pattern IDs referenced in routing rules. Zero value (nil) preserves existing behavior.

---

Copyright (c) 2026 Jeremy K. Johnson. All rights reserved.
