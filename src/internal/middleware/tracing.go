package middleware

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// DefaultSkipPaths defines paths to exclude from tracing and metrics to reduce noise.
// This includes health check and metrics endpoints.
var DefaultSkipPaths = []string{
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
			return !slices.Contains(DefaultSkipPaths, req.URL.Path)
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
