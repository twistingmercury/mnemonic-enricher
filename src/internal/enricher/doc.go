// Package enricher provides background goroutine management for asynchronous
// processing tasks within the Mnemonic server.
//
// The primary component is the Worker, which polls for pending enrichment jobs,
// claims them, and processes them via the EnrichmentService. It also runs a
// maintenance loop that reclaims stale jobs and cleans up old completed/failed
// jobs.
//
// The worker is designed for in-process operation within the Mnemonic server
// binary. All goroutine lifecycle is managed through context cancellation and
// errgroup coordination, ensuring graceful shutdown.
//
// Documentation:
//   - Design: docs/design/pattern-processing.md (Enrichment Worker Deployment)
//   - Design: docs/design/service-layer.md (Enrichment Service Interface Usage)
//   - Config: docs/design/configuration.md (EnrichmentConfig fields)
package enricher
