# Architecture

[Back to Project README](../../README.md)

Start here:

1. [What is Mnemonic?](../mnemonic-concept.md) — the problem, the vision, how it fits together
2. [Requirements](../mnemonic-requirements.md) — what Mnemonic must do
3. The architecture docs below — how those requirements are realized

## Architecture Documents

| Document                                                       | Description                                    |
| -------------------------------------------------------------- | ---------------------------------------------- |
| [Architectural Decisions](00-architectural-decisions.md)       | Key decision records (ADR-001 through ADR-004) |
| [Security Architecture](01-security-architecture.md)           | Authentication and authorization (Post-MVP)    |
| [System Architecture](02-system-architecture.md)               | Component breakdown and data flow              |
| [Communication Patterns](03-communication-patterns.md)         | MCP and REST protocol patterns                 |
| [Data Architecture](04-data-architecture.md)                   | Database schemas and data management           |
| [Database Integration Flow](05-database-integration-flow.md)   | End-to-end database interaction patterns       |
| [Deployment Architecture](06-deployment-architecture.md)       | Deployment topology and operations             |
| [Observability Architecture](07-observability-architecture.md) | Monitoring, logging, and tracing               |
| [MCP Tools](08-mcp-tools.md)                                  | Tool discovery, definitions, and response patterns |

## Design Documents

Architecture docs describe **what** and **why**. Design docs (in `docs/design/`) describe **how**.

| Document                                                                   | Description                            | Status  |
| -------------------------------------------------------------------------- | -------------------------------------- | ------- |
| [Pivot API Specification](../design/2026-02-15-pivot-api-specification.md) | Admin REST + MCP Server specification  | Current |
| [Data Storage](../design/data-storage.md)                                  | Data storage architecture              | Current |
| [Pattern Processing](../design/pattern-processing.md)                      | Pattern enrichment and search pipeline | Current |
| [Configuration](../design/configuration.md)                                | Server configuration                   | Current |
| [Observability Implementation](../design/observability-implementation.md)  | Observability design                   | Current |
