package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// MnemonicConfig is the top-level configuration structure for the Mnemonic server.
type MnemonicConfig struct {
	Server        ServerConfig        `mapstructure:"server"`
	MCP           MCPConfig           `mapstructure:"mcp"`
	Database      DatabaseConfig      `mapstructure:"database"`
	OpenAI        OpenAIConfig        `mapstructure:"openai"`
	RateLimit     RateLimitConfig     `mapstructure:"rate_limit"`
	Enrichment    EnrichmentConfig    `mapstructure:"enrichment"`
	Logging       LoggingConfig       `mapstructure:"logging"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Vocabulary    VocabularyConfig    `mapstructure:"vocabulary"`
}

// VocabularyConfig holds the allowed values for pattern language and domain fields.
type VocabularyConfig struct {
	Languages []string `mapstructure:"languages"`
	Domains   []string `mapstructure:"domains"`
}

// MCPConfig contains MCP server settings.
type MCPConfig struct {
	Port                   int           `mapstructure:"port"`
	ReadTimeout            time.Duration `mapstructure:"read_timeout"`
	WriteTimeout           time.Duration `mapstructure:"write_timeout"`
	IdleTimeout            time.Duration `mapstructure:"idle_timeout"`
	DefaultSearchThreshold float64       `mapstructure:"default_search_threshold"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	TLS             TLSConfig     `mapstructure:"tls"`
}

// TLSConfig contains TLS settings for the server.
type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

// DatabaseConfig contains database connection settings.
type DatabaseConfig struct {
	Postgres PostgresConfig `mapstructure:"postgres"`
	Neo4j    Neo4jConfig    `mapstructure:"neo4j"`
}

// PostgresConfig contains PostgreSQL connection settings.
type PostgresConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Database        string        `mapstructure:"database"`
	Username        string        `mapstructure:"username"`
	Password        string        `mapstructure:"password"` // #nosec G117 -- credentials loaded from config/env, not serialized
	SSLMode         string        `mapstructure:"ssl_mode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// Neo4jConfig contains Neo4j connection settings.
type Neo4jConfig struct {
	URI                          string        `mapstructure:"uri"`
	Username                     string        `mapstructure:"username"`
	Password                     string        `mapstructure:"password"` // #nosec G117 -- credentials loaded from config/env, not serialized
	Database                     string        `mapstructure:"database"`
	MaxConnectionPoolSize        int           `mapstructure:"max_connection_pool_size"`
	ConnectionAcquisitionTimeout time.Duration `mapstructure:"connection_acquisition_timeout"`
}

// OpenAIConfig contains OpenAI API settings.
type OpenAIConfig struct {
	APIKey               string        `mapstructure:"api_key"` // #nosec G117 -- credentials loaded from config/env, not serialized
	EmbeddingModel       string        `mapstructure:"embedding_model"`
	EmbeddingDimensions  int           `mapstructure:"embedding_dimensions"`
	ExtractionModel      string        `mapstructure:"extraction_model"`
	MaxRequestsPerMinute int           `mapstructure:"max_requests_per_minute"`
	RetryAttempts        int           `mapstructure:"retry_attempts"`
	RetryDelay           time.Duration `mapstructure:"retry_delay"`
}

// RateLimitConfig contains rate limiting settings.
type RateLimitConfig struct {
	Enabled           bool             `mapstructure:"enabled"`
	RequestsPerSecond int              `mapstructure:"requests_per_second"`
	BurstSize         int              `mapstructure:"burst_size"`
	PerUser           PerUserRateLimit `mapstructure:"per_user"`
}

// PerUserRateLimit contains per-user rate limiting settings.
type PerUserRateLimit struct {
	RequestsPerMinute int `mapstructure:"requests_per_minute"`
	BurstSize         int `mapstructure:"burst_size"`
}

// EnrichmentConfig contains enrichment worker settings.
type EnrichmentConfig struct {
	WorkerCount            int           `mapstructure:"worker_count"`
	PollInterval           time.Duration `mapstructure:"poll_interval"`
	MaxAttempts            int           `mapstructure:"max_attempts"`
	RetryDelay             time.Duration `mapstructure:"retry_delay"`
	JobTimeout             time.Duration `mapstructure:"job_timeout"`
	DrainTimeout           time.Duration `mapstructure:"drain_timeout"`
	CompletedRetention     time.Duration `mapstructure:"completed_retention"`
	FailedRetention        time.Duration `mapstructure:"failed_retention"`
	RelatedToMinSimilarity float64       `mapstructure:"related_to_min_similarity"`
}

// LoggingConfig contains logging settings.
type LoggingConfig struct {
	Level         string `mapstructure:"level"`
	Format        string `mapstructure:"format"`
	IncludeCaller bool   `mapstructure:"include_caller"`
}

// ObservabilityConfig contains observability settings.
type ObservabilityConfig struct {
	Metrics MetricsConfig `mapstructure:"metrics"`
	Health  HealthConfig  `mapstructure:"health"`
	Tracing TracingConfig `mapstructure:"tracing"`
}

// MetricsConfig contains metrics settings.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
	Port    int    `mapstructure:"port"`
}

// HealthConfig contains health check settings.
type HealthConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

// TracingConfig contains distributed tracing settings.
type TracingConfig struct {
	Enabled      bool    `mapstructure:"enabled"`
	Endpoint     string  `mapstructure:"endpoint"`
	SampleRate   float64 `mapstructure:"sample_rate"`
	OTLPInsecure bool    `mapstructure:"otlp_insecure"`
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("configuration validation failed:\n")
	for _, err := range e {
		fmt.Fprintf(&sb, "  - %s\n", err.Error())
	}
	return sb.String()
}

// Load loads configuration from all sources with the following precedence:
// 1. Compiled defaults (lowest priority)
// 2. Configuration file
// 3. Environment variables (highest priority)
//
// The config file is discovered in the following order:
// 1. --config flag (if provided)
// 2. $MNEMONIC_CONFIG_FILE (if set)
// 3. /etc/mnemonic/config.yaml (production)
// 4. ./config.yaml (development)
func Load() (*MnemonicConfig, error) {
	return LoadWithFlags(nil)
}

// LoadWithFlags loads configuration using the provided flagset.
// Pass nil to use the default flags.
func LoadWithFlags(flags *pflag.FlagSet) (*MnemonicConfig, error) {
	v := viper.New()

	// Set defaults first
	SetDefaults(v)

	// Determine config file path
	configPath := findConfigFile(flags)
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			// Only return error if the config file was explicitly specified
			if isExplicitConfigPath(flags) {
				return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
			}
			// Otherwise, silently continue with defaults + env vars
		}
	}

	// Set up environment variable binding
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Unmarshal into config struct
	cfg := &MnemonicConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, errs
	}

	return cfg, nil
}

// LoadFromViper loads configuration from an already-configured viper instance.
// This is primarily useful for testing.
func LoadFromViper(v *viper.Viper) (*MnemonicConfig, error) {
	cfg := &MnemonicConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, errs
	}

	return cfg, nil
}

// SetDefaults sets all default values in the viper instance.
// This function is exported to allow tests to use the same defaults as production code.
func SetDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.host", DefaultServerHost)
	v.SetDefault("server.port", DefaultServerPort)
	v.SetDefault("server.read_timeout", DefaultServerReadTimeout)
	v.SetDefault("server.write_timeout", DefaultServerWriteTimeout)
	v.SetDefault("server.idle_timeout", DefaultServerIdleTimeout)
	v.SetDefault("server.shutdown_timeout", DefaultServerShutdownTimeout)
	v.SetDefault("server.tls.enabled", DefaultServerTLSEnabled)
	v.SetDefault("server.tls.cert_file", "")
	v.SetDefault("server.tls.key_file", "")

	// MCP server defaults
	v.SetDefault("mcp.port", DefaultMCPPort)
	v.SetDefault("mcp.read_timeout", DefaultMCPReadTimeout)
	v.SetDefault("mcp.write_timeout", DefaultMCPWriteTimeout)
	v.SetDefault("mcp.idle_timeout", DefaultMCPIdleTimeout)
	v.SetDefault("mcp.default_search_threshold", DefaultMCPDefaultSearchThreshold)

	// PostgreSQL defaults
	v.SetDefault("database.postgres.host", DefaultPostgresHost)
	v.SetDefault("database.postgres.port", DefaultPostgresPort)
	v.SetDefault("database.postgres.database", DefaultPostgresDatabase)
	v.SetDefault("database.postgres.username", DefaultPostgresUsername)
	v.SetDefault("database.postgres.password", "")
	v.SetDefault("database.postgres.ssl_mode", DefaultPostgresSSLMode)
	v.SetDefault("database.postgres.max_open_conns", DefaultPostgresMaxOpenConns)
	v.SetDefault("database.postgres.max_idle_conns", DefaultPostgresMaxIdleConns)
	v.SetDefault("database.postgres.conn_max_lifetime", DefaultPostgresConnMaxLifetime)

	// Neo4j defaults
	v.SetDefault("database.neo4j.uri", DefaultNeo4jURI)
	v.SetDefault("database.neo4j.username", DefaultNeo4jUsername)
	v.SetDefault("database.neo4j.password", "")
	v.SetDefault("database.neo4j.database", DefaultNeo4jDatabase)
	v.SetDefault("database.neo4j.max_connection_pool_size", DefaultNeo4jMaxConnectionPoolSize)
	v.SetDefault("database.neo4j.connection_acquisition_timeout", DefaultNeo4jConnectionAcquisitionTimeout)

	// OpenAI defaults
	v.SetDefault("openai.api_key", "")
	v.SetDefault("openai.embedding_model", DefaultOpenAIEmbeddingModel)
	v.SetDefault("openai.embedding_dimensions", DefaultOpenAIEmbeddingDimensions)
	v.SetDefault("openai.extraction_model", DefaultOpenAIExtractionModel)
	v.SetDefault("openai.max_requests_per_minute", DefaultOpenAIMaxRequestsPerMinute)
	v.SetDefault("openai.retry_attempts", DefaultOpenAIRetryAttempts)
	v.SetDefault("openai.retry_delay", DefaultOpenAIRetryDelay)

	// Rate limit defaults
	v.SetDefault("rate_limit.enabled", DefaultRateLimitEnabled)
	v.SetDefault("rate_limit.requests_per_second", DefaultRateLimitRequestsPerSecond)
	v.SetDefault("rate_limit.burst_size", DefaultRateLimitBurstSize)
	v.SetDefault("rate_limit.per_user.requests_per_minute", DefaultRateLimitPerUserRPM)
	v.SetDefault("rate_limit.per_user.burst_size", DefaultRateLimitPerUserBurst)

	// Enrichment defaults
	v.SetDefault("enrichment.worker_count", DefaultEnrichmentWorkerCount)
	v.SetDefault("enrichment.poll_interval", DefaultEnrichmentPollInterval)
	v.SetDefault("enrichment.max_attempts", DefaultEnrichmentMaxAttempts)
	v.SetDefault("enrichment.retry_delay", DefaultEnrichmentRetryDelay)
	v.SetDefault("enrichment.job_timeout", DefaultEnrichmentJobTimeout)
	v.SetDefault("enrichment.drain_timeout", DefaultEnrichmentDrainTimeout)
	v.SetDefault("enrichment.completed_retention", DefaultEnrichmentCompletedRetention)
	v.SetDefault("enrichment.failed_retention", DefaultEnrichmentFailedRetention)
	v.SetDefault("enrichment.related_to_min_similarity", DefaultEnrichmentRelatedToMinSimilarity)

	// Logging defaults
	v.SetDefault("logging.level", DefaultLoggingLevel)
	v.SetDefault("logging.format", DefaultLoggingFormat)
	v.SetDefault("logging.include_caller", DefaultLoggingIncludeCaller)

	// Vocabulary defaults
	v.SetDefault("vocabulary.languages", DefaultVocabularyLanguages())
	v.SetDefault("vocabulary.domains", DefaultVocabularyDomains())

	// Observability defaults
	v.SetDefault("observability.metrics.enabled", DefaultMetricsEnabled)
	v.SetDefault("observability.metrics.path", DefaultMetricsPath)
	v.SetDefault("observability.metrics.port", DefaultMetricsPort)
	v.SetDefault("observability.health.enabled", DefaultHealthEnabled)
	v.SetDefault("observability.health.path", DefaultHealthPath)
	v.SetDefault("observability.tracing.enabled", DefaultTracingEnabled)
	v.SetDefault("observability.tracing.endpoint", "")
	v.SetDefault("observability.tracing.sample_rate", DefaultTracingSampleRate)
	v.SetDefault("observability.tracing.otlp_insecure", DefaultTracingOTLPInsecure)
}

// findConfigFile determines which config file to use based on the discovery order.
func findConfigFile(flags *pflag.FlagSet) string {
	// 1. Check --config flag
	if flags != nil {
		if configFlag := flags.Lookup("config"); configFlag != nil && configFlag.Changed {
			return configFlag.Value.String()
		}
	}

	// 2. Check MNEMONIC_CONFIG_FILE environment variable
	if envPath := os.Getenv(EnvConfigFile); envPath != "" {
		return envPath
	}

	// 3. Check /etc/mnemonic/config.yaml (production)
	if _, err := os.Stat(ProductionConfigPath); err == nil {
		return ProductionConfigPath
	}

	// 4. Check ./config.yaml (development)
	if _, err := os.Stat(DevelopmentConfigPath); err == nil {
		return DevelopmentConfigPath
	}

	return ""
}

// isExplicitConfigPath returns true if a config path was explicitly provided.
func isExplicitConfigPath(flags *pflag.FlagSet) bool {
	// Check --config flag
	if flags != nil {
		if configFlag := flags.Lookup("config"); configFlag != nil && configFlag.Changed {
			return true
		}
	}

	// Check MNEMONIC_CONFIG_FILE environment variable
	if os.Getenv(EnvConfigFile) != "" {
		return true
	}

	return false
}

// Validate validates the configuration and returns any validation errors.
func (c *MnemonicConfig) Validate() ValidationErrors {
	var errs ValidationErrors

	// Server validation
	errs = append(errs, c.Server.validate()...)

	// MCP validation
	errs = append(errs, c.MCP.validate()...)

	// Database validation
	errs = append(errs, c.Database.validate()...)

	// OpenAI validation
	errs = append(errs, c.OpenAI.validate()...)

	// Rate limit validation
	errs = append(errs, c.RateLimit.validate()...)

	// Enrichment validation
	errs = append(errs, c.Enrichment.validate()...)

	// Logging validation
	errs = append(errs, c.Logging.validate()...)

	// Observability validation
	errs = append(errs, c.Observability.validate()...)

	// Vocabulary validation
	errs = append(errs, c.Vocabulary.validate()...)

	// Cross-configuration validation
	if c.Observability.Metrics.Enabled && c.Server.Port == c.Observability.Metrics.Port {
		errs = append(errs, ValidationError{
			Field:   "observability.metrics.port",
			Message: fmt.Sprintf("must be different from server.port (%d) to avoid port conflict", c.Server.Port),
		})
	}
	if c.Server.Port == c.MCP.Port {
		errs = append(errs, ValidationError{
			Field:   "mcp.port",
			Message: fmt.Sprintf("must be different from server.port (%d) to avoid port conflict", c.Server.Port),
		})
	}
	return errs
}

func (c *ServerConfig) validate() ValidationErrors {
	var errs ValidationErrors

	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "server.port",
			Message: fmt.Sprintf("must be between 1 and 65535, got %d", c.Port),
		})
	}

	if c.ReadTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "server.read_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.WriteTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "server.write_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.IdleTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "server.idle_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.ShutdownTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "server.shutdown_timeout",
			Message: "must be a positive duration",
		})
	}

	// TLS validation
	if c.TLS.Enabled {
		if c.TLS.CertFile == "" {
			errs = append(errs, ValidationError{
				Field:   "server.tls.cert_file",
				Message: "required when TLS is enabled",
			})
		} else if _, err := os.Stat(c.TLS.CertFile); err != nil {
			errs = append(errs, ValidationError{
				Field:   "server.tls.cert_file",
				Message: fmt.Sprintf("cannot access file: %v", err),
			})
		}

		if c.TLS.KeyFile == "" {
			errs = append(errs, ValidationError{
				Field:   "server.tls.key_file",
				Message: "required when TLS is enabled",
			})
		} else if _, err := os.Stat(c.TLS.KeyFile); err != nil {
			errs = append(errs, ValidationError{
				Field:   "server.tls.key_file",
				Message: fmt.Sprintf("cannot access file: %v", err),
			})
		}
	}

	return errs
}

func (c *MCPConfig) validate() ValidationErrors {
	var errs ValidationErrors

	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "mcp.port",
			Message: fmt.Sprintf("must be between 1 and 65535, got %d", c.Port),
		})
	}

	if c.ReadTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "mcp.read_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.WriteTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "mcp.write_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.IdleTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "mcp.idle_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.DefaultSearchThreshold < 0 || c.DefaultSearchThreshold > 1 {
		errs = append(errs, ValidationError{
			Field:   "mcp.default_search_threshold",
			Message: fmt.Sprintf("must be between 0.0 and 1.0, got %f", c.DefaultSearchThreshold),
		})
	}

	return errs
}

func (c *DatabaseConfig) validate() ValidationErrors {
	var errs ValidationErrors

	// PostgreSQL validation
	if c.Postgres.Host == "" {
		errs = append(errs, ValidationError{
			Field:   "database.postgres.host",
			Message: "required",
		})
	}

	if c.Postgres.Database == "" {
		errs = append(errs, ValidationError{
			Field:   "database.postgres.database",
			Message: "required",
		})
	}

	if c.Postgres.Port < 1 || c.Postgres.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "database.postgres.port",
			Message: fmt.Sprintf("must be between 1 and 65535, got %d", c.Postgres.Port),
		})
	}

	if c.Postgres.MaxOpenConns < 1 {
		errs = append(errs, ValidationError{
			Field:   "database.postgres.max_open_conns",
			Message: "must be at least 1",
		})
	}

	if c.Postgres.MaxIdleConns < 0 {
		errs = append(errs, ValidationError{
			Field:   "database.postgres.max_idle_conns",
			Message: "must be non-negative",
		})
	}

	if c.Postgres.MaxIdleConns > c.Postgres.MaxOpenConns {
		errs = append(errs, ValidationError{
			Field:   "database.postgres.max_idle_conns",
			Message: fmt.Sprintf("must be less than or equal to max_open_conns (%d), got %d", c.Postgres.MaxOpenConns, c.Postgres.MaxIdleConns),
		})
	}

	if c.Postgres.ConnMaxLifetime < 0 {
		errs = append(errs, ValidationError{
			Field:   "database.postgres.conn_max_lifetime",
			Message: "must be non-negative",
		})
	}

	// Neo4j validation
	if c.Neo4j.URI == "" {
		errs = append(errs, ValidationError{
			Field:   "database.neo4j.uri",
			Message: "required",
		})
	} else if !strings.HasPrefix(c.Neo4j.URI, "bolt://") && !strings.HasPrefix(c.Neo4j.URI, "neo4j://") {
		errs = append(errs, ValidationError{
			Field:   "database.neo4j.uri",
			Message: fmt.Sprintf("must start with bolt:// or neo4j://, got %q", c.Neo4j.URI),
		})
	}

	if c.Neo4j.MaxConnectionPoolSize < 1 {
		errs = append(errs, ValidationError{
			Field:   "database.neo4j.max_connection_pool_size",
			Message: "must be at least 1",
		})
	}

	if c.Neo4j.ConnectionAcquisitionTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "database.neo4j.connection_acquisition_timeout",
			Message: "must be a positive duration",
		})
	}

	return errs
}

func (c *OpenAIConfig) validate() ValidationErrors {
	var errs ValidationErrors

	// API key is required for production, but we allow empty for development/testing
	// The actual check will happen at runtime when trying to use the API

	if c.EmbeddingDimensions < 1 {
		errs = append(errs, ValidationError{
			Field:   "openai.embedding_dimensions",
			Message: "must be at least 1",
		})
	}

	if c.MaxRequestsPerMinute < 1 {
		errs = append(errs, ValidationError{
			Field:   "openai.max_requests_per_minute",
			Message: "must be at least 1",
		})
	}

	if c.RetryAttempts < 0 {
		errs = append(errs, ValidationError{
			Field:   "openai.retry_attempts",
			Message: "must be non-negative",
		})
	}

	if c.RetryDelay < 0 {
		errs = append(errs, ValidationError{
			Field:   "openai.retry_delay",
			Message: "must be non-negative",
		})
	}

	return errs
}

func (c *RateLimitConfig) validate() ValidationErrors {
	var errs ValidationErrors

	if c.Enabled {
		if c.RequestsPerSecond < 1 {
			errs = append(errs, ValidationError{
				Field:   "rate_limit.requests_per_second",
				Message: "must be at least 1 when rate limiting is enabled",
			})
		}

		if c.BurstSize < 1 {
			errs = append(errs, ValidationError{
				Field:   "rate_limit.burst_size",
				Message: "must be at least 1 when rate limiting is enabled",
			})
		}

		if c.PerUser.RequestsPerMinute < 1 {
			errs = append(errs, ValidationError{
				Field:   "rate_limit.per_user.requests_per_minute",
				Message: "must be at least 1 when rate limiting is enabled",
			})
		}

		if c.PerUser.BurstSize < 1 {
			errs = append(errs, ValidationError{
				Field:   "rate_limit.per_user.burst_size",
				Message: "must be at least 1 when rate limiting is enabled",
			})
		}
	}

	return errs
}

func (c *EnrichmentConfig) validate() ValidationErrors {
	var errs ValidationErrors

	if c.WorkerCount < 1 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.worker_count",
			Message: "must be at least 1",
		})
	}

	if c.PollInterval <= 0 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.poll_interval",
			Message: "must be a positive duration",
		})
	}

	if c.MaxAttempts < 1 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.max_attempts",
			Message: "must be at least 1",
		})
	}

	if c.RetryDelay < 0 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.retry_delay",
			Message: "must be non-negative",
		})
	}

	if c.JobTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.job_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.DrainTimeout <= 0 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.drain_timeout",
			Message: "must be a positive duration",
		})
	}

	if c.CompletedRetention <= 0 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.completed_retention",
			Message: "must be a positive duration",
		})
	}

	if c.FailedRetention <= 0 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.failed_retention",
			Message: "must be a positive duration",
		})
	}

	if c.RelatedToMinSimilarity < 0 || c.RelatedToMinSimilarity > 1 {
		errs = append(errs, ValidationError{
			Field:   "enrichment.related_to_min_similarity",
			Message: fmt.Sprintf("must be between 0 and 1, got %f", c.RelatedToMinSimilarity),
		})
	}

	return errs
}

func (c *LoggingConfig) validate() ValidationErrors {
	var errs ValidationErrors

	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	if !validLevels[strings.ToLower(c.Level)] {
		errs = append(errs, ValidationError{
			Field:   "logging.level",
			Message: fmt.Sprintf("must be one of: debug, info, warn, error; got %q", c.Level),
		})
	}

	validFormats := map[string]bool{
		"json": true,
		"text": true,
	}

	if !validFormats[strings.ToLower(c.Format)] {
		errs = append(errs, ValidationError{
			Field:   "logging.format",
			Message: fmt.Sprintf("must be one of: json, text; got %q", c.Format),
		})
	}

	return errs
}

func (c *ObservabilityConfig) validate() ValidationErrors {
	var errs ValidationErrors

	// Metrics validation
	if c.Metrics.Enabled {
		if c.Metrics.Port < 1 || c.Metrics.Port > 65535 {
			errs = append(errs, ValidationError{
				Field:   "observability.metrics.port",
				Message: fmt.Sprintf("must be between 1 and 65535, got %d", c.Metrics.Port),
			})
		}

		if c.Metrics.Path == "" {
			errs = append(errs, ValidationError{
				Field:   "observability.metrics.path",
				Message: "required when metrics are enabled",
			})
		}
	}

	// Health validation
	if c.Health.Enabled {
		if c.Health.Path == "" {
			errs = append(errs, ValidationError{
				Field:   "observability.health.path",
				Message: "required when health checks are enabled",
			})
		}
	}

	// Tracing validation
	if c.Tracing.Enabled {
		if c.Tracing.Endpoint == "" {
			errs = append(errs, ValidationError{
				Field:   "observability.tracing.endpoint",
				Message: "required when tracing is enabled",
			})
		}

		if c.Tracing.SampleRate < 0 || c.Tracing.SampleRate > 1 {
			errs = append(errs, ValidationError{
				Field:   "observability.tracing.sample_rate",
				Message: fmt.Sprintf("must be between 0 and 1, got %f", c.Tracing.SampleRate),
			})
		}
	}

	return errs
}

func (c *VocabularyConfig) validate() ValidationErrors {
	var errs ValidationErrors

	if len(c.Languages) == 0 {
		errs = append(errs, ValidationError{
			Field:   "vocabulary.languages",
			Message: "required",
		})
	}

	if len(c.Domains) == 0 {
		errs = append(errs, ValidationError{
			Field:   "vocabulary.domains",
			Message: "required",
		})
	}

	return errs
}

// Address returns the server address in host:port format.
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Address returns the MCP server address in host:port format.
// The MCP server binds to the same host as the admin API.
func (c *MCPConfig) Address(host string) string {
	return fmt.Sprintf("%s:%d", host, c.Port)
}

// ConnectionString returns the PostgreSQL connection string.
func (c *PostgresConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.Username, c.Password, c.Database, c.SSLMode,
	)
}

// DSN returns the PostgreSQL DSN for use with database/sql.
// Username, password, and SSLMode are URL-encoded to handle special characters safely.
func (c *PostgresConfig) DSN() string {
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.Username, c.Password),
		Host:     fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:     c.Database,
		RawQuery: fmt.Sprintf("sslmode=%s", url.QueryEscape(c.SSLMode)),
	}
	return u.String()
}

// SafeConnectionString returns the PostgreSQL connection string with the password masked.
// Use this method for logging to prevent secret exposure.
func (c *PostgresConfig) SafeConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=***** dbname=%s sslmode=%s",
		c.Host, c.Port, c.Username, c.Database, c.SSLMode,
	)
}

// SafeDSN returns the PostgreSQL DSN with the password masked.
// Use this method for logging to prevent secret exposure.
func (c *PostgresConfig) SafeDSN() string {
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.User(c.Username),
		Host:     fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:     c.Database,
		RawQuery: fmt.Sprintf("sslmode=%s", url.QueryEscape(c.SSLMode)),
	}
	// Build the DSN with the URL-encoded username, then inject the mask.
	// url.User encodes the username safely; we append the fixed mask ourselves
	// so that the literal "*****" is not percent-encoded.
	safe := u.String()
	// Replace "postgres://encodeduser@" with "postgres://encodeduser:*****@"
	encoded := u.User.String()
	safe = strings.Replace(safe, encoded+"@", encoded+":*****@", 1)
	return safe
}

// ConnectionURI returns the Neo4j URI for API consistency with PostgresConfig.
func (c *Neo4jConfig) ConnectionURI() string {
	return c.URI
}

// SafeURI returns the Neo4j URI with any embedded credentials masked for logging.
// If the URI contains embedded credentials (user:pass@host), the password is masked.
// Use this method for logging to prevent secret exposure.
func (c *Neo4jConfig) SafeURI() string {
	// Check for embedded credentials in the URI (e.g., bolt://user:pass@host:port)
	// Format: scheme://[user[:password]@]host[:port]
	uri := c.URI

	// Find the scheme separator
	schemeEnd := strings.Index(uri, "://")
	if schemeEnd == -1 {
		return uri
	}

	// Get the part after the scheme
	rest := uri[schemeEnd+3:]

	// Find the LAST @ symbol which separates credentials from host
	// This handles passwords that contain @ symbols
	atIndex := strings.LastIndex(rest, "@")
	if atIndex == -1 {
		// No embedded credentials
		return uri
	}

	// Extract the credentials part (before the last @)
	credentials := rest[:atIndex]

	// Find the FIRST colon which separates username from password
	colonIndex := strings.Index(credentials, ":")
	if colonIndex == -1 {
		// Only username, no password to mask
		return uri
	}

	// Build the safe URI with masked password
	scheme := uri[:schemeEnd+3]
	username := credentials[:colonIndex]
	hostPart := rest[atIndex+1:]

	return fmt.Sprintf("%s%s:*****@%s", scheme, username, hostPart)
}

// Credentials returns the username and password for use with neo4j.BasicAuth().
func (c *Neo4jConfig) Credentials() (username, password string) {
	return c.Username, c.Password
}
