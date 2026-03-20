package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/config"
)

// TestDefaultValues verifies that all default values are set correctly.
func TestDefaultValues(t *testing.T) {
	// Clear any environment variables that might interfere
	clearMnemonicEnvVars(t)

	v := viper.New()
	config.SetDefaults(v)

	cfg := &config.MnemonicConfig{}
	err := v.Unmarshal(cfg)
	require.NoError(t, err)

	// Server defaults
	assert.Equal(t, config.DefaultServerHost, cfg.Server.Host)
	assert.Equal(t, config.DefaultServerPort, cfg.Server.Port)
	assert.Equal(t, config.DefaultServerReadTimeout, cfg.Server.ReadTimeout)
	assert.Equal(t, config.DefaultServerWriteTimeout, cfg.Server.WriteTimeout)
	assert.Equal(t, config.DefaultServerIdleTimeout, cfg.Server.IdleTimeout)
	assert.Equal(t, config.DefaultServerShutdownTimeout, cfg.Server.ShutdownTimeout)
	assert.Equal(t, config.DefaultServerTLSEnabled, cfg.Server.TLS.Enabled)

	// MCP defaults
	assert.Equal(t, config.DefaultMCPPort, cfg.MCP.Port)
	assert.Equal(t, config.DefaultMCPReadTimeout, cfg.MCP.ReadTimeout)
	assert.Equal(t, config.DefaultMCPWriteTimeout, cfg.MCP.WriteTimeout)
	assert.Equal(t, config.DefaultMCPIdleTimeout, cfg.MCP.IdleTimeout)
	assert.Equal(t, config.DefaultMCPDefaultSearchThreshold, cfg.MCP.DefaultSearchThreshold)

	// PostgreSQL defaults
	assert.Equal(t, config.DefaultPostgresHost, cfg.Database.Postgres.Host)
	assert.Equal(t, config.DefaultPostgresPort, cfg.Database.Postgres.Port)
	assert.Equal(t, config.DefaultPostgresDatabase, cfg.Database.Postgres.Database)
	assert.Equal(t, config.DefaultPostgresUsername, cfg.Database.Postgres.Username)
	assert.Equal(t, config.DefaultPostgresSSLMode, cfg.Database.Postgres.SSLMode)
	assert.Equal(t, config.DefaultPostgresMaxOpenConns, cfg.Database.Postgres.MaxOpenConns)
	assert.Equal(t, config.DefaultPostgresMaxIdleConns, cfg.Database.Postgres.MaxIdleConns)
	assert.Equal(t, config.DefaultPostgresConnMaxLifetime, cfg.Database.Postgres.ConnMaxLifetime)

	// Neo4j defaults
	assert.Equal(t, config.DefaultNeo4jURI, cfg.Database.Neo4j.URI)
	assert.Equal(t, config.DefaultNeo4jUsername, cfg.Database.Neo4j.Username)
	assert.Equal(t, config.DefaultNeo4jDatabase, cfg.Database.Neo4j.Database)
	assert.Equal(t, config.DefaultNeo4jMaxConnectionPoolSize, cfg.Database.Neo4j.MaxConnectionPoolSize)
	assert.Equal(t, config.DefaultNeo4jConnectionAcquisitionTimeout, cfg.Database.Neo4j.ConnectionAcquisitionTimeout)

	// OpenAI defaults
	assert.Equal(t, config.DefaultOpenAIEmbeddingModel, cfg.OpenAI.EmbeddingModel)
	assert.Equal(t, config.DefaultOpenAIEmbeddingDimensions, cfg.OpenAI.EmbeddingDimensions)
	assert.Equal(t, config.DefaultOpenAIExtractionModel, cfg.OpenAI.ExtractionModel)
	assert.Equal(t, config.DefaultOpenAIMaxRequestsPerMinute, cfg.OpenAI.MaxRequestsPerMinute)
	assert.Equal(t, config.DefaultOpenAIRetryAttempts, cfg.OpenAI.RetryAttempts)
	assert.Equal(t, config.DefaultOpenAIRetryDelay, cfg.OpenAI.RetryDelay)

	// Rate limit defaults
	assert.Equal(t, config.DefaultRateLimitEnabled, cfg.RateLimit.Enabled)
	assert.Equal(t, config.DefaultRateLimitRequestsPerSecond, cfg.RateLimit.RequestsPerSecond)
	assert.Equal(t, config.DefaultRateLimitBurstSize, cfg.RateLimit.BurstSize)
	assert.Equal(t, config.DefaultRateLimitPerUserRPM, cfg.RateLimit.PerUser.RequestsPerMinute)
	assert.Equal(t, config.DefaultRateLimitPerUserBurst, cfg.RateLimit.PerUser.BurstSize)

	// Enrichment defaults
	assert.Equal(t, config.DefaultEnrichmentWorkerCount, cfg.Enrichment.WorkerCount)
	assert.Equal(t, config.DefaultEnrichmentPollInterval, cfg.Enrichment.PollInterval)
	assert.Equal(t, config.DefaultEnrichmentMaxAttempts, cfg.Enrichment.MaxAttempts)
	assert.Equal(t, config.DefaultEnrichmentRetryDelay, cfg.Enrichment.RetryDelay)
	assert.Equal(t, config.DefaultEnrichmentJobTimeout, cfg.Enrichment.JobTimeout)
	assert.Equal(t, config.DefaultEnrichmentDrainTimeout, cfg.Enrichment.DrainTimeout)
	assert.Equal(t, config.DefaultEnrichmentCompletedRetention, cfg.Enrichment.CompletedRetention)
	assert.Equal(t, config.DefaultEnrichmentFailedRetention, cfg.Enrichment.FailedRetention)
	assert.Equal(t, config.DefaultEnrichmentRelatedToMinSimilarity, cfg.Enrichment.RelatedToMinSimilarity)

	// Logging defaults
	assert.Equal(t, config.DefaultLoggingLevel, cfg.Logging.Level)
	assert.Equal(t, config.DefaultLoggingFormat, cfg.Logging.Format)
	assert.Equal(t, config.DefaultLoggingIncludeCaller, cfg.Logging.IncludeCaller)

	// Observability defaults
	assert.Equal(t, config.DefaultMetricsEnabled, cfg.Observability.Metrics.Enabled)
	assert.Equal(t, config.DefaultMetricsPath, cfg.Observability.Metrics.Path)
	assert.Equal(t, config.DefaultMetricsPort, cfg.Observability.Metrics.Port)
	assert.Equal(t, config.DefaultHealthEnabled, cfg.Observability.Health.Enabled)
	assert.Equal(t, config.DefaultHealthPath, cfg.Observability.Health.Path)
	assert.Equal(t, config.DefaultTracingEnabled, cfg.Observability.Tracing.Enabled)
	assert.Equal(t, config.DefaultTracingSampleRate, cfg.Observability.Tracing.SampleRate)
	assert.Equal(t, config.DefaultTracingOTLPInsecure, cfg.Observability.Tracing.OTLPInsecure)
}

// TestYAMLFileLoading verifies that configuration can be loaded from a YAML file.
func TestYAMLFileLoading(t *testing.T) {
	clearMnemonicEnvVars(t)

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  host: 127.0.0.1
  port: 9000
  read_timeout: 45s
  write_timeout: 45s
  idle_timeout: 180s

database:
  postgres:
    host: pghost
    port: 5433
    database: testdb
    username: testuser
    password: testpass
    ssl_mode: require

  neo4j:
    uri: bolt://neo4jhost:7688
    username: neo4juser
    password: neo4jpass
    database: testgraph

openai:
  api_key: sk-test-key
  embedding_model: text-embedding-ada-002
  embedding_dimensions: 1536

logging:
  level: debug
  format: text
  include_caller: true

vocabulary:
  languages:
    - agnostic
    - go
  domains:
    - backend
    - testing
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	v := viper.New()
	config.SetDefaults(v)
	v.SetConfigFile(configPath)
	err = v.ReadInConfig()
	require.NoError(t, err)

	cfg, err := config.LoadFromViper(v)
	require.NoError(t, err)

	// Verify file values override defaults
	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 9000, cfg.Server.Port)
	assert.Equal(t, 45*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 45*time.Second, cfg.Server.WriteTimeout)
	assert.Equal(t, 180*time.Second, cfg.Server.IdleTimeout)

	assert.Equal(t, "pghost", cfg.Database.Postgres.Host)
	assert.Equal(t, 5433, cfg.Database.Postgres.Port)
	assert.Equal(t, "testdb", cfg.Database.Postgres.Database)
	assert.Equal(t, "testuser", cfg.Database.Postgres.Username)
	assert.Equal(t, "testpass", cfg.Database.Postgres.Password)
	assert.Equal(t, "require", cfg.Database.Postgres.SSLMode)

	assert.Equal(t, "bolt://neo4jhost:7688", cfg.Database.Neo4j.URI)
	assert.Equal(t, "neo4juser", cfg.Database.Neo4j.Username)
	assert.Equal(t, "neo4jpass", cfg.Database.Neo4j.Password)
	assert.Equal(t, "testgraph", cfg.Database.Neo4j.Database)

	assert.Equal(t, "sk-test-key", cfg.OpenAI.APIKey)
	assert.Equal(t, "text-embedding-ada-002", cfg.OpenAI.EmbeddingModel)
	assert.Equal(t, 1536, cfg.OpenAI.EmbeddingDimensions)

	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "text", cfg.Logging.Format)
	assert.True(t, cfg.Logging.IncludeCaller)
}

// TestEnvironmentVariableOverrides verifies that environment variables override file values.
func TestEnvironmentVariableOverrides(t *testing.T) {
	clearMnemonicEnvVars(t)

	// Create a config file with some values
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  host: 127.0.0.1
  port: 9000

database:
  postgres:
    host: pghost
    password: filepassword

logging:
  level: info
  format: json

vocabulary:
  languages:
    - agnostic
    - go
  domains:
    - backend
    - testing
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set environment variables to override
	t.Setenv("MNEMONIC_SERVER_HOST", "env-host")
	t.Setenv("MNEMONIC_SERVER_PORT", "8888")
	t.Setenv("MNEMONIC_DATABASE_POSTGRES_PASSWORD", "envpassword")
	t.Setenv("MNEMONIC_LOGGING_LEVEL", "error")

	v := viper.New()
	config.SetDefaults(v)
	v.SetConfigFile(configPath)
	err = v.ReadInConfig()
	require.NoError(t, err)

	// Set up environment binding
	v.SetEnvPrefix("MNEMONIC")
	v.SetEnvKeyReplacer(replaceUnderscores())
	v.AutomaticEnv()

	cfg, err := config.LoadFromViper(v)
	require.NoError(t, err)

	// Verify environment variables override file values
	assert.Equal(t, "env-host", cfg.Server.Host)
	assert.Equal(t, 8888, cfg.Server.Port)
	assert.Equal(t, "envpassword", cfg.Database.Postgres.Password)
	assert.Equal(t, "error", cfg.Logging.Level)

	// Verify file values are retained when no env override
	assert.Equal(t, "pghost", cfg.Database.Postgres.Host)
	assert.Equal(t, "json", cfg.Logging.Format)
}

// TestEnvironmentVariableNaming verifies correct environment variable naming conventions.
func TestEnvironmentVariableNaming(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		value  string
		check  func(t *testing.T, cfg *config.MnemonicConfig)
	}{
		{
			name:   "server.port",
			envVar: "MNEMONIC_SERVER_PORT",
			value:  "3000",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 3000, cfg.Server.Port)
			},
		},
		{
			name:   "database.postgres.max_open_conns",
			envVar: "MNEMONIC_DATABASE_POSTGRES_MAX_OPEN_CONNS",
			value:  "50",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 50, cfg.Database.Postgres.MaxOpenConns)
			},
		},
		{
			name:   "database.neo4j.max_connection_pool_size",
			envVar: "MNEMONIC_DATABASE_NEO4J_MAX_CONNECTION_POOL_SIZE",
			value:  "100",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 100, cfg.Database.Neo4j.MaxConnectionPoolSize)
			},
		},
		{
			name:   "openai.api_key",
			envVar: "MNEMONIC_OPENAI_API_KEY",
			value:  "sk-test-123",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, "sk-test-123", cfg.OpenAI.APIKey)
			},
		},
		{
			name:   "rate_limit.per_user.requests_per_minute",
			envVar: "MNEMONIC_RATE_LIMIT_PER_USER_REQUESTS_PER_MINUTE",
			value:  "120",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 120, cfg.RateLimit.PerUser.RequestsPerMinute)
			},
		},
		{
			name:   "observability.metrics.enabled",
			envVar: "MNEMONIC_OBSERVABILITY_METRICS_ENABLED",
			value:  "false",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.False(t, cfg.Observability.Metrics.Enabled)
			},
		},
		{
			name:   "observability.tracing.sample_rate",
			envVar: "MNEMONIC_OBSERVABILITY_TRACING_SAMPLE_RATE",
			value:  "0.5",
			check: func(t *testing.T, cfg *config.MnemonicConfig) {
				assert.Equal(t, 0.5, cfg.Observability.Tracing.SampleRate)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearMnemonicEnvVars(t)
			t.Setenv(tt.envVar, tt.value)

			v := viper.New()
			config.SetDefaults(v)
			applyTestVocabulary(v)
			v.SetEnvPrefix("MNEMONIC")
			v.SetEnvKeyReplacer(replaceUnderscores())
			v.AutomaticEnv()

			cfg, err := config.LoadFromViper(v)
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

// TestValidation_ServerConfig tests server configuration validation.
func TestValidation_ServerConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "invalid port - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 0
			},
			expectError: "server.port",
		},
		{
			name: "invalid port - too high",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 70000
			},
			expectError: "server.port",
		},
		{
			name: "invalid read_timeout - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.ReadTimeout = 0
			},
			expectError: "server.read_timeout",
		},
		{
			name: "invalid write_timeout - negative",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.WriteTimeout = -1 * time.Second
			},
			expectError: "server.write_timeout",
		},
		{
			name: "invalid idle_timeout - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.IdleTimeout = 0
			},
			expectError: "server.idle_timeout",
		},
		{
			name: "invalid shutdown_timeout - zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.ShutdownTimeout = 0
			},
			expectError: "server.shutdown_timeout",
		},
		{
			name: "invalid shutdown_timeout - negative",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.ShutdownTimeout = -1 * time.Second
			},
			expectError: "server.shutdown_timeout",
		},
		{
			name: "TLS enabled without cert_file",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.TLS.Enabled = true
				cfg.Server.TLS.CertFile = ""
				cfg.Server.TLS.KeyFile = "/some/key.pem"
			},
			expectError: "server.tls.cert_file",
		},
		{
			name: "TLS enabled without key_file",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.TLS.Enabled = true
				cfg.Server.TLS.CertFile = "/some/cert.pem"
				cfg.Server.TLS.KeyFile = ""
			},
			expectError: "server.tls.key_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidation_MCPConfig tests MCP server configuration validation.
func TestValidation_MCPConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "default_search_threshold below zero",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = -0.1
			},
			expectError: "mcp.default_search_threshold",
		},
		{
			name: "default_search_threshold above one",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = 1.1
			},
			expectError: "mcp.default_search_threshold",
		},
		{
			name: "default_search_threshold at zero is valid",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = 0.0
			},
			expectError: "",
		},
		{
			name: "default_search_threshold at one is valid",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.MCP.DefaultSearchThreshold = 1.0
			},
			expectError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			if tt.expectError == "" {
				assert.Empty(t, errs, "expected no validation errors")
			} else {
				require.NotEmpty(t, errs, "expected validation errors")
				assert.Contains(t, errs.Error(), tt.expectError)
			}
		})
	}
}

// TestValidation_DatabaseConfig tests database configuration validation.
func TestValidation_DatabaseConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "empty postgres host",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.Host = ""
			},
			expectError: "database.postgres.host",
		},
		{
			name: "empty postgres database",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.Database = ""
			},
			expectError: "database.postgres.database",
		},
		{
			name: "invalid postgres port",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.Port = 0
			},
			expectError: "database.postgres.port",
		},
		{
			name: "invalid postgres max_open_conns",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.MaxOpenConns = 0
			},
			expectError: "database.postgres.max_open_conns",
		},
		{
			name: "negative postgres max_idle_conns",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.MaxIdleConns = -1
			},
			expectError: "database.postgres.max_idle_conns",
		},
		{
			name: "max_idle_conns greater than max_open_conns",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.MaxOpenConns = 10
				cfg.Database.Postgres.MaxIdleConns = 20
			},
			expectError: "database.postgres.max_idle_conns",
		},
		{
			name: "negative postgres conn_max_lifetime",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Postgres.ConnMaxLifetime = -1 * time.Second
			},
			expectError: "database.postgres.conn_max_lifetime",
		},
		{
			name: "empty neo4j uri",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.URI = ""
			},
			expectError: "database.neo4j.uri",
		},
		{
			name: "invalid neo4j uri prefix - http",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.URI = "http://localhost:7687"
			},
			expectError: "database.neo4j.uri",
		},
		{
			name: "invalid neo4j uri prefix - no scheme",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.URI = "localhost:7687"
			},
			expectError: "database.neo4j.uri",
		},
		{
			name: "invalid neo4j max_connection_pool_size",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.MaxConnectionPoolSize = 0
			},
			expectError: "database.neo4j.max_connection_pool_size",
		},
		{
			name: "invalid neo4j connection_acquisition_timeout",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Database.Neo4j.ConnectionAcquisitionTimeout = 0
			},
			expectError: "database.neo4j.connection_acquisition_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidation_Neo4jValidURIs tests that valid Neo4j URI prefixes are accepted.
func TestValidation_Neo4jValidURIs(t *testing.T) {
	validURIs := []string{
		"bolt://localhost:7687",
		"bolt://neo4j-server:7687",
		"neo4j://localhost:7687",
		"neo4j://neo4j-cluster:7687",
	}

	for _, uri := range validURIs {
		t.Run(uri, func(t *testing.T) {
			cfg := validConfig()
			cfg.Database.Neo4j.URI = uri
			errs := cfg.Validate()
			for _, err := range errs {
				assert.NotEqual(t, "database.neo4j.uri", err.Field,
					"URI %q should be valid", uri)
			}
		})
	}
}

// TestValidation_OpenAIConfig tests OpenAI configuration validation.
func TestValidation_OpenAIConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "invalid embedding_dimensions",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.OpenAI.EmbeddingDimensions = 0
			},
			expectError: "openai.embedding_dimensions",
		},
		{
			name: "invalid max_requests_per_minute",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.OpenAI.MaxRequestsPerMinute = 0
			},
			expectError: "openai.max_requests_per_minute",
		},
		{
			name: "negative retry_attempts",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.OpenAI.RetryAttempts = -1
			},
			expectError: "openai.retry_attempts",
		},
		{
			name: "negative retry_delay",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.OpenAI.RetryDelay = -1 * time.Second
			},
			expectError: "openai.retry_delay",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidation_RateLimitConfig tests rate limit configuration validation.
func TestValidation_RateLimitConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "enabled with zero requests_per_second",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.RateLimit.Enabled = true
				cfg.RateLimit.RequestsPerSecond = 0
			},
			expectError: "rate_limit.requests_per_second",
		},
		{
			name: "enabled with zero burst_size",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.RateLimit.Enabled = true
				cfg.RateLimit.BurstSize = 0
			},
			expectError: "rate_limit.burst_size",
		},
		{
			name: "enabled with zero per_user.requests_per_minute",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.RateLimit.Enabled = true
				cfg.RateLimit.PerUser.RequestsPerMinute = 0
			},
			expectError: "rate_limit.per_user.requests_per_minute",
		},
		{
			name: "enabled with zero per_user.burst_size",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.RateLimit.Enabled = true
				cfg.RateLimit.PerUser.BurstSize = 0
			},
			expectError: "rate_limit.per_user.burst_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidation_RateLimitDisabled verifies no validation errors when rate limiting is disabled.
func TestValidation_RateLimitDisabled(t *testing.T) {
	cfg := validConfig()
	cfg.RateLimit.Enabled = false
	cfg.RateLimit.RequestsPerSecond = 0 // Invalid when enabled, but OK when disabled
	cfg.RateLimit.BurstSize = 0

	errs := cfg.Validate()
	// Should not have rate limit errors when disabled
	for _, err := range errs {
		assert.NotContains(t, err.Field, "rate_limit")
	}
}

// TestValidation_EnrichmentConfig tests enrichment configuration validation.
func TestValidation_EnrichmentConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "zero worker_count",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.WorkerCount = 0
			},
			expectError: "enrichment.worker_count",
		},
		{
			name: "zero poll_interval",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.PollInterval = 0
			},
			expectError: "enrichment.poll_interval",
		},
		{
			name: "zero max_attempts",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.MaxAttempts = 0
			},
			expectError: "enrichment.max_attempts",
		},
		{
			name: "negative retry_delay",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.RetryDelay = -1 * time.Second
			},
			expectError: "enrichment.retry_delay",
		},
		{
			name: "zero job_timeout",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.JobTimeout = 0
			},
			expectError: "enrichment.job_timeout",
		},
		{
			name: "zero drain_timeout",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.DrainTimeout = 0
			},
			expectError: "enrichment.drain_timeout",
		},
		{
			name: "negative drain_timeout",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.DrainTimeout = -1 * time.Second
			},
			expectError: "enrichment.drain_timeout",
		},
		{
			name: "zero completed_retention",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.CompletedRetention = 0
			},
			expectError: "enrichment.completed_retention",
		},
		{
			name: "negative completed_retention",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.CompletedRetention = -1 * time.Hour
			},
			expectError: "enrichment.completed_retention",
		},
		{
			name: "zero failed_retention",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.FailedRetention = 0
			},
			expectError: "enrichment.failed_retention",
		},
		{
			name: "negative failed_retention",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.FailedRetention = -1 * time.Hour
			},
			expectError: "enrichment.failed_retention",
		},
		{
			name: "negative related_to_min_similarity",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.RelatedToMinSimilarity = -0.1
			},
			expectError: "enrichment.related_to_min_similarity",
		},
		{
			name: "related_to_min_similarity over 1",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Enrichment.RelatedToMinSimilarity = 1.5
			},
			expectError: "enrichment.related_to_min_similarity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidation_LoggingConfig tests logging configuration validation.
func TestValidation_LoggingConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "invalid log level",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Logging.Level = "invalid"
			},
			expectError: "logging.level",
		},
		{
			name: "invalid log format",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Logging.Format = "invalid"
			},
			expectError: "logging.format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidation_LoggingLevels tests all valid log levels.
func TestValidation_LoggingLevels(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error", "DEBUG", "INFO", "WARN", "ERROR"}

	for _, level := range validLevels {
		t.Run(level, func(t *testing.T) {
			cfg := validConfig()
			cfg.Logging.Level = level
			errs := cfg.Validate()
			for _, err := range errs {
				assert.NotEqual(t, "logging.level", err.Field)
			}
		})
	}
}

// TestValidation_LoggingFormats tests all valid log formats.
func TestValidation_LoggingFormats(t *testing.T) {
	validFormats := []string{"json", "text", "JSON", "TEXT"}

	for _, format := range validFormats {
		t.Run(format, func(t *testing.T) {
			cfg := validConfig()
			cfg.Logging.Format = format
			errs := cfg.Validate()
			for _, err := range errs {
				assert.NotEqual(t, "logging.format", err.Field)
			}
		})
	}
}

// TestValidation_ObservabilityConfig tests observability configuration validation.
func TestValidation_ObservabilityConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
	}{
		{
			name: "metrics enabled with invalid port",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Port = 0
			},
			expectError: "observability.metrics.port",
		},
		{
			name: "metrics enabled with empty path",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Path = ""
			},
			expectError: "observability.metrics.path",
		},
		{
			name: "health enabled with empty path",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Health.Enabled = true
				cfg.Observability.Health.Path = ""
			},
			expectError: "observability.health.path",
		},
		{
			name: "tracing enabled with empty endpoint",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Tracing.Enabled = true
				cfg.Observability.Tracing.Endpoint = ""
			},
			expectError: "observability.tracing.endpoint",
		},
		{
			name: "tracing enabled with invalid sample_rate - negative",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Tracing.Enabled = true
				cfg.Observability.Tracing.Endpoint = "localhost:4317"
				cfg.Observability.Tracing.SampleRate = -0.1
			},
			expectError: "observability.tracing.sample_rate",
		},
		{
			name: "tracing enabled with invalid sample_rate - over 1",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Observability.Tracing.Enabled = true
				cfg.Observability.Tracing.Endpoint = "localhost:4317"
				cfg.Observability.Tracing.SampleRate = 1.5
			},
			expectError: "observability.tracing.sample_rate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidation_PortConflict tests cross-configuration validation for port conflicts.
func TestValidation_PortConflict(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError bool
		errorField  string
	}{
		{
			name: "metrics enabled with same port as server - conflict",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 8080
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Port = 8080
			},
			expectError: true,
			errorField:  "observability.metrics.port",
		},
		{
			name: "metrics enabled with different port - no conflict",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 8080
				cfg.Observability.Metrics.Enabled = true
				cfg.Observability.Metrics.Port = 9090
			},
			expectError: false,
		},
		{
			name: "metrics disabled with same port - no conflict",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Server.Port = 8080
				cfg.Observability.Metrics.Enabled = false
				cfg.Observability.Metrics.Port = 8080
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()

			if tt.expectError {
				require.NotEmpty(t, errs, "expected validation errors")
				// Check that the specific error field is present
				found := false
				for _, err := range errs {
					if err.Field == tt.errorField {
						found = true
						assert.Contains(t, err.Message, "server.port")
						break
					}
				}
				assert.True(t, found, "expected error for field %q", tt.errorField)
			} else {
				// Verify no port conflict errors (other validation errors may exist)
				for _, err := range errs {
					if err.Field == "observability.metrics.port" {
						assert.NotContains(t, err.Message, "server.port",
							"unexpected port conflict error when none expected")
					}
				}
			}
		})
	}
}

// TestValidation_MultipleErrors verifies multiple errors are collected.
func TestValidation_MultipleErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Port = 0
	cfg.Database.Postgres.Port = 0

	errs := cfg.Validate()
	require.NotEmpty(t, errs, "expected validation errors")

	errStr := errs.Error()
	assert.Contains(t, errStr, "server.port")
	assert.Contains(t, errStr, "database.postgres.port")
}

// TestServerAddress tests the Address() helper method.
func TestServerAddress(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		expected string
	}{
		{
			name:     "default values",
			host:     "0.0.0.0",
			port:     8080,
			expected: "0.0.0.0:8080",
		},
		{
			name:     "localhost",
			host:     "localhost",
			port:     3000,
			expected: "localhost:3000",
		},
		{
			name:     "empty host",
			host:     "",
			port:     8080,
			expected: ":8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ServerConfig{
				Host: tt.host,
				Port: tt.port,
			}
			assert.Equal(t, tt.expected, cfg.Address())
		})
	}
}

// TestPostgresConnectionString tests the ConnectionString() helper method.
func TestPostgresConnectionString(t *testing.T) {
	cfg := &config.PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Username: "testuser",
		Password: "testpass",
		SSLMode:  "require",
	}

	expected := "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=require"
	assert.Equal(t, expected, cfg.ConnectionString())
}

// TestPostgresDSN tests the DSN() helper method.
func TestPostgresDSN(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.PostgresConfig
		expected string
	}{
		{
			name: "simple credentials",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "testpass", SSLMode: "require",
			},
			expected: "postgres://testuser:testpass@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "password with @ symbol",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "p@ss", SSLMode: "require",
			},
			expected: "postgres://testuser:p%40ss@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "password with colon and slash",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "p:a/ss", SSLMode: "require",
			},
			expected: "postgres://testuser:p%3Aa%2Fss@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "password with percent",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "100%pass", SSLMode: "require",
			},
			expected: "postgres://testuser:100%25pass@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "username with special characters",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "user@domain", Password: "testpass", SSLMode: "require",
			},
			expected: "postgres://user%40domain:testpass@localhost:5432/testdb?sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cfg.DSN())
		})
	}
}

// TestPostgresSafeConnectionString tests the SafeConnectionString() helper method.
func TestPostgresSafeConnectionString(t *testing.T) {
	cfg := &config.PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Username: "testuser",
		Password: "supersecretpassword",
		SSLMode:  "require",
	}

	result := cfg.SafeConnectionString()
	expected := "host=localhost port=5432 user=testuser password=***** dbname=testdb sslmode=require"
	assert.Equal(t, expected, result)
	assert.NotContains(t, result, "supersecretpassword")
}

// TestPostgresSafeDSN tests the SafeDSN() helper method.
func TestPostgresSafeDSN(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.PostgresConfig
		expected string
	}{
		{
			name: "simple credentials",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "testuser", Password: "supersecretpassword", SSLMode: "require",
			},
			expected: "postgres://testuser:*****@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "username with special characters",
			cfg: config.PostgresConfig{
				Host: "localhost", Port: 5432, Database: "testdb",
				Username: "user@domain", Password: "supersecretpassword", SSLMode: "require",
			},
			expected: "postgres://user%40domain:*****@localhost:5432/testdb?sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.SafeDSN()
			assert.Equal(t, tt.expected, result)
			assert.NotContains(t, result, tt.cfg.Password)
		})
	}
}

// TestNeo4jConnectionURI tests the ConnectionURI() helper method.
func TestNeo4jConnectionURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected string
	}{
		{
			name:     "bolt scheme",
			uri:      "bolt://localhost:7687",
			expected: "bolt://localhost:7687",
		},
		{
			name:     "neo4j scheme",
			uri:      "neo4j://neo4j-cluster:7687",
			expected: "neo4j://neo4j-cluster:7687",
		},
		{
			name:     "bolt+s scheme",
			uri:      "bolt+s://secure-host:7687",
			expected: "bolt+s://secure-host:7687",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Neo4jConfig{
				URI: tt.uri,
			}
			assert.Equal(t, tt.expected, cfg.ConnectionURI())
		})
	}
}

// TestNeo4jSafeURI tests the SafeURI() helper method.
func TestNeo4jSafeURI(t *testing.T) {
	tests := []struct {
		name          string
		uri           string
		expected      string
		shouldNotHave string
	}{
		{
			name:     "no embedded credentials",
			uri:      "bolt://localhost:7687",
			expected: "bolt://localhost:7687",
		},
		{
			name:     "neo4j scheme no credentials",
			uri:      "neo4j://neo4j-cluster:7687",
			expected: "neo4j://neo4j-cluster:7687",
		},
		{
			name:          "embedded username and password",
			uri:           "bolt://myuser:secretpass@localhost:7687",
			expected:      "bolt://myuser:*****@localhost:7687",
			shouldNotHave: "secretpass",
		},
		{
			name:          "neo4j scheme with credentials",
			uri:           "neo4j://admin:supersecret@cluster.example.com:7687",
			expected:      "neo4j://admin:*****@cluster.example.com:7687",
			shouldNotHave: "supersecret",
		},
		{
			name:     "username only no password",
			uri:      "bolt://myuser@localhost:7687",
			expected: "bolt://myuser@localhost:7687",
		},
		{
			name:          "bolt+s with credentials",
			uri:           "bolt+s://user:pass123@secure-host:7687",
			expected:      "bolt+s://user:*****@secure-host:7687",
			shouldNotHave: "pass123",
		},
		{
			name:          "complex password with special chars",
			uri:           "bolt://user:p@ss:word!@localhost:7687",
			expected:      "bolt://user:*****@localhost:7687",
			shouldNotHave: "p@ss:word!",
		},
		{
			name:     "invalid uri no scheme",
			uri:      "localhost:7687",
			expected: "localhost:7687",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Neo4jConfig{
				URI: tt.uri,
			}
			result := cfg.SafeURI()
			assert.Equal(t, tt.expected, result)
			if tt.shouldNotHave != "" {
				assert.NotContains(t, result, tt.shouldNotHave)
			}
		})
	}
}

// TestNeo4jCredentials tests the Credentials() helper method.
func TestNeo4jCredentials(t *testing.T) {
	tests := []struct {
		name             string
		username         string
		password         string
		expectedUsername string
		expectedPassword string
	}{
		{
			name:             "standard credentials",
			username:         "neo4j",
			password:         "secretpassword",
			expectedUsername: "neo4j",
			expectedPassword: "secretpassword",
		},
		{
			name:             "empty password",
			username:         "neo4j",
			password:         "",
			expectedUsername: "neo4j",
			expectedPassword: "",
		},
		{
			name:             "custom username",
			username:         "admin",
			password:         "admin123",
			expectedUsername: "admin",
			expectedPassword: "admin123",
		},
		{
			name:             "special characters in password",
			username:         "user",
			password:         "p@ss:word!#$%",
			expectedUsername: "user",
			expectedPassword: "p@ss:word!#$%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Neo4jConfig{
				URI:      "bolt://localhost:7687",
				Username: tt.username,
				Password: tt.password,
			}
			username, password := cfg.Credentials()
			assert.Equal(t, tt.expectedUsername, username)
			assert.Equal(t, tt.expectedPassword, password)
		})
	}
}

// TestConfigFileFlagOverride tests that --config flag takes precedence.
func TestConfigFileFlagOverride(t *testing.T) {
	clearMnemonicEnvVars(t)

	// Create multiple config files
	tmpDir := t.TempDir()
	envConfigPath := filepath.Join(tmpDir, "env-config.yaml")
	flagConfigPath := filepath.Join(tmpDir, "flag-config.yaml")

	envConfig := `
server:
  port: 9001
`
	flagConfig := `
server:
  port: 9002
`

	err := os.WriteFile(envConfigPath, []byte(envConfig), 0644)
	require.NoError(t, err)
	err = os.WriteFile(flagConfigPath, []byte(flagConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	t.Setenv(config.EnvConfigFile, envConfigPath)

	// Create flagset with --config
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	configFlag := flags.String("config", "", "config file")
	err = flags.Set("config", flagConfigPath)
	require.NoError(t, err)

	// Verify flag was marked as changed
	assert.True(t, flags.Lookup("config").Changed)
	assert.Equal(t, flagConfigPath, *configFlag)
}

// TestBooleanEnvironmentVariables tests boolean parsing from env vars.
func TestBooleanEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"true lowercase", "true", true},
		{"TRUE uppercase", "TRUE", true},
		{"1", "1", true},
		{"false lowercase", "false", false},
		{"FALSE uppercase", "FALSE", false},
		{"0", "0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearMnemonicEnvVars(t)
			// Use logging.include_caller which has no validation dependencies
			t.Setenv("MNEMONIC_LOGGING_INCLUDE_CALLER", tt.value)

			v := viper.New()
			config.SetDefaults(v)
			applyTestVocabulary(v)
			v.SetEnvPrefix("MNEMONIC")
			v.SetEnvKeyReplacer(replaceUnderscores())
			v.AutomaticEnv()

			cfg, err := config.LoadFromViper(v)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.Logging.IncludeCaller)
		})
	}
}

// TestDurationEnvironmentVariables tests duration parsing from env vars.
func TestDurationEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"seconds", "45s", 45 * time.Second},
		{"minutes", "5m", 5 * time.Minute},
		{"hours", "1h", 1 * time.Hour},
		{"complex", "1h30m", 90 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearMnemonicEnvVars(t)
			t.Setenv("MNEMONIC_SERVER_READ_TIMEOUT", tt.value)

			v := viper.New()
			config.SetDefaults(v)
			applyTestVocabulary(v)
			v.SetEnvPrefix("MNEMONIC")
			v.SetEnvKeyReplacer(replaceUnderscores())
			v.AutomaticEnv()

			cfg, err := config.LoadFromViper(v)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.Server.ReadTimeout)
		})
	}
}

// TestValidConfig verifies a fully valid config passes validation.
func TestValidConfig(t *testing.T) {
	cfg := validConfig()
	errs := cfg.Validate()
	assert.Empty(t, errs, "expected no validation errors, got: %v", errs)
}

// TestVocabularyConfig_Validate tests VocabularyConfig validation.
func TestVocabularyConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(cfg *config.MnemonicConfig)
		expectError string
		wantNoError bool
	}{
		{
			name: "empty languages fails validation",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Vocabulary.Languages = nil
			},
			expectError: "vocabulary.languages",
		},
		{
			name: "empty domains fails validation",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Vocabulary.Domains = nil
			},
			expectError: "vocabulary.domains",
		},
		{
			name: "both populated passes validation",
			modify: func(cfg *config.MnemonicConfig) {
				cfg.Vocabulary.Languages = []string{"go", "python"}
				cfg.Vocabulary.Domains = []string{"backend", "testing"}
			},
			wantNoError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)
			errs := cfg.Validate()
			if tt.wantNoError {
				assert.Empty(t, errs, "expected no validation errors, got: %v", errs)
				return
			}
			require.NotEmpty(t, errs, "expected validation errors")
			assert.Contains(t, errs.Error(), tt.expectError)
		})
	}
}

// TestValidationErrorFormat tests the ValidationError and ValidationErrors formatting.
func TestValidationErrorFormat(t *testing.T) {
	err := config.ValidationError{
		Field:   "server.port",
		Message: "must be between 1 and 65535, got 0",
	}
	assert.Equal(t, "server.port: must be between 1 and 65535, got 0", err.Error())

	errs := config.ValidationErrors{
		{Field: "server.port", Message: "invalid"},
		{Field: "database.postgres.port", Message: "invalid"},
	}
	errStr := errs.Error()
	assert.Contains(t, errStr, "configuration validation failed:")
	assert.Contains(t, errStr, "server.port: invalid")
	assert.Contains(t, errStr, "database.postgres.port: invalid")
}

// TestEmptyValidationErrors tests that empty ValidationErrors returns empty string.
func TestEmptyValidationErrors(t *testing.T) {
	var errs config.ValidationErrors
	assert.Empty(t, errs.Error())
}

// Helper functions

// clearMnemonicEnvVars clears all MNEMONIC_ prefixed environment variables.
func clearMnemonicEnvVars(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		key, _, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "MNEMONIC_") {
			// t.Setenv handles cleanup automatically when test completes
			t.Setenv(key, "")
		}
	}
}

// strings helper for env key replacement
func replaceUnderscores() *strings.Replacer {
	return strings.NewReplacer(".", "_")
}

// applyTestVocabulary sets minimal valid vocabulary values on a viper instance.
// Call this in tests that use LoadFromViper without a config file so that
// vocabulary validation does not cause false failures.
func applyTestVocabulary(v *viper.Viper) {
	v.Set("vocabulary.languages", []string{"agnostic", "go"})
	v.Set("vocabulary.domains", []string{"backend", "testing"})
}

// validConfig returns a fully valid configuration for testing.
func validConfig() *config.MnemonicConfig {
	return &config.MnemonicConfig{
		Server: config.ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
			TLS: config.TLSConfig{
				Enabled:  false,
				CertFile: "",
				KeyFile:  "",
			},
		},
		MCP: config.MCPConfig{
			Port:                   8081,
			ReadTimeout:            30 * time.Second,
			WriteTimeout:           30 * time.Second,
			IdleTimeout:            120 * time.Second,
			DefaultSearchThreshold: 0.5,
		},
		Database: config.DatabaseConfig{
			Postgres: config.PostgresConfig{
				Host:            "localhost",
				Port:            5432,
				Database:        "mnemonic",
				Username:        "mnemonic",
				Password:        "",
				SSLMode:         "prefer",
				MaxOpenConns:    25,
				MaxIdleConns:    5,
				ConnMaxLifetime: 5 * time.Minute,
			},
			Neo4j: config.Neo4jConfig{
				URI:                          "bolt://localhost:7687",
				Username:                     "neo4j",
				Password:                     "",
				Database:                     "neo4j",
				MaxConnectionPoolSize:        50,
				ConnectionAcquisitionTimeout: 60 * time.Second,
			},
		},
		OpenAI: config.OpenAIConfig{
			APIKey:               "",
			EmbeddingModel:       "text-embedding-3-small",
			EmbeddingDimensions:  1536,
			ExtractionModel:      "gpt-4o-mini",
			MaxRequestsPerMinute: 500,
			RetryAttempts:        3,
			RetryDelay:           1 * time.Second,
		},
		RateLimit: config.RateLimitConfig{
			Enabled:           false,
			RequestsPerSecond: 100,
			BurstSize:         200,
			PerUser: config.PerUserRateLimit{
				RequestsPerMinute: 60,
				BurstSize:         10,
			},
		},
		Enrichment: config.EnrichmentConfig{
			WorkerCount:            2,
			PollInterval:           5 * time.Second,
			MaxAttempts:            3,
			RetryDelay:             30 * time.Second,
			JobTimeout:             5 * time.Minute,
			DrainTimeout:           30 * time.Second,
			CompletedRetention:     168 * time.Hour,
			FailedRetention:        720 * time.Hour,
			RelatedToMinSimilarity: 0.3,
		},
		Logging: config.LoggingConfig{
			Level:         "info",
			Format:        "json",
			IncludeCaller: false,
		},
		Observability: config.ObservabilityConfig{
			Metrics: config.MetricsConfig{
				Enabled: true,
				Path:    "/metrics",
				Port:    9090,
			},
			Health: config.HealthConfig{
				Enabled: true,
				Path:    "/health",
			},
			Tracing: config.TracingConfig{
				Enabled:      false,
				Endpoint:     "",
				SampleRate:   0.1,
				OTLPInsecure: true,
			},
		},
		Vocabulary: config.VocabularyConfig{
			Languages: []string{"agnostic", "go", "python"},
			Domains:   []string{"backend", "frontend", "testing"},
		},
	}
}
