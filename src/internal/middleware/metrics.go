package middleware

import (
	"context"
	"fmt"
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

// NewRequestMetrics creates request metric instruments using the provided meter.
func NewRequestMetrics(meter metric.Meter) (*RequestMetrics, error) {
	requestCount, err := meter.Int64Counter(
		"mnemonic.http.request.count",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("request count counter: %w", err)
	}

	requestDuration, err := meter.Float64Histogram(
		"mnemonic.http.request.duration",
		metric.WithDescription("HTTP request duration in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
	)
	if err != nil {
		return nil, fmt.Errorf("request duration histogram: %w", err)
	}

	requestInFlight, err := meter.Int64UpDownCounter(
		"mnemonic.http.request.in_flight",
		metric.WithDescription("Number of HTTP requests currently in flight"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("request in-flight counter: %w", err)
	}

	return &RequestMetrics{
		requestCount:    requestCount,
		requestDuration: requestDuration,
		requestInFlight: requestInFlight,
	}, nil
}

// Middleware returns Gin middleware that records request metrics.
// It tracks request count, duration, and in-flight requests with attributes
// for method, route, and status code.
func (m *RequestMetrics) Middleware() gin.HandlerFunc {
	return m.MiddlewareWithSkipPaths(nil)
}

// MiddlewareWithSkipPaths returns metrics middleware that skips specified paths.
// This is useful for excluding health checks and metrics endpoints from metrics collection.
func (m *RequestMetrics) MiddlewareWithSkipPaths(skipPaths []string) gin.HandlerFunc {
	skipMap := make(map[string]struct{}, len(skipPaths))
	for _, path := range skipPaths {
		skipMap[path] = struct{}{}
	}

	return func(c *gin.Context) {
		// Check if this path should be skipped
		if _, skip := skipMap[c.Request.URL.Path]; skip {
			c.Next()
			return
		}

		start := time.Now()
		ctx := c.Request.Context()

		// Track in-flight requests
		// Use the request context for increment but context.Background() for decrement
		// because the request context may be cancelled after c.Next() completes
		m.requestInFlight.Add(ctx, 1)
		defer m.requestInFlight.Add(context.Background(), -1)

		// Process request
		c.Next()

		// Record metrics after request completes
		duration := float64(time.Since(start).Milliseconds())

		// Use "unknown" if no route pattern available to avoid high-cardinality metrics
		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}

		attrs := []attribute.KeyValue{
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.route", route),
			attribute.String("http.status_code", strconv.Itoa(c.Writer.Status())),
		}

		// Use context.Background() for post-request metric recording
		// because the request context may be cancelled after c.Next() completes
		m.requestCount.Add(context.Background(), 1, metric.WithAttributes(attrs...))
		m.requestDuration.Record(context.Background(), duration, metric.WithAttributes(attrs...))
	}
}
