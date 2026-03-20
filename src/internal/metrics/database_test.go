package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewDatabase(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	dm, err := metrics.NewDatabase(meter)
	require.NoError(t, err)
	assert.NotNil(t, dm)
}

func TestDatabaseRecordPoolStats(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	dm, err := metrics.NewDatabase(meter)
	require.NoError(t, err)

	ctx := context.Background()
	dm.RecordPoolStats(ctx, "postgres", 25, 10)
	dm.RecordPoolStats(ctx, "neo4j", 50, 5)

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
	foundPoolSize := false
	foundPoolInUse := false
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "mnemonic.db.connection_pool.size" {
				foundPoolSize = true
			}
			if m.Name == "mnemonic.db.connection_pool.in_use" {
				foundPoolInUse = true
			}
		}
	}
	assert.True(t, foundPoolSize, "pool size metric should be recorded")
	assert.True(t, foundPoolInUse, "pool in use metric should be recorded")
}

func TestDatabaseRecordQuery(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	dm, err := metrics.NewDatabase(meter)
	require.NoError(t, err)

	ctx := context.Background()
	dm.RecordQuery(ctx, "postgres", "select", 50*time.Millisecond)
	dm.RecordQuery(ctx, "postgres", "insert", 25*time.Millisecond)
	dm.RecordQuery(ctx, "neo4j", "query", 100*time.Millisecond)

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
	foundQueryLatency := false
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "mnemonic.db.query_latency" {
				foundQueryLatency = true
			}
		}
	}
	assert.True(t, foundQueryLatency, "query latency metric should be recorded")
}

func TestDatabaseRecordError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	dm, err := metrics.NewDatabase(meter)
	require.NoError(t, err)

	ctx := context.Background()
	dm.RecordError(ctx, "postgres", "select")
	dm.RecordError(ctx, "postgres", "insert")
	dm.RecordError(ctx, "neo4j", "query")

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
	foundQueryErrors := false
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "mnemonic.db.query_errors" {
				foundQueryErrors = true
			}
		}
	}
	assert.True(t, foundQueryErrors, "query errors metric should be recorded")
}

func TestDatabaseRecordQueryWithError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	dm, err := metrics.NewDatabase(meter)
	require.NoError(t, err)

	ctx := context.Background()

	// Record successful query
	dm.RecordQueryWithError(ctx, "postgres", "select", 50*time.Millisecond, nil)

	// Record failed query
	queryErr := errors.New("connection timeout")
	dm.RecordQueryWithError(ctx, "postgres", "select", 5000*time.Millisecond, queryErr)

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
	foundQueryLatency := false
	foundQueryErrors := false
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "mnemonic.db.query_latency" {
				foundQueryLatency = true
			}
			if m.Name == "mnemonic.db.query_errors" {
				foundQueryErrors = true
			}
		}
	}
	assert.True(t, foundQueryLatency, "query latency metric should be recorded")
	assert.True(t, foundQueryErrors, "query errors metric should be recorded for failed query")
}

func TestDatabaseWithMultipleDatabases(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	dm, err := metrics.NewDatabase(meter)
	require.NoError(t, err)

	ctx := context.Background()

	// Record metrics for multiple databases
	databases := []string{"postgres", "pgvector", "neo4j"}
	operations := []string{"select", "insert", "update", "delete"}

	for _, db := range databases {
		dm.RecordPoolStats(ctx, db, 25, 5)
		for _, op := range operations {
			dm.RecordQuery(ctx, db, op, 10*time.Millisecond)
		}
	}

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
}
