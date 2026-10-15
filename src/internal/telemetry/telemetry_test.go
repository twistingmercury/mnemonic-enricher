package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic-enricher/internal/config"
	"github.com/twistingmercury/mnemonic-enricher/internal/telemetry"
)

func createTestConfig() *config.MnemonicConfig {
	return &config.MnemonicConfig{
		Server: config.ServerConfig{
			Host:            "localhost",
			Port:            8080,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Database: config.DatabaseConfig{
			Postgres: config.PostgresConfig{
				Host:            "localhost",
				Port:            5432,
				Database:        "mnemonic",
				Username:        "mnemonic",
				SSLMode:         "disable",
				MaxOpenConns:    25,
				MaxIdleConns:    5,
				ConnMaxLifetime: 5 * time.Minute,
			},
			Neo4j: config.Neo4jConfig{
				URI:                          "bolt://localhost:7687",
				Username:                     "neo4j",
				Database:                     "neo4j",
				MaxConnectionPoolSize:        50,
				ConnectionAcquisitionTimeout: 60 * time.Second,
			},
		},
		OpenAI: config.OpenAIConfig{
			EmbeddingModel:      "text-embedding-3-small",
			EmbeddingDimensions: 1536,
			ExtractionModel:     "gpt-4o-mini",
			RetryAttempts:       3,
			RetryDelay:          time.Second,
		},
		Enrichment: config.EnrichmentConfig{
			WorkerCount: 2,
			RetryDelay:  30 * time.Second,
			JobTimeout:  5 * time.Minute,
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		Observability: config.ObservabilityConfig{
			Metrics: config.MetricsConfig{
				Enabled: false, // Disable for tests
				Path:    "/metrics",
				Port:    9090,
			},
			Tracing: config.TracingConfig{
				Enabled:      false, // Disable for tests
				Endpoint:     "",
				SampleRate:   0.1,
				OTLPInsecure: true,
			},
		},
	}
}

func TestInitialize(t *testing.T) {
	cfg := createTestConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	// Verify we can get a logger
	logger := tel.Logger()
	assert.NotNil(t, logger)

	// Verify we can get a meter
	meter := tel.Meter("test")
	assert.NotNil(t, meter)

	// Clean shutdown
	err = tel.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestInitializeWithMetricsEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Observability.Metrics.Enabled = true
	cfg.Observability.Metrics.Port = 19090 // Use a high port to avoid conflicts

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	defer func() {
		_ = tel.Shutdown(ctx)
	}()

	assert.NotNil(t, tel.Meter("test"))
}

func TestInitializeWithTracingEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Observability.Tracing.Enabled = true
	cfg.Observability.Tracing.Endpoint = "localhost:4317"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	defer func() {
		_ = tel.Shutdown(ctx)
	}()

	assert.NotNil(t, tel.Otelx().TracerProvider)
}

func TestShutdown(t *testing.T) {
	cfg := createTestConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)

	// Shutdown should not error
	err = tel.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestOtelxAccess(t *testing.T) {
	cfg := createTestConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	defer func() {
		_ = tel.Shutdown(ctx)
	}()

	// Verify we can access the underlying otelx instance
	otelx := tel.Otelx()
	assert.NotNil(t, otelx)
}

func TestInitializeWithInvalidLogLevel(t *testing.T) {
	cfg := createTestConfig()
	cfg.Logging.Level = "invalid-level"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.Error(t, err)
	assert.Nil(t, tel)
	assert.Contains(t, err.Error(), "invalid log level")
}

func TestMetricsRegistryCreatedAndAccessible(t *testing.T) {
	cfg := createTestConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)
	defer func() {
		_ = tel.Shutdown(ctx)
	}()

	// Verify metrics registry is created and accessible
	registry := tel.MetricsRegistry()
	assert.NotNil(t, registry, "MetricsRegistry should not be nil")

	// Verify the registry has its sub-registries initialized
	assert.NotNil(t, registry.Jobs, "Patterns metrics should be initialized")
	assert.NotNil(t, registry.Database, "Database metrics should be initialized")
}
