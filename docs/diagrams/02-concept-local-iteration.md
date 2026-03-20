# MVP 2

Iteration 2 decomposes the local runtime and introduces async enrichment:

1. Split MCP server, Admin API, and enricher into separate local services.
2. Add RabbitMQ for enrichment jobs published by the Admin API.
3. Keep MCP read-only and verify enrichment updates PostgreSQL + PGVector and Neo4j.

```mermaid
---
config:
  theme: redux
  layout: dagre
---
flowchart TB
  claude["Claude Code"] -->|MCP| mcp_server["mnemonic MCP server"]

  admin["admin tools"] -->|REST| admin_api["mnemonic admin API"]

  admin_api --> pg[("PostgreSQL + PGVector")]
  admin_api --> queue[("RabbitMQ enrichment job queue")]
  queue --> enricher["enricher"]
  enricher --> pg
  enricher --> neo4j[("Neo4j")]

  mcp_server -->|read only| pg
  mcp_server -->|read only| neo4j
```
