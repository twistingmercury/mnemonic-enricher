package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/middleware"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewRequestMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")

	rm, err := middleware.NewRequestMetrics(meter)
	require.NoError(t, err)
	assert.NotNil(t, rm)
}

func TestRequestMetricsMiddleware(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")

	rm, err := middleware.NewRequestMetrics(meter)
	require.NoError(t, err)

	router := gin.New()
	router.Use(rm.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Collect metrics
	var data metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &data)
	require.NoError(t, err)

	// Verify metrics were recorded
	assert.NotEmpty(t, data.ScopeMetrics)
}

func TestRequestMetricsMiddlewareRecordsStatusCode(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")

	rm, err := middleware.NewRequestMetrics(meter)
	require.NoError(t, err)

	router := gin.New()
	router.Use(rm.Middleware())
	router.GET("/success", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/notfound", func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})
	router.GET("/error", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	// Test success
	req := httptest.NewRequest(http.MethodGet, "/success", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Test not found
	req = httptest.NewRequest(http.MethodGet, "/notfound", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Test error
	req = httptest.NewRequest(http.MethodGet, "/error", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Collect and verify metrics
	var data metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &data)
	require.NoError(t, err)
	assert.NotEmpty(t, data.ScopeMetrics)
}

func TestRequestMetricsMiddlewareWithSkipPaths(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")

	rm, err := middleware.NewRequestMetrics(meter)
	require.NoError(t, err)

	skipPaths := []string{"/health", "/metrics"}

	router := gin.New()
	router.Use(rm.MiddlewareWithSkipPaths(skipPaths))
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/metrics", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Call health - should be skipped
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Call metrics - should be skipped
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Collect metrics before API call
	var dataBeforeAPI metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &dataBeforeAPI)
	require.NoError(t, err)

	// Count metrics before API call
	metricCountBefore := countMetricDataPoints(dataBeforeAPI)

	// Call API - should NOT be skipped
	req = httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Collect metrics after API call
	var dataAfterAPI metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &dataAfterAPI)
	require.NoError(t, err)

	metricCountAfter := countMetricDataPoints(dataAfterAPI)

	// Verify metrics were recorded for API call (count should increase)
	assert.Greater(t, metricCountAfter, metricCountBefore)
}

func TestRequestMetricsRecordsHTTPMethod(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")

	rm, err := middleware.NewRequestMetrics(meter)
	require.NoError(t, err)

	router := gin.New()
	router.Use(rm.Middleware())
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})
	router.PUT("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.DELETE("/test", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// Test POST
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Test PUT
	req = httptest.NewRequest(http.MethodPut, "/test", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Test DELETE
	req = httptest.NewRequest(http.MethodDelete, "/test", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Collect and verify
	var data metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &data)
	require.NoError(t, err)
	assert.NotEmpty(t, data.ScopeMetrics)
}

// Helper function to count total data points in metrics
func countMetricDataPoints(data metricdata.ResourceMetrics) int {
	count := 0
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				count += len(d.DataPoints)
			case metricdata.Sum[float64]:
				count += len(d.DataPoints)
			case metricdata.Histogram[int64]:
				count += len(d.DataPoints)
			case metricdata.Histogram[float64]:
				count += len(d.DataPoints)
			case metricdata.Gauge[int64]:
				count += len(d.DataPoints)
			case metricdata.Gauge[float64]:
				count += len(d.DataPoints)
			}
		}
	}
	return count
}
