# System Architecture

[Back to Overview](README.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Component Breakdown](#component-breakdown)
  - [Mnemonic Server](#mnemonic-server)
  - [Claude Code Integration](#claude-code-integration)
- [Data Flow](#data-flow)
- [Component Interactions](#component-interactions)
- [Boundary Definitions](#boundary-definitions)
  - [Cross-Cutting Concerns](#cross-cutting-concerns)
- [Post-MVP](#post-mvp)
  - [Expanded Component Topology](#expanded-component-topology)
  - [Boundary Changes](#boundary-changes)
  - [Multiple Instances](#multiple-instances)

## Architecture Overview

Mnemonic follows a single-server architecture with dual protocol interfaces: REST for administration and MCP for Claude Code integration.

```mermaid
graph TB
    subgraph "Claude Code"
        CC[Claude Code MCP Client]
    end

    subgraph "Admin Tools"
        CURL[curl/scripts]
    end

    subgraph "Mnemonic Server"
        subgraph "Two HTTP Listeners"
            ADMIN[Admin REST API :8080]
            MCP[MCP Server :8081]
        end
        PATTERN[Pattern Enrichment Service]
        TOOLING[Tooling Repository]
    end

    subgraph "Data Tier"
        PG[(Postgres + PGVector)]
        NEO[(Neo4j)]
    end

    subgraph "External Services"
        OPENAI[OpenAI Embedding API]
    end

    CURL -->|"POST/PUT/DELETE"| ADMIN
    CC -->|"MCP: Pattern search"| MCP
    ADMIN --> PATTERN
    ADMIN --> TOOLING
    MCP --> PATTERN
    PATTERN <--> PG
    PATTERN <--> NEO
    TOOLING <--> PG
    PATTERN <--> OPENAI
```

## Component Breakdown

### Mnemonic Server

Mnemonic is a single Go server with two HTTP listeners providing team knowledge graph and tooling synchronization.

**Responsibilities:**

- **Admin REST API** (`:8080`): CRUD operations for patterns, agents, and skills
- **MCP Server** (`:8081`): Read-only pattern search for Claude Code (3 tools)
- **Pattern enrichment**: Semantic search via PGVector, knowledge graph via Neo4j
- **Tooling synchronization**: Shared agent and skill definitions across team

**Key Characteristics:**

- Single server process, two HTTP listeners
- Lightweight service (calls embedding API only; no generative inference)
- Stateless request handling
- Dual protocol architecture (REST admin + MCP read-only)
- Full storage stack: Postgres + PGVector + Neo4j

```mermaid
graph TB
    subgraph "Mnemonic Internal Structure"
        subgraph "Admin REST API :8080"
            REST_HANDLER[HTTP Handler]
            REST_VALID[Request Validator]
            ADMIN_SVC[Admin Handler]
        end

        subgraph "MCP Server :8081"
            MCP_HANDLER[MCP Handler]
            MCP_TOOLS[Tool Implementations]
        end

        subgraph "Core Services"
            PATTERN[Pattern Service]
            AGENT[Agent Service]
            SKILL[Skill Service]
        end

        subgraph "Storage Layer"
            PG[(Postgres)]
            PGV[(PGVector)]
            NEO[(Neo4j)]
        end

        subgraph "Background Processing"
            ENRICH[Enrichment Worker]
        end
    end

    subgraph "External Services"
        OPENAI[OpenAI Embedding API]
    end

    REST_HANDLER --> REST_VALID
    REST_VALID --> ADMIN_SVC
    ADMIN_SVC --> PATTERN
    ADMIN_SVC --> AGENT
    ADMIN_SVC --> SKILL

    MCP_HANDLER --> MCP_TOOLS
    MCP_TOOLS --> PATTERN

    PATTERN <--> PG
    PATTERN <--> PGV
    PATTERN <--> NEO
    AGENT <--> PG
    SKILL <--> PG

    ENRICH --> PATTERN
    ENRICH --> OPENAI
    PATTERN --> OPENAI
```

**What Mnemonic Does NOT Do:**

- Perform LLM inference (Mnemonic calls an embedding API for pattern enrichment, but does not perform generative AI inference)
- Store user credentials
- Execute tools or file operations
- Maintain session state

### Claude Code Integration

Claude Code integrates with Mnemonic via the MCP (Model Context Protocol) interface.

**MCP Tools Provided (3 pattern search tools):**

| Tool                    | Purpose                                  |
| ----------------------- | ---------------------------------------- |
| `search_patterns`       | Semantic search over team knowledge graph |
| `find_related_patterns` | Find patterns related to a given pattern |
| `get_pattern`           | Retrieve specific pattern by ID          |

For full tool definitions and parameters, see [MCP Tools](08-mcp-tools.md).

**Integration Characteristics:**

- Read-only access via MCP
- Runs in trusted environment (local network)
- No authentication (MVP)
- Searches team knowledge for workflow patterns

## Data Flow

The following diagrams show data flow for the two primary use cases.

### Pattern Search via MCP

```mermaid
sequenceDiagram
    participant User
    participant CC as Claude Code
    participant MCP as MCP Server
    participant OPENAI as OpenAI Embedding API
    participant PG as Postgres + PGVector

    User->>CC: Ask question
    CC->>CC: Determine need for team knowledge

    CC->>MCP: search_patterns(query, limit)

    MCP->>OPENAI: Generate query embedding
    OPENAI-->>MCP: vector(1536)
    MCP->>PG: Vector similarity search (PGVector)
    PG-->>MCP: Ranked patterns + similarity scores

    MCP-->>CC: {patterns ranked by vector similarity}

    CC->>CC: Incorporate team knowledge
    CC-->>User: Answer with team context
```

Post-MVP: search_patterns will incorporate Neo4j graph scores for blended ranking.

### Data Loading via Admin API

```mermaid
sequenceDiagram
    participant Admin
    participant REST as Admin REST API
    participant PG as Postgres
    participant WORKER as Background Worker
    participant OPENAI as OpenAI Embedding API
    participant NEO as Neo4j

    Admin->>REST: POST /v1/api/patterns (JSON)
    REST->>REST: Validate request
    REST->>PG: Store pattern (status: pending)
    REST->>PG: Queue enrichment job
    PG-->>REST: Pattern ID
    REST-->>Admin: 202 Accepted

    Note over WORKER: Async enrichment
    WORKER->>PG: Claim job (FOR UPDATE SKIP LOCKED)
    WORKER->>OPENAI: Generate embedding
    OPENAI-->>WORKER: vector(1536)
    WORKER->>PG: Store embedding, status: enriched
    WORKER->>NEO: Create knowledge graph nodes/relationships
    NEO-->>WORKER: Success
    WORKER->>PG: Job completed
```

## Component Interactions

### Claude Code to MCP Server

| Aspect            | Detail                                         |
| ----------------- | ---------------------------------------------- |
| Protocol          | MCP over Streamable HTTP                       |
| Authentication    | None (MVP, local trusted environment)          |
| Request contains  | MCP tool name, parameters                      |
| Response contains | Search results, pattern details               |

### Admin Tools to REST API

| Aspect            | Detail                                                                            |
| ----------------- | --------------------------------------------------------------------------------- |
| Protocol          | REST (HTTP/HTTPS)                                                                 |
| Authentication    | None (MVP); see [Security Architecture](01-security-architecture.md) for Post-MVP |
| Request contains  | JSON payloads for CRUD operations                                                 |
| Response contains | Created/updated resources, success/error status                                   |

### Mnemonic to Storage Layer

| Aspect   | Detail                           |
| -------- | -------------------------------- |
| Postgres | Relational data, pattern storage |
| PGVector | Semantic search via embeddings   |
| Neo4j    | Knowledge graph relationships    |

## Boundary Definitions

Clear boundaries separate concerns between components.

```mermaid
graph TB
    subgraph "User Domain"
        UD1[Workflow and orchestration]
    end

    subgraph "Claude Code Domain"
        CD1[Tool execution]
        CD2[File operations]
        CD3[MCP client]
    end

    subgraph "Mnemonic Domain"
        MD1[Team knowledge graph]
        MD2[Pattern semantic search]
        MD3[Tooling synchronization]
    end

    subgraph "Admin Domain"
        AD1[Pattern CRUD]
        AD2[Agent CRUD]
        AD3[Skill CRUD]
    end

    UD1 --> CD3
    CD3 -->|"search_patterns"| MD2
    AD1 --> MD1
    AD2 --> MD3
```

**Boundary Rules:**

- The user drives all workflow and orchestration decisions
- Claude Code is the interface; Mnemonic is the memory
- Pattern storage and search live only in Mnemonic
- Tooling definitions (agents, skills) managed via admin API
- File operations happen only on the workstation
- Mnemonic never receives or stores user credentials

### Cross-Cutting Concerns

- **Observability**: Mnemonic emits structured logs, exposes a health check endpoint, and publishes metrics on both interfaces. See [Observability Architecture](07-observability-architecture.md).
- **Schema migration**: Migrations are applied externally by `golang-migrate` CLI as a deployment step. Mnemonic verifies schema compatibility at startup but does not run migrations. See [Data Architecture - Migration Strategy](04-data-architecture.md#migration-strategy).

## Post-MVP

This section covers system-level changes when moving from local MVP to cloud or production deployment. It does not repeat MVP content — refer to the sections above for the single-instance topology.

### Expanded Component Topology

In production, a security proxy layer sits in front of Mnemonic. Envoy handles TLS termination and identity validation; OPA evaluates authorization policy as a sidecar. Multiple Mnemonic instances run behind a load balancer.

```mermaid
graph TB
    CLIENT[Client / Claude Code]
    LB[Load Balancer]

    subgraph "Security Proxy Layer"
        ENVOY[Envoy Proxy\nTLS termination\nJWT + API key validation]
        OPA[OPA Sidecar\nRBAC policy evaluation]
    end

    subgraph "Mnemonic Instances"
        MN1[Mnemonic Instance 1]
        MN2[Mnemonic Instance 2]
        MNN[Mnemonic Instance N]
    end

    subgraph "Data Tier"
        PG[(Postgres + PGVector)]
        NEO[(Neo4j)]
    end

    CLIENT -->|HTTPS| LB
    LB --> ENVOY
    ENVOY -->|"Authorization check"| OPA
    OPA -->|Allow / Deny| ENVOY
    ENVOY -->|"Trusted headers injected"| MN1
    ENVOY --> MN2
    ENVOY --> MNN
    MN1 <--> PG
    MN1 <--> NEO
    MN2 <--> PG
    MN2 <--> NEO
```

The request path is: Client → Load Balancer → Envoy (TLS + identity validation) → OPA check → Mnemonic.

See [Security Architecture - Component Architecture](01-security-architecture.md#component-architecture) for a detailed view of the Envoy and OPA configuration, and [Deployment Architecture - Post-MVP](06-deployment-architecture.md#post-mvp) for the full production deployment topology.

### Boundary Changes

The [MVP Boundary Definitions](#boundary-definitions) assume a trusted local network — Mnemonic accepts all traffic without verifying caller identity. In production the trust model shifts:

- Mnemonic only accepts traffic from Envoy (network isolation enforces this)
- Client-provided identity headers are stripped by Envoy before requests reach Mnemonic
- Mnemonic trusts the identity headers that Envoy injects (`X-User-ID`, `X-Team-ID`, `X-User-Roles`), not raw request headers
- Admin API requests must pass through the same auth path before reaching Mnemonic

Mnemonic itself requires no changes to authentication logic — it reads injected headers and proceeds. The security boundary is enforced externally.

See [Security Architecture - Identity Headers](01-security-architecture.md#identity-headers) for the full header specification.

### Multiple Instances

Mnemonic is stateless — all state lives in Postgres and Neo4j — so multiple instances can run behind a load balancer without coordination between them. Each instance is identical.

Two areas to review when scaling out:

- **Connection pool sizing**: Each instance holds its own connection pool. With N instances, total connections to Postgres and Neo4j multiply by N. See [Data Architecture - Connection Pool Configuration](04-data-architecture.md#connection-pool-configuration) for pool sizing guidance.
- **Background enrichment workers**: The enrichment worker uses `FOR UPDATE SKIP LOCKED` when claiming jobs, which is already part of the MVP design. This ensures safe concurrent access across instances without duplicate processing.

**Next:** [Communication Patterns](03-communication-patterns.md)
