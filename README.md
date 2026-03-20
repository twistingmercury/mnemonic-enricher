# Mnemonic

[![Mnemonic CD](https://github.com/twistingmercury/ace/actions/workflows/mnemonic-cd.yaml/badge.svg)](https://github.com/twistingmercury/ace/actions/workflows/mnemonic-cd.yaml)

> **Maturity Level**: Emerging - Pivot completed, MCP server and Admin API under development

---

Mnemonic is a team knowledge graph and tooling synchronization service for Claude Code, providing semantic pattern search over curated institutional knowledge and synchronized access to agents, skills, and commands across team members.

## Usage

Mnemonic provides two interfaces:

**MCP Server** (read-only, for Claude Code):

```json
// Claude Code invokes via MCP
{
  "tool": "search_patterns",
  "arguments": {
    "query": "Go error handling patterns"
  }
}
// Returns: Ranked patterns with similarity scores
```

**Admin REST API** (for data management):

```bash
# Store a new pattern
curl -X POST http://localhost:8080/v1/api/patterns \
  -H "Content-Type: application/json" \
  -d '{
    "name": "go-error-wrapping",
    "description": "Pattern for wrapping errors with context",
    "content": "Use fmt.Errorf with %w for error chains...",
    "tags": ["go", "error-handling"],
    "agent_associations": [{"agent_name": "go-software-engineer", "relevance": 0.9}]
  }'

# Search patterns semantically
curl -X GET "http://localhost:8080/v1/api/patterns/search?q=error+handling&limit=5"
```

See the [API Specification](docs/openapi/mnemonic-v1.yaml)
for complete endpoint documentation.

## How it works

Mnemonic provides two core capabilities:

**Team knowledge graph**: Curated engineering patterns, guidelines, and institutional knowledge stored in a knowledge graph (Postgres + PGVector + Neo4j). Relevant patterns are retrieved using semantic search and graph traversal, providing agents with project-specific context. Patterns are enriched automatically with embeddings and concept extraction to enable semantic search via the MCP `search_patterns` tool.

**Tooling synchronization**: Team-wide agents, skills, and commands are stored in Mnemonic and synchronized to team members via the Admin REST API. This ensures consistent Claude Code configurations across the team, eliminating "works on my machine" issues and enabling rapid onboarding.

**Dual protocol architecture**: Read-only MCP server (port 8081) for Claude Code integration, separate Admin REST API (port 8080) for data management. Both interfaces run in a single server process backed by Postgres and Neo4j.

### Architectural Pivot

Mnemonic originally focused on deterministic agent routing but pivoted in February 2026 to focus on team knowledge and tooling sync (see [2026-02-14-mnemonic-pivot-knowledge-sync.md](docs/plans/2026-02-14-mnemonic-pivot-knowledge-sync.md)). The user is the orchestrator in spec-based development; Mnemonic provides memory and tools, not routing decisions.

### Phased Approach

- **Phase 1** (Current): Local deployment with MCP server, Admin API, and pattern enrichment
- **Phase 2** (Future): Production deployment with authentication, rate limiting, and multi-region support

## Key Considerations

- **Pivot completed**: Routing engine removed, focus shifted to knowledge graph and tooling sync (February 2026)
- **Current state**: Repository layer with vector similarity search functional, MCP server and Admin API endpoints in development, pattern enrichment pipeline planned
- **MVP scope**: Local deployment via Docker Compose, single-user trusted environment, no authentication
- **MCP integration**: Claude Code connects via MCP protocol on port 8081 for read-only pattern search
- **Admin API**: Data management (patterns, agents, skills, commands) via REST on port 8080
- **Post-MVP features**: Multi-user authentication, production deployment, rate limiting, remote access

## Development Considerations

### Quick Start

Clone and build:

```bash
git clone https://github.com/twistingmercury/mnemonic-enricher.git
cd mnemonic/src/mnemonic
./build/build.sh
```

Requires Go 1.25+, Docker 27+, and Docker Compose 2.32+
([Go installation](https://go.dev/doc/install),
[Docker installation](https://docs.docker.com/get-docker/))

### Building & running

Build and test Mnemonic:

```bash
cd src/mnemonic
./build/build.sh
```

The build script runs unit tests, integration tests (with PostgreSQL in
Docker), and builds the Docker image.

### Testing

Run unit tests:

```bash
cd src/mnemonic
go test ./...
```

Run integration tests (requires Docker):

```bash
cd src/mnemonic/internal/repository/tests
./run-agent-integration-tests.sh
./run-pattern-integration-tests.sh
```

The build script runs both unit and integration tests automatically.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from git tags:

```bash
git describe --tags --always
```

No releases published yet. See [CHANGELOG.md](CHANGELOG.md) for
development progress.

## Documentation

### Background

- [Project Blog](https://twistingmercury.github.io) - Development
  journey, design rationale, and updates

### Architecture

- [Architecture Overview](docs/architecture/00-overview.md) - System
  model, phased approach, key principles
- [Requirements](docs/architecture/01-requirements.md) - Problem
  statement and success criteria
- [Architectural Decisions](docs/architecture/02-architectural-decisions.md) -
  Major decisions with rationale
- [System Architecture](docs/architecture/03-system-architecture.md) -
  Component breakdown and data flow

### Design

- [Pivot API Specification](docs/design/2026-02-15-pivot-api-specification.md) -
  REST Admin API + MCP Server specification (post-pivot)
- [Pattern Processing](docs/design/pattern-processing.md) -
  Pattern enrichment and search pipeline
- [Observability Implementation](docs/design/observability-implementation.md) -
  Metrics, tracing, and logging design
- [Configuration](docs/design/configuration.md) -
  Server configuration reference
