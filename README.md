# mnemonic-enricher

[![Build Status](https://github.com/twistingmercury/mnemonic-enricher/actions/workflows/ci.yml/badge.svg)](https://github.com/twistingmercury/mnemonic-enricher/actions/workflows/ci.yml)

> **Maturity Level**: Beta - Stable enrichment worker, RabbitMQ integration operational

A standalone enrichment worker that processes embedding and knowledge graph synchronization jobs delivered via message queue.

## Usage

`mnemonic-enricher` subscribes to a message queue, processes enrichment jobs (embeddings via OpenAI + graph synchronization to Neo4j), and exposes only health and version endpoints:

```bash
# Health check
curl http://localhost:8080/health

# Version information
curl http://localhost:8080/version
```

Start the service with Docker Compose:

```bash
docker compose up
```

## How it works

### Queue-driven architecture

The worker subscribes to a configurable message queue (default: RabbitMQ) for enrichment job delivery. The queue abstraction (`queue.Subscriber` interface in `internal/queue/queue.go`) permits swapping queue providers without changing worker logic.

**Current provider:** RabbitMQ (`internal/queue/rabbitmq/`)

**Adding a new provider:** Create a new package under `internal/queue/<provider>/` implementing the `Subscriber` interface, then add a case in the server's provider switch.

### Enrichment pipeline

For each job:

1. **Embedding**: Call OpenAI API to generate vector embeddings
2. **Graph sync**: Store embeddings and synchronize relationships to Neo4j

Patterns are enriched with vector embeddings to enable semantic search in downstream consumers.

## Configuration

Configure the worker via environment variables. All values have sensible defaults.

### Queue Provider

| Variable | Default | Purpose |
|----------|---------|---------|
| `MNEMONIC_QUEUE_PROVIDER` | `rabbitmq` | Queue provider (e.g., `rabbitmq`) |

### RabbitMQ

| Variable | Default | Purpose |
|----------|---------|---------|
| `MNEMONIC_QUEUE_RABBITMQ_HOST` | `localhost` | RabbitMQ host |
| `MNEMONIC_QUEUE_RABBITMQ_PORT` | `5672` | RabbitMQ port |
| `MNEMONIC_QUEUE_RABBITMQ_USER` | `guest` | RabbitMQ username |
| `MNEMONIC_QUEUE_RABBITMQ_PASSWORD` | `guest` | RabbitMQ password |
| `MNEMONIC_QUEUE_RABBITMQ_VHOST` | `/` | RabbitMQ virtual host |
| `MNEMONIC_QUEUE_RABBITMQ_QUEUE` | `enrichment-jobs` | Queue name |
| `MNEMONIC_QUEUE_RABBITMQ_PREFETCH_COUNT` | `5` | Prefetch count per worker |
| `MNEMONIC_QUEUE_RABBITMQ_RECONNECT_DELAY` | `5s` | Reconnection delay |

### Database: PostgreSQL

| Variable | Default | Purpose |
|----------|---------|---------|
| `MNEMONIC_DATABASE_POSTGRES_HOST` | `localhost` | PostgreSQL host |
| `MNEMONIC_DATABASE_POSTGRES_PORT` | `5432` | PostgreSQL port |
| `MNEMONIC_DATABASE_POSTGRES_DATABASE` | `mnemonic` | Database name |
| `MNEMONIC_DATABASE_POSTGRES_USERNAME` | `postgres` | Username |
| `MNEMONIC_DATABASE_POSTGRES_PASSWORD` | `postgres` | Password |

### Database: Neo4j

| Variable | Default | Purpose |
|----------|---------|---------|
| `MNEMONIC_DATABASE_NEO4J_URI` | `neo4j://localhost:7687` | Neo4j connection URI |
| `MNEMONIC_DATABASE_NEO4J_USERNAME` | `neo4j` | Username |
| `MNEMONIC_DATABASE_NEO4J_PASSWORD` | `neo4j` | Password |
| `MNEMONIC_DATABASE_NEO4J_DATABASE` | `neo4j` | Database name |

### OpenAI

| Variable | Default | Purpose |
|----------|---------|---------|
| `MNEMONIC_OPENAI_API_KEY` | (required) | OpenAI API key |
| `MNEMONIC_OPENAI_EMBEDDING_MODEL` | `text-embedding-3-small` | Embedding model |
| `MNEMONIC_OPENAI_EXTRACTION_MODEL` | `gpt-4o-mini` | Extraction model for concept analysis |

### Enrichment Worker

| Variable | Default | Purpose |
|----------|---------|---------|
| `MNEMONIC_ENRICHMENT_WORKER_COUNT` | `4` | Number of concurrent enrichment workers |
| `MNEMONIC_ENRICHMENT_JOB_TIMEOUT` | `30s` | Job processing timeout |
| `MNEMONIC_ENRICHMENT_MAX_ATTEMPTS` | `3` | Retry attempts per job |

## Key Considerations

- **No HTTP API handlers**: This is a worker service. Only `/health` and `/version` are exposed for operational use.
- **Queue abstraction**: Swap queue providers by implementing the `Subscriber` interface and configuring the provider name.
- **Database dependency**: PostgreSQL stores patterns and their metadata; Neo4j stores the enriched knowledge graph.
- **OpenAI API cost**: Each pattern embedding incurs an OpenAI API cost. Consider batch processing and rate limiting for large datasets.

## Development Considerations

### Quick Start

Clone and build:

```bash
git clone https://github.com/twistingmercury/mnemonic-enricher.git
cd mnemonic-enricher
make build
```

Requires:
- Go 1.21+
- Docker 27+
- Docker Compose 2.32+

([Go installation](https://go.dev/doc/install),
[Docker installation](https://docs.docker.com/get-docker/))

### Building & running

Build locally and run unit + integration + E2E tests:

```bash
make build
```

The `make build` target invokes the full CI build script (`src/build/build.sh`), which:
- Runs unit tests
- Runs integration tests (PostgreSQL and Neo4j via Docker)
- Builds the Docker image
- Runs E2E tests

For development, use `make local` to build a binary without Docker:

```bash
make local
```

This runs only the Go build step (no tests, no Docker image).

### Testing

Run unit tests:

```bash
make tests-unit
```

Run graph repository integration tests (PostgreSQL + Neo4j required):

```bash
make tests-db-graph
```

Run benchmarks:

```bash
make tests-bench
```

### Linting and code quality

```bash
make analyze
```

This runs:
- `goimports` code formatting
- `golangci-lint` linting
- `govulncheck` vulnerability scanning
- `gosec` security scanning

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from git tags:

```bash
git describe --tags --always
```

See [CHANGELOG.md](CHANGELOG.md) for release history.
