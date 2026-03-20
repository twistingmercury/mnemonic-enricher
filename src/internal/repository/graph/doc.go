// Package graph provides the Neo4j repository implementation for knowledge
// graph operations. It manages agent and pattern node synchronization, concept
// extraction via MENTIONED_IN relationships, agent relevance scoring via
// RELEVANT_FOR relationships, and graph-based discovery queries.
//
// Documentation:
//   - Architecture: docs/architecture/08-data-architecture.md (Neo4j Graph Model, Data Flow Patterns, Data Lifecycle Management)
//   - Architecture: docs/architecture/07-observability-architecture.md (SLOs (Service Level Objectives))
//   - Design: docs/design/data-storage.md (Repository Interfaces > GraphRepository)
package graph
