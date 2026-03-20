# Mnemonic Build & Deployment

This directory contains build and deployment configurations for the Mnemonic service.

## Files

- `Dockerfile` - Multi-stage build for production deployment
- `build.sh` - Build script for CI/CD pipelines
- [Build Pipeline Diagram](../../../docs/diagrams/build-pipeline.md) - End-to-end flow for local + CI/CD builds

## Local Development with Docker Compose

For local development, use the Docker Compose configuration in the parent directory:

```bash
# From the mnemonic directory
cd /Users/doublej/dev/ace/src/mnemonic

# Copy environment template
cp .env.example .env

# Edit .env and add your OpenAI API key
# MNEMONIC_OPENAI_API_KEY=sk-...

# Start all services
docker compose up

# Start in detached mode
docker compose up -d

# View logs
docker compose logs -f mnemonic

# Stop all services
docker compose down

# Remove volumes (clean slate)
docker compose down -v
```

## Service Endpoints

When running via Docker Compose:

| Service   | Endpoint                  | Purpose                    |
|-----------|---------------------------|----------------------------|
| Mnemonic  | <http://localhost:8080>     | REST API                   |
| Mnemonic  | <http://localhost:9090>     | Metrics (Prometheus)       |
| Neo4j     | <http://localhost:7475>     | Neo4j Browser              |
| Neo4j     | bolt://localhost:7688     | Neo4j Bolt protocol        |
| Postgres  | postgresql://localhost:5433| PostgreSQL connection     |

**Note:** Ports are offset from defaults (5433 instead of 5432,
7475/7688 instead of 7474/7687) to avoid conflicts with other local
services.

## Database Credentials (Development)

**PostgreSQL:**

- Database: `mnemonic`
- Username: `mnemonic`
- Password: `mnemonic_dev`

**Neo4j:**

- Username: `neo4j`
- Password: `mnemonic_dev`

**Note:** These credentials are for local development only. Never use
these in production.

## Building Manually

```bash
# Build the Docker image
docker build -f build/Dockerfile \
  --build-arg BUILD_VER=v0.1.0 \
  --build-arg BUILD_COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t mnemonic:latest .

# Run the container
docker run -p 8080:8080 \
  -e MNEMONIC_DATABASE_POSTGRES_HOST=postgres \
  -e MNEMONIC_DATABASE_NEO4J_URI=bolt://neo4j:7687 \
  mnemonic:latest
```

## Troubleshooting

**Mnemonic fails to start:**

- Check database health: `docker compose ps`
- View logs: `docker compose logs postgres neo4j`
- Ensure databases are healthy before Mnemonic starts

**Cannot connect to databases:**

- Verify network: `docker compose ps`
- Check environment variables: `docker compose config`
- Restart services: `docker compose restart`

**OpenAI API errors:**

- Verify API key is set in `.env`
- Check key validity at <https://platform.openai.com/api-keys>
- View Mnemonic logs: `docker compose logs mnemonic`

## Health Checks

```bash
# Check Mnemonic health
curl http://localhost:8080/health

# Check metrics
curl http://localhost:9090/metrics

# Check PostgreSQL
docker compose exec postgres pg_isready -U mnemonic

# Check Neo4j
docker compose exec neo4j cypher-shell -u neo4j -p mnemonic_dev "RETURN 1"
```
