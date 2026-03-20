// Package enrichmentjob provides the PostgreSQL repository implementation for
// enrichment job lifecycle management. It supports job creation, atomic claim
// via FOR UPDATE SKIP LOCKED, state transitions (pending, processing, completed,
// failed), retry scheduling, stale job reclamation, and cleanup operations.
//
// Documentation:
//   - Architecture: docs/architecture/08-data-architecture.md (Data Model Design > Enrichment Jobs, Consistency and Integrity, Data Lifecycle Management)
//   - Design: docs/design/data-storage.md (Repository Interfaces > EnrichmentJob Repository)
//   - Design: docs/design/pattern-processing.md (Enrichment Pipeline, Enrichment Worker Deployment)
package enrichmentjob
