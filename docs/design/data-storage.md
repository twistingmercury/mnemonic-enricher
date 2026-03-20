# Data Storage Implementation

[Back to Architecture Overview](../../architecture/README.md) | [Back to Project README](../../../README.md)

## Table of Contents

- [Overview](#overview)
- [JSONB Document Model](#jsonb-document-model)
  - [Design Rationale](#design-rationale)
  - [Common Table Shape](#common-table-shape)
  - [CRC64 Change Detection](#crc64-change-detection)
  - [JSONB Contents by Entity](#jsonb-contents-by-entity)
  - [JSONB Indexing Strategy](#jsonb-indexing-strategy)
  - [Application-Layer Validation](#application-layer-validation)
- [PostgreSQL Migrations](#postgresql-migrations)
  - [Migration File Structure](#migration-file-structure)
  - [Migration 001: Extensions](#migration-001-extensions)
  - [Migration 002: Agents Table](#migration-002-agents-table)
  - [Migration 003: Patterns Table](#migration-003-patterns-table)
  - [Migration 004: Pattern-Agent Associations](#migration-004-pattern-agent-associations)
  - [Migration 005: Enrichment Jobs Table](#migration-005-enrichment-jobs-table)
  - [Migration 006: Performance Indexes](#migration-006-performance-indexes)
  - [Migration 007: Create Skills Table](#migration-007-create-skills-table)
  - [Migration 008: Create Skill Files Table](#migration-008-create-skill-files-table)
- [PGVector Configuration](#pgvector-configuration)
  - [Index Selection Guidelines](#index-selection-guidelines)
  - [Similarity Search Queries](#similarity-search-queries)
  - [Index Maintenance](#index-maintenance)
- [Neo4j Setup](#neo4j-setup)
  - [Schema Constraints](#schema-constraints)
  - [Index Configuration](#index-configuration)
  - [Node Creation Patterns](#node-creation-patterns)
  - [Relationship Definitions](#relationship-definitions)
  - [Graph Synchronization Queries](#graph-synchronization-queries)
- [Repository Interfaces](#repository-interfaces)
  - [AgentRepository](#agentrepository)
  - [PatternRepository](#patternrepository)
  - [SkillRepository](#skillrepository)
  - [SkillFileRepository](#skillfilerepository)
  - [EnrichmentJob Repository](#enrichmentjob-repository)
  - [GraphRepository](#graphrepository)
- [Connection Configuration](#connection-configuration)
  - [PostgreSQL Connection](#postgresql-connection)
  - [Neo4j Connection](#neo4j-connection)
  - [Connection String Formats](#connection-string-formats)
- [References](#references)

## Overview

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture](../../architecture/04-data-architecture.md) | [System Architecture - Mnemonic](../../architecture/02-system-architecture.md#mnemonic)

This document provides implementation details for the Mnemonic data storage layer. It translates the architectural specifications from [Data Architecture](../../architecture/04-data-architecture.md) into concrete SQL migrations, Cypher queries, and Go interface definitions.

**Implementation Scope:**

| Component | Technology | Purpose |
|-----------|------------|---------|
| Relational Storage | PostgreSQL 15+ | Document tables (JSONB), patterns, jobs |
| Vector Storage | PGVector extension | Pattern embeddings for semantic search |
| Graph Storage | Neo4j 5.x | Knowledge graph relationships |
| Migrations | golang-migrate CLI | Schema versioning and deployment (run externally, not embedded in Mnemonic) |

**JSONB Document Model:**

- `agents`, `skills` use JSONB document model with `crc64` change detection
- `skill_files` stores skill child files with path and content columns
- `patterns` use relational columns (enrichment workflow, pgvector embeddings, graph context)
- `pattern_agent_associations` is a join table for agent-scoped pattern filtering
- `enrichment_jobs` is a background processing queue
- Neo4j is required for the knowledge graph

**Deployment Independence:**

Database migrations and application code are versioned and deployed independently. Migrations have their own CI/CD pipeline that triggers only on changes to the `migrations/` directory. This enables:

- Logic bug fixes in Go without database deployment
- Schema changes without rebuilding application containers
- Forward-compatible migrations for zero-downtime deployments

## JSONB Document Model

[Table of Contents](#table-of-contents)

### Design Rationale

Agents, skills, and skill files are markdown documents synced to disk. Their field sets evolve as the Claude Code Agent Skills spec evolves. Storing each field as an individual relational column creates migration churn every time a field is added, renamed, or removed.

The JSONB document model stores the full document as a single JSONB column. Only the fields required for database-level operations (lookup key, change detection, audit timestamps) are promoted to top-level columns. Everything else lives inside the JSONB document.

**Benefits:**

- **No migration churn.** Adding a field to the agent spec requires only an application change, not a database migration.
- **Spec alignment.** The JSONB column mirrors what the API returns and what the sync protocol transmits. No impedance mismatch between storage and wire format.
- **Simpler repository code.** One `definition JSONB` column replaces five or more individual columns. Reads and writes are straightforward marshal/unmarshal operations.

**Trade-offs:**

- **No column-level constraints on JSONB contents.** Field validation (max lengths, required fields, allowed values) is enforced by the application, not by CHECK constraints on individual columns.
- **GIN indexes required for JSONB queries.** Querying inside the document (tag filtering, field searches) requires GIN indexes instead of simple btree indexes.

### Common Table Shape

All document tables (agents, skills) share the same column structure:

| Column | Type | Constraints | Purpose |
|--------|------|-------------|---------|
| `id` | UUID | PK, DEFAULT gen_random_uuid() | Internal identifier |
| `name` | VARCHAR(255) | UNIQUE NOT NULL | Lookup key; the one field always queried by |
| `definition` | JSONB | NOT NULL | Complete document (all entity fields) |
| `crc64` | VARCHAR(20) | NOT NULL | CRC-64 checksum of serialized JSONB, for change detection |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now() | DB sets on INSERT |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now() | App updates on UPDATE |

The `skill_files` table stores child files for skills with `skill_id` and `path` as the composite lookup key, plus `content` as TEXT for the file body.

### CRC64 Change Detection

Each document table includes a `crc64` column storing a CRC-64 checksum of the JSONB content.

**Computation:**

- Computed server-side (in the Go application) on every INSERT and UPDATE.
- Input: the serialized JSONB content in canonical form (deterministic key ordering, no extra whitespace). Go's `encoding/json` produces deterministic output for the same struct, but the application should use a canonical serialization function to guarantee consistency.
- Algorithm: CRC-64 with ISO polynomial (matching Go's `hash/crc64` package with `crc64.MakeTable(crc64.ISO)`).
- Output: a 64-bit unsigned integer, stored as PostgreSQL VARCHAR(20) (decimal string representation). Go converts the uint64 to its decimal string form for storage and parses it back on read.

**Usage in the sync protocol:**

- The Admin REST API returns per-entity CRC64 values in list responses.
- The sync client compares local CRC64 values against the manifest to determine which entities have changed.
- A collection-level version hash can be derived from individual CRC64 values (e.g., XOR of all entity CRC64s for a given collection).

**Why CRC-64 and not SHA-256:**

CRC-64 is fast, fits in a single VARCHAR(20) column, and provides sufficient collision resistance for change detection (not security). The sync protocol uses it to answer "has this document changed?" not "is this document authentic?"

### JSONB Contents by Entity

#### Agents definition JSONB

The `definition` column stores the complete agent specification:

```json
{
  "description": "Implements Go code: functions, packages, tests",
  "system_prompt": "You are an expert Go engineer...",
  "model": "sonnet",
  "allowed_tools": ["Read", "Write", "Edit", "Bash"],
  "version": "1.2.0"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | yes | Short summary of the agent's purpose |
| `system_prompt` | string | yes | Full system prompt content |
| `model` | string | yes | Claude model: sonnet, opus, haiku |
| `allowed_tools` | string[] | yes | MCP tool names this agent can use |
| `version` | string | yes | Semantic version of the definition |

#### Skills definition JSONB

Aligned with the [Claude Code Agent Skills spec](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/skills) frontmatter fields:

```json
{
  "description": "Synchronize agents and skills from Mnemonic",
  "content": "---\nname: mnemonic-sync\n---\n\nYou are synchronizing...",
  "tags": ["sync", "infrastructure"],
  "license": "MIT",
  "compatibility": "Claude Code 1.0+",
  "metadata": {"author": "team-platform"},
  "allowed_tools": ["Read", "Write", "Bash"],
  "version": "1.0.0"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | yes | Short summary of the skill |
| `content` | string | yes | Full skill markdown content |
| `tags` | string[] | no | Categorization tags |
| `license` | string | no | License identifier (e.g., MIT) |
| `compatibility` | string | no | Compatible Claude Code versions |
| `metadata` | object | no | Arbitrary key-value metadata |
| `allowed_tools` | string[] | no | MCP tool names this skill can use |
| `version` | string | yes | Semantic version of the definition |

#### Skill files

Skill files do not use the JSONB document model. They store file content directly in a TEXT column with a `path` column for identification within the skill directory. The `crc64` column provides change detection on the file content.

| Column | Type | Purpose |
|--------|------|---------|
| `id` | UUID | PK, DEFAULT gen_random_uuid() |
| `skill_id` | UUID | FK to parent skill (CASCADE DELETE) |
| `path` | VARCHAR(1024) | File path within the skill directory |
| `content` | TEXT | File content |
| `crc64` | VARCHAR(20) | CRC-64 checksum for change detection |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now() |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT now(); app updates on UPDATE |

### JSONB Indexing Strategy

Document tables use GIN indexes on the `definition` column to support queries that filter by JSONB contents.

**Primary use case: tag filtering.**

Skills support tag-based filtering. The GIN index enables efficient `@>` (contains) queries:

```sql
-- Find skills tagged with "sync"
SELECT id, name, definition
FROM skills
WHERE definition @> '{"tags": ["sync"]}'::jsonb;
```

**Index definitions** (created in respective table migrations):

```sql
-- GIN index on definition column for tag filtering
CREATE INDEX idx_skills_definition ON skills USING GIN (definition);
```

**Path-specific GIN indexes** (alternative, more selective):

```sql
-- Index only the tags path within definition
CREATE INDEX idx_skills_definition_tags ON skills USING GIN ((definition -> 'tags'));
```

The path-specific indexes are smaller and faster for tag-only queries. Use full-column GIN indexes if queries need to filter on other JSONB fields.

### Application-Layer Validation

Because JSONB contents are not constrained at the database level, the application enforces all field validation:

| Entity | Field | Constraint | Enforced By |
|--------|-------|-----------|-------------|
| Agent | name | max 255 chars, unique | DB (VARCHAR + UNIQUE) |
| Agent | name | `^[a-z][a-z0-9-]*$` format | Application |
| Agent | definition.description | max 500 chars | Application |
| Agent | definition.system_prompt | max 50KB | Application |
| Agent | definition.model | one of: sonnet, opus, haiku | Application |
| Agent | definition.allowed_tools | must be string array | Application |
| Agent | definition.version | required, semver format | Application |
| Skill | name | max 255 chars, unique | DB (VARCHAR + UNIQUE) |
| Skill | name | `^[a-z][a-z0-9-]*$` format | Application |
| Skill | definition.description | max 1024 chars | Application |
| Skill | definition.content | max 512KB | Application |
| Skill | definition.version | required, semver format | Application |
| Skill File | path | max 1024 chars | DB (VARCHAR) |
| Skill File | content | max 1MB | Application |

The database enforces only structural constraints (primary keys, uniqueness, foreign keys). Name format validation and content-level validation belong to the application.

**Future Consideration:**

For database-level JSONB schema validation, consider the [pg_jsonschema](https://github.com/supabase/pg_jsonschema) PostgreSQL extension. This extension enables CHECK constraints that validate JSONB documents against JSON Schema specifications, providing an alternative to application-only validation.

## PostgreSQL Migrations

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture - Migration Strategy](../../architecture/04-data-architecture.md#migration-strategy)

### Migration File Structure

All PostgreSQL migrations follow the golang-migrate flat file convention. Migrations are run by the golang-migrate CLI as a deployment step; Mnemonic does not run or manage migrations at runtime.

```text
src/migrations/
├── postgres/
│   ├── 000001_extensions.up.sql
│   ├── 000001_extensions.down.sql
│   ├── 000002_create_agents.up.sql
│   ├── 000002_create_agents.down.sql
│   ├── 000003_create_patterns.up.sql
│   ├── 000003_create_patterns.down.sql
│   ├── 000004_create_pattern_agent_associations.up.sql
│   ├── 000004_create_pattern_agent_associations.down.sql
│   ├── 000005_create_enrichment_jobs.up.sql
│   ├── 000005_create_enrichment_jobs.down.sql
│   ├── 000006_create_performance_indexes.up.sql
│   ├── 000006_create_performance_indexes.down.sql
│   ├── 000007_create_skills.up.sql
│   ├── 000007_create_skills.down.sql
│   ├── 000008_create_skill_files.up.sql
│   └── 000008_create_skill_files.down.sql
└── neo4j/
    ├── 001_create_constraints.cypher
    ├── 002_create_existence_constraints.cypher
    └── 003_create_indexes.cypher
```

golang-migrate requires a single flat directory with paired `.up.sql` and `.down.sql` files. The 6-digit zero-padded prefix (e.g., `000001`) matches the output of `migrate create -ext sql -dir src/migrations/postgres -seq <name>`. The `-path` flag points to this directory.

Neo4j migrations are not managed by golang-migrate. They are numbered `.cypher` files applied manually via `cypher-shell` (see [Manual Constraint Creation](#startup-constraint-validation)).

**Running Migrations:**

```bash
# Apply all pending migrations
migrate -path src/migrations/postgres -database "$DATABASE_URL" up

# Rollback last migration
migrate -path src/migrations/postgres -database "$DATABASE_URL" down 1

# Check current version
migrate -path src/migrations/postgres -database "$DATABASE_URL" version
```

### Migration 001: Extensions

**Purpose:** Enable required PostgreSQL extensions.

```sql
-- src/migrations/postgres/000001_extensions.up.sql
-- Enables required PostgreSQL extensions.
-- Part of Mnemonic MVP

-- Enable vector operations for embeddings (pgvector extension)
create extension if not exists vector;
```

```sql
-- src/migrations/postgres/000001_extensions.down.sql
-- Extensions are not dropped to avoid breaking other schemas that may use them.
-- If you need to drop extensions, uncomment the following line:
-- drop extension if exists vector;
```

**Note:** Per the storage-only database philosophy, no trigger functions are created. `updated_at` management is the application's responsibility. The application sets `updated_at = now()` on every UPDATE.

### Migration 002: Agents Table

**Purpose:** Create the agents table using the JSONB document model with CRC64 change detection.

```sql
-- src/migrations/postgres/000002_create_agents.up.sql
-- Creates the agents table with JSONB document model.
-- Part of Mnemonic MVP

create table if not exists agents (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Unique lookup key: lowercase-with-hyphens format, URL-safe
    name varchar(255) unique not null,

    -- Complete agent definition as JSONB document
    definition jsonb not null,

    -- CRC-64 checksum of serialized definition for change detection
    crc64 varchar(20) not null,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- GIN index on definition for JSONB queries
create index idx_agents_definition on agents using gin (definition);

comment on table agents is 'Agent definitions stored as JSONB documents for team tooling synchronization';
comment on column agents.id is 'UUID primary key';
comment on column agents.name is 'Unique lookup key, lowercase-with-hyphens format';
comment on column agents.definition is 'Complete agent definition as JSONB document';
comment on column agents.crc64 is 'CRC-64 checksum of serialized definition for change detection';
```

```sql
-- src/migrations/postgres/000002_create_agents.down.sql
drop index if exists idx_agents_definition;
drop table if exists agents;
```

### Migration 003: Patterns Table

**Purpose:** Create the patterns table with PGVector embedding column for semantic search.

**Note:** Patterns use relational columns (not the JSONB document model) because they have enrichment status, graph context, and pgvector embeddings that require individual columns. Performance indexes (IVFFlat, GIN, full-text) are created separately in migration 006.

```sql
-- src/migrations/postgres/000003_create_patterns.up.sql
-- Creates the patterns table with PGVector embedding support.
-- Part of Mnemonic MVP

create table if not exists patterns (
    -- UUID primary key for stable references (patterns may be renamed)
    id uuid primary key default gen_random_uuid(),

    -- Pattern metadata
    name varchar(255) not null,
    description varchar(500),

    -- Pattern content (up to 10KB)
    content text not null,

    -- Categorization tags (JSON array)
    tags jsonb not null default '[]'::jsonb,

    -- Vector embedding for semantic search (1536 dimensions for text-embedding-3-small)
    embedding vector(1536),

    -- Enrichment processing state
    enrichment_status varchar(20) not null default 'pending',
    enrichment_error text,
    enriched_at timestamptz,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Constraints
    constraint patterns_name_unique unique (name),
    constraint patterns_content_length
        check (length(content) <= 10240),
    constraint patterns_tags_array
        check (jsonb_typeof(tags) = 'array'),
    constraint patterns_enrichment_status_valid
        check (enrichment_status in ('pending', 'enriched', 'failed'))
);

comment on table patterns is 'Reusable context patterns for prompt enrichment';
comment on column patterns.embedding is 'Vector embedding (1536d) for semantic similarity search';
comment on column patterns.enrichment_status is 'Processing state: pending, enriched, or failed';
```

```sql
-- src/migrations/postgres/000003_create_patterns.down.sql
drop table if exists patterns;
```

### Migration 004: Pattern-Agent Associations

**Purpose:** Create the many-to-many association table between patterns and agents with relevance scores.

**Note:** The `agent_id` column references `agents(id)` (UUID), not the agent name. This follows the post-pivot schema where agents have a UUID primary key.

```sql
-- src/migrations/postgres/000004_create_pattern_agent_associations.up.sql
-- Creates the pattern-agent association table for many-to-many relationships.
-- Part of Mnemonic MVP

create table if not exists pattern_agent_associations (
    -- Composite primary key
    pattern_id uuid not null,
    agent_id uuid not null,

    -- Relevance score (0.0 to 1.0)
    relevance double precision not null,

    -- Foreign keys
    constraint fk_pattern_agent_assoc_pattern
        foreign key (pattern_id) references patterns(id) on delete cascade,
    constraint fk_pattern_agent_assoc_agent
        foreign key (agent_id) references agents(id) on delete cascade,

    -- Primary key
    primary key (pattern_id, agent_id),

    -- Constraints
    constraint pattern_agent_assoc_relevance_range
        check (relevance >= 0 and relevance <= 1)
);

-- Index for reverse FK lookup (agent_id is not the leading PK column).
-- pattern_id lookup is covered by the composite PK index.
create index idx_pattern_agent_assoc_agent
    on pattern_agent_associations(agent_id);

comment on table pattern_agent_associations is
    'Many-to-many relationship between patterns and agents with relevance scores';
```

```sql
-- src/migrations/postgres/000004_create_pattern_agent_associations.down.sql
drop index if exists idx_pattern_agent_assoc_agent;
drop table if exists pattern_agent_associations;
```

### Migration 005: Enrichment Jobs Table

**Purpose:** Create the enrichment jobs queue table for background pattern processing.

```sql
-- src/migrations/postgres/000005_create_enrichment_jobs.up.sql
-- Creates the enrichment jobs queue table for background pattern processing.
-- Part of Mnemonic MVP

create table if not exists enrichment_jobs (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Reference to pattern being enriched
    pattern_id uuid not null,

    -- Job processing state
    status varchar(20) not null default 'pending',

    -- Retry tracking
    attempts integer not null default 0,
    max_attempts integer not null default 3,

    -- Error information
    last_error text,

    -- Scheduling and timing
    scheduled_for timestamptz not null default now(),
    started_at timestamptz,
    completed_at timestamptz,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Foreign key with cascade delete
    constraint fk_enrichment_jobs_pattern
        foreign key (pattern_id) references patterns(id) on delete cascade,

    -- Constraints
    constraint enrichment_jobs_status_valid
        check (status in ('pending', 'processing', 'completed', 'failed')),
    constraint enrichment_jobs_attempts_valid
        check (attempts >= 0),
    constraint enrichment_jobs_max_attempts_valid
        check (max_attempts >= 1)
);

-- Index for pattern lookups
create index idx_enrichment_jobs_pattern
    on enrichment_jobs(pattern_id);

-- Prevent duplicate pending or processing jobs for the same pattern
create unique index idx_enrichment_jobs_unique_pending
    on enrichment_jobs(pattern_id)
    where status in ('pending', 'processing');

comment on table enrichment_jobs is 'Background processing queue for pattern enrichment';
comment on column enrichment_jobs.status is 'Job state: pending, processing, completed, or failed';
comment on column enrichment_jobs.scheduled_for is 'When the job should be processed (supports delayed retry)';
```

```sql
-- src/migrations/postgres/000005_create_enrichment_jobs.down.sql
drop index if exists idx_enrichment_jobs_unique_pending;
drop index if exists idx_enrichment_jobs_pattern;
drop table if exists enrichment_jobs;
```

### Migration 006: Performance Indexes

**Purpose:** Create performance-optimized indexes for common query patterns.

```sql
-- src/migrations/postgres/000006_create_performance_indexes.up.sql
-- Creates performance-optimized indexes for common query patterns.
-- Part of Mnemonic MVP

-- =============================================================================
-- PATTERNS INDEXES
-- =============================================================================

-- Partial index for filtering to only enriched patterns
-- Used when selecting patterns eligible for similarity search
create index idx_patterns_enriched
    on patterns(id)
    where enrichment_status = 'enriched';

-- Vector similarity search (IVFFlat for MVP scale)
-- lists = 100 suitable for 1,000-10,000 patterns
create index idx_patterns_embedding
    on patterns using ivfflat (embedding vector_cosine_ops)
    with (lists = 100);

-- GIN index for tag filtering using JSONB containment operator (@>)
create index idx_patterns_tags
    on patterns using gin (tags);

-- Full-text search on name and description
create index idx_patterns_search
    on patterns using gin (
        to_tsvector('english', name || ' ' || coalesce(description, ''))
    );

-- =============================================================================
-- ENRICHMENT JOBS INDEXES
-- =============================================================================

-- Pending jobs by scheduled time (worker polling)
create index idx_enrichment_jobs_pending
    on enrichment_jobs(scheduled_for)
    where status = 'pending';

-- Processing jobs for timeout detection
create index idx_enrichment_jobs_processing
    on enrichment_jobs(started_at)
    where status = 'processing';

-- Index documentation
comment on index idx_patterns_embedding is
    'IVFFlat index for vector similarity search (100 lists for MVP scale)';
comment on index idx_enrichment_jobs_pending is
    'Optimizes worker polling for pending jobs';
```

```sql
-- src/migrations/postgres/000006_create_performance_indexes.down.sql
drop index if exists idx_enrichment_jobs_processing;
drop index if exists idx_enrichment_jobs_pending;
drop index if exists idx_patterns_search;
drop index if exists idx_patterns_tags;
drop index if exists idx_patterns_embedding;
drop index if exists idx_patterns_enriched;
```

### Migration 007: Create Skills Table

**Purpose:** Create the skills table using the JSONB document model.

```sql
-- src/migrations/postgres/000007_create_skills.up.sql
-- Creates the skills table with JSONB document model.
-- Part of Mnemonic MVP

create table if not exists skills (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Unique lookup key: matches Claude Code skill directory name
    name varchar(255) unique not null,

    -- Complete skill definition as JSONB document
    definition jsonb not null,

    -- CRC-64 checksum of serialized definition for change detection
    crc64 varchar(20) not null,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- GIN index on definition for tag filtering and JSONB queries
create index idx_skills_definition on skills using gin (definition);

comment on table skills is 'Skill definitions stored as JSONB documents for team tooling synchronization';
comment on column skills.name is 'Unique lookup key, lowercase-with-hyphens (matches Claude Code skill directory name)';
comment on column skills.definition is 'Complete skill definition as JSONB document (Agent Skills spec aligned)';
comment on column skills.crc64 is 'CRC-64 checksum of serialized definition for change detection';
```

```sql
-- src/migrations/postgres/000007_create_skills.down.sql
drop index if exists idx_skills_definition;
drop table if exists skills;
```

### Migration 008: Create Skill Files Table

**Purpose:** Create the skill_files table for files associated with skills.

```sql
-- src/migrations/postgres/000008_create_skill_files.up.sql
-- Creates the skill_files table for files associated with skills.
-- Part of Mnemonic MVP

create table if not exists skill_files (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Parent skill reference, cascade delete
    skill_id uuid not null references skills(id) on delete cascade,

    -- File path within the skill directory
    path varchar(1024) not null,

    -- File content
    content text not null,

    -- CRC-64 checksum of content for change detection
    crc64 varchar(20) not null,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Unique constraint: one file per path per skill
    constraint skill_files_unique_path unique (skill_id, path)
);

-- Index for skill_id lookups (foreign key)
create index idx_skill_files_skill_id on skill_files(skill_id);

comment on table skill_files is 'Child files (scripts, references, assets) for skill definitions';
comment on column skill_files.skill_id is 'Parent skill reference, cascade delete';
comment on column skill_files.path is 'File path within the skill directory';
comment on column skill_files.content is 'File content';
comment on column skill_files.crc64 is 'CRC-64 checksum of content for change detection';
```

```sql
-- src/migrations/postgres/000008_create_skill_files.down.sql
drop index if exists idx_skill_files_skill_id;
drop table if exists skill_files;
```

## PGVector Configuration

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture - PGVector Configuration](../../architecture/04-data-architecture.md#pgvector-configuration)

### Index Selection Guidelines

The vector index type should match the expected pattern count:

| Pattern Count | Index Type | Configuration | Query Performance |
|---------------|------------|---------------|-------------------|
| < 1,000 | None (exact search) | N/A | ~10ms |
| 1,000 - 100,000 | IVFFlat | `lists = sqrt(N)` | ~20-50ms |
| > 100,000 | HNSW | `m=16, ef_construction=64` | ~5-20ms |

**MVP Recommendation:** IVFFlat with 100 lists (suitable for 1,000-10,000 patterns).

**Upgrading to HNSW** (when pattern count exceeds 100,000):

```sql
-- Drop existing IVFFlat index
drop index if exists idx_patterns_embedding;

-- Create HNSW index (higher memory usage, better recall)
create index idx_patterns_embedding
    on patterns using hnsw (embedding vector_cosine_ops)
    with (m = 16, ef_construction = 64);
```

### Similarity Search Queries

**Basic Similarity Search:**

```sql
-- Find patterns similar to a given embedding
-- Returns patterns with similarity score (1.0 = identical)
select
    id,
    name,
    content,
    1 - (embedding <=> $1::vector) as similarity
from patterns
where enrichment_status = 'enriched'
  and embedding is not null
order by embedding <=> $1::vector
limit $2;  -- max_patterns parameter
```

**Similarity Search with Threshold:**

```sql
-- Find patterns above a similarity threshold
-- $1: query embedding, $2: minimum similarity (e.g., 0.7), $3: limit
select
    id,
    name,
    content,
    tags,
    1 - (embedding <=> $1::vector) as similarity
from patterns
where enrichment_status = 'enriched'
  and embedding is not null
  and (embedding <=> $1::vector) < (1 - $2)  -- convert similarity to distance
order by embedding <=> $1::vector
limit $3;
```

**Similarity Search with Tag Filter:**

```sql
-- Find similar patterns filtered by tags
-- $1: query embedding, $2: required tag, $3: limit
select
    id,
    name,
    content,
    1 - (embedding <=> $1::vector) as similarity
from patterns
where enrichment_status = 'enriched'
  and embedding is not null
  and tags @> $2::jsonb  -- contains the specified tag
order by embedding <=> $1::vector
limit $3;
```

### Index Maintenance

**Adjusting IVFFlat Probes:**

```sql
-- Increase probes for better recall (at cost of latency)
-- Default is 1, higher values check more lists
set ivfflat.probes = 10;

-- Query with increased probes
select id, name, 1 - (embedding <=> $1::vector) as similarity
from patterns
where enrichment_status = 'enriched'
order by embedding <=> $1::vector
limit 5;

-- Reset to default
reset ivfflat.probes;
```

**Reindexing After Bulk Updates:**

```sql
-- Reindex if significant portion of patterns updated
-- Use CONCURRENTLY to avoid blocking reads
reindex index concurrently idx_patterns_embedding;
```

**Analyzing for Query Planner:**

```sql
-- Update statistics after bulk operations
analyze patterns;
```

## Neo4j Setup

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Data Architecture - Neo4j Graph Model](../../architecture/04-data-architecture.md#neo4j-graph-model)

### Schema Constraints

Create these constraints during initial Neo4j setup.

**Uniqueness Constraints (Community Edition + Enterprise Edition):**

These constraints are compatible with all Neo4j editions and are created by `001_create_constraints.cypher`:

```cypher
// src/migrations/neo4j/001_create_constraints.cypher
// Creates uniqueness constraints for node labels
// Part of Mnemonic MVP

// Pattern nodes: UUID from Postgres
CREATE CONSTRAINT pattern_id_unique IF NOT EXISTS
FOR (p:Pattern) REQUIRE p.id IS UNIQUE;

// Agent nodes: name from Postgres
CREATE CONSTRAINT agent_name_unique IF NOT EXISTS
FOR (a:Agent) REQUIRE a.name IS UNIQUE;

// Concept nodes: normalized lowercase name
CREATE CONSTRAINT concept_name_unique IF NOT EXISTS
FOR (c:Concept) REQUIRE c.name IS UNIQUE;
```

**Existence Constraints (Enterprise Edition Only):**

These constraints require Neo4j Enterprise Edition and are created by `002_create_existence_constraints.cypher`. Community Edition users should skip this migration. The application layer enforces property completeness regardless of whether these database-level constraints are present.

```cypher
// src/migrations/neo4j/002_create_existence_constraints.cypher
// Requires Neo4j Enterprise Edition
// Part of Mnemonic MVP

// Existence constraints (properties must exist)
CREATE CONSTRAINT pattern_name_exists IF NOT EXISTS
FOR (p:Pattern) REQUIRE p.name IS NOT NULL;

CREATE CONSTRAINT agent_name_exists IF NOT EXISTS
FOR (a:Agent) REQUIRE a.name IS NOT NULL;

CREATE CONSTRAINT concept_name_exists IF NOT EXISTS
FOR (c:Concept) REQUIRE c.name IS NOT NULL;
```

### Startup Constraint Validation

Neo4j is required. Mnemonic fails to start if Neo4j is unreachable. Missing constraints are not fatal -- Mnemonic validates constraints at startup and logs warnings, allowing operators to remediate without blocking service startup.

**Validation Behavior:**

| Constraint Status | Mnemonic Behavior |
|-------------------|-------------------|
| All constraints exist | Log info message, continue startup |
| One or more missing | Log warning with missing constraint names, continue startup |
| Connection failure | Fatal error, Mnemonic refuses to start |

**Constraints Checked:**

The constraints checked depend on the Neo4j edition:

**Community Edition (3 constraints):**

- `pattern_id_unique` - Uniqueness on Pattern.id
- `agent_name_unique` - Uniqueness on Agent.name
- `concept_name_unique` - Uniqueness on Concept.name

**Enterprise Edition (6 constraints):**

- All Community Edition constraints above, plus:
- `pattern_name_exists` - Existence of Pattern.name
- `agent_name_exists` - Existence of Agent.name
- `concept_name_exists` - Existence of Concept.name

**Manual Constraint Creation:**

If constraints are missing, run the appropriate migration scripts manually:

```bash
# Using cypher-shell (Community + Enterprise)
cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/001_create_constraints.cypher

# Enterprise Edition only (optional)
cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/002_create_existence_constraints.cypher
```

### Index Configuration

```cypher
// src/migrations/neo4j/003_create_indexes.cypher
// Creates indexes for common query patterns
// Part of Mnemonic MVP

// Pattern lookup by name
CREATE INDEX pattern_name_index IF NOT EXISTS
FOR (p:Pattern) ON (p.name);

// Concept filtering by type
CREATE INDEX concept_type_index IF NOT EXISTS
FOR (c:Concept) ON (c.type);

// Full-text search on pattern content
CREATE FULLTEXT INDEX pattern_content_fulltext IF NOT EXISTS
FOR (p:Pattern) ON EACH [p.name, p.description];

// Full-text search on concept names
CREATE FULLTEXT INDEX concept_name_fulltext IF NOT EXISTS
FOR (c:Concept) ON EACH [c.name];
```

### Node Creation Patterns

**Agent Node:**

```cypher
// Create or update agent node (sync from Postgres)
MERGE (a:Agent {name: $name})
ON CREATE SET
    a.createdAt = datetime()
ON MATCH SET
    a.updatedAt = datetime()
RETURN a;
```

**Pattern Node:**

```cypher
// Create or update pattern node (sync from Postgres after enrichment)
MERGE (p:Pattern {id: $id})
ON CREATE SET
    p.name = $name,
    p.description = $description,
    p.createdAt = datetime()
ON MATCH SET
    p.name = $name,
    p.description = $description,
    p.updatedAt = datetime()
RETURN p;
```

**Concept Node:**

```cypher
// Create concept if not exists (extracted during enrichment)
MERGE (c:Concept {name: $name})
ON CREATE SET
    c.type = $type,
    c.createdAt = datetime()
RETURN c;
```

### Relationship Definitions

**RELEVANT_FOR (Pattern to Agent):**

```cypher
// Create pattern-agent relevance relationship
// Mirrors pattern_agent_associations table
MATCH (p:Pattern {id: $patternId})
MATCH (a:Agent {name: $agentName})
MERGE (p)-[r:RELEVANT_FOR]->(a)
SET r.relevance = $relevance,
    r.updatedAt = datetime()
RETURN r;
```

**MENTIONED_IN (Concept to Pattern):**

```cypher
// Create concept-pattern mention relationship
MATCH (c:Concept {name: $conceptName})
MATCH (p:Pattern {id: $patternId})
MERGE (c)-[r:MENTIONED_IN]->(p)
ON CREATE SET r.createdAt = datetime()
RETURN r;
```

**RELATED_TO (Pattern to Pattern):**

```cypher
// Create pattern-pattern similarity relationship
// Computed from shared concepts
MATCH (p1:Pattern {id: $patternId1})
MATCH (p2:Pattern {id: $patternId2})
WHERE p1 <> p2
MERGE (p1)-[r:RELATED_TO]->(p2)
SET r.similarity = $similarity,
    r.updatedAt = datetime()
RETURN r;
```

### Graph Synchronization Queries

**Sync Agent from Postgres:**

```cypher
// Called on agent create/update in Postgres
MERGE (a:Agent {name: $name})
SET a.updatedAt = datetime()
RETURN a;
```

**Sync Pattern with Associations:**

```cypher
// Called after pattern enrichment completes
// Transaction: create/update pattern, then set all associations

// Step 1: Create/update pattern node
MERGE (p:Pattern {id: $patternId})
SET p.name = $patternName,
    p.description = $patternDescription,
    p.updatedAt = datetime();

// Step 2: Remove old RELEVANT_FOR relationships
MATCH (p:Pattern {id: $patternId})-[r:RELEVANT_FOR]->()
DELETE r;

// Step 3: Create new RELEVANT_FOR relationships
UNWIND $associations AS assoc
MATCH (p:Pattern {id: $patternId})
MATCH (a:Agent {name: assoc.agentName})
CREATE (p)-[:RELEVANT_FOR {relevance: assoc.relevance}]->(a);
```

**Sync Concepts for Pattern:**

```cypher
// Called during enrichment to add extracted concepts
// $concepts: array of {name: string, type: string}

// Step 1: Remove old MENTIONED_IN relationships for this pattern
MATCH (:Concept)-[r:MENTIONED_IN]->(:Pattern {id: $patternId})
DELETE r;

// Step 2: Create concepts and relationships
UNWIND $concepts AS concept
MERGE (c:Concept {name: concept.name})
ON CREATE SET c.type = concept.type, c.createdAt = datetime()
WITH c
MATCH (p:Pattern {id: $patternId})
CREATE (c)-[:MENTIONED_IN]->(p);
```

**Compute RELATED_TO Edges:**

Called by `EnrichmentService.ProcessJob` after concept extraction to compute and store pattern-to-pattern similarity edges. Existing RELATED_TO edges for the pattern are deleted first, then recomputed from shared concepts. Only edges meeting the minimum similarity threshold are created. RELATED_TO is symmetric; edges are stored in one direction and queried without direction. See [Pattern Processing - RELATED_TO Edge Computation](pattern-processing.md#related_to-edge-computation) for the similarity formula.

```cypher
// ComputeRelatedToEdges: delete old edges, recompute from shared concepts
// Parameters: $patternId (UUID), $minSimilarity (float, e.g. 0.3)

// Step 1: Delete existing RELATED_TO edges for this pattern
MATCH (p:Pattern {id: $patternId})-[r:RELATED_TO]-()
DELETE r

// Step 2: Find other patterns sharing concepts, compute similarity, create edges
WITH 1 AS dummy
MATCH (p1:Pattern {id: $patternId})<-[:MENTIONED_IN]-(c:Concept)-[:MENTIONED_IN]->(p2:Pattern)
WHERE p1 <> p2
WITH p1, p2, count(DISTINCT c) AS sharedCount

// Count total concepts for each pattern
OPTIONAL MATCH (c1:Concept)-[:MENTIONED_IN]->(p1)
WITH p1, p2, sharedCount, count(DISTINCT c1) AS totalA
OPTIONAL MATCH (c2:Concept)-[:MENTIONED_IN]->(p2)
WITH p1, p2, sharedCount, totalA, count(DISTINCT c2) AS totalB

// Compute similarity = sharedConcepts / max(totalConceptsA, totalConceptsB)
WITH p1, p2, sharedCount,
     CASE WHEN totalA > totalB THEN totalA ELSE totalB END AS maxTotal
WITH p1, p2, sharedCount,
     CASE WHEN maxTotal = 0 THEN 0.0
          ELSE toFloat(sharedCount) / toFloat(maxTotal)
     END AS similarity
WHERE similarity >= $minSimilarity

// Create RELATED_TO edge (one direction only; queries use undirected traversal)
CREATE (p1)-[:RELATED_TO {similarity: similarity, updatedAt: datetime()}]->(p2)
```

**Find Related Patterns:**

Uses pre-computed RELATED_TO edges (created during enrichment) and collects shared concept names. See [service-layer.md "Resolving the find_related_patterns Data Gap"](service-layer.md#resolving-the-find_related_patterns-data-gap) for the design decision to use pre-computed edges rather than recomputing similarity at query time.

```cypher
// Query: find patterns related to a given pattern using pre-computed RELATED_TO edges
MATCH (p1:Pattern {id: $patternId})-[r:RELATED_TO]-(p2:Pattern)
WITH p2, r.similarity AS similarity

// Collect shared concept names
OPTIONAL MATCH (p1:Pattern {id: $patternId})<-[:MENTIONED_IN]-(c:Concept)-[:MENTIONED_IN]->(p2)
WITH p2, similarity, collect(c.name) AS conceptNames, count(c) AS sharedConcepts

ORDER BY similarity DESC
LIMIT $limit
RETURN p2.id AS id, p2.name AS name, sharedConcepts, similarity, conceptNames
```

**Find Patterns for Agent:**

```cypher
// Query: find patterns relevant to an agent, ordered by relevance
MATCH (p:Pattern)-[r:RELEVANT_FOR]->(a:Agent {name: $agentName})
RETURN p.id AS id, p.name AS name, r.relevance AS relevance
ORDER BY r.relevance DESC
LIMIT $limit;
```

**Cleanup Orphaned Concepts:**

```cypher
// Maintenance: remove concepts with no pattern relationships
MATCH (c:Concept)
WHERE NOT (c)-[:MENTIONED_IN]->()
DELETE c
RETURN count(c) AS deletedCount;
```

## Repository Interfaces

[Table of Contents](#table-of-contents)

> **Note:** These are interface definitions only. Implementation is handled by the go-software-engineer.

### AgentRepository

```go
// AgentRepository defines data access operations for agents.
// Agents are stored as JSONB documents with name as the unique lookup key.
// Implementation: internal/repository/agent/repository.go
type AgentRepository interface {
    // Create stores a new agent. The application computes crc64 from the
    // serialized definition before calling this method.
    // Returns ErrExists if name already exists.
    Create(ctx context.Context, agent *Agent) error

    // Get retrieves an agent by name. Returns ErrNotFound if not found.
    Get(ctx context.Context, name string) (*Agent, error)

    // GetByID retrieves an agent by UUID. Returns ErrNotFound if not found.
    GetByID(ctx context.Context, id uuid.UUID) (*Agent, error)

    // Update modifies an existing agent. The application computes crc64
    // from the serialized definition and sets updated_at before calling
    // this method. Returns ErrNotFound if not found.
    Update(ctx context.Context, agent *Agent) error

    // Delete removes an agent by name. Returns ErrNotFound if not found.
    Delete(ctx context.Context, name string) error

    // DeleteByID removes an agent by UUID. Returns ErrNotFound if not found.
    // The REST API routes DELETE by name; DeleteByID supports internal callers
    // that resolve agents by UUID (e.g., cascade operations, programmatic cleanup).
    DeleteByID(ctx context.Context, id uuid.UUID) error

    // List retrieves all agents with optional pagination.
    List(ctx context.Context, opts ListOptions) ([]*Agent, int64, error)

    // Exists checks if an agent with the given name exists.
    Exists(ctx context.Context, name string) (bool, error)

    // GetManifest returns name and crc64 for all agents (used by sync protocol).
    GetManifest(ctx context.Context) ([]ManifestEntry, error)
}

// Agent represents an agent definition stored as a JSONB document.
type Agent struct {
    ID         uuid.UUID       `db:"id"`
    Name       string          `db:"name"`
    Definition json.RawMessage `db:"definition"` // JSONB document
    CRC64      string          `db:"crc64"`       // CRC-64 checksum (decimal string)
    CreatedAt  time.Time       `db:"created_at"`
    UpdatedAt  time.Time       `db:"updated_at"`
}

// ManifestEntry represents a single entity in the sync manifest.
type ManifestEntry struct {
    Name  string `db:"name"`
    CRC64 string `db:"crc64"`
}
```

### PatternRepository

```go
// PatternRepository defines data access operations for patterns.
// Patterns use relational columns (NOT the JSONB document model).
// Implementation: internal/repository/pattern/repository.go
type PatternRepository interface {
    // Create stores a new pattern. Returns ErrNameExists if name exists.
    Create(ctx context.Context, pattern *Pattern) error

    // Get retrieves a pattern by ID. Returns ErrNotFound if not found.
    Get(ctx context.Context, id uuid.UUID) (*Pattern, error)

    // GetByName retrieves a pattern by name. Returns ErrNotFound if not found.
    GetByName(ctx context.Context, name string) (*Pattern, error)

    // Update modifies an existing pattern. The application sets updated_at
    // before calling this method. Returns ErrNotFound if not found.
    Update(ctx context.Context, pattern *Pattern) error

    // Delete removes a pattern by ID. Returns ErrNotFound if not found.
    Delete(ctx context.Context, id uuid.UUID) error

    // List retrieves patterns with filtering and pagination.
    List(ctx context.Context, filter Filter, opts ListOptions) ([]*Pattern, int64, error)

    // UpdateEmbedding stores the embedding vector for a pattern.
    UpdateEmbedding(ctx context.Context, id uuid.UUID, embedding []float32) error

    // UpdateEnrichmentStatus updates the enrichment state of a pattern.
    UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, err error) error

    // FindSimilar finds patterns similar to the given embedding vector.
    FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error)

    // SetAgentAssociations replaces all agent associations for a pattern.
    SetAgentAssociations(ctx context.Context, patternID uuid.UUID, associations []AgentAssociation) error

    // GetAgentAssociations retrieves all agent associations for a pattern.
    GetAgentAssociations(ctx context.Context, patternID uuid.UUID) ([]AgentAssociation, error)

    // GetPatternIDsByAgent returns all pattern IDs associated with the given agent.
    // Used by SearchService for agent-scoped similarity search pre-filtering.
    // See: service-layer.md "Agent Filter for search_patterns"
    GetPatternIDsByAgent(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error)
}

// Pattern represents a context pattern (relational columns, not JSONB).
type Pattern struct {
    ID               uuid.UUID  `db:"id"`
    Name             string     `db:"name"`
    Description      *string    `db:"description"`
    Content          string     `db:"content"`
    Tags             []string   `db:"-"` // Unmarshaled from JSONB
    Embedding        []float32  `db:"-"` // Vector type
    EnrichmentStatus string     `db:"enrichment_status"`
    EnrichmentError  *string    `db:"enrichment_error"`
    EnrichedAt       *time.Time `db:"enriched_at"`
    CreatedAt        time.Time  `db:"created_at"`
    UpdatedAt        time.Time  `db:"updated_at"`
}

// SimilarityOptions defines options for similarity search.
type SimilarityOptions struct {
    MinSimilarity float64     // Minimum similarity threshold (0.0-1.0)
    MaxResults    int         // Maximum number of results
    Tags          []string    // Optional tag filter
    PatternIDs    []uuid.UUID // Optional filter to specific pattern IDs
}

// Match represents a similarity search result.
type Match struct {
    Pattern    *Pattern
    Similarity float64
}

// AgentAssociation represents a pattern-agent relationship.
type AgentAssociation struct {
    AgentID   uuid.UUID `db:"agent_id"`
    Relevance float64   `db:"relevance"`
}
```

**GetPatternIDsByAgent SQL:**

```sql
-- GetPatternIDsByAgent: returns all pattern IDs associated with a given agent.
-- Uses idx_pattern_agent_assoc_agent index for efficient lookup.
SELECT pattern_id FROM pattern_agent_associations WHERE agent_id = $1;
```

```go
// Filter defines filtering options for pattern queries.
// This type is package-specific to internal/repository/pattern.
type Filter struct {
    Tags             []string // Filter by any of these tags
    EnrichmentStatus string   // Filter by enrichment status
    SearchQuery      string   // Full-text search in name/description
}
```

### SkillRepository

```go
// SkillRepository defines data access operations for skills.
// Skills are stored as JSONB documents with name as the unique lookup key.
// Implementation: internal/repository/skill/repository.go
type SkillRepository interface {
    // Create stores a new skill. The application computes crc64 from the
    // serialized definition before calling this method.
    // Returns ErrExists if name already exists.
    Create(ctx context.Context, skill *Skill) error

    // Get retrieves a skill by ID. Returns ErrNotFound if not found.
    Get(ctx context.Context, id uuid.UUID) (*Skill, error)

    // GetByName retrieves a skill by name. Returns ErrNotFound if not found.
    GetByName(ctx context.Context, name string) (*Skill, error)

    // Update modifies an existing skill. Returns ErrNotFound if not found.
    Update(ctx context.Context, skill *Skill) error

    // Delete removes a skill by ID. Returns ErrNotFound if not found.
    Delete(ctx context.Context, id uuid.UUID) error

    // List retrieves skills with optional pagination.
    List(ctx context.Context, opts ListOptions) ([]*Skill, int64, error)

    // Exists checks if a skill with the given ID exists.
    Exists(ctx context.Context, id uuid.UUID) (bool, error)

    // GetManifest returns name and crc64 for all skills (used by sync protocol).
    GetManifest(ctx context.Context) ([]ManifestEntry, error)
}

// Skill represents a skill definition stored as a JSONB document.
type Skill struct {
    ID         uuid.UUID       `db:"id"`
    Name       string          `db:"name"`
    Definition json.RawMessage `db:"definition"` // JSONB document
    CRC64      string          `db:"crc64"`       // CRC-64 checksum (decimal string)
    CreatedAt  time.Time       `db:"created_at"`
    UpdatedAt  time.Time       `db:"updated_at"`
}
```

### SkillFileRepository

```go
// SkillFileRepository defines data access operations for skill child files.
// Skill files are keyed by (skill_id, path).
// Implementation: internal/repository/skillfile/repository.go
type SkillFileRepository interface {
    // Create stores a new skill file. Returns ErrExists if the
    // (skill_id, path) combination already exists.
    Create(ctx context.Context, file *SkillFile) error

    // Get retrieves a skill file by ID. Returns ErrNotFound if not found.
    Get(ctx context.Context, id uuid.UUID) (*SkillFile, error)

    // GetByPath retrieves a skill file by skill ID and path.
    // Returns ErrNotFound if not found.
    GetByPath(ctx context.Context, skillID uuid.UUID, path string) (*SkillFile, error)

    // Update modifies an existing skill file. Returns ErrNotFound if not found.
    Update(ctx context.Context, file *SkillFile) error

    // Delete removes a skill file by ID. Returns ErrNotFound if not found.
    Delete(ctx context.Context, id uuid.UUID) error

    // ListBySkill retrieves all files for a given skill.
    ListBySkill(ctx context.Context, skillID uuid.UUID) ([]*SkillFile, error)

    // DeleteBySkill removes all files for a given skill.
    DeleteBySkill(ctx context.Context, skillID uuid.UUID) error
}

// SkillFile represents a child file for a skill.
type SkillFile struct {
    ID        uuid.UUID `db:"id"`
    SkillID   uuid.UUID `db:"skill_id"`
    Path      string    `db:"path"`
    Content   string    `db:"content"`
    CRC64     string    `db:"crc64"`
    CreatedAt time.Time `db:"created_at"`
    UpdatedAt time.Time `db:"updated_at"`
}
```

### EnrichmentJob Repository

```go
// EnrichmentJobRepository defines data access operations for enrichment jobs.
// Implementation: internal/repository/enrichmentjob/repository.go
type EnrichmentJobRepository interface {
    // Create stores a new enrichment job.
    Create(ctx context.Context, job *Job) error

    // Get retrieves an enrichment job by ID. Returns ErrNotFound if not found.
    Get(ctx context.Context, id uuid.UUID) (*Job, error)

    // GetByPatternID retrieves the latest job for a pattern.
    GetByPatternID(ctx context.Context, patternID uuid.UUID) (*Job, error)

    // ClaimPending atomically claims a pending job for processing.
    // Uses FOR UPDATE SKIP LOCKED for safe concurrent processing.
    // Returns nil if no pending jobs are available.
    ClaimPending(ctx context.Context) (*Job, error)

    // MarkProcessing updates job status to processing with start time.
    MarkProcessing(ctx context.Context, id uuid.UUID) error

    // MarkCompleted updates job status to completed with completion time.
    MarkCompleted(ctx context.Context, id uuid.UUID) error

    // MarkFailed updates job status to failed with error message.
    // Increments attempt count and schedules retry if under max_attempts.
    MarkFailed(ctx context.Context, id uuid.UUID, err error, retryDelay time.Duration) error

    // ReclaimStale reclaims jobs stuck in processing state.
    // Jobs older than timeout are reset to pending for retry.
    ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error)

    // DeleteCompleted removes completed jobs older than the retention period.
    DeleteCompleted(ctx context.Context, retention time.Duration) (int64, error)

    // DeleteFailed removes failed jobs older than the retention period.
    DeleteFailed(ctx context.Context, retention time.Duration) (int64, error)

    // List retrieves enrichment jobs with filtering and pagination.
    List(ctx context.Context, filter Filter, opts ListOptions) ([]*Job, int64, error)
}

// Job represents a background enrichment task.
type Job struct {
    ID           uuid.UUID  `db:"id"`
    PatternID    uuid.UUID  `db:"pattern_id"`
    Status       string     `db:"status"`
    Attempts     int        `db:"attempts"`
    MaxAttempts  int        `db:"max_attempts"`
    LastError    *string    `db:"last_error"`
    ScheduledFor time.Time  `db:"scheduled_for"`
    StartedAt    *time.Time `db:"started_at"`
    CompletedAt  *time.Time `db:"completed_at"`
    CreatedAt    time.Time  `db:"created_at"`
    UpdatedAt    time.Time  `db:"updated_at"`
}

// Filter defines filtering options for job queries.
// This type is package-specific to internal/repository/enrichmentjob.
type Filter struct {
    Status    *string    // Filter by job status (pending, processing, completed, failed)
    PatternID *uuid.UUID // Filter by the associated pattern ID
}
```

### GraphRepository

```go
// GraphRepository defines data access operations for the Neo4j knowledge graph.
// Implementation: internal/repository/graph/repository.go
type GraphRepository interface {
    // SyncAgent creates or updates an agent node in the graph.
    SyncAgent(ctx context.Context, agentName string) error

    // DeleteAgent removes an agent node and its relationships.
    DeleteAgent(ctx context.Context, agentName string) error

    // SyncPattern creates or updates a pattern node with its relationships.
    SyncPattern(ctx context.Context, pattern *GraphPattern) error

    // DeletePattern removes a pattern node and its relationships.
    DeletePattern(ctx context.Context, patternID uuid.UUID) error

    // SyncConcepts creates concepts and their relationships to a pattern.
    SyncConcepts(ctx context.Context, patternID uuid.UUID, concepts []Concept) error

    // SetPatternAgentRelevance sets the relevance relationships for a pattern.
    SetPatternAgentRelevance(ctx context.Context, patternID uuid.UUID, associations []AgentAssociation) error

    // ComputeRelatedToEdges computes and creates RELATED_TO edges between
    // the given pattern and other patterns sharing concepts. Existing
    // RELATED_TO edges for this pattern are deleted first, then recomputed.
    // Only edges with similarity >= minSimilarity are created.
    // Called by EnrichmentService.ProcessJob after concept extraction.
    // See: service-layer.md "RELATED_TO Edge Computation"
    ComputeRelatedToEdges(ctx context.Context, patternID uuid.UUID, minSimilarity float64) error

    // FindRelatedPatterns finds patterns related through shared concepts.
    // Uses pre-computed RELATED_TO edges and collects shared concept names.
    FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]RelatedPattern, error)

    // FindPatternsByAgent finds patterns relevant to an agent.
    FindPatternsByAgent(ctx context.Context, agentName string, limit int) ([]PatternRelevance, error)

    // CleanupOrphanedConcepts removes concepts with no pattern relationships.
    CleanupOrphanedConcepts(ctx context.Context) (int64, error)

    // HealthCheck verifies the graph database connection.
    HealthCheck(ctx context.Context) error
}

// GraphPattern represents pattern data for graph synchronization.
type GraphPattern struct {
    ID          uuid.UUID
    Name        string
    Description *string
}

// Concept represents an extracted concept entity.
type Concept struct {
    Name string // Normalized lowercase
    Type string // technology, practice, domain
}

// RelatedPattern represents a pattern found through graph traversal.
// Updated to include similarity score and shared concept names for the
// MCP find_related_patterns tool.
// See: service-layer.md "Resolving the find_related_patterns Data Gap"
type RelatedPattern struct {
    ID             uuid.UUID
    Name           string
    SharedConcepts int      // Count of shared concepts (retained for backward compat)
    Similarity     float64  // Pre-computed similarity score from RELATED_TO edge (0.0-1.0)
    ConceptNames   []string // Names of the shared concepts
}

// AgentAssociation represents an agent relevance pair for graph sync.
// Uses AgentName (not AgentID) because Neo4j Agent nodes are keyed by name.
// See: service-layer.md "Enrichment Job Lifecycle" step 6
type AgentAssociation struct {
    AgentName string
    Relevance float64
}

// PatternRelevance represents a pattern with its relevance to an agent.
type PatternRelevance struct {
    ID        uuid.UUID
    Name      string
    Relevance float64
}
```

### Common Types

`ListOptions` is a shared type used across all repository packages that support pagination.

`Filter` types are **not** shared -- each repository package defines its own `Filter` struct with fields specific to that entity. See the Filter definitions in [PatternRepository](#patternrepository) and [EnrichmentJob Repository](#enrichmentjob-repository).

```go
// ListOptions defines pagination parameters.
// Shared across all repository packages.
type ListOptions struct {
    Offset int // Number of items to skip
    Limit  int // Maximum items to return (0 = no limit)
}

// Repository errors (package-specific)

// agent package errors
var (
    ErrNotFound = errors.New("agent not found")
    ErrExists   = errors.New("agent already exists")
)

// pattern package errors
var (
    ErrNotFound   = errors.New("pattern not found")
    ErrNameExists = errors.New("pattern name already exists")
)

// skill package errors
var (
    ErrNotFound = errors.New("skill not found")
    ErrExists   = errors.New("skill already exists")
)

// skillfile package errors
var (
    ErrNotFound  = errors.New("skill file not found")
    ErrExists    = errors.New("skill file already exists")
)

// enrichmentjob package errors
var (
    ErrNotFound  = errors.New("enrichment job not found")
    ErrNoPending = errors.New("no pending enrichment jobs available")
)
```

## Connection Configuration

[Table of Contents](#table-of-contents)

> **Architecture Reference:** [Configuration - Database Connections](configuration.md#configuration-file)

### PostgreSQL Connection

**Configuration Structure:**

```go
// PostgresConfig defines PostgreSQL connection parameters.
// Viper handles env var binding via viper.BindEnv() or viper.AutomaticEnv().
type PostgresConfig struct {
    Host            string        `mapstructure:"host"`
    Port            int           `mapstructure:"port"`
    Database        string        `mapstructure:"database"`
    Username        string        `mapstructure:"username"`
    Password        string        `mapstructure:"password"`
    SSLMode         string        `mapstructure:"ssl_mode"`
    MaxOpenConns    int           `mapstructure:"max_open_conns"`
    MaxIdleConns    int           `mapstructure:"max_idle_conns"`
    ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}
```

**Recommended Pool Settings:**

| Setting | Development | Production (Single Pod) | Production (Multi-Pod) |
|---------|-------------|-------------------------|------------------------|
| MaxOpenConns | 5 | 25 | 15 per pod |
| MaxIdleConns | 2 | 5 | 3 per pod |
| ConnMaxLifetime | 5m | 1h | 1h |

### Neo4j Connection

**Configuration Structure:**

```go
// Neo4jConfig defines Neo4j connection parameters.
// Viper handles env var binding via viper.BindEnv() or viper.AutomaticEnv().
type Neo4jConfig struct {
    URI                          string        `mapstructure:"uri"`
    Username                     string        `mapstructure:"username"`
    Password                     string        `mapstructure:"password"`
    Database                     string        `mapstructure:"database"`
    MaxConnectionPoolSize        int           `mapstructure:"max_connection_pool_size"`
    ConnectionAcquisitionTimeout time.Duration `mapstructure:"connection_acquisition_timeout"`
}
```

**Recommended Pool Settings:**

| Setting | Development | Production |
|---------|-------------|------------|
| MaxConnectionPoolSize | 10 | 50 |
| ConnectionAcquisitionTimeout | 30s | 60s |

### Connection String Formats

**PostgreSQL DSN:**

```text
# Standard format
postgres://username:password@host:port/database?sslmode=prefer

# With all options
postgres://mnemonic:secret@localhost:5432/mnemonic?sslmode=require&connect_timeout=10

# pgx-style (recommended for Go)
host=localhost port=5432 dbname=mnemonic user=mnemonic password=secret sslmode=prefer pool_max_conns=25
```

**Neo4j URI:**

```text
# Bolt protocol (standard)
bolt://localhost:7687

# Bolt with encryption
bolt+s://neo4j.example.com:7687

# Neo4j AuraDB (cloud)
neo4j+s://xxxx.databases.neo4j.io
```

**Environment Variable Examples:**

```bash
# PostgreSQL
export MNEMONIC_DATABASE_POSTGRES_HOST="localhost"
export MNEMONIC_DATABASE_POSTGRES_PORT="5432"
export MNEMONIC_DATABASE_POSTGRES_DATABASE="mnemonic"
export MNEMONIC_DATABASE_POSTGRES_USERNAME="mnemonic"
export MNEMONIC_DATABASE_POSTGRES_PASSWORD="secure-password"
export MNEMONIC_DATABASE_POSTGRES_SSL_MODE="require"

# Neo4j
export MNEMONIC_DATABASE_NEO4J_URI="bolt://localhost:7687"
export MNEMONIC_DATABASE_NEO4J_USERNAME="neo4j"
export MNEMONIC_DATABASE_NEO4J_PASSWORD="secure-password"
export MNEMONIC_DATABASE_NEO4J_DATABASE="neo4j"
```

## References

[Table of Contents](#table-of-contents)

**Architecture Documents:**

- [Data Architecture](../../architecture/04-data-architecture.md) - Data model design and storage decisions
- [System Architecture](../../architecture/02-system-architecture.md) - Component overview
- [Deployment Architecture](../../architecture/06-deployment-architecture.md) - Deployment patterns

**Design Documents:**

- [API Specification](2026-02-15-pivot-api-specification.md) - REST and MCP API contracts
- [Configuration](configuration.md) - Server configuration including database settings
- [Pattern Processing](pattern-processing.md) - Enrichment pipeline using this data layer

**External References:**

- [golang-migrate](https://github.com/golang-migrate/migrate) - Migration tool
- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver for Go
- [pgvector](https://github.com/pgvector/pgvector) - Vector similarity search extension
- [neo4j-go-driver](https://github.com/neo4j/neo4j-go-driver) - Neo4j driver for Go
- [Claude Code Agent Skills Spec](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/skills) - Skill definition format
