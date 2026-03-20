package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewPattern(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	pm, err := metrics.NewPattern(meter)
	require.NoError(t, err)
	assert.NotNil(t, pm)
}

func TestPatternRecordQuery(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	pm, err := metrics.NewPattern(meter)
	require.NoError(t, err)

	ctx := context.Background()
	pm.RecordQuery(ctx, "postgres", 50*time.Millisecond, 10)
	pm.RecordQuery(ctx, "pgvector", 100*time.Millisecond, 5)
	pm.RecordQuery(ctx, "neo4j", 75*time.Millisecond, 8)

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
	foundQueryLatency := false
	foundPatternsReturned := false
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "mnemonic.patterns.query_latency" {
				foundQueryLatency = true
			}
			if m.Name == "mnemonic.patterns.returned" {
				foundPatternsReturned = true
			}
		}
	}
	assert.True(t, foundQueryLatency, "query latency metric should be recorded")
	assert.True(t, foundPatternsReturned, "patterns returned metric should be recorded")
}

func TestPatternRecordQueryLatency(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	pm, err := metrics.NewPattern(meter)
	require.NoError(t, err)

	ctx := context.Background()
	pm.RecordQueryLatency(ctx, "postgres", 50*time.Millisecond)
	pm.RecordQueryLatency(ctx, "postgres", 100*time.Millisecond)

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
}

func TestPatternRecordPatternsReturned(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	pm, err := metrics.NewPattern(meter)
	require.NoError(t, err)

	ctx := context.Background()
	pm.RecordPatternsReturned(ctx, "neo4j", 15)
	pm.RecordPatternsReturned(ctx, "neo4j", 0)

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
}

func TestPatternWithDifferentDatabases(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	pm, err := metrics.NewPattern(meter)
	require.NoError(t, err)

	ctx := context.Background()

	databases := []string{"postgres", "pgvector", "neo4j"}
	for _, db := range databases {
		pm.RecordQuery(ctx, db, 25*time.Millisecond, 5)
	}

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
}
