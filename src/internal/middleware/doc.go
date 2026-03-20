// Package middleware provides Gin middleware for HTTP request metrics and
// distributed tracing. It instruments incoming requests with OpenTelemetry
// spans and records request count, duration, and in-flight metrics with
// configurable path exclusions.
//
// Documentation:
//   - Architecture: docs/architecture/07-observability-architecture.md (Metrics (Prometheus) > Application Metrics)
//   - Architecture: docs/architecture/07-observability-architecture.md (Distributed Tracing (Jaeger) > Trace Structure)
//   - Design: docs/design/observability-implementation.md (Request Metrics Middleware, Tracing Middleware)
package middleware
