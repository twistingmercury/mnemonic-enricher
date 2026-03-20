package config

import "time"

// Default values for server configuration
const (
	DefaultServerHost            = "0.0.0.0"
	DefaultServerPort            = 8080
	DefaultServerReadTimeout     = 30 * time.Second
	DefaultServerWriteTimeout    = 30 * time.Second
	DefaultServerIdleTimeout     = 120 * time.Second
	DefaultServerShutdownTimeout = 5 * time.Second
	DefaultServerTLSEnabled      = false
)

// Default values for MCP server configuration
const (
	DefaultMCPPort                   = 8081
	DefaultMCPReadTimeout            = 30 * time.Second
	DefaultMCPWriteTimeout           = 30 * time.Second
	DefaultMCPIdleTimeout            = 120 * time.Second
	DefaultMCPDefaultSearchThreshold = 0.5
)

// Default values for PostgreSQL configuration
const (
	DefaultPostgresHost            = "localhost"
	DefaultPostgresPort            = 5432
	DefaultPostgresDatabase        = "mnemonic"
	DefaultPostgresUsername        = "mnemonic"
	DefaultPostgresSSLMode         = "prefer"
	DefaultPostgresMaxOpenConns    = 25
	DefaultPostgresMaxIdleConns    = 5
	DefaultPostgresConnMaxLifetime = 5 * time.Minute
)

// Default values for Neo4j configuration
const (
	DefaultNeo4jURI                          = "bolt://localhost:7687"
	DefaultNeo4jUsername                     = "neo4j"
	DefaultNeo4jDatabase                     = "neo4j"
	DefaultNeo4jMaxConnectionPoolSize        = 50
	DefaultNeo4jConnectionAcquisitionTimeout = 60 * time.Second
)

// Default values for OpenAI configuration
const (
	DefaultOpenAIEmbeddingModel       = "text-embedding-3-large"
	DefaultOpenAIEmbeddingDimensions  = 2000
	DefaultOpenAIExtractionModel      = "gpt-4o-mini"
	DefaultOpenAIMaxRequestsPerMinute = 500
	DefaultOpenAIRetryAttempts        = 3
	DefaultOpenAIRetryDelay           = 1 * time.Second
)

// Default values for rate limiting configuration
const (
	DefaultRateLimitEnabled           = false
	DefaultRateLimitRequestsPerSecond = 100
	DefaultRateLimitBurstSize         = 200
	DefaultRateLimitPerUserRPM        = 60
	DefaultRateLimitPerUserBurst      = 10
)

// Default values for enrichment configuration
const (
	DefaultEnrichmentWorkerCount            = 2
	DefaultEnrichmentPollInterval           = 5 * time.Second
	DefaultEnrichmentMaxAttempts            = 3
	DefaultEnrichmentRetryDelay             = 30 * time.Second
	DefaultEnrichmentJobTimeout             = 5 * time.Minute
	DefaultEnrichmentDrainTimeout           = 30 * time.Second
	DefaultEnrichmentCompletedRetention     = 168 * time.Hour // 7 days
	DefaultEnrichmentFailedRetention        = 720 * time.Hour // 30 days
	DefaultEnrichmentRelatedToMinSimilarity = 0.3
)

// Default values for logging configuration
const (
	DefaultLoggingLevel         = "info"
	DefaultLoggingFormat        = "json"
	DefaultLoggingIncludeCaller = false
)

// Default values for observability configuration
const (
	DefaultMetricsEnabled      = true
	DefaultMetricsPath         = "/metrics"
	DefaultMetricsPort         = 9090
	DefaultHealthEnabled       = true
	DefaultHealthPath          = "/health"
	DefaultTracingEnabled      = false
	DefaultTracingSampleRate   = 0.1
	DefaultTracingOTLPInsecure = true
)

// DefaultVocabularyLanguages returns a fresh slice of the default allowed language values.
func DefaultVocabularyLanguages() []string {
	return []string{
		"agnostic",
		"bash",
		"c",
		"cpp",
		"csharp",
		"cql",
		"cypher",
		"dart",
		"delphi",
		"docker",
		"elixir",
		"erlang",
		"go",
		"json",
		"java",
		"javascript",
		"kotlin",
		"lua",
		"markdown",
		"matlab",
		"mql",
		"objective-c",
		"perl",
		"php",
		"plsql",
		"powershell",
		"python",
		"r",
		"react",
		"ruby",
		"rust",
		"scala",
		"shell",
		"sql",
		"swift",
		"terraform",
		"toml",
		"tsql",
		"typescript",
		"visual-basic",
		"yaml",
		"zig",
	}
}

// DefaultVocabularyDomains returns a fresh slice of the default allowed domain values.
func DefaultVocabularyDomains() []string {
	return []string{
		"api-design", "backend", "frontend", "testing", "devops", "cli", "data-design", "documentation", "data-access", "security", "shell-scripting", "configuration", "observability", "source-management",
	}
}

// Configuration file paths
const (
	ProductionConfigPath  = "/etc/mnemonic/config.yaml"
	DevelopmentConfigPath = "./config.yaml"
	EnvConfigFile         = "MNEMONIC_CONFIG_FILE"
	EnvPrefix             = "MNEMONIC"
)
