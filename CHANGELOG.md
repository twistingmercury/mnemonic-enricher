# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
