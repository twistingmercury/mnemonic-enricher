# Observability Implementation Design

[Back to Architecture Overview](../../architecture/README.md) | [Back to Project README](../../../README.md)

**Note:** This document reflects the post-pivot implementation following the architectural change from routing to knowledge graph + tooling sync (see [2026-02-14-mnemonic-pivot-knowledge-sync.md](../plans/2026-02-14-mnemonic-pivot-knowledge-sync.md)).

> **Pre-pivot code pending removal:** The following packages still exist in the codebase but are slated for removal. Do **not** instrument them:
>
> - `internal/routing/` (routing engine)
> - `internal/handlers/routes/` (route handlers)
> - `internal/repository/routingrule/` (routing rule repository)
> - `RoutingConfig` in `internal/config/config.go`

> **Note:** This document describes the target observability implementation. The current codebase has not been fully migrated to match this specification. Code will be updated to conform to this design during implementation phases.

## Table of Contents

- [Overview](#overview)
- [otelx Package Integration](#otelx-package-integration)
- [Initialization and Configuration](#initialization-and-configuration)
- [Distributed Tracing Implementation](#distributed-tracing-implementation)
- [Metrics Implementation](#metrics-implementation)
- [Enrichment Worker Observability](#enrichment-worker-observability)
- [Metrics Registry](#metrics-registry)
- [Structured Logging Implementation](#structured-logging-implementation)
- [Handler Instrumentation Patterns](#handler-instrumentation-patterns)
- [Database Instrumentation](#database-instrumentation)
- [Health Check Implementation](#health-check-implementation)
- [Gaps and Additional Implementation](#gaps-and-additional-implementation)
- [Implementation Checklist](#implementation-checklist)

## Overview

> **Architecture Reference:** [Observability Architecture](../../architecture/07-observability-architecture.md) | [Requirements - Quality Attributes](../mnemonic-requirements.md#quality-attributes)

This document provides the detailed Go implementation design for MVP observability in Mnemonic, as defined in the [Observability Architecture](../../architecture/07-observability-architecture.md).

**MVP Scope:**

- OpenTelemetry SDK integration via `otelx`
- Structured logging with trace correlation
- Metrics emission (counters, histograms, gauges)
- Distributed tracing with span creation
- OTLP export configuration

**Primary Package:** `github.com/twistingmercury/otelx`

The otelx package provides unified OpenTelemetry initialization with:

- Zerolog-based structured logging with automatic trace correlation
- Prometheus metrics exporter
- OTLP gRPC trace exporter
- Gin middleware for HTTP request instrumentation

### Current Mnemonic Architecture

```text
src/mnemonic/
├── cmd/
│   ├── main/main.go           # Application entrypoint
│   └── version/version.go     # Version information
└── internal/
    ├── handlers/
    │   ├── agents/            # Agent CRUD endpoints (Admin API)
    │   ├── patterns/          # Pattern CRUD endpoints (Admin API)
    │   ├── skills/            # Skill CRUD endpoints (Admin API)
    │   ├── search/            # Search endpoints (Admin API)
    │   └── operations/        # Health and version endpoints
    ├── mcpserver/             # MCP server handlers
    └── server/server.go       # HTTP server setup (Admin API + MCP)
```

**Key Integration Points:**

1. `internal/server/server.go` - Telemetry initialization, shutdown, and middleware registration (Admin API and MCP listeners)
2. `cmd/main/main.go` - Calls `server.ListenAndServe()` which internally initializes telemetry
3. All handler packages - Span creation and logging

## otelx Package Integration

> **Architecture Reference:** [Observability Architecture - Observability Stack](../../architecture/07-observability-architecture.md#observability-stack)

### Package Capabilities

The `otelx` package (v1.0.0) provides:

| Capability                 | otelx Support | Notes                              |
| -------------------------- | ------------- | ---------------------------------- |
| Unified initialization     | Yes           | Single `Initialize()` call         |
| Zerolog logging            | Yes           | With automatic trace correlation   |
| Prometheus metrics         | Yes           | Exposes `/metrics` endpoint        |
| OTLP tracing               | Yes           | gRPC exporter to collector         |
| Gin logging middleware     | Yes           | `middleware/gin.LoggingMiddleware` |
| Gin tracing middleware     | No            | Requires additional implementation |
| Request metrics middleware | No            | Requires additional implementation |
| Database instrumentation   | No            | Requires additional implementation |

### Required Dependencies

Add to `go.mod`:

```go
require (
    github.com/twistingmercury/otelx v1.0.0
    go.opentelemetry.io/otel v1.35.0
    go.opentelemetry.io/otel/metric v1.35.0
    go.opentelemetry.io/otel/trace v1.35.0
    go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin v0.60.0
)
```

The `otelgin` package provides the tracing middleware that otelx does not include.

## Initialization and Configuration

> **Architecture Reference:** [Observability Architecture](../../architecture/07-observability-architecture.md) | [Deployment Architecture - Operational Considerations](../../architecture/06-deployment-architecture.md#operational-considerations)
>
> **Note:** This configuration aligns with the established patterns in [Configuration Design](configuration.md). Environment variables use the `MNEMONIC_OBSERVABILITY_*` prefix for observability settings.

### Configuration Package

Observability configuration is integrated into the main `MnemonicConfig` structure following the unified configuration pattern. The observability settings are nested under the `Observability` section.

**Actual Implementation:**

Configuration structure from `internal/config/config.go`:

```go
type MnemonicConfig struct {
    Server        ServerConfigs
    Logging       LoggingConfig
    Observability ObservabilityConfig
}

type ServerConfigs struct {
    Admin AdminServerConfig
    MCP   MCPServerConfig
}

type AdminServerConfig struct {
    Host            string
    Port            int
    ReadTimeout     time.Duration
    WriteTimeout    time.Duration
    IdleTimeout     time.Duration
    ShutdownTimeout time.Duration
    TLS             TLSConfig
}

type MCPServerConfig struct {
    Host            string
    Port            int
    ReadTimeout     time.Duration
    WriteTimeout    time.Duration
    IdleTimeout     time.Duration
    ShutdownTimeout time.Duration
    SessionTimeout  time.Duration
    TLS             TLSConfig
}

type ObservabilityConfig struct {
    Metrics         MetricsConfig
    Tracing         TracingConfig
    Health          HealthConfig
    LogDBStatements bool // Default: false. When true, logs SQL/Cypher queries via zerolog at DEBUG level.
}

type HealthConfig struct {
    Enabled bool   // Default: true. When false, the /health endpoint is not registered.
    Path    string // Default: "/health". The path for the health check endpoint.
}

type MetricsConfig struct {
    Enabled bool
    Port    int
    Path    string
}

type TracingConfig struct {
    Enabled      bool
    Endpoint     string
    OTLPInsecure bool
    SampleRate   float64
}

type LoggingConfig struct {
    Level         string // Parsed to zerolog.Level in telemetry package
    Format        string // "json" (default) or "console" for human-readable development output
    IncludeCaller bool   // When true, adds caller file:line to log entries
}
```

The configuration is loaded via the `config.Load()` function which handles environment variables and defaults.

### Telemetry Initialization

**Actual Implementation** from `internal/telemetry/telemetry.go`:

```go
package telemetry

import (
    "context"
    "fmt"

    "github.com/rs/zerolog"
    "github.com/twistingmercury/mnemonic-enricher/internal/config"
    "github.com/twistingmercury/mnemonic-enricher/internal/metrics"
    "github.com/twistingmercury/mnemonic-enricher/internal/version"
    "github.com/twistingmercury/otelx"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

// Telemetry wraps the otelx.Telemetry with application-specific helpers.
type Telemetry struct {
    otel            *otelx.Telemetry
    logger          zerolog.Logger
    metricsRegistry *metrics.Registry
}

// Initialize creates and configures the telemetry system using otelx.
func Initialize(ctx context.Context, cfg *config.MnemonicConfig) (*Telemetry, error) {
    opts, err := buildOptions(cfg)
    if err != nil {
        return nil, fmt.Errorf("failed to build telemetry options: %w", err)
    }

    tel, err := otelx.Initialize(ctx, opts...)
    if err != nil {
        return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
    }

    // Create the centralized metrics registry using the meter provider.
    meter := tel.MeterProvider.Meter("mnemonic")
    registry, err := metrics.NewRegistry(meter)
    if err != nil {
        return nil, fmt.Errorf("failed to create metrics registry: %w", err)
    }

    return &Telemetry{
        otel:            tel,
        logger:          tel.Logger,
        metricsRegistry: registry,
    }, nil
}

func buildOptions(cfg *config.MnemonicConfig) ([]otelx.Option, error) {
    logLevel, err := parseLogLevel(cfg.Logging.Level)
    if err != nil {
        return nil, err
    }

    opts := []otelx.Option{
        otelx.WithService(
            "mnemonic",
            version.Version(),
            getEnvironment(),
        ),
        otelx.WithLogLevel(logLevel),
    }

    // Metrics configuration - otelx uses opt-in pattern
    if cfg.Observability.Metrics.Enabled {
        opts = append(opts, otelx.WithMetrics(cfg.Observability.Metrics.Port))
        opts = append(opts, otelx.WithMetricsPath(cfg.Observability.Metrics.Path))
    }

    // Tracing configuration - otelx uses opt-in pattern
    if cfg.Observability.Tracing.Enabled {
        opts = append(opts, otelx.WithTracing())
        opts = append(opts, otelx.WithTraceSampleRate(cfg.Observability.Tracing.SampleRate))
        if cfg.Observability.Tracing.Endpoint != "" {
            opts = append(opts, otelx.WithOTLPEndpoint(cfg.Observability.Tracing.Endpoint))
        }
        if cfg.Observability.Tracing.OTLPInsecure {
            opts = append(opts, otelx.WithOTLPInsecure())
        }
    }

    return opts, nil
}

// Shutdown gracefully shuts down telemetry, flushing pending data.
func (t *Telemetry) Shutdown(ctx context.Context) error {
    return t.otel.Shutdown(ctx)
}

// Logger returns the zerolog logger with trace correlation support.
func (t *Telemetry) Logger() zerolog.Logger {
    return t.logger
}

// Tracer returns an OpenTelemetry tracer for creating spans.
func (t *Telemetry) Tracer(name string) trace.Tracer {
    if t.otel.TracerProvider != nil {
        return t.otel.TracerProvider.Tracer(name)
    }
    return otel.Tracer(name)
}

// Meter returns an OpenTelemetry meter for creating metrics.
func (t *Telemetry) Meter(name string) metric.Meter {
    if t.otel.MeterProvider != nil {
        return t.otel.MeterProvider.Meter(name)
    }
    return otel.Meter(name)
}

// TracerProvider returns the underlying trace provider.
func (t *Telemetry) TracerProvider() trace.TracerProvider {
    if t.otel.TracerProvider != nil {
        return t.otel.TracerProvider
    }
    return otel.GetTracerProvider()
}

// MeterProvider returns the underlying meter provider.
func (t *Telemetry) MeterProvider() metric.MeterProvider {
    if t.otel.MeterProvider != nil {
        return t.otel.MeterProvider
    }
    return otel.GetMeterProvider()
}

// MetricsRegistry returns the centralized metrics registry.
func (t *Telemetry) MetricsRegistry() *metrics.Registry {
    return t.metricsRegistry
}

// Otelx returns the underlying otelx.Telemetry instance.
func (t *Telemetry) Otelx() *otelx.Telemetry {
    return t.otel
}
```

### Server Lifecycle and Telemetry

**Actual Implementation:**

Telemetry initialization is owned by the `server` package, not `main.go`. This follows the pattern where the server package owns configuration loading and lifecycle management.

From `internal/server/server.go`:

```go
// ListenAndServe starts the server using the provided configuration.
func ListenAndServe(cfg *config.MnemonicConfig) error {
    shutdown, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    // Initialize telemetry
    tel, err := telemetry.Initialize(shutdown, cfg)
    if err != nil {
        return fmt.Errorf("failed to initialize telemetry: %w", err)
    }
    defer func() {
        if shutdownErr := tel.Shutdown(context.Background()); shutdownErr != nil {
            logger := tel.Logger()
            logger.Error().Err(shutdownErr).Msg("telemetry shutdown error")
        }
    }()

    logger := tel.Logger()
    logger.Info().
        Str("admin_host", cfg.Server.Admin.Host).
        Int("admin_port", cfg.Server.Admin.Port).
        Str("mcp_host", cfg.Server.MCP.Host).
        Int("mcp_port", cfg.Server.MCP.Port).
        Bool("metrics_enabled", cfg.Observability.Metrics.Enabled).
        Bool("tracing_enabled", cfg.Observability.Tracing.Enabled).
        Msg("mnemonic starting")

    // Create request metrics middleware
    requestMetrics, err := middleware.NewRequestMetrics(tel.Meter("mnemonic/http"))
    if err != nil {
        return fmt.Errorf("failed to create request metrics: %w", err)
    }

    router := setupRouter(tel, requestMetrics)
    operations.SetupHandlers(router)

    // Server startup and graceful shutdown...
}
```

The `main.go` remains simple and delegates to the server package.

## Distributed Tracing Implementation

> **Architecture Reference:** [Observability Architecture - Distributed Tracing (Jaeger)](../../architecture/07-observability-architecture.md#distributed-tracing-jaeger)

### Trace Structure

Based on the architecture document, traces should capture:

```text
MCP search_patterns (45ms)
├── Validate Request (2ms)
├── Fetch Patterns (30ms)
│   ├── Postgres Query (10ms)
│   ├── PGVector Search (12ms)
│   └── Neo4j Query (8ms)
└── Build Response (13ms)
```

```text
POST /v1/api/patterns (50ms)
├── Validate Request (2ms)
├── Store Pattern (10ms)
│   └── Postgres Insert (8ms)
├── Queue Enrichment Job (3ms)
└── Build Response (5ms)
```

### Tracing Middleware

**Actual Implementation** from `internal/middleware/tracing.go`:

```go
package middleware

import (
    "net/http"
    "slices"

    "github.com/gin-gonic/gin"
    "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// skipPaths defines paths to exclude from tracing to reduce noise.
var defaultSkipPaths = []string{
    "/health",
    "/metrics",
}

// TracingMiddleware returns Gin middleware that creates spans for HTTP requests.
// It uses W3C Trace Context for trace propagation and automatically instruments
// incoming requests with OpenTelemetry spans.
func TracingMiddleware(serviceName string) gin.HandlerFunc {
    return otelgin.Middleware(serviceName,
        otelgin.WithFilter(func(req *http.Request) bool {
            // Skip tracing for health checks and metrics to reduce noise
            return !slices.Contains(defaultSkipPaths, req.URL.Path)
        }),
    )
}

// TracingMiddlewareWithSkipPaths returns Gin middleware with custom skip paths.
// This allows callers to specify which paths should not be traced.
func TracingMiddlewareWithSkipPaths(serviceName string, skipPaths []string) gin.HandlerFunc {
    return otelgin.Middleware(serviceName,
        otelgin.WithFilter(func(req *http.Request) bool {
            return !slices.Contains(skipPaths, req.URL.Path)
        }),
    )
}
```

The implementation provides two functions for flexibility:

- `TracingMiddleware()` with default skip paths
- `TracingMiddlewareWithSkipPaths()` for custom skip path configuration

### Creating Child Spans in Handlers

Handlers create child spans for logical operations:

```go
package search

import (
    "context"

    "github.com/gin-gonic/gin"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("mnemonic/handlers/search")

func SearchPatterns(c *gin.Context) {
    ctx := c.Request.Context()

    // Validate request
    ctx, validateSpan := tracer.Start(ctx, "validate_request")
    req, err := validateSearchRequest(ctx, c)
    if err != nil {
        validateSpan.RecordError(err)
        validateSpan.SetStatus(codes.Error, err.Error())
        validateSpan.End()
        // Return error response...
        return
    }
    validateSpan.SetAttributes(
        attribute.String("query.preview", truncate(req.Query, 100)),
    )
    validateSpan.End()

    // Fetch patterns
    ctx, patternSpan := tracer.Start(ctx, "fetch_patterns")
    patterns, err := fetchPatterns(ctx, req)
    patternSpan.SetAttributes(
        attribute.Int("patterns.count", len(patterns)),
    )
    if err != nil {
        patternSpan.RecordError(err)
        patternSpan.SetStatus(codes.Error, err.Error())
    }
    patternSpan.End()

    // Build and return response...
}
```

### Span Naming Conventions

| Operation          | Span Name               | Attributes                       |
| ------------------ | ----------------------- | -------------------------------- |
| HTTP request       | `HTTP {METHOD} {route}` | Auto by otelgin                  |
| MCP tool call      | `mcp.{tool_name}`       | `tool.name`, `session.id`        |
| Request validation | `validate_request`      | `query.preview`                  |
| Pattern fetch      | `fetch_patterns`        | `patterns.count`                 |
| Postgres query     | `postgres.query`        | `db.system`, `db.operation`      |
| PGVector search    | `pgvector.search`       | `db.system`, `vector.dimensions` |
| Neo4j query        | `neo4j.query`           | `db.system`, `db.operation`      |

## Metrics Implementation

> **Architecture Reference:** [Observability Architecture - Metrics (Prometheus)](../../architecture/07-observability-architecture.md#metrics-prometheus)

> **Prometheus naming:** OpenTelemetry uses dot-separated metric names (e.g., `mnemonic.http.request.count`), but Prometheus converts dots to underscores when scraping and appends a `_total` suffix to counters. For example, `mnemonic.http.request.count` becomes `mnemonic_http_request_count_total` in PromQL, and `mnemonic.http.request.duration` becomes `mnemonic_http_request_duration_milliseconds`. Keep this conversion in mind when writing PromQL queries or configuring alert rules.

### Metrics Categories

Based on the architecture document, implement these metric categories:

1. **Request metrics** - HTTP request counts, durations, in-flight (Admin API)
2. **MCP server metrics** - Tool invocations, session counts, active sessions
3. **Pattern metrics** - Query latency, patterns returned
4. **Tooling metrics** - List, get, and write operations by resource type (agents/skills)
5. **Database metrics** - Connection pools, query latency, errors

### Request Metrics Middleware

**Actual Implementation** from `internal/middleware/metrics.go`:

```go
package middleware

import (
    "context"
    "strconv"
    "time"

    "github.com/gin-gonic/gin"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// RequestMetrics holds the instruments for HTTP request metrics.
type RequestMetrics struct {
    requestCount    metric.Int64Counter
    requestDuration metric.Float64Histogram
    requestInFlight metric.Int64UpDownCounter
}

// NewRequestMetrics creates request metric instruments.
func NewRequestMetrics(meter metric.Meter) (*RequestMetrics, error) {
    requestCount, err := meter.Int64Counter(
        "mnemonic.http.request.count",
        metric.WithDescription("Total number of HTTP requests"),
        metric.WithUnit("{request}"),
    )
    if err != nil {
        return nil, err
    }

    requestDuration, err := meter.Float64Histogram(
        "mnemonic.http.request.duration",
        metric.WithDescription("HTTP request duration in milliseconds"),
        metric.WithUnit("ms"),
        metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
    )
    if err != nil {
        return nil, err
    }

    requestInFlight, err := meter.Int64UpDownCounter(
        "mnemonic.http.request.in_flight",
        metric.WithDescription("Number of HTTP requests currently in flight"),
        metric.WithUnit("{request}"),
    )
    if err != nil {
        return nil, err
    }

    return &RequestMetrics{
        requestCount:    requestCount,
        requestDuration: requestDuration,
        requestInFlight: requestInFlight,
    }, nil
}

// Middleware returns Gin middleware that records request metrics.
func (m *RequestMetrics) Middleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()

        // Track in-flight requests
        // Use the request context for increment but context.Background() for decrement
        // because the request context may be cancelled after c.Next() completes
        m.requestInFlight.Add(c.Request.Context(), 1)
        defer m.requestInFlight.Add(context.Background(), -1)

        // Process request
        c.Next()

        // Record metrics after request completes
        // Use context.Background() for post-request metric recording
        // because the request context may be cancelled after c.Next() completes
        duration := float64(time.Since(start).Milliseconds())
        attrs := []attribute.KeyValue{
            attribute.String("http.method", c.Request.Method),
            attribute.String("http.route", c.FullPath()),
            attribute.String("http.status_code", strconv.Itoa(c.Writer.Status())),
        }

        m.requestCount.Add(context.Background(), 1, metric.WithAttributes(attrs...))
        m.requestDuration.Record(context.Background(), duration, metric.WithAttributes(attrs...))
    }
}

// MiddlewareWithSkipPaths returns Gin middleware that records request metrics,
// skipping the specified paths (e.g., /health, /metrics) to reduce noise.
func (m *RequestMetrics) MiddlewareWithSkipPaths(skipPaths []string) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Skip metrics recording for excluded paths
        for _, path := range skipPaths {
            if c.Request.URL.Path == path {
                c.Next()
                return
            }
        }

        start := time.Now()

        m.requestInFlight.Add(c.Request.Context(), 1)
        defer m.requestInFlight.Add(context.Background(), -1)

        c.Next()

        duration := float64(time.Since(start).Milliseconds())
        attrs := []attribute.KeyValue{
            attribute.String("http.method", c.Request.Method),
            attribute.String("http.route", c.FullPath()),
            attribute.String("http.status_code", strconv.Itoa(c.Writer.Status())),
        }

        m.requestCount.Add(context.Background(), 1, metric.WithAttributes(attrs...))
        m.requestDuration.Record(context.Background(), duration, metric.WithAttributes(attrs...))
    }
}
```

### MCP Server Metrics

Create `internal/metrics/mcp.go`:

```go
package metrics

import (
    "context"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// MCP holds instruments for MCP server metrics.
type MCP struct {
    toolInvocations metric.Int64Counter
    toolDuration    metric.Float64Histogram
    sessionCount    metric.Int64Counter
    activeSessions  metric.Int64UpDownCounter
}

// NewMCP creates MCP server metric instruments.
func NewMCP(meter metric.Meter) (*MCP, error) {
    toolInvocations, err := meter.Int64Counter(
        "mnemonic.mcp.tool_invocations",
        metric.WithDescription("Number of MCP tool invocations"),
        metric.WithUnit("{invocation}"),
    )
    if err != nil {
        return nil, err
    }

    toolDuration, err := meter.Float64Histogram(
        "mnemonic.mcp.tool_duration",
        metric.WithDescription("MCP tool invocation duration in milliseconds"),
        metric.WithUnit("ms"),
        metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
    )
    if err != nil {
        return nil, err
    }

    sessionCount, err := meter.Int64Counter(
        "mnemonic.mcp.session_count",
        metric.WithDescription("Number of MCP sessions created"),
        metric.WithUnit("{session}"),
    )
    if err != nil {
        return nil, err
    }

    activeSessions, err := meter.Int64UpDownCounter(
        "mnemonic.mcp.active_sessions",
        metric.WithDescription("Number of active MCP sessions"),
        metric.WithUnit("{session}"),
    )
    if err != nil {
        return nil, err
    }

    return &MCP{
        toolInvocations: toolInvocations,
        toolDuration:    toolDuration,
        sessionCount:    sessionCount,
        activeSessions:  activeSessions,
    }, nil
}

// RecordToolInvocation records an MCP tool invocation with its duration.
func (m *MCP) RecordToolInvocation(ctx context.Context, toolName string, durationMS float64) {
    attrs := metric.WithAttributes(attribute.String("tool", toolName))
    m.toolInvocations.Add(ctx, 1, attrs)
    m.toolDuration.Record(ctx, durationMS, attrs)
}

// RecordSessionCreated records a new MCP session.
func (m *MCP) RecordSessionCreated(ctx context.Context) {
    m.sessionCount.Add(ctx, 1)
    m.activeSessions.Add(ctx, 1)
}

// RecordSessionClosed records an MCP session closure.
func (m *MCP) RecordSessionClosed(ctx context.Context) {
    m.activeSessions.Add(ctx, -1)
}
```

> **Deferred:** MCP server-side instrumentation (wiring these metrics into MCP tool handlers, adding tracing spans for MCP requests) is deferred to a later MVP iteration. The metric instruments above define the contract; the integration point in `internal/mcpserver/` will be implemented once the MCP SDK's handler middleware patterns are finalized. Until then, MCP tool calls will be instrumented via native MCP receiving middleware, not through Admin API proxying.

### Pattern Metrics

Create `internal/metrics/patterns.go`:

```go
package metrics

import (
    "context"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// Pattern holds instruments for pattern retrieval metrics.
type Pattern struct {
    queryLatency     metric.Float64Histogram
    patternsReturned metric.Int64Histogram
}

// NewPattern creates pattern metric instruments.
func NewPattern(meter metric.Meter) (*Pattern, error) {
    queryLatency, err := meter.Float64Histogram(
        "mnemonic.patterns.query_latency",
        metric.WithDescription("Pattern query latency in milliseconds"),
        metric.WithUnit("ms"),
        metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500),
    )
    if err != nil {
        return nil, err
    }

    patternsReturned, err := meter.Int64Histogram(
        "mnemonic.patterns.returned",
        metric.WithDescription("Number of patterns returned per query"),
        metric.WithUnit("{pattern}"),
        metric.WithExplicitBucketBoundaries(0, 1, 5, 10, 25, 50, 100),
    )
    if err != nil {
        return nil, err
    }

    return &Pattern{
        queryLatency:     queryLatency,
        patternsReturned: patternsReturned,
    }, nil
}

// RecordQuery records a pattern query with its latency and result count.
func (m *Pattern) RecordQuery(ctx context.Context, database string, duration time.Duration, count int) {
    attrs := metric.WithAttributes(attribute.String("database", database))
    m.queryLatency.Record(ctx, float64(duration.Milliseconds()), attrs)
    m.patternsReturned.Record(ctx, int64(count), attrs)
}
```

### Tooling Metrics

> **Architecture Reference:** [Observability Architecture - Metrics](../../architecture/07-observability-architecture.md#metrics) (Tooling metrics section)

Create `internal/metrics/tooling.go`:

```go
package metrics

import (
    "context"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// Tooling holds instruments for agent/skill CRUD operation metrics.
type Tooling struct {
    listOperations  metric.Int64Counter
    getOperations   metric.Int64Counter
    writeOperations metric.Int64Counter
}

// NewTooling creates tooling metric instruments.
func NewTooling(meter metric.Meter) (*Tooling, error) {
    listOperations, err := meter.Int64Counter(
        "mnemonic.tooling.list.operations",
        metric.WithDescription("Number of tooling list operations"),
        metric.WithUnit("{operation}"),
    )
    if err != nil {
        return nil, err
    }

    getOperations, err := meter.Int64Counter(
        "mnemonic.tooling.get.operations",
        metric.WithDescription("Number of tooling get-by-ID operations"),
        metric.WithUnit("{operation}"),
    )
    if err != nil {
        return nil, err
    }

    writeOperations, err := meter.Int64Counter(
        "mnemonic.tooling.write.operations",
        metric.WithDescription("Number of tooling admin write operations (create, update, delete)"),
        metric.WithUnit("{operation}"),
    )
    if err != nil {
        return nil, err
    }

    return &Tooling{
        listOperations:  listOperations,
        getOperations:   getOperations,
        writeOperations: writeOperations,
    }, nil
}

// RecordListOperation records a tooling list operation for the given resource type.
func (m *Tooling) RecordListOperation(ctx context.Context, resourceType string) {
    attrs := metric.WithAttributes(attribute.String("resource_type", resourceType))
    m.listOperations.Add(ctx, 1, attrs)
}

// RecordGetOperation records a tooling get-by-ID operation for the given resource type.
func (m *Tooling) RecordGetOperation(ctx context.Context, resourceType string) {
    attrs := metric.WithAttributes(attribute.String("resource_type", resourceType))
    m.getOperations.Add(ctx, 1, attrs)
}

// RecordWriteOperation records a tooling admin write operation.
func (m *Tooling) RecordWriteOperation(ctx context.Context, resourceType, operation string) {
    attrs := metric.WithAttributes(
        attribute.String("resource_type", resourceType),
        attribute.String("operation", operation),
    )
    m.writeOperations.Add(ctx, 1, attrs)
}
```

The `resource_type` attribute distinguishes between `agents` and `skills`. The `operation` attribute on write operations distinguishes between `create`, `update`, and `delete`.

### Database Metrics

Create `internal/metrics/database.go`:

```go
package metrics

import (
    "context"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// Database holds instruments for database-related metrics.
type Database struct {
    connectionPoolSize   metric.Int64Gauge
    connectionPoolInUse  metric.Int64Gauge
    queryLatency         metric.Float64Histogram
    queryErrors          metric.Int64Counter
}

// NewDatabase creates database metric instruments.
func NewDatabase(meter metric.Meter) (*Database, error) {
    connectionPoolSize, err := meter.Int64Gauge(
        "mnemonic.db.connection_pool.size",
        metric.WithDescription("Total size of the database connection pool"),
        metric.WithUnit("{connection}"),
    )
    if err != nil {
        return nil, err
    }

    connectionPoolInUse, err := meter.Int64Gauge(
        "mnemonic.db.connection_pool.in_use",
        metric.WithDescription("Number of connections currently in use"),
        metric.WithUnit("{connection}"),
    )
    if err != nil {
        return nil, err
    }

    queryLatency, err := meter.Float64Histogram(
        "mnemonic.db.query_latency",
        metric.WithDescription("Database query latency in milliseconds"),
        metric.WithUnit("ms"),
        metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
    )
    if err != nil {
        return nil, err
    }

    queryErrors, err := meter.Int64Counter(
        "mnemonic.db.query_errors",
        metric.WithDescription("Number of database query errors"),
        metric.WithUnit("{error}"),
    )
    if err != nil {
        return nil, err
    }

    return &Database{
        connectionPoolSize:  connectionPoolSize,
        connectionPoolInUse: connectionPoolInUse,
        queryLatency:        queryLatency,
        queryErrors:         queryErrors,
    }, nil
}

// RecordPoolStats records connection pool statistics.
func (m *Database) RecordPoolStats(ctx context.Context, database string, size, inUse int64) {
    attrs := metric.WithAttributes(attribute.String("database", database))
    m.connectionPoolSize.Record(ctx, size, attrs)
    m.connectionPoolInUse.Record(ctx, inUse, attrs)
}

// RecordQuery records a database query with latency.
func (m *Database) RecordQuery(ctx context.Context, database, operation string, duration time.Duration) {
    m.queryLatency.Record(ctx, float64(duration.Milliseconds()), metric.WithAttributes(
        attribute.String("database", database),
        attribute.String("operation", operation),
    ))
}

// RecordError records a database error.
func (m *Database) RecordError(ctx context.Context, database, operation string) {
    m.queryErrors.Add(ctx, 1, metric.WithAttributes(
        attribute.String("database", database),
        attribute.String("operation", operation),
    ))
}
```

## Enrichment Worker Observability

> **Architecture Reference:** [Observability Architecture - Metrics](../../architecture/07-observability-architecture.md#metrics) | [Pattern Enrichment](pattern-processing.md)

The enrichment worker runs as a background goroutine processing pattern enrichment jobs (embedding generation, concept extraction, graph node creation). It requires dedicated metrics, tracing spans, and structured logging to provide visibility into asynchronous processing.

### Enrichment Metrics

Create `internal/metrics/enrichment.go`:

```go
package metrics

import (
    "context"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// Enrichment holds instruments for enrichment worker metrics.
type Enrichment struct {
    jobsQueued    metric.Int64Counter
    jobsClaimed   metric.Int64Counter
    jobsCompleted metric.Int64Counter
    jobsFailed    metric.Int64Counter
    jobDuration   metric.Float64Histogram
    retries       metric.Int64Counter
}

// NewEnrichment creates enrichment worker metric instruments.
func NewEnrichment(meter metric.Meter) (*Enrichment, error) {
    jobsQueued, err := meter.Int64Counter(
        "mnemonic.enrichment.jobs.queued",
        metric.WithDescription("Number of enrichment jobs added to the queue"),
        metric.WithUnit("{job}"),
    )
    if err != nil {
        return nil, err
    }

    jobsClaimed, err := meter.Int64Counter(
        "mnemonic.enrichment.jobs.claimed",
        metric.WithDescription("Number of enrichment jobs picked up by the worker"),
        metric.WithUnit("{job}"),
    )
    if err != nil {
        return nil, err
    }

    jobsCompleted, err := meter.Int64Counter(
        "mnemonic.enrichment.jobs.completed",
        metric.WithDescription("Number of enrichment jobs finished successfully"),
        metric.WithUnit("{job}"),
    )
    if err != nil {
        return nil, err
    }

    jobsFailed, err := meter.Int64Counter(
        "mnemonic.enrichment.jobs.failed",
        metric.WithDescription("Number of enrichment jobs that failed"),
        metric.WithUnit("{job}"),
    )
    if err != nil {
        return nil, err
    }

    jobDuration, err := meter.Float64Histogram(
        "mnemonic.enrichment.job.duration",
        metric.WithDescription("Enrichment job processing time from claim to completion in milliseconds"),
        metric.WithUnit("ms"),
        metric.WithExplicitBucketBoundaries(100, 500, 1000, 2000, 5000, 10000, 30000),
    )
    if err != nil {
        return nil, err
    }

    retries, err := meter.Int64Counter(
        "mnemonic.enrichment.retries",
        metric.WithDescription("Number of enrichment job retry attempts"),
        metric.WithUnit("{retry}"),
    )
    if err != nil {
        return nil, err
    }

    return &Enrichment{
        jobsQueued:    jobsQueued,
        jobsClaimed:   jobsClaimed,
        jobsCompleted: jobsCompleted,
        jobsFailed:    jobsFailed,
        jobDuration:   jobDuration,
        retries:       retries,
    }, nil
}

// RecordJobQueued records an enrichment job being added to the queue.
func (m *Enrichment) RecordJobQueued(ctx context.Context) {
    m.jobsQueued.Add(ctx, 1)
}

// RecordJobClaimed records an enrichment job being picked up by the worker.
func (m *Enrichment) RecordJobClaimed(ctx context.Context) {
    m.jobsClaimed.Add(ctx, 1)
}

// RecordJobCompleted records a successfully completed enrichment job.
func (m *Enrichment) RecordJobCompleted(ctx context.Context, durationMS float64) {
    m.jobsCompleted.Add(ctx, 1)
    m.jobDuration.Record(ctx, durationMS)
}

// RecordJobFailed records a failed enrichment job with the failure reason.
func (m *Enrichment) RecordJobFailed(ctx context.Context, reason string) {
    attrs := metric.WithAttributes(attribute.String("reason", reason))
    m.jobsFailed.Add(ctx, 1, attrs)
}

// RecordRetry records an enrichment job retry attempt.
func (m *Enrichment) RecordRetry(ctx context.Context) {
    m.retries.Add(ctx, 1)
}
```

### Enrichment Tracing Spans

The enrichment worker creates a parent span for the full job lifecycle with child spans for each processing step. This enables trace-based debugging of slow or failed enrichment jobs.

| Span Name                       | Parent               | Attributes                               |
| ------------------------------- | -------------------- | ---------------------------------------- |
| `enrichment.process`            | (root)               | `pattern.id`, `job.id`, `job.attempt`    |
| `enrichment.embed`              | `enrichment.process` | `pattern.id`, `embedding.dimensions`     |
| `enrichment.extract_concepts`   | `enrichment.process` | `pattern.id`, `concept.count`            |
| `enrichment.create_graph_nodes` | `enrichment.process` | `pattern.id`, `node.count`, `edge.count` |

Example span creation in the enrichment worker:

```go
package enrichment

import (
    "context"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("mnemonic/enrichment")

// processJob processes a single enrichment job with full tracing.
func (w *Worker) processJob(ctx context.Context, job EnrichmentJob) error {
    ctx, span := tracer.Start(ctx, "enrichment.process",
        trace.WithAttributes(
            attribute.String("pattern.id", job.PatternID.String()),
            attribute.String("job.id", job.ID.String()),
            attribute.Int("job.attempt", job.Attempts),
        ),
    )
    defer span.End()

    // Step 1: Generate embedding
    ctx, embedSpan := tracer.Start(ctx, "enrichment.embed",
        trace.WithAttributes(
            attribute.String("pattern.id", job.PatternID.String()),
        ),
    )
    embedding, err := w.generateEmbedding(ctx, job.PatternID)
    if err != nil {
        embedSpan.RecordError(err)
        embedSpan.SetStatus(codes.Error, "embedding generation failed")
        embedSpan.End()
        return err
    }
    embedSpan.SetAttributes(attribute.Int("embedding.dimensions", len(embedding)))
    embedSpan.End()

    // Step 2: Extract concepts
    ctx, extractSpan := tracer.Start(ctx, "enrichment.extract_concepts",
        trace.WithAttributes(
            attribute.String("pattern.id", job.PatternID.String()),
        ),
    )
    concepts, err := w.extractConcepts(ctx, job.PatternID)
    if err != nil {
        extractSpan.RecordError(err)
        extractSpan.SetStatus(codes.Error, "concept extraction failed")
        extractSpan.End()
        return err
    }
    extractSpan.SetAttributes(attribute.Int("concept.count", len(concepts)))
    extractSpan.End()

    // Step 3: Create graph nodes
    _, graphSpan := tracer.Start(ctx, "enrichment.create_graph_nodes",
        trace.WithAttributes(
            attribute.String("pattern.id", job.PatternID.String()),
        ),
    )
    nodeCount, edgeCount, err := w.createGraphNodes(ctx, job.PatternID, concepts)
    if err != nil {
        graphSpan.RecordError(err)
        graphSpan.SetStatus(codes.Error, "graph node creation failed")
        graphSpan.End()
        return err
    }
    graphSpan.SetAttributes(
        attribute.Int("node.count", nodeCount),
        attribute.Int("edge.count", edgeCount),
    )
    graphSpan.End()

    span.SetStatus(codes.Ok, "enrichment completed")
    return nil
}
```

### Enrichment Structured Logging

The enrichment worker emits structured log events using `github.com/rs/zerolog` for each stage of job processing. These events provide operational visibility into the background pipeline.

| Event               | Level | Fields                                           |
| ------------------- | ----- | ------------------------------------------------ |
| Job claimed         | Info  | `pattern_id`, `worker_id`                        |
| Embedding generated | Debug | `pattern_id`, `duration_ms`                      |
| Concepts extracted  | Debug | `pattern_id`, `concept_count`                    |
| Graph nodes created | Debug | `pattern_id`                                     |
| Job completed       | Info  | `pattern_id`, `total_duration_ms`                |
| Job failed          | Error | `pattern_id`, `error`, `attempt_count`           |
| Job retried         | Warn  | `pattern_id`, `attempt_number`, `next_scheduled` |

Example logging in the enrichment worker:

```go
package enrichment

import (
    "time"

    "github.com/rs/zerolog/log"
)

func (w *Worker) logJobClaimed(patternID string) {
    log.Info().
        Str("pattern_id", patternID).
        Str("worker_id", w.id).
        Msg("enrichment job claimed")
}

func (w *Worker) logEmbeddingGenerated(patternID string, duration time.Duration) {
    log.Debug().
        Str("pattern_id", patternID).
        Int64("duration_ms", duration.Milliseconds()).
        Msg("embedding generated")
}

func (w *Worker) logConceptsExtracted(patternID string, conceptCount int) {
    log.Debug().
        Str("pattern_id", patternID).
        Int("concept_count", conceptCount).
        Msg("concepts extracted")
}

func (w *Worker) logGraphNodesCreated(patternID string) {
    log.Debug().
        Str("pattern_id", patternID).
        Msg("graph nodes created")
}

func (w *Worker) logJobCompleted(patternID string, totalDuration time.Duration) {
    log.Info().
        Str("pattern_id", patternID).
        Int64("total_duration_ms", totalDuration.Milliseconds()).
        Msg("enrichment job completed")
}

func (w *Worker) logJobFailed(patternID string, err error, attemptCount int) {
    log.Error().
        Err(err).
        Str("pattern_id", patternID).
        Int("attempt_count", attemptCount).
        Msg("enrichment job failed")
}

func (w *Worker) logJobRetried(patternID string, attemptNumber int, nextScheduled time.Time) {
    log.Warn().
        Str("pattern_id", patternID).
        Int("attempt_number", attemptNumber).
        Time("next_scheduled", nextScheduled).
        Msg("enrichment job retried")
}
```

## Metrics Registry

Create `internal/metrics/registry.go` to centralize metric initialization:

```go
package metrics

import (
    "go.opentelemetry.io/otel/metric"
)

// Registry holds all metric instruments for the application.
type Registry struct {
    MCP        *MCP
    Patterns   *Pattern
    Tooling    *Tooling
    Database   *Database
    Enrichment *Enrichment
}

// NewRegistry creates all metric instruments.
func NewRegistry(meter metric.Meter) (*Registry, error) {
    mcp, err := NewMCP(meter)
    if err != nil {
        return nil, err
    }

    patterns, err := NewPattern(meter)
    if err != nil {
        return nil, err
    }

    tooling, err := NewTooling(meter)
    if err != nil {
        return nil, err
    }

    database, err := NewDatabase(meter)
    if err != nil {
        return nil, err
    }

    enrichment, err := NewEnrichment(meter)
    if err != nil {
        return nil, err
    }

    return &Registry{
        MCP:        mcp,
        Patterns:   patterns,
        Tooling:    tooling,
        Database:   database,
        Enrichment: enrichment,
    }, nil
}
```

## Structured Logging Implementation

> **Architecture Reference:** [Observability Architecture - Logs (Loki)](../../architecture/07-observability-architecture.md#logs-loki)

### Using otelx Gin Middleware

**Actual Implementation:**

otelx provides `middleware/gin.LoggingMiddleware` for request logging with trace correlation. From `internal/server/server.go`:

```go
package server

import (
    "github.com/gin-gonic/gin"
    "github.com/twistingmercury/mnemonic-enricher/internal/middleware"
    "github.com/twistingmercury/mnemonic-enricher/internal/telemetry"
    otelxgin "github.com/twistingmercury/otelx/middleware/gin"
)

// setupRouter creates and configures the Gin router with middleware.
func setupRouter(tel *telemetry.Telemetry, requestMetrics *middleware.RequestMetrics) *gin.Engine {
    // Use gin.New() instead of gin.Default() to avoid duplicate logging
    router := gin.New()

    // Recovery middleware (keep this)
    router.Use(gin.Recovery())

    // Paths to skip for tracing and metrics
    skipPaths := []string{"/health", "/metrics"}

    // Tracing middleware using otelgin
    router.Use(middleware.TracingMiddlewareWithSkipPaths("mnemonic", skipPaths))

    // otelx logging middleware with trace correlation
    router.Use(otelxgin.LoggingMiddleware(tel.Otelx(),
        otelxgin.WithSkipPaths("/health", "/metrics"),
        otelxgin.WithRequestHeaders("X-Request-ID", "X-Correlation-ID"),
    ))

    // Request metrics middleware
    router.Use(requestMetrics.MiddlewareWithSkipPaths(skipPaths))

    return router
}
```

**Key Differences:**

- Import alias `otelxgin` distinguishes from contrib `otelgin`
- `WithSkipPaths()` uses variadic parameters, not slices
- Skip paths for `/health` and `/metrics`
- `WithRequestHeaders()` uses variadic parameters
- Middleware registration includes tracing and metrics

### Handler Logging

Use the logger from context in handlers:

```go
package agents

import (
    "github.com/gin-gonic/gin"
    otelgin "github.com/twistingmercury/otelx/middleware/gin"
    "github.com/twistingmercury/mnemonic-enricher/internal/telemetry"
)

func ListAgents(c *gin.Context, tel *telemetry.Telemetry) {
    logger := otelgin.Logger(c, tel.Otelx())

    logger.Info().Msg("listing agents")

    // Business logic...

    logger.Debug().
        Int("count", len(agents)).
        Msg("agents retrieved")
}
```

### Log Entry Structure

All log entries automatically include (via otelx):

```json
{
  "level": "info",
  "time": "2024-01-21T10:30:00Z",
  "service": "mnemonic",
  "trace_id": "abc123def456...",
  "span_id": "789xyz...",
  "message": "request completed",
  "http.method": "POST",
  "http.path": "/v1/api/patterns",
  "http.status_code": 200,
  "latency_ms": 45
}
```

### Log Levels by Event Type

| Event Type              | Level | Example                     |
| ----------------------- | ----- | --------------------------- |
| Request received        | Debug | Start of request processing |
| Request completed (2xx) | Info  | Successful response         |
| Request completed (4xx) | Warn  | Client error                |
| Request completed (5xx) | Error | Server error                |
| MCP tool invocation     | Info  | Tool called                 |
| Pattern query           | Debug | Database query executed     |
| Configuration loaded    | Info  | Startup configuration       |
| Service lifecycle       | Info  | Start/stop events           |
| Validation failure      | Warn  | Invalid input               |
| Database error          | Error | Connection/query failure    |

## Handler Instrumentation Patterns

> **Architecture Reference:** [System Architecture - Mnemonic](../../architecture/02-system-architecture.md#mnemonic) | [Observability Architecture - Key Takeaways](../../architecture/07-observability-architecture.md#key-takeaways)

### Handler Dependencies

Create `internal/handlers/deps.go`:

```go
package handlers

import (
    "github.com/twistingmercury/mnemonic-enricher/internal/metrics"
    "github.com/twistingmercury/mnemonic-enricher/internal/telemetry"
    "go.opentelemetry.io/otel/trace"
)

// Dependencies holds shared dependencies for all handlers.
type Dependencies struct {
    Tel     *telemetry.Telemetry
    Metrics *metrics.Registry
    Tracer  trace.Tracer
}

// NewDependencies creates handler dependencies.
func NewDependencies(tel *telemetry.Telemetry, metrics *metrics.Registry) *Dependencies {
    return &Dependencies{
        Tel:     tel,
        Metrics: metrics,
        Tracer:  tel.TracerProvider().Tracer("mnemonic/handlers"),
    }
}
```

### Instrumented Handler Pattern

Example of a fully instrumented handler:

```go
package search

import (
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
    otelgin "github.com/twistingmercury/otelx/middleware/gin"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"

    "github.com/twistingmercury/mnemonic-enricher/internal/handlers"
)

// SearchPatterns handles GET /v1/api/patterns/search with full observability.
func SearchPatterns(deps *handlers.Dependencies) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx := c.Request.Context()
        logger := otelgin.Logger(c, deps.Tel.Otelx())

        // 1. Validate request with span
        ctx, validateSpan := deps.Tracer.Start(ctx, "validate_request")
        req, err := validateSearchRequest(c)
        if err != nil {
            validateSpan.RecordError(err)
            validateSpan.SetStatus(codes.Error, "validation failed")
            validateSpan.End()

            logger.Warn().Err(err).Msg("request validation failed")
            c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
            return
        }
        validateSpan.End()

        // 2. Fetch patterns with span and metrics
        ctx, patternSpan := deps.Tracer.Start(ctx, "fetch_patterns")
        patternStart := time.Now()

        patterns, err := fetchPatterns(ctx, deps, req)
        patternDuration := time.Since(patternStart)

        patternSpan.SetAttributes(attribute.Int("patterns.count", len(patterns)))
        if err != nil {
            patternSpan.RecordError(err)
            patternSpan.SetStatus(codes.Error, "pattern fetch failed")
        }
        patternSpan.End()

        deps.Metrics.Patterns.RecordQuery(ctx, "combined", patternDuration, len(patterns))

        // 3. Build and return response
        response := buildSearchResponse(patterns)

        logger.Debug().
            Int("pattern_count", len(patterns)).
            Msg("response built")

        c.JSON(http.StatusOK, response)
    }
}
```

### Handler Registration Update

Update handler registration to use dependencies:

```go
package search

import (
    "github.com/gin-gonic/gin"
    "github.com/twistingmercury/mnemonic-enricher/internal/handlers"
)

// SetupHandlers registers search handlers with dependencies.
func SetupHandlers(r *gin.Engine, deps *handlers.Dependencies) {
    r.GET("/v1/api/patterns/search", SearchPatterns(deps))
}
```

## Database Instrumentation

> **Architecture Reference:** [System Architecture - Mnemonic](../../architecture/02-system-architecture.md#mnemonic) | [Observability Architecture - Metrics (Prometheus)](../../architecture/07-observability-architecture.md#metrics-prometheus)

### Postgres/PGVector Instrumentation

Create `internal/repository/postgres/instrumented.go`:

```go
package postgres

import (
    "context"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/rs/zerolog/log"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"

    "github.com/twistingmercury/mnemonic-enricher/internal/metrics"
)

var tracer = otel.Tracer("mnemonic/repository/postgres")

// InstrumentedPool wraps pgxpool.Pool with observability.
type InstrumentedPool struct {
    pool            *pgxpool.Pool
    metrics         *metrics.Database
    logDBStatements bool
}

// NewInstrumentedPool creates an instrumented database pool.
func NewInstrumentedPool(pool *pgxpool.Pool, metrics *metrics.Database, logDBStatements bool) *InstrumentedPool {
    return &InstrumentedPool{
        pool:            pool,
        metrics:         metrics,
        logDBStatements: logDBStatements,
    }
}

// logStatement conditionally logs the SQL query text at DEBUG level.
// Controlled by the observability.log_db_statements config flag (default: false).
// Mnemonic does not store sensitive data, but defaulting to off keeps logs lean
// and gives operators a knob for debugging.
func (p *InstrumentedPool) logStatement(sql string, args ...any) {
    if p.logDBStatements {
        log.Debug().
            Str("statement", sql).
            Int("args_count", len(args)).
            Msg("db.query")
    }
}

// Query executes a query with tracing and metrics.
func (p *InstrumentedPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
    ctx, span := tracer.Start(ctx, "postgres.query",
        trace.WithAttributes(
            attribute.String("db.system", "postgresql"),
        ),
    )
    defer span.End()

    p.logStatement(sql, args...)

    start := time.Now()
    rows, err := p.pool.Query(ctx, sql, args...)
    duration := time.Since(start)

    p.metrics.RecordQuery(ctx, "postgres", "query", duration)

    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        p.metrics.RecordError(ctx, "postgres", "query")
        return nil, err
    }

    return rows, nil
}

// QueryRow executes a query that returns a single row with tracing.
//
// NOTE: Span timing limitation — pgx.Row uses lazy scanning, so the actual
// data read happens when the caller invokes Scan(), not during QueryRow().
// The span created here closes before data is read, which means the recorded
// duration reflects only the query dispatch time, not the full read cycle.
// For accurate QueryRow timing, create a span at the service layer that
// encompasses both the QueryRow call and the subsequent Scan().
func (p *InstrumentedPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
    ctx, span := tracer.Start(ctx, "postgres.query_row",
        trace.WithAttributes(
            attribute.String("db.system", "postgresql"),
        ),
    )

    p.logStatement(sql, args...)

    start := time.Now()
    row := p.pool.QueryRow(ctx, sql, args...)
    duration := time.Since(start)

    p.metrics.RecordQuery(ctx, "postgres", "query_row", duration)
    span.End()

    return row
}

// Exec executes a command with tracing and metrics.
func (p *InstrumentedPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
    ctx, span := tracer.Start(ctx, "postgres.exec",
        trace.WithAttributes(
            attribute.String("db.system", "postgresql"),
        ),
    )
    defer span.End()

    p.logStatement(sql, args...)

    start := time.Now()
    tag, err := p.pool.Exec(ctx, sql, args...)
    duration := time.Since(start)

    p.metrics.RecordQuery(ctx, "postgres", "exec", duration)

    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        p.metrics.RecordError(ctx, "postgres", "exec")
        return tag, err
    }

    return tag, nil
}

// RecordPoolStats records connection pool statistics.
func (p *InstrumentedPool) RecordPoolStats(ctx context.Context) {
    stats := p.pool.Stat()
    p.metrics.RecordPoolStats(ctx, "postgres",
        int64(stats.MaxConns()),
        int64(stats.AcquiredConns()),
    )
}
```

### Neo4j Instrumentation

Create `internal/repository/neo4j/instrumented.go`:

```go
package neo4j

import (
    "context"
    "time"

    "github.com/neo4j/neo4j-go-driver/v5/neo4j"
    "github.com/rs/zerolog/log"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"

    "github.com/twistingmercury/mnemonic-enricher/internal/metrics"
)

var tracer = otel.Tracer("mnemonic/repository/neo4j")

// InstrumentedSession wraps neo4j.SessionWithContext with observability.
type InstrumentedSession struct {
    session         neo4j.SessionWithContext
    metrics         *metrics.Database
    logDBStatements bool
}

// NewInstrumentedSession creates an instrumented Neo4j session.
func NewInstrumentedSession(session neo4j.SessionWithContext, metrics *metrics.Database, logDBStatements bool) *InstrumentedSession {
    return &InstrumentedSession{
        session:         session,
        metrics:         metrics,
        logDBStatements: logDBStatements,
    }
}

// Run executes a Cypher query with tracing and metrics.
func (s *InstrumentedSession) Run(ctx context.Context, cypher string, params map[string]any) (neo4j.ResultWithContext, error) {
    ctx, span := tracer.Start(ctx, "neo4j.query",
        trace.WithAttributes(
            attribute.String("db.system", "neo4j"),
        ),
    )
    defer span.End()

    if s.logDBStatements {
        log.Debug().
            Str("statement", cypher).
            Int("args_count", len(params)).
            Msg("db.query")
    }

    start := time.Now()
    result, err := s.session.Run(ctx, cypher, params)
    duration := time.Since(start)

    s.metrics.RecordQuery(ctx, "neo4j", "query", duration)

    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        s.metrics.RecordError(ctx, "neo4j", "query")
        return nil, err
    }

    return result, nil
}

// Close closes the session.
func (s *InstrumentedSession) Close(ctx context.Context) error {
    return s.session.Close(ctx)
}
```

### Connection Pool Monitoring

Create a background goroutine to periodically record pool stats:

```go
package telemetry

import (
    "context"
    "time"
)

// StartPoolStatsRecorder starts a background routine to record pool stats.
func StartPoolStatsRecorder(ctx context.Context, pool *postgres.InstrumentedPool, interval time.Duration) {
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                pool.RecordPoolStats(ctx)
            }
        }
    }()
}
```

## Health Check Implementation

> **Architecture Reference:** [Observability Architecture - Health Check Endpoint](../../architecture/07-observability-architecture.md#health-check-endpoint)

Mnemonic uses the [`github.com/twistingmercury/heartbeat`](https://github.com/twistingmercury/heartbeat) package for health checks. The health endpoint is registered at `GET /health` on the Admin API (:8080).

### Heartbeat Integration

The `heartbeat` package provides a Gin-compatible handler that checks registered dependencies and returns structured health status. The operations handler registers it directly on the router.

**Actual Implementation** from `internal/handlers/operations/operations.go`:

```go
package operations

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/twistingmercury/heartbeat"
    "github.com/twistingmercury/mnemonic-enricher/internal/version"
)

// SetupHandlers associates the handlers related to operations endpoints
// to the gin.Engine that is passed in.
func SetupHandlers(r *gin.Engine) {
    deps := DefineDependencies()

    r.GET("/health", heartbeat.Handler("mnemonic", deps...))
    r.GET("/version", GetVersion)
}
```

The `heartbeat.Handler` function accepts a service name and a variadic list of `heartbeat.DependencyDescriptor` values. Each descriptor defines a named dependency with a health check function that returns a `heartbeat.StatusResult`.

### Dependency Registration

Health check dependencies are defined in `internal/health/health.go` using `heartbeat.DependencyDescriptor`:

```go
package health

import (
    "github.com/twistingmercury/heartbeat"
    "github.com/twistingmercury/mnemonic-enricher/internal/config"
)

func Initialize(conf *config.MnemonicConfig) error {
    // Register dependency descriptors for PostgreSQL, Neo4j,
    // and AI model connectivity checks
    deps = []heartbeat.DependencyDescriptor{
        {
            Name:        "PostgreSQL check",
            Type:        "database",
            HandlerFunc: checkPostgreSQLHealth,
        },
        {
            Name:        "Neo4j check",
            Type:        "database",
            HandlerFunc: checkNeo4jHealth,
        },
        // Additional dependency checks...
    }
    return nil
}
```

Each `HandlerFunc` performs a lightweight connectivity probe (ping or equivalent) against its dependency. The `heartbeat` package aggregates results and returns HTTP 200 when all dependencies are healthy or HTTP 503 when any dependency is unhealthy.

### Health Check in Middleware Skip Paths

The health endpoint `/health` is included in `DefaultSkipPaths` to exclude it from tracing, logging, and request metrics, avoiding noise from frequent probe requests by container orchestration and load balancers.

## Gaps and Additional Implementation

### What otelx Provides

| Capability                   | Status   | Notes                      |
| ---------------------------- | -------- | -------------------------- |
| Unified initialization       | Provided | Single `Initialize()` call |
| Structured logging (zerolog) | Provided | With trace correlation     |
| Prometheus metrics endpoint  | Provided | Configurable port/path     |
| OTLP trace export            | Provided | gRPC to collector          |
| Gin logging middleware       | Provided | Request completion logging |
| Gin correlation middleware   | Provided | Logger in context          |

### What Requires Additional Implementation

| Capability                   | Status | Implementation Needed          |
| ---------------------------- | ------ | ------------------------------ |
| Gin tracing middleware       | Gap    | Use `otelgin` from contrib     |
| Request metrics middleware   | Gap    | Custom `middleware/metrics.go` |
| Application-specific metrics | Gap    | Custom `metrics/*.go`          |
| Database instrumentation     | Gap    | Custom repository wrappers     |
| Connection pool monitoring   | Gap    | Background stat recorder       |
| Custom sampling logic        | Gap    | Configure via otelx options    |

### Recommended Package Structure

```text
src/mnemonic/internal/
├── config/
│   └── config.go              # Configuration including observability
├── telemetry/
│   └── telemetry.go           # otelx initialization wrapper
├── middleware/
│   ├── tracing.go             # otelgin tracing middleware
│   └── metrics.go             # Request metrics middleware
├── metrics/
│   ├── registry.go            # Centralized metric registry
│   ├── patterns.go            # Pattern-specific metrics
│   ├── tooling.go             # Tooling (agents/skills) metrics
│   ├── enrichment.go          # Enrichment worker metrics
│   └── database.go            # Database-specific metrics
├── handlers/
│   ├── deps.go                # Handler dependencies
│   └── ...                    # Existing handlers (updated)
└── repository/
    ├── postgres/
    │   └── instrumented.go    # Instrumented Postgres pool
    └── neo4j/
        └── instrumented.go    # Instrumented Neo4j session
```

## Implementation Checklist

### Foundation

- [ ] Create `internal/config/config.go` with observability configuration
- [ ] Create `internal/telemetry/telemetry.go` wrapping otelx
- [ ] Verify `cmd/main/main.go` calls `server.ListenAndServe()`, which internally initializes telemetry
- [ ] Add required dependencies to `go.mod`

### Middleware

- [ ] Create `internal/middleware/tracing.go` using otelgin
- [ ] Create `internal/middleware/metrics.go` for request metrics
- [ ] Update `internal/server/server.go` to register middleware
- [ ] Configure otelx logging middleware with skip paths

### Application Metrics

- [ ] Create `internal/metrics/registry.go`
- [ ] Create `internal/metrics/patterns.go`
- [ ] Create `internal/metrics/tooling.go`
- [ ] Create `internal/metrics/enrichment.go`
- [ ] Create `internal/metrics/database.go`

### Handler Instrumentation

- [ ] Create `internal/handlers/deps.go` for dependency injection
- [ ] Update handler signatures to accept dependencies
- [ ] Add span creation in handlers for logical operations
- [ ] Add metric recording calls in handlers
- [ ] Use otelgin.Logger for trace-correlated logging

### Database Instrumentation

- [ ] Create `internal/repository/postgres/instrumented.go`
- [ ] Create `internal/repository/neo4j/instrumented.go`
- [ ] Implement connection pool stat recording
- [ ] Wrap all database calls with instrumentation

### Testing and Verification

- [ ] Unit tests for metric instruments
- [ ] Integration tests for middleware
- [ ] Verify log output contains trace IDs
- [ ] Verify Prometheus metrics endpoint works
- [ ] Document local development setup (disable/stdout exporters)

---

Copyright (c) 2025 Jeremy K. Johnson. All rights reserved.
