# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2026-08-19

### Changed

- Modernized the root README for clearer project usage, architecture, and development guidance
- Reworked `src/build/README.md` to accurately document build, E2E, CI, and Docker Compose workflows, service boundaries, safe diagnostics, image constraints, and RabbitMQ endpoints, credentials, and troubleshooting

### Fixed

- Corrected the example logging keys to `MNEMONIC_LOGGING_LEVEL` and `MNEMONIC_LOGGING_FORMAT`
- Restored normal registry pull behavior for the companion API image while retaining `pull_policy: never` for the locally built enricher image

## [0.2.0] - 2026-03-21

### Added

- E2E test suite for enrichment pipeline (`tests/e2e/pipeline/`) validating happy path, malformed message, job-not-found, and OpenAI failure scenarios against a live Docker Compose environment
- OpenAI HTTP stub (`tests/e2e/openai-stub/`) serving deterministic embeddings and chat completions for E2E tests, with a `/control/fail-next` endpoint to simulate API failures
- Neo4j helper (`tests/e2e/helpers/neo4j.go`) for asserting graph node existence and cleaning up test data
- AMQP helper (`tests/e2e/helpers/amqp.go`) for publishing enrichment job messages to RabbitMQ during E2E tests
- Database helpers (`tests/e2e/helpers/db.go`) for seeding patterns, chunks, and enrichment jobs, and for polling job status

### Fixed

- OpenAI embedding and extraction services now correctly append `/embeddings` and `/chat/completions` path suffixes to `cfg.BaseURL`, so that stub and production URLs are constructed correctly from the base URL configured via `MNEMONIC_OPENAI_BASE_URL`
- Docker Compose E2E configuration (`tests/docker-compose.yaml`): enricher now waits for Postgres and Neo4j to be healthy before starting, preventing connection errors on slow infra startup
- OpenAI stub `fail-next` counter uses a decrement-based mechanism so that all embedding retry attempts within a single operation fail, matching the test intent of simulating a fully unavailable API

## [0.1.0] - 2026-03-20

### Added

- Initial release of `mnemonic-enricher` as a standalone service
- Queue abstraction layer (`internal/queue/queue.go`) with `Subscriber` interface for pluggable queue providers
- RabbitMQ subscriber implementation (`internal/queue/rabbitmq/`) with connection pooling and automatic reconnection
- Enrichment worker pool for concurrent processing of embedding and graph synchronization jobs
- Configuration system (`internal/config/`) with environment variable support and layered defaults
- OpenAI API integration for vector embeddings (text-embedding-3-small by default)
- Neo4j graph synchronization for knowledge graph updates
- PostgreSQL backing store for pattern metadata and embeddings
- HTTP health check endpoint (`GET /health`) and version endpoint (`GET /version`)
- Docker and Docker Compose setup for local development and deployment
- GitHub Actions CI/CD workflows for automated testing and image building
- Comprehensive unit and integration tests for all components
- Structured logging with request tracing support
- Graceful shutdown with configurable drain timeout for in-flight jobs

### Changed

- Extracted from monolithic `mnemonic` service to standalone worker service
- Refactored enrichment processing from polling to queue-driven subscription model
- Moved queue interaction behind abstraction layer for provider independence

### Removed

- Admin REST API handlers for agents, patterns, skills, and skillfiles
- MCP server (pattern search) — now provided by separate service
- Agent routing logic and policy engine
- Pattern search REST endpoints

[Unreleased]: https://github.com/twistingmercury/mnemonic-enricher/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/twistingmercury/mnemonic-enricher/releases/tag/v0.1.0
