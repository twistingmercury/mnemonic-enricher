# Pattern Enrichment

[Back to Architecture Overview](../../architecture/README.md) | [Back to Project README](../../../README.md)

## Overview

> **Architecture Reference:** [System Architecture - Mnemonic](../../architecture/02-system-architecture.md#mnemonic) | [ADR-004: Unified Backend with REST API](../../architecture/00-architectural-decisions.md#adr-004-unified-backend-with-rest-api)

Pattern enrichment transforms raw pattern content into searchable, interconnected knowledge. When a pattern is created or updated, Mnemonic automatically enriches it to enable:

1. **Semantic search** - Find patterns by meaning, not just keywords via the MCP `search_patterns` tool and Admin API search endpoint
2. **Relationship discovery** - Connect related patterns and agents via knowledge graph

Enrichment data feeds all three MCP tools: `search_patterns` (semantic search), `find_related_patterns` (graph traversal), and `get_pattern` (full pattern with graph context).

This design is inspired by Cognee's cognify pipeline (chunk, classify, extract, integrate, summarize) but adapted for Mnemonic's simpler use case: patterns are already curated documents, not raw data requiring extensive preprocessing.

**Note:** Following the architectural pivot (see [2026-02-14-mnemonic-pivot-knowledge-sync.md](../plans/2026-02-14-mnemonic-pivot-knowledge-sync.md)), enriched patterns are consumed by the MCP server's search tools and the Admin API search endpoint, not by a routing engine.

## Enrichment Model

> **Architecture Reference:** [Communication Patterns - Response Structure](../../architecture/03-communication-patterns.md#response-structure)

Patterns include enrichment status fields to track processing state:

```yaml
Pattern:
  type: object
  properties:
    # ... existing fields (id, name, description, content, tags, etc.)

    # Enrichment status fields
    enrichment_status:
      type: string
      enum: [pending, enriched, failed]
      description: Aggregate enrichment state across all chunks
    enrichment_error:
      type: string
      description: Error message if enrichment_status is "failed"
    enriched_at:
      type: string
      format: date-time
      description: Timestamp of last successful enrichment
```

`enrichment_status` on the pattern reflects aggregate chunk status: the pattern is `enriched` when ALL its `pattern_chunks` rows have `enrichment_status = 'enriched'`. It is `failed` if any chunk failed. The `enrichment_jobs` table has a separate status field with an additional `processing` state (`pending`, `processing`, `completed`, `failed`). The `processing` state exists only on jobs, not on patterns.

Each `pattern_chunks` row also has its own `enrichment_status`, `enrichment_error`, and `enriched_at` fields tracking the per-chunk embedding state.

## Automatic Enrichment Flow

> **Architecture Reference:** [System Architecture - Data Flow](../../architecture/02-system-architecture.md#data-flow)

Enrichment is triggered automatically when a pattern is created or updated. The API responds immediately while enrichment processes asynchronously in the background.

```mermaid
sequenceDiagram
    participant Client as Client (REST API)
    participant API as Mnemonic API
    participant PG as Postgres
    participant Worker as Background Worker
    participant OpenAI as OpenAI API
    participant PGV as PGVector
    participant Neo4j as Neo4j

    Client->>API: POST /v1/api/patterns
    API->>PG: Save pattern (status: "pending")
    API->>PG: Queue enrichment job
    API-->>Client: 202 Accepted

    Note over Worker,PG: Asynchronous processing

    Worker->>PG: Pick up job from queue
    Worker->>PG: Load pattern content
    Worker->>Worker: Split content at H2 headings into chunks
    loop For each chunk
        Worker->>OpenAI: Generate chunk embedding
        OpenAI-->>Worker: Embedding vector
        Worker->>PGV: Store chunk embedding in pattern_chunks
    end
    Worker->>OpenAI: Extract entities from full content (LLM)
    OpenAI-->>Worker: Extracted entities
    Worker->>Neo4j: Create relationships
    Worker->>PG: Update status to "enriched"

    alt OpenAI call fails
        Worker->>PG: Update status to "failed"
        Note over Worker,PG: Store enrichment_error message
    end
```

Key characteristics:

- **Automatic**: Users do not invoke enrichment separately; it triggers on create/update
- **Non-blocking**: API returns 202 Accepted immediately; enrichment happens asynchronously
- **Status tracking**: Pattern's `enrichment_status` field reflects current state
- **Idempotent**: Re-enrichment on update replaces previous enrichment data

**Why 202 Accepted instead of 201 Created?** The pattern resource is accepted for processing but not immediately usable. Patterns with `enrichment_status: 'pending'` are excluded from search results until enrichment completes. HTTP 202 accurately signals that the request was accepted but processing is not yet complete.

## Enrichment Pipeline

> **Architecture Reference:** [System Architecture - Mnemonic](../../architecture/02-system-architecture.md#mnemonic) | [Concept](../mnemonic-concept.md)

### Write-time Enrichment

When a pattern is created or updated via `POST/PUT /v1/api/patterns`:

```mermaid
stateDiagram-v2
    [*] --> ValidateAndStore: Pattern Create/Update

    state "1. Validate & Store Metadata" as ValidateAndStore
    note right of ValidateAndStore
        Postgres: Name, description,
        tags, pattern_agent_associations
        enrichment_status: 'pending'
    end note

    ValidateAndStore --> GenerateEmbedding

    state "2. Generate Embeddings" as GenerateEmbedding
    note right of GenerateEmbedding
        Split content at H2 headings
        PGVector: Embed each chunk
        Store in pattern_chunks
    end note

    GenerateEmbedding --> ExtractEntities

    state "3. Extract Entities" as ExtractEntities
    note right of ExtractEntities
        LLM: Identify concepts,
        technologies, practices
    end note

    ExtractEntities --> CreateRelationships

    state "4. Create Relationships" as CreateRelationships
    note right of CreateRelationships
        Neo4j: RELATED_TO, RELEVANT_FOR,
        MENTIONED_IN relationships
    end note

    CreateRelationships --> UpdateStatus

    state "5. Update Status" as UpdateStatus
    note right of UpdateStatus
        enrichment_status: 'enriched'
        enriched_at: now()
    end note

    UpdateStatus --> [*]
```

#### Step 1: Validate and Store Metadata

Store pattern metadata in Postgres:

- `id` (UUID, generated)
- `name`, `description`, `content`
- `tags` (array)
- `entity_type`, `language`, `domain`, `version`, `related_patterns`
- `pattern_agent_associations` (join table: `pattern_id UUID`, `agent_id UUID`, `relevance double precision`)
- `enrichment_status` (initially "pending")
- `enrichment_error` (null initially)
- `enriched_at` (null initially)
- `created_at`, `updated_at`

#### Step 2: Generate Embeddings

Content is split at H2 headings into chunks. Each chunk receives its own embedding stored as a row in `pattern_chunks`. Splitting enables precise section-level search results. If a pattern has no H2 headings, it is stored as a single chunk.

```go
// Pseudocode
chunks := splitAtH2Headings(pattern.Content)
for i, chunk := range chunks {
    embedding := embeddingModel.Embed(chunk.Content)
    chunkRepo.Create(ctx, PatternChunk{
        PatternID:    pattern.ID,
        SectionTitle: chunk.Title,
        ChunkIndex:   i,
        Content:      chunk.Content,
        Embedding:    embedding,
    })
}
```

##### Enriched Text Format for Embeddings

When generating embeddings during enrichment, each chunk is prefixed with metadata before being sent to the embedding model. This enrichment happens at embedding time only and does not modify the stored chunk content.

**Format:**

```
{pattern.Name} | {tags joined by ", "} | {chunk.SectionTitle}

{chunk.Content}
```

**Example:**

```
Go Error Handling | go, patterns, error-handling | Recovery Strategies

When an operation fails, consider whether retry logic is appropriate...
```

This enriched format is sent to the embedding model to generate the vector. The query text submitted by users, by contrast, is embedded as-is without metadata enrichment. This asymmetry (rich document vectors, plain query vectors) is intentional and standard practice in information retrieval: document vectors benefit from contextual metadata, while user queries remain simple and natural.

**Operational Constraint — Format Changes Require Re-enrichment:**

If this enriched text format ever changes, ALL existing chunk embeddings in the database become semantically stale relative to new ones. A format change requires a complete re-enrichment pass over all pattern chunks to regenerate their vectors. This is a breaking change at the data level and should be approached as a major version upgrade for the enrichment pipeline.

Examples that would require re-enrichment:
- Adding new metadata fields to the prefix (e.g., domain, language)
- Changing the delimiter or separator format
- Removing existing fields from the prefix
- Modifying the order of prefix fields

Plan accordingly if enrichment format changes are needed.

#### Step 3: Extract Entities

Use an LLM to extract structured information from the pattern content:

```json
{
  "concepts": ["error handling", "retry logic", "exponential backoff"],
  "technologies": ["Go", "context package"],
  "practices": ["defensive programming", "graceful degradation"]
}
```

These categories map to Concept nodes with `type` = `"domain"`, `"technology"`, and `"practice"` respectively. Concept names are normalized to lowercase before storage.

This LLM call adds 1-5 seconds of processing time per pattern, which is why enrichment runs asynchronously.

#### Step 4: Create Relationships

Store relationships in Neo4j:

The following runs as a transaction during enrichment:

```cypher
// Step 1: Create/update pattern node with full properties
MERGE (p:Pattern {id: $patternId})
ON CREATE SET p.name = $name, p.description = $description, p.createdAt = datetime()
ON MATCH SET p.name = $name, p.description = $description, p.updatedAt = datetime()

// Step 2: Remove old RELEVANT_FOR relationships
MATCH (p:Pattern {id: $patternId})-[r:RELEVANT_FOR]->()
DELETE r

// Step 3: Create new RELEVANT_FOR relationships
UNWIND $associations AS assoc
MATCH (p:Pattern {id: $patternId})
MATCH (a:Agent {name: assoc.agentName})
CREATE (p)-[:RELEVANT_FOR {relevance: assoc.relevance}]->(a)

// Step 4: Remove old MENTIONED_IN relationships for this pattern
MATCH (:Concept)-[r:MENTIONED_IN]->(:Pattern {id: $patternId})
DELETE r

// Step 5: Create concepts and MENTIONED_IN relationships
UNWIND $concepts AS concept
MERGE (c:Concept {name: concept.name})
ON CREATE SET c.type = concept.type, c.createdAt = datetime()
WITH c
MATCH (p:Pattern {id: $patternId})
CREATE (c)-[:MENTIONED_IN]->(p)

// Step 6: Delete old RELATED_TO edges for this pattern
MATCH (p:Pattern {id: $patternId})-[r:RELATED_TO]-()
DELETE r
```

### RELATED_TO Edge Computation

RELATED_TO is a symmetric relationship. Edges are created in one direction only, and queries use direction-agnostic traversal (`MATCH (a)-[:RELATED_TO]-(b)`, no arrow). This is the standard Neo4j pattern for symmetric relationships.

After concept extraction and MENTIONED_IN edge creation, the enrichment pipeline
computes direct RELATED_TO edges between patterns:

1. For each newly enriched pattern, query Neo4j for other patterns that share
   concepts (via MENTIONED_IN traversal)
2. Compute a similarity score (0.0-1.0) based on concept overlap only:

   ```text
   similarity = sharedConcepts / max(totalConceptsA, totalConceptsB)
   ```

3. Create RELATED_TO edges between pattern pairs with the computed similarity score
4. Edges below a minimum threshold (default 0.3) are not created; the threshold
   is configurable via `enrichment.related_to_min_similarity` (see [configuration.md](configuration.md))

This step runs as part of the asynchronous enrichment pipeline, after embedding
generation and concept extraction.

#### Step 5: Update Status

On successful completion:

```sql
UPDATE patterns
SET enrichment_status = 'enriched',
    enriched_at = NOW(),
    enrichment_error = NULL
WHERE id = $patternId;
```

On failure:

```sql
UPDATE patterns
SET enrichment_status = 'failed',
    enrichment_error = $errorMessage
WHERE id = $patternId;
```

### Query-time Processing

When patterns are retrieved via MCP `search_patterns` tool or Admin API `GET /v1/api/patterns/search`:

```mermaid
stateDiagram-v2
    [*] --> EmbedQuery: Pattern Search Request

    state "1. Embed Query" as EmbedQuery
    note right of EmbedQuery
        Generate embedding from query
    end note

    EmbedQuery --> VectorSearch

    state "2. Chunk-Level Vector Search" as VectorSearch
    note right of VectorSearch
        PGVector: Search pattern_chunks.embedding
        Filter by language, domain, tags, agent
        Only enriched chunks
    end note

    VectorSearch --> [*]
```

Note: Query-time search queries `pattern_chunks.embedding` and returns `ChunkMatch` results. Each result includes `section_title`, `chunk_index`, and parent pattern metadata (`pattern_name`, `entity_type`, `language`, `domain`, `tags`). Only chunks with `enrichment_status = 'enriched'` appear in results.

Graph traversal to expand and re-rank results via Neo4j is a post-MVP enhancement (see Relevance Scoring below).

#### Relevance Scoring

**MVP**: `search_patterns` ranks results by vector similarity only (PGVector cosine similarity). This is the similarity score returned in results.

**Post-MVP Enhancement**: Blended scoring combining vector similarity with graph context:

```text
relevance = (0.7 × vector_similarity) + (0.3 × graph_score)
```

Where `graph_score` would consider direct agent association relevance, hop distance from matched patterns, and shared concept count. The algorithm for computing `graph_score` will be designed when this enhancement is prioritized.

## Enrichment Worker Deployment

> **Architecture Reference:** [Deployment Architecture - Component Deployment](../../architecture/06-deployment-architecture.md#component-deployment) | [Deployment Architecture - Scaling Considerations](../../architecture/06-deployment-architecture.md#scaling-considerations)

### In-Process Background Worker with RabbitMQ

The enrichment worker runs as a background goroutine within the same Mnemonic process, consuming messages from RabbitMQ:

```mermaid
flowchart LR
    subgraph mnemonic["Mnemonic Process"]
        A[HTTP Handler] --> RMQ_PUB[RabbitMQ Publisher]
        RMQ_PUB --> RMQ["enrichment-jobs<br/>Queue"]
        RMQ --> C[Background Worker<br/>goroutine]
        C --> D[Embedding Service]
        C --> E[Entity Extraction Service]
    end

    F[(Postgres<br/>+ PGVector)] <--> A
    F <--> C
    G[(Neo4j)] <--> C
    H[OpenAI API] <--> D
    H <--> E
```

**Why RabbitMQ?**

- **Reliable message delivery**: Messages persisted in RabbitMQ, safe across restarts
- **Decoupled publisher/subscriber**: API publishes jobs; workers consume asynchronously
- **Horizontal scaling**: Multiple workers can safely consume from the same queue
- **Dead-letter handling**: Failed messages can be routed to DLQ for analysis

### Job Queue Design

Use a RabbitMQ message queue for reliable job delivery:

**RabbitMQ Queue Configuration:**

```go
// Queue: enrichment-jobs
// Properties:
//   Durable: true (survives broker restarts)
//   Exclusive: false (shareable among workers)
//   AutoDelete: false (persists when empty)
//   Arguments:
//     x-max-priority: 10 (support priority levels)
//     x-dead-letter-exchange: "enrichment-dlx" (failed messages route here)

ch.QueueDeclare(
    name: "enrichment-jobs",
    durable: true,
    exclusive: false,
    autoDelete: false,
    noWait: false,
    args: amqp.Table{
        "x-max-priority": 10,
        "x-dead-letter-exchange": "enrichment-dlx",
    },
)
```

**Message Format:**

```json
{
  "pattern_id": "550e8400-e29b-41d4-a716-446655440001",
  "chunk_ids": ["..."],
  "retry_count": 0,
  "max_retries": 3,
  "priority": 5
}
```

Worker subscription:

```go
// Worker subscribes to queue with manual acknowledgment
msgs, err := ch.Consume(
    queue: "enrichment-jobs",
    consumer: "",
    autoAck: false,  // Manual acknowledgment required
    exclusive: false,
    noLocal: false,
    noWait: false,
    args: nil,
)

// Process messages
for msg := range msgs {
    job := parseMessage(msg.Body)
    err := processEnrichment(job)
    if err != nil {
        // Nack and requeue (with backoff via dead-letter)
        msg.Nack(false, true)
    } else {
        // Acknowledge successful processing
        msg.Ack(false)
    }
}
```

### Scaling and Concurrency

When running multiple Mnemonic instances (pods), all instances can safely process enrichment jobs concurrently without duplicate processing. This is achieved through RabbitMQ message delivery semantics.

#### How Multi-Pod Message Consumption Works

```mermaid
sequenceDiagram
    participant Pod1 as Mnemonic Pod 1
    participant Pod2 as Mnemonic Pod 2
    participant RMQ as RabbitMQ

    Note over Pod1,Pod2: Both pods subscribe to enrichment-jobs queue

    Pod1->>RMQ: Consume (ch.Consume)
    Pod2->>RMQ: Consume (ch.Consume)

    Note over RMQ: RabbitMQ round-robins messages
    RMQ-->>Pod1: Message A (Job A)

    RMQ-->>Pod2: Message B (Job B)

    Pod1->>Pod1: Process Job A
    Pod2->>Pod2: Process Job B

    Pod1->>RMQ: Ack message (manual acknowledgment)
    Pod2->>RMQ: Ack message (manual acknowledgment)

    Note over Pod1,Pod2: Each pod processes different jobs
```

#### RabbitMQ Message Delivery Guarantee

RabbitMQ ensures exactly-once delivery with manual acknowledgment:

**RabbitMQ Delivery Flow:**

```
1. Message published to enrichment-jobs queue
2. RabbitMQ marks message as "unacked"
3. Worker receives message (ch.Consume)
4. Worker processes enrichment job
5. Worker explicitly acknowledges (msg.Ack)
6. RabbitMQ removes message from queue
```

**Failure Handling:**

- **Worker crashes mid-processing**: Message remains unacked; RabbitMQ redelivers to next worker
- **Processing fails**: Worker nacks (msg.Nack); message requeued with backoff via dead-letter exchange
- **Max retries exceeded**: Message moves to dead-letter queue for analysis

This means:

- **No duplicate processing**: RabbitMQ ensures only one worker processes each message
- **No blocking**: Workers don't wait on each other; RabbitMQ distributes messages asynchronously
- **Automatic failover**: If a pod crashes, unacked messages are redelivered to other workers
- **Backpressure handling**: Dead-letter exchange manages retries with exponential backoff

#### Message Timeout and Recovery

To handle crashed workers, RabbitMQ uses message time-to-live (TTL) and dead-letter exchange:

```go
// Configure message TTL (5 minutes for processing timeout)
// Configure dead-letter exchange for redelivery
ch.ExchangeDeclare(
    name: "enrichment-dlx",
    kind: "direct",
    durable: true,
)

// When worker nacks or TTL expires, message goes to DLX
// DLX has configurable backoff before redelivery
// After max retries, message moves to poison-pill queue for analysis
```

**Retry Strategy:**

- First attempt: Immediate delivery
- Retry 1: After 30 seconds (via dead-letter with TTL)
- Retry 2: After 60 seconds
- Retry 3: After 120 seconds
- Max retries exceeded: Move to `enrichment-failed` queue for manual inspection

#### Horizontal Scaling Behavior

| Pods   | Behavior                                       |
| ------ | ---------------------------------------------- |
| 1 pod  | Single worker consumes from queue               |
| 2 pods | Jobs distributed round-robin; ~2x throughput  |
| N pods | Jobs distributed round-robin; ~Nx throughput  |

**Note**: Throughput scales linearly until limited by:

- RabbitMQ channel limits and memory
- OpenAI API rate limits (shared across all pods)
- Postgres connection pool exhaustion
- Neo4j write capacity

RabbitMQ automatically distributes messages across connected consumers (worker goroutines) in round-robin fashion without requiring external coordination.

### Future Scaling: Dedicated Enrichment Processor

For larger deployments or separation of concerns, the enrichment worker can be extracted to a dedicated service that consumes from RabbitMQ:

```mermaid
flowchart TB
    subgraph api["Mnemonic API (Multiple Pods)"]
        A1[API Pod 1]
        A2[API Pod 2]
        A3[API Pod N]
    end

    subgraph rmq["RabbitMQ Message Broker"]
        Q["enrichment-jobs<br/>Queue"]
        DLX["enrichment-dlx<br/>(Dead-Letter Exchange)"]
    end

    subgraph worker["Enrichment Processor (Separate Service)"]
        W1[Worker Pod 1]
        W2[Worker Pod 2]
    end

    PG[(Postgres)]
    OpenAI[OpenAI API]
    Neo4j[(Neo4j)]

    A1 -->|Publish| Q
    A2 -->|Publish| Q
    A3 -->|Publish| Q

    Q -->|Consume| W1
    Q -->|Consume| W2

    W1 <--> PG
    W2 <--> PG
    W1 <--> OpenAI
    W2 <--> OpenAI
    W1 <--> Neo4j
    W2 <--> Neo4j

    W1 -->|Nack (retry)| DLX
    W2 -->|Nack (retry)| DLX
    DLX -->|Requeue with backoff| Q
```

**Why consider a dedicated enrichment processor?**

| Benefit                    | Description                                                             |
| -------------------------- | ----------------------------------------------------------------------- |
| **Separation of concerns** | API publishes jobs to queue; processors consume asynchronously           |
| **Independent scaling**    | Scale API pods for request volume; scale workers for enrichment backlog |
| **Resource isolation**     | LLM calls don't compete with API request handling                       |
| **Deployment flexibility** | Update enrichment logic without redeploying API                         |
| **Cost optimization**      | Run workers on cheaper/burstable instances                              |

**When to migrate to dedicated processor:**

- Enrichment backlog consistently grows (processing can't keep up)
- API latency affected by enrichment worker resource usage
- Need to scale enrichment independently from API
- Want to deploy enrichment changes without API downtime

**Migration path:**

1. Extract worker code to separate Go binary (same codebase, different main)
2. Deploy as separate container/service that connects to RabbitMQ
3. Remove in-process worker from API pods
4. Scale worker pods based on queue depth and monitoring
5. RabbitMQ ensures safe distributed processing without code changes

## External Service Dependencies

> **Architecture Reference:** [Requirements - Non-Goals](../mnemonic-requirements.md#non-goals) | [System Architecture - Boundary Definitions](../../architecture/02-system-architecture.md#boundary-definitions)

Pattern enrichment requires external API calls for embedding generation and entity extraction.

### OpenAI API (Embeddings)

Embedding generation requires the OpenAI API:

| Requirement        | Details                                                   |
| ------------------ | --------------------------------------------------------- |
| **Service**        | OpenAI API                                                |
| **Endpoint**       | `https://api.openai.com/v1/embeddings`                    |
| **Model**          | `text-embedding-3-small`                                  |
| **Dimensions**     | 1536 (must match PGVector column configuration)           |
| **Authentication** | API key required                                          |
| **Cost**           | ~$0.0001 per pattern (~$0.00002 per 1K tokens)            |
| **Rate limits**    | 3,000 RPM / 1,000,000 TPM (tier 1), higher for paid tiers |

**Why OpenAI?**

- Industry-standard embedding quality
- Simple API integration
- Predictable costs at scale
- No infrastructure to maintain

Additional embedding providers (Azure OpenAI, self-hosted models) can be added post-MVP if needed.

### OpenAI API (Entity Extraction)

Entity extraction uses the OpenAI API:

| Requirement        | Details                                               |
| ------------------ | ----------------------------------------------------- |
| **Service**        | OpenAI API                                            |
| **Endpoint**       | `https://api.openai.com/v1/chat/completions`          |
| **Model**          | `gpt-4o-mini`                                         |
| **Authentication** | API key required (same key used for embeddings)       |
| **Cost**           | ~$0.01-0.05 per pattern                               |
| **Rate limits**    | 500 RPM / 200,000 TPM (tier 1), higher for paid tiers |

Additional LLM providers (Anthropic, Azure OpenAI) can be added post-MVP if needed.

## Configuration Requirements

> **Architecture Reference:** [Deployment Architecture - Operational Considerations](../../architecture/06-deployment-architecture.md#operational-considerations)

### Required Environment Variables

```bash
# Required for embedding generation and entity extraction
MNEMONIC_OPENAI_API_KEY=sk-...
```

### Application Configuration

Configuration follows the patterns established in [configuration.md](configuration.md).

```yaml
openai:
  # API key should be set via MNEMONIC_OPENAI_API_KEY
  api_key: ""

  # Embedding configuration
  embedding_model: text-embedding-3-small
  embedding_dimensions: 1536 # Must match PGVector column size

  # Entity extraction configuration
  extraction_model: gpt-4o-mini

  # Rate limiting (recommended)
  max_requests_per_minute: 500
  retry_attempts: 3
  retry_delay: 1s

# Enrichment worker configuration
enrichment:
  worker_count: 2 # Number of concurrent workers (goroutines)
  poll_interval: 5s # How often to check for new jobs
  max_attempts: 3 # Retry attempts before marking as failed
  retry_delay: 30s # Delay between retry attempts
  job_timeout: 5m  # Maximum time for a single enrichment job

# Neo4j configuration (required)
neo4j:
  uri: bolt://localhost:7687
  username: neo4j
  # password should be set via MNEMONIC_DATABASE_NEO4J_PASSWORD
  password: ""
  database: neo4j
```

Changing the embedding model requires re-embedding all existing patterns - the dimensions must match across all stored vectors. Additional embedding providers can be supported post-MVP if needed.

## Cost and Latency

### Per-Pattern Processing

| Operation             | Time      | Cost            |
| --------------------- | --------- | --------------- |
| Embedding generation  | 100-200ms | ~$0.0001        |
| Entity extraction     | 1-5s      | ~$0.01-0.05     |
| Neo4j writes          | 50-100ms  | N/A             |
| **Total per pattern** | **1-5s**  | **~$0.01-0.05** |

The 1-5 second processing time per pattern reinforces why enrichment runs asynchronously. Users should not wait for this during API calls.

### Projected Monthly Costs

| Patterns/month | Embedding Cost | LLM Cost | Total    |
| -------------- | -------------- | -------- | -------- |
| 100            | $0.01          | $1-5     | $1-5     |
| 1,000          | $0.10          | $10-50   | $10-50   |
| 10,000         | $1.00          | $100-500 | $100-500 |

Query embeddings also incur costs (~$0.0001 per query). For high-volume query scenarios, consider caching strategies.

### Rate Limit Considerations

OpenAI enforces rate limits that affect burst processing:

- **Tier 1 (default)**: 3,000 requests/minute, 1M tokens/minute
- **Tier 2+**: Higher limits available with usage history

For bulk pattern imports, implement:

- Request queuing with backoff
- Batch processing with delays
- Rate limit monitoring and alerting

## Deployment Requirements

> **Architecture Reference:** [Deployment Architecture - Infrastructure Requirements](../../architecture/06-deployment-architecture.md#infrastructure-requirements) | [Deployment Architecture - Deployment Topology](../../architecture/06-deployment-architecture.md#deployment-topology)
>
> **Observability Reference:** [Enrichment Worker Observability](observability-implementation.md#enrichment-worker-observability) — metrics, tracing spans, and structured logging for the enrichment pipeline

### Infrastructure Checklist

Before deploying pattern enrichment, verify:

- [ ] OpenAI API key provisioned and tested
- [ ] API key stored securely (secrets manager, not in code)
- [ ] Environment variables configured in deployment
- [ ] Rate limits appropriate for expected load
- [ ] Cost monitoring/alerting configured
- [ ] Network egress to `api.openai.com` allowed
- [ ] PGVector dimensions match configured embedding dimensions (1536)
- [ ] Enrichment job table created in Postgres
- [ ] Neo4j instance provisioned and accessible
- [ ] Neo4j credentials configured

### Failure Modes

| Failure                 | Impact                                 | Mitigation                        |
| ----------------------- | -------------------------------------- | --------------------------------- |
| Invalid/missing API key | All enrichment jobs fail               | Startup validation, health checks |
| Rate limit exceeded     | Temporary failures, 429 responses      | Exponential backoff, queuing      |
| OpenAI outage           | Embedding/extraction unavailable       | Circuit breaker, queue for retry  |
| Neo4j unavailable       | Relationship storage fails             | Circuit breaker, queue for retry  |
| Dimension mismatch      | Vectors unusable for similarity search | Validate config at startup        |
| Network blocked         | Cannot reach external APIs             | Verify egress rules               |
| Worker crash            | Jobs stuck in "processing"             | Job timeout, automatic requeuing  |

### Health Check Endpoint

The pattern service should expose a health check that validates enrichment capability:

```go
// Health check should verify:
// 1. OpenAI API key is configured
// 2. OpenAI API is reachable (optional: test calls)
// 3. PGVector is available with correct dimensions
// 4. Neo4j is available and accessible
// 5. Enrichment worker is running
// 6. Job queue is accessible
```

## Internal Dependencies

> **Architecture Reference:** [System Architecture - Mnemonic](../../architecture/02-system-architecture.md#mnemonic)

### PGVector Configuration

Vector embeddings are stored in `pattern_chunks.embedding`, not `patterns.embedding`. The `patterns` table no longer has an `embedding` column.

```sql
-- Recommended index for chunk-level search
CREATE INDEX ON pattern_chunks
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);

-- For larger collections (10K+), consider HNSW
CREATE INDEX ON pattern_chunks
USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);
```

The vector column must be configured for the same dimensions as the embedding model:

```sql
-- Must match embedding.dimensions in config (default: 1536)
-- Column lives in pattern_chunks, not patterns
ALTER TABLE pattern_chunks ADD COLUMN embedding vector(1536);
```

### Neo4j Schema

```cypher
// Create constraints for pattern nodes
CREATE CONSTRAINT pattern_id IF NOT EXISTS
FOR (p:Pattern) REQUIRE p.id IS UNIQUE;

// Create constraints for agent nodes
CREATE CONSTRAINT agent_name IF NOT EXISTS
FOR (a:Agent) REQUIRE a.name IS UNIQUE;

// Create constraints for concept nodes
CREATE CONSTRAINT concept_name IF NOT EXISTS
FOR (c:Concept) REQUIRE c.name IS UNIQUE;

// Create indexes for common queries
CREATE INDEX pattern_name IF NOT EXISTS
FOR (p:Pattern) ON (p.name);

// Full-text indexes for search
CREATE FULLTEXT INDEX pattern_content_fulltext IF NOT EXISTS
FOR (p:Pattern) ON EACH [p.name, p.description];

CREATE FULLTEXT INDEX concept_name_fulltext IF NOT EXISTS
FOR (c:Concept) ON EACH [c.name];

// Property index for concept type filtering
CREATE INDEX concept_type_index IF NOT EXISTS
FOR (c:Concept) ON (c.type);
```

### Entity Extraction Prompt

```text
Extract key concepts from this pattern document.

Return JSON with:
- concepts: General programming concepts
- technologies: Languages, frameworks, tools
- practices: Best practices, patterns, methodologies

Pattern content:
{content}
```

## References

- [Architecture Overview](../../architecture/README.md)
- [System Architecture](../../architecture/02-system-architecture.md) - Storage stack details
- [Mnemonic OpenAPI Spec](../../../api/openapi/mnemonic-v1.yaml) - Full API definition
