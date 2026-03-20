# MCP Tools

[Back to Overview](README.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Overview](#overview)
- [Tool Discovery](#tool-discovery)
- [Pattern Search Tools](#pattern-search-tools)
- [Response Formats](#response-formats)
- [Error Handling](#error-handling)
- [References](#references)

## Overview

Mnemonic exposes 3 read-only MCP tools for pattern search: `search_patterns`, `find_related_patterns`, and `get_pattern`. These tools are called during conversation when Claude Code needs team knowledge.

## Tool Discovery

Claude Code discovers and invokes tools through four steps:

1. **Connection**: Claude Code connects via MCP over Streamable HTTP to `http://localhost:8081/mcp`
2. **Initialization**: sends `initialize` to establish the session
3. **Discovery**: sends `tools/list` to receive all 3 tool registrations
4. **Invocation**: during conversation, sends `tools/call` with the tool name and arguments

Client configuration:

```json
{
  "mcpServers": {
    "mnemonic": {
      "type": "streamable-http",
      "url": "http://localhost:8081/mcp"
    }
  }
}
```

## Pattern Search Tools

These three tools are called during conversation when Claude Code determines that team knowledge is relevant to a user request.

### `search_patterns`

Semantic search over the team knowledge graph.

**Parameters:**

| Parameter   | Type         | Required | Default | Constraints | Description                          |
| ----------- | ------------ | -------- | ------- | ----------- | ------------------------------------ |
| `query`     | string       | yes      | —       | —           | Natural language search query        |
| `limit`     | integer      | no       | 10      | max 50      | Maximum number of results to return  |
| `threshold` | number       | no       | 0.7     | 0.0–1.0     | Minimum cosine similarity score      |
| `tags`      | string array | no       | —       | —           | Conjunctive (AND) filter by tag      |
| `agent`     | string       | no       | —       | —           | Filter results by agent association  |

**Returns:** Markdown-formatted text with ranked results. Each result includes pattern name, similarity percentage, tags, and full content. The similarity percentage reflects the vector similarity score.

**Notes:** Only enriched patterns appear in results. Results are ranked by PGVector cosine similarity (vector similarity only for MVP). Post-MVP enhancement: blended scoring combining vector similarity with Neo4j graph context is planned. See [Pattern Processing](../design/pattern-processing.md) for details.

### `find_related_patterns`

Find patterns related to a given pattern via the knowledge graph.

**Parameters:**

| Parameter    | Type    | Required | Default | Constraints | Description                              |
| ------------ | ------- | -------- | ------- | ----------- | ---------------------------------------- |
| `pattern_id` | UUID    | yes      | —       | —           | ID of the pattern to find relations for  |
| `limit`      | integer | no       | 5       | max 20      | Maximum number of related patterns       |

**Returns:** Markdown with related patterns, relationship type, similarity score, and shared concepts.

**Notes:** Traverses `RELATED_TO` edges in Neo4j. Similarity score (0.0–1.0) reflects concept overlap between the source pattern and each related pattern.

### `get_pattern`

Retrieve a specific pattern by ID.

**Parameters:**

| Parameter | Type | Required | Description         |
| --------- | ---- | -------- | ------------------- |
| `id`      | UUID | yes      | Pattern ID to fetch |

**Returns:** Markdown with full pattern content, metadata, related patterns, and extracted concepts.

**Notes:** Queries both PostgreSQL (for pattern content and metadata) and Neo4j (for graph relationships). The graph section is omitted when enrichment is still pending.

## Response Formats

MCP tools return markdown-formatted text. Structured data such as similarity scores and metadata is embedded in the prose because Claude Code consumes tool results as text content during conversation. Markdown makes results readable in context without additional parsing.

Results are delivered as MCP tool results with:

```json
{"content": [{"type": "text", "text": "..."}]}
```

## Error Handling

Mnemonic uses two error mechanisms depending on whether the failure is at the application or transport level.

### Application-Level Errors

For not-found conditions, validation failures, and backend unavailability, Mnemonic returns a normal JSON-RPC 200 response with `isError: true` in the tool result. The `isError` flag signals to Claude Code that the tool call did not succeed. Claude Code can retry or inform the user accordingly.

| Pattern             | Applies To                                                       |
| ------------------- | ---------------------------------------------------------------- |
| Not found           | `get_pattern` and `find_related_patterns`                        |
| Invalid input       | All 3 pattern search tools                                       |
| Service unavailable | All 3 tools — returned when a backend database is unreachable    |

### Transport-Level Errors

For protocol problems, Mnemonic returns a JSON-RPC error response. These use standard JSON-RPC 2.0 error codes:

| Code    | Meaning         | Description                              |
| ------- | --------------- | ---------------------------------------- |
| -32700  | Parse error     | Malformed JSON                           |
| -32600  | Invalid request | Not a valid JSON-RPC 2.0 request         |
| -32601  | Method not found| Unknown tool name                        |
| -32602  | Invalid params  | Parameters do not match the tool schema  |
| -32603  | Internal error  | Unexpected server error                  |

## References

- [Pivot API Specification — MCP Tool Definitions](../design/2026-02-15-pivot-api-specification.md#3-mcp-tool-definitions) — design rationale and wire format details
- [Communication Patterns](03-communication-patterns.md) — protocol overview and request flow

**Next:** Return to [Architecture Overview](README.md)

---

Copyright (c) 2025 Jeremy K. Johnson. All rights reserved.
