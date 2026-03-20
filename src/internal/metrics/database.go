package metrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Database holds instruments for database-related metrics.
// It tracks connection pool statistics, query latency, and errors.
type Database struct {
	connectionPoolSize  metric.Int64Gauge
	connectionPoolInUse metric.Int64Gauge
	queryLatency        metric.Float64Histogram
	queryErrors         metric.Int64Counter
}

// NewDatabase creates database metric instruments using the provided meter.
func NewDatabase(meter metric.Meter) (*Database, error) {
	connectionPoolSize, err := meter.Int64Gauge(
		"mnemonic.db.connection_pool.size",
		metric.WithDescription("Total size of the database connection pool"),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return nil, fmt.Errorf("connection pool size gauge: %w", err)
	}

	connectionPoolInUse, err := meter.Int64Gauge(
		"mnemonic.db.connection_pool.in_use",
		metric.WithDescription("Number of connections currently in use"),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return nil, fmt.Errorf("connection pool in-use gauge: %w", err)
	}

	queryLatency, err := meter.Float64Histogram(
		"mnemonic.db.query_latency",
		metric.WithDescription("Database query latency in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
	)
	if err != nil {
		return nil, fmt.Errorf("query latency histogram: %w", err)
	}

	queryErrors, err := meter.Int64Counter(
		"mnemonic.db.query_errors",
		metric.WithDescription("Number of database query errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("query errors counter: %w", err)
	}

	return &Database{
		connectionPoolSize:  connectionPoolSize,
		connectionPoolInUse: connectionPoolInUse,
		queryLatency:        queryLatency,
		queryErrors:         queryErrors,
	}, nil
}

// RecordPoolStats records connection pool statistics for the specified database.
// Call this periodically (e.g., every 30 seconds) to track pool health.
// The database parameter should be a predefined database name (e.g., "postgres", "neo4j")
// with bounded cardinality. Do not use user-provided or dynamic values.
func (m *Database) RecordPoolStats(ctx context.Context, database string, size, inUse int64) {
	attrs := metric.WithAttributes(attribute.String("database", database))
	m.connectionPoolSize.Record(ctx, size, attrs)
	m.connectionPoolInUse.Record(ctx, inUse, attrs)
}

// RecordQuery records a database query with its latency and operation type.
// The operation parameter identifies the type of query (e.g., "select", "insert", "update").
// Both database and operation should be predefined values with bounded cardinality.
// Do not use user-provided or dynamic values to avoid metric explosion.
func (m *Database) RecordQuery(ctx context.Context, database, operation string, duration time.Duration) {
	m.queryLatency.Record(ctx, float64(duration.Milliseconds()), metric.WithAttributes(
		attribute.String("database", database),
		attribute.String("operation", operation),
	))
}

// RecordError records a database query error.
// Both database and operation should be predefined values with bounded cardinality.
// Do not use user-provided or dynamic values to avoid metric explosion.
func (m *Database) RecordError(ctx context.Context, database, operation string) {
	m.queryErrors.Add(ctx, 1, metric.WithAttributes(
		attribute.String("database", database),
		attribute.String("operation", operation),
	))
}

// RecordQueryWithError records a database query, including an error if one occurred.
// This is a convenience method for recording both latency and potential errors.
func (m *Database) RecordQueryWithError(ctx context.Context, database, operation string, duration time.Duration, err error) {
	m.RecordQuery(ctx, database, operation, duration)
	if err != nil {
		m.RecordError(ctx, database, operation)
	}
}
