# Architectural Review: Pivot Implementation (Phases 9-13)

[Back to Overview](README.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Scope](#scope)
- [Files Reviewed](#files-reviewed)
- [Design Docs Referenced](#design-docs-referenced)
- [Findings Summary](#findings-summary)
- [Detailed Findings](#detailed-findings)
  - [F-01: Handler imports repository types directly](#f-01-handler-imports-repository-types-directly)
  - [F-02: Agent handler silently discards JSON unmarshal errors](#f-02-agent-handler-silently-discards-json-unmarshal-errors)
  - [F-03: Pattern Get returns agent UUIDs instead of agent names](#f-03-pattern-get-returns-agent-uuids-instead-of-agent-names)
  - [F-04: SetAgentAssociations response bypasses service read-back](#f-04-setagentassociations-response-bypasses-service-read-back)
  - [F-05: Config structure diverges from design doc](#f-05-config-structure-diverges-from-design-doc)
  - [F-06: MCP port conflict validation is incomplete](#f-06-mcp-port-conflict-validation-is-incomplete)
  - [F-07: Enrichment worker shutdown is not sequenced](#f-07-enrichment-worker-shutdown-is-not-sequenced)
  - [F-08: MCP server shutdown uses a hardcoded timeout](#f-08-mcp-server-shutdown-uses-a-hardcoded-timeout)
  - [F-09: No health check on MCP listener](#f-09-no-health-check-on-mcp-listener)
- [Positive Observations](#positive-observations)
- [Recommendations](#recommendations)

## Scope

Review of 12 files changed on `feature/enrichment` vs `develop` implementing Phases 9-13: MCP server, REST handlers, enrichment worker, and server lifecycle wiring. The review evaluates separation of concerns, DI wiring, concurrency model, design doc compliance, error propagation, and coupling.

## Files Reviewed

| File                                                  | Role                                  |
| ----------------------------------------------------- | ------------------------------------- |
| `src/mnemonic/internal/config/config.go`              | Config structs, validation, loading   |
| `src/mnemonic/internal/config/defaults.go`            | Default constants                     |
| `src/mnemonic/internal/handlers/agents/agents.go`     | Agent CRUD handler                    |
| `src/mnemonic/internal/handlers/patterns/patterns.go` | Pattern CRUD + search handler         |
| `src/mnemonic/internal/handlers/respond.go`           | Shared error mapping, pagination      |
| `src/mnemonic/internal/server/server.go`              | DI hub, errgroup orchestration        |
| `src/mnemonic/internal/server/routes.go`              | Handler wiring and route registration |
| `src/mnemonic/internal/enricher/enrichment.go`        | Enrichment worker lifecycle           |
| `src/mnemonic/internal/mcpserver/server.go`           | MCP server setup                      |
| `src/mnemonic/internal/mcpserver/deps.go`             | ToolDependencies facade               |
| `src/mnemonic/internal/service/errors.go`             | Service sentinel errors               |

## Design Docs Referenced

| Document                                      | Purpose                                                           |
| --------------------------------------------- | ----------------------------------------------------------------- |
| `docs/design/service-layer.md`                | Service interfaces, error mapping, transaction boundaries, wiring |
| `docs/design/configuration.md`                | Config structure, env vars, validation                            |
| `docs/architecture/02-system-architecture.md` | Component breakdown, data flow                                    |

## Findings Summary

| ID   | Severity | Category          | Title                                                    |
| ---- | -------- | ----------------- | -------------------------------------------------------- |
| F-01 | MEDIUM   | Coupling          | Handler imports repository types directly                |
| F-02 | MEDIUM   | Error handling    | Agent handler silently discards JSON unmarshal errors    |
| F-03 | MEDIUM   | Data integrity    | Pattern Get returns agent UUIDs instead of agent names   |
| F-04 | LOW      | Data consistency  | SetAgentAssociations response bypasses service read-back |
| F-05 | MEDIUM   | Design compliance | Config structure diverges from design doc                |
| F-06 | LOW      | Validation        | MCP port conflict validation is incomplete               |
| F-07 | MEDIUM   | Concurrency       | Enrichment worker shutdown is not sequenced              |
| F-08 | LOW      | Configuration     | MCP server shutdown uses a hardcoded timeout             |
| F-09 | LOW      | Observability     | No health check on MCP listener                          |

## Detailed Findings

### F-01: Handler imports repository types directly

**Severity:** MEDIUM
**Category:** Coupling
**Files:** `internal/handlers/agents/agents.go` (line 18), `internal/handlers/patterns/patterns.go` (lines 18-19)

**Description:**

Both handler packages import repository packages and reference their types directly:

```go
// agents/agents.go
import agentrepo "github.com/twistingmercury/mnemonic-enricher/internal/repository/agent"

func toAgentResponse(a *agentrepo.Agent) agentResponse { ... }
```

```go
// patterns/patterns.go
import patternrepo "github.com/twistingmercury/mnemonic-enricher/internal/repository/pattern"

func toPatternResponse(p *patternrepo.Pattern, ...) patternResponse { ... }
func toPatternSummary(p *patternrepo.Pattern) patternSummaryResponse { ... }
```

The service-layer.md design doc states: "Service methods accept and return domain types, not HTTP request/response types." The service interfaces already return `*patternrepo.Pattern` and `*agentrepo.Agent`, which means the handler must know about the repository type to consume them. This is an accepted trade-off for MVP, but it creates a transitive dependency: handler -> repository package.

**Impact:** If repository types change (field renames, struct refactors), handlers break even though the service layer should be the only interface boundary. The `patternrepo.Filter` usage in the patterns handler (line 279) is especially concerning because it means the handler constructs repository-level query objects, bypassing service-layer abstraction.

**Recommendation:** Acceptable for MVP. Post-MVP, service methods should return service-layer DTOs instead of repository structs. For immediate action, move `patternrepo.Filter` construction into the service layer by accepting primitive filter parameters in `patternsvc.ListOptions`.

---

### F-02: Agent handler silently discards JSON unmarshal errors

**Severity:** MEDIUM
**Category:** Error handling
**Files:** `internal/handlers/agents/agents.go` (line 91)

**Description:**

The `toAgentResponse` converter calls `json.Unmarshal(a.Definition, &def)` and discards the error:

```go
_ = json.Unmarshal(a.Definition, &def)
```

If the JSONB `definition` column contains malformed JSON, the handler returns a response with zero-value fields (empty strings, nil slices) rather than signaling the problem. This violates the error handling principle from the service-layer.md design: errors should propagate clearly.

**Impact:** A corrupted definition row produces a 200 response with empty data. The caller has no way to distinguish "agent has no description" from "data corruption."

**Recommendation:** Return an internal server error if `json.Unmarshal` fails. The service layer should validate definition integrity before returning the struct, or the handler should treat unmarshal failure as a 500.

---

### F-03: Pattern Get returns agent UUIDs instead of agent names

**Severity:** MEDIUM
**Category:** Data integrity
**Files:** `internal/handlers/patterns/patterns.go` (lines 348-361)

**Description:**

The `Get` handler fetches agent associations via `h.patternSvc.GetAgentAssociations`, which returns `[]patternrepo.AgentAssociation` containing `AgentID` (UUID). The handler maps `AgentID.String()` into the `agent_name` response field:

```go
assocs[i] = associationResponse{
    AgentName: a.AgentID.String(),  // UUID, not a name
    Relevance: a.Relevance,
}
```

The code includes a TODO acknowledging this gap. The OpenAPI schema and the design doc both specify that associations use agent names. This creates an API contract violation where clients receive UUIDs in a field labeled `agent_name`.

**Impact:** API consumers that parse `agent_name` as a human-readable name will receive UUIDs. This breaks the contract before any client code is written against the API.

**Recommendation:** Add a service method (e.g., `GetAgentAssociationsResolved`) that returns agent names instead of IDs. The service layer already has access to `agentRepo` and can resolve `agent_id -> name` in a single batch query. This should be addressed before the first external API test.

---

### F-04: SetAgentAssociations response bypasses service read-back

**Severity:** LOW
**Category:** Data consistency
**Files:** `internal/handlers/patterns/patterns.go` (lines 471-484)

**Description:**

After calling `h.patternSvc.SetAgentAssociations`, the handler builds the response from the request body rather than reading back from the service:

```go
assocs := make([]associationResponse, len(req.Associations))
for i, a := range req.Associations {
    relevance := a.Relevance
    if relevance == 0 {
        relevance = 1.0
    }
    assocs[i] = associationResponse{
        AgentName: a.AgentName,
        Relevance: relevance,
    }
}
```

If the service layer modifies the data (e.g., normalizes relevance, resolves names, or silently drops invalid agents), the response will not reflect the actual persisted state.

**Impact:** Low for MVP since the service layer currently persists exactly what it receives. Becomes a bug if post-processing is added.

**Recommendation:** Read back from the service after the write, consistent with how Create and Update work.

---

### F-05: Config structure diverges from design doc

**Severity:** MEDIUM
**Category:** Design compliance
**Files:** `internal/config/config.go` (lines 14-23, 26-31, 34-42)

**Description:**

The implemented config structure differs from `configuration.md` in two ways:

1. **Flat server/MCP split.** The design doc defines `server.admin.{host,port,...}` and `server.mcp.{host,port,...}` as nested siblings under `ServerConfigs`. The implementation uses a flat structure: `ServerConfig` (for admin) at `server.{host,port,...}` and `MCPConfig` at `mcp.{port,...}`.

2. **MCPConfig lacks Host and ShutdownTimeout.** The design doc gives the MCP listener its own `host` and `shutdown_timeout`. The implementation omits both: `MCPConfig.Address(host string)` takes the host from the admin server config, and shutdown uses a hardcoded 5-second constant.

Corresponding environment variables are affected. The design doc specifies `MNEMONIC_SERVER_ADMIN_PORT` and `MNEMONIC_SERVER_MCP_PORT`; the implementation uses `MNEMONIC_SERVER_PORT` and `MNEMONIC_MCP_PORT`.

**Impact:** Anyone configuring the server using the design doc's env var names will have no effect. The MCP server cannot bind to a different host than the admin server if the deployment topology requires it.

**Recommendation:** Either update the design doc to match the implementation (the simpler path since MVP runs a single process) or restructure the config to match the design doc. Pick one and document the decision. The flat structure is a reasonable simplification for MVP if documented.

---

### F-06: MCP port conflict validation is incomplete

**Severity:** LOW
**Category:** Validation
**Files:** `internal/config/config.go` (lines 401-413)

**Description:**

Port conflict validation checks:

- `server.port` vs `observability.metrics.port` (when metrics enabled)
- `server.port` vs `mcp.port`

Missing checks:

- `mcp.port` vs `observability.metrics.port`
- All three ports could be 9090 if `server.port=9090` is not the default

**Impact:** Low. Mis-configuration produces a clear bind error at startup. But a validation-time error is more helpful than a runtime bind failure.

**Recommendation:** Add the missing cross-port check between `mcp.port` and `observability.metrics.port`.

---

### F-07: Enrichment worker shutdown is not sequenced

**Severity:** MEDIUM
**Category:** Concurrency
**Files:** `internal/server/server.go` (lines 100-116), `internal/enricher/enrichment.go` (lines 42-69)

**Description:**

All three components (admin API, MCP server, enrichment worker) run in the same errgroup. When the context is cancelled (SIGINT/SIGTERM), all three receive cancellation simultaneously. The enrichment worker may be mid-job when context cancels.

The enrichment worker's `runWorker` loop checks `ctx.Done()` at the top of each iteration but not within `svc.ProcessJob`. If `ProcessJob` is in the middle of a multi-step pipeline (e.g., step 5: sync to Neo4j), context cancellation may cause a partial write.

The `enricher.Worker.Run` uses its own internal errgroup, and the goroutines always return nil (lines 48-51, 56-58). This means the outer errgroup in `server.go` never sees an error from the enrichment worker -- it always returns nil from `g.Wait()`.

**Impact:** On shutdown, in-flight enrichment jobs may be left in "processing" state. The maintenance loop will reclaim them eventually (stale job reclaim), but there is no graceful drain: the worker does not finish current jobs before shutting down.

**Recommendation:**

1. Distinguish between shutdown context (stop accepting new work) and cancellation context (abort current work). Use a two-phase shutdown: cancel the claim loop first, then give in-flight jobs a grace period before cancelling their context.
2. The inner errgroup always returning nil is correct behavior (worker goroutines should not crash the server), but add a comment explaining this design choice.

---

### F-08: MCP server shutdown uses a hardcoded timeout

**Severity:** LOW
**Category:** Configuration
**Files:** `internal/server/server.go` (lines 279-291)

**Description:**

The `runMCPServer` function uses `mcpShutdownTimeout = 5 * time.Second`, while the design doc (`configuration.md`) specifies `server.mcp.shutdown_timeout` as a configurable duration.

The admin server's shutdown timeout is correctly read from `cfg.Server.ShutdownTimeout`.

**Impact:** The MCP shutdown timeout is not configurable. If MCP sessions take longer than 5 seconds to drain, connections are forcibly closed.

**Recommendation:** Either make it configurable via `MCPConfig.ShutdownTimeout` (matching the design doc) or document the hardcoded value as a deliberate simplification.

---

### F-09: No health check on MCP listener

**Severity:** LOW
**Category:** Observability
**Files:** `internal/mcpserver/server.go` (lines 51-62)

**Description:**

The MCP HTTP server's mux only registers `/mcp`. There is no `/health` endpoint on port 8081. If a load balancer or orchestrator probes the MCP port for health, it will get a 404.

The admin API has health checks via the observability config (`/health` on port 8080).

**Impact:** In a deployment where the MCP port is probed independently (e.g., Kubernetes readiness on each port), the MCP listener appears unhealthy.

**Recommendation:** Add a lightweight `/health` handler to the MCP mux, or document that health checks should target the admin port only.

## Positive Observations

The following aspects of the implementation are well-executed:

**Clean DI wiring.** The `wireDependencies` function in `server.go` (lines 168-214) follows the exact wiring pattern prescribed by `service-layer.md`. Repository, service, and facade creation is ordered correctly with no circular dependencies. The function returns clean composite types (`Services`, `mcpserver.ToolDependencies`, `*enricher.Worker`) that separate REST, MCP, and worker concerns.

**Error propagation.** The centralized `handlers.RespondError` function in `respond.go` implements the exact error mapping table from `service-layer.md` with RFC 7807 Problem Details. All handler methods consistently delegate to this function. The sentinel error hierarchy (`service.ErrNotFound`, `ErrConflict`, `ErrInvalidInput`, `ErrServiceUnavailable`) is clean.

**ToolDependencies facade.** The `mcpserver.ToolDependencies` interface (deps.go) is a textbook thin facade: three methods, each mapping 1:1 to an MCP tool. The concrete `toolDeps` struct delegates to service interfaces with no added logic. This matches the design doc exactly.

**Errgroup pattern.** The three-goroutine errgroup in `server.go` is structurally correct. Each goroutine monitors both its own errors and context cancellation. The `select` pattern in `runHTTPServer` and `runMCPServer` handles both early server failure and graceful shutdown. The channel-based coordination avoids goroutine leaks.

**Enrichment worker.** The `enricher.Worker` implementation is well-structured. The `sleep` helper respects context cancellation. The maintenance goroutine runs on a separate ticker. The `ProcessJob` error semantics (nil = handled pipeline failure, non-nil = unrecoverable) match the design doc.

**Cursor pagination.** Cursor encoding/decoding in `respond.go` matches the design doc specification exactly. The handler-layer responsibility for cursor translation is correctly implemented, keeping the service layer cursor-unaware.

**Config validation.** The config validation is thorough: port ranges, duration positivity, TLS file existence, cross-port conflict detection, and Neo4j URI scheme validation. The `ValidationErrors` type provides clear multi-error reporting.

## Recommendations

Ranked by priority:

1. **F-03 (MEDIUM):** Resolve the UUID-as-name issue in pattern associations before external testing begins. This is an API contract violation.
2. **F-05 (MEDIUM):** Decide whether the flat config structure or the design doc's nested structure is canonical. Update whichever is wrong so they match.
3. **F-01 (MEDIUM):** Accept handler-to-repo coupling for MVP. Elevate `patternrepo.Filter` into the service layer as a near-term cleanup.
4. **F-02 (MEDIUM):** Stop silently discarding `json.Unmarshal` errors in `toAgentResponse`.
5. **F-07 (MEDIUM):** Add graceful drain for in-flight enrichment jobs on shutdown.
6. **F-04, F-06, F-08, F-09 (LOW):** Address in subsequent passes.

---

Copyright (c) 2025 Jeremy K. Johnson. All rights reserved.
