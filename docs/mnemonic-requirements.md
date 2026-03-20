# Mnemonic Requirements

[Back to Architecture Overview](architecture/README.md) | [Back to Project README](../README.md)

## Table of Contents

- [Overview](#overview)
- [Knowledge Management](#knowledge-management)
- [Tooling Management](#tooling-management)
- [Tooling Synchronization](#tooling-synchronization)
- [Administration](#administration)
- [Observability](#observability)
- [Security](#security)
- [Deployment](#deployment)
- [Scope Boundaries](#scope-boundaries)
- [Assumptions and Constraints](#assumptions-and-constraints)

## Overview

Mnemonic solves the problem of inconsistent tooling and isolated knowledge in teams using Claude Code.
See [mnemonic-concept.md](mnemonic-concept.md) for the full problem statement and rationale.

Mnemonic must provide two primary capabilities: a searchable team knowledge base and a synchronized
tooling library. An admin REST API handles all writes; a read-only MCP interface serves Claude Code.

## Knowledge Management

<a id="km-1"></a>**KM-1.** Mnemonic must store team knowledge as named, searchable patterns. Each pattern has a name,
description, content body (up to 10KB), and optional tags.

<a id="km-2"></a>**KM-2.** Mnemonic must support semantic search over patterns so that a natural-language query
returns the most relevant patterns by meaning, not just keyword match.

<a id="km-3"></a>**KM-3.** Mnemonic must expose relationships between patterns so that a caller can discover patterns
related to a given pattern by shared concepts or associations.

<a id="km-4"></a>**KM-4.** Mnemonic must track the enrichment state of each pattern (pending, enriched, failed) and
only include fully enriched patterns in semantic search results.

<a id="km-5"></a>**KM-5.** Mnemonic must retry failed pattern enrichment and surface enrichment errors to
administrators.

<a id="km-6"></a>**KM-6.** Mnemonic must allow patterns to be associated with specific agents, with a relevance score
per association.

## Tooling Management

<a id="tm-1"></a>**TM-1.** Mnemonic must store agent definitions as named, retrievable documents.

<a id="tm-2"></a>**TM-2.** Mnemonic must store skill definitions as named, retrievable documents. Skills may include
child files (scripts, references, assets) that are returned as part of the skill definition.

<a id="tm-4"></a>**TM-4.** Mnemonic must enforce consistent naming rules for agents and skills so that
names are URL-safe and predictable.

<a id="tm-5"></a>**TM-5.** Mnemonic must support full create, update, and delete operations for all tooling types via
the admin API.

<a id="tm-6"></a>**TM-6.** Mnemonic must detect whether a tooling definition has changed since it was last stored,
without requiring a full content comparison.

## Tooling Synchronization

<a id="ts-1"></a>**TS-1.** Mnemonic must expose a synchronization manifest that a client can use to determine which
agents and skills are new or changed since the client last synced.

<a id="ts-2"></a>**TS-2.** The synchronization manifest must include enough information for a client to fetch only
what has changed, enabling incremental sync.

<a id="ts-3"></a>**TS-3.** Mnemonic must return complete skill definitions including all child files in a single
response, so a sync client does not need multiple round-trips per skill.

<a id="ts-4"></a>**TS-4.** Mnemonic must serve list and detail endpoints for agents and skills so a sync
client can discover and retrieve any item individually.

## Administration

<a id="ad-1"></a>**AD-1.** Mnemonic must expose an administrative API for all write operations: creating, updating,
and deleting patterns, agents, and skills.

<a id="ad-2"></a>**AD-2.** The admin API must return structured error responses so that scripts and CI pipelines can
detect and handle failures programmatically.

<a id="ad-3"></a>**AD-3.** The admin API must support listing patterns with filtering by tag and full-text search on
name and description.

<a id="ad-4"></a>**AD-4.** The admin API must accept pattern uploads and immediately acknowledge them, completing
semantic enrichment asynchronously in the background.

<a id="ad-5"></a>**AD-5.** The admin API must expose an endpoint to manually trigger retry of failed enrichment jobs.

## Observability

<a id="ob-1"></a>**OB-1.** Mnemonic must emit structured logs for all incoming requests on both the admin and MCP
interfaces, including method, path, status code, and latency.

<a id="ob-2"></a>**OB-2.** Mnemonic must expose a health check endpoint that indicates whether the service and its
storage dependencies are ready to serve traffic.

<a id="ob-3"></a>**OB-3.** Mnemonic must expose metrics sufficient to monitor request rates, error rates, and latency
for both the admin API and MCP server.

<a id="ob-4"></a>**OB-4.** Mnemonic must log enrichment job activity, including successful completions, failures, and
retry attempts.

## Security

<a id="se-1"></a>**SE-1.** In MVP (local deployment), Mnemonic may operate without authentication, as it runs in
a trusted network environment.

<a id="se-2"></a>**SE-2.** In production (Post-MVP), the admin API must be protected by
infrastructure-layer authentication and authorization. The MCP interface may remain unauthenticated
within a trusted internal network.

<a id="se-3"></a>**SE-3.** Mnemonic must never store user credentials or session tokens.

<a id="se-4"></a>**SE-4.** The MCP interface must be read-only. No write operations may be performed through it.

<a id="se-5"></a>**SE-5.** In production, all external traffic must be carried over TLS.

<a id="se-6"></a>**SE-6.** Database credentials must not be embedded in the deployed service. They must be injected
from the runtime environment.

## Deployment

<a id="dp-1"></a>**DP-1.** Mnemonic must be deployable to both local development and production environments.

<a id="dp-2"></a>**DP-2.** Mnemonic must be stateless. All persistent state must reside in external storage so that
multiple instances can run without coordination.

<a id="dp-3"></a>**DP-3.** Mnemonic must verify schema compatibility at startup (e.g., by checking that the database
schema version is compatible with the running version). Mnemonic does not manage schema versioning or
run migrations. Migrations are applied externally by the golang-migrate CLI as a deployment step,
independent of the application lifecycle.

## Scope Boundaries

Mnemonic's scope is knowledge storage and tooling synchronization. The following boundaries define
what the system is responsible for:

<a id="sb-1"></a>- Mnemonic's only use of external AI services is embedding generation for pattern enrichment.
<a id="sb-2"></a>- All workflow and orchestration decisions belong to the user.
<a id="sb-3"></a>- All tool execution and file operations happen on the client side.
<a id="sb-4"></a>- Identity management and access policy enforcement belong to the infrastructure layer.
<a id="sb-5"></a>- All interaction with Mnemonic is programmatic (API and protocol).
<a id="sb-6"></a>- The primary data store is the source of truth. Secondary stores are eventually consistent.

## Assumptions and Constraints

- Workstations running Claude Code can reach the Mnemonic MCP endpoint over the local or team
  network.
- A team member or CI process takes responsibility for loading and maintaining patterns and tooling
  definitions via the admin API.
- Pattern content is curator-supplied. Mnemonic does not generate or validate content quality.
- The external embedding API (used during pattern enrichment) is available. If it is unavailable,
  enrichment will be retried; patterns will not be searchable until enrichment succeeds.
- Expected pattern counts are in the range of hundreds to tens of thousands, not millions.
- Mnemonic does not enforce multi-tenancy. A single deployment serves one team.

**Next:** [Architectural Decisions](architecture/00-architectural-decisions.md)
