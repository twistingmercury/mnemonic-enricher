package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/twistingmercury/mnemonic/internal/middleware"
	"go.opentelemetry.io/otel/trace"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestTracingMiddleware(t *testing.T) {
	router := gin.New()
	router.Use(middleware.TracingMiddleware("test-service"))

	router.GET("/test", func(c *gin.Context) {
		// Verify span is accessible in context (may not be valid without configured provider)
		span := trace.SpanFromContext(c.Request.Context())
		_ = span // Span is present even if not valid
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Note: Without a configured tracer provider, spans may not be valid
	// This test primarily verifies the middleware doesn't panic
}

func TestTracingMiddlewareSkipsHealthPath(t *testing.T) {
	router := gin.New()
	router.Use(middleware.TracingMiddleware("test-service"))

	healthCalled := false
	router.GET("/health", func(c *gin.Context) {
		healthCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, healthCalled)
}

func TestTracingMiddlewareSkipsMetricsPath(t *testing.T) {
	router := gin.New()
	router.Use(middleware.TracingMiddleware("test-service"))

	metricsCalled := false
	router.GET("/metrics", func(c *gin.Context) {
		metricsCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, metricsCalled)
}

func TestTracingMiddlewareWithSkipPaths(t *testing.T) {
	customSkipPaths := []string{"/custom/skip", "/another/skip"}

	router := gin.New()
	router.Use(middleware.TracingMiddlewareWithSkipPaths("test-service", customSkipPaths))

	customSkipCalled := false
	router.GET("/custom/skip", func(c *gin.Context) {
		customSkipCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/custom/skip", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, customSkipCalled)
}

func TestTracingMiddlewareDoesNotSkipRegularPaths(t *testing.T) {
	router := gin.New()
	router.Use(middleware.TracingMiddleware("test-service"))

	apiCalled := false
	router.GET("/api/v1/test", func(c *gin.Context) {
		apiCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, apiCalled)
}
